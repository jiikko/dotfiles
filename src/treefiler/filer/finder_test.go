package filer

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jiikko/dotfiles/src/testwait"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// waitFinder は / の走査を取り込み終えるまで Advance を回す。
func waitFinder(t *testing.T, m *Model) {
	t.Helper()
	testwait.Until(t, "ファイルを探す", func() bool {
		m.Advance(fixedNow)
		return m.finder.done
	})
}

func finderRels(m *Model) []string {
	var out []string
	for _, h := range m.finder.list() {
		out = append(out, h.p.rel)
	}
	return out
}

// 開いていないフォルダの中のファイルも候補に出て、Enter で途中のフォルダを開いてカーソルが移る。
func TestFinderFindsFilesInClosedFoldersAndRevealsThem(t *testing.T) {
	m := newTest(t)
	b := m.findNode(filepath.Join(m.root.path(), "b"))
	if b == nil || b.expanded {
		t.Fatal("前提: b は閉じている")
	}
	m.HandleKey("/")
	if !m.OwnsKeys() {
		t.Fatal("/ でポップアップを開いていない (OwnsKeys が偽)")
	}
	waitFinder(t, m)
	typeText(m, "x.txt")
	if got := finderRels(m); len(got) == 0 || got[0] != filepath.Join("b", "deep", "x.txt") {
		t.Fatalf("x.txt の候補 = %v (閉じた b の中の b/deep/x.txt が先頭のはず)", got)
	}
	m.HandleKey("enter")
	if want := filepath.Join(m.root.path(), "b", "deep", "x.txt"); m.cur.path() != want {
		t.Fatalf("Enter の後のカーソル = %s, want %s", m.cur.path(), want)
	}
	if !b.expanded || !m.cur.parent.expanded {
		t.Fatal("Enter で途中のフォルダ (b・b/deep) を開いていない")
	}
	if m.finder.active || m.OwnsKeys() {
		t.Fatal("Enter で移った後もポップアップが開いている")
	}
}

// 隠したフォルダの中は探し、.git の中と git が無視するフォルダの中には降りない (フォルダ自身は候補に出る)。
func TestFinderSearchesHiddenButNotGitOrIgnoredFolders(t *testing.T) {
	m := newTest(t)
	root := m.root.path()
	mkfile(t, filepath.Join(root, ".config", "app", "settings.toml"), "x\n")
	mkfile(t, filepath.Join(root, ".git", "objects", "packed"), "x\n")
	setGit(m, "!! b/\x00")
	m.HandleKey("/")
	waitFinder(t, m)
	rels := finderRels(m)
	for _, want := range []string{filepath.Join(".config", "app", "settings.toml"), ".hidden", ".git", "b"} {
		if !slices.Contains(rels, want) {
			t.Errorf("候補に %s が無い (隠したものも探す・降りないフォルダ自身は出す)", want)
		}
	}
	for _, not := range []string{filepath.Join(".git", "objects"), filepath.Join("b", "deep"), filepath.Join("b", "deep", "x.txt")} {
		if slices.Contains(rels, not) {
			t.Errorf("候補に %s がある (.git と git が無視する b の中には降りない)", not)
		}
	}
}

// 並びは、名前 (最後の要素) に一致したもの → パスの途中にしか一致しないもの。同じ段の中は浅い順。
func TestFinderRanksNameMatchesFirstThenShallow(t *testing.T) {
	m := newTest(t)
	root := m.root.path()
	mkfile(t, filepath.Join(root, "deep1", "match.txt"), "x\n")
	mkfile(t, filepath.Join(root, "matchdir", "zzz.txt"), "x\n")
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "match")
	want := []string{"matchdir", filepath.Join("deep1", "match.txt"), filepath.Join("matchdir", "zzz.txt")}
	if got := finderRels(m); !slices.Equal(got, want) {
		t.Fatalf("match の並び = %v, want %v", got, want)
	}
}

// 語を打ち替えると先頭の候補を選び直す。Tab / Shift-Tab で選ぶ候補を回し、Enter はその候補へ移る。
func TestFinderTabSelectsAndEnterMovesToSelected(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "file")
	rels := finderRels(m)
	if len(rels) < 2 {
		t.Fatalf("前提: file の候補が 2 つ以上 (%v)", rels)
	}
	m.HandleKey("tab")
	m.HandleKey("tab")
	m.HandleKey("shift+tab")
	if m.finder.sel != 1 {
		t.Fatalf("tab tab shift+tab の後の選択 = %d, want 1", m.finder.sel)
	}
	typeText(m, "1") // 打ち替えたら、前に選んでいた項目が残っていても先頭を選び直す
	if m.finder.sel != 0 {
		t.Fatalf("語を打ち替えた後の選択 = %d (先頭のはず)", m.finder.sel)
	}
	m.HandleKey("backspace")
	m.HandleKey("tab")
	m.HandleKey("enter")
	if want := filepath.Join(m.root.path(), rels[1]); m.cur.path() != want {
		t.Fatalf("Enter の後のカーソル = %s, want 2 番目の候補 %s", m.cur.path(), want)
	}
}

// Esc はカーソルを動かさずに閉じ、裏の走査を止める。候補の無い Enter は閉じない (打ち直せる)。
func TestFinderEscKeepsCursorAndEnterWithoutHitsStaysOpen(t *testing.T) {
	m := newTest(t)
	cur := m.cur
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "qqqzzz")
	if len(m.finder.list()) != 0 {
		t.Fatal("前提: qqqzzz に一致する候補は無い")
	}
	m.HandleKey("enter")
	if !m.finder.active {
		t.Fatal("候補の無い Enter でポップアップを閉じた")
	}
	m.HandleKey("esc")
	if m.finder.active || m.cur != cur {
		t.Fatalf("Esc の後: 開いている=%v・カーソル %s (動かないはず)", m.finder.active, m.cur.path())
	}
	m.HandleKey("/")
	scan := m.finder.scan
	m.Close() // glogx で F を閉じたとき
	if m.finder.active {
		t.Fatal("Close の後もポップアップが開いている")
	}
	scan.mu.Lock()
	canceled := scan.canceled
	scan.mu.Unlock()
	if !canceled {
		t.Fatal("Close で裏の走査を止めていない")
	}
	finderWG.Wait() // 止めた走査の goroutine が戻る (戻らなければテストの時間切れ)
}

// 辿る項目の数に上限を置き、超えたら止めて知らせる。走っている間は Busy (呼び出し側が Advance を回す)。
func TestFinderStopsAtLimitAndIsBusyWhileScanning(t *testing.T) {
	orig := finderMax
	finderMax = 3
	t.Cleanup(func() { finderMax = orig })
	m := newTest(t)
	m.set.Git = false // キーのたびの git の取得で Busy が立たないように (ポップアップの走査の分だけを見る)
	settleBackground(t, m)
	if m.Busy() {
		t.Fatal("前提: 開く前は裏の処理が無い")
	}
	m.HandleKey("/")
	if !m.Busy() {
		t.Fatal("辿っている間に Busy が偽 (呼び出し側が取り込みの Advance を回さない)")
	}
	waitFinder(t, m)
	if !m.finder.capped || len(m.finder.paths) != 3 {
		t.Fatalf("上限 3 で: 止めた=%v・取り込んだ %d 件", m.finder.capped, len(m.finder.paths))
	}
	if st := m.finderStatus(); !strings.Contains(st, "上限で止めた") {
		t.Fatalf("上限で止めたことを出していない: %q", st)
	}
	if m.finderScanning() {
		t.Fatal("取り込み終えた後も辿っている扱い")
	}
}

// 貼り付けは語に入り、↑ は t の検索と同じ履歴を辿る。
func TestFinderPasteAndHistory(t *testing.T) {
	m := newTest(t)
	m.HandleKey("t")
	typeText(m, "file10")
	m.HandleKey("enter")
	m.HandleKey("/")
	waitFinder(t, m)
	m.HandleKey("up")
	if got := m.finder.line.String(); got != "file10" {
		t.Fatalf("↑ の語 = %q (t の検索の履歴 file10 のはず)", got)
	}
	if rels := finderRels(m); len(rels) == 0 || rels[0] != "file10.txt" {
		t.Fatalf("履歴の語で引き直していない: %v", rels)
	}
	m.HandleKey("esc")
	m.HandleKey("/")
	waitFinder(t, m)
	m.Paste("two")
	if got := m.finder.line.String(); got != "two" || len(finderRels(m)) == 0 || finderRels(m)[0] != filepath.Join("a", "two.go") {
		t.Fatalf("貼り付け: 語 %q・候補 %v", got, finderRels(m))
	}
}

// 入力欄のキャレットは、ポップアップに描いた語の末尾の直後にある (描画と CaretPos が同じ幾何を使う)。
func TestFinderCaretFollowsDrawnInput(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "日本END")
	x, y, ok := m.CaretPos()
	if !ok {
		t.Fatal("ポップアップの入力欄にキャレットが無い")
	}
	row := m.draw().plain()[y]
	i := strings.Index(row, "END")
	if i < 0 {
		t.Fatalf("キャレットの行に入力が描かれていない: %q", row)
	}
	if want := termwidth.Of(row[:i]) + 3; x != want {
		t.Fatalf("キャレットの桁 = %d, want %d (描いた END の直後): %q", x, want, row)
	}
}

// 長いパスは頭を切って名前を残し、一致した文字に一致の地の色を付ける。狭すぎる画面では最下行に語と件数を出す。
func TestFinderDrawsLongPathTailWithMatchAndFallsBackWhenSmall(t *testing.T) {
	m := newTest(t)
	long := filepath.Join(strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), "target.txt")
	mkfile(t, filepath.Join(m.root.path(), long), "x\n")
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "target")
	c := m.draw()
	rows := c.plain()
	found := false
	for y, row := range rows {
		i := strings.Index(row, "target.txt")
		if i < 0 {
			continue
		}
		found = true
		if !strings.Contains(row, "▶ …") {
			t.Fatalf("長いパスの頭を … で切っていない・選択の印が無い: %q", row)
		}
		x := termwidth.Of(row[:i])
		for k := range len("target") {
			if p := c.at(x+k, y); p == nil || p.bg != cMatchBg {
				t.Fatalf("一致した %d 文字目に一致の地の色が無い: %q", k, row)
			}
		}
		if p := c.at(x+len("target"), y); p == nil || p.bg == cMatchBg {
			t.Fatalf("一致していない . に一致の地の色が付いた: %q", row)
		}
	}
	if !found {
		t.Fatalf("候補の行に target.txt が無い:\n%s", strings.Join(rows, "\n"))
	}
	m.Resize(30, 5)
	last := m.draw().plain()[4]
	if !strings.Contains(last, "/ target") || !strings.Contains(last, "画面が小さい") {
		t.Fatalf("狭い画面の最下行 = %q (語と、候補を出せない理由)", last)
	}
}

// 走査中に前の段へ一致が足されても、選んでいる (▶ の付いた) 候補を選び続ける。Tab で選んだときも、選ばずに先頭のままのときも
// (番号だけを持つと、ずれた先の見ていない候補へ Enter で移る)。
func TestFinderSelectionFollowsItemWhenEarlierTierGrows(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, "ile") // file10.txt・file2.txt は名前の途中の一致 (前方一致より後ろの段)
	m.HandleKey("tab")
	chosen := finderRels(m)[m.finder.sel]
	if m.finder.sel != 1 {
		t.Fatalf("前提: Tab で 2 番目を選ぶ (sel=%d)", m.finder.sel)
	}
	m.finder.match(&finderPath{rel: "ilex.txt", shown: "ilex.txt"}) // 取り込みで前の段 (名前の前方一致) に 1 件足されたのと同じ
	if got := m.finder.list()[m.finder.sel].p.rel; got != chosen {
		t.Fatalf("前の段に一致が足された後の選択 = %s, want Tab で選んだ %s", got, chosen)
	}
	m.HandleKey("backspace")
	typeText(m, "e") // 打ち替えて先頭 (Tab で選んでいない) を見ている
	seen := finderRels(m)[m.finder.sel]
	m.finder.match(&finderPath{rel: "ilez.txt", shown: "ilez.txt"})
	if got := m.finder.list()[m.finder.sel].p.rel; got != seen {
		t.Fatalf("Tab 前に前の段へ一致が足された後の選択 = %s, want 見ていた先頭の %s", got, seen)
	}
}

// 止める旗を、走査の goroutine が次のフォルダへ進む前に見て止まる (閉じた後に root の配下を辿り続けない)。
func TestFinderScanStopsWhenCanceled(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "a", "b", "c", "deep.txt"), "x\n")
	ready := make(chan *finderScan, 1)
	var once sync.Once
	s := startFinderScan(root, 100, func(string) bool {
		once.Do(func() { (<-ready).cancel() }) // root の直下の a を見たところで止める (2 回目からは待たない)
		return false
	})
	ready <- s
	testwait.Until(t, "止めた走査", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.done
	})
	found, _, _ := s.take()
	for _, p := range found {
		if strings.Count(p.rel, string(filepath.Separator)) > 0 {
			t.Fatalf("止めた後も a の中まで辿った: %v", found)
		}
	}
	if len(found) != 1 || found[0].rel != "a" {
		t.Fatalf("止める前に見つけたのは root の直下の a だけのはず: %v", found)
	}
}

// ポップアップを出す最小の幅を切ると最下行に落とし、そのときもキャレットは描いた語の末尾の直後に置く
// (glogx は CaretPos で「入力中か」を見る。返さないと、入力中の編集キーを別の意味に取る)。
func TestFinderNarrowFallsBackWithCaret(t *testing.T) {
	m := newTest(t)
	m.Resize(finderMinW+4, 20)
	m.HandleKey("/")
	waitFinder(t, m)
	if _, ok := m.finderRect(); !ok {
		t.Fatal("前提: 最小の幅ちょうどでポップアップを出す")
	}
	m.Resize(finderMinW+3, 20)
	typeText(m, strings.Repeat("x", 40)+"日本END") // 窓 (幅の半分) より長い語: 頭が流れて末尾が見える
	x, y, ok := m.CaretPos()
	last := m.draw().plain()[m.h-1]
	i := strings.Index(last, "END")
	if !ok || y != m.h-1 || !strings.HasPrefix(last, "/ ") || i < 0 || x != termwidth.Of(last[:i])+3 {
		t.Fatalf("最下行に落ちたときのキャレット = (%d, %d, %v)、最下行 %q (描いた END の直後のはず)", x, y, ok, last)
	}
}

// 窓より長い語でキャレットを頭へ戻しても、語は入力の窓の中で切れて、件数と右の枠を潰さない。
func TestFinderLongQueryStaysInsideFrame(t *testing.T) {
	m := newTest(t)
	m.Resize(finderMinW+4, 20)
	m.HandleKey("/")
	waitFinder(t, m)
	typeText(m, strings.Repeat("y", 40))
	m.HandleKey("ctrl+a")
	r, _ := m.finderRect()
	row := m.draw().plain()[r.y+1]
	if !strings.HasSuffix(strings.TrimRight(termwidth.Cut(row, r.x+r.w), " "), "│") {
		t.Fatalf("入力欄の行の右の枠が無い (語がはみ出した): %q", row)
	}
	if !strings.Contains(row, "0/0") {
		t.Fatalf("件数が語に潰された: %q", row)
	}
}

// 語を打ち替えたら先頭を選び直す (前の語で選んでいた項目が、新しい並びの先頭でなくても残っていると、そちらを選び続けてしまう)。
func TestFinderRetypeSelectsTopEvenIfOldChoiceRemains(t *testing.T) {
	var f finderState
	f.paths = []finderPath{{rel: "k_m.txt", shown: "k_m.txt"}, {rel: "m_k.txt", shown: "m_k.txt"}}
	f.line.Insert("k")
	f.refilter()
	if got := f.list()[f.sel].p.rel; got != "k_m.txt" {
		t.Fatalf("前提: k の先頭は k_m.txt (%s)", got)
	}
	f.line.Reset()
	f.line.Insert("m")
	f.refilter()
	hits := f.list()
	if len(hits) != 2 || hits[0].p.rel != "m_k.txt" {
		t.Fatalf("前提: m の並びは m_k.txt が先頭で k_m.txt も残る (%v)", hits)
	}
	if got := hits[f.sel].p.rel; got != "m_k.txt" {
		t.Fatalf("打ち替えた後の選択 = %s, want 新しい先頭の m_k.txt", got)
	}
}
