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
func stateColor(st [3]int) color.RGBA {
	return color.RGBA{R: uint8(st[0] + 1), G: uint8(st[1]*128 + st[2]), B: 200, A: 255}
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
		var states [][3]int
		for _, part := range strings.Split(strings.TrimPrefix(u.Fragment, "sheet="), ";") {
			var st [3]int
			if _, err := fmt.Sscanf(part, "%d,%d,%d", &st[0], &st[1], &st[2]); err != nil {
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

var stateNameRe = regexp.MustCompile(`state_(-?\d+)_(\d+)_(\d+)\.png$`)

// checkFrames は mux に渡された concat の一覧の各絵が、ファイル名の状態の色かを確かめ、結果を記録に書く
// (build の一時 dir は終わると消えるので、絵は mux の時点で見る)
func checkFrames(args []string) {
	var lst string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-f" && args[i+1] == "concat" {
			for j := i; j < len(args)-1; j++ {
				if args[j] == "-i" {
					lst = args[j+1]
					break
				}
			}
		}
	}
	b, err := os.ReadFile(lst)
	result := "frames-ok"
	files := map[string]bool{}
	if err != nil {
		result = "frames-bad 一覧を読めない"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "file ") {
			continue
		}
		p := strings.Trim(strings.TrimPrefix(l, "file "), "'")
		files[p] = true
	}
	for p := range files {
		m := stateNameRe.FindStringSubmatch(p)
		if m == nil {
			result = "frames-bad 名前が状態でない " + p
			break
		}
		li, _ := strconv.Atoi(m[1])
		sp, _ := strconv.Atoi(m[2])
		lv, _ := strconv.Atoi(m[3])
		want := stateColor([3]int{li - 1, sp, lv})
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
		if got := color.RGBAModel.Convert(img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2)).(color.RGBA); got != want {
			result = fmt.Sprintf("frames-bad %s の色が %v (期待 %v)", filepath.Base(p), got, want)
			break
		}
		if fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy()) != os.Getenv("FAKE_MP4_SIZE") {
			result = "frames-bad 大きさ " + p
			break
		}
	}
	if log := os.Getenv("FAKE_MP4_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%s %d\n", result, len(files))
			_ = f.Close()
		}
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
		must(t, os.WriteFile(filepath.Join(bin, tool), []byte(body), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CHROME", filepath.Join(bin, "chrome"))
	// 絵を小さくする (1280x720 のままだと -race の下で PNG の処理に 1 本 25 秒かかる。大きさは判定に関係しない)。
	// 🚨 この package のテストは t.Parallel を使わない前提 (videoSize はパッケージ変数)
	old := videoSize
	videoSize = [2]int{64, 36}
	t.Cleanup(func() { videoSize = old })
	t.Setenv("FAKE_MP4_SIZE", fmt.Sprintf("%dx%d", videoSize[0], videoSize[1]))
	return log
}

// buildMP4 は testdata/build の台本を mp4 で build する (CPU の負荷は空いていることにする)
func buildMP4(t *testing.T) (out string, log string, err error) {
	t.Helper()
	log = fakeMP4Tools(t)
	env := testEnv(t)
	env.LoadAvg = func() (float64, bool) { return 0, true }
	env.Now, env.Sleep = time.Now, func(time.Duration) {}
	out = filepath.Join(t.TempDir(), "out")
	err = cmdBuild(env, filepath.Join("testdata", "build", "script.json"), out, "mp4", 2, 64)
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
	out, log, err := buildMP4(t)
	must(t, err)
	if b, err := os.ReadFile(out); err != nil || string(b) != "fake mp4" {
		t.Fatalf("mp4 が書かれていない: %v %q", err, b)
	}
	// 呼び出しの記録から、撮った状態を全部集める (1 回のまとめ撮りは sheetStates 枚まで)
	var states [][3]int
	for _, c := range chromeCalls(t, log) {
		frag := c[strings.Index(c, "#sheet=")+len("#sheet="):]
		n := 0
		for part := range strings.SplitSeq(frag, ";") {
			var st [3]int
			if _, err := fmt.Sscanf(part, "%d,%d,%d", &st[0], &st[1], &st[2]); err != nil {
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
	distinct := map[[3]int]bool{}
	for _, st := range states {
		distinct[st] = true
	}
	if res != fmt.Sprintf("frames-ok %d", len(distinct)) {
		t.Errorf("mux の一覧の絵の種類が撮った状態の数 %d と合わない: %q", len(distinct), res)
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
	err := cmdBuild(env, filepath.Join("testdata", "build", "script.json"), out, "mp4", 2, 64)
	if err == nil || !strings.Contains(err.Error(), "mp4 の書き出しに失敗") {
		t.Fatalf("mux の失敗で止まるはず: %v", err)
	}
	if b, _ := os.ReadFile(out + ".mp4"); string(b) != "previous" {
		t.Errorf("前回の出力が壊れた: %q", b)
	}
}
