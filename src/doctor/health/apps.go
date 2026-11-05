package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	tmuxServerWarnKB = 1024 * 1024 // tmux サーバの常駐量がこれ以上 = 注意 (スクロールバックが溜まっている)
	nvimSwapOld      = 7 * 24 * time.Hour
	chromeApp        = "/Applications/Google Chrome.app"
)

// lspNames は nvim が起こす言語サーバのうち、ネイティブの実行ファイルのもの。親 (nvim) が先に死ぬと launchd の子になって残る。
// 🚨 node で動く言語サーバ (typescript-language-server・vscode-*-language-server・pyright など) は載せない: ucomm は "node" で、
// 名前で見分けるには引数を読むしかなく、引数は秘密が載りうるので読まない。node のものは検出できないと Note に書く。
// ruby-lsp・solargraph も同じ理由 (ucomm は "ruby")。
var lspNames = []string{"gopls", "lua-language-server", "rust-analyzer", "clangd", "sourcekit-lsp", "marksman", "terraform-ls"}

func (s *scanner) tmux(ctx context.Context) []Check {
	cs := []Check{}
	snap, err := s.snapshot(ctx)
	if err != nil {
		cs = append(cs, unknown("tmux.server", "tmux のサーバ", err.Error()))
	} else {
		cs = append(cs, tmuxServers(snap.Second))
	}
	// 読むだけのサブコマンドしか打たない。対象は今の環境のサーバ ($TMUX があればそれ、無ければ既定の socket)
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "tmux", "list-panes", "-a", "-F", "#{history_size}\t#{history_limit}\t#{pane_dead}")
	if err != nil || rc != 0 {
		// サーバが無いのは 2 つの形: socket が残って誰も listen していない (no server running) と、socket が無い
		// (error connecting to … (No such file or directory))。"error connecting to" は権限の拒否 (Permission denied・sandbox) でも
		// 出るので、それは判定不能のままにする (サーバは動いているかもしれない)
		if err == nil && (strings.Contains(stderr, "no server running") ||
			(strings.Contains(stderr, "error connecting to") && strings.Contains(stderr, "(No such file or directory)"))) {
			return append(cs, Check{ID: "tmux.panes", Label: "tmux の pane", Value: "サーバが動いていない", Status: StatusInfo})
		}
		return append(cs, unknown("tmux.panes", "tmux の pane", cmdNote("tmux", rc, err, stderr)))
	}
	cs = append(cs, tmuxPanes(out))
	if out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "tmux", "list-clients", "-F", "#{client_pid}"); err != nil || rc != 0 {
		cs = append(cs, unknown("tmux.clients", "tmux のクライアント", cmdNote("tmux", rc, err, stderr)))
	} else {
		n := len(strings.Fields(out))
		cs = append(cs, Check{ID: "tmux.clients", Label: "tmux のクライアント (attach している端末)", Value: fmt.Sprintf("%d 個", n), Status: StatusInfo,
			Note: "macOS 15 / 26 ではクライアントの数とカーネルの data.kalloc.1024 の漏れが連動した (proc の観点の同じ項目と併せて見る)"})
	}
	return cs
}

// tmuxServers は launchd の子になっている tmux (= サーバ) の数と常駐量。
func tmuxServers(ps []proc) Check {
	c := Check{ID: "tmux.server", Label: "tmux のサーバ"}
	var parts []string
	var big []string
	for _, p := range ps {
		if p.Name() != "tmux" || p.PPID != 1 {
			continue
		}
		parts = append(parts, fmt.Sprintf("pid %d %s (起動から%s)", p.PID, fmtMB(p.RSSKB), fmtDur(p.Elapsed)))
		if p.RSSKB >= tmuxServerWarnKB {
			big = append(big, strconv.Itoa(p.PID))
		}
	}
	if len(parts) == 0 {
		c.Value, c.Status = "動いていない", StatusInfo
		return c
	}
	c.Value, c.Status = fmt.Sprintf("%d 個: %s", len(parts), strings.Join(parts, " / ")), StatusOK
	if len(parts) > 1 {
		c.Status = StatusInfo
		c.Note = "サーバが複数ある。テストや -L で立てた隔離サーバの残りかもしれない (本番のサーバかを socket で確かめてから扱う)"
	}
	if len(big) > 0 {
		c.Status = StatusWarn
		c.Note = "サーバの常駐量が大きい。スクロールバックは history-limit を下げても既存の pane には効かない"
		c.Fix = "全 pane のスクロールバックを解放する (dotfiles の _tmux.conf では prefix M-c。確認が出る) か、要らない pane だけ clear-history -t <pane> で捨てる"
	}
	return c
}

func tmuxPanes(out string) Check {
	c := Check{ID: "tmux.panes", Label: "tmux の pane"}
	panes, lines, dead := 0, 0, 0
	limit := ""
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			continue
		}
		panes++
		n, _ := strconv.Atoi(f[0])
		lines += n
		limit = f[1]
		if f[2] == "1" {
			dead++
		}
	}
	c.Value = fmt.Sprintf("%d 個 / スクロールバック計 %d 行 (history-limit %s)", panes, lines, cmp.Or(limit, "-"))
	c.Status = StatusOK
	if dead > 0 {
		c.Status = StatusWarn
		c.Value += fmt.Sprintf(" / 終了したまま残った pane %d 個", dead)
		c.Fix = "終了した pane を閉じる (pane を名指しする。サーバ・セッションごと止めない)"
	}
	return c
}

func (s *scanner) nvim(ctx context.Context) []Check {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return []Check{unknown("nvim.instances", "nvim", err.Error())}
	}
	return []Check{nvimInstances(snap.Second), orphanLSP(snap.Second), s.nvimSwap(ctx)}
}

// nvimInstances は端末を持つ nvim (画面) と、その子 (--embed の本体・言語サーバ) の常駐量。
// 0.10 以降の nvim は画面のプロセスと --embed の本体の 2 つに分かれ、本体は端末を持たない。
// 残骸に数えるのは、端末が無く launchd の子になったもの (親が死んだ) だけ。端末が無くても親が生きているもの
// (Neovide や VS Code の拡張が起こす --embed) は使用中のエディタなので数えない。--headless で起こした道具も同じ形になるので、止める前に args を見させる。
func nvimInstances(ps []proc) Check {
	c := Check{ID: "nvim.instances", Label: "nvim の起動数と常駐量"}
	ui, embed := 0, 0
	var orphans []string
	family := map[int]bool{}
	for _, p := range ps {
		if p.Name() != "nvim" {
			continue
		}
		family[p.PID] = true
		if p.TTY != "??" {
			ui++
			continue
		}
		embed++
		if p.PPID == 1 {
			orphans = append(orphans, strconv.Itoa(p.PID))
		}
	}
	if ui+embed == 0 {
		c.Value, c.Status = "動いていない", StatusInfo
		return c
	}
	var rss int64
	children := map[string]int{}
	for _, p := range ps {
		if family[p.PID] {
			rss += p.RSSKB
		} else if family[p.PPID] {
			rss += p.RSSKB
			children[p.Name()]++
		}
	}
	c.Value = fmt.Sprintf("画面 %d 個 / 合計 %s", ui, fmtMB(rss))
	if len(children) > 0 {
		c.Value += " / 子: " + joinCounts(children)
	}
	c.Status = StatusOK
	if len(orphans) > 0 {
		c.Status = StatusWarn
		c.Note = "端末が無く親も死んだ nvim (閉じた端末の本体・テストや道具が起こした headless の残り)。ps -ww -o args= -p <pid> で何者かを見てから止める"
		c.Fix = "kill " + strings.Join(orphans, " ") + " (pid を名指し。🚨 未保存の編集は失われうる: swapfile を無効にした設定では回復できない。止める前に、その nvim に未保存のバッファが無いかを人に確かめる)"
	}
	return c
}

// orphanLSP は親を失った言語サーバ (launchd の子になって残ったもの)。
func orphanLSP(ps []proc) Check {
	c := Check{ID: "nvim.lsp", Label: "親を失った言語サーバ"}
	var hits []string
	var pids []string
	for _, p := range ps {
		if p.PPID == 1 && slices.ContainsFunc(lspNames, func(n string) bool { return sameName(p.Name(), n) }) {
			hits = append(hits, fmt.Sprintf("%s (pid %d, %s, 起動から%s)", p.Name(), p.PID, fmtMB(p.RSSKB), fmtDur(p.Elapsed)))
			pids = append(pids, strconv.Itoa(p.PID))
		}
	}
	const blind = "node・ruby で動く言語サーバ (typescript・pyright・ruby-lsp など) は名前で見分けられないので数えていない"
	if len(hits) == 0 {
		c.Value, c.Status, c.Note = "なし", StatusOK, blind
		return c
	}
	c.Value, c.Status = strings.Join(hits, " / "), StatusWarn
	c.Note = "エディタが落ちた・強制終了したときに残る。メモリを持ち続け、ファイルの監視も続ける。" + blind
	c.Fix = "kill " + strings.Join(pids, " ")
	return c
}

// nvimSwap は古い swap ファイル (落ちたセッションの残り。開くたびに E325 の確認が出る)。
// 古さは mtime で見るが、動いている nvim が開いている swap は数えない: 開いたまま何日も編集していないバッファの swap も古く見え、
// それを動かすとそのバッファは落ちたときに回復できなくなる。開いているかを確かめられなければ、移す案内を出さない。
func (s *scanner) nvimSwap(ctx context.Context) Check {
	const id, label = "nvim.swap", "nvim の古い swap ファイル"
	dir := filepath.Join(s.o.Home, ".local/state/nvim/swap")
	shown := tildePath(s.o.Home, dir)
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return Check{ID: id, Label: label, Value: "置き場が無い", Status: StatusInfo}
	}
	if err != nil {
		return unknown(id, label, shown+" を読めない")
	}
	open, openErr := s.openFilesIn(ctx, dir)
	since := s.o.Now().Add(-nvimSwapOld)
	old, inUse := 0, 0
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() || !fi.ModTime().Before(since) {
			continue
		}
		if open[e.Name()] {
			inUse++
			continue
		}
		old++
	}
	c := Check{ID: id, Label: label, Value: fmt.Sprintf("全 %d 個のうち、7 日より古く開かれていないもの %d 個", len(ents), old), Status: StatusInfo}
	switch {
	case openErr != nil:
		c.Value = fmt.Sprintf("全 %d 個 (開いているものを確かめられない)", len(ents))
		c.Note = "lsof で開いている swap を確かめられなかったので、移す案内は出さない: " + openErr.Error()
	case old > 0:
		c.Note = fmt.Sprintf("落ちたセッションの残り。そのファイルを開くたびに回復の確認を出す (動いている nvim が開いている古い swap %d 個は数えていない)", inUse)
		c.Fix = "中身が要らなければ " + shown + " の、開かれていない古いものをゴミ箱へ移す (移す直前に lsof でもう一度確かめる)"
	}
	return c
}

// openFilesIn は dir の直下で、どれかのプロセスが開いているファイルの名前。自分のユーザーのプロセスしか見えないが、
// nvim の swap は自分の nvim しか開かないので足りる。
// 出力が 0 行なら、開いているものが無いのか lsof に見えていないのかを区別できないので失敗として返す (swap の判定は、誤ると
// 人の編集を失う側に倒れる)。nvim が 1 つも swap を開いていないときも案内は出なくなるが、それは安全側。
func (s *scanner) openFilesIn(ctx context.Context, dir string) (map[string]bool, error) {
	out, stderr, rc, err := s.cmd(ctx, 20*time.Second, "/usr/sbin/lsof", "-Fn", "+d", dir)
	if lsofFailed(rc, err, stderr) {
		return nil, errors.New(cmdNote("lsof", rc, err, stderr))
	}
	if !strings.Contains(out, "\nn") && !strings.HasPrefix(out, "n") {
		return nil, errors.New("lsof が 1 件も返さない (開いているものが無いのか、見えていないのかを区別できない)")
	}
	m := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		// +d で範囲は dir の直下に絞られている。lsof は symlink を解いた実体のパスを返すので、dir と文字列で比べず名前だけで照合する
		if name, ok := strings.CutPrefix(line, "n"); ok {
			m[filepath.Base(name)] = true
		}
	}
	return m, nil
}

// tildePath は home の下のパスを ~ で始まる形にする (レポートにユーザー名を出さない)。
func tildePath(home, p string) string {
	if home != "" {
		if rest, ok := strings.CutPrefix(p, home+"/"); ok {
			return "~/" + rest
		}
	}
	return p
}

func (s *scanner) chrome(ctx context.Context) []Check {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return []Check{unknown("chrome.version", "Chrome", err.Error())}
	}
	installed, note := "", ""
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/bin/defaults", "read", chromeApp+"/Contents/Info", "CFBundleShortVersionString")
	if err != nil || rc != 0 {
		note = cmdNote("defaults", rc, err, stderr)
	} else {
		installed = strings.TrimSpace(out)
	}
	return chromeChecks(snap.Second, installed, note)
}

// chromeChecks は動いている版と入っている版の食い違いと、プロセスの数・常駐量。
// 動いている版は Helper の実行ファイルのパス (…/Versions/<版>/Helpers/…) から読む (2026-11-06 に実測)。
func chromeChecks(ps []proc, installed, installedNote string) []Check {
	var main *proc
	running := map[string]bool{}
	renderers, total := 0, 0
	var rss int64
	for i, p := range ps {
		if !strings.HasPrefix(p.Exe, chromeApp+"/") {
			continue
		}
		total++
		rss += p.RSSKB
		if p.Exe == chromeApp+"/Contents/MacOS/Google Chrome" {
			main = &ps[i]
		}
		if strings.Contains(path.Base(p.Exe), "(Renderer)") { // ucomm は 16 文字で切れて種類が読めないので Exe で見る (表示はしない)
			renderers++
		}
		if _, rest, ok := strings.Cut(p.Exe, "/Versions/"); ok {
			// argv[0] は偽れるので、版の形 (数字 4 組) のものだけを数える。ディレクトリの実在では見ない: .app ごと差し替える更新では
			// 動いている古い版のディレクトリが消えており、「再起動が要る」を判定不能に化けさせる
			if v, _, ok := strings.Cut(rest, "/"); ok && chromeVersion.MatchString(v) {
				running[v] = true
			}
		}
	}
	ver := Check{ID: "chrome.version", Label: "Chrome の版 (動いている / 入っている)"}
	switch {
	case main == nil:
		ver.Value, ver.Status = "動いていない (入っている版 "+cmp.Or(installed, "-")+")", StatusInfo
	case installed == "":
		ver = unknown(ver.ID, ver.Label, "入っている版が読めない: "+installedNote)
	case len(running) == 0:
		ver = unknown(ver.ID, ver.Label, "動いている Helper のパスから版が読めない")
	default:
		vs := make([]string, 0, len(running))
		for v := range running {
			vs = append(vs, v)
		}
		slices.Sort(vs)
		ver.Value = strings.Join(vs, ", ") + " / " + installed
		ver.Status = StatusOK
		if len(vs) != 1 || vs[0] != installed {
			ver.Status = StatusWarn
			ver.Note = "更新は入っているが、動いているのは古い版 (再起動するまで更新が効かない。新しいタブが開けなくなることもある)"
			ver.Fix = "Chrome を再起動する (アドレスバーに chrome://restart。タブは戻る)"
		}
	}
	cs := []Check{ver}
	if main != nil {
		cs = append(cs, Check{ID: "chrome.procs", Label: "Chrome のプロセス", Status: StatusInfo,
			Value: fmt.Sprintf("%d 個 (タブなどの描画 %d) / 合計 %s / 起動から%s", total, renderers, fmtMB(rss), fmtDur(main.Elapsed)),
			Note:  "重いタブは Chrome のタスクマネージャ (Shift+Esc 相当: メニュー > その他のツール) で探す"})
	}
	return cs
}

func (s *scanner) apps(ctx context.Context) []Check {
	cs := []Check{}
	if snap, err := s.snapshot(ctx); err != nil {
		cs = append(cs, unknown("apps.top", "アプリごとの常駐量", err.Error()))
	} else {
		cs = append(cs, appsTop(snap.Second))
	}
	return append(cs, s.overwrittenApps())
}

// appsTop は .app ごと (無ければ実行ファイル名ごと) に常駐量を合計した上位。
func appsTop(ps []proc) Check {
	sum := map[string]int64{}
	for _, p := range ps {
		sum[appKey(p)] += p.RSSKB
	}
	keys := make([]string, 0, len(sum))
	for k := range sum {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return int(sum[b] - sum[a]) })
	parts := make([]string, 0, 8)
	for _, k := range keys[:min(8, len(keys))] {
		parts = append(parts, fmt.Sprintf("%s %s", k, fmtMB(sum[k])))
	}
	return Check{ID: "apps.top", Label: "アプリごとの常駐量 (上位 8)", Value: strings.Join(parts, " / "), Status: StatusInfo}
}

// appKey は、いちばん外側の .app の名前 (Helper も本体に数える)。.app の中に無いものは ucomm。
// Exe (argv[0]) から取るのは /Applications と /System の下の .app だけ: 置き場が決まっているので、argv[0] を書き換えた偽物の文字列が
// レポートに出る経路を絞れる (それ以外の .app は ucomm に落とす)。
func appKey(p proc) string {
	exe := filepath.Clean(p.Exe) // /Applications/../ で接頭辞の検査を抜けさせない
	if strings.HasPrefix(exe, "/Applications/") || strings.HasPrefix(exe, "/System/Applications/") {
		parts := strings.Split(exe, "/")
		for i, part := range parts {
			name, ok := strings.CutSuffix(part, ".app")
			if !ok || name == "" {
				continue
			}
			// argv[0] は接頭辞ごと偽れるので、その .app が実在するときだけ名前に使う
			if appExists(strings.Join(parts[:i+1], "/")) {
				return name
			}
			break
		}
	}
	return p.Name()
}

var chromeVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){3}$`)

// appExists は .app (や版のディレクトリ) が実在するか。テストは差し替える。
var appExists = func(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// overwrittenApps は ExcUserFault_<アプリ>-*.ips (動いているアプリを更新が上書きした記録) の直近 7 日分。
func (s *scanner) overwrittenApps() Check {
	const id, label = "apps.overwritten", "動作中に更新で上書きされたアプリ (直近 7 日)"
	since := s.o.Now().Add(-recentDiag)
	counts := map[string]int{}
	read := 0
	for _, dir := range s.o.DiagDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		read++
		for _, e := range ents {
			rest, ok := strings.CutPrefix(e.Name(), "ExcUserFault_")
			if !ok || !strings.HasSuffix(rest, ".ips") {
				continue
			}
			if fi, err := e.Info(); err == nil && !fi.ModTime().Before(since) {
				counts[diagProcName(rest)]++
			}
		}
	}
	if read == 0 {
		return unknown(id, label, "DiagnosticReports を読めない")
	}
	if len(counts) == 0 {
		return Check{ID: id, Label: label, Value: "なし", Status: StatusOK}
	}
	return Check{ID: id, Label: label, Value: joinCounts(counts), Status: StatusWarn,
		Note: "Homebrew の cask などが動作中のアプリを入れ替えた。古い部品が消えて、新しい窓・タブが開けなくなることがある",
		Fix:  "そのアプリを再起動する"}
}
