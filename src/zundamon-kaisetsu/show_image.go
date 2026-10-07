package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	stdimage "image" // パッケージに定数 image (エンジンのイメージ名。engine.go) があるので別名にする
	_ "image/jpeg"   // stdimage.Decode で JPEG を読む
	_ "image/png"    // stdimage.Decode で PNG を読む
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 図 (show の image) を出す箱の大きさ (720p の舞台での px)。player.html の .stage-show (幅 46%・高さ 52%) から
// .show-image の padding (0.8cqw ≒ 10px) を両側で引いた値 (約 568.3 x 353.9 を切り上げた)。CSS との対応は
// TestImageBoxMatchesPlayerCSS が見る。
const (
	imageBoxW = 569
	imageBoxH = 354
	// imageMaxBytes は 1 枚の画像の上限。HTML に base64 で埋め込むので 1.33 倍になる (音声は 64kbps で 10 分あたり約 5MB)
	imageMaxBytes = 2 << 20
)

// 小さく視認性の低い図を入れない (issue 645 のユーザーの指示) ための範囲。
//   - 縮尺 (箱に収めたときの倍率) が 1 を超える (箱より小さい) と、player.html は引き伸ばさないので小さく表示されて読みにくく、
//     0.5 を下回る (箱の 2 倍を超える画像を縮める) と中の文字が読めなくなる
//   - 表示したときに箱の縦と横のそれぞれ半分以上を占めること。1 辺だけ箱に届く細い帯 (569x10 など) を止める
//     (縦横比でいうと、およそ 4:5 から 16:5 の間になる)
const (
	imageScaleMin = 0.5
	imageScaleMax = 1.0
	imageFillMin  = 0.5
)

func parseImage(path, at string, m map[string]any) (*showData, error) {
	if err := onlyKeys(path, at+" (image)", m, "type", "src", "alt"); err != nil {
		return nil, err
	}
	src, err := showField(path, at, m, "src", true, 1<<30)
	if err != nil {
		return nil, err
	}
	if imageMIME(src) == "" {
		return nil, fail("%s: %s.src は .png / .jpg / .jpeg の画像 (実際: %s)", path, at, pyStrRepr(src))
	}
	// alt は HTML の代替テキストで、手順 3 の整合チェックが図の中身を資料と突き合わせるときの手がかりにもなる
	alt, err := showField(path, at, m, "alt", true, keywordSubMax)
	if err != nil {
		return nil, err
	}
	// Src は実際の場所 (symlink を解決した絶対パス) にしておく。同じ画像を ./a.png と a.png のように別の書き方で指しても、
	// 図解の表で 1 つにまとまり、埋め込みが重複しない。ファイルはここでは読まない (synth / kana を画像の欠けで止めないため)
	return &showData{Type: "image", Src: imageRealPath(path, src), Alt: alt, srcWritten: src}, nil
}

// imageRealPath は台本に書いた src を、立ち絵の cast.*.faces (facesDir) と同じく台本の場所から解いた実際のパスにする。
func imageRealPath(scriptPath, src string) string {
	p := expandUser(src)
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(scriptPath), p)
	}
	return resolvePath(p)
}

func imageMIME(src string) string {
	switch strings.ToLower(filepath.Ext(src)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return ""
}

// embedShowImages は図の画像を読み、検査して data URI に置き換える (mermaid の図は PNG にしてから同じ検査に通す)。
// 立ち絵 (loadFaces) と同じく assemble の頭で呼ぶ。
func embedShowImages(s *Script, shows []showData) error {
	for i := range shows {
		sd := &shows[i]
		if sd.Type == "mermaid" {
			if err := embedMermaid(s.Path, sd); err != nil {
				return err
			}
			continue
		}
		if sd.Type != "image" {
			continue
		}
		uri, err := loadShowImage(s.Path, sd.srcWritten, sd.Src)
		if err != nil {
			return err
		}
		sd.Src = uri
	}
	return nil
}

func loadShowImage(scriptPath, written, p string) (string, error) {
	name := pyStrRepr(written)
	f, err := os.Open(p)
	if err != nil {
		return "", fail("%s: 図の画像 %s を読めない (%v)", scriptPath, name, err)
	}
	defer func() { _ = f.Close() }() // 読むだけなので close の失敗は結果に関係しない
	// 大きさは読む前に見て、読むときも上限 + 1 バイトで打ち切る (stat が大きさを返さない特殊ファイルでも読み続けない)
	if st, err := f.Stat(); err == nil && st.Size() > imageMaxBytes {
		return "", fail("%s: 図の画像 %s が大きすぎる (%d バイト。上限 %d)", scriptPath, name, st.Size(), imageMaxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, imageMaxBytes+1))
	if err != nil {
		return "", fail("%s: 図の画像 %s を読めない (%v)", scriptPath, name, err)
	}
	if len(b) > imageMaxBytes {
		return "", fail("%s: 図の画像 %s が大きすぎる (上限 %d バイト)", scriptPath, name, imageMaxBytes)
	}
	// 縦横はヘッダだけで読んで先に検査し、通った画像だけを本体まで読む。先に本体を読むと、ファイルは小さくても縦横の値が
	// 巨大な画像 (圧縮で 2MB に収まる 20000x20000 など) で、検査より前にピクセル全体を確保してしまう (2 周目の反証レビューの実測)
	cfg, format, err := stdimage.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", fail("%s: 図の画像 %s を画像として読めない (PNG か JPEG で書き出す。%v)", scriptPath, name, err)
	}
	if o := jpegOrientation(b); format == "jpeg" && o != 1 {
		// ブラウザは EXIF の向きに従って回すが、Go は回さない。検査した縦横と画面の縦横が食い違うので止める
		return "", fail("%s: 図の画像 %s が向きの情報 (EXIF の Orientation=%d) を持っている。向きを直して書き出し直す", scriptPath, name, o)
	}
	if err := checkImageScale(scriptPath, written, cfg.Width, cfg.Height); err != nil {
		return "", err
	}
	// 本体まで読む (ヘッダだけ正しく中身が壊れた画像は、ブラウザで壊れた画像のアイコンになる)。縦横は上で抑えてある
	if _, _, err := stdimage.Decode(bytes.NewReader(b)); err != nil {
		return "", fail("%s: 図の画像 %s を画像として読めない (PNG か JPEG で書き出す。%v)", scriptPath, name, err)
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg"}[format]
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// imageProblem は w x h の画像を図の箱に出したときの問題 (無ければ "")。画像の図と mermaid の図で案内の文面が違うので、
// 判定はここに寄せ、文面は呼び出し側が決める。
type imageProblem string

const (
	imageInvalid imageProblem = "invalid" // 大きさが 0 以下
	imageSmall   imageProblem = "small"   // どちらの辺も箱に届かない
	imageLarge   imageProblem = "large"   // 箱の 2 倍を超える
	imageThin    imageProblem = "thin"    // 表示すると箱の縦横どちらかの半分に届かない
)

func imageScaleProblem(w, h int) (imageProblem, float64, float64) {
	if w <= 0 || h <= 0 {
		return imageInvalid, 0, 0
	}
	scale := min(float64(imageBoxW)/float64(w), float64(imageBoxH)/float64(h))
	shownW, shownH := scale*float64(w), scale*float64(h)
	switch {
	case scale > imageScaleMax:
		return imageSmall, shownW, shownH
	case scale < imageScaleMin:
		return imageLarge, shownW, shownH
	case shownW < imageBoxW*imageFillMin || shownH < imageBoxH*imageFillMin:
		return imageThin, shownW, shownH
	}
	return "", shownW, shownH
}

// checkImageScale は w x h の画像を箱に収めたときの縮尺と、表示したときに箱を占める割合を見る。
func checkImageScale(scriptPath, src string, w, h int) error {
	name := pyStrRepr(src)
	problem, shownW, shownH := imageScaleProblem(w, h)
	switch problem {
	case imageInvalid:
		return fail("%s: 図の画像 %s の大きさが不正 (%dx%d)", scriptPath, name, w, h)
	case imageSmall:
		return fail("%s: 図の画像 %s (%dx%d) が小さすぎる。小さく表示されて読みにくいので、幅 %d か高さ %d 以上で書き出す",
			scriptPath, name, w, h, imageBoxW, imageBoxH)
	case imageLarge:
		return fail("%s: 図の画像 %s (%dx%d) が大きすぎる。半分より小さく縮められて文字が読めなくなるので、幅 %d・高さ %d 以下に収める"+
			" (中身が多すぎるなら図を分けるか、図を使わずにセリフで説明する)",
			scriptPath, name, w, h, int(imageBoxW/imageScaleMin), int(imageBoxH/imageScaleMin))
	case imageThin:
		return fail("%s: 図の画像 %s (%dx%d) が細長すぎる。表示すると %.0fx%.0f px で、図の箱 (%dx%d) の縦横それぞれ半分に届かない",
			scriptPath, name, w, h, shownW, shownH, imageBoxW, imageBoxH)
	}
	return nil
}

// jpegOrientation は JPEG の EXIF (APP1) にある Orientation を返す。無い・読めない・1〜8 以外 (ブラウザも無視する) ときは
// 1 (回さない) を返す。
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b) && b[i] == 0xFF; {
		if b[i+1] == 0xFF { // マーカーの前の詰め物 (0xFF は何個でも置ける)
			i++
			continue
		}
		marker := b[i+1]
		if marker == 0xD9 || marker == 0xDA { // EOI / SOS より後に APP1 は来ない
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return exifOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 0 || off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off:]))
	for k := 0; k < count; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation (SHORT)
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}
