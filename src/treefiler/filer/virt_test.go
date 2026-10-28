package filer

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// virt_test.go は巨大なフォルダの仮想化 (virt.go) が、全部を動かした場合と同じ画面を描くことを固定する。
// 正解役は同じ木・同じ時計で仮想化を切った Model (virtOverride < 0)。毎フレームの View を 1 文字残らず比べる。

// virtPair は同じ木を開いた 2 つの Model (仮想化あり / なし)。画面は低く (仮想化の窓が木より小さくなるように)、
// 仮想化の下限を小さくして 600 件のフォルダで仮想化させる。
type virtPair struct {
	t         *testing.T
	virt, ref *Model
	at        time.Time
	dir       string
	faintOK   bool   // 消えかけの薄いセルの違いを許す (sameButFaint)
	what      string // 落ちたときに出すそれまでの操作の列 (乱数のテスト)
}

func newVirtPair(t *testing.T) *virtPair {
	t.Helper()
	isolateConfig(t)
	// 木は 1 段深い所に作る: root を 1 段上げる (-) と親が見える。親に別のフォルダ (設定の一時フォルダ等) があると、その熱の色は
	// 裏の走査の結果が届く時刻で決まり、2 つの Model で揃わない (仮想化と関係なく食い違う)
	dir := filepath.Join(t.TempDir(), "up", "tree")
	big := filepath.Join(dir, "big")
	for i := range 600 {
		mustWriteAll(t, filepath.Join(big, fmt.Sprintf("f%04d.txt", i)))
	}
	mustWriteAll(t, filepath.Join(big, ".hidden"))
	mustWriteAll(t, filepath.Join(big, "f0300sub", "inner.txt")) // 開いたままの子フォルダ (遠くても配置する)
	mustWriteAll(t, filepath.Join(dir, "alpha", "x.txt"))
	mustWriteAll(t, filepath.Join(dir, "zeta.txt"))
	restampTree(t, filepath.Dir(dir))
	vp := &virtPair{t: t, dir: dir}
	for i, ov := range []int{40, -1} {
		m, err := New(dir, Options{Now: func() time.Time { return fixedNow }})
		if err != nil {
			t.Fatal(err)
		}
		m.virtOverride = ov
		m.Resize(100, 14)
		sub := m.loadPath(filepath.Join(big, "f0300sub", "inner.txt"))
		sub.parent.expanded = true
		cur := m.loadPath(filepath.Join(big, "f0100.txt"))
		for p := cur.parent; p != nil; p = p.parent {
			p.expanded = true
		}
		m.setCur(cur)
		settleBackground(t, m)
		m.Advance(fixedNow.Add(time.Hour)) // 裏の処理を待った回数は 2 つで違うので、同じ時刻へ揃えて止める
		m.snapAll()
		if i == 0 {
			vp.virt = m
		} else {
			vp.ref = m
		}
	}
	vp.at = fixedNow.Add(time.Hour + time.Second)
	vp.frames("起動", 50)
	return vp
}

// quiesce は両方の Model の裏の走査が止まるのを待つ。ファイルを書き換える前に呼ぶ: 走査の途中に作ったファイルの「今」の更新時刻を
// 拾うと、熱の色がどちらの Model で拾ったかで食い違う (走査は Advance の中でしか頼まれないので、待った後には始まらない)。
func (vp *virtPair) quiesce() {
	vp.t.Helper()
	idle := func(m *Model) bool {
		m.walker.mu.Lock()
		defer m.walker.mu.Unlock()
		return !m.walker.running
	}
	waitFor(vp.t, "裏の走査", func() bool { return idle(vp.virt) && idle(vp.ref) })
}

func mustWriteAll(t *testing.T, p string) {
	t.Helper()
	mkfile(t, p, strings.Repeat("x", 1+int(virtHash(p)%40))+"\n") // 大きさを子ごとにずらす (size の並びを名前順と違えるため。issue 720)
}

func virtHash(p string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(filepath.Base(p)))
	return h.Sum32()
}

// virtStamp は項目の mtime。fNNNN の名前のファイルは名前から決まる分だけ 1 時間前より古くする (modified の並びを名前順と違えるため)。
// 🚨 フォルダと、操作で消えないファイルはちょうど 1 時間前にそろえる: フォルダの熱の色は裏の走査が数えた「配下でいちばん新しい時刻」で決まり、
// 走査の結果が届く時刻は 2 つの Model で揃わない。いちばん新しい時刻を消えない項目に持たせると、どの時点の走査でも同じ値になる
// (fNNNN を消す・足すライブ更新の操作でも変わらない。ずらしたファイルにいちばん新しい時刻を持たせると、300 本の種で 4 本が色の値の差で落ちた)
func virtStamp(p string, fi os.FileInfo) time.Time {
	base := filepath.Base(p)
	if (fi != nil && fi.IsDir()) || !strings.HasPrefix(base, "f") {
		return fixedNow.Add(-time.Hour)
	}
	return fixedNow.Add(-time.Hour - time.Duration(1+virtHash(p)%3600)*time.Second)
}

// frames は両方を n フレーム (60fps) 進め、毎フレームの画面を比べる。
// quiet は両方の Model の裏の計算 (配下の走査・e の展開) が終わるまで待つ。結果は次の Advance で両方とも同じフレームに取り込まれる。
// 待たないと、結果が届くフレームが 2 つの Model で実時間の速さ次第にずれ、仮想化と関係なく画面が食い違う (r の後のステータスバーの
// 大きさが 1 フレームだけ違う。負荷の高いときや GOMAXPROCS=1 で出る)。
func (vp *virtPair) quiet() {
	vp.t.Helper()
	waitFor(vp.t, "裏の計算", func() bool { return computed(vp.virt) && computed(vp.ref) })
}

// computed は m の裏の計算が終わっているか (結果は取り込んでいなくてよい)。
func computed(m *Model) bool {
	m.walker.mu.Lock()
	running := m.walker.running
	m.walker.mu.Unlock()
	if running {
		return false
	}
	if j := m.exploding; j != nil {
		j.mu.Lock()
		defer j.mu.Unlock()
		return j.done
	}
	return true
}

func (vp *virtPair) frames(what string, n int) {
	vp.t.Helper()
	vp.same(what + " (キーの直後)")
	for i := range n {
		vp.at = vp.at.Add(16 * time.Millisecond)
		vp.quiet()
		vp.virt.Advance(vp.at)
		vp.ref.Advance(vp.at)
		vp.same(fmt.Sprintf("%s のフレーム %d", what, i+1))
	}
}

// gapFrames は両方を n 回、間隔 gap で進めて毎回比べる (1 回の進みは 0.05 秒で頭打ちになるので、50ms を超える間隔では動きが壁時計より
// 遅れる。重いフレームや glogx の遅い tick の形)。
func (vp *virtPair) gapFrames(what string, gap time.Duration, n int) {
	vp.t.Helper()
	for i := range n {
		vp.at = vp.at.Add(gap)
		vp.quiet()
		vp.virt.Advance(vp.at)
		vp.ref.Advance(vp.at)
		vp.same(fmt.Sprintf("%s の遅いフレーム %d", what, i+1))
	}
}

func (vp *virtPair) key(k string, n int) {
	vp.t.Helper()
	vp.virt.HandleKey(k)
	vp.ref.HandleKey(k)
	vp.frames("キー "+k, n)
}

// sameButFaint は same の緩い版: 違ってよいのは、全部を動かした側のセルがほぼ地の色 (透明度 1 割未満の消えかけ) の文字で、
// 仮想化した側がそのセルを地のまま・薄い文字・その下の線で描いたものだけ (縮んで消える子の消えかけの名前が重なったとき、上に来る方が
// 違う。virt.go の冒頭)。乱数の操作列のテストだけが使う (決まった操作列のテストは厳密に比べる)。
func (vp *virtPair) sameButFaint(what string) {
	vp.t.Helper()
	got, want := vp.virt.draw(), vp.ref.draw()
	for i, w := range want.cells {
		g := got.cells[i]
		if g == w {
			continue
		}
		// 全部を動かす側が消えかけの薄い文字を描いたセル: 仮想化した側は地のまま・薄い文字・その下の線のどれか
		if g.bg == cBg && w.bg == cBg && faint(w.fg) && (g.s == " " || faint(g.fg) || lineGlyph(g.s)) {
			continue
		}
		vp.t.Fatalf("%s%s: (%d,%d) が違う: 仮想化 %+v / 全部 %+v\n仮想化:\n%s\n全部:\n%s", vp.prefix(), what, i%want.w, i/want.w, g, w,
			strings.Join(got.plain(), "\n"), strings.Join(vp.ref.draw().plain(), "\n"))
	}
}

// prefix は落ちたときの見出しに付ける、それまでの操作の列。
func (vp *virtPair) prefix() string {
	if vp.what == "" {
		return ""
	}
	return "[" + vp.what + "] "
}

// lineGlyph は線の文字か (罫線の範囲と、線の端の点)。
func lineGlyph(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return (r >= 0x2500 && r <= 0x257F) || s == "·" || s == "." || s == "|" || s == "-" || s == "+" || s == "="
}

// faint は c が地の色から 1 割未満しか離れていないか。
func faint(c rgb) bool {
	for i := range c {
		if math.Abs(c[i]-cBg[i]) > 26 {
			return false
		}
	}
	return true
}

func (vp *virtPair) same(what string) {
	vp.t.Helper()
	if vp.faintOK {
		vp.sameButFaint(what)
		return
	}
	got, want := vp.virt.View(), vp.ref.View()
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return
	}
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] {
			vp.t.Logf("仮想化 %q\n全部   %q", got[i], want[i])
			vp.t.Fatalf("%s: %d 行目が違う\n仮想化:\n%s\n全部:\n%s", what, i, strings.Join(vp.virt.draw().plain(), "\n"), strings.Join(vp.ref.draw().plain(), "\n"))
		}
	}
	vp.t.Fatalf("%s: 行数が違う (%d / %d)", what, len(got), len(want))
}

// 前提: 仮想化した側は本当に一部の子しかアニメを持っていない (持っていなければ、比べても仮想化を試していない)。
func (vp *virtPair) assertVirtualized() {
	vp.t.Helper()
	if len(vp.virt.anims)*2 > len(vp.ref.anims) {
		vp.t.Fatalf("前提: 仮想化していない (アニメ %d / 全部 %d)", len(vp.virt.anims), len(vp.ref.anims))
	}
}

// 動く操作 (1 つずつ・10 ずつ・半ページ・端から端・巨大なフォルダの外へ出て戻る) の毎フレームが、全部を動かした場合と同じ。
// 端から端は、画面を横切る子が毎フレーム入れ替わる (描く直前に兄弟の動きを写す realizeVirtual の経路)。
func TestVirtualizedMovesMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.assertVirtualized()
	for _, k := range []string{"j", "j", "k", "J", "K", "ctrl+d", "ctrl+u", "G", "g", "j", "h", "j", "k", "l", "end", "home"} {
		vp.key(k, 45)
	}
	vp.assertVirtualized()
}

// 遠くで開いたままの子フォルダ (f0300sub) が画面へ入ってくる: 遠くても配置し、その子のブロックも配置している。
func TestVirtualizedFarOpenFolderMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for range 20 {
		vp.key("J", 20)
	}
	vp.key("j", 20) // f0300.txt の次
	if vp.virt.cur.raw != "f0300sub" {
		t.Fatalf("前提: f0300sub へ来ていない (%s)", vp.virt.cur.raw)
	}
	vp.frames("止まるまで", 45)
	if !strings.Contains(strings.Join(vp.virt.draw().plain(), "\n"), "inner.txt") {
		t.Fatal("前提: f0300sub の中身を描いていない")
	}
}

// キーで動きを打ち切った途中からの次の動き (動きの途中で次のキーが来る) も同じ。
func TestVirtualizedInterruptedMovesMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, k := range []string{"G", "g", "J", "G", "j", "j", "j"} {
		vp.key(k, 5)
	}
	vp.frames("止まるまで", 45)
}

// 読み直しで増えた・消えた子 (祖先の右端から湧く / 縮んで消える) と、隠しファイルの表示の切り替えも同じ。
func TestVirtualizedReloadAndHiddenMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	mustWriteAll(t, filepath.Join(big, "f0101a.txt")) // カーソルのすぐ下に増える
	if err := os.Remove(filepath.Join(big, "f0098.txt")); err != nil {
		t.Fatal(err)
	}
	// 熱の色は裏の走査の結果が届く時刻で変わる (2 つの Model で揃わない) ので、更新時刻を元のままにする
	stamp := fixedNow.Add(-time.Hour)
	for _, p := range []string{big, filepath.Join(big, "f0101a.txt")} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	vp.key("r", 45)
	vp.key(".", 45)
	vp.key(".", 45)
	vp.assertVirtualized()
}

// 端から端へ飛んでいる途中に、ライブ更新で子が増える: 増えた所より下の子は上の子より 1 行ぶん多くずれて動く。
// 画面を横切って入ってくる子は、同じ側の兄弟の動きを写さないと 1 行ずれる (virtBlock.nearest)。
func TestVirtualizedLiveInsertDuringJumpMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("G", 6)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	mustWriteAll(t, filepath.Join(big, "f0350a.txt"))
	stamp := fixedNow.Add(-time.Hour)
	for _, p := range []string{big, filepath.Join(big, "f0350a.txt")} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.applyChange(dirChange{dir: big, changed: []string{"f0350a.txt"}, listing: true})
	}
	vp.frames("増えた後", 45)
}

// 巨大なフォルダを畳む・開き直す (開いた直後は全部を動かす期間) も同じ。畳むと、画面から遠いので捨てていた子も縮んで消える
// (ghostCulled)。
func TestVirtualizedCollapseAndReopenMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("h", 45)
	vp.key("tab", 45) // 畳む
	vp.key("tab", 60) // 開き直す
	vp.key("l", 45)
	vp.assertVirtualized()
}

// 食い違いを再現した回帰テスト。

// 行の間隔を変える: 兄弟のずれ方が順位に比例する (「近い兄弟の動きを写す」が成り立たない) ので、全部を動かす期間に入り、
// 画面の外で捨てていた子は前の目標の位置から動く。
func TestVirtualizedRowSpacingMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.RowSpacing = 2
		m.applySettings("row_spacing")
	}
	vp.frames("行の間隔", 60)
	vp.key("G", 45)
	vp.assertVirtualized()
}

// 並びを変える (フォルダを上に): 順位がばらばらに変わる。
func TestVirtualizedResortMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.FoldersFirst = !m.set.FoldersFirst
		m.applySettings("folders_first")
	}
	vp.frames("フォルダを上に", 60)
	vp.key("j", 45)
	vp.assertVirtualized()
}

// 仮想化をやめる (子が閾値の半分を下回った): 捨てていた子は湧かずに前の位置に現れる。
func TestVirtualizedStopVirtualizingMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.assertVirtualized()
	vp.virt.virtOverride = 1000 // 閾値を上回っても、半分 (500) を上回る間は仮想化を続ける (閾値の前後を行き来しない)
	vp.frames("閾値を上げた", 3)
	vp.assertVirtualized()
	vp.virt.virtOverride = 1300 // 600 件は 1300 の半分を下回る
	vp.frames("仮想化をやめた", 45)
	if len(vp.virt.anims) != len(vp.ref.anims) {
		t.Fatalf("前提: 仮想化をやめていない (アニメ %d / 全部 %d)", len(vp.virt.anims), len(vp.ref.anims))
	}
	vp.key("G", 45)
}

// 画面の高さを変える (glogx は View の度に Resize する)。
func TestVirtualizedResizeMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.Resize(100, 30)
	}
	vp.frames("高くした", 45)
	vp.key("G", 45)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.Resize(100, 10)
	}
	vp.frames("低くした", 45)
	vp.key("g", 45)
}

// glogx の順 (キー → View → しばらく後の tick で Advance) で開き直す: 全部を動かす期間の起点は、キーの時点の古い時刻ではなく
// 次の Advance (holdClock)。
func TestVirtualizedGlogxOrderReopenMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("h", 45)
	vp.key("tab", 45)
	vp.at = vp.at.Add(5 * time.Second) // glogx は動きが止まると tick を止める
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.HandleKey("tab")
	}
	vp.same("開き直した直後の View")
	vp.at = vp.at.Add(33 * time.Millisecond)
	vp.frames("開き直す (glogx の順)", 60)
	vp.assertVirtualized()
}

// 検索 (列の全部の項目が対象。layoutAll) とその飛び先、root を 1 段上げる、カーソル以外を畳む (C) も同じ。
func TestVirtualizedSearchRerootCollapseOthersMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, k := range []string{"/", "f", "0", "5", "enter", "n", "n", "N"} {
		vp.key(k, 30)
	}
	vp.key("-", 60)
	vp.key("G", 45)
	vp.key("C", 60)
}

// 巨大なブロックが 2 つ同じ列に並ぶ (片方は丸ごと画面の外に押し出され、親からの肘だけが画面を縦に貫く)。描く順 (重なった
// ラベルの前後) と、ブロックの両端で決まる線が同じ。
func TestVirtualizedTwoBigBlocksMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	for i := range 300 {
		mustWriteAll(t, filepath.Join(vp.dir, "alpha", fmt.Sprintf("a%04d.txt", i)))
	}
	stamp := fixedNow.Add(-time.Hour)
	_ = filepath.Walk(vp.dir, func(p string, _ os.FileInfo, _ error) error { return os.Chtimes(p, stamp, stamp) })
	for _, k := range []string{"r", "h", "k", "tab", "l", "G", "h", "j", "l", "G"} {
		vp.key(k, 45)
	}
	vp.assertVirtualized()
}

// フォルダが飛び飛びにある中で並びを変える: 順位の付け方が変わるので全部を動かす期間に入る (virt.go noteGeo)。捨てていた遠くの
// フォルダは、前の配置での位置 (前の順位) から画面の中の新しい位置へ飛び込む (reviveAnim は前の順位で写し元を選ぶ)。
func TestVirtualizedResortManyFoldersMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	for i := 0; i < 600; i += 25 {
		mustWriteAll(t, filepath.Join(big, fmt.Sprintf("f%04ddir", i), "x.txt"))
	}
	stamp := fixedNow.Add(-time.Hour)
	_ = filepath.Walk(vp.dir, func(p string, _ os.FileInfo, _ error) error { return os.Chtimes(p, stamp, stamp) })
	vp.key("r", 45)
	vp.key("g", 45) // 先頭へ: 遠くのフォルダが飛び込む先 (先頭付近) が画面の中になり、動きの終わりにゆっくり画面へ入ってくる
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.FoldersFirst = !m.set.FoldersFirst
		m.applySettings("folders_first")
	}
	vp.frames("フォルダを上に", 60)
	vp.assertVirtualized()
}

// 食い違いを再現した回帰テスト。捨てていた子は、前の配置で近い兄弟から作り直す (reviveAnim)。

// 端から端へ飛んでいる途中に、飛び飛びの挿入 (ばらばらの変化) がライブ更新で来る: 捨てていた子は止まっていたのではなく、
// 兄弟と同じずれで飛んでいる途中。
func TestVirtualizedScrambleDuringJumpMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("G", 6)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	var names []string
	for i := 0; i < 600; i += 50 {
		n := fmt.Sprintf("f%04da.txt", i)
		mustWriteAll(t, filepath.Join(big, n))
		names = append(names, n)
	}
	restampTree(t, vp.dir)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.applyChange(dirChange{dir: big, changed: names, listing: true})
	}
	vp.frames("増えた後", 60)
}

// 端から端へ飛んでいる途中に行の間隔を変える。
func TestVirtualizedRowSpacingDuringJumpMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("G", 6)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.RowSpacing = 1
		m.applySettings("row_spacing")
	}
	vp.frames("行の間隔", 60)
}

// 遠くの閉じたフォルダ 1 つだけが並び替えで先頭へ飛ぶ (順位の差の変化は数回だが、1 つの子が大きく飛ぶ)。
func TestVirtualizedSingleFarFolderResortMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	mustWriteAll(t, filepath.Join(big, "f0450dir", "x.txt"))
	restampTree(t, vp.dir)
	vp.key("r", 45)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.FoldersFirst = !m.set.FoldersFirst
		m.applySettings("folders_first")
	}
	vp.frames("フォルダを上に", 60)
}

// 履歴だけで仮想化を続けているブロック (子が閾値の半分と閾値の間) が、検索 (layoutAll) を挟んでも仮想化を続ける。
func TestVirtualizedSearchKeepsHistoryMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.virt.virtOverride = 1000
	vp.frames("閾値を上げた", 3)
	vp.assertVirtualized()
	vp.key("G", 6)
	for _, k := range []string{"/", "f", "0", "5"} {
		vp.key(k, 1)
	}
	vp.assertVirtualized()
	vp.frames("検索の後", 45)
}

func restampTree(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, _ error) error { st := virtStamp(p, fi); return os.Chtimes(p, st, st) })
}

// 遠くで仮想化に捨てていた選択線の子 (big.last) が、覚えの作り直し (隠しファイルの表示) と同じ打鍵で選択線に乗る: 全部を動かす期間の
// 最初の描画 (Advance の前) で、選択線の縦棒が画面を貫くには、その子を前の位置に作り直す必要がある (realizeVirtual は全部を動かす
// 期間でも作り直す。乱数の操作列のテストの種 39 の形)。
func TestVirtualizedSpineKidRevivedInHoldMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, k := range []string{"g", "j", "h", "j"} { // big.last = f0001、カーソルは zeta (big は選択線の外で f0001 は遠い)
		vp.key(k, 45)
	}
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.HandleKey("k") // big へ (f0001 が選択線に乗る)
		m.HandleKey(".") // 覚えの作り直し → 全部を動かす期間
	}
	vp.frames("k と . の後", 45)
}

// カーソルが今の見せる子ではない項目 (読み直しで消えた) を指す: 仮想化した側は、古い順位でその項目を配置しない。検索が
// 消えた項目へカーソルを戻す形のように、カーソルが木の外を指す経路はありうる。ここではその状態を直接作る。
func TestVirtualizedCursorOutsideTreeMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	gone := map[*Model]*node{}
	for _, m := range []*Model{vp.virt, vp.ref} {
		gone[m] = m.cur // f0100.txt
	}
	if err := os.Remove(filepath.Join(big, "f0100.txt")); err != nil {
		t.Fatal(err)
	}
	restampTree(t, vp.dir)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.applyChange(dirChange{dir: big, removed: []string{"f0100.txt"}, listing: true})
		m.cur = gone[m] // 消えた項目を指させる
	}
	if vp.virt.inTree(vp.virt.cur) {
		t.Fatal("前提: カーソルが木の外を指していない")
	}
	vp.frames("消えた項目を指す", 60) // 全部を動かす期間 (読み直し) が終わって仮想化に戻った後まで
}

// 食い違いを再現した回帰テスト。

// 遅いフレーム (Advance の間隔が 50ms を超える) の間に行の間隔を変えると、動きは壁時計より遅れて進む: 全部を動かす期間は動きと
// 同じ時計 (animClock) で測る。壁時計で測ると、動きが途中のまま仮想化に戻り、順位に比例してずれた遠い兄弟を写した。
func TestVirtualizedSlowFramesRowSpacingMatchFull(t *testing.T) {
	for _, gap := range []time.Duration{16 * time.Millisecond, 100 * time.Millisecond, 747 * time.Millisecond} {
		t.Run(gap.String(), func(t *testing.T) {
			vp := newVirtPair(t)
			vp.key("K", 45)
			for _, m := range []*Model{vp.virt, vp.ref} {
				m.Resize(63, 3)
			}
			vp.frames("低く", 45)
			for _, m := range []*Model{vp.virt, vp.ref} {
				m.set.RowSpacing = 1
				m.applySettings("row_spacing")
			}
			vp.same("行の間隔 1 の直後")
			vp.gapFrames("行の間隔 1", gap, 6)
			for _, m := range []*Model{vp.virt, vp.ref} {
				m.Resize(83, 45)
			}
			vp.frames("高くした後", 45)
		})
	}
}

// 中身だけが変わるライブ更新 (ログへの追記。一覧は変わらない) では、全部を動かす期間に入らない (覚えの世代を変えない)。
func TestVirtualizedContentOnlyLiveUpdateStaysVirtual(t *testing.T) {
	vp := newVirtPair(t)
	big := filepath.Join(vp.dir, "big")
	for i := range 3 {
		vp.quiesce()
		mustWrite(t, filepath.Join(big, "f0105.txt"), strings.Repeat("x", i+2)+"\n")
		restampTree(t, vp.dir)
		for _, m := range []*Model{vp.virt, vp.ref} {
			m.applyChange(dirChange{dir: big, changed: []string{"f0105.txt"}})
		}
		for f := range 20 {
			vp.frames(fmt.Sprintf("中身の変更 %d", i+1), 1)
			if f == 0 || f == 19 {
				vp.assertVirtualized()
			}
		}
	}
}

// 縮んで消える子の回帰テスト (乱数の操作列 300 本で見つけた形を、厳密に比べる決まった操作列にしたもの)。どれも、低い画面で縮み始めて
// から画面を高くし、窓の外にいた子が縮みながら画面を横切るのを見る。

func (vp *virtPair) resize(w, h int) {
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.Resize(w, h)
	}
}

// 巨大なフォルダを畳む: 画面から遠いので捨てていた子も縮んで消える (ghostCulled のブロックが消えた側)。
func TestVirtualizedCollapseThenTallMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.key("h", 45)
	vp.resize(100, 8)
	vp.frames("低く", 45)
	vp.key("tab", 2) // 畳む
	vp.resize(100, 60)
	vp.frames("畳んで高くした", 45)
}

// ライブ更新で遠くの子がたくさん消える: 見せる子から外れた子も縮んで消える (ghostCulled の外れた側)。
func TestVirtualizedLiveRemoveThenTallMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.resize(100, 8)
	vp.frames("低く", 45)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	var removed []string
	for i := 130; i < 200; i += 3 {
		name := fmt.Sprintf("f%04d.txt", i)
		if err := os.Remove(filepath.Join(big, name)); err != nil {
			t.Fatal(err)
		}
		removed = append(removed, name)
	}
	restampTree(t, vp.dir)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.applyChange(dirChange{dir: big, removed: removed, listing: true})
	}
	vp.frames("消えた直後", 2)
	vp.resize(100, 120)
	vp.frames("消えて高くした", 45)
}

// カーソル以外を畳む (C) と、遠くの開いたフォルダ (f0300sub) も閉じ、中身 (inner.txt) が縮んで消える。行き先の f0300sub は遠くても
// 捨てない (cullFar)。捨てると inner.txt が big の右端へ向かって縮み、全部を動かす側と違う向きに動く。
func TestVirtualizedCollapseOthersFarFolderMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.set.FocusDim = 3 // 選択線の外の枝を暗くする (既定の 10 は暗くしないので、捨てずに置いた親の place.active の差が見えない)
	}
	vp.key("C", 2)
	vp.resize(100, 500) // f0300sub (カーソルから 200 行) まで画面に入れる
	vp.frames("C の後", 45)
}

// 食い違いを再現した回帰テスト。

// 読み直しで遠くの子が消えた直後、Advance の前に画面を高くする: 全部を動かす側は、次の step まで消えた子を前の位置に描く。
func TestVirtualizedRemovedDrawnUntilStepMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	if err := os.Remove(filepath.Join(big, "f0170.txt")); err != nil {
		t.Fatal(err)
	}
	restampTree(t, vp.dir)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.HandleKey("r")
		m.Resize(100, 170)
	}
	vp.frames("読み直して高くした", 45)
}

// Advance を挟まずに覚えを 2 回作り直す (glogx はキー → View → キー → View → tick): 1 回目に外れた子も縮んで消え、前の順位は
// 直前の step の世代のものが残る。
func TestVirtualizedDoubleRebuildMatchFull(t *testing.T) {
	vp := newVirtPair(t)
	vp.quiesce()
	big := filepath.Join(vp.dir, "big")
	if err := os.Remove(filepath.Join(big, "f0170.txt")); err != nil {
		t.Fatal(err)
	}
	restampTree(t, vp.dir)
	for _, m := range []*Model{vp.virt, vp.ref} {
		m.HandleKey("r")
		m.View() // 1 回目の作り直し
		m.HandleKey(".")
		m.View() // 2 回目の作り直し
		m.Resize(100, 170)
	}
	vp.frames("2 回作り直して高くした", 45)
}

// 全部を動かす期間のうちに 2 回読み直して遠くの子がたくさん外れても、仮想化に戻った後に外れた子の記録が残り続けない
// (残ると描く直前に毎フレーム舐める)。
func TestVirtualizedRemovedRecordIsDropped(t *testing.T) {
	vp := newVirtPair(t)
	big := filepath.Join(vp.dir, "big")
	remove := func(from, to int) {
		vp.quiesce()
		var names []string
		for i := from; i < to; i++ {
			name := fmt.Sprintf("f%04d.txt", i)
			if err := os.Remove(filepath.Join(big, name)); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		restampTree(t, vp.dir)
		for _, m := range []*Model{vp.virt, vp.ref} {
			m.applyChange(dirChange{dir: big, removed: names, listing: true})
		}
	}
	remove(500, 501)
	vp.frames("1 回目", 10) // まだ全部を動かす期間
	remove(200, 300)
	vp.frames("2 回目", 120) // 期間が終わって仮想化に戻る
	vp.assertVirtualized()
	if n := len(vp.virt.cur.parent.vis.removed); n != 0 {
		t.Fatalf("外れた子の記録が残っている (%d 件)", n)
	}
}

// 操作を乱数で並べて、全部を動かす Model と毎フレーム突き合わせる (仮想化の近似が本物と食い違う入力を、思いつく形に頼らずに探す。
// 同じ不変条件を別の入力で破られうるので、本物を正解役にした網羅の検査を置く)。種は固定 (落ちたら同じ列を再現できる)。
// 畳む動きの「消えかけの薄いセル」だけは違ってよい (sameButFaint)。
func TestVirtualizedRandomOpsMatchFull(t *testing.T) {
	if raceEnabled {
		t.Skip("-race の計装で 7 倍遅いので、-race なしの run で回す (Makefile の test)")
	}
	keys := []string{"j", "k", "J", "K", "G", "g", "ctrl+d", "ctrl+u", "h", "l", "tab", ".", "r", "/", "f", "0", "esc", "C"}
	frames := []int{1, 2, 5, 12, 45}
	seeds := int64(12) // 手元で広げるときは TREEFILER_VIRT_SEEDS=300 などで回す (CI は 12 本)
	if n, err := strconv.Atoi(os.Getenv("TREEFILER_VIRT_SEEDS")); err == nil && n > 0 {
		seeds = int64(n)
	}
	for seed := int64(1); seed <= seeds; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(uint64(seed), 712))
			vp := newVirtPair(t)
			vp.faintOK = true
			big := filepath.Join(vp.dir, "big")
			var log []string
			step := func(what string) {
				log = append(log, what)
				vp.what = strings.Join(log, " → ")
				switch rng.IntN(7) {
				case 0: // 遅いフレーム (重いフレーム・遅い tick。1 回の進みは 0.05 秒で頭打ち)
					vp.gapFrames(what, time.Duration(100+rng.IntN(900))*time.Millisecond, 1+rng.IntN(4))
				case 2: // 16ms より短い間隔 (同じ時刻の Advance も含む)
					vp.gapFrames(what, time.Duration(rng.IntN(8))*time.Millisecond, 5+rng.IntN(30))
				case 1: // Advance を挟まずに View だけ (glogx はキーの直後に描き、tick は後から来る)
					vp.same(what + " (View だけ)")
				default:
					vp.frames(what, frames[rng.IntN(len(frames))])
				}
			}
			speeds := []string{"slow", "normal", "fast", "instant"}
			for range 30 {
				switch op := rng.IntN(12); {
				case op == 11: // 並びを変える (Sort by の巡回か Reverse。issue 720)
					what := "並び Reverse"
					if rng.IntN(3) > 0 {
						vp.virt.HandleKey("S")
						vp.ref.HandleKey("S")
						what = "並び S → " + vp.ref.set.SortBy
					} else {
						for _, m := range []*Model{vp.virt, vp.ref} {
							m.set.Reverse = !m.set.Reverse
							m.applySettings("reverse")
						}
					}
					step(what)
				case op == 10:
					sp := speeds[rng.IntN(len(speeds))]
					for _, m := range []*Model{vp.virt, vp.ref} {
						m.set.Speed = sp
					}
					step("速さ " + sp)
				case op < 6:
					k := keys[rng.IntN(len(keys))]
					vp.virt.HandleKey(k)
					vp.ref.HandleKey(k)
					step("キー " + k)
				case op == 6: // ライブ更新で増える・消える (飛び飛びに何件か)
					vp.quiesce()
					var changed, removed []string
					for range 1 + rng.IntN(15) {
						i := rng.IntN(620)
						name := fmt.Sprintf("f%04d.txt", i)
						p := filepath.Join(big, name)
						if _, err := os.Stat(p); err == nil && rng.IntN(2) == 0 {
							_ = os.Remove(p)
							removed = append(removed, name)
						} else {
							name = fmt.Sprintf("f%04dz.txt", i)
							mustWriteAll(t, filepath.Join(big, name))
							changed = append(changed, name)
						}
					}
					restampTree(t, vp.dir)
					for _, m := range []*Model{vp.virt, vp.ref} {
						m.applyChange(dirChange{dir: big, changed: changed, removed: removed, listing: true})
					}
					step(fmt.Sprintf("ライブ更新 +%d -%d", len(changed), len(removed)))
				case op == 7:
					rs := rng.IntN(3)
					for _, m := range []*Model{vp.virt, vp.ref} {
						m.set.RowSpacing = rs
						m.applySettings("row_spacing")
					}
					step(fmt.Sprintf("行の間隔 %d", rs))
				case op == 8:
					for _, m := range []*Model{vp.virt, vp.ref} {
						m.set.FoldersFirst = !m.set.FoldersFirst
						m.applySettings("folders_first")
					}
					step("フォルダを上に")
				default:
					h := 8 + rng.IntN(25)
					for _, m := range []*Model{vp.virt, vp.ref} {
						m.Resize(100, h)
					}
					step(fmt.Sprintf("高さ %d", h))
				}
			}
		})
	}
}
