package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var videoSize = [2]int{1280, 720} // mp4 は 720p 固定 (字幕・アバターの大きさはこの解像度で合わせている)

// sheetStates はまとめ撮り 1 回の枚数。縦 720 x 20 = 14400px (Chrome が 1 枚で撮れる高さに収める)
const sheetStates = 20

// mp4 の撮影前に、CPU が混んでいたら空くまで待つ。負荷が高いときに headless Chrome が watchdog で落ちた
// (rc=2 "own watchdog expired"。同じ条件の再実行で通った) ので、負荷との関係を仮説として入れている。
// 1 分平均の load がコア数 × loadBusyRatio 以上を「混んでいる」とし、loadWaitMax 待っても空かなければそのまま撮る
const (
	loadBusyRatio = 0.8
	loadWaitMax   = 20 * time.Minute
	loadPoll      = 15 * time.Second
)

func encodeAudio(wavPath, outPath string, kbps int) error {
	var name string
	var args []string
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		name = "ffmpeg"
		args = []string{"-v", "error", "-y", "-i", wavPath, "-ac", "1", "-c:a", "aac", "-b:a", fmt.Sprintf("%dk", kbps), "-movflags", "+faststart", outPath}
	} else if _, err := exec.LookPath("afconvert"); err == nil {
		name = "afconvert"
		args = []string{"-f", "m4af", "-d", "aac", "-b", strconv.Itoa(kbps * 1000), wavPath, outPath}
	} else {
		return fail("ffmpeg も afconvert も無い (brew install ffmpeg)")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(appCtx, name, args...)
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if e := interruptedErr(); e != nil {
		return e
	}
	if st, serr := os.Stat(outPath); err != nil || serr != nil || st.Size() == 0 {
		return fail("音声の圧縮に失敗 (%s, %v): %s", name, err, firstRunes(strings.TrimSpace(stderr.String()), 300))
	}
	return nil
}

func findChrome() string {
	cands := []string{os.Getenv("CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium"}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			cands = append(cands, p)
		}
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return c
		}
	}
	return ""
}

var (
	libx264Re      = regexp.MustCompile(`(?m)^\s*V\S*\s+libx264\s`)
	videotoolboxRe = regexp.MustCompile(`(?m)^\s*V\S*\s+h264_videotoolbox\s`)
)

func h264Encoder() []string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil
	}
	rc, out := runQuiet(60*time.Second, "ffmpeg", "-hide_banner", "-encoders")
	switch {
	case rc == 0 && libx264Re.MatchString(out):
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", "20", "-tune", "stillimage"}
	case rc == 0 && videotoolboxRe.MatchString(out):
		return []string{"-c:v", "h264_videotoolbox", "-b:v", "3M"}
	}
	return nil
}

// ffq は ffconcat の引用。パスに ' があっても壊れないよう '\” で閉じて開き直す。
func ffq(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

func pngSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 24)
	if n, _ := f.Read(head); n < 24 || string(head[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(head[16:20])), int(binary.BigEndian.Uint32(head[20:24]))
}

// loadAvg1 は 1 分平均の load。Go の標準に os.getloadavg は無いので、macOS は sysctl、Linux は /proc/loadavg を読む。
func loadAvg1() (float64, bool) {
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f, err := strconv.ParseFloat(strings.Fields(string(b))[0], 64); err == nil {
			return f, true
		}
	}
	rc, out := runQuiet(5*time.Second, "sysctl", "-n", "vm.loadavg") // 例: "{ 3.26 3.65 3.34 }"
	if rc == 0 {
		fields := strings.Fields(strings.Trim(out, "{} "))
		if len(fields) > 0 {
			if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// waitForIdleCPU は CPU が混んでいる間、最大 loadWaitMax 待つ。待っている間も 1 分ごとに状況を出す (黙って止まって見えないように)。
func waitForIdleCPU(env *Env) {
	cores := runtime.NumCPU()
	limit := float64(cores) * loadBusyRatio
	start := env.Now()
	var lastNote *time.Duration
	for appCtx.Err() == nil {
		load, ok := env.LoadAvg()
		if !ok {
			fmt.Fprintln(env.Stderr, "build: CPU の負荷を読めないので、空くのを待たずに撮る")
			return
		}
		waited := env.Now().Sub(start)
		if load < limit {
			if lastNote != nil {
				fmt.Fprintf(env.Stderr, "build: CPU が空いた (load %.1f / %d コア、%.1f 分待った)\n", load, cores, waited.Minutes())
			}
			return
		}
		if waited >= loadWaitMax {
			fmt.Fprintf(env.Stderr, "build: %d 分待っても CPU が空かない (load %.1f / %d コア)。そのまま撮る (Chrome が落ちたら空いてから build し直す)\n",
				int(loadWaitMax.Minutes()), load, cores)
			return
		}
		if lastNote == nil || waited-*lastNote >= time.Minute {
			fmt.Fprintf(env.Stderr, "build: CPU が混んでいる (load %.1f / %d コア、閾値 %.1f)。空くまで最大 %d 分待つ (経過 %.0f 分)\n",
				load, cores, limit, int(loadWaitMax.Minutes()), waited.Minutes())
			w := waited
			lastNote = &w
		}
		env.Sleep(loadPoll)
	}
}

// sheetFragment は、まとめ撮りの状態 (字幕の行, 話し中か, 口の開き) の並びを player.html が読む URL の断片にする。
// 🚨 書式は player.html の `location.hash.match(/^#sheet=…/)` と 1 対 1。片方だけ変えると、プレイヤーは通常の画面のまま撮られる
// (TestSheetFragmentMatchesPlayer が両者を突き合わせる。issue 650)
func sheetFragment(group [][3]int) string {
	parts := make([]string, len(group))
	for i, st := range group {
		parts[i] = fmt.Sprintf("%d,%d,%d", st[0], st[1], st[2])
	}
	return "#sheet=" + strings.Join(parts, ";")
}

// writeMP4 は HTML プレイヤーで「見た目の状態」ごとの絵を Chrome に撮らせ、状態の列どおりに並べて音声と合わせる。
//
// 見た目を HTML 版と同じにするため、絵は自前で描かずプレイヤーに描かせる。撮るのは見た目の状態の
// 種類の数 (行数 × 口の段階 程度) だけで、フレームの数ではない。Chrome の起動 (1 回 1 秒強) が律速なので、
// プレイヤーのまとめ撮りモード (#sheet=) で sheetStates 枚を縦に並べて 1 回で撮り、ffmpeg で切り分ける。
func writeMP4(env *Env, data *PlayerData, m4a, out, td string, jobs int) error {
	chrome := findChrome()
	enc := h264Encoder()
	if e := interruptedErr(); e != nil {
		return e // 中断で ffmpeg の確認が失敗したのを「H.264 を書ける ffmpeg が要る」と言わない
	}
	if chrome == "" {
		return fail("mp4 には Chrome か Chromium が要る (環境変数 CHROME で実行ファイルを指定できる)")
	}
	if enc == nil {
		return fail("mp4 には H.264 を書ける ffmpeg が要る (brew install ffmpeg)")
	}
	html, err := renderHTML(data, env)
	if err != nil {
		return err
	}
	page := filepath.Join(td, "render.html")
	if err := os.WriteFile(page, []byte(html), 0o644); err != nil {
		return fail("%s: 書けない (%v)", page, err)
	}
	waitForIdleCPU(env)
	if e := interruptedErr(); e != nil {
		return e
	}
	states := sortedStates(data.Frames)
	shots := map[[3]int]string{}
	for _, st := range states {
		shots[st] = filepath.Join(td, fmt.Sprintf("state_%d_%d_%d.png", st[0]+1, st[1], st[2]))
	}
	var groups [][][3]int
	for i := 0; i < len(states); i += sheetStates {
		groups = append(groups, states[i:min(i+sheetStates, len(states))])
	}
	w, h := videoSize[0], videoSize[1]
	pageURI := (&url.URL{Scheme: "file", Path: page}).String()

	shoot := func(gi int) string {
		group, sheet := groups[gi], filepath.Join(td, fmt.Sprintf("sheet_%d.png", gi))
		// --user-data-dir は付けない: 付けると撮影後も Chrome の更新プロセスが残って終了しない (実測)。
		// 付けなければ並列に起動しても衝突しない
		rc, msg := runQuiet(120*time.Second, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
			fmt.Sprintf("--window-size=%d,%d", w, h*len(group)), "--virtual-time-budget=3000", "--screenshot="+sheet,
			pageURI+sheetFragment(group))
		if rc != 0 || !isFile(sheet) {
			return fmt.Sprintf("Chrome が失敗 (rc=%d): %s", rc, lastRunes(msg, 300))
		}
		if gw, gh := pngSize(sheet); gw != w || gh != h*len(group) { // 縦に長すぎて切られた・倍率が違う、を切り分け前に止める
			return fmt.Sprintf("撮った絵の大きさが (%d, %d) で、期待した (%d, %d) と違う", gw, gh, w, h*len(group))
		}
		var fc strings.Builder
		fmt.Fprintf(&fc, "[0]split=%d", len(group))
		for k := range group {
			fmt.Fprintf(&fc, "[s%d]", k)
		}
		for k := range group {
			fmt.Fprintf(&fc, ";[s%d]crop=%d:%d:0:%d[o%d]", k, w, h, h*k, k)
		}
		args := []string{"-v", "error", "-y", "-i", sheet, "-filter_complex", fc.String()}
		for k, st := range group {
			args = append(args, "-map", fmt.Sprintf("[o%d]", k), shots[st])
		}
		rc, msg = runQuiet(60*time.Second, "ffmpeg", args...)
		allOK := rc == 0
		for _, st := range group {
			if gw, gh := pngSize(shots[st]); gw != w || gh != h {
				allOK = false
			}
		}
		if !allOK {
			return fmt.Sprintf("切り分けに失敗 (rc=%d): %s", rc, lastRunes(msg, 300))
		}
		return ""
	}

	fmt.Fprintf(env.Stderr, "build: 見た目の状態 %d 種類を Chrome %d 回で描く\n", len(states), len(groups))
	errs := make([]string, len(groups))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for gi := range groups {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[gi] = shoot(gi)
		}()
	}
	wg.Wait()
	if e := interruptedErr(); e != nil {
		return e
	}
	for gi, e := range errs { // 失敗はグループの順に最初のものを報告する (Python 版と同じ)
		if e != "" {
			return fail("%d 枚目のまとめ撮り: %s", gi+1, e)
		}
	}
	// 全部が同じ絵なら、まとめ撮りモードが効かずに同じ画面を撮っている (字幕も口も変わらない動画になる)
	if len(states) > 1 {
		seen := map[[32]byte]bool{}
		for _, p := range shots {
			b, _ := os.ReadFile(p)
			seen[sha256.Sum256(b)] = true
		}
		if len(seen) == 1 {
			return fail("Chrome が撮った絵がすべて同じ。templates/player.html のまとめ撮りモード (#sheet=) が効いていない")
		}
	}

	listing := ffconcatListing(data.Frames, data.Duration, func(st [3]int) string { return shots[st] })
	lst := filepath.Join(td, "frames.ffconcat")
	if err := os.WriteFile(lst, []byte(strings.Join(listing, "\n")+"\n"), 0o644); err != nil {
		return fail("%s: 書けない (%v)", lst, err)
	}
	args := []string{"-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", lst, "-i", m4a, "-map", "0:v", "-map", "1:a"}
	args = append(args, enc...)
	// 出力先の隣の一時ファイルに書いてから置き換える (中断・失敗で前回の正常な出力を壊さない)
	part, err := partPath(out)
	if err != nil {
		return fail("%s: 一時ファイルを作れない (%v)", out, err)
	}
	defer func() { _ = os.Remove(part) }()
	args = append(args, "-pix_fmt", "yuv420p", "-r", strconv.Itoa(mouthFPS), "-fps_mode", "cfr", "-c:a", "copy", "-shortest",
		"-movflags", "+faststart", "-f", "mp4", part)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(appCtx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	if e := interruptedErr(); e != nil {
		return e
	}
	if st, serr := os.Stat(part); err != nil || serr != nil || st.Size() == 0 {
		return fail("mp4 の書き出しに失敗 (%v): %s", err, lastRunes(strings.TrimSpace(stderr.String()), 400))
	}
	if err := replaceKeepingMode(part, out); err != nil {
		return fail("%s: 書けない (%v)", out, err)
	}
	return nil
}

// partPath は出力先の隣に一意な一時ファイルを作って名前を返す (同じ dir なので rename で置き換えられる。
// 同じ出力先へ同時に build しても取り合わない)。
func partPath(out string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".*.part")
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}

// fileUmask は起動時の umask (main が読む。テストでは 022 とみなす)。
var fileUmask = os.FileMode(0o022)

// checkReplaceable は dst を一時ファイルからの rename で置き換えられるか (既存ならディレクトリでなく、書き込める) を見る。
// replaceKeepingMode と、build の最初の確認 (cmdBuild) が同じ判定を使う
func checkReplaceable(dst string) error {
	st, err := os.Stat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // まだ無い (置き換えではなく作る)
	}
	if err != nil {
		return err // 自分を指す symlink (ELOOP)・途中が dir でない・読めない、を「まだ無い」と読まない
	}
	if st.IsDir() {
		return fmt.Errorf("ディレクトリがある")
	}
	if err := syscall.Access(dst, 2 /* W_OK */); err != nil {
		return fmt.Errorf("書き込めない既存のファイル: %w", err)
	}
	return nil
}

// replaceKeepingMode は tmp を dst へ rename する。dst が既にあればそのパーミッションを引き継ぎ、無ければ 0666 から umask を
// 引いたもの (Python の write_text と同じ)。読み取り専用の既存のファイルは置き換えない (Python は PermissionError で止まる)。
// dst は呼び出し側で symlink を解決しておく (rename はリンクそのものを置き換えるので、リンク先に書く Python 版と違ってしまう)。
// 既存のファイルのハードリンクは切れる (rename は別の inode にする。出力物にハードリンクを張る運用は想定しない)。
func replaceKeepingMode(tmp, dst string) error {
	mode := 0o666 &^ fileUmask
	if err := checkReplaceable(dst); err != nil {
		return err
	}
	if st, err := os.Stat(dst); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ffconcatListing は状態の列から ffconcat の一覧を作る。各絵の表示秒数は「次の状態の開始フレーム - 自分の開始フレーム」で、
// 最後の状態は丸めた duration から作った総フレーム数までを使う (Python 版と同じ。frameRuns は丸める前の duration で
// 数えるので、両者が 1 フレーム食い違うと最後の行の秒数が 0 になりうる)。
func ffconcatListing(runs [][4]int, duration float64, shot func([3]int) string) []string {
	total := max(1, int(math.Ceil(duration*mouthFPS)))
	listing := []string{"ffconcat version 1.0"}
	for i, r := range runs {
		end := total
		if i+1 < len(runs) {
			end = runs[i+1][0]
		}
		listing = append(listing, "file "+ffq(shot([3]int{r[1], r[2], r[3]})), fmt.Sprintf("duration %.6f", float64(end-r[0])/mouthFPS))
	}
	last := runs[len(runs)-1]
	listing = append(listing, "file "+ffq(shot([3]int{last[1], last[2], last[3]}))) // concat demuxer は最後の duration を使わないので 1 枚足す
	return listing
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[len(r)-n:])
	}
	return s
}

// sortedStates はフレームの状態 (字幕の行, 話し中か, 口の開き) の種類を Python の sorted と同じ順に並べる。
func sortedStates(frames [][4]int) [][3]int {
	set := map[[3]int]bool{}
	for _, r := range frames {
		set[[3]int{r[1], r[2], r[3]}] = true
	}
	out := make([][3]int, 0, len(set))
	for st := range set {
		out = append(out, st)
	}
	slicesSortStates(out)
	return out
}

func slicesSortStates(xs [][3]int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && lessState(xs[j], xs[j-1]); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func lessState(a, b [3]int) bool {
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// cmdBuild は合成済みの wav を連結し、HTML プレイヤーか mp4 (か両方) を書き出す。
func cmdBuild(env *Env, scriptArg, output, format string, jobs, kbps int) error {
	s, err := loadScript(resolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	// filepath.Abs を通さない (論理パスの $PWD で ".." を字面で畳んでしまう。敵対的レビュー 2 周目 P2-3)
	base := resolvePath(output)
	for _, ext := range []string{".html", ".mp4"} { // 付いていれば外す。v1.2 の ".2" のような拡張子でない部分は残す
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
		}
	}
	formats := []string{format}
	if format == "both" {
		formats = []string{"html", "mp4"}
	}
	// 出力先に書けるかを最初に確かめる (確かめないと、音声の圧縮と Chrome の撮影 (CPU の空き待ちで最大 20 分) を
	// 全部終えてから「一時ファイルを作れない」で止まる。issue 652)
	for _, f := range formats {
		dst := resolvePath(base + "." + f)
		if err := checkReplaceable(dst); err != nil {
			return fail("%s: 書けない (%v)", base+"."+f, err)
		}
		p, err := partPath(dst)
		if err != nil {
			return fail("%s: 書けない (%v)", base+"."+f, err)
		}
		_ = os.Remove(p)
	}
	td, err := os.MkdirTemp("", "zundamon-kaisetsu-")
	if err != nil {
		return fail("一時ディレクトリを作れない (%v)", err)
	}
	defer func() { _ = os.RemoveAll(td) }() // Ctrl-C でも appCtx の取り消しで子が止まってから走る

	data, pcm, err := assemble(s, env)
	if err != nil {
		return err
	}
	joined := filepath.Join(td, "joined.wav")
	if err := writeWav(joined, pcm); err != nil {
		return fail("%s: 書けない (%v)", joined, err)
	}
	m4a := filepath.Join(td, "audio.m4a")
	if err := encodeAudio(joined, m4a, kbps); err != nil {
		return err
	}
	for _, f := range formats {
		out := base + "." + f
		dst := resolvePath(out) // 出力先が symlink ならリンク先に書く (Python 版の write_text と同じ)
		if f == "html" {
			// mime は容器の型を明示する (推定に任せると audio/mp4a-latm 等になり、ブラウザが再生できない)
			audio, err := dataURI(m4a, "audio/mp4")
			if err != nil {
				return fail("%s: 読めない (%v)", m4a, err)
			}
			d := *data
			d.Audio = audio
			html, err := renderHTML(&d, env)
			if err != nil {
				return err
			}
			// 出力先の隣の一時ファイルに書いてから置き換える (中断で前回の正常な出力を壊さない)
			if err := writeOutput(dst, []byte(html)); err != nil {
				return fail("%s: 書けない (%v)", out, err)
			}
		} else if err := writeMP4(env, data, m4a, dst, td, jobs); err != nil {
			return err
		}
		st, err := os.Stat(dst)
		if err != nil {
			return fail("%s: 書き出されていない (%v)", out, err)
		}
		fmt.Fprintf(env.Stderr, "build: %s / %d 行 / %.1f 秒 / %.1f MB → %s\n", f, len(data.Lines), data.Duration, float64(st.Size())/1e6, out)
	}
	return nil
}
