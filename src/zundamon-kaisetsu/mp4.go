package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
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
