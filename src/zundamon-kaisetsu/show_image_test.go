package main

import (
	"bytes"
	"encoding/base64"
	stdimage "image" // パッケージに定数 image (engine.go) があるので別名にする
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// writeImage は w x h の画像を dir/name に書く。noise なら乱数で埋めて圧縮が効かないようにする (ファイルの大きさの上限を試す)。
func writeImage(t *testing.T, dir, name string, w, h int, noise bool) {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	if noise {
		r := rand.New(rand.NewPCG(1, 2))
		for i := range img.Pix {
			img.Pix[i] = uint8(r.IntN(256))
		}
	} else {
		img.Set(0, 0, color.Black)
	}
	var buf bytes.Buffer
	var err error
	if strings.HasSuffix(name, ".png") {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// withOrientation は JPEG の先頭 (SOI の直後) に、Orientation だけを持つ EXIF (APP1) を差し込む。
func withOrientation(t *testing.T, path string, o uint16) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, byte(o >> 8), byte(o), 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := append([]byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	out := append(append(append([]byte{}, b[:2]...), app1...), b[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func imageShow(src string) map[string]any {
	return map[string]any{"type": "image", "src": src, "alt": "ロックの流れ"}
}

func TestRejectBadImageShow(t *testing.T) {
	for _, tc := range []struct {
		show any
		want string
	}{
		{imageShow("a.gif"), "show.src は .png / .jpg / .jpeg"},
		{imageShow("a"), "show.src は .png / .jpg / .jpeg"},
		{imageShow(""), "show.src は空でない文字列"},
		{map[string]any{"type": "image", "src": "a.png"}, "show.alt は空でない文字列"},
		{withKey(imageShow("a.png"), "alt", strings.Repeat("説", 41)), "show.alt は 40 字まで"},
		{withKey(imageShow("a.png"), "caption", "x"), "show (image) に書けるのは type/src/alt だけ"},
	} {
		path := writeScript(t, map[string]any{"lines": []any{
			map[string]any{"who": "metan", "text": "a"},
			map[string]any{"who": "metan", "text": "b", "show": tc.show},
		}})
		_, err := loadScript(path, testEnv(t))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %q を含む拒否にならなかった (%v)", tc.show, tc.want, err)
		}
	}
}

// TestImageScaleBounds は、箱 (imageBoxW x imageBoxH) に収めたときの縮尺が 0.5〜1 の画像だけを通すことを、境界の両側で確かめる。
func TestImageScaleBounds(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want string // 空なら通る
	}{
		{imageBoxW, imageBoxH, ""},              // 箱ちょうど (縮尺 1)
		{imageBoxW * 2, imageBoxH * 2, ""},      // 縮尺 0.5 ちょうど
		{imageBoxW - 1, imageBoxH - 1, "小さすぎる"}, // どちらの辺も箱に届かない
		{imageBoxW*2 + 1, imageBoxH, "大きすぎる"},   // 幅が 2 倍を超える
		{imageBoxW, imageBoxH*2 + 1, "大きすぎる"},   // 高さが 2 倍を超える
		{imageBoxW, imageBoxH / 2, ""},          // 横長: 表示の高さが箱のちょうど半分
		{imageBoxW, imageBoxH/2 - 1, "細長すぎる"},   // 横長: 半分に届かない
		{(imageBoxW + 1) / 2, imageBoxH, ""},    // 縦長: 表示の幅が箱の半分以上
		{imageBoxW/2 - 1, imageBoxH, "細長すぎる"},   // 縦長: 半分に届かない
		{imageBoxW, 10, "細長すぎる"},                // 1 辺だけ箱に届く細い帯
		{0, 10, "大きさが不正"},
	} {
		err := checkImageScale("s.json", "a.png", tc.w, tc.h)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%dx%d: 通るはずが止まった (%v)", tc.w, tc.h, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%dx%d: %q で止まるはず (%v)", tc.w, tc.h, tc.want, err)
		}
	}
}

func TestEmbedShowImages(t *testing.T) {
	set := func(src string, gen func(dir string)) (*PlayerData, error) {
		return buildWithShows(t, func(dir string, lines []any) {
			gen(dir)
			lines[1].(map[string]any)["show"] = imageShow(src)
		})
	}
	data, err := set("fig/a.png", func(dir string) {
		if err := os.Mkdir(filepath.Join(dir, "fig"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeImage(t, filepath.Join(dir, "fig"), "a.png", 800, 500, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	src := data.Shows[0].Src
	if !strings.HasPrefix(src, "data:image/png;base64,") {
		t.Fatalf("図の src が data URI になっていない: %.60s", src)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(src, "data:image/png;base64,")); err != nil {
		t.Errorf("data URI が base64 として読めない: %v", err)
	}
	if strings.Contains(src, "fig/a.png") {
		t.Error("手元のパスが HTML のデータに残っている")
	}

	// 中身が JPEG なら、拡張子が .png でも MIME は中身に合わせる
	data, err = set("b.png", func(dir string) {
		writeImage(t, dir, "b.png.jpg", 800, 500, false)
		must(t, os.Rename(filepath.Join(dir, "b.png.jpg"), filepath.Join(dir, "b.png")))
	})
	if err != nil || !strings.HasPrefix(data.Shows[0].Src, "data:image/jpeg;base64,") {
		t.Errorf("中身が JPEG の図の MIME が違う: %v", err)
	}

	for _, tc := range []struct {
		name string
		gen  func(dir string)
		want string
	}{
		{"無い", func(string) {}, "を読めない"},
		{"小さい", func(dir string) { writeImage(t, dir, "x.png", 300, 200, false) }, "小さすぎる"},
		{"大きい", func(dir string) { writeImage(t, dir, "x.png", 2000, 600, false) }, "大きすぎる"},
		{"細長い", func(dir string) { writeImage(t, dir, "x.png", 1000, 60, false) }, "細長すぎる"},
		{"重い", func(dir string) { writeImage(t, dir, "x.png", 1000, 700, true) }, "バイト。上限"},
		{"壊れている", func(dir string) { must(t, os.WriteFile(filepath.Join(dir, "x.png"), []byte("not png"), 0o644)) }, "画像として読めない"},
		{"本体が欠けている", func(dir string) {
			writeImage(t, dir, "x.png", 800, 500, false)
			b, _ := os.ReadFile(filepath.Join(dir, "x.png"))
			must(t, os.WriteFile(filepath.Join(dir, "x.png"), b[:40], 0o644)) // 署名と IHDR は残り、大きさだけは読める
		}, "画像として読めない"},
		{"向きの情報", func(dir string) {
			writeImage(t, dir, "x.png.jpg", 800, 500, false)
			withOrientation(t, filepath.Join(dir, "x.png.jpg"), 6)
			must(t, os.Rename(filepath.Join(dir, "x.png.jpg"), filepath.Join(dir, "x.png")))
		}, "Orientation=6"},
	} {
		_, err := set("x.png", tc.gen)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %q で止まるはず (%v)", tc.name, tc.want, err)
		}
	}
}

// TestEmbedShowImagesPaths は、2 枚目以降の図も埋め込むこと、同じ画像を別の書き方 (./・~・symlink) で指しても
// 図解の表で 1 つにまとまること、向きの情報が 1 (回さない) の JPEG は通ることを確かめる。
func TestEmbedShowImagesPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeImage(t, home, "h.png", 800, 500, false)
	data, err := buildWithShows(t, func(dir string, lines []any) {
		writeImage(t, dir, "a.png", 800, 500, false)
		writeImage(t, dir, "b.jpg", 800, 500, false)
		withOrientation(t, filepath.Join(dir, "b.jpg"), 1)
		if err := os.Symlink(filepath.Join(dir, "a.png"), filepath.Join(dir, "link.png")); err != nil {
			t.Fatal(err)
		}
		lines[0].(map[string]any)["show"] = imageShow("a.png")
		lines[1].(map[string]any)["show"] = imageShow("./a.png")
		lines[2].(map[string]any)["show"] = imageShow("link.png")
		lines[3].(map[string]any)["show"] = imageShow("b.jpg")
		lines[4].(map[string]any)["show"] = imageShow("~/h.png")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Shows) != 3 {
		t.Fatalf("図解の表: got %d 個 want 3 (a.png の 3 つの書き方は 1 つにまとまる)", len(data.Shows))
	}
	for i, want := range []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/png;base64,"} {
		if !strings.HasPrefix(data.Shows[i].Src, want) {
			t.Errorf("図 %d が埋め込まれていない: %.40s", i, data.Shows[i].Src)
		}
	}
	for i, l := range data.Lines {
		if want := []int{0, 0, 0, 1, 2}[i]; l.Show == nil || *l.Show != want {
			t.Errorf("lines[%d] の図解: got %v want %d", i, l.Show, want)
		}
	}
}

// TestImageBoxMatchesPlayerCSS は、図の箱の定数 (imageBoxW / imageBoxH) が player.html の .stage-show と .show-image から
// 計算した 720p の箱の大きさと合っていることを確かめる。CSS だけを変えると、build の検査と画面の大きさが食い違う。
func TestImageBoxMatchesPlayerCSS(t *testing.T) {
	tb, err := os.ReadFile(testEnv(t).Template())
	if err != nil {
		t.Fatal(err)
	}
	tpl := string(tb)
	num := func(re string) float64 {
		m := regexp.MustCompile(re).FindStringSubmatch(tpl)
		if m == nil {
			t.Fatalf("player.html に %s が無い", re)
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 先頭の規則だけを読む (行頭のセレクタに限り、width は max-width / min-width と区別する)
	boxW := num(`(?m)^\.stage-show \{[^}]*[{;]\s*width: ([0-9.]+)%`)
	boxH := num(`(?m)^\.stage-show \{[^}]*[{;]\s*height: ([0-9.]+)%`)
	pad := num(`(?m)^\.show-image \{[^}]*padding: ([0-9.]+)cqw`)
	// 画像の大きさを実際に決めているのは img の max-width / max-height。箱 (幅 boxW%・高さ boxH% x 9/16) から padding の 2 倍を引いた値か
	imgW := num(`(?m)^\.show-image img \{[^}]*max-width: calc\(([0-9.]+)cqw - `)
	imgWPad := num(`(?m)^\.show-image img \{[^}]*max-width: calc\([0-9.]+cqw - ([0-9.]+)cqw\)`)
	imgH := num(`(?m)^\.show-image img \{[^}]*max-height: calc\(([0-9.]+)cqw - `)
	imgHPad := num(`(?m)^\.show-image img \{[^}]*max-height: calc\([0-9.]+cqw - ([0-9.]+)cqw\)`)
	if imgW != boxW || imgH != boxH*9/16 || imgWPad != 2*pad || imgHPad != 2*pad {
		t.Errorf("img の max-width / max-height (calc(%gcqw - %gcqw) / calc(%gcqw - %gcqw)) が箱 (%g%% x %g%%) と padding (%gcqw) に合わない",
			imgW, imgWPad, imgH, imgHPad, boxW, boxH, pad)
	}
	w := 1280*boxW/100 - 2*1280*pad/100
	h := 720*boxH/100 - 2*1280*pad/100
	if math.Ceil(w) != imageBoxW || math.Ceil(h) != imageBoxH {
		t.Errorf("CSS から計算した箱 %.1fx%.1f が定数 %dx%d と合わない", w, h, imageBoxW, imageBoxH)
	}
}

// TestImageBombStopsBeforeDecode は、ファイルは小さいが縦横の値が巨大な画像を、ピクセル全体を確保する前に止めることを確かめる。
// 20000x20000 のグレーは展開すると 400MB になる (ゼロ埋めの PNG は 400KB ほどに縮む)。
func TestImageBombStopsBeforeDecode(t *testing.T) {
	dir := t.TempDir()
	func() { // 作った画像はこの中で手放し、測る前の GC で回収させる
		f, err := os.Create(filepath.Join(dir, "bomb.png"))
		must(t, err)
		defer func() { must(t, f.Close()) }()
		must(t, png.Encode(f, stdimage.NewGray(stdimage.Rect(0, 0, 20000, 20000))))
	}()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := loadShowImage("s.json", "bomb.png", filepath.Join(dir, "bomb.png"))
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "大きすぎる") {
		t.Fatalf("巨大な画像を止めなかった: %v", err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Errorf("止めるまでに %d MB 確保した (本体を展開してから縦横を見ている)", got>>20)
	}
}

// TestJPEGOrientationVariants は、マーカーの前の詰め物 (0xFF) を挟んでも向きを読み、1〜8 以外の値は回さない (1) と読むことを確かめる。
func TestJPEGOrientationVariants(t *testing.T) {
	app1 := func(o uint16) []byte {
		tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(o), byte(o >> 8), 0, 0, 0, 0, 0, 0}
		seg := append([]byte("Exif\x00\x00"), tiff...)
		return append([]byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	}
	app0 := []byte{0xFF, 0xE0, 0, 4, 0, 0}
	eoi := []byte{0xFF, 0xD9}
	join := func(parts ...[]byte) []byte { return bytes.Join(append([][]byte{{0xFF, 0xD8}}, parts...), nil) }
	for _, tc := range []struct {
		name string
		b    []byte
		want int
	}{
		{"APP1 だけ", join(app1(6), eoi), 6},
		{"APP0 の後ろ", join(app0, app1(8), eoi), 8},
		{"詰め物の後ろ", join([]byte{0xFF}, app1(6), eoi), 6},
		{"APP0 と詰め物の後ろ", join(app0, []byte{0xFF, 0xFF}, app1(5), eoi), 5},
		{"不正な値", join(app1(9), eoi), 1},
		{"0", join(app1(0), eoi), 1},
		{"EXIF 無し", join(app0, eoi), 1},
	} {
		if got := jpegOrientation(tc.b); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
