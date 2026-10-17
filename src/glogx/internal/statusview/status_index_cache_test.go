package statusview

import (
	"strconv"
	"strings"
	"testing"
)

// oldDisplayIndex は **メモ化前の実装をそのまま書き写した独立オラクル**。
//
// 🚨 検証対象の rebuildDisplayIndex を呼んではいけない。当初そう書いていたため、
// 両辺が同じ関数由来になり `at[i] = len(index)` のバグが相殺されて
// **cursorAt を全滅させる変異 (at[i] = 0) でも PASS した** (2026-08-14 の R2 レビューが実証)。
// cursorAt がずれると「画面のカーソル行とスクロール位置が別の行を指す」という
// ユーザーに見える不具合になるので、ここは独立に組む価値がある。
//
// 元実装は 1 パスで、cursorAt をループ内の `if i == v.cursor` で拾っていた
// (メモ化後は idxAt を引く形なので、両者の等価性がこのオラクルの主張)。
func oldDisplayIndex(rows []worktreeRow, cursor int) (index []statusDisplayLine, cursorAt int) {
	index = make([]statusDisplayLine, 0, len(rows)+4)
	for _, sec := range []worktreeSection{sectionStaged, sectionUnstaged, sectionUntracked, sectionConflicted} {
		n := 0
		for _, r := range rows {
			if r.section == sec {
				n++
			}
		}
		if n == 0 {
			continue
		}
		index = append(index, statusDisplayLine{sec: sec, row: -1, n: n})
		for i, r := range rows {
			if r.section != sec {
				continue
			}
			if i == cursor {
				cursorAt = len(index)
			}
			index = append(index, statusDisplayLine{sec: sec, row: i})
		}
	}
	return index, cursorAt
}

// メモを返す経路が、毎回作り直した場合と完全に同じ結果を返すこと。
// カーソルを全行に振って cursorAt も突き合わせる (cursorAt を O(1) 化した箇所の等価性)。
func TestDisplayIndexMemoMatchesFreshBuild(t *testing.T) {
	for _, files := range []int{0, 1, 2, 3, 40, 201} {
		v := statusRowsFixture(t, files)
		for cur := -1; cur <= files; cur++ {
			v.cursor = cur
			gotIdx, gotAt := v.displayIndex()
			wantIdx, wantAt := oldDisplayIndex(v.rows, v.cursor)
			if len(gotIdx) != len(wantIdx) {
				t.Fatalf("files=%d cursor=%d: 行数が違う %d != %d", files, cur, len(gotIdx), len(wantIdx))
			}
			for i := range wantIdx {
				if gotIdx[i] != wantIdx[i] {
					t.Fatalf("files=%d cursor=%d 行 %d: %+v != %+v", files, cur, i, gotIdx[i], wantIdx[i])
				}
			}
			if gotAt != wantAt {
				t.Fatalf("files=%d cursor=%d: cursorAt が違う %d != %d", files, cur, gotAt, wantAt)
			}
		}
	}
}

// rows が差し替わったらメモが無効になること (これが壊れると stage/unstage の後に
// 古い行構成を返し、画面が実際の作業ツリーと食い違う)。
func TestDisplayIndexCacheInvalidatesWhenRowsReplaced(t *testing.T) {
	v := statusRowsFixture(t, 4)
	before, _ := v.displayIndex()
	beforeLen := len(before)

	// 外部で 2 件増えた状態を受け取る (production と同じ経路)
	recs := []string{"## master...origin/master"}
	for i := range 6 {
		recs = append(recs, "M  src/pkg"+strconv.Itoa(i)+"/f.go")
	}
	v.Receive(StatusLoadMsg{st: parseWorktreeStatus(strings.Join(recs, "\x00") + "\x00"), gen: v.gen})

	after, _ := v.displayIndex()
	if len(after) == beforeLen {
		t.Fatalf("rows が差し替わったのにメモを返している (行数 %d のまま)", len(after))
	}
	wantIdx, _ := oldDisplayIndex(v.rows, v.cursor)
	if len(after) != len(wantIdx) {
		t.Fatalf("無効化後の行数が正解と違う: %d != %d", len(after), len(wantIdx))
	}
}

// セクション構成が変わる差し替え (件数が同じでも無効になること)。
// 🚨 件数だけを鍵にすると素通りする形。裏の配列の同一性で見ているので通る。
func TestDisplayIndexCacheInvalidatesWhenSectionsChangeAtSameCount(t *testing.T) {
	v := statusRowsFixture(t, 4) // staged 2 / unstaged 2 = 見出し 2 + 行 4 = 6 行
	before, _ := v.displayIndex()

	// 同じ 4 件だが全部 staged (見出しが 1 つになる = 5 行)
	recs := []string{"## master...origin/master"}
	for i := range 4 {
		recs = append(recs, "M  src/pkg"+strconv.Itoa(i)+"/f.go")
	}
	v.Receive(StatusLoadMsg{st: parseWorktreeStatus(strings.Join(recs, "\x00") + "\x00"), gen: v.gen})

	after, _ := v.displayIndex()
	if len(after) == len(before) {
		t.Fatalf("件数が同じセクション構成の変化でメモが無効になっていない (%d 行のまま)", len(after))
	}
}

// カーソル移動では作り直さないこと (memo の狙いそのもの)。
func TestDisplayIndexNoRebuildOnCursorMove(t *testing.T) {
	v := statusRowsFixture(t, 40)
	first, _ := v.displayIndex()
	// 返ったスライスの裏の配列が変わらない = 作り直していない
	for cur := range 40 {
		v.cursor = cur
		got, at := v.displayIndex()
		if len(got) == 0 || len(first) == 0 {
			t.Fatal("index が空")
		}
		if &got[0] != &first[0] {
			t.Fatalf("cursor=%d で行構成を作り直している (カーソル移動では作り直さないはず)", cur)
		}
		if got[at].row != cur {
			t.Fatalf("cursor=%d の cursorAt=%d が指す行が違う: %+v", cur, at, got[at])
		}
	}
}

// 🚨 既知の限界を明示する characterization test。
//
// メモの有効性は「rows の裏の配列の同一性」で見ているので、**同じ配列を in-place で
// 書き換える**とメモは古いまま返る。現状 rows は receive で丸ごと差し替えるだけなので
// この経路は production に存在しないが、将来 in-place の書き換えを足すなら
// displayIndex の無効化も直す必要がある — それをこのテストが示す。
//
// このテストが落ちたら「in-place 書き換えを足した」か「無効化の方式を変えた」かの
// どちらかなので、status_view.go の idxRows のコメントごと見直すこと。
func TestDisplayIndexCacheStaleOnInPlaceMutation(t *testing.T) {
	v := statusRowsFixture(t, 4)
	before, _ := v.displayIndex()
	// 同じ配列のまま section を書き換える (production には無い経路)
	for i := range v.rows {
		v.rows[i].section = sectionStaged
	}
	after, _ := v.displayIndex()
	if len(after) != len(before) {
		t.Fatalf("in-place 書き換えでメモが無効になった (%d -> %d)。"+
			"無効化の方式が変わったか、in-place 経路が production に入った。"+
			"status_view.go の idxRows のコメントを見直すこと", len(before), len(after))
	}
}
