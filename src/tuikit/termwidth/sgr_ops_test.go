package termwidth

import (
	"strings"
	"testing"
)

// glogx の render_test.go / width_test.go から移した (実装が glogx から移ったため)。

// DropColumns は左 N 桁を捨て、cut 前の SGR を replay して右側を復元する
// (overlayCenteredBox の右背景合成に使う。Cut の鏡像)。
func TestDropColumns(t *testing.T) {
	// 素の ASCII: 先頭 N 桁を落とす
	if got := DropColumns("abcdef", 2); got != "cdef" {
		t.Errorf(`DropColumns("abcdef",2)=%q; want "cdef"`, got)
	}
	// n<=0 は素通し
	if got := DropColumns("abc", 0); got != "abc" {
		t.Errorf(`DropColumns("abc",0)=%q; want "abc"`, got)
	}
	// n が内容末尾以降なら空
	if got := DropColumns("abc", 5); got != "" {
		t.Errorf(`DropColumns("abc",5)=%q; want ""`, got)
	}
	// ANSI: cut より前の色コードを replay し、残り suffix は色を保つ
	got := DropColumns("\x1b[32m"+"abcdef"+"\x1b[0m", 2)
	if !strings.HasPrefix(got, "\x1b[32m") {
		t.Errorf("cut 前の色が replay されていない: %q", got)
	}
	if StripSGR(got) != "cdef" {
		t.Errorf("suffix の内容がずれた: %q (plain=%q)", got, StripSGR(got))
	}
	// 全角グリフが cut をまたぐ場合: そのグリフを落とし空白で列 n に揃える
	// "あい" は各幅 2。n=1 は 'あ'(列0-1) をまたぐ → 'あ' を落とし 1 空白 + 'い'
	if got := DropColumns("あい", 1); got != " い" {
		t.Errorf(`DropColumns("あい",1)=%q; want " い"`, got)
	}
}

// Clip と Cut の**実際の差**を固定する。
//
// なぜ必要か (issue 107): 両者の doc は長らく「Clip は色を落とす / Cut は
// 保持する」と対比していたが、Clip が ansi.Truncate へ移った時点で**どちらも色を保持
// する**ようになり、記述だけが旧実装のまま残った。読んだ人が「色を残したいから」という理由で
// 誤った方を選ぶ状態だったので、差 (= `…` の有無) を実行で pin する。
func TestClipVsCutDifference(t *testing.T) {
	const colored = "\x1b[31mABCDEF\x1b[0m"

	clipped := Clip(colored, 3)
	kept := Cut(colored, 3)

	// 1. どちらも SGR を保持する (ここが緩むと「色を落とす」時代へ逆戻り)
	if !strings.Contains(clipped, "\x1b[31m") {
		t.Errorf("Clip が色を落とした: %q", clipped)
	}
	if !strings.Contains(kept, "\x1b[31m") {
		t.Errorf("Cut が色を落とした: %q", kept)
	}

	// 2. 差は `…` の有無だけ
	if !strings.Contains(clipped, "…") {
		t.Errorf("Clip は切り詰めを `…` で示すはず: %q", clipped)
	}
	if strings.Contains(kept, "…") {
		t.Errorf("Cut は `…` を付けないはず (覆い先とつながって見える): %q", kept)
	}
}

// ansi.Truncate は自分で reset を足さない (入力にあった SGR を持ち越すだけ) ことを固定する。
// Cut の doc 「末尾に reset は付かない。開いた色は呼び出し側が閉じる」の根拠。
func TestCutDoesNotCloseOpenStyle(t *testing.T) {
	open := "\x1b[31mABCDEF" // 色を開いたまま閉じない入力
	got := Cut(open, 3)
	if strings.Contains(got, "\x1b[0m") {
		t.Fatalf("開いた色を勝手に閉じている (呼び出し側の境界処理と二重になる): %q", got)
	}
	closed := Cut("\x1b[31mABCDEF\x1b[0m", 3)
	if !strings.Contains(closed, "\x1b[0m") {
		t.Fatalf("入力にあった reset が持ち越されていない: %q", closed)
	}
}

// DropColumns の列不変条件を、**ansi と uniseg で幅が食い違う文字**で pin する (issue 112)。
//
// 合成側は「n 桁落とせば表示幅も n 減る」を当てにして浮動ボックスを背景行に重ねる。
// clusterWidth が 2 本目の幅エンジン (uniseg) を使っていた頃、この不変条件はインド系文字と
// Arabic format 文字で崩れ、ボックス右側の背景が 1〜3 桁ずれていた (silent)。
// 🚨 ASCII / CJK / 絵文字では ansi と uniseg が一致するため、それらだけで測っても検出できない。
func TestDropColumnsWidthInvariantWhereEnginesDisagree(t *testing.T) {
	for _, in := range []string{
		"ಕಾ ಕಾ commit",       // カンナダ語: ansi=1 / uniseg=2 のクラスタ
		"؀ arabic sign",      // Arabic number sign: ansi=0 / uniseg=1
		"கா கா தமிழ் commit", // タミル語
		"ASCII only commit",  // 対照 (エンジンが一致する側)
		// 🚨 全角を含む入力を必ず 1 本入れる: 全角境界で切ると DropColumns の straddle 分岐
		//   (空白で埋め直す経路) を通る。ここが無いと、その分岐を丸ごと削っても本テストは
		//   green のまま通る (敵対的レビューで実測)
		"日本語の コミット 12345",
		// SGR を含む入力 (色を replay する経路を通す)
		"\x1b[31mred\x1b[0m ಕಾ \x1b[32mgreen\x1b[0m",
		// 🚨 **分割器**の食い違いで壊れていた rune (issue 124)。幅を 1 本にしても
		//   (issue 112)、クラスタ境界が 2 本あると不変条件は閉じない。
		//   U+0897 / U+1ACF / U+113B8 / U+1E5EE は Unicode 16 で追加され、uniseg v0.4.7 (15.0)
		//   は前の文字と結合せず別クラスタとして返す = 「クラスタ幅の総和 ≠ 全体の幅」。
		//   これらを 1 つも含まないと、分割を uniseg へ戻す変更が green のまま通る。
		//   **範囲の総当たり**は termwidth 側 (TestFirstClusterWidthsSumToOf) が持つ:
		//   根の不変条件「クラスタ幅の総和 = 全体の幅」を守れば、それを当てにする
		//   この経路も閉じる。ここは消費者側の代表ケースだけ置く
		"ax\u0897z", "a\u1acfb", "a\u113b8b", "a\u1e5eeb", "\u10efa x",
	} {
		total := Of(in)
		for n := 0; n <= total; n++ {
			if got := Of(DropColumns(in, n)); got != total-n {
				t.Fatalf("列不変条件が崩れた: %q n=%d → 幅 %d (期待 %d)", in, n, got, total-n)
			}
		}
	}
}

// DropColumns は全角グリフが cut をまたぐとき空白で列を揃える (overlay 合成の整合)。
// 絵文字クラスタも幅 2 の 1 単位として扱う。
func TestDropColumnsStraddleAndCluster(t *testing.T) {
	// 日(幅2) が列 1 をまたぐ: 日 を落とし 1 空白で列 1 に揃え、残りを継ぐ
	if got := DropColumns("日本X", 1); got != " 本X" {
		t.Errorf("DropColumns(\"日本X\", 1) = %q; want %q", got, " 本X")
	}
	// ⚠️(幅2) が列 1 をまたぐ: クラスタごと落として 1 空白 + 残り
	if got := DropColumns("⚠️x", 1); got != " x" {
		t.Errorf("DropColumns(\"⚠️x\", 1) = %q; want %q", got, " x")
	}
	// 列 0 は素通り / 内容末尾以降は空
	if DropColumns("abc", 0) != "abc" || DropColumns("abc", 10) != "" {
		t.Error("DropColumns の境界 (n<=0 / n>=幅) が壊れている")
	}
}
