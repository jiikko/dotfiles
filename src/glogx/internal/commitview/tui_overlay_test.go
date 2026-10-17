package commitview

import (
	"glogx/internal/gitdata"
	"glogx/internal/statusview"
	"glogx/internal/term"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
)

// diff ポップアップの本文にスクロールバー列が出ても枠が崩れない (全行が同じ表示幅) ことと、
// 全行が収まるときはバー列が出ないこと (TestJobDetailBoxLinesScrollbar の diff 版。
// term.WithScrollbar の単体テストではなく描画経路 boxLines で幅の均一性を見る)。
func TestDiffBoxLinesScrollbar(t *testing.T) {
	diffLines := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "diff line"
		}
		return out
	}
	const width, rows = 50, 8
	commit := &gitdata.Commit{SHA: "a", ShortSHA: "abc1234", Subject: "subject"}
	o := NewDiffOverlay()

	// 溢れる: バー列あり + 幅は均一 + thumb は比率
	o.Sha = commit.SHA
	o.Cache.Store(commit.SHA, diffLines(50), commit.SHA)
	o.Pager.Offset = 20
	box := o.BoxLines(width, false, "", commit, rows)
	uniformWidth(t, box)
	// 影付き箱: 末尾 2 行は下辺 (▖▁▗+影) と下端影なので本文から除く。本文行の行末は
	// 右影 1 桁 (NO_COLOR は ░/▒) が付く
	body := box[1 : len(box)-2]
	thumbs := 0
	for i, l := range body {
		shade := layout.ShadowMono
		if i == 0 {
			shade = layout.ShadowMonoEdge
		}
		trimmed := strings.TrimSuffix(strings.TrimSuffix(l, shade), " "+layout.BorderLight.V)
		switch {
		case strings.HasSuffix(trimmed, layout.ScrollbarThumb):
			thumbs++
		case strings.HasSuffix(trimmed, layout.ScrollbarTrack):
		default:
			t.Fatalf("本文行 %d にバー列が無い: %q", i, l)
		}
	}
	if thumbs == 0 || thumbs == len(body) {
		t.Fatalf("thumb 行数 = %d (本文 %d 行) — 比率になっていない", thumbs, len(body))
	}

	// colored でも幅は均一 (SGR 入りの影・バー列が幅計算を狂わせない)
	uniformWidth(t, o.BoxLines(width, true, "", commit, rows))

	// 収まる: バー列なし (本文幅が戻る)。枠幅は溢れる場合と同じ
	o.Cache.Store(commit.SHA, diffLines(rows-2), commit.SHA)
	o.Pager.Offset = 0
	fit := o.BoxLines(width, false, "", commit, rows)
	if w := uniformWidth(t, fit); w != term.DispWidth(term.StripANSI(box[0])) {
		t.Fatalf("収まる場合の枠幅 = %d, 溢れる場合 = %d", w, term.DispWidth(term.StripANSI(box[0])))
	}
	for i, l := range fit[1 : len(fit)-2] {
		if strings.Contains(l, layout.ScrollbarThumb) {
			t.Fatalf("収まるのに thumb が出ている (行 %d): %q", i, l)
		}
	}
}

// pager overlay の描画は offset を「窓 (total-rows)」へ再クランプすること。offset はキー処理側
// (term.PagerScrollKey 等) がその時点の rows でクランプするが、G で末尾へ行った後にリサイズで rows が
// 増えると保存済み offset が窓の上限を越えたまま残る。描画側で clampScrollOffset を通さないと、
// 広がった分の行を使わず末尾数行だけの箱になる (status viewer の pagerBox と同じ規律)。
func TestPagerOverlayReclampsOffsetOnRedraw(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	const width, rows = 50, 8 // 期待窓: start=42 → タイトル [43-50/50]

	commit := &gitdata.Commit{SHA: "a", ShortSHA: "abc1234", Subject: "subject"}
	d := NewDiffOverlay()
	d.Sha = commit.SHA
	d.Cache.Store(commit.SHA, lines, commit.SHA)
	d.Pager.Offset = 49 // 旧 rows で G → リサイズで rows が増えた後を想定 (窓上限 42 を越えている)
	if title := d.BoxLines(width, false, "", commit, rows)[0]; !strings.Contains(title, "[43-50/50]") {
		t.Errorf("diff overlay が offset を窓へ再クランプしていない: title=%q, want [43-50/50]", title)
	}

	// 🚨 描画 (title) だけでなく **論理 offset フィールドが収束したか**も見る。描画は毎回
	// clamp するので title は書き戻しが無くても正しく、上スクロールだけが死ぬ (k / up /
	// ctrl+p は max(offset-n, 0) しか見ないため rows_new - rows_old 打鍵ぶん無反応。
	// 実測 2026-08-21: diff 33 打鍵 / job 詳細 11 打鍵)。title だけの検査ではこの退行が緑になる。
	if d.Pager.Offset != 42 {
		t.Errorf("diff overlay が論理 offset を書き戻していない: offset=%d, want 42 (上スクロールが死ぬ)", d.Pager.Offset)
	}

	j := NewJobDetailOverlay()
	j.Open = true
	j.Cache.Store("key", lines, "key")
	j.Pager.Offset = 49
	if title := j.BoxLines(width, false, "", "job", "key", rows)[0]; !strings.Contains(title, "[43-50/50]") {
		t.Errorf("job 詳細 overlay が offset を窓へ再クランプしていない: title=%q, want [43-50/50]", title)
	}
	if j.Pager.Offset != 42 {
		t.Errorf("job 詳細 overlay が論理 offset を書き戻していない: offset=%d, want 42", j.Pager.Offset)
	}

	// status viewer の全画面 diff も同じ規律 (pagerRows = page - 5 なので page 13 で rows 8)
	sv := newTestStatusView(t, "")
	sv.Fixture().SetPager("k", "t")
	sv.Fixture().Preview().Store("k", lines, "k")
	sv.Fixture().Pager().Offset = 49
	if title := sv.Fixture().PagerBox(statusview.StatusRenderOpts{Width: width, Page: rows + 5})[0]; !strings.Contains(title, "[43-50/50]") {
		t.Errorf("status viewer の pager が offset を窓へ再クランプしていない: title=%q, want [43-50/50]", title)
	}
	if sv.Fixture().Pager().Offset != 42 {
		t.Errorf("status viewer の pager が論理 offset を書き戻していない: offset=%d, want 42", sv.Fixture().Pager().Offset)
	}
}

// 回帰 (レビュー確定 medium): close→reopen 連打で in-flight の PR 取得を二重発火しない。
func TestPRStatusOverlayNoDoubleFetch(t *testing.T) {
	o := NewPRStatusOverlay()
	if !o.Open("A") {
		t.Fatal("初回 open が fetch を要求しない")
	}
	o.Close() // 応答前に閉じる (busy[A] は保持されるべき)
	if o.Open("A") {
		t.Fatal("in-flight 中の reopen が二重 fetch を要求した")
	}
	o.Receive("A", &gitdata.PRStatus{PRRef: gitdata.PRRef{Number: 1}}, nil) // 応答 → busy 解除 + cache
	o.Close()
	if o.Open("A") {
		t.Fatal("cache 済みなのに再 fetch を要求した")
	}
}
