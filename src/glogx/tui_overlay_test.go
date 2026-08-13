package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
	"glogx/usage"
)

func TestBrowseCopyURL(t *testing.T) {
	copied := stubClipboard(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	// コミット一覧では commit URL
	m.handleKey("y")
	wantCommit := "https://github.com/o/r/commit/" + m.commits[0].SHA
	if *copied != wantCommit {
		t.Errorf("commit URL = %q; want %q", *copied, wantCommit)
	}
	// job フォーカス中はその job の URL
	m.openPanel()
	m.handleKey("j")
	m.handleKey("y")
	if *copied != "https://github.com/o/r/runs/1" {
		t.Errorf("job URL = %q", *copied)
	}
	if !m.toast.ok || !strings.Contains(m.toast.text, "コピーしました") {
		t.Errorf("コピー成功トーストが出ていない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	// URL なし job (job1) は失敗トースト
	m.handleKey("j")
	m.handleKey("y")
	if m.toast.ok || !strings.Contains(m.toast.text, "コピーできる URL がありません") {
		t.Errorf("URL なしの失敗トーストが出ていない: %q ok=%v", m.toast.text, m.toast.ok)
	}
}

func TestBrowseBatchPRsFeedPCache(t *testing.T) {
	// 一括取得の PR は p キーのキャッシュとコミット行バッジの両方に合流する
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{shas: shas, batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateSuccess},
		Details:  map[string][]CheckDetail{},
		PRs:      map[string]*PRRef{shas[0]: {Number: 7, URL: "https://github.com/o/r/pull/7", State: "MERGED"}},
	}})
	opened := stubBrowser(t)
	// 再取得なしで即 open
	_, cmd := m.handleKey("p")
	if cmd == nil || m.prBusy[shas[0]] {
		t.Fatalf("バッチ由来の PR キャッシュで即 open にならない")
	}
	cmd()
	if *opened != "https://github.com/o/r/pull/7" {
		t.Errorf("URL = %q", *opened)
	}
	// バッジも View に出る
	if !strings.Contains(m.View().Content, "#7") {
		t.Errorf("PR バッジが View に出ていない:\n%s", m.View().Content)
	}
}

func TestBrowseOpenPR(t *testing.T) {
	opened := stubBrowser(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	// p → 取得 Cmd (実行はしない) と busy
	_, cmd := m.handleKey("p")
	if cmd == nil || !m.prBusy[sha] {
		t.Fatalf("p で PR 取得が始まらない (cmd=%v busy=%v)", cmd, m.prBusy[sha])
	}
	// 取得結果 (PR あり) → ブラウザで開く Cmd が返る
	_, cmd = m.Update(prMsg{sha: sha, pr: &PRRef{Number: 12, URL: "https://github.com/o/r/pull/12", State: "OPEN"}})
	if cmd == nil {
		t.Fatalf("prMsg (PR あり) で open Cmd が返らない")
	}
	runCmdTree(cmd) // open Cmd は maybeTick と Batch されるので再帰実行する
	if *opened != "https://github.com/o/r/pull/12" {
		t.Errorf("開いた URL = %q", *opened)
	}
	if !m.toast.ok || !strings.Contains(m.toast.text, "PR #12") {
		t.Errorf("PR を開く成功トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	// キャッシュ済みなので 2 回目の p は再取得せず即 open
	_, cmd = m.handleKey("p")
	if cmd == nil || m.prBusy[sha] {
		t.Errorf("キャッシュ済み PR で即 open にならない")
	}
}

// 取得中にカーソルが動いたら、届いた PR は反映 (キャッシュ・バッジ) だけして自動オープン
// しない。離れたコミットの PR がいきなりブラウザで開く回帰の防止 (2026-07-29)。
func TestBrowsePRResultAfterCursorMoveDoesNotOpen(t *testing.T) {
	opened := stubBrowser(t)
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	if _, cmd := m.handleKey("p"); cmd == nil {
		t.Fatal("p で PR 取得が始まらない")
	}
	m.handleKey("j") // 取得中にカーソル移動
	_, cmd := m.Update(prMsg{sha: sha, pr: &PRRef{Number: 7, URL: "https://github.com/o/r/pull/7", State: "OPEN"}})
	runCmdTree(cmd)
	if *opened != "" {
		t.Errorf("カーソル移動後の遅延 PR がブラウザで開いた: %q", *opened)
	}
	if strings.Contains(m.toast.text, "開きます") {
		t.Errorf("stale な結果のトーストが出た: %q", m.toast.text)
	}
	if pr, ok := m.prCache[sha]; !ok || pr == nil || pr.Number != 7 {
		t.Errorf("キャッシュ/バッジへの反映まで捨てられた: %v", m.prCache[sha])
	}
	// stale なエラーも警告を出さない (無関係な失敗 notice を被せない)
	m.prBusy[sha] = true
	m.Update(prMsg{sha: sha, ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if m.lastWarning != "" {
		t.Errorf("stale なエラーで警告が出た: %q", m.lastWarning)
	}
}

// detail/basis/jobDetail の一過性エラーは、後続の成功結果で hint 警告がクリアされる
// (set-only だとセッション中ずっと張り付いていた・レビュー C4)。
func TestBrowseGhErrClearedOnSuccess(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	// パネル対象の detail 取得が一過性エラー → 警告が出る
	m.panelSHA = sha
	m.Update(detailMsg{sha: sha, batch: emptyBatch(), ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if m.ghErr == nil || !strings.Contains(m.hintLine(), "取得に失敗") {
		t.Fatalf("detail エラーで警告が出ない: ghErr=%v hint=%q", m.ghErr, m.hintLine())
	}
	// 成功 detail が届くと警告はクリアされる (set-only なら残ってしまう)
	m.Update(detailMsg{sha: sha, batch: emptyBatch(), ghErr: nil})
	if m.ghErr != nil {
		t.Fatalf("成功 detail 後も ghErr が残っている: %v", m.ghErr)
	}
	// basis / jobDetail も同様に成功でクリアする
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom2"}
	m.Update(basisMsg{batch: emptyBatch(), ghErr: nil})
	if m.ghErr != nil {
		t.Errorf("成功 basis 後も ghErr が残っている: %v", m.ghErr)
	}
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom3"}
	m.Update(jobDetailMsg{key: "k", lines: []string{"x"}, ghErr: nil})
	if m.ghErr != nil {
		t.Errorf("成功 jobDetail 後も ghErr が残っている: %v", m.ghErr)
	}
}

func TestBrowseOpenPRErrorNotCached(t *testing.T) {
	// 一時エラーは「PR なし」としてキャッシュしない (次の p で再試行できる)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.Update(prMsg{sha: sha, pr: nil, ghErr: &GHError{Kind: GHOther, Detail: "network down"}})
	if _, ok := m.prCache[sha]; ok {
		t.Fatalf("エラー結果がキャッシュされている")
	}
	if m.toast.ok || !strings.Contains(m.toast.text, "PR の取得に失敗") {
		t.Errorf("エラートーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	// 再度 p → 再取得が走る
	_, cmd := m.handleKey("p")
	if cmd == nil || !m.prBusy[sha] {
		t.Errorf("エラー後の p で再取得しない")
	}
}

func TestBrowseOpenPRNotFound(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.Update(prMsg{sha: sha, pr: nil})
	if !strings.Contains(m.toast.text, "PR はありません") {
		t.Errorf("PR なしのトーストが出ない: %q", m.toast.text)
	}
	// nil もキャッシュされ、再度 p を押しても API へ行かない
	_, cmd := m.handleKey("p")
	if cmd != nil || m.prBusy[sha] {
		t.Errorf("PR なしキャッシュ後に再取得した")
	}
}

func TestBrowseOpenPRUnpushed(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	if _, cmd := m.handleKey("p"); cmd != nil {
		t.Errorf("未 push コミットで PR 取得が走った")
	}
	if !strings.Contains(m.toast.text, "未 push") {
		t.Errorf("未 push のトーストが出ない: %q", m.toast.text)
	}
}

func TestBrowseDiffOpenScrollClose(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	diffLines := make([]string, 20)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	calls := stubDiff(t, diffLines, nil)

	_, cmd := m.handleKey("d")
	if m.diffOv.sha != m.commits[0].SHA {
		t.Fatalf("diffSHA = %q; want カーソル位置のコミット", m.diffOv.sha)
	}
	if !m.diffOv.cache.loading(m.diffOv.sha) {
		t.Error("取得中フラグが立っていない")
	}
	deliverDiffMsg(t, m, cmd)
	if len(*calls) != 1 || (*calls)[0] != m.commits[0].SHA {
		t.Fatalf("loadCommitDiff の呼び出し = %v", *calls)
	}
	if m.diffOv.cache.loading(m.diffOv.sha) {
		t.Error("取得完了後も busy のまま")
	}
	view := m.View().Content
	if !strings.Contains(view, "line-0") || !strings.Contains(view, "diff:") {
		t.Fatalf("diff ポップアップが描画されていない:\n%s", view)
	}
	// スクロール: j で 1 行、G で末尾
	m.handleKey("j")
	if m.diffOv.offset != 1 {
		t.Errorf("j 後の offset = %d; want 1", m.diffOv.offset)
	}
	m.handleKey("G")
	if m.diffOv.offset != len(diffLines)-m.visibleDiffRows() {
		t.Errorf("G 後の offset = %d", m.diffOv.offset)
	}
	// q で閉じる (アプリは終了しない)
	m.handleKey("q")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v; want 閉じるのみ", m.diffOv.sha, m.done)
	}
}

// diff ポップアップの本文にスクロールバー列が出ても枠が崩れない (全行が同じ表示幅) ことと、
// 全行が収まるときはバー列が出ないこと (TestJobDetailBoxLinesScrollbar の diff 版。
// withScrollbar の単体テストではなく描画経路 boxLines で幅の均一性を見る)。
func TestDiffBoxLinesScrollbar(t *testing.T) {
	diffLines := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "diff line"
		}
		return out
	}
	const width, rows = 50, 8
	commit := &Commit{SHA: "a", ShortSHA: "abc1234", Subject: "subject"}
	o := newDiffOverlay()

	// 溢れる: バー列あり + 幅は均一 + thumb は比率
	o.sha = commit.SHA
	o.cache.store(commit.SHA, diffLines(50), commit.SHA)
	o.offset = 20
	box := o.boxLines(width, false, "", commit, rows)
	uniformWidth(t, box)
	// 影付き箱: 末尾 2 行は下辺 (▖▁▗+影) と下端影なので本文から除く。本文行の行末は
	// 右影 1 桁 (NO_COLOR は ░/▒) が付く
	body := box[1 : len(box)-2]
	thumbs := 0
	for i, l := range body {
		shade := shadowGlyphMono
		if i == 0 {
			shade = shadowGlyphMonoEdge
		}
		trimmed := strings.TrimSuffix(strings.TrimSuffix(l, shade), " "+borderLight.v)
		switch {
		case strings.HasSuffix(trimmed, scrollbarThumbGlyph):
			thumbs++
		case strings.HasSuffix(trimmed, scrollbarTrackGlyph):
		default:
			t.Fatalf("本文行 %d にバー列が無い: %q", i, l)
		}
	}
	if thumbs == 0 || thumbs == len(body) {
		t.Fatalf("thumb 行数 = %d (本文 %d 行) — 比率になっていない", thumbs, len(body))
	}

	// colored でも幅は均一 (SGR 入りの影・バー列が幅計算を狂わせない)
	uniformWidth(t, o.boxLines(width, true, "", commit, rows))

	// 収まる: バー列なし (本文幅が戻る)。枠幅は溢れる場合と同じ
	o.cache.store(commit.SHA, diffLines(rows-2), commit.SHA)
	o.offset = 0
	fit := o.boxLines(width, false, "", commit, rows)
	if w := uniformWidth(t, fit); w != dispWidth(stripANSI(box[0])) {
		t.Fatalf("収まる場合の枠幅 = %d, 溢れる場合 = %d", w, dispWidth(stripANSI(box[0])))
	}
	for i, l := range fit[1 : len(fit)-2] {
		if strings.Contains(l, scrollbarThumbGlyph) {
			t.Fatalf("収まるのに thumb が出ている (行 %d): %q", i, l)
		}
	}
}

// pager overlay の描画は offset を「窓 (total-rows)」へ再クランプすること。offset はキー処理側
// (pagerScrollKey 等) がその時点の rows でクランプするが、G で末尾へ行った後にリサイズで rows が
// 増えると保存済み offset が窓の上限を越えたまま残る。描画側で clampScrollOffset を通さないと、
// 広がった分の行を使わず末尾数行だけの箱になる (status viewer の pagerBox と同じ規律)。
func TestPagerOverlayReclampsOffsetOnRedraw(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	const width, rows = 50, 8 // 期待窓: start=42 → タイトル [43-50/50]

	commit := &Commit{SHA: "a", ShortSHA: "abc1234", Subject: "subject"}
	d := newDiffOverlay()
	d.sha = commit.SHA
	d.cache.store(commit.SHA, lines, commit.SHA)
	d.offset = 49 // 旧 rows で G → リサイズで rows が増えた後を想定 (窓上限 42 を越えている)
	if title := d.boxLines(width, false, "", commit, rows)[0]; !strings.Contains(title, "[43-50/50]") {
		t.Errorf("diff overlay が offset を窓へ再クランプしていない: title=%q, want [43-50/50]", title)
	}

	j := newJobDetailOverlay()
	j.open = true
	j.cache.store("key", lines, "key")
	j.offset = 49
	if title := j.boxLines(width, false, "", "job", "key", rows)[0]; !strings.Contains(title, "[43-50/50]") {
		t.Errorf("job 詳細 overlay が offset を窓へ再クランプしていない: title=%q, want [43-50/50]", title)
	}
}

func TestBrowseDiffToggleAndCache(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	calls := stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	releaseKey(m)    // 指を離してから押し直す (キーリピート判定を跨ぐ)
	m.handleKey("d") // toggle 閉
	if m.diffOv.sha != "" {
		t.Fatal("d の再押下で閉じていない")
	}
	releaseKey(m)
	_, cmd2 := m.handleKey("d") // 再度開く → キャッシュヒットで再取得しない
	if cmd2 != nil {
		t.Error("キャッシュヒット時にも取得コマンドが返った")
	}
	if len(*calls) != 1 {
		t.Errorf("loadCommitDiff 呼び出し回数 = %d; want 1 (キャッシュ)", len(*calls))
	}
	if m.diffOv.sha == "" {
		t.Error("キャッシュヒット時に開いていない")
	}
}

func TestBrowseDiffFromPanelUsesPanelSHA(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	withJobs(m, 0)
	m.handleKey("enter") // panel を開く
	if m.panelSHA == "" {
		t.Fatal("panel が開いていない")
	}
	stubDiff(t, []string{"x"}, nil)
	m.handleKey("d")
	if m.diffOv.sha != m.commits[0].SHA {
		t.Errorf("diffSHA = %q; want panel のコミット", m.diffOv.sha)
	}
	if m.panelSHA != "" {
		t.Error("diff を開いたら panel は閉じる契約")
	}
}

func TestBrowseDiffErrorShowsToastAndCloses(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, nil, errors.New("boom"))
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	if m.diffOv.sha != "" {
		t.Error("取得失敗時にポップアップが開いたまま")
	}
	if m.toast.ok || !strings.Contains(m.toast.text, "diff の取得に失敗") {
		t.Errorf("失敗トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	if m.lastWarning != m.toast.text {
		t.Errorf("取得失敗が lastWarning に残らない: %q", m.lastWarning)
	}
}

// 回帰: diff ポップアップは pager 流儀 — Space/Enter はスクロールであり閉じない。
// 末尾に達したら最終行を表示したまま止まる (実機で Space 送り中に突然閉じた報告への修正)。
func TestBrowseDiffPagerKeysScrollNotClose(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	diffLines := make([]string, 30)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	stubDiff(t, diffLines, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	maxOffset := len(diffLines) - m.visibleDiffRows()

	m.handleKey(" ")
	if m.diffOv.sha == "" {
		t.Fatal("Space で閉じた (半ページスクロールのはず)")
	}
	if m.diffOv.offset == 0 {
		t.Error("Space でスクロールしていない")
	}
	m.handleKey("enter")
	if m.diffOv.sha == "" {
		t.Fatal("Enter で閉じた (1 行スクロールのはず)")
	}
	// 末尾を大きく超えて送っても最終行位置で止まり、開いたまま
	for range 100 {
		m.handleKey(" ")
	}
	if m.diffOv.sha == "" {
		t.Fatal("末尾到達後のスクロールで閉じた")
	}
	if m.diffOv.offset != maxOffset {
		t.Errorf("末尾で offset = %d; want %d (最終行を表示し続ける)", m.diffOv.offset, maxOffset)
	}
	// 半ページ移動は glide (scroll_glide.go) で数フレームかけて着地する。⚠️ ここで手で
	// advanceGlide を呼ぶ前に「キー処理が tick を返している」ことを必ず確かめる: 手で進める
	// だけのテストは、tick を張り忘れて実機で永久に固まるバグ (敵対的レビュー P1) を隠す。
	// ⚠️ cmd の nil 判定では足りない: maybeTick は single-flight で、既にチェーンが生きていれば
	// nil を返す。不変条件は「glide 中は tick チェーンが生きている (m.ticking)」。
	if m.handleKey(" "); m.diffOv.glide.active && !m.ticking {
		t.Fatal("glide 中なのに tick チェーンが無い (実機では中途位置で固まる)")
	}
	for range scrollAnimFrames {
		m.diffOv.advanceGlide()
	}
	if m.diffOv.glide.active {
		t.Errorf("%d フレームで glide が着地しない", scrollAnimFrames)
	}
	view := m.View().Content
	if !strings.Contains(view, diffLines[len(diffLines)-1]) {
		t.Error("末尾で最終行が描画されていない")
	}
	// 閉じるのは q
	m.handleKey("q")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v", m.diffOv.sha, m.done)
	}
}

// diff が空 (変更なし) のときは "(diff はありません)" を出す (busy でもエラーでもない経路)。
func TestBrowseDiffEmptyShowsMessage(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	if m.diffOv.sha == "" {
		t.Fatal("空 diff でポップアップが閉じてしまった (エラーではないので開いたままが仕様)")
	}
	if v := stripANSI(m.View().Content); !strings.Contains(v, "diff はありません") {
		t.Fatalf("空 diff の案内が出ていない:\n%s", v)
	}
}

// diff ポップアップ表示中の y はカーソル位置コミットの URL をコピーする。
func TestBrowseDiffCopyURLWhileOpen(t *testing.T) {
	copied := stubClipboard(t)
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("y")
	want := "https://github.com/o/r/commit/" + m.commits[0].SHA
	if *copied != want {
		t.Errorf("diff 表示中の y でコピーされた URL = %q; want %q", *copied, want)
	}
	if m.diffOv.sha == "" {
		t.Error("y でポップアップが閉じた (コピーのみが仕様)")
	}
}

// 上スクロールは 0 で止まり、g で先頭・b (ctrl+u) で半ページ戻る。
func TestBrowseDiffScrollUpClampAndReset(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	diffLines := make([]string, 40)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	stubDiff(t, diffLines, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)

	m.handleKey("G") // 末尾へ
	end := m.diffOv.offset
	if end == 0 {
		t.Fatal("G で末尾へ動いていない")
	}
	m.handleKey("b") // 半ページ戻る
	if m.diffOv.offset >= end || m.diffOv.offset < 0 {
		t.Errorf("b (半ページ上) 後の offset = %d; want 0<=x<%d", m.diffOv.offset, end)
	}
	m.handleKey("g") // 先頭
	if m.diffOv.offset != 0 {
		t.Errorf("g 後の offset = %d; want 0", m.diffOv.offset)
	}
	m.handleKey("k") // 先頭で k は 0 に張り付く
	if m.diffOv.offset != 0 {
		t.Errorf("先頭での k 後の offset = %d; want 0 (クランプ)", m.diffOv.offset)
	}
}

// esc でも diff ポップアップは閉じる (q/h/left/d と同じ閉じる系キー)。
func TestBrowseDiffEscCloses(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("esc")
	if m.diffOv.sha != "" || m.done {
		t.Errorf("esc: diffSHA=%q done=%v; want 閉じるのみ", m.diffOv.sha, m.done)
	}
}

// 別コミットの古い diff 取得エラーが遅れて届いても、現在開いている diff は閉じない
// (diffMsg の sha 一致ガードの回帰。閉じるのは msg.sha == 現在表示中のときだけ)。
func TestBrowseDiffStaleErrorDoesNotCloseCurrent(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d") // commit[0] の diff を開く
	deliverDiffMsg(t, m, cmd)
	current := m.diffOv.sha
	if current == "" {
		t.Fatal("diff が開いていない")
	}
	// commit[1] 宛の古いエラーが遅れて到着 (直接 Update へ流す)
	m.Update(diffMsg{sha: m.commits[1].SHA, err: errors.New("stale boom")})
	if m.diffOv.sha != current {
		t.Errorf("別 SHA のエラーで現在の diff が閉じた: diffSHA=%q; want %q", m.diffOv.sha, current)
	}
	// 表示中でない SHA の遅延エラーは警告トーストも出さない (2026-07-29)
	if m.lastWarning != "" {
		t.Errorf("stale な diff エラーで警告が出た: %q", m.lastWarning)
	}
	// 表示中の SHA のエラーは従来どおり警告する
	m.Update(diffMsg{sha: current, err: errors.New("current boom")})
	if !strings.Contains(m.lastWarning, "diff の取得に失敗") {
		t.Errorf("表示中 diff のエラーで警告が出ない: %q", m.lastWarning)
	}
}

func TestBrowseListOpenCommitURL(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	var opened []string
	stubBrowserFunc(t, func(url string) error { opened = append(opened, url); return nil })

	m.handleKey("j") // 2 番目のコミットへ
	_, cmd := m.handleKey("o")
	if cmd == nil {
		t.Fatal("o で open コマンドが返らない")
	}
	cmd()
	want := "https://github.com/o/r/commit/" + m.commits[1].SHA
	if len(opened) != 1 || opened[0] != want {
		t.Errorf("開いた URL = %v; want %s", opened, want)
	}
}

func TestBrowseListOpenCommitURLNoRepo(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.hasRepo = false
	_, cmd := m.handleKey("o")
	if cmd != nil {
		t.Error("repo なしで open コマンドが返った")
	}
	if !m.toast.visible() || m.toast.ok {
		t.Errorf("repo なしの失敗トーストが出ていない: visible=%v ok=%v", m.toast.visible(), m.toast.ok)
	}
}

func TestBrowseCopyJobContextCached(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	copied := stubClipboard(t)
	m.openPanel()
	m.handleKey("j")
	// 詳細キャッシュ済み → 即コピー (追加 fetch なし)
	m.detailOv.cache.store(m.detailKey(), []string{"✗ lint (13s)", "", "[failure] a.go:1", "  boom"}, m.detailKey())
	if _, cmd := m.handleKey("Y"); cmd != nil {
		t.Fatal("キャッシュ済みなのに fetch が走った")
	}
	for _, want := range []string{"## CI job: lint", "o/r@aaaaaaa", "https://github.com/o/r/runs/9", "[failure] a.go:1"} {
		if !strings.Contains(*copied, want) {
			t.Fatalf("コピー内容に %q が無い:\n%s", want, *copied)
		}
	}
	if strings.Contains(*copied, "\x1b[") {
		t.Fatal("コピー内容に ANSI が残っている")
	}
	if !m.toast.ok || !strings.Contains(m.toast.text, "コピーしました") {
		t.Fatalf("完了トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
}

func TestBrowseCopyJobContextFetchesThenCopies(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	copied := stubClipboard(t)
	m.openPanel()
	m.handleKey("j")
	// 未取得 → 詳細ポップアップを開いて取得し、到着時にコピーされる
	_, cmd := m.handleKey("Y")
	if cmd == nil || !m.detailOv.visible() || m.copyOnDetail != m.detailKey() {
		t.Fatalf("Y で詳細取得+コピー予約に入らない: cmd=%v open=%v pending=%q",
			cmd != nil, m.detailOv.visible(), m.copyOnDetail)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"✗ step", "log line"}})
	if !strings.Contains(*copied, "log line") || !strings.Contains(*copied, "## CI job: lint") {
		t.Fatalf("到着時にコピーされない:\n%s", *copied)
	}
	if m.copyOnDetail != "" {
		t.Fatal("コピー予約が消えない")
	}
	// 取得失敗 (ghErr) では予約破棄のみでコピーされない
	*copied = ""
	m.detailOv.reset()
	m.openPanel()
	m.handleKey("j")
	m.handleKey("Y")
	m.Update(jobDetailMsg{key: m.detailKey(), ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if *copied != "" || m.copyOnDetail != "" {
		t.Fatalf("取得失敗でコピーされた / 予約が残った: copied=%q pending=%q", *copied, m.copyOnDetail)
	}
	// closePanel で予約破棄 (閉じた後の到着でコピーしない)
	m.detailOv.reset()
	m.openPanel()
	m.handleKey("j")
	m.handleKey("Y")
	key := m.detailKey()
	m.closePanel()
	m.Update(jobDetailMsg{key: key, lines: []string{"late"}})
	if *copied != "" {
		t.Fatal("パネルを閉じた後の到着でコピーされた")
	}
}

func TestBrowsePRStatusFlow(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	sha := m.commits[0].SHA
	// P で開いて取得に入る
	_, cmd := m.handleKey("P")
	if cmd == nil || !m.prStatusOv.visible() || !m.prStatusOv.fetching() {
		t.Fatalf("P で PR 取得に入らない: cmd=%v visible=%v fetching=%v", cmd != nil, m.prStatusOv.visible(), m.prStatusOv.fetching())
	}
	// 取得結果が描画される (state / レビュー / conflict / CI)
	m.Update(prStatusMsg{sha: sha, status: &PRStatus{
		PRRef: PRRef{Number: 12, URL: "https://github.com/o/r/pull/12", State: "OPEN"},
		Title: "new feature", ReviewDecision: "APPROVED", Mergeable: "CONFLICTING",
		BaseRefName: "master", HeadRefName: "f/new",
	}})
	m.details[sha] = []CheckDetail{{Name: "lint", State: StateFailure}}
	v := stripANSI(m.View().Content)
	for _, want := range []string{"PR #12: new feature", "OPEN", "f/new → master", "APPROVED", "CONFLICTING", "CI: ✗", "1 job 失敗"} {
		if !strings.Contains(v, want) {
			t.Fatalf("PR ポップアップに %q が無い:\n%s", want, v)
		}
	}
	// 表示中はモーダル: o でブラウザ、q で閉じる
	opened := stubBrowser(t)
	_, cmd = m.handleKey("o")
	if cmd == nil {
		t.Fatal("o でブラウザ Cmd が返らない")
	}
	cmd()
	if *opened != "https://github.com/o/r/pull/12" {
		t.Fatalf("開いた URL が違う: %q", *opened)
	}
	m.handleKey("q")
	if m.prStatusOv.visible() {
		t.Fatal("q で閉じない")
	}
	// キャッシュ済みなので開き直しは fetch なし、P の再押下で toggle 閉
	if _, cmd := m.handleKey("P"); cmd != nil {
		t.Fatal("キャッシュ済みなのに再取得した")
	}
	if !m.prStatusOv.visible() {
		t.Fatal("開き直せない")
	}
	releaseKey(m) // 指を離してから押し直す
	m.handleKey("P")
	if m.prStatusOv.visible() {
		t.Fatal("P の再押下で toggle 閉しない")
	}
}

func TestBrowsePRStatusGuardsAndErrors(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateUnpushed)
	// 未 push は取得しない
	// 未 push は取得コマンドを返さないが、理由の失敗トーストは出す (cmd は maybeTick を含む)
	if m.handleKey("P"); m.prStatusOv.visible() {
		t.Fatalf("未 push で PR 取得に入った: %q", m.toast.text)
	}
	if !strings.Contains(m.toast.text, "未 push") {
		t.Fatalf("未 push トーストが出ない: %q", m.toast.text)
	}
	// 取得エラーはキャッシュせず閉じ、失敗トーストを出す (次の P で再試行できる)
	m.statuses = statusesFor(m, StateSuccess)
	releaseKey(m) // 直前の P から続けて押すのは実機ではリピート扱い
	m.handleKey("P")
	sha := m.commits[0].SHA
	m.Update(prStatusMsg{sha: sha, ghErr: &GHError{Kind: GHOther, Detail: "boom"}})
	if m.prStatusOv.visible() || m.toast.ok || !strings.Contains(m.toast.text, "PR の取得に失敗") {
		t.Fatalf("エラーで閉じない / 失敗トーストが出ない: visible=%v toast=%q", m.prStatusOv.visible(), m.toast.text)
	}
	if _, ok := m.prStatusOv.cache[sha]; ok {
		t.Fatal("エラーがキャッシュされた (PR なし誤答が固定される)")
	}
	// PR なしは nil キャッシュ + その旨の表示
	releaseKey(m)
	m.handleKey("P")
	m.Update(prStatusMsg{sha: sha, status: nil})
	if !strings.Contains(stripANSI(m.View().Content), "紐づく PR はありません") {
		t.Fatal("PR なしの表示が出ない")
	}
}

// 回帰 (レビュー確定 high): Y でコピー予約後にフォーカスが別 job へ動いたら、旧 job の詳細到着で
// コピーしない (別 job のヘッダに旧 job 本文を貼る silent 誤コピーの防止)。
func TestBrowseCopyJobContextFocusMovedDropsCopy(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, CheckID: 1},
		{Name: "lint", State: StateFailure, CheckID: 7},
	}
	copied := stubClipboard(t)
	m.openPanel()
	m.handleKey("j") // job0 (build) にフォーカス
	key0 := m.detailKey()
	m.handleKey("Y") // 未取得 → 予約 + 詳細ポップアップ取得
	if m.copyOnDetail != key0 {
		t.Fatalf("コピー予約されない: %q", m.copyOnDetail)
	}
	m.handleKey("esc") // 詳細ポップアップだけ閉じる (パネルは残る)
	if m.panelSHA == "" {
		t.Fatal("esc でパネルまで閉じた")
	}
	m.handleKey("j") // job1 (lint) へフォーカス移動
	m.Update(jobDetailMsg{key: key0, lines: []string{"build log line"}})
	if *copied != "" {
		t.Fatalf("フォーカス移動後に旧 job をコピーした: %q", *copied)
	}
	if m.copyOnDetail != "" {
		t.Fatal("到着で予約が破棄されていない")
	}
}

// 回帰 (レビュー確定 medium security): コピー時に job 名/URL の端末制御シーケンス (OSC52 等) を
// 除去する。stripANSI 単体は OSC を残すため sanitizeDetailLine と併用している。
func TestBrowseCopyJobContextSanitizesHeader(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "li\x1b[31mnt", State: StateFailure, CheckID: 7, URL: "https://x/\x1b]0;pwn\x07ok"},
	}
	copied := stubClipboard(t)
	m.openPanel()
	m.handleKey("j")
	m.detailOv.cache.store(m.detailKey(), []string{"log"}, m.detailKey())
	m.handleKey("Y")
	if strings.ContainsRune(*copied, '\x1b') || strings.ContainsRune(*copied, '\x07') {
		t.Fatalf("コピー内容に端末制御シーケンスが残った: %q", *copied)
	}
	if !strings.Contains(*copied, "lint") || !strings.Contains(*copied, "https://x/ok") {
		t.Fatalf("無害化で本来の文字まで欠けた: %q", *copied)
	}
}

// 回帰 (レビュー確定 medium): close→reopen 連打で in-flight の PR 取得を二重発火しない。
func TestPRStatusOverlayNoDoubleFetch(t *testing.T) {
	o := newPRStatusOverlay()
	if !o.open("A") {
		t.Fatal("初回 open が fetch を要求しない")
	}
	o.close() // 応答前に閉じる (busy[A] は保持されるべき)
	if o.open("A") {
		t.Fatal("in-flight 中の reopen が二重 fetch を要求した")
	}
	o.receive("A", &PRStatus{PRRef: PRRef{Number: 1}}, nil) // 応答 → busy 解除 + cache
	o.close()
	if o.open("A") {
		t.Fatal("cache 済みなのに再 fetch を要求した")
	}
}

// 回帰 (レビュー確定 low): 別 sha へ移った後に届く PR 取得エラーで、表示中 (別 sha) に無関係な
// 失敗トーストを被せない。
func TestBrowsePRStatusStaleErrorNoToast(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	shaA := m.commits[0].SHA
	shaB := m.commits[1].SHA
	m.handleKey("P") // A を開く (fetch A)
	releaseKey(m)
	m.handleKey("P") // A を閉じる (toggle)
	m.cursor = 1
	releaseKey(m)
	m.handleKey("P") // B を開く (fetch B)
	m.Update(prStatusMsg{sha: shaB, status: &PRStatus{PRRef: PRRef{Number: 2, State: "OPEN"}, Title: "b"}})
	m.toast = toast{}
	m.Update(prStatusMsg{sha: shaA, ghErr: &GHError{Kind: GHOther, Detail: "boom"}}) // A の遅延エラー
	if m.toast.visible() {
		t.Fatalf("別 sha の遅延エラーでトーストが出た: %q", m.toast.text)
	}
	if !m.prStatusOv.visible() {
		t.Fatal("別 sha の遅延エラーで B の表示が閉じられた")
	}
}

// issues viewer (i キー) の結合。ビュー本体のテストは issues_view_test.go にあり、ここでは
// browseModel 側の配線 (キー経路の判定順・全画面差し替え・hint・スピナー) だけを見る。

func TestIssuesViewerOpenAndSwallowKeys(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.handleKey("i")
	if cmd == nil {
		t.Fatal("i でスキャンの Cmd が返らない")
	}
	if !m.issuesOv.visible() {
		t.Fatal("i で issues viewer が開かない")
	}
	// ⚠️ 回帰防止の主眼: viewer 表示中のキーは viewer が全部飲む。判定順を崩すと、一覧を
	// 見ている最中の u / b が pull / push の確認を開く (裸の b/u が push/pull なので)
	m.handleKey("u")
	if m.actModal.pullConfirm {
		t.Fatal("viewer 表示中の u が pull 確認を開いた")
	}
	m.handleKey("b")
	if m.actModal.pushConfirm {
		t.Fatal("viewer 表示中の b が push 確認を開いた")
	}
	// C (claude update) や w (警告コピー) も素通りしない
	m.handleKey("C")
	if m.actModal.updating {
		t.Fatal("viewer 表示中の C が update を起動した")
	}
	// ⚠️ U (usage) だけは素通りではなく viewer の上でも効く (ユーザー要望 2026-08-01)。
	// 以前は「全画面 viewer の下では描かれないのに取得だけ走る」ため弾いていたので、
	// 開けるようにした今は「本当に画面へ出ること」まで見る (出ないなら弾いていた頃と同じ、
	// 見えない層へ状態を書く経路に戻る)。
	m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 42}}}
	m.usageOv.visible = false
	m.handleKey("U")
	if !m.usageOv.visible {
		t.Fatal("viewer 表示中の U で usage が開かない")
	}
	if out := stripANSI(m.View().Content); !strings.Contains(out, "5h") {
		t.Fatalf("usage が viewer の窓へ合成されていない:\n%s", out)
	}
	// 一覧と同じ語彙: 次のキーで引っ込む
	m.handleKey("j")
	if m.usageOv.visible {
		t.Fatal("viewer 表示中の他キーで usage が引っ込まない")
	}
	m.handleKey("i") // トグルで閉じる
	if m.issuesOv.visible() {
		t.Fatal("i で閉じない")
	}
}

func TestIssuesViewerReplacesWholeScreen(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.handleKey("i")
	m.Update(issuesScanMsg{
		dirs:   []string{"/repo/issues"},
		issues: []*issues.Issue{fakeIssue("030", "feat", "new-thing", issues.StatusOpen)},
	})
	m.issuesOv.finishAnim() // 開く演出は別テストで見る (ここは着地後の画面を見たい)
	out := m.viewLines()
	if strings.Contains(out, "subject") {
		t.Fatalf("コミット一覧が残っている:\n%s", out)
	}
	if !strings.Contains(out, "030") || !strings.Contains(out, "feat") {
		t.Fatalf("issue の行が出ていない:\n%s", out)
	}
	// 窓はちょうど pageSize 行 + hint 行 (枠 OFF のテスト設定)
	if got, want := len(strings.Split(out, "\n")), m.pageSize()+1; got != want {
		t.Fatalf("行数が違う: got %d want %d", got, want)
	}
	if !strings.Contains(m.hintLine(), "カテゴリ") {
		t.Fatalf("hint が viewer のものになっていない: %q", m.hintLine())
	}
}

func TestIssuesViewerHintIsNotPrefixed(t *testing.T) {
	// viewer の hint は popup の実幅ぴったりに詰めてある。CI 進捗や GH 警告を前置すると
	// 末尾のキー案内が黙って切り落とされる (issues_view.go の hint の不変条件が結合点で壊れる)。
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	m.fetching = true
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom"}
	hint := m.hintLine()
	if strings.Contains(hint, "CI 状態を取得中") || strings.Contains(hint, "⚠") {
		t.Fatalf("viewer の hint に前置が入った: %q", hint)
	}
	if !strings.Contains(hint, "q: 閉じる") {
		t.Fatalf("viewer の hint の末尾が切れている: %q", hint)
	}
}

func TestIssuesViewerReloadsAfterEditorCloses(t *testing.T) {
	// v で nvim を開いて編集して戻ったとき、取り直さないと編集結果 (H1・status・チェックボックス)
	// が一覧にも本文にも出ず、viewer が古い内容を最新として表示する。
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	// 取り直しは cwd から探索し直すので、issues ディレクトリを持つ木を作ってそこを起点にする
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "001-feat-x.md")
	if err := os.WriteFile(path, []byte("# 001 feat: 編集前\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	m.issuesOv.cwd = root
	m.Update(issuesScanMsg{root: root, dirs: []string{dir}, issues: []*issues.Issue{iss}})
	m.issuesOv.finishAnim()
	if out := m.viewLines(); !strings.Contains(out, "編集前") {
		t.Fatalf("前提が崩れた: 編集前のタイトルが出ていない:\n%s", out)
	}

	// nvim が本文を書き換えて閉じた
	if err := os.WriteFile(path, []byte("# 001 feat: 編集後\n\n- [x] やった\n- [ ] まだ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(editorClosedMsg{})
	if cmd == nil {
		t.Fatal("editorClosedMsg で取り直しの Cmd が返らない")
	}
	msg := drainScanMsg(t, cmd)
	m.Update(msg)
	m.issuesOv.finishAnim()
	out := m.viewLines()
	if !strings.Contains(out, "編集後") {
		t.Fatalf("編集結果が一覧に反映されていない:\n%s", out)
	}
	// ⚠️ フィクスチャに checkbox を残しているのは意図。これが無いと下の
	// 「一覧に進捗が出ない」は**どんな退行でも赤にならない** (チェックボックスが無ければ
	// 進捗を出す実装でも "1/1" は生成されない)。差が出る状況を作ってから主張する
	// (2026-08-14 の R1 レビューで、フィクスチャを削ったせいでこの assert が
	// 空回りしていたのを検出された)。済み/未を 1 個ずつにして期待値を "1/2" にするのは
	// operand を入れ違えた実装 ("2/1") を捕まえるため — "1/1" では対称で通ってしまう (R3 の指摘)
	if strings.Contains(out, "1/2") {
		t.Errorf("一覧に進捗が出ている (全 issue の全文読みを止めた前提が崩れている):\n%s", out)
	}
	// 進捗は詳細を開いたときだけ出る (そこでは Body が全文を持っているので追加の I/O が無い)。
	// 一覧から消したぶんの表示カバレッジをここで持つ
	m.issuesOv.handleKey("enter", issuesViewport{width: 120, page: 20})
	m.issuesOv.drawer.finish()
	if body := m.viewLines(); !strings.Contains(body, "1/2") {
		t.Errorf("詳細ヘッダに進捗が出ていない:\n%s", body)
	}
}

// drainScanMsg は tea.Cmd (Batch を含む) から issuesScanMsg を取り出す。
func drainScanMsg(t *testing.T, cmd tea.Cmd) issuesScanMsg {
	t.Helper()
	switch msg := cmd().(type) {
	case issuesScanMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if scan, ok := c().(issuesScanMsg); ok {
				return scan
			}
		}
	}
	t.Fatal("Cmd から issuesScanMsg が取れない")
	return issuesScanMsg{}
}

func TestIssuesViewerNotOpenedWhilePanelOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("i")
	if m.issuesOv.visible() {
		t.Fatal("job パネル表示中の i が viewer を開いた (パネルの語彙を優先すべき)")
	}
}

func TestIssuesViewerScanKeepsSpinnerAlive(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	if !m.spinnerActive() {
		t.Fatal("スキャン中にスピナーの tick が回らない")
	}
	m.Update(issuesScanMsg{dirs: []string{"/repo/issues"}})
	if !m.spinnerActive() {
		t.Fatal("開く演出中はチェーンを回し続ける必要がある")
	}
	if got := m.tickInterval(); got != zoomInterval {
		t.Fatalf("開閉スライド中の tick 周期 = %v, want zoomInterval (60fps)", got)
	}
	m.issuesOv.finishAnim()
	if m.spinnerActive() {
		t.Fatal("スキャンも演出も終わったのにスピナーが回り続けている")
	}
	if got := m.tickInterval(); got != spinnerInterval {
		t.Fatalf("演出後に tick 周期が戻っていない: %v", got)
	}
}

// status viewer の開閉スライドは 60fps、pager glide は 30fps で tick する
// (spinnerActive に足すだけでは 12.5fps で点滅して見える回帰の防止)。
func TestStatusViewerSlideTicksAt60FPS(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statusOv.closeAnimOff = false // newTestBrowse は演出を切っているので戻す (演出そのものの検査)
	m.handleKey("s")
	if !m.statusOv.visible() {
		t.Fatal("s で status viewer が開かない")
	}
	if !m.spinnerActive() {
		t.Fatal("開く演出中はチェーンを回し続ける必要がある")
	}
	if got := m.tickInterval(); got != zoomInterval {
		t.Fatalf("開閉スライド中の tick 周期 = %v, want zoomInterval (60fps)", got)
	}
	m.statusOv.animStart = time.Time{} // スライド着地後
	if got := m.tickInterval(); got != spinnerInterval {
		t.Fatalf("スライド後に tick 周期が戻っていない: %v", got)
	}
	m.statusOv.pagerGlide.active = true
	if !m.spinnerActive() {
		t.Fatal("pager glide 中にスピナーの tick が回らない")
	}
	if got := m.tickInterval(); got != scrollInterval {
		t.Fatalf("pager glide 中の tick 周期 = %v, want scrollInterval (30fps)", got)
	}
}

// status ⇔ issues の相互切り替え (i / s の横断キー。ユーザー要望 2026-08-06)。
func TestViewerCrossSwitching(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("s")
	if !m.statusOv.visible() {
		t.Fatal("前提が崩れた: s で status viewer が開かない")
	}
	releaseKey(m)
	m.handleKey("i")
	if m.statusOv.visible() {
		t.Fatal("status viewer の i で status が閉じない")
	}
	if !m.issuesOv.visible() {
		t.Fatal("status viewer の i で issues viewer が開かない")
	}
	releaseKey(m)
	m.handleKey("s")
	if m.issuesOv.visible() {
		t.Fatal("issues viewer の s で issues が閉じない")
	}
	if !m.statusOv.visible() {
		t.Fatal("issues viewer の s で status viewer が開かない")
	}
}

// 起動時 restore が返る前に type-ahead で status viewer が開いていたら復元を捨てる
// (両 viewer 同時 shown だと「見えている status」と「キーを受ける issues」が食い違う。
// 敵対レビューで再現 2026-08-06)。
func TestIssuesRestoreDroppedWhenStatusOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("s")
	m.Update(issuesRestoreMsg{})
	if m.issuesOv.visible() {
		t.Fatal("status viewer 表示中の遅延 restore で issues viewer が開いた")
	}
	if !m.statusOv.visible() {
		t.Fatal("前提が崩れた: status viewer が開いていない")
	}
}

// viewer からの q/esc は git log 一覧へ戻らず glogx ごと終了する (ユーザー要望 2026-08-06)。
// 一覧へ戻るのは toggle キー (s / i) だけ。
func TestViewerQuitKeysQuitApp(t *testing.T) {
	for _, tc := range []struct{ open, quit string }{
		{"s", "q"}, {"s", "esc"}, {"i", "q"}, {"i", "esc"},
	} {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.handleKey(tc.open)
		releaseKey(m)
		m.handleKey(tc.quit)
		if !m.done {
			t.Fatalf("%s viewer の %s で glogx が終了しない", tc.open, tc.quit)
		}
	}
	// toggle キーは従来どおり一覧へ戻る (終了しない)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("s")
	releaseKey(m)
	m.handleKey("s")
	if m.done || m.statusOv.visible() {
		t.Fatalf("s の toggle が壊れた (done=%v visible=%v)", m.done, m.statusOv.visible())
	}
}

// 番号入力中 (/) の s は検索語の入力へ飲まれ、viewer を切り替えない。
func TestIssuesViewerFilterSwallowsSwitchKey(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	releaseKey(m)
	m.handleKey("/")
	releaseKey(m)
	m.handleKey("s")
	if !m.issuesOv.visible() || m.statusOv.visible() {
		t.Fatalf("番号入力中の s が viewer を切り替えた (issues=%v status=%v)",
			m.issuesOv.visible(), m.statusOv.visible())
	}
}

func TestSpaceKeyScrollsEverywhere(t *testing.T) {
	// bubbletea v2 の KeyPressMsg.String() は Space を "space" と綴る。v2 移行時にこの綴りを
	// 拾い落として Space の割当が全経路で無反応になっていた (ユーザー報告 2026-07-31)。
	// 主要 3 経路で「Space が効く」ことを固定する。
	if got := (tea.KeyPressMsg{Code: ' ', Text: " "}).String(); got != "space" {
		t.Fatalf("前提が変わった: v2 の Space の綴りが %q (このテストの根拠は \"space\")", got)
	}

	// 1. issues viewer の本文 pager
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-x.md")
	var body strings.Builder
	for i := range 80 {
		fmt.Fprintf(&body, "段落 %d\n\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(issuesScanMsg{
		dirs:   []string{dir},
		issues: []*issues.Issue{{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}},
	})
	m.handleKey("enter")
	m.viewLines() // 幅ごとの整形を確定させる
	m.handleKey("space")
	if m.issuesOv.bodyOff == 0 {
		t.Fatal("issues 本文で Space が半ページ送りしていない")
	}
	// 2. issues viewer の一覧 (カーソルの半ページ移動)
	m.handleKey("h") // 一覧へ戻る
	many := make([]*issues.Issue, 0, 30)
	for i := 30; i > 0; i-- {
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", issues.StatusOpen))
	}
	m.Update(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: many})
	m.handleKey("space")
	if m.issuesOv.cursor == 0 {
		t.Fatal("issues 一覧で Space がカーソルを送っていない")
	}
	m.handleKey("i") // viewer を閉じる

	// 3. diff pager (同じ綴り落ちで壊れていた既存経路)
	m.diffOv.cache.store(m.commits[0].SHA, manyLines(100), "")
	m.diffOv.sha = m.commits[0].SHA
	m.handleKey("space")
	if m.diffOv.offset == 0 {
		t.Fatal("diff pager で Space が半ページ送りしていない")
	}
}

func manyLines(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

// issues viewer のカーソル行に矢印が 1 本だけ出る (二重矢印の回帰)。
//
// 溝の矢印は viewer が付け (rowLine)、browseModel から渡す cursorPaint は「強調だけ」を担う
// 契約。ここに溝込みの cursorLine を渡すと "→ → 030 ..." になり、2 桁ぶん右へずれて末尾が切れる
// (ユーザー報告 2026-07-31)。色あり/なしの両方で見る (cursorEmphasis が分岐するため)。
func TestIssuesViewerCursorArrowNotDoubled(t *testing.T) {
	for _, colored := range []bool{false, true} {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.width, m.height = 100, 16
		m.colored = colored
		m.handleKey("i")
		m.issuesOv.receive(issuesScanMsg{
			dirs: []string{"/repo/issues"},
			issues: []*issues.Issue{
				fakeIssue("030", "feat", "alpha", issues.StatusOpen),
				fakeIssue("029", "feat", "beta", issues.StatusOpen),
			},
		})
		m.issuesOv.finishAnim() // 開く演出中は行が流し込み途中で出ない
		var cursorRow string
		for _, ln := range strings.Split(stripANSI(m.View().Content), "\n") {
			if strings.Contains(ln, "alpha") {
				cursorRow = ln
			}
		}
		if cursorRow == "" {
			t.Fatalf("colored=%v: カーソル行が描かれていない", colored)
		}
		if n := strings.Count(cursorRow, cursorGutterMark[:len(cursorGutterMark)-1]); n != 1 {
			t.Errorf("colored=%v: カーソル行の矢印が %d 本 (want 1): %q", colored, n, strings.TrimRight(cursorRow, " "))
		}
		// 溝は 2 桁ぶんだけ (矢印 + 空白)。番号がその直後から始まる
		if !strings.HasPrefix(strings.TrimLeft(cursorRow, " "), cursorGutterMark+"030") {
			t.Errorf("colored=%v: 溝の幅がずれている: %q", colored, strings.TrimRight(cursorRow, " "))
		}
	}
}

// issues viewer の操作結果は glogx 共通の右下トーストに出る (ユーザー要望 2026-07-31)。
//
// ⚠️ viewer は全画面で viewLines が早期 return するため、トーストを合成し忘れると通知が
// 画面に一切出ない (それが以前ヘッダー行に出していた理由)。ここは実 View の出力まで見る。
func TestIssuesViewerNotifiesViaToast(t *testing.T) {
	stubClipboardFunc(t, func(string) error { return nil })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.width, m.height = 100, 16
	m.handleKey("i")
	m.issuesOv.receive(issuesScanMsg{
		dirs:   []string{"/repo/issues"},
		issues: []*issues.Issue{fakeIssue("030", "feat", "alpha", issues.StatusOpen)},
	})
	m.issuesOv.finishAnim()

	m.handleKey("p") // 番号をコピー
	if !m.toast.visible() {
		t.Fatal("番号コピーでトーストが出ない")
	}
	if !m.toast.ok {
		t.Errorf("成功なのに失敗色: %q", m.toast.text)
	}
	if !strings.Contains(m.toast.text, "コピーしました") {
		t.Errorf("トーストの文面が想定と違う: %q", m.toast.text)
	}
	// トーストは右から滑り込むので、入場アニメを進めてから見る (通常経路と同じ)
	for range 30 {
		if !m.toast.animating() {
			break
		}
		m.toast.advance(m.colored)
	}
	// viewer は全画面なので、トーストを合成しないと画面に出ない
	if out := stripANSI(m.View().Content); !strings.Contains(out, "コピーしました") {
		t.Errorf("viewer の上にトーストが載っていない:\n%s", out)
	}
	// ヘッダー行には出さない (トーストと二重に出さない)
	if v := m.issuesOv; v.notice != "" {
		t.Errorf("通知が viewer 側に残っている: %q", v.notice)
	}
}

// 失敗は失敗色のトーストで出し、w でコピーできるよう lastWarning にも残す。
func TestIssuesViewerCopyFailureToast(t *testing.T) {
	stubClipboardFunc(t, func(string) error { return errors.New("pbcopy が見つかりません") })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.width, m.height = 100, 16
	m.handleKey("i")
	m.issuesOv.receive(issuesScanMsg{
		dirs:   []string{"/repo/issues"},
		issues: []*issues.Issue{fakeIssue("030", "feat", "alpha", issues.StatusOpen)},
	})
	m.issuesOv.finishAnim()

	m.handleKey("y")
	if !m.toast.visible() || m.toast.ok {
		t.Fatalf("失敗が失敗色のトーストになっていない: visible=%v ok=%v", m.toast.visible(), m.toast.ok)
	}
	if !strings.Contains(m.lastWarning, "コピーに失敗") {
		t.Errorf("lastWarning に残っていない (w でコピーできない): %q", m.lastWarning)
	}
}
