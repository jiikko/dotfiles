// tfperf は実物の bin/treefiler を隔離した tmux の中で起こし、打鍵を 1 つずつ送って、打鍵から端末への書き出しまでの所要を測る。
// treefiler は TREEFILER_PERFLOG のログ (perflog) を書き、tfperf は送った時刻と組にして打鍵ごとに集計する。
// tfprobe (Model をヘッドレスで回す) と違い、bubbletea の描画の周期と tmux の画面への書き込みまでを含む (外の端末の描画は含まない)。
//
//	tfperf run -bin <treefiler> -root <dir> -out <dir> [-keys 'j*30 k*30'] [-interval 200ms]
//	tfperf report <out>
//
// run は report まで行う。out には perf.log (treefiler のログ)・sends.tsv (送った時刻)・keys.tsv (打鍵ごと)・summary.txt を置く。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"subproc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tfperf run -bin <treefiler> -root <dir> -out <dir> [-keys 'j*30 k*30'] | tfperf report <out>")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "report":
		if len(os.Args) != 3 {
			err = errors.New("usage: tfperf report <out>")
			break
		}
		err = report(os.Args[2], os.Stdout)
	default:
		err = fmt.Errorf("知らないサブコマンド: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tfperf:", err)
		os.Exit(1)
	}
}

type runOpts struct {
	bin, root, out, keys string
	interval, warmup     time.Duration
	width, height        int
}

func run(args []string) error {
	var o runOpts
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.StringVar(&o.bin, "bin", "", "測る treefiler のバイナリ (必須。直す前後で別のバイナリを渡して比べる)")
	fs.StringVar(&o.root, "root", ".", "開くフォルダ")
	fs.StringVar(&o.out, "out", "", "結果を書くフォルダ (必須。無ければ作る)")
	fs.StringVar(&o.keys, "keys", "j*30 k*30", "送るキー (tmux の send-keys の名前。`名前*回数` で繰り返す)")
	fs.DurationVar(&o.interval, "interval", 250*time.Millisecond, "打鍵の間隔 (前の打鍵の動きが終わる前に次を送ると、押しっぱなしに近い形になる)")
	fs.DurationVar(&o.warmup, "warmup", 1500*time.Millisecond, "起動して最初の描画が出てから最初の打鍵までの間 (開いたときの動きと裏の走査を待つ)")
	fs.IntVar(&o.width, "width", 200, "画面の幅")
	fs.IntVar(&o.height, "height", 50, "画面の高さ")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.bin == "" || o.out == "" {
		return errors.New("-bin と -out は必須")
	}
	// 絶対パスにする: treefiler は root を cwd にして起き、TREEFILER_PERFLOG を相対で渡すと root の下に書く
	out, err := filepath.Abs(o.out)
	if err != nil {
		return err
	}
	o.out = out
	keys, err := parseKeys(o.keys)
	if err != nil {
		return err
	}
	bin, err := filepath.Abs(o.bin)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		return err
	}
	cfg := filepath.Join(o.out, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		return err
	}
	if err := copyUserSettings(cfg); err != nil {
		return err
	}
	logPath := filepath.Join(o.out, "perf.log")
	if _, err := os.Stat(logPath); err == nil {
		return fmt.Errorf("%s が既にある (run ごとに新しい -out を渡す)", logPath)
	}

	t := newTmuxServer(o.out)
	defer t.stop()
	// Ctrl-C でも自分の tmux サーバ (と中の treefiler) を止めて socket を消す (tmux は daemon なので、go run と一緒には死なない)
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
	go func() {
		<-sig
		go func() { <-sig; os.Exit(130) }() // 後片付けの tmux が応答しないとき、もう 1 度で抜ける
		t.stop()
		os.Exit(130)
	}()
	if err := t.start(o, bin, root, cfg, logPath); err != nil {
		return err
	}
	if err := waitFor(15*time.Second, func() bool { return logHas(logPath, "\tview\t") }); err != nil {
		return fmt.Errorf("treefiler の最初の描画がログに出ない: %w (画面: %s)", err, t.capture())
	}
	time.Sleep(o.warmup) // sleep-ok: 入力の間隔 (起動直後の動きと走査が落ち着いてから打ち始める)

	sends, err := os.Create(filepath.Join(o.out, "sends.tsv"))
	if err != nil {
		return err
	}
	for _, k := range keys {
		before := time.Now().UnixNano()
		if err := t.cmd("send-keys", "-t", t.session, k); err != nil {
			sends.Close()
			return err
		}
		// 送り終えた時刻も残す: send-keys は tmux のクライアントを 1 本起こすので、その起動の分を treefiler の所要から外して読む
		fmt.Fprintf(sends, "%d\t%d\t%s\n", before, time.Now().UnixNano(), k)
		time.Sleep(o.interval) // sleep-ok: 打鍵の間隔そのもの (人の打鍵の速さを模す入力)
	}
	if err := sends.Close(); err != nil {
		return err
	}
	if err := t.cmd("send-keys", "-t", t.session, "q"); err != nil {
		return err
	}
	if err := waitFor(10*time.Second, func() bool { return t.cmd("has-session", "-t", t.session) != nil }); err != nil {
		return fmt.Errorf("q で閉じない: %w", err)
	}
	f, err := os.Create(filepath.Join(o.out, "summary.txt"))
	if err != nil {
		return err
	}
	defer f.Close()
	return report(o.out, io.MultiWriter(os.Stdout, f))
}

// parseKeys は `j*30 k*30 l` を 1 打ずつに展開する。
func parseKeys(s string) ([]string, error) {
	var keys []string
	for _, tok := range strings.Fields(s) {
		name, n := tok, 1
		if i := strings.LastIndexByte(tok, '*'); i > 0 {
			c, err := strconv.Atoi(tok[i+1:])
			if err != nil || c < 1 {
				return nil, fmt.Errorf("-keys の %q: 回数が読めない", tok)
			}
			name, n = tok[:i], c
		}
		for range n {
			keys = append(keys, name)
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("-keys が空")
	}
	return keys, nil
}

// copyUserSettings はユーザーの treefiler.toml (配色などの見た目) だけを写す。places と history は写さない:
// 前回の場所から始めると run ごとに開く木が変わり、閉じたときに本物の places を書き換えるため。
func copyUserSettings(dst string) error {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		base = filepath.Join(home, ".config")
	}
	b, err := os.ReadFile(filepath.Join(base, "treefiler", "treefiler.toml"))
	if errors.Is(err, os.ErrNotExist) {
		// まだ前の置き場 (~/.config/glogx) にある (treefiler は初めて置き場を引いたときに移す。ここでは移さず読むだけ)
		b, err = os.ReadFile(filepath.Join(base, "glogx", "treefiler.toml"))
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dst, "treefiler.toml"), string(b))
}

// tmuxServer は run ごとの tmux サーバ。socket は out の中のパスを -S で名指しする: 呼び出し元が tmux の中でも本番のサーバには繋がず
// ($TMUX は -L / -S が無いと TMUX_TMPDIR より優先される)、起動に失敗しても消すパスが決まっている (kill-server は socket を消さない)。
// TMUX / TMUX_PANE も env から落とす (中で起こすものに本番を見せない)。
type tmuxServer struct {
	bin, socket, session string
	env                  []string
	once                 sync.Once
}

func newTmuxServer(out string) *tmuxServer {
	t := &tmuxServer{bin: "tmux", socket: filepath.Join(out, "tmux.sock"), session: "tfperf"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			t.env = append(t.env, kv)
		}
	}
	return t
}

// start は treefiler を detached な session で起こす。🚨 client を付けないので、測る書き込みは tmux の画面の格子までで、
// 外の端末の描画は含まない。default-terminal だけは本番 (_tmux.conf) に揃える (treefiler の色と同期出力の判定が TERM を見る)。
func (t *tmuxServer) start(o runOpts, bin, root, cfg, logPath string) error {
	conf := filepath.Join(o.out, "tmux.conf")
	if err := writeFile(conf, "set -g default-terminal \"tmux-256color\"\n"); err != nil {
		return err
	}
	return t.cmd("-f", conf, "new-session", "-d", "-s", t.session,
		"-x", strconv.Itoa(o.width), "-y", strconv.Itoa(o.height), "-c", root,
		"-e", "TREEFILER_PERFLOG="+logPath, "-e", "TREEFILER_CONFIG_DIR="+cfg,
		bin, root)
}

func writeFile(path, body string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// tmux は自分の -S のサーバへ args を送り、stdout を返す。combined なら stderr も混ぜる (失敗の理由を出すため)。
func (t *tmuxServer) tmux(combined bool, args ...string) (string, error) {
	c := subproc.CommandContext(context.Background(), t.bin, append([]string{"-S", t.socket}, args...)...)
	c.Env = t.env
	var out []byte
	var err error
	if combined {
		out, err = c.CombinedOutput()
	} else {
		out, err = c.Output()
	}
	return string(out), err
}

func (t *tmuxServer) cmd(args ...string) error {
	if out, err := t.tmux(true, args...); err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func (t *tmuxServer) output(args ...string) (string, error) { return t.tmux(false, args...) }

func (t *tmuxServer) capture() string {
	out, _ := t.output("capture-pane", "-p", "-t", t.session)
	return strings.TrimSpace(out)
}

// stop は自分の -S のサーバだけを止め、その socket だけを消す (Ctrl-C の goroutine と defer の両方から呼ばれる)。
func (t *tmuxServer) stop() {
	t.once.Do(func() {
		if t.cmd("has-session") == nil {
			_ = t.cmd("kill-server")
		}
		_ = os.Remove(t.socket) //nolint:forbidigo // 自分が起こした tmux の socket (ユーザーのファイルではない)
	})
}

func waitFor(limit time.Duration, cond func() bool) error {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); {
		if cond() {
			return nil
		}
		time.Sleep(20 * time.Millisecond) // sleep-ok: ポーリングの刻み (待ちの実体は cond)
	}
	return fmt.Errorf("%v 待っても成立しない", limit)
}

func logHas(path, s string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), s)
}

// event は perf.log の 1 行 (perflog の行の形)。
type event struct {
	t         int64
	kind      string
	ns, bytes int64
	msg, key  string
	anim      bool
}

func readEvents(path string) ([]event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var evs []event
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		c := strings.Split(sc.Text(), "\t")
		bad := fmt.Errorf("%s:%d: 読めない行 %q", path, line, sc.Text())
		if len(c) < 3 {
			return nil, bad
		}
		e := event{kind: c[1]}
		var err1, err2 error
		e.t, err1 = strconv.ParseInt(c[0], 10, 64)
		e.ns, err2 = strconv.ParseInt(c[2], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, bad
		}
		switch e.kind {
		case "update":
			if len(c) != 6 {
				return nil, bad
			}
			e.msg, e.key, e.anim = c[3], c[4], c[5] == "1"
		case "view", "write":
			if len(c) != 4 {
				return nil, bad
			}
			if e.bytes, err1 = strconv.ParseInt(c[3], 10, 64); err1 != nil {
				return nil, bad
			}
		default:
			return nil, bad
		}
		evs = append(evs, e)
	}
	return evs, sc.Err()
}

// send は送った打鍵。t は send-keys を起こす前、done は send-keys が戻った後 (tmux が pty へ書き終えた後)。
type send struct {
	t, done int64
	key     string
}

func readSends(path string) ([]send, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []send
	for i, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		c := strings.Split(l, "\t")
		if len(c) != 3 {
			return nil, fmt.Errorf("%s:%d: 読めない行 %q", path, i+1, l)
		}
		t, err1 := strconv.ParseInt(c[0], 10, 64)
		done, err2 := strconv.ParseInt(c[1], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%s:%d: 読めない行 %q", path, i+1, l)
		}
		out = append(out, send{t, done, c[2]})
	}
	return out, nil
}

// keyStat は 1 打の集計。時間は ms。窓は送った時刻から次の打鍵を送った時刻 (最後の打鍵は記録の終わり) まで。
type keyStat struct {
	key string
	// deliver は送り始めてから key の Update が始まるまで (tmux のクライアント・pty・bubbletea の読み取り。端末側の分)。
	// draw は key の Update が始まってから最初の書き込みが終わるまで (treefiler と bubbletea の描画の周期の分)。
	// send-keys が戻る時刻は使わない: tmux のクライアントは pty へ書いた後も終わるまで数十 ms かかり、Update の方が先に始まる。
	deliver, handle, draw float64
	settle                float64 // key の Update が始まってから動きが止まった Update まで (窓の中で止まらなければ -1)
	frames                int     // 窓の中の書き込みの回数
	// nextFrame は key の Update から次の tick の Update まで (動きが始まった打鍵だけ。16ms 前後なら動きの 2 フレーム目が待たされていない)。
	// 書き込みの間隔では測らない: 1 行の移動はセルの粒度で途中に 1 回しか画面が変わらず、間隔はカクつきでなく粒度を測る。
	nextFrame        float64
	viewMax, viewSum float64 // 窓の中の View の最大と合計
	updSum, writeSum float64 // 窓の中の Update (キーと tick) と書き込みの合計
	bytes            int64   // 窓の中で端末へ書いた量
}

func report(dir string, w io.Writer) error {
	evs, err := readEvents(filepath.Join(dir, "perf.log"))
	if err != nil {
		return err
	}
	sends, err := readSends(filepath.Join(dir, "sends.tsv"))
	if err != nil {
		return err
	}
	stats, err := analyze(evs, sends)
	if err != nil {
		return err
	}
	if err := writeKeys(filepath.Join(dir, "keys.tsv"), stats); err != nil {
		return err
	}
	return summarize(w, stats)
}

// analyze は i 番目に送った打鍵を i 番目の key の Update と組にする。数か名前が合わなければ判定不能 (取りこぼした・
// 別のキーが混ざった。順番の組み合わせが崩れているので、どの打鍵の数字も信用できない)。
func analyze(evs []event, sends []send) ([]keyStat, error) {
	var keyIdx []int
	for i, e := range evs {
		if e.kind == "update" && e.msg == "key" {
			keyIdx = append(keyIdx, i)
		}
	}
	// 最後の q (閉じる) は送った記録に無い
	if n := len(keyIdx); n == len(sends)+1 && evs[keyIdx[n-1]].key == "q" {
		keyIdx = keyIdx[:n-1]
	}
	if len(keyIdx) != len(sends) {
		return nil, fmt.Errorf("送った打鍵 %d 件と、ログの key の Update %d 件が合わない (判定不能)", len(sends), len(keyIdx))
	}
	for i, s := range sends {
		if got, want := evs[keyIdx[i]].key, teaKeyName(s.key); got != want {
			return nil, fmt.Errorf("%d 打目: 送ったのは %s (%s) だが、ログの key は %s (判定不能)", i, s.key, want, got)
		}
	}
	const ms = 1e6
	stats := make([]keyStat, 0, len(sends))
	for i, s := range sends {
		k := evs[keyIdx[i]]
		end := int64(1<<63 - 1)
		if i+1 < len(sends) {
			end = sends[i+1].t
		}
		start := k.t - k.ns // key の Update が始まった時刻
		// 前のフレームとの境目: キーの後の最初の View が終わった時刻。bubbletea は Update の後に View を呼び、書き込みは描画の
		// goroutine が自分の周期で行うので、それより前に書き始めた書き込みは前のフレームを書いている
		drawn := int64(1<<63 - 1)
		for _, e := range evs[keyIdx[i]:] {
			if e.kind == "view" {
				drawn = e.t
				break
			}
		}
		st := keyStat{key: k.key, deliver: float64(start-s.t) / ms, handle: float64(k.ns) / ms, draw: -1, settle: -1, nextFrame: -1}
		if !k.anim {
			st.settle = float64(k.t-start) / ms
		}
		for _, e := range evs[keyIdx[i]:] {
			if e.t >= end {
				break
			}
			switch e.kind {
			case "update":
				st.updSum += float64(e.ns) / ms
				if st.nextFrame < 0 && k.anim && e.msg == "tick" {
					st.nextFrame = float64(e.t-e.ns-k.t) / ms
				}
				if st.settle < 0 && !e.anim {
					st.settle = float64(e.t-start) / ms
				}
			case "view":
				st.viewSum += float64(e.ns) / ms
				st.viewMax = max(st.viewMax, float64(e.ns)/ms)
			case "write":
				if e.t-e.ns < drawn {
					continue // 前のフレームの書き込み
				}
				st.frames++
				st.writeSum += float64(e.ns) / ms
				st.bytes += e.bytes
				if st.draw < 0 {
					st.draw = float64(e.t-start) / ms
				}
			}
		}
		stats = append(stats, st)
	}
	return stats, nil
}

// teaKeyName は tmux の send-keys の名前を bubbletea の KeyPressMsg.String() の名前へ写す (1 文字はそのまま)。
// 写せない名前は判定不能で止まる側に倒れる (analyze が名前の食い違いとして落とす)。
func teaKeyName(tmux string) string {
	// 端末では区別できない ctrl の組み合わせは、bubbletea が別の名前で受ける
	if n, ok := map[string]string{"c-[": "esc", "c-m": "enter", "c-i": "tab"}[strings.ToLower(tmux)]; ok {
		return n
	}
	if len(tmux) > 2 && tmux[1] == '-' {
		switch strings.ToLower(tmux[:2]) { // tmux は修飾の大文字小文字を区別しない
		case "c-":
			return "ctrl+" + teaKeyName(tmux[2:])
		case "m-":
			return "alt+" + teaKeyName(tmux[2:])
		case "s-":
			return "shift+" + teaKeyName(tmux[2:])
		}
	}
	if n, ok := map[string]string{
		"bspace": "backspace", "escape": "esc", "ppage": "pgup", "npage": "pgdown",
		"dc": "delete", "ic": "insert", "btab": "shift+tab",
	}[strings.ToLower(tmux)]; ok {
		return n
	}
	if len([]rune(tmux)) == 1 {
		return tmux
	}
	return strings.ToLower(tmux) // Up / Down / Left / Right / Enter / Tab / Space / Home / End / F1
}

func writeKeys(path string, stats []keyStat) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(f, "i\tkey\tdeliver_ms\thandle_ms\tdraw_ms\tsettle_ms\tframes\tnext_frame_ms\tview_max_ms\tview_sum_ms\tupdate_sum_ms\twrite_sum_ms\tbytes")
	for i, s := range stats {
		fmt.Fprintf(f, "%d\t%s\t%.2f\t%.2f\t%.2f\t%.1f\t%d\t%.1f\t%.2f\t%.1f\t%.1f\t%.1f\t%d\n",
			i, s.key, s.deliver, s.handle, s.draw, s.settle, s.frames, s.nextFrame, s.viewMax, s.viewSum, s.updSum, s.writeSum, s.bytes)
	}
	return f.Close()
}

func summarize(w io.Writer, stats []keyStat) error {
	metrics := []struct {
		name string
		get  func(keyStat) float64
	}{
		{"deliver_ms (送り始めてからキーが届くまで。tmux と pty)", func(s keyStat) float64 { return s.deliver }},
		{"handle_ms (キーの Update)", func(s keyStat) float64 { return s.handle }},
		{"draw_ms (キーが届いてから最初の書き込みまで)", func(s keyStat) float64 { return s.draw }},
		{"settle_ms (キーが届いてから動きが止まるまで)", func(s keyStat) float64 { return s.settle }},
		{"next_frame_ms (キーの Update から次のフレームの tick まで)", func(s keyStat) float64 { return s.nextFrame }},
		{"view_max_ms (1 回の View の最大)", func(s keyStat) float64 { return s.viewMax }},
		{"cpu_ms (窓の中の Update+View+write の合計)", func(s keyStat) float64 { return s.updSum + s.viewSum + s.writeSum }},
		{"frames (窓の中の書き込み回数)", func(s keyStat) float64 { return float64(s.frames) }},
		{"kbytes (窓の中で端末へ書いた量)", func(s keyStat) float64 { return float64(s.bytes) / 1024 }},
	}
	fmt.Fprintf(w, "打鍵 %d 件 (最初の 1 打は起動の名残を含むので集計から外す)\n", len(stats))
	body := stats
	if len(body) > 1 {
		body = body[1:]
	}
	fmt.Fprintf(w, "%-50s %9s %9s %9s\n", "", "p50", "p95", "max")
	for _, m := range metrics {
		vals := make([]float64, 0, len(body))
		for _, s := range body {
			if v := m.get(s); v >= 0 {
				vals = append(vals, v)
			}
		}
		if len(vals) < len(body) {
			fmt.Fprintf(w, "  🚨 %s: %d 打で値が取れなかった (書き込みが無い / 窓の中で止まらない)\n", m.name, len(body)-len(vals))
		}
		if len(vals) == 0 {
			continue
		}
		slices.Sort(vals)
		fmt.Fprintf(w, "%-50s %9.2f %9.2f %9.2f\n", m.name, pct(vals, 0.5), pct(vals, 0.95), vals[len(vals)-1])
	}
	worst := 1
	for i := 1; i < len(stats); i++ {
		if stats[i].draw > stats[worst].draw {
			worst = i
		}
	}
	if len(stats) > 1 {
		fmt.Fprintf(w, "最も遅く描かれた打鍵: #%d %s (draw %.2fms・handle %.2fms・view_max %.2fms)。全件は keys.tsv\n",
			worst, stats[worst].key, stats[worst].draw, stats[worst].handle, stats[worst].viewMax)
	}
	return nil
}

func pct(sorted []float64, p float64) float64 {
	return sorted[min(len(sorted)-1, int(p*float64(len(sorted))))]
}
