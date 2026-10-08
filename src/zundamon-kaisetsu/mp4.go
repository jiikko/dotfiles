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
	"slices"
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

// sheetFragment は、まとめ撮りの状態 (字幕の行, 話し中か, 口の開き, 目を閉じているキャラのビット, 区切りのカード) の並びを player.html が読む URL の断片にする。
// 🚨 書式は player.html の `location.hash.match(/^#sheet=…/)` と 1 対 1。片方だけ変えると、プレイヤーは通常の画面のまま撮られる
// (TestSheetFragmentMatchesPlayer が両者を突き合わせる。issue 650)
func sheetFragment(group []visualState) string {
	parts := make([]string, len(group))
	for i, st := range group {
		parts[i] = fmt.Sprintf("%d,%d,%d,%d,%d", st[0], st[1], st[2], st[3], st[4])
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
	perFrame := frameStates(data.Frames, data.Blinks, data.Cards, data.Duration)
	states := sortedStates(perFrame)
	shots := map[visualState]string{}
	for _, st := range states {
		shots[st] = filepath.Join(td, fmt.Sprintf("state_%d_%d_%d_%d_%d.png", st[0]+1, st[1], st[2], st[3], st[4]))
	}
	var groups [][]visualState
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

	// フレームごとに状態の絵への symlink を連番で並べ、30fps の連番として読ませる (issue 660)。
	// 🚨 concat の一覧 (各絵の表示秒数) を -r 30 -fps_mode cfr で 30fps にする形には戻さない: 変換のタイムスタンプの丸めで、
	//    字幕と口の切り替わりが音声と ±1 フレーム前後し、-shortest と合わせて終わりの映像が欠けた (実測は issue 660)
	seq := filepath.Join(td, "seq")
	if err := os.MkdirAll(seq, 0o755); err != nil {
		return fail("%s: 作れない (%v)", seq, err)
	}
	for k, st := range perFrame {
		if err := os.Symlink(shots[st], filepath.Join(seq, fmt.Sprintf("f%06d.png", k))); err != nil {
			return fail("%s: 書けない (%v)", seq, err)
		}
	}
	// 連番の書式 (%06d) の外にある % は、ffmpeg が書式として読まないよう %% にする
	pattern := strings.ReplaceAll(seq, "%", "%%") + "/f%06d.png"
	args := []string{"-v", "error", "-y", "-framerate", strconv.Itoa(mouthFPS), "-i", pattern, "-i", m4a, "-map", "0:v", "-map", "1:a"}
	args = append(args, enc...)
	// 出力先の隣の一時ファイルに書いてから置き換える (中断・失敗で前回の正常な出力を壊さない)
	part, err := partPath(out)
	if err != nil {
		return fail("%s: 一時ファイルを作れない (%v)", out, err)
	}
	defer func() { _ = os.Remove(part) }()
	// -shortest は付けない: 音声の終わりの手前のフレームの境で切られる。映像は ceil(尺 × fps) フレームで音声を覆う (差は 1 フレーム未満)
	args = append(args, "-pix_fmt", "yuv420p", "-c:a", "copy", "-movflags", "+faststart", "-f", "mp4", part)
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

// visualState は 1 フレームの見た目の状態 (字幕の行, 話し中か, 口の開き, 目を閉じているキャラのビット, 区切りのカード)。
// mp4 はこの種類ごとに 1 枚撮る。
type visualState [5]int

// frameStates はフレームごとの状態を並べる。総フレーム数は ceil(duration × fps) で、runs (frameRuns の出力) と
// blinks (blinkRuns の出力)・cards (chapterCardRuns の出力) の各区間を、次の区間の開始まで繰り返す (nil なら 0 のまま)。
// 最後の状態は総フレーム数まで続ける
func frameStates(runs [][4]int, blinks, cards [][2]int, duration float64) []visualState {
	total := max(1, int(math.Ceil(duration*mouthFPS)))
	out := make([]visualState, 0, total)
	ri := 0
	blinkAt, cardAt := runCursor(blinks), runCursor(cards)
	for k := range total {
		for ri+1 < len(runs) && runs[ri+1][0] <= k {
			ri++
		}
		st := visualState{-1, 0, 0, 0, 0}
		if len(runs) > 0 && runs[ri][0] <= k {
			st = visualState{runs[ri][1], runs[ri][2], runs[ri][3], 0, 0}
		}
		st[3], st[4] = blinkAt(k), cardAt(k)
		out = append(out, st)
	}
	return out
}

// runCursor は [開始フレーム, 値] の変わり目の列を、フレームの昇順に引く関数にする (最初の区間より前と nil は 0)。
func runCursor(runs [][2]int) func(k int) int {
	i := 0
	return func(k int) int {
		for i+1 < len(runs) && runs[i+1][0] <= k {
			i++
		}
		if len(runs) > 0 && runs[i][0] <= k {
			return runs[i][1]
		}
		return 0
	}
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[len(r)-n:])
	}
	return s
}

// sortedStates はフレームごとの状態の種類を (行, 話し中, 口, まばたき, カード) の辞書順に並べる。
func sortedStates(states []visualState) []visualState {
	set := map[visualState]bool{}
	for _, st := range states {
		set[st] = true
	}
	out := make([]visualState, 0, len(set))
	for st := range set {
		out = append(out, st)
	}
	slices.SortFunc(out, cmpState) // 状態は重複しないので安定でなくてよい
	return out
}

// cmpState は状態を (行, 話し中, 口, まばたき, カード) の辞書順で比べる
func cmpState(a, b visualState) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
