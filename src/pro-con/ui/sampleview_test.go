package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// sampleForm は W2 に見本つきの選択肢の質問を載せて回答フォームを開く (issue 495)。
// 問 1: 点 (見本 dots.ans・文字) / 点字 (見本なし) / 棒 (見本 bar.png・画像) / 表 (見本 table.pdf・画像でも文字でもない)。
// 問 2: 赤 (見本の名前はあるが添付が無い) / 青 (見本 long.ans・板より長い 100 行)。
// dots.ans は新しい版と古い版の 2 件があり (記録の並びは新しい方が先。並び順でなく時刻で選ぶことを見る)、新しい方には色と OSC 52 が混ざる。
func sampleForm(t *testing.T) (*Model, *[][]string) {
	t.Helper()
	be := newSpy()
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("old.ans", "古い見本\n")
	wide := strings.Repeat("あ", 5) + strings.Repeat("x", 300) + "右端"
	cur := write("cur.ans", "\x1b[31m新しい見本の赤い行\x1b[0m\x1b]52;c;cHduZWQ=\x07\n二行目\n"+wide+"\n")
	var long strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&long, "行%03d\n", i)
	}
	longPath := write("long.ans", long.String())
	w, err := card.AskWait("スピナーの形", []card.Question{
		{Question: "形", Header: "形", Options: []card.Option{
			{Label: "点", Sample: "dots.ans", Recommended: true},
			{Label: "点字"},
			{Label: "棒", Sample: "bar.png"},
			{Label: "表", Sample: "table.pdf"},
		}},
		{Question: "色", Options: []card.Option{{Label: "赤", Sample: "red.ans"}, {Label: "青", Sample: "long.ans"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := be.snap.Now
	be.snap.Cards[2].Wait = w
	be.snap.Cards[2].Attachments = []card.Attachment{
		{Path: cur, Name: "dots.ans", Kind: card.AttachText, At: now},
		{Path: old, Name: "dots.ans", Kind: card.AttachText, At: now.Add(-time.Minute)},
		{Path: filepath.Join(dir, "bar.png"), Name: "bar.png", Kind: card.AttachImage, At: now},
		{Path: filepath.Join(dir, "table.pdf"), Name: "table.pdf", Kind: card.AttachFile, At: now},
		{Path: longPath, Name: "long.ans", Kind: card.AttachText, At: now},
	}
	m := openForm(t, be)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	var opened [][]string
	m.openFiles = func(p []string) error { opened = append(opened, p); return nil }
	return m, &opened
}

// 選択肢の行で v を押すと、その案の見本 (同じ名前の一番新しい添付) を全幅の板で開く。色は残り、OSC 52 は落ちる。
// esc で閉じると、回答フォームの同じ位置へ戻る (送っていない・選択も変わらない)。
func TestSampleOpensLatestAttachmentAndReturnsToForm(t *testing.T) {
	m, _ := sampleForm(t)
	cur := m.form.cur
	press(m, "v")
	if !m.sample.open {
		t.Fatal("見本のある案の行で v を押しても見本の板が開かない")
	}
	out := m.render()
	plain := ansi.Strip(out)
	for _, want := range []string{"W2 の見本", "点", "(dots.ans)", "新しい見本の赤い行", "二行目"} {
		if !strings.Contains(plain, want) {
			t.Errorf("見本の板に %q が無い:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "古い見本") {
		t.Error("同じ名前の古い添付を映した (付け直した新しい方を映す)")
	}
	if !strings.Contains(out, "\x1b[31m") {
		t.Error("見本の色が落ちた")
	}
	if strings.Contains(out, "\x1b]52") {
		t.Error("見本の OSC 52 が画面に出る (端末のクリップボードを書き換えられる)")
	}
	if strings.Contains(plain, "W2 へ回答") {
		t.Error("見本の板の下から回答フォームが透けている (全幅の板で置き換える)")
	}
	if !slices.Contains(m.hints(), "esc / v フォームへ戻る") {
		t.Errorf("案内が見本の板のものでない: %v", m.hints())
	}
	press(m, "esc")
	if m.sample.open || m.mode != modeForm || m.form.cur != cur {
		t.Fatalf("esc で回答フォームの同じ位置へ戻らない: open=%v mode=%v cur=%d (want %d)", m.sample.open, m.mode, m.form.cur, cur)
	}
	if !strings.Contains(screen(m), "W2 へ回答") {
		t.Fatal("見本を閉じたのに回答フォームが見えない")
	}
}

// 見本の板を開いている間のキーは板が受け取り、下の回答フォームへ漏れない (space で選択が変わらない・j でカーソルが動かない・enter で送らない)。
func TestSampleKeysDoNotLeakToForm(t *testing.T) {
	m, _ := sampleForm(t)
	cur, radio := m.form.cur, slices.Clone(m.form.radio)
	press(m, "v")
	pressForm(m, "space", "j", "k")
	press(m, "enter")
	if !m.sample.open {
		t.Fatal("板のキーで見本の板が閉じた")
	}
	if m.form.cur != cur || !slices.Equal(m.form.radio, radio) || m.mode != modeForm {
		t.Fatalf("板のキーが回答フォームへ漏れた: cur %d→%d radio %v→%v mode=%v", cur, m.form.cur, radio, m.form.radio, m.mode)
	}
}

// n / p は同じ問いの見本を持つ案だけを渡り (見本の無い「点字」を飛ばす)、回答フォームのカーソルも付いて行く。端では断って留まる。
func TestSampleStepsBetweenOptionsAndMovesFormCursor(t *testing.T) {
	m, opened := sampleForm(t)
	press(m, "v")
	if cmd := press(m, "n"); cmd != nil { // 次の見本は画像の「棒」: Preview に渡す
		m.Update(cmd())
	}
	if len(*opened) != 1 || !strings.HasSuffix((*opened)[0][0], "bar.png") {
		t.Fatalf("n で次の案 (棒) の画像の見本を開かない: %v", *opened)
	}
	if r := m.form.row(); r.q != 0 || r.opt != 2 {
		t.Fatalf("n でフォームのカーソルが棒へ付いて行かない: %+v", r)
	}
	if !m.sample.open || m.sample.opt != 2 || !strings.Contains(screen(m), "Preview で開いた") {
		t.Fatalf("n で画像の案へ移ったのに、板が前の案の見本を映したまま: open=%v opt=%d\n%s", m.sample.open, m.sample.opt, screen(m))
	}
	m.sample = sampleView{} // 画像は板を開かないので、点から開き直して端を試す
	pressForm(m, "k", "k")
	press(m, "v")
	press(m, "p")
	if !m.sample.open || m.sample.opt != 0 {
		t.Fatalf("先頭の案で p を押したら留まるはず: open=%v opt=%d", m.sample.open, m.sample.opt)
	}
}

// 画像の見本は板を開かずに Preview へ渡す (o と同じ。既定のアプリには渡さない)。
func TestSampleImageOpensInPreview(t *testing.T) {
	m, opened := sampleForm(t)
	pressForm(m, "j", "j") // 棒
	cmd := press(m, "v")
	if cmd == nil {
		t.Fatal("画像の見本で何も起きない")
	}
	m.Update(cmd())
	if m.sample.open {
		t.Error("画像の見本で板を開いた")
	}
	if len(*opened) != 1 || !strings.HasSuffix((*opened)[0][0], "bar.png") {
		t.Fatalf("Preview に渡したもの %v", *opened)
	}
}

// 名前はあるが添付がまだ無い見本は、板に「まだ添付されていない」を出す。見本の無い案では v を断り、案内にも v を出さない。
func TestSampleMissingAndAbsent(t *testing.T) {
	m, _ := sampleForm(t)
	if !slices.Contains(m.hints(), "v 見本") {
		t.Errorf("見本のある案の行で案内に v が無い: %v", m.hints())
	}
	pressForm(m, "j") // 点字 (見本なし)
	if slices.Contains(m.hints(), "v 見本") {
		t.Errorf("見本の無い案の行で案内に v がある: %v", m.hints())
	}
	press(m, "v")
	if m.sample.open {
		t.Fatal("見本の無い案で見本の板が開いた")
	}
	pressForm(m, "tab") // 問 2 の赤 (添付が無い)
	press(m, "v")
	if !m.sample.open || !strings.Contains(screen(m), "まだ添付されていない") {
		t.Fatalf("添付の無い見本で知らせが出ない:\n%s", screen(m))
	}
}

// 見本のある選択肢には [見本] の印が付き、見本の無い選択肢には付かない。
func TestSampleMarkerOnOptions(t *testing.T) {
	m, _ := sampleForm(t)
	var marked []string
	for _, l := range m.form.formLines(76) {
		if s := ansi.Strip(l.text); strings.Contains(s, "[見本]") {
			marked = append(marked, s)
		}
	}
	if len(marked) != 5 || !strings.Contains(marked[0], "点") || !strings.Contains(marked[1], "棒") || !strings.Contains(marked[2], "表") ||
		!strings.Contains(marked[3], "赤") || !strings.Contains(marked[4], "青") {
		t.Fatalf("[見本] の印の付き方が違う: %q", marked)
	}
}

// 端末より広い見本は l / h で横にずらして読める (右端まで届き、左へ戻せる。ずらしすぎない)。
func TestSampleScrollsHorizontally(t *testing.T) {
	m, _ := sampleForm(t)
	press(m, "v")
	if strings.Contains(screen(m), "右端") {
		t.Fatal("前提: 幅 100 の画面では広い行の右端は見えないはず")
	}
	for range 60 {
		press(m, "l")
	}
	if !strings.Contains(screen(m), "右端") {
		t.Fatalf("l で右端まで届かない (left=%d wide=%d)", m.sample.left, m.sample.wide)
	}
	if max := m.sample.wide - m.sampleBodyWidth(); m.sample.left > max {
		t.Fatalf("ずらしすぎ: left=%d (上限 %d)", m.sample.left, max)
	}
	for range 60 {
		press(m, "h")
	}
	if m.sample.left != 0 || !strings.Contains(screen(m), "新しい見本の赤い行") {
		t.Fatalf("h で左端へ戻らない: left=%d", m.sample.left)
	}
}

// 時刻が同じ見本が 2 件あるときは、記録の後ろ (後から載った方) を採る。
func TestLatestAttachmentTieTakesLater(t *testing.T) {
	at := time.Unix(1000, 0)
	c := card.Card{Attachments: []card.Attachment{{Path: "/a", Name: "x", At: at}, {Path: "/b", Name: "x", At: at}, {Path: "/c", Name: "y", At: at.Add(time.Hour)}}}
	if a, ok := latestAttachment(c, "x"); !ok || a.Path != "/b" {
		t.Fatalf("時刻が同じなら後ろを採るはず: %+v ok=%v", a, ok)
	}
}

// 板より長い見本は縦に送れる: j で 1 行・G で末尾・g で先頭へ。space の半ページは滑走し、コマを回し切ると止まって、その位置に着く。
func TestSampleScrollsVertically(t *testing.T) {
	m, _ := sampleForm(t)
	pressForm(m, "tab", "j") // 問 2 の青 (long.ans)
	press(m, "v")
	top := func() string { // 板の本文の先頭の行 (見出しと区切りの 2 行の下)
		ls := strings.Split(screen(m), "\n")
		return strings.TrimSpace(ls[m.headerRows()+2])
	}
	if !strings.HasPrefix(top(), "行001") {
		t.Fatalf("開いた直後の先頭が 1 行目でない: %q", top())
	}
	press(m, "j")
	if !strings.HasPrefix(top(), "行002") {
		t.Fatalf("j で 1 行送らない: %q", top())
	}
	press(m, "G")
	if s := screen(m); !strings.Contains(s, "行100") || strings.Contains(s, "行002") {
		t.Fatalf("G で末尾へ行かない:\n%s", s)
	}
	press(m, "g")
	// 時刻で終わる他の演出 (開いたときのカーソルの滑走など) を終わらせ、残る動きを見本の板の滑走だけにする
	clk := &clock{t: time.Now().Add(time.Hour)}
	m.now = clk.now
	for range 100 { // 回っていたコマを止め切る (止まっている間でないと、次の動きで tick を回し始めたかを見られない)
		if !m.framing {
			break
		}
		m.onFrame()
	}
	if _, cmd := m.Update(formKey("space")); cmd == nil {
		t.Fatal("space の半ページでコマの tick を回し始めない (滑走が画面で進まない)")
	}
	if !m.animating() {
		t.Fatal("space の半ページで見本の板の滑走が始まらない (コマが回らない)")
	}
	for range 100 {
		if !m.animating() {
			break
		}
		m.onFrame()
	}
	if m.animating() {
		t.Fatal("半ページの滑走がコマを回しても止まらない")
	}
	if want := fmt.Sprintf("行%03d", m.sample.pager.Offset+1); m.sample.pager.Offset == 0 || !strings.HasPrefix(top(), want) {
		t.Fatalf("滑走の後に送った位置へ着いていない: offset=%d 先頭=%q", m.sample.pager.Offset, top())
	}
}

// 見本の板は esc のほか q と v でも閉じてフォームへ戻る (案内の「esc / v フォームへ戻る」)。
func TestSampleClosesWithQAndV(t *testing.T) {
	for _, k := range []string{"q", "v"} {
		m, _ := sampleForm(t)
		press(m, "v")
		press(m, k)
		if m.sample.open || m.mode != modeForm {
			t.Errorf("%s で見本の板が閉じてフォームへ戻らない: open=%v mode=%v", k, m.sample.open, m.mode)
		}
	}
}

// 画像でも文字でもない見本 (pdf 等) は中身を読まず外のアプリにも渡さず、パスだけを出す (開くと動くファイルを動かさない)。
func TestSampleOtherKindShowsPathOnly(t *testing.T) {
	m, opened := sampleForm(t)
	pressForm(m, "j", "j", "j") // 表 (table.pdf)
	if cmd := press(m, "v"); cmd != nil {
		m.Update(cmd())
	}
	if len(*opened) != 0 {
		t.Fatalf("pdf の見本を外のアプリに渡した: %v", *opened)
	}
	if s := screen(m); !m.sample.open || !strings.Contains(s, "ここでは映せない") || !strings.Contains(s, "table.pdf") {
		t.Fatalf("pdf の見本でパスの知らせが出ない:\n%s", s)
	}
}

// 板を開いたまま端末を広げたら、ずらしすぎた分を戻す (右が空いたまま残さない)。
func TestSampleClampsScrollOnResize(t *testing.T) {
	m, _ := sampleForm(t)
	press(m, "v")
	for range 60 {
		press(m, "l")
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	s := screen(m)
	if max := m.sample.wide - m.sampleBodyWidth(); m.sample.left > max {
		t.Fatalf("広げた後もずらしすぎのまま: left=%d (上限 %d)", m.sample.left, max)
	}
	if want := fmt.Sprintf("← %d 桁ずらしている", m.sample.left); !strings.Contains(s, want) {
		t.Fatalf("広げた直後の見出しが本文と違う桁数を出している (want %q):\n%s", want, strings.Split(s, "\n")[m.headerRows()])
	}
}

// フォームを開いた後にカードが記録から消えたら (取り下げ・書庫)、「まだ添付されていない」ではなくカードが無いことを出す。
func TestSampleCardGoneSaysSo(t *testing.T) {
	m, _ := sampleForm(t)
	snap := m.snap
	snap.Cards = slices.DeleteFunc(slices.Clone(snap.Cards), func(c card.Card) bool { return c.ID == "W2" })
	m.setSnap(snap)
	press(m, "v")
	s := screen(m)
	if !m.sample.open || !strings.Contains(s, "W2 はもう記録に無い") || strings.Contains(s, "まだ添付されていない") {
		t.Fatalf("カードが消えた後の知らせが違う:\n%s", s)
	}
}
