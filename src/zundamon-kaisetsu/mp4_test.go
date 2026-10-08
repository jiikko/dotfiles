package main

import (
	"fmt"
	goimage "image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mp4 の組み立て (writeMP4) を、偽の Chrome と偽の ffmpeg で通す (issue 651)。
//
// 偽物はテストのバイナリ自身を FAKE_MP4_TOOL つきで起こす (PNG を書いて切り出すので shell では書けない)。
// 偽の Chrome は #sheet= の状態ごとに、状態を表す色で 1 枚分の帯を塗った縦長の PNG を書く。偽の ffmpeg は本当に crop する。
// 切り出した state_*.png の色が状態と合うかで、グループ分け・crop の位置・状態との対応を判定する。
// プレイヤーの描画そのものは見ない (人が見る。issue 645)。

// stateColor は状態を色にする (帯ごとに違う色 = 切り出し位置がずれたら別の状態の色になる)
func stateColor(st visualState) color.RGBA {
	return color.RGBA{R: uint8(st[0] + 1), G: uint8(st[1]*128 + st[2]), B: uint8(200 + st[3] + 40*st[4]), A: 255}
}

// TestFakeMP4Tool は偽物の本体。FAKE_MP4_TOOL が無いときは何もしない (通常のテストとしては空で通る)
func TestFakeMP4Tool(t *testing.T) {
	tool := os.Getenv("FAKE_MP4_TOOL")
	if tool == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if log := os.Getenv("FAKE_MP4_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%s %s\n", tool, strings.Join(args, " "))
			_ = f.Close()
		}
	}
	os.Exit(fakeMP4Tool(tool, args))
}

func fakeMP4Tool(tool string, args []string) int {
	switch tool {
	case "chrome":
		if os.Getenv("FAKE_CHROME_RC") != "" {
			rc, _ := strconv.Atoi(os.Getenv("FAKE_CHROME_RC"))
			return rc
		}
		var w, h int
		var shot, uri string
		for _, a := range args {
			switch {
			case strings.HasPrefix(a, "--window-size="):
				_, _ = fmt.Sscanf(strings.TrimPrefix(a, "--window-size="), "%d,%d", &w, &h)
			case strings.HasPrefix(a, "--screenshot="):
				shot = strings.TrimPrefix(a, "--screenshot=")
			case strings.HasPrefix(a, "file:"):
				uri = a
			}
		}
		u, err := url.Parse(uri)
		if err != nil {
			return 3
		}
		// 本物のプレイヤーが受け取る書式か (player.html から抜き出した正規表現で見る。偽物の Sscanf は空白などを許すので頼らない)
		if re, err := regexp.Compile(os.Getenv("FAKE_SHEET_RE")); err != nil || os.Getenv("FAKE_SHEET_RE") == "" || !re.MatchString("#"+u.EscapedFragment()) { // Chrome の location.hash はデコードしない
			fmt.Fprintf(os.Stderr, "偽の Chrome: player.html が受け取らない断片 %q\n", u.EscapedFragment())
			return 11
		}
		var states []visualState
		for _, part := range strings.Split(strings.TrimPrefix(u.Fragment, "sheet="), ";") {
			var st visualState
			if _, err := fmt.Sscanf(part, "%d,%d,%d,%d,%d", &st[0], &st[1], &st[2], &st[3], &st[4]); err != nil {
				return 4
			}
			states = append(states, st)
		}
		band := h / max(len(states), 1) // 1 枚分の高さ (テストは videoSize を縮めるので、窓の大きさから割り出す)
		img := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
		for k, st := range states {
			c := stateColor(st)
			if os.Getenv("FAKE_CHROME_SAME") != "" { // まとめ撮りが効かない (全部同じ画面) を演じる
				c = color.RGBA{R: 9, G: 9, B: 9, A: 255}
			}
			for y := k * band; y < (k+1)*band && y < h; y++ {
				for x := range w {
					img.SetRGBA(x, y, c)
				}
			}
		}
		return writePNG(shot, img)
	case "ffmpeg":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "-encoders"):
			fmt.Println(" V....D libx264              libx264 H.264 / AVC (偽物)")
			return 0
		case strings.Contains(joined, "-filter_complex"):
			return fakeCrop(args)
		case strings.Contains(joined, "-f mp4"):
			if os.Getenv("FAKE_MUX_RC") != "" {
				rc, _ := strconv.Atoi(os.Getenv("FAKE_MUX_RC"))
				return rc
			}
			checkFrames(args)
			return writeFile(args[len(args)-1], "fake mp4")
		default: // 音声の圧縮 (最後の引数が出力)
			return writeFile(args[len(args)-1], "fake m4a")
		}
	}
	return 2
}

var stateNameRe = regexp.MustCompile(`state_(-?\d+)_(\d+)_(\d+)_(\d+)_(\d+)\.png$`)

// checkFrames は mux に渡された連番 (-framerate 30 -i <dir>/f%06d.png) の各フレームが、symlink の先の状態の色かを確かめ、
// 結果を記録に書く (build の一時 dir は終わると消えるので、絵は mux の時点で見る)。concat の一覧で渡されたら失敗にする (issue 660)
func checkFrames(args []string) {
	var pattern string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-i" {
			pattern = args[i+1]
			break
		}
	}
	result := ""
	files := []string{}
	if !strings.HasSuffix(pattern, "/f%06d.png") || !slices.Contains(args, "-framerate") {
		result = "frames-bad 連番で渡されていない: " + pattern
	} else {
		dir := strings.ReplaceAll(strings.TrimSuffix(pattern, "/f%06d.png"), "%%", "%")
		files, _ = filepath.Glob(filepath.Join(dir, "f*.png"))
		sort.Strings(files)
		if len(files) == 0 {
			result = "frames-bad 連番が空 " + dir
		}
	}
	states := map[string]bool{}
	var order []string // フレームごとの状態の絵の名前 (テストが frameStates の期待と突き合わせる)
	for k, p := range files {
		if result != "" {
			break
		}
		if filepath.Base(p) != fmt.Sprintf("f%06d.png", k) {
			result = "frames-bad 番号が飛んでいる " + filepath.Base(p)
			break
		}
		target, err := os.Readlink(p)
		m := stateNameRe.FindStringSubmatch(target)
		if err != nil || m == nil {
			result = "frames-bad symlink の先が状態の絵でない " + p
			break
		}
		states[filepath.Base(target)] = true
		order = append(order, filepath.Base(target))
		li, _ := strconv.Atoi(m[1])
		sp, _ := strconv.Atoi(m[2])
		lv, _ := strconv.Atoi(m[3])
		bl, _ := strconv.Atoi(m[4])
		cd, _ := strconv.Atoi(m[5])
		want := stateColor(visualState{li - 1, sp, lv, bl, cd})
		f, err := os.Open(p)
		if err != nil {
			result = "frames-bad 開けない " + p
			break
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			result = "frames-bad 読めない " + p
			break
		}
		if fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy()) != os.Getenv("FAKE_MP4_SIZE") {
			result = "frames-bad 大きさ " + p
			break
		}
		// 全画素が状態の色で一様 (中央の 1 点だけだと、帯の半分未満のずれを見逃す)
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y && result == ""; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				if got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA); got != want {
					result = fmt.Sprintf("frames-bad %s の (%d,%d) の色が %v (期待 %v)", filepath.Base(target), x, y, got, want)
					break
				}
			}
		}
	}
	if result == "" {
		result = fmt.Sprintf("frames-ok %d %d", len(states), len(files))
	}
	if log := os.Getenv("FAKE_MP4_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, result)
			_ = f.Close()
		}
		_ = os.WriteFile(log+".order", []byte(strings.Join(order, "\n")), 0o644)
	}
}

var cropRe = regexp.MustCompile(`\[s(\d+)\]crop=(\d+):(\d+):(\d+):(\d+)\[o(\d+)\]`)

// fakeCrop は -filter_complex の crop と -map [oK] <出力> を読んで、入力の PNG を本当に切り出す
func fakeCrop(args []string) int {
	var in, fc string
	maps := map[string]string{}
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-i":
			in = args[i+1]
		case "-filter_complex":
			fc = args[i+1]
		case "-map":
			maps[args[i+1]] = args[i+2]
		}
	}
	f, err := os.Open(in)
	if err != nil {
		return 5
	}
	src, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		return 6
	}
	for _, m := range cropRe.FindAllStringSubmatch(fc, -1) {
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		x, _ := strconv.Atoi(m[4])
		y, _ := strconv.Atoi(m[5])
		out := maps["[o"+m[6]+"]"]
		sub := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
		for yy := range h {
			for xx := range w {
				sub.Set(xx, yy, src.At(x+xx, y+yy))
			}
		}
		if rc := writePNG(out, sub); rc != 0 {
			return rc
		}
	}
	return 0
}

func writePNG(path string, img goimage.Image) int {
	f, err := os.Create(path)
	if err != nil {
		return 7
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		return 8
	}
	return 0
}

func writeFile(path, body string) int {
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return 9
	}
	return 0
}

// fakeMP4Tools は偽の chrome と ffmpeg を PATH の先頭と CHROME に置き、呼び出しの記録の場所を返す
func fakeMP4Tools(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	self, err := os.Executable()
	must(t, err)
	for _, tool := range []string{"chrome", "ffmpeg"} {
		body := fmt.Sprintf("#!/bin/sh\nFAKE_MP4_TOOL=%s FAKE_MP4_LOG=%q exec %q -test.run='^TestFakeMP4Tool$' -- \"$@\"\n", tool, log, self)
		writeShim(t, bin, tool, body)
	}
	prependPath(t, bin)
	t.Setenv("CHROME", filepath.Join(bin, "chrome"))
	// 絵を小さくする (1280x720 のままだと -race の下で PNG の処理に 1 本 25 秒かかる。大きさは判定に関係しない)。
	// 🚨 この package のテストは t.Parallel を使わない前提 (videoSize はパッケージ変数)
	old := videoSize
	videoSize = [2]int{64, 36}
	t.Cleanup(func() { videoSize = old })
	t.Setenv("FAKE_MP4_SIZE", fmt.Sprintf("%dx%d", videoSize[0], videoSize[1]))
	t.Setenv("FAKE_SHEET_RE", playerSheetRegexp(t).String())
	return log
}

// buildMP4 は testdata/build の台本を mp4 で build する (CPU の負荷は空いていることにする)
func buildMP4(t *testing.T) (out string, log string, err error) {
	t.Helper()
	return buildMP4From(t, filepath.Join("testdata", "build", "script.json"))
}

func buildMP4From(t *testing.T, script string) (out string, log string, err error) {
	t.Helper()
	log = fakeMP4Tools(t)
	env := testEnv(t)
	env.LoadAvg = func() (float64, bool) { return 0, true }
	env.Now, env.Sleep = time.Now, func(time.Duration) {}
	out = filepath.Join(t.TempDir(), "out")
	err = cmdBuild(env, script, out, "mp4", 2, 64, true)
	return out + ".mp4", log, err
}

func chromeCalls(t *testing.T, log string) []string {
	t.Helper()
	var cs []string
	for _, l := range calls(t, log) {
		if strings.HasPrefix(l, "chrome ") {
			cs = append(cs, l)
		}
	}
	return cs
}

// TestWriteMP4CropsEachState は、まとめ撮りの帯を状態ごとに正しく切り出し、全状態を撮り、mp4 を書くことを確かめる
func TestWriteMP4CropsEachState(t *testing.T) {
	checkMP4CropsEachState(t, filepath.Join("testdata", "build", "script.json"))
}

func checkMP4CropsEachState(t *testing.T, script string) {
	t.Helper()
	out, log, err := buildMP4From(t, script)
	must(t, err)
	if b, err := os.ReadFile(out); err != nil || string(b) != "fake mp4" {
		t.Fatalf("mp4 が書かれていない: %v %q", err, b)
	}
	// 呼び出しの記録から、撮った状態を全部集める (1 回のまとめ撮りは sheetStates 枚まで)
	var states []visualState
	for _, c := range chromeCalls(t, log) {
		frag := c[strings.Index(c, "#sheet=")+len("#sheet="):]
		n := 0
		for part := range strings.SplitSeq(frag, ";") {
			var st visualState
			if _, err := fmt.Sscanf(part, "%d,%d,%d,%d,%d", &st[0], &st[1], &st[2], &st[3], &st[4]); err != nil {
				t.Fatalf("撮った状態を読めない: %q", part)
			}
			states = append(states, st)
			n++
		}
		if n > sheetStates {
			t.Errorf("1 回のまとめ撮りが %d 枚 (上限 %d)", n, sheetStates)
		}
		if want := fmt.Sprintf("--window-size=%d,%d", videoSize[0], videoSize[1]*n); !strings.Contains(c, want) {
			t.Errorf("窓の大きさが状態の数と合わない (%s が無い): %s", want, c)
		}
	}
	if len(states) < 2 {
		t.Fatalf("前提: 状態が 2 つ以上ある台本で試す (got %d)", len(states))
	}
	// 切り出した絵の色が状態と合う (crop の位置・グループ分け・状態との対応がずれていない)。偽の mux が一覧の各絵を確かめて記録する
	var res string
	for _, l := range calls(t, log) {
		if strings.HasPrefix(l, "frames-") {
			res = l
		}
	}
	if !strings.HasPrefix(res, "frames-ok ") {
		t.Fatalf("mux に渡った絵が状態と合わない: %q", res)
	}
	distinct := map[visualState]bool{}
	for _, st := range states {
		distinct[st] = true
	}
	var nStates, nFrames int
	if _, err := fmt.Sscanf(res, "frames-ok %d %d", &nStates, &nFrames); err != nil || nStates != len(distinct) {
		t.Errorf("mux の連番の絵の種類が撮った状態の数 %d と合わない: %q", len(distinct), res)
	}
	// 総フレーム数は ceil(尺 × fps) (testdata/build の台本は 3.5835 秒 = 108 フレーム。issue 660 で実物の尺を ffprobe で確かめた)
	if nFrames != 108 {
		t.Errorf("連番のフレーム数: got %d want 108 (ceil(3.5835 × 30))", nFrames)
	}
	// フレームごとの状態の並びが frameStates (プレイヤーの規則: フレーム k は時刻 (k+0.5)/fps の状態) と一致する (issue 660)
	env := testEnv(t)
	sc, err := loadScript(resolvePath(script), env)
	must(t, err)
	data, _, err := assemble(sc, env)
	must(t, err)
	var want []string
	for _, st := range frameStates(data.Frames, data.Blinks, data.Cards, data.Duration) {
		want = append(want, fmt.Sprintf("state_%d_%d_%d_%d_%d.png", st[0]+1, st[1], st[2], st[3], st[4]))
	}
	gotOrder, err := os.ReadFile(log + ".order")
	must(t, err)
	if got := strings.Split(string(gotOrder), "\n"); !slices.Equal(got, want) {
		for k := range min(len(got), len(want)) {
			if got[k] != want[k] {
				t.Errorf("フレーム %d の状態: got %s want %s (全 %d / %d フレーム)", k, got[k], want[k], len(got), len(want))
				break
			}
		}
		if len(got) != len(want) {
			t.Errorf("フレーム数: got %d want %d", len(got), len(want))
		}
	}
	listing := ""
	for _, l := range calls(t, log) {
		if strings.HasPrefix(l, "ffmpeg ") && strings.Contains(l, "-f mp4") {
			listing = l + " "
		}
	}
	if listing == "" {
		t.Fatal("最後の mux が呼ばれていない")
	}
	// 30fps の連番として読ませ、映像を変換・切り詰めるオプションを付けない (-shortest は終わりを欠かせ、-r / -fps_mode は切り替わりをずらした)
	for _, a := range []string{" -shortest", " -fps_mode ", " -r "} {
		if strings.Contains(listing, a) {
			t.Errorf("mux の引数に %q がある: %s", strings.TrimSpace(a), listing)
		}
	}
	if !strings.Contains(listing, fmt.Sprintf(" -framerate %d ", mouthFPS)) {
		t.Errorf("mux の引数に -framerate %d が無い: %s", mouthFPS, listing)
	}
}

// TestWriteMP4RejectsIdenticalShots は、撮った絵が全部同じ (まとめ撮りが効いていない) なら mp4 を作らずに止めることを確かめる
func TestWriteMP4RejectsIdenticalShots(t *testing.T) {
	t.Setenv("FAKE_CHROME_SAME", "1")
	out, _, err := buildMP4(t)
	if err == nil || !strings.Contains(err.Error(), "すべて同じ") {
		t.Fatalf("全部同じ絵で止まるはず: %v", err)
	}
	if isFile(out) {
		t.Error("止めたのに mp4 が書かれた")
	}
}

// TestWriteMP4ReportsChromeFailure は、Chrome の失敗を rc つきで報告して止めることを確かめる
func TestWriteMP4ReportsChromeFailure(t *testing.T) {
	t.Setenv("FAKE_CHROME_RC", "3")
	out, _, err := buildMP4(t)
	if err == nil || !strings.Contains(err.Error(), "Chrome が失敗 (rc=3)") {
		t.Fatalf("Chrome の失敗で止まるはず: %v", err)
	}
	if isFile(out) {
		t.Error("止めたのに mp4 が書かれた")
	}
}

// TestWriteMP4KeepsOutputOnMuxFailure は、最後の mux が失敗したら止め、前回の出力を壊さないことを確かめる
func TestWriteMP4KeepsOutputOnMuxFailure(t *testing.T) {
	t.Setenv("FAKE_MUX_RC", "1")
	log := fakeMP4Tools(t)
	_ = log
	env := testEnv(t)
	env.LoadAvg = func() (float64, bool) { return 0, true }
	env.Now, env.Sleep = time.Now, func(time.Duration) {}
	out := filepath.Join(t.TempDir(), "out")
	must(t, os.WriteFile(out+".mp4", []byte("previous"), 0o644))
	err := cmdBuild(env, filepath.Join("testdata", "build", "script.json"), out, "mp4", 2, 64, true)
	if err == nil || !strings.Contains(err.Error(), "mp4 の書き出しに失敗") {
		t.Fatalf("mux の失敗で止まるはず: %v", err)
	}
	if b, _ := os.ReadFile(out + ".mp4"); string(b) != "previous" {
		t.Errorf("前回の出力が壊れた: %q", b)
	}
}
