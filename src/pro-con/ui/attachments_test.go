package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// attachModel は W1 に画像・文字・ファイルの添付を 1 件ずつ付けた詳細のモデル。外のアプリは開かずに、渡したパスを控える。
func attachModel(t *testing.T) (*Model, *clock, *[][]string, card.Attachment) {
	t.Helper()
	m, be, clk := drawerModel(t)
	dir := t.TempDir()
	text := filepath.Join(dir, "screen.ans")
	// 色 (SGR) は残し、クリップボードを書き換える OSC 52 は落とす
	if err := os.WriteFile(text, []byte("\x1b[31m赤い行\x1b[0m\n\x1b]52;c;aGk=\x07二行目\n\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	img := card.Attachment{Path: filepath.Join(dir, "a.png"), Name: "a.png", Note: "詳細の見た目", Kind: card.AttachImage, At: be.snap.Now}
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "W1" {
			be.snap.Cards[i].Attachments = []card.Attachment{
				img,
				{Path: text, Name: "screen.ans", Kind: card.AttachText, At: be.snap.Now},
				{Path: filepath.Join(dir, "b.pdf"), Name: "b.pdf", Kind: card.AttachFile, At: be.snap.Now},
			}
		}
	}
	m.setSnap(be.Poll())
	var opened [][]string
	m.openFiles = func(p []string) error { opened = append(opened, p); return nil }
	return m, clk, &opened, img
}

// 詳細 (enter) に添付の一覧 (時刻・種類・一言) が出て、文字の添付は中身がそのまま出る (色は残し、ほかの制御は落とす)。
func TestDrawerShowsAttachments(t *testing.T) {
	m, clk, _, _ := attachModel(t)
	open(t, m, clk)
	body := strings.Join(m.drawerBody(), "\n")
	plain := ansi.Strip(body)
	for _, want := range []string{"添付", "画像  詳細の見た目", "文字  screen.ans", "赤い行", "二行目", "ファイル  b.pdf"} {
		if !strings.Contains(plain, want) {
			t.Errorf("詳細に %q が無い:\n%s", want, plain)
		}
	}
	if !strings.Contains(body, "\x1b[31m") {
		t.Error("文字の添付の色が落ちた")
	}
	if strings.Contains(body, "\x1b]52") {
		t.Error("文字の添付の OSC 52 が詳細に出る (端末のクリップボードを書き換えられる)")
	}
	if !slices.Contains(m.hints(), "o 添付を開く") {
		t.Errorf("案内に o が無い: %v", m.hints())
	}
}

// o は画像の添付だけを Preview に渡す (文字の添付は詳細に出ている。ほかのファイルは開くと動くものがあるので渡さない)。詳細を開いたままでも効く。
func TestOpenKeyOpensImagesOnly(t *testing.T) {
	m, clk, opened, img := attachModel(t)
	open(t, m, clk)
	cmd := press(m, "o")
	if cmd == nil {
		t.Fatal("o で何も起きない")
	}
	m.Update(cmd())
	want := []string{img.Path}
	if len(*opened) != 1 || !slices.Equal((*opened)[0], want) {
		t.Fatalf("開いたもの %v (want [%v])", *opened, want)
	}
	if !m.showDetail {
		t.Fatal("o で詳細が閉じた")
	}
}

// 開ける添付が無いカードでは o を断り、外のアプリを起こさない。案内の o は暗い。
func TestOpenKeyRefusesWithoutAttachments(t *testing.T) {
	m, clk, opened, _ := attachModel(t)
	m.selected = "W2"
	press(m, "enter")
	clk.t = clk.t.Add(drawerDuration)
	m.onFrame()
	if cmd := press(m, "o"); cmd != nil {
		m.Update(cmd())
	}
	if len(*opened) != 0 {
		t.Fatalf("添付の無いカードで開いた: %v", *opened)
	}
	if slices.Contains(m.hints(), "o 添付を開く") {
		t.Error("開けないのに案内の o が明るい")
	}
}
