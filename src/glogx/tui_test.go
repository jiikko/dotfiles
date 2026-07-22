package main

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func newTestBrowse(t *testing.T, n int, statuses map[string]CIState, toFetch []string) *browseModel {
	t.Helper()
	commits := make([]Commit, n)
	for i := range commits {
		sha := strings.Repeat(string(rune('a'+i)), 40)
		commits[i] = Commit{
			SHA: sha, ShortSHA: sha[:7], Subject: "subject", Author: "koji", AuthorEmail: "k@x",
			Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "now", Message: "subject",
		}
	}
	m := newBrowseModel(commits, statuses, toFetch, Repo{Owner: "o", Name: "r"}, true,
		&Options{}, false, 80, 10)
	t.Cleanup(m.cancel)
	return m
}

func statusesFor(m *browseModel, state CIState) map[string]CIState {
	s := map[string]CIState{}
	for _, c := range m.commits {
		s[c.SHA] = state
	}
	return s
}

// withJobs は commit idx の details を job 2 件で埋めるテストヘルパー。
func withJobs(m *browseModel, idx int) {
	m.details[m.commits[idx].SHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: ""},
	}
}

func TestBrowseCursorNavigation(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Errorf("j 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("k")
	m.handleKey("k") // 先頭で止まる
	if m.cursor != 0 {
		t.Errorf("k 連打後の cursor = %d; want 0", m.cursor)
	}
	// emacs 風の Ctrl-N / Ctrl-P でも移動できる
	m.handleKey("ctrl+n")
	if m.cursor != 1 {
		t.Errorf("ctrl+n 後の cursor = %d; want 1", m.cursor)
	}
	m.handleKey("ctrl+p")
	if m.cursor != 0 {
		t.Errorf("ctrl+p 後の cursor = %d; want 0", m.cursor)
	}
	m.handleKey("G")
	if m.cursor != 2 {
		t.Errorf("G 後の cursor = %d; want 2", m.cursor)
	}
	m.handleKey("g")
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("g 後の cursor/offset = %d/%d; want 0/0", m.cursor, m.offset)
	}
}

func TestBrowseWrapUsesFullWidth(t *testing.T) {
	// カーソル溝の廃止 (git log と左マージンを揃える) 後は端末の全幅で折り返す。
	// 折り返し幅と clip 幅がずれると全幅の行の末尾が「…」に食われる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.commits[0].Message = strings.Repeat("あ", 38) // 表示幅 76 (旧実装だと 1 行に収まり溝で溢れる)
	m.height = 30
	view := m.View()
	if got := strings.Count(view, "あ"); got != 38 {
		t.Errorf("折り返しで文字が欠けた: あ が %d 文字 (want 38)\n%s", got, view)
	}
	for line := range strings.SplitSeq(view, "\n") {
		if w := runewidth.StringWidth(stripANSI(line)); w > m.width {
			t.Errorf("幅超過 (%d > %d): %q", w, m.width, line)
		}
	}
}

func TestBrowseCursorScrollsViewport(t *testing.T) {
	// medium 形式 1 コミット ≈ 6 行 × 5 件 > 高さ 10 なので、下へ移動すると offset が進む
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	for range 4 {
		m.handleKey("j")
	}
	if m.offset == 0 {
		t.Errorf("末尾コミットへ移動しても offset が 0 のまま")
	}
}

// job パネル表示中は 3 秒間隔で状態を取り直す (経過時間のライブ監視。ユーザー要望)。
// in-flight refresh 中にパネルを閉じても panelRefresh が stuck true にならず、以降の
// パネルのライブ更新が止まらない (レビュー C2/C3/K1 の回帰)。
func TestBrowsePanelRefreshLatchClearedOnClose(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	m.details[sha] = running
	// パネルを開く (details キャッシュ済み → fetch 無し・running job なので poll 予約)
	m.openPanel()
	// poll 発火 → refresh 起動 (panelRefresh=true, refresh Cmd が in-flight)
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd == nil || !m.panelRefresh {
		t.Fatal("poll で refresh が起動しない")
	}
	// refresh 完了前にパネルを閉じる → latch は必ず下りる
	m.closePanel()
	if m.panelRefresh {
		t.Fatal("closePanel で panelRefresh が下りない (stuck-latch)")
	}
	// 遅延到着した旧 refresh の detailMsg (sha != panelSHA) は panelRefresh を触らない
	m.Update(detailMsg{sha: sha, batch: CIBatch{Details: map[string][]CheckDetail{sha: running}}})
	if m.panelRefresh {
		t.Fatal("閉じた後の遅延 detailMsg で panelRefresh が復活した")
	}
	// 同じ (キャッシュ済み) コミットを開き直すと poll が実際に refresh を起動できる
	m.openPanel()
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd == nil || !m.panelRefresh {
		t.Fatal("再オープン後の poll で refresh が起動しない (latch stuck の疑い)")
	}
}

// j/k でビューポートがコミット単位に動くとき、表示 offset を glide させる (ユーザー要望)。
func TestBrowseScrollAnim(t *testing.T) {
	m := newTestBrowse(t, 6, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.height = 10 // pageSize 9。medium 1 コミット ~5 行 + 行間で数 j 目にスクロールが起きる
	scrolled := false
	for range 5 {
		prev := m.offset
		_, cmd := m.handleKey("j")
		if m.offset == prev {
			if m.scrollAnim {
				t.Fatal("画面内のカーソル移動で scrollAnim が立った")
			}
			continue
		}
		// ビューポートが動いた最初の j: glide 開始
		if !m.scrollAnim || m.offsetShown != prev || cmd == nil {
			t.Fatalf("スクロール開始で glide が仕込まれない: scrollAnim=%v offsetShown=%d prev=%d cmd=%v",
				m.scrollAnim, m.offsetShown, prev, cmd != nil)
		}
		scrolled = true
		break
	}
	if !scrolled {
		t.Fatal("6 コミットで一度もスクロールしなかった (テスト前提の破れ)")
	}
	// 連打: glide 中の次の j は積まず即スナップ (押した分だけ遅延する体感を避ける)
	m.handleKey("j")
	if m.scrollAnim {
		t.Fatal("glide 中の j で scrollAnim が積まれた (即スナップのはず)")
	}
}

// 背高コミット (大きな offset ジャンプ) でも 1 コミット移動なら glide する
// (行数キャップ撤去の回帰: 以前は >12 行で snap してコミット高で挙動が変わっていた)。
func TestBrowseScrollAnimNoHeightCap(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.offset = 40 // ensureCursorVisible が背高コミットで飛ばした後を想定した大ジャンプ
	cmd := m.startScrollAnim(0)
	if !m.scrollAnim || m.offsetShown != 0 || cmd == nil {
		t.Fatalf("大ジャンプが animate されない: scrollAnim=%v offsetShown=%d cmd=%v",
			m.scrollAnim, m.offsetShown, cmd != nil)
	}
}

// advanceScroll は上下どちらの向きでも tick で表示 offset を論理 offset へ寄せ、
// 有限フレームで着地して scrollAnim を下ろす (geometry 非依存に決定的検証)。
func TestBrowseScrollAnimConverges(t *testing.T) {
	for _, tc := range []struct{ from, to int }{
		{from: 0, to: 7}, // 下スクロール (1 コミット ~7 行)
		{from: 7, to: 0}, // 上スクロール
		{from: 3, to: 4}, // 残り 1 行
		{from: 5, to: 5}, // 動きなし → 即座に scrollAnim を下ろす
	} {
		m := newTestBrowse(t, 6, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.offsetShown = tc.from
		m.scrollFrom = tc.from
		m.scrollFrame = 0
		m.offset = tc.to
		m.scrollAnim = true
		prevShown := m.offsetShown
		frames := 0
		for m.scrollAnim {
			m.advanceScroll()
			frames++
			// ease-in: 表示 offset は目標を通り越さず単調に近づく
			if (tc.to > tc.from && (m.offsetShown < prevShown || m.offsetShown > tc.to)) ||
				(tc.to < tc.from && (m.offsetShown > prevShown || m.offsetShown < tc.to)) {
				t.Fatalf("from=%d to=%d: 非単調/行き過ぎ (offsetShown=%d)", tc.from, tc.to, m.offsetShown)
			}
			prevShown = m.offsetShown
			if frames > 20 {
				t.Fatalf("from=%d to=%d: 収束しない (offsetShown=%d)", tc.from, tc.to, m.offsetShown)
			}
		}
		if m.offsetShown != tc.to {
			t.Errorf("from=%d to=%d: 着地 offsetShown=%d, want %d", tc.from, tc.to, m.offsetShown, tc.to)
		}
		if frames > scrollAnimFrames {
			t.Errorf("from=%d to=%d: %d フレーム (scrollAnimFrames=%d 以内のはず)", tc.from, tc.to, frames, scrollAnimFrames)
		}
	}
}

// 80ms tick は single-flight: チェーンは常に高々 1 本 (レビュー C1)。
func TestBrowseTickSingleFlight(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.visible = false // usage 取得中も spinnerActive になるため、tick 単体の検証から隔離する
	if m.maybeTick() == nil || !m.ticking {
		t.Fatal("初回 maybeTick が tick を返さない / ticking が立たない")
	}
	if m.maybeTick() != nil {
		t.Fatal("チェーンが生きているのに 2 本目の maybeTick が非 nil (二重チェーン)")
	}
	// tickMsg 到着で 1 拍消費 → spinnerActive なら 1 本だけ再アーム
	m.fetching = true
	if _, cmd := m.Update(tickMsg{}); cmd == nil || !m.ticking {
		t.Fatal("spinnerActive 中の tickMsg で再アームされない")
	}
	// spinnerActive でなくなれば再アームせずチェーンは死ぬ
	m.fetching = false
	if _, cmd := m.Update(tickMsg{}); cmd != nil || m.ticking {
		t.Fatalf("非 spinnerActive で tick が止まらない: cmd=%v ticking=%v", cmd != nil, m.ticking)
	}
}

// pull アニメは 1 tickMsg = offset 1 減算 (複数チェーン併存による 2 倍速化の回帰・C1)。
func TestBrowseTickPullAnimOncePerTick(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.height = 6 // 短すぎない (offset を持てる)
	m.pullAnimating = true
	m.offset = 3
	m.Update(tickMsg{})
	if m.offset != 2 {
		t.Fatalf("1 tickMsg で offset=%d (want 2 = 1 回だけ減算)", m.offset)
	}
	m.Update(tickMsg{})
	if m.offset != 1 {
		t.Fatalf("2 tickMsg 後 offset=%d (want 1)", m.offset)
	}
}

// tickMsg の list 無効化は fetch/pushPoll のときだけ (レビュー C7)。
func TestBrowseTickInvalidateGate(t *testing.T) {
	// fetching 中はリストの loading スピナーが動くので無効化する
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.fetching = true
	m.linesValid = true
	m.Update(tickMsg{})
	if m.linesValid {
		t.Error("fetching 中の tickMsg でリストが無効化されない")
	}
	// pullAnimating だけ (fetch/pushPoll 無し) では list 内容は不変なので無効化しない
	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m2.height = 6
	m2.pullAnimating = true
	m2.offset = 3
	m2.linesValid = true
	m2.Update(tickMsg{})
	if !m2.linesValid {
		t.Error("pullAnimating のみで list が無効化された (C7 gate 破れ = 毎フレーム全行再構築)")
	}
}

func TestBrowsePanelPolling(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	m.details[sha] = running
	// 実行中 job があるパネルを開く → ポーリング timer が仕掛かる
	if cmd := m.openPanel(); cmd == nil {
		t.Fatal("実行中 job ありでポーリング timer が仕掛からない")
	}
	seq := m.panelPollSeq
	// 世代一致の poll → リフレッシュ (panelRefresh) + 次回予約
	_, cmd := m.Update(panelPollMsg{seq: seq})
	if cmd == nil || !m.panelRefresh {
		t.Fatalf("poll でリフレッシュが走らない: cmd=%v refresh=%v", cmd != nil, m.panelRefresh)
	}
	// リフレッシュ中の poll は fetch を重ねない (timer 予約のみ = panelRefresh のまま)
	m.Update(panelPollMsg{seq: seq})
	if !m.panelRefresh {
		t.Fatal("リフレッシュ中の poll で状態が壊れた")
	}
	// リフレッシュ結果の到着: panelRefresh が解除され、job 縮小でカーソルがクランプされる
	m.panelCursor = 0
	m.Update(detailMsg{sha: sha, batch: CIBatch{Details: map[string][]CheckDetail{sha: {}}}})
	if m.panelRefresh {
		t.Fatal("detailMsg で panelRefresh が解除されない")
	}
	if m.panelCursor != -1 {
		t.Fatalf("job 0 件への縮小でカーソルがクランプされない: %d", m.panelCursor)
	}
	// 全 job 完了 (実行中なし) の poll はポーリングを止める
	m.details[sha] = []CheckDetail{{Name: "build", State: StateSuccess}}
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd != nil {
		t.Fatal("全 job 完了後も poll が続く")
	}
	// パネルを閉じた後の残タイマー (旧世代) は無視される
	m.details[sha] = running
	m.closePanel()
	if _, cmd := m.Update(panelPollMsg{seq: seq}); cmd != nil {
		t.Fatal("閉じた後の残タイマーが有効になっている")
	}
}

func TestBrowsePanelOpenClose(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("詳細取得済みなのに fetch Cmd が返った")
	}
	if m.panelSHA != m.commits[0].SHA {
		t.Fatalf("パネルが開いていない")
	}
	view := m.View()
	for _, want := range []string{"CI jobs:", "✓ build", "✗ lint"} {
		if !strings.Contains(view, want) {
			t.Errorf("パネルに %q が出ていない:\n%s", want, view)
		}
	}
	// h で閉じる
	m.handleKey("h")
	if m.panelSHA != "" {
		t.Errorf("h で閉じない")
	}
	// esc でも閉じる (アプリ終了にはならない)
	m.openPanel()
	m.handleKey("esc")
	if m.panelSHA != "" || m.done {
		t.Errorf("esc でパネルだけ閉じるべき: panelSHA=%q done=%v", m.panelSHA, m.done)
	}
}

func TestBrowsePanelEnterToggles(t *testing.T) {
	// Enter は popup の表示・非表示の toggle (ユーザー要望)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.handleKey("enter")
	if m.panelSHA == "" {
		t.Fatalf("Enter でパネルが開かない")
	}
	m.handleKey("enter")
	if m.panelSHA != "" || m.done {
		t.Errorf("Enter 2 回目でパネルが閉じない: panelSHA=%q done=%v", m.panelSHA, m.done)
	}
}

func TestBrowsePanelAnchoredAtCommit(t *testing.T) {
	// パネルは一律上部でなく、対象コミットのヘッダー行直下に出る (ユーザー要望)
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.height = 40 // 3 コミットが全部見える高さ
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 1)
	m.handleKey("j") // 2 番目のコミットへ
	m.openPanel()
	view := strings.Split(m.View(), "\n")
	headerIdx, panelIdx := -1, -1
	for i, line := range view {
		if strings.Contains(line, "commit "+m.commits[1].SHA) {
			headerIdx = i
		}
		if panelIdx == -1 && strings.Contains(line, "CI jobs:") {
			panelIdx = i
		}
	}
	if headerIdx == -1 || panelIdx == -1 {
		t.Fatalf("ヘッダー行 (%d) かパネル (%d) が見つからない:\n%s", headerIdx, panelIdx, m.View())
	}
	if panelIdx != headerIdx+1 {
		t.Errorf("パネル位置 = %d 行目; want ヘッダー直下 %d 行目:\n%s", panelIdx, headerIdx+1, m.View())
	}
}

func TestBrowsePanelClampedToViewport(t *testing.T) {
	// 対象コミットが画面下部でも、パネルはビューポート内へ収まる位置に出る
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 4)
	m.handleKey("G") // 末尾コミットへ (ヘッダーはビューポート下端付近)
	m.openPanel()
	view := m.View()
	if !strings.Contains(view, "CI jobs:") || !strings.Contains(view, "✓ build") {
		t.Errorf("下端のコミットでパネルが見えていない:\n%s", view)
	}
	if got := strings.Count(view, "\n"); got+1 > m.pageSize()+1 {
		t.Errorf("パネルでビューポートが伸びた: %d 行", got+1)
	}
}

func TestBrowsePanelKeepsListHeight(t *testing.T) {
	// パネルはリストへ行を差し込まず上へ重ねるため、View の行数は開閉で変わらない
	// (高さのガタつき防止: ユーザー要望の回帰テスト)
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	before := strings.Count(m.View(), "\n")
	m.openPanel()
	after := strings.Count(m.View(), "\n")
	if before != after {
		t.Errorf("パネル開閉で View の行数が変わった: %d → %d", before, after)
	}
}

func TestBrowsePanelJobCursorAndOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	if m.panelCursor != -1 {
		t.Fatalf("開いた直後のフォーカスはタイトル行 (-1) のはず: %d", m.panelCursor)
	}
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	// j で job0 にフォーカスして o → ブラウザで開く (Enter は TUI 内の詳細に使う)
	m.handleKey("j")
	if m.panelCursor != 0 {
		t.Fatalf("j 後の panelCursor = %d; want 0", m.panelCursor)
	}
	_, cmd := m.handleKey("o")
	if cmd == nil {
		t.Fatalf("job フォーカス中の o で Cmd が返らない")
	}
	if msg := cmd(); msg.(openURLMsg).err != nil {
		t.Fatalf("openURLMsg.err = %v", msg.(openURLMsg).err)
	}
	if opened != "https://github.com/o/r/runs/1" {
		t.Errorf("開いた URL = %q", opened)
	}
	// j で job1 へ (末尾で止まる)。URL なし job は notice を出して開かない
	m.handleKey("j")
	m.handleKey("j")
	if m.panelCursor != 1 {
		t.Errorf("panelCursor = %d; want 1", m.panelCursor)
	}
	_, cmd = m.handleKey("o")
	if cmd != nil {
		t.Errorf("URL なし job で Cmd が返った")
	}
	if !strings.Contains(m.hintLine(), "URL がありません") {
		t.Errorf("notice が hint に出ていない: %q", m.hintLine())
	}
	// k でタイトル行まで戻れば Enter は「閉じる」に戻る
	m.handleKey("k")
	m.handleKey("k")
	if m.panelCursor != -1 {
		t.Fatalf("k で -1 に戻らない: %d", m.panelCursor)
	}
	m.handleKey("enter")
	if m.panelSHA != "" {
		t.Errorf("タイトル行フォーカスの Enter で閉じない")
	}
}

func TestBrowseEnterOpensDetailAndToggles(t *testing.T) {
	// job 行の Enter は TUI 内の詳細ポップアップの開閉 toggle (ブラウザは o)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	_, cmd := m.handleKey("enter")
	if cmd == nil || !m.detailOpen {
		t.Fatalf("job 行の Enter で詳細が開かない (cmd=%v detailOpen=%v)", cmd, m.detailOpen)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"line"}})
	m.handleKey("enter")
	if m.detailOpen {
		t.Errorf("詳細表示中の Enter で閉じない (toggle)")
	}
	if m.panelSHA == "" || m.panelCursor != 0 {
		t.Errorf("詳細を閉じた後 job フォーカスに戻らない: panelSHA=%q cursor=%d", m.panelSHA, m.panelCursor)
	}
}

func TestBrowsePanelTriggersDetailFetch(t *testing.T) {
	// キャッシュヒットで詳細が無い SHA のパネルはオンデマンド取得になる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	// usage オーバーレイ (取得中は自身が "取得中..." を描く) を隔離しないと、パネルの
	// ローディング指標が壊れても overlay の "取得中" で下の曖昧 assert が通ってしまう
	// (マスク。レビュー指摘 2026-07-21)。openPanel を handleKey 経由で呼ばないため overlay が
	// 自動 dismiss されないので明示的に消す。
	m.usageOv.visible = false
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess // キャッシュ由来 (details なし)
	cmd := m.openPanel()
	if cmd == nil {
		t.Fatalf("詳細未取得なのに fetch Cmd が返らない")
	}
	if !m.detailsLoading[sha] {
		t.Errorf("detailsLoading が立っていない")
	}
	if !strings.Contains(m.View(), "取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View())
	}
	// 取得完了メッセージで反映される
	m.Update(detailMsg{sha: sha, batch: CIBatch{
		Statuses: map[string]CIState{sha: StateSuccess},
		Details:  map[string][]CheckDetail{sha: {{Name: "build", State: StateSuccess}}}}})
	if m.detailsLoading[sha] {
		t.Errorf("取得完了後も loading のまま")
	}
	if !strings.Contains(m.View(), "✓ build") {
		t.Errorf("取得した詳細がパネルに出ていない:\n%s", m.View())
	}
	if m.fetched[sha] != StateSuccess {
		t.Errorf("詳細取得の状態がキャッシュ保存対象 (fetched) に入っていない")
	}
}

func TestBrowsePanelDuringBatchFetchWaits(t *testing.T) {
	// 一括取得中にその対象 SHA のパネルを開いても、重複リクエストは打たず結果を待つ
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("一括取得中の SHA に重複 fetch Cmd が返った")
	}
	if !m.detailsLoading[shas[0]] {
		t.Errorf("待機中の loading 表示が立っていない")
	}
	// 一括取得の完了で loading が解除され details が表示される
	m.Update(ciResultMsg{batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateSuccess},
		Details:  map[string][]CheckDetail{shas[0]: {{Name: "build", State: StateSuccess}}},
	}})
	if m.detailsLoading[shas[0]] {
		t.Errorf("一括取得完了後も loading のまま")
	}
	if !strings.Contains(m.View(), "✓ build") {
		t.Errorf("一括取得の詳細がパネルに出ていない:\n%s", m.View())
	}
}

func TestBrowseCIResultMergesAndStopsSpinner(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if !m.fetching {
		t.Fatalf("toFetch ありで fetching が立っていない")
	}
	m.Update(ciResultMsg{batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateFailure},
		Details:  map[string][]CheckDetail{shas[0]: {{Name: "lint", State: StateFailure}}},
	}})
	if m.fetching {
		t.Errorf("取得完了後も fetching のまま")
	}
	if m.statuses[shas[0]] != StateFailure || m.fetched[shas[0]] != StateFailure {
		t.Errorf("statuses/fetched に反映されていない: %+v", m.statuses)
	}
}

func TestBrowseQuitFillsUnknown(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	_, cmd := m.handleKey("q")
	if cmd == nil {
		t.Fatalf("q で Quit が返らない")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("q の Cmd が tea.Quit でない: %v", msg)
	}
	if !m.done {
		t.Errorf("done が立っていない")
	}
	if m.statuses[shas[0]] != StateUnknown {
		t.Errorf("取得中断で unknown に落ちていない: %v", m.statuses[shas[0]])
	}
	if m.View() != "" {
		t.Errorf("done 後の View が空でない (TUI 領域が残る): %q", m.View())
	}
}

func TestBrowseBatchedRunesKeyMsg(t *testing.T) {
	// 高速連打で複数キーが 1 つの KeyMsg (Runes="hhq") にまとまっても 1 文字ずつ
	// 処理される (未対応だと q が無視されて終了できない: pty スモークで実測した回帰)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.openJobDetail()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hhq")})
	if !m.done {
		t.Fatalf("hhq のまとめ配送で終了しない (detailOpen=%v panelSHA=%q)", m.detailOpen, m.panelSHA)
	}
	if cmd == nil {
		t.Errorf("q の Quit Cmd が返らない")
	}
}

func TestBrowseQPopsViewStack(t *testing.T) {
	// q はビューのスタックを 1 段戻る (詳細 → job 一覧 → コミット一覧 → 終了)。
	// 即終了は Ctrl-C (ユーザー要望: tig 流)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.jobDetail[m.detailKey()] = []string{"line"}
	m.openJobDetail()
	m.handleKey("q")
	if m.detailOpen || m.panelSHA == "" || m.done {
		t.Fatalf("q 1回目: 詳細だけ閉じるべき (detailOpen=%v panelSHA=%q done=%v)", m.detailOpen, m.panelSHA, m.done)
	}
	m.handleKey("q")
	if m.panelSHA != "" || m.done {
		t.Fatalf("q 2回目: パネルだけ閉じるべき (panelSHA=%q done=%v)", m.panelSHA, m.done)
	}
	_, cmd := m.handleKey("q")
	if cmd == nil || !m.done {
		t.Errorf("q 3回目 (一覧) で終了しない")
	}
}

func TestBrowseCtrlCQuitsAnywhere(t *testing.T) {
	// Ctrl-C はどの階層からでも即終了
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.jobDetail[m.detailKey()] = []string{"line"}
	m.openJobDetail()
	_, cmd := m.handleKey("ctrl+c")
	if cmd == nil || !m.done {
		t.Errorf("詳細表示中の Ctrl-C で即終了しない")
	}
}

func TestBrowseCIResultNegativeCachesUnknown(t *testing.T) {
	// API から結果が返らなかった SHA は unknown 表示 + 負キャッシュ対象 (fetched) に入る
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{batch: emptyBatch()})
	if m.statuses[shas[0]] != StateUnknown {
		t.Errorf("statuses = %v; want unknown", m.statuses[shas[0]])
	}
	if m.fetched[shas[0]] != StateUnknown {
		t.Errorf("unknown が負キャッシュ対象 (fetched) に入っていない")
	}
}

func TestBrowsePanelUnpushedNoFetch(t *testing.T) {
	// 未 push の SHA のパネルは GitHub へ問い合わせない
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateUnpushed
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("未 push SHA で fetch Cmd が返った")
	}
	if !strings.Contains(m.View(), "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View())
	}
}

func TestBrowseOpenJobRejectsNonHTTP(t *testing.T) {
	// targetUrl は外部 CI が任意に設定できるため http(s) 以外は開かない
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	m.details[sha] = []CheckDetail{{Name: "evil", State: StateSuccess, URL: "file:///etc/passwd"}}
	m.openPanel()
	m.handleKey("j")
	called := false
	orig := openInBrowser
	openInBrowser = func(string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	if _, cmd := m.handleKey("o"); cmd != nil {
		t.Errorf("file:// URL で Cmd が返った")
	}
	if called {
		t.Errorf("file:// URL がブラウザに渡された")
	}
	if !strings.Contains(m.hintLine(), "http(s) 以外") {
		t.Errorf("notice が出ていない: %q", m.hintLine())
	}
}

// job 詳細ログを v キーで nvim (stdin 渡し) に開く。ANSI は除去し、ファイルは残さない。
func TestBrowseJobLogOpenInEditor(t *testing.T) {
	// jobLogText: ANSI 除去 + 各行 + 改行
	got := jobLogText([]string{ansiGreen + "ok" + ansiReset, "plain", "\x1b[31mred\x1b[0m line"})
	if got != "ok\nplain\nred line\n" {
		t.Fatalf("jobLogText = %q", got)
	}

	// v キー: 詳細表示中に nvim 起動コマンドを組む (実起動はスタブで捕捉)
	var captured *exec.Cmd
	orig := runEditorCmd
	runEditorCmd = func(cmd *exec.Cmd) tea.Cmd {
		captured = cmd
		return func() tea.Msg { return editorClosedMsg{} }
	}
	t.Cleanup(func() { runEditorCmd = orig })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.detailOpen = true
	m.panelCursor = 0
	m.jobDetail[m.detailKey()] = []string{ansiRed + "boom" + ansiReset, "at foo.go:10"}

	_, cmd := m.handleKey("v")
	if cmd == nil || captured == nil {
		t.Fatal("v で nvim 起動コマンドが組まれない")
	}
	// nvim -R ... - (readonly、stdin から読む)
	if captured.Args[0] != "nvim" || captured.Args[len(captured.Args)-1] != "-" {
		t.Fatalf("nvim ... - で起動していない: %v", captured.Args)
	}
	if !slices.Contains(captured.Args, "-R") {
		t.Fatalf("readonly (-R) で開いていない: %v", captured.Args)
	}
	// stdin に ANSI 除去済みログが載っている (ファイルは作らない)
	buf, _ := io.ReadAll(captured.Stdin)
	if string(buf) != "boom\nat foo.go:10\n" {
		t.Fatalf("stdin の中身 = %q", string(buf))
	}
	// エラーで閉じたら notice、成功なら無し
	m.Update(editorClosedMsg{err: errors.New("nvim: not found")})
	if !strings.Contains(m.notice, "nvim を開けません") {
		t.Errorf("nvim 起動失敗の notice が出ない: %q", m.notice)
	}

	// ログが空なら起動しない
	m.jobDetail[m.detailKey()] = nil
	if _, cmd := m.handleKey("v"); cmd != nil {
		t.Error("空ログで nvim を起動しようとした")
	}
}

func TestBrowseJobDetailPopup(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j") // job0 へ
	// l で詳細ポップアップ (未取得なので fetch Cmd が返る)
	_, cmd := m.handleKey("l")
	if cmd == nil {
		t.Fatalf("l で詳細取得 Cmd が返らない")
	}
	if !m.detailOpen || !m.jobDetailBusy[m.detailKey()] {
		t.Fatalf("詳細が開いていない / busy でない")
	}
	if !strings.Contains(m.View(), "詳細を取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View())
	}
	// 取得完了 → 末尾から表示
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("log line %d", i)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: lines})
	rows := m.visibleDetailRows()
	if m.detailOffset != 30-rows {
		t.Errorf("detailOffset = %d; want 末尾表示 %d", m.detailOffset, 30-rows)
	}
	if !strings.Contains(m.View(), "log line 29") {
		t.Errorf("末尾行が見えていない (低い端末でも末尾は見える):\n%s", m.View())
	}
	// 詳細ボックスは job パネルの子であることが分かるよう段差付き (ユーザー要望)
	indented := false
	for line := range strings.SplitSeq(m.View(), "\n") {
		if strings.HasPrefix(stripANSI(line), detailIndent+"┌") {
			indented = true
			break
		}
	}
	if !indented {
		t.Errorf("詳細ボックスに段差がない:\n%s", m.View())
	}
	// k で上へスクロール、g で先頭
	m.handleKey("k")
	if m.detailOffset != 30-rows-1 {
		t.Errorf("k 後の offset = %d", m.detailOffset)
	}
	m.handleKey("g")
	if m.detailOffset != 0 {
		t.Errorf("g 後の offset = %d", m.detailOffset)
	}
	// h で job フォーカスへ戻る (パネルは開いたまま)
	m.handleKey("h")
	if m.detailOpen || m.panelSHA == "" || m.panelCursor != 0 {
		t.Errorf("h 後の状態: detailOpen=%v panelSHA=%q cursor=%d", m.detailOpen, m.panelSHA, m.panelCursor)
	}
	// 再度 l → キャッシュ済みなので fetch なしで即表示
	if _, cmd := m.handleKey("l"); cmd != nil {
		t.Errorf("キャッシュ済み詳細で再 fetch した")
	}
	if !m.detailOpen {
		t.Errorf("2 回目の l で開かない")
	}
}

func TestBrowseCopyURL(t *testing.T) {
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { copyToClipboard = orig })
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	// コミット一覧では commit URL
	m.handleKey("y")
	wantCommit := "https://github.com/o/r/commit/" + m.commits[0].SHA
	if copied != wantCommit {
		t.Errorf("commit URL = %q; want %q", copied, wantCommit)
	}
	// job フォーカス中はその job の URL
	m.openPanel()
	m.handleKey("j")
	m.handleKey("y")
	if copied != "https://github.com/o/r/runs/1" {
		t.Errorf("job URL = %q", copied)
	}
	if !strings.Contains(m.hintLine(), "コピーしました") {
		t.Errorf("notice が出ていない: %q", m.hintLine())
	}
	// URL なし job (job1) は notice
	m.handleKey("j")
	m.handleKey("y")
	if !strings.Contains(m.hintLine(), "コピーできる URL がありません") {
		t.Errorf("URL なしの notice が出ていない: %q", m.hintLine())
	}
}

func TestBrowseBatchPRsFeedPCache(t *testing.T) {
	// 一括取得の PR は p キーのキャッシュとコミット行バッジの両方に合流する
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateSuccess},
		Details:  map[string][]CheckDetail{},
		PRs:      map[string]*PRRef{shas[0]: {Number: 7, URL: "https://github.com/o/r/pull/7", State: "MERGED"}},
	}})
	var opened string
	orig := openInBrowser
	openInBrowser = func(u string) error {
		opened = u
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
	// 再取得なしで即 open
	_, cmd := m.handleKey("p")
	if cmd == nil || m.prBusy[shas[0]] {
		t.Fatalf("バッチ由来の PR キャッシュで即 open にならない")
	}
	cmd()
	if opened != "https://github.com/o/r/pull/7" {
		t.Errorf("URL = %q", opened)
	}
	// バッジも View に出る
	if !strings.Contains(m.View(), "#7") {
		t.Errorf("PR バッジが View に出ていない:\n%s", m.View())
	}
}

func TestBrowsePanelShowsJobDuration(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.details[sha] = []CheckDetail{{Name: "dotfiles-tests", State: StateFailure, Duration: 2*time.Minute + 39*time.Second}}
	m.openPanel()
	if !strings.Contains(m.View(), "(2m39s)") {
		t.Errorf("job 行に所要時間が出ていない:\n%s", m.View())
	}
}

func TestBrowsePanelShowsRunningElapsed(t *testing.T) {
	orig := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.visible = false // 右上 usage モーダルの "残り / リセット" 見出しが「残り」不在アサートに紛れるのを避ける
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	// 開始 90 秒前・ETA basis なし (履歴が画面に無い) → 経過時間だけ出る
	m.details[sha] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(910, 0)}}
	m.openPanel()
	view := m.View()
	if !strings.Contains(view, "1m30s 経過") {
		t.Errorf("実行中 job の経過時間が出ていない:\n%s", view)
	}
	if strings.Contains(view, "残り") {
		t.Errorf("basis が無いのに ETA が出ている:\n%s", view)
	}
	if !m.spinnerActive() {
		t.Error("実行中 job がある間は tick を回して経過をライブ更新すべき")
	}
}

func TestBrowsePanelShowsRunningETA(t *testing.T) {
	orig := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	running, prev := m.commits[0].SHA, m.commits[1].SHA
	m.statuses[running] = StatePending
	m.statuses[prev] = StateSuccess
	// 実行中 job: 開始 60 秒前。直近の同名完了 job は 100 秒 → 残り ~40s
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(940, 0)}}
	m.details[prev] = []CheckDetail{{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}
	m.openPanel()
	view := m.View()
	if !strings.Contains(view, "1m00s 経過") {
		t.Errorf("経過時間が出ていない:\n%s", view)
	}
	if !strings.Contains(view, "残り ~40s") {
		t.Errorf("ETA (残り ~40s) が出ていない:\n%s", view)
	}
}

func TestBrowsePanelRunningETAOverrun(t *testing.T) {
	orig := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	running, prev := m.commits[0].SHA, m.commits[1].SHA
	m.statuses[running] = StatePending
	// 経過 120 秒 > 前回 100 秒 → 予定超過
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(880, 0)}}
	m.details[prev] = []CheckDetail{{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}
	m.openPanel()
	if !strings.Contains(m.View(), "予定超過") {
		t.Errorf("前回所要時間を超えたら予定超過を出すべき:\n%s", m.View())
	}
}

func TestBrowseRunningETASkipsCancelled(t *testing.T) {
	orig := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	running := m.commits[0].SHA
	m.statuses[running] = StatePending
	// 実行中: 開始 60 秒前
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(940, 0)}}
	// 直近 (近い側): cancel された同名 job (Duration>0 だが StateNeutral) → basis に使わない
	m.details[m.commits[1].SHA] = []CheckDetail{{Name: "build", State: StateNeutral, Duration: 3 * time.Second}}
	// その先: 正常完了 100 秒 → こちらを basis にして残り ~40s
	m.details[m.commits[2].SHA] = []CheckDetail{{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}
	m.openPanel()
	view := m.View()
	if strings.Contains(view, "予定超過") {
		t.Errorf("cancel run (3s) を basis に拾って誤って超過判定している:\n%s", view)
	}
	if !strings.Contains(view, "残り ~40s") {
		t.Errorf("cancel をスキップして正常完了 (100s) を basis にすべき:\n%s", view)
	}
}

func TestBrowseRunningETAFetchesMissingBasis(t *testing.T) {
	orig := timeNow
	timeNow = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.usageOv.visible = false // 右上 usage モーダルの "残り / リセット" 見出しが「残り」不在アサートに紛れるのを避ける
	running, prev := m.commits[0].SHA, m.commits[1].SHA
	m.statuses[running] = StatePending
	m.statuses[prev] = StateSuccess // 完了コミット: cache ヒット相当で Details 未取得
	// 開き直し後の状態: pending は再取得され details あり、完了コミットは Details 無し
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(940, 0)}}

	cmd := m.openPanel()
	if cmd == nil {
		t.Fatal("basis 未取得の完了コミットがあるのに補充 fetch が仕掛けられていない")
	}
	if !m.detailsLoading[prev] {
		t.Errorf("完了コミットを basis 取得対象にしていない")
	}
	if strings.Contains(m.View(), "残り") {
		t.Errorf("basis 未着なのに ETA が出ている:\n%s", m.View())
	}
	// basis (prev の完了 job 100s) が届く → 残り ~40s
	m.Update(basisMsg{targets: []string{prev}, batch: CIBatch{
		Statuses: map[string]CIState{prev: StateSuccess},
		Details:  map[string][]CheckDetail{prev: {{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}},
		PRs:      map[string]*PRRef{},
	}})
	if !strings.Contains(m.View(), "残り ~40s") {
		t.Errorf("basis 補充後に ETA が出ていない:\n%s", m.View())
	}
	if m.detailsLoading[prev] {
		t.Error("basisMsg 到着後も loading が解除されていない")
	}
}

func TestBrowseETABasisFillsEmptyToStopRefetch(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	prev := m.commits[1].SHA
	// target を要求したが応答に details が無い (GitHub 上に無い等) → 空スライスで確定させ、
	// 同じ target を無限に取り直さないこと
	m.detailsLoading[prev] = true
	m.Update(basisMsg{targets: []string{prev}, batch: CIBatch{
		Statuses: map[string]CIState{},
		Details:  map[string][]CheckDetail{},
		PRs:      map[string]*PRRef{},
	}})
	if _, ok := m.details[prev]; !ok {
		t.Error("応答に無かった target の Details が確定されず、再取得ループの余地が残る")
	}
	if m.detailsLoading[prev] {
		t.Error("loading が解除されていない")
	}
}

func TestBrowseOpenPR(t *testing.T) {
	var opened string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })
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
	cmd()
	if opened != "https://github.com/o/r/pull/12" {
		t.Errorf("開いた URL = %q", opened)
	}
	// キャッシュ済みなので 2 回目の p は再取得せず即 open
	_, cmd = m.handleKey("p")
	if cmd == nil || m.prBusy[sha] {
		t.Errorf("キャッシュ済み PR で即 open にならない")
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
	if !strings.Contains(m.hintLine(), "PR の取得に失敗") {
		t.Errorf("エラー notice が出ない: %q", m.hintLine())
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
	if !strings.Contains(m.hintLine(), "PR はありません") {
		t.Errorf("PR なしの notice が出ない: %q", m.hintLine())
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
	if !strings.Contains(m.hintLine(), "未 push") {
		t.Errorf("未 push の notice が出ない: %q", m.hintLine())
	}
}

func TestBrowseNonGitHubRepoPanel(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.hasRepo = false
	sha := m.commits[0].SHA
	m.statuses[sha] = StateNone
	if cmd := m.openPanel(); cmd != nil {
		t.Errorf("GitHub 以外の remote で fetch Cmd が返った")
	}
	if !strings.Contains(m.View(), "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View())
	}
}

func TestBrowseLinesMemoized(t *testing.T) {
	// 行リストはカーソル移動やパネル開閉で再構築しない (メモ化)。-p の巨大 patch で
	// キー 1 打ごとに全行を組み直す計算量爆発を防ぐ
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	first := m.lines()
	m.handleKey("j")
	m.handleKey("ctrl+d")
	withJobs(m, 0)
	m.openPanel()
	if second := m.lines(); &first[0] != &second[0] {
		t.Errorf("カーソル移動・パネル開閉で行リストが再構築された")
	}
	// 状態を変える更新 (CI 結果のマージ) では再構築される
	sha := m.commits[0].SHA
	m.Update(ciResultMsg{batch: CIBatch{Statuses: map[string]CIState{sha: StateFailure}}})
	rebuilt := m.lines()
	if &first[0] == &rebuilt[0] {
		t.Errorf("CI 結果反映後も古い行リストのまま")
	}
	// 先頭は all pushed マークになりうるため、ヘッダー行 (CommitIdx 0) で状態を見る
	var header string
	for _, l := range rebuilt {
		if l.Header && l.CommitIdx == 0 {
			header = l.Text
			break
		}
	}
	if !strings.Contains(header, "✗") {
		t.Errorf("再構築後の行に新しい状態が反映されていない: %q", header)
	}
}

func TestBrowsePanelHomeKeyOnEmptyJobs(t *testing.T) {
	// job 0 件のパネルで g を押してもタイトル行 (-1) から動かず、Enter で閉じられる
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateNone
	m.details[sha] = []CheckDetail{}
	m.openPanel()
	m.handleKey("g")
	if m.panelCursor != -1 {
		t.Fatalf("空パネルで g がフォーカスを動かした: %d", m.panelCursor)
	}
	m.handleKey("enter")
	if m.panelSHA != "" {
		t.Errorf("空パネルが Enter で閉じない")
	}
}

func TestBuildPanelBoxWidths(t *testing.T) {
	lines := buildPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	if len(lines) != 4 {
		t.Fatalf("枠 + 2 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestBuildShadowPanelBoxWidths(t *testing.T) {
	lines := buildShadowPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	// 枠 (top/bottom) + 2 行 + 下端の落ち影 1 行 = 5 行。影を足しても footprint 幅は 40 のまま
	if len(lines) != 5 {
		t.Fatalf("枠 + 2 行 + 影 1 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestJapanesePanelBoxWidths(t *testing.T) {
	// 全角の job 名・タイトルでも罫線の幅が揃う (全角境界の切り詰め込み)
	rows := []string{
		"❯ ✓ テストジョブ (日本語)",
		"  ✗ " + strings.Repeat("長", 40), // inner を超えて全角境界で切り詰められる
	}
	lines := buildPanelBox(" CI jobs: abc1234 日本語のサブジェクトがとても長い場合の切り詰め ", rows, 40, true)
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestJapaneseFullViewStaysInWidth(t *testing.T) {
	// subject・message・diff 本文・job 名・詳細ログの全部が日本語でも View の全行が幅内
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.height = 40
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.commits[0].Subject = "日本語のサブジェクト: 表示崩れの検証"
	m.commits[0].Message = "日本語のサブジェクト: 表示崩れの検証\n\n" + strings.Repeat("本文の長い日本語テキスト。", 20)
	m.commits[0].Body = "+\t日本語のコード行 := \"値\"\n-\tもう一行の日本語\n"
	m.details[sha] = []CheckDetail{
		{Name: "テスト (ユニット)", State: StateFailure, URL: "https://github.com/o/r/runs/1"},
		{Name: strings.Repeat("長いジョブ名", 15), State: StateSuccess},
	}
	m.openPanel()
	m.handleKey("j")
	m.jobDetail[m.detailKey()] = []string{
		strings.Repeat("日本語のログ行です。", 12),
		"##[error]日本語のエラーメッセージ",
	}
	m.openJobDetail()
	for line := range strings.SplitSeq(m.View(), "\n") {
		if w := runewidth.StringWidth(stripANSI(line)); w > m.width {
			t.Errorf("幅超過 (%d > %d): %q", w, m.width, line)
		}
	}
	if !strings.Contains(m.View(), "テスト (ユニット)") {
		t.Errorf("日本語 job 名が表示されていない:\n%s", m.View())
	}
}

func TestBuildPanelBoxTitleStripsANSI(t *testing.T) {
	// SGR 入りの job 名/subject がタイトルに載っても罫線幅と dim 塗りを崩さない
	lines := buildPanelBox(" \x1b[31mred job\x1b[0m ", []string{"row"}, 40, false)
	if strings.Contains(lines[0], "\x1b") {
		t.Errorf("タイトルに ANSI が残っている: %q", lines[0])
	}
	if w := runewidth.StringWidth(lines[0]); w != 40 {
		t.Errorf("タイトル行の幅 = %d; want 40: %q", w, lines[0])
	}
}

// --- diff ポップアップ (d キー) ---

// stubDiff は loadCommitDiff を差し替え、呼び出し記録と固定行を返す。
func stubDiff(t *testing.T, lines []string, err error) *[]string {
	t.Helper()
	var calls []string
	orig := loadCommitDiff
	loadCommitDiff = func(sha string, colored bool) ([]string, error) {
		calls = append(calls, sha)
		return lines, err
	}
	t.Cleanup(func() { loadCommitDiff = orig })
	return &calls
}

// runCmd は tea.Cmd (tea.Batch 含む) を同期実行して diffMsg を探して Update へ流す。
func deliverDiffMsg(t *testing.T, m *browseModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd が nil (diff 取得コマンドが返っていない)")
	}
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case diffMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
}

func TestBrowseDiffOpenScrollClose(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	diffLines := make([]string, 20)
	for i := range diffLines {
		diffLines[i] = fmt.Sprintf("line-%d", i)
	}
	calls := stubDiff(t, diffLines, nil)

	_, cmd := m.handleKey("d")
	if m.diffSHA != m.commits[0].SHA {
		t.Fatalf("diffSHA = %q; want カーソル位置のコミット", m.diffSHA)
	}
	if !m.diffBusy[m.diffSHA] {
		t.Error("取得中フラグが立っていない")
	}
	deliverDiffMsg(t, m, cmd)
	if len(*calls) != 1 || (*calls)[0] != m.commits[0].SHA {
		t.Fatalf("loadCommitDiff の呼び出し = %v", *calls)
	}
	if m.diffBusy[m.diffSHA] {
		t.Error("取得完了後も busy のまま")
	}
	view := m.View()
	if !strings.Contains(view, "line-0") || !strings.Contains(view, "diff:") {
		t.Fatalf("diff ポップアップが描画されていない:\n%s", view)
	}
	// スクロール: j で 1 行、G で末尾
	m.handleKey("j")
	if m.diffOffset != 1 {
		t.Errorf("j 後の offset = %d; want 1", m.diffOffset)
	}
	m.handleKey("G")
	if m.diffOffset != len(diffLines)-m.visibleDiffRows() {
		t.Errorf("G 後の offset = %d", m.diffOffset)
	}
	// q で閉じる (アプリは終了しない)
	m.handleKey("q")
	if m.diffSHA != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v; want 閉じるのみ", m.diffSHA, m.done)
	}
}

func TestBrowseDiffToggleAndCache(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	calls := stubDiff(t, []string{"x"}, nil)
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	m.handleKey("d") // toggle 閉
	if m.diffSHA != "" {
		t.Fatal("d の再押下で閉じていない")
	}
	_, cmd2 := m.handleKey("d") // 再度開く → キャッシュヒットで再取得しない
	if cmd2 != nil {
		t.Error("キャッシュヒット時にも取得コマンドが返った")
	}
	if len(*calls) != 1 {
		t.Errorf("loadCommitDiff 呼び出し回数 = %d; want 1 (キャッシュ)", len(*calls))
	}
	if m.diffSHA == "" {
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
	if m.diffSHA != m.commits[0].SHA {
		t.Errorf("diffSHA = %q; want panel のコミット", m.diffSHA)
	}
	if m.panelSHA != "" {
		t.Error("diff を開いたら panel は閉じる契約")
	}
}

func TestBrowseDiffErrorShowsNoticeAndCloses(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	stubDiff(t, nil, errors.New("boom"))
	_, cmd := m.handleKey("d")
	deliverDiffMsg(t, m, cmd)
	if m.diffSHA != "" {
		t.Error("取得失敗時にポップアップが開いたまま")
	}
	if !strings.Contains(m.notice, "diff の取得に失敗") {
		t.Errorf("notice = %q", m.notice)
	}
	if strings.Contains(m.hintLine(), "diff の取得に失敗") == false {
		t.Errorf("hint に notice が出ていない: %q", m.hintLine())
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
	if m.diffSHA == "" {
		t.Fatal("Space で閉じた (半ページスクロールのはず)")
	}
	if m.diffOffset == 0 {
		t.Error("Space でスクロールしていない")
	}
	m.handleKey("enter")
	if m.diffSHA == "" {
		t.Fatal("Enter で閉じた (1 行スクロールのはず)")
	}
	// 末尾を大きく超えて送っても最終行位置で止まり、開いたまま
	for range 100 {
		m.handleKey(" ")
	}
	if m.diffSHA == "" {
		t.Fatal("末尾到達後のスクロールで閉じた")
	}
	if m.diffOffset != maxOffset {
		t.Errorf("末尾で offset = %d; want %d (最終行を表示し続ける)", m.diffOffset, maxOffset)
	}
	view := m.View()
	if !strings.Contains(view, diffLines[len(diffLines)-1]) {
		t.Error("末尾で最終行が描画されていない")
	}
	// 閉じるのは q
	m.handleKey("q")
	if m.diffSHA != "" || m.done {
		t.Errorf("q: diffSHA=%q done=%v", m.diffSHA, m.done)
	}
}

// --- o (commit をブラウザで開く) と emacs 水平キー ---

func TestBrowseListOpenCommitURL(t *testing.T) {
	m := newTestBrowse(t, 2, nil, nil)
	var opened []string
	orig := openInBrowser
	openInBrowser = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { openInBrowser = orig })

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
	if m.notice == "" {
		t.Error("repo なしの notice が出ていない")
	}
}

// git log --color は短縮形リセット "\x1b[m" を使う。bgLine (カーソル行の bg 塗り) は
// これでも bg を張り直して行末まで塗れる (literal "\x1b[0m" 一致だけだと途切れる回帰の防止)。
func TestBgLineReappliesAfterShortReset(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.colored = true
	got := m.bgLine("\x1b[33mcommit abc\x1b[m subject", ansiCursorBg)
	if !strings.Contains(got, "\x1b[m"+ansiCursorBg) {
		t.Fatalf("短縮形リセット後に bg が張り直されない: %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) || !strings.Contains(got, "  ") {
		t.Fatalf("行末までの padding が無い: %q", got)
	}
}

// C-f は全ビューで → の別名。C-b の ← 別名は無い (push は b。glogx の独自仕様)。
func TestBrowseEmacsHorizontalAliases(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	withJobs(m, 0)
	// 一覧: C-f = → = パネルを開く
	m.handleKey("ctrl+f")
	if m.panelSHA == "" {
		t.Fatal("C-f でパネルが開かない (right の別名のはず)")
	}
	// C-b は left の別名ではない (未割当で何も起きない)
	m.handleKey("ctrl+b")
	if m.panelSHA == "" {
		t.Fatal("C-b でパネルが閉じた (glogx では未割当のはず)")
	}
}

// b → y/N → git push (glogx の独自機能)。
// push/pull 確認は Enter を y と同じ「実行」として扱う (ユーザー要望 2026-07-21)。
func TestBrowseConfirmEnterConfirms(t *testing.T) {
	// push: Enter で実行
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	var pushed int
	origPush := runGitPush
	runGitPush = func() error { pushed++; return nil }
	t.Cleanup(func() { runGitPush = origPush })
	m.handleKey("b")
	if !m.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	if _, cmd := m.handleKey("enter"); cmd == nil || !m.pushing || m.pushConfirm {
		t.Fatalf("Enter で push が実行されない: cmd=%v pushing=%v confirm=%v", cmd != nil, m.pushing, m.pushConfirm)
	}
	// pull: Enter で実行
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	origPull := runGitPullRebase
	runGitPullRebase = func() error { return nil }
	t.Cleanup(func() { runGitPullRebase = origPull })
	m2.handleKey("u")
	if !m2.pullConfirm {
		t.Fatal("u で pull 確認に入らない")
	}
	if _, cmd := m2.handleKey("enter"); cmd == nil || !m2.pulling || m2.pullConfirm {
		t.Fatalf("Enter で pull が実行されない: cmd=%v pulling=%v confirm=%v", cmd != nil, m2.pulling, m2.pullConfirm)
	}
}

// C → claude update (確認なし即実行。glogx の独自機能)。
func TestBrowseUpdateFlow(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	var calls int
	orig := runClaudeUpdate
	runClaudeUpdate = func() (string, string, error) { calls++; return "2.1.216", "2.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate = orig })

	// C で確認を挟まず即実行 (updating=true & cmd 返却)
	_, cmd := m.handleKey("C")
	if cmd == nil || !m.updating {
		t.Fatalf("C で claude update が始まらない: cmd=%v updating=%v", cmd != nil, m.updating)
	}
	// 実行中は spinner モーダルが出て、終了できない旨も表示する
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "claude update") || !strings.Contains(v, "updating") ||
		!strings.Contains(v, "完了まで終了できません") {
		t.Fatal("claude update 実行中モーダルが描画されない")
	}
	// update 中は Ctrl-G/Ctrl-C で終了できない (自己更新の途中 kill を防ぐ)
	if _, qcmd := m.handleKey("ctrl+g"); qcmd != nil || m.done || !m.updating {
		t.Fatalf("update 中に Ctrl-G で終了してしまう: cmd=%v done=%v", qcmd != nil, m.done)
	}
	// cmd を実行して updateMsg を配送
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case updateMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
	if calls != 1 {
		t.Fatalf("claude update 実行回数 = %d, want 1", calls)
	}
	if m.updating {
		t.Fatal("updateMsg 後も updating のまま")
	}
	// 変わった場合は結果ダイアログに "vX → vY" が出る
	if !strings.Contains(m.updateResult, "v2.1.216 → v2.2.0") {
		t.Fatalf("バージョン変化が結果ダイアログに出ない: %q", m.updateResult)
	}
	// ダイアログは何かキーで閉じる (キーは消費)
	if _, cmd := m.handleKey("j"); cmd != nil || m.updateResult != "" {
		t.Fatalf("結果ダイアログが任意キーで閉じない: cmd=%v result=%q", cmd != nil, m.updateResult)
	}

	// 変わらなかった場合は「変更なし」
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	runClaudeUpdate = func() (string, string, error) { return "2.2.0", "2.2.0", nil }
	_, cmd2 := m2.handleKey("C")
	deliverTo := func(model *browseModel, c tea.Cmd) {
		var dl func(tea.Msg)
		dl = func(msg tea.Msg) {
			switch v := msg.(type) {
			case tea.BatchMsg:
				for _, cc := range v {
					if cc != nil {
						dl(cc())
					}
				}
			case updateMsg:
				model.Update(v)
			}
		}
		dl(c())
	}
	deliverTo(m2, cmd2)
	if !strings.Contains(m2.updateResult, "最新版") || !strings.Contains(m2.updateResult, "v2.2.0") {
		t.Fatalf("最新版が結果ダイアログに出ない: %q", m2.updateResult)
	}
}

// 更新失敗 (runClaudeUpdate が err を返す) 経路: updating が必ず解けて結果ダイアログに
// エラー理由が出る。updateTimeout 超過時のエラーもこの経路を通るため、無限ブロックからの
// 復帰 (updating 解除 → q/Ctrl-C が再び効く) を保証する回帰テスト。
func TestBrowseUpdateFailureShowsDialogAndClearsUpdating(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	orig := runClaudeUpdate
	runClaudeUpdate = func() (string, string, error) {
		return "2.1.216", "", errors.New("claude update がタイムアウトしました (5m0s)")
	}
	t.Cleanup(func() { runClaudeUpdate = orig })

	_, cmd := m.handleKey("C")
	if !m.updating {
		t.Fatal("C で updating に入らない")
	}
	var dl func(tea.Msg)
	dl = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, cc := range v {
				if cc != nil {
					dl(cc())
				}
			}
		case updateMsg:
			m.Update(v)
		}
	}
	dl(cmd())

	if m.updating {
		t.Fatal("更新失敗後も updating のまま (無限ブロックから復帰できない)")
	}
	if !strings.Contains(m.updateResult, "更新に失敗しました") || !strings.Contains(m.updateResult, "タイムアウト") {
		t.Fatalf("失敗理由が結果ダイアログに出ない: %q", m.updateResult)
	}
	// updating が解けたので、結果ダイアログは任意キーで閉じられる (無反応から復帰済み)。
	m.handleKey("q")
	if m.updateResult != "" || m.done {
		t.Fatalf("q で結果ダイアログが閉じない: result=%q done=%v", m.updateResult, m.done)
	}
}

func TestBrowsePushFlow(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.statuses[m.commits[1].SHA] = StateUnpushed // 2 コミットまとめて push するケース
	var pushed int
	orig := runGitPush
	runGitPush = func() error { pushed++; return nil }
	t.Cleanup(func() { runGitPush = orig })
	// b で確認に入り、n でキャンセル (push されない)
	m.handleKey("b")
	if !m.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	// 確認中は中央モーダルが出る (幅より狭いボックス + 左パディングでセンタリング)
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "git push") || !strings.Contains(v, "push します") {
		t.Fatal("push 確認モーダルが描画されない")
	}
	m.handleKey("n")
	if m.pushConfirm || pushed != 0 {
		t.Fatalf("n でキャンセルされない: confirm=%v pushed=%d", m.pushConfirm, pushed)
	}
	// y で push が走り、成功で未 push が unknown へ落ちて再取得に乗る
	m.handleKey("b")
	_, cmd := m.handleKey("y")
	if cmd == nil || !m.pushing {
		t.Fatal("y で push が始まらない")
	}
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case pushMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
	if pushed != 1 {
		t.Fatalf("push 実行回数 = %d, want 1", pushed)
	}
	if m.pushing {
		t.Fatal("pushMsg 後も pushing のまま")
	}
	// push 成功でリスト全体のキャッシュが破棄され、全 SHA が再取得に乗る
	for i, c := range m.commits {
		if _, ok := m.statuses[c.SHA]; ok {
			t.Fatalf("push 成功後も commits[%d] の status キャッシュが残っている", i)
		}
	}
	if !m.fetching || len(m.toFetch) != len(m.commits) {
		t.Fatalf("push 成功で全件再取得に入らない: fetching=%v toFetch=%d", m.fetching, len(m.toFetch))
	}
	// ポーリング対象は tip (最新の unpushed) だけ。途中のコミットには CI が走らないため
	newSHA := m.commits[0].SHA
	if !m.pushPoll[newSHA] {
		t.Fatal("push の tip がポーリング対象にならない")
	}
	if m.pushPoll[m.commits[1].SHA] || len(m.pushPoll) != 1 {
		t.Fatalf("tip 以外までポーリング対象になった: %v", m.pushPoll)
	}
	// tip の「CI がまだ見えない (none)」応答は捨てられ、ネガティブキャッシュに乗らず
	// 再ポーリング。途中コミットの none は本物なので通常どおり残る
	m.Update(ciResultMsg{batch: CIBatch{Statuses: map[string]CIState{
		newSHA: StateNone, m.commits[1].SHA: StateNone,
	}}})
	if _, ok := m.statuses[newSHA]; ok {
		t.Fatal("CI が見えない応答が statuses に残った (スピナーに戻るべき)")
	}
	if _, ok := m.fetched[newSHA]; ok {
		t.Fatal("CI が見えない応答が fetched に残った (ネガティブキャッシュされる)")
	}
	if !m.pushPoll[newSHA] {
		t.Fatal("CI が見えないのにポーリングが止まった")
	}
	if m.statuses[m.commits[1].SHA] != StateNone || m.fetched[m.commits[1].SHA] != StateNone {
		t.Fatal("途中コミットの none (本物) まで捨てられた")
	}
	// pushPollMsg で再取得が走る
	m.fetching = false
	if _, cmd := m.Update(pushPollMsg{}); cmd == nil || !m.fetching {
		t.Fatal("pushPollMsg で再取得が始まらない")
	}
	// CI が見えたら (pending) ポーリング対象から外れ、通常のキャッシュ運用に戻る
	m.Update(ciResultMsg{batch: CIBatch{Statuses: map[string]CIState{newSHA: StatePending}}})
	if m.pushPoll[newSHA] {
		t.Fatal("CI が見えてもポーリングが止まらない")
	}
	if m.statuses[newSHA] != StatePending {
		t.Fatalf("pending が反映されない: %v", m.statuses[newSHA])
	}
	if !strings.Contains(m.notice, "push") {
		t.Fatalf("push 完了 notice が出ない: %q", m.notice)
	}
}

// u → y/N → git pull --rebase → 一覧の全面リロード (glogx の独自機能)。
func TestBrowsePullFlow(t *testing.T) {
	newTempRepo(t, []string{"first", "second"}) // reloadAfterPull が実 git を読むため
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.opts = &Options{MaxCount: 20}
	var pulled int
	orig := runGitPullRebase
	runGitPullRebase = func() error { pulled++; return nil }
	t.Cleanup(func() { runGitPullRebase = orig })
	// u で確認に入り、n でキャンセル
	m.handleKey("u")
	if !m.pullConfirm {
		t.Fatal("u で pull 確認に入らない")
	}
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "pull --rebase") {
		t.Fatal("pull 確認モーダルが描画されない")
	}
	m.handleKey("n")
	if m.pullConfirm || pulled != 0 {
		t.Fatalf("n でキャンセルされない: confirm=%v pulled=%d", m.pullConfirm, pulled)
	}
	// y で pull が走り、成功で一覧が実 repo の内容にリロードされる
	m.handleKey("u")
	_, cmd := m.handleKey("y")
	if cmd == nil || !m.pulling {
		t.Fatal("y で pull が始まらない")
	}
	m.details["stale"] = []CheckDetail{{Name: "old"}}
	m.cursor = 0
	var deliver func(msg tea.Msg)
	deliver = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					deliver(c())
				}
			}
		case pullMsg:
			m.Update(v)
		}
	}
	deliver(cmd())
	if pulled != 1 {
		t.Fatalf("pull 実行回数 = %d, want 1", pulled)
	}
	if m.pulling {
		t.Fatal("pullMsg 後も pulling のまま")
	}
	if len(m.commits) != 2 || m.commits[0].Subject != "second" {
		t.Fatalf("pull 後に一覧がリロードされない: %+v", m.commits)
	}
	if len(m.details) != 0 {
		t.Fatal("pull 後に旧 SHA の details キャッシュが残っている")
	}
	// 失敗は notice に出す (リロードしない)
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	runGitPullRebase = func() error { return errors.New("conflict のため rebase を中断して元に戻しました") }
	m2.handleKey("u")
	m2.handleKey("y")
	m2.Update(pullMsg{err: errors.New("conflict のため rebase を中断して元に戻しました")})
	if !strings.Contains(m2.notice, "conflict") {
		t.Fatalf("pull 失敗の notice が出ない: %q", m2.notice)
	}
}

// tmux prefix (popup 内では tmux に届かない) の誤爆フィードバック。
// TUI 内 notice に加えて、外側の tmux status line へのトースト (display-message) も出す。
func TestBrowseTmuxPrefixFeedback(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.width, m.height = 80, 20
	m.Update(prefixMsg{key: "ctrl+t"})
	// prefix 単体: 目立つ中央トーストを出す (カーソルは動かない)
	m.handleKey("ctrl+t")
	if !strings.Contains(m.prefixNote, "効きません") {
		t.Fatalf("prefix の中央トーストが出ない: %q", m.prefixNote)
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "効きません") || !strings.Contains(v, "⚠ tmux") {
		t.Fatal("中央トーストが描画されない")
	}
	// prefix に続く 1 キーは飲み込む (p が PR オープンに化けない・j でカーソルも動かない)
	m.handleKey("j")
	if m.cursor != 0 {
		t.Fatal("prefix 直後のキーが飲み込まれずカーソルが動いた")
	}
	if !strings.Contains(m.prefixNote, "prefix+j") {
		t.Fatalf("押したキー名入りの中央トーストが出ない: %q", m.prefixNote)
	}
	// 飲み込みは 1 キーだけ (次の j は通常動作。トーストも消える)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatal("prefix の 2 キー後まで飲み込まれた")
	}
	if m.prefixNote != "" {
		t.Fatalf("通常キーで中央トーストが消えない: %q", m.prefixNote)
	}
	// prefix 連打 (tmux のリテラル送信の癖) は pending を張り直して同じ案内
	m.handleKey("ctrl+t")
	m.handleKey("ctrl+t")
	if !m.prefixPending || !strings.Contains(m.prefixNote, "効きません") {
		t.Fatalf("prefix 連打で pending が張り直されない: pending=%v note=%q", m.prefixPending, m.prefixNote)
	}
	m.handleKey("esc") // pending を消化して以降のテストに影響させない
	// y/N 確認モーダル中はモーダルの語彙を優先: C-t は「任意キー = キャンセル」で
	// prefix 検知は発動しない (続く y が飲み込まれる事故の防止)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.handleKey("b")
	if !m.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	m.handleKey("ctrl+t")
	if m.pushConfirm || m.prefixPending {
		t.Fatalf("確認モーダル中の C-t がキャンセルにならない: confirm=%v pending=%v", m.pushConfirm, m.prefixPending)
	}
	// tmux 外 (prefix 不明) では機能オフ = ctrl+t は何もしない
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.Update(prefixMsg{key: ""})
	m2.handleKey("ctrl+t")
	if m2.prefixNote != "" || m2.prefixPending {
		t.Fatalf("tmux 外で prefix 案内が出た: %q", m2.prefixNote)
	}
}

// parseTmuxPrefix: show-options 出力 → bubbletea キー表記。
func TestParseTmuxPrefix(t *testing.T) {
	for out, want := range map[string]string{
		"prefix C-t":  "ctrl+t",
		"prefix C-b":  "ctrl+b",
		"prefix M-a":  "", // C-<英字> 以外は機能オフ
		"prefix None": "",
		"garbage":     "",
		"":            "",
	} {
		if got := parseTmuxPrefix(out); got != want {
			t.Errorf("parseTmuxPrefix(%q) = %q; want %q", out, got, want)
		}
	}
}

// 未 push が 1 件も無いときは確認に入らない。
func TestBrowsePushNoUnpushed(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateSuccess
	m.statuses[m.commits[1].SHA] = StateSuccess
	m.handleKey("b")
	if m.pushConfirm {
		t.Fatal("未 push なしで push 確認に入った")
	}
	// hint 行でなく警告モーダルが出る (ユーザー要望)
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "未 push のコミットはありません") {
		t.Fatal("未 push なしの警告モーダルが出ない")
	}
	// 何かキーで閉じ、そのキーは消費される (カーソルが動かない)
	m.handleKey("j")
	if m.pushWarn != "" {
		t.Fatal("キーで警告モーダルが閉じない")
	}
	if m.cursor != 0 {
		t.Fatal("モーダルを閉じたキーが消費されずカーソルが動いた")
	}
}

// カーソル溝の廃止 (2026-07-19): 行頭 2 桁の溝を足さず git log と左マージンが一致し、
// カーソルはヘッダー行全体の bg 塗りで示す。
func TestBrowseCursorGutterArrowAndBgHighlight(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.usageOv.visible = false // 右上 usage モーダルは上部行を覆うため、カーソル強調の検証から隔離する
	m.statuses = statusesFor(m, StateSuccess)
	m.colored = true
	view := strings.Split(m.View(), "\n")
	var authorLine, cursorHeader, otherHeader string
	for _, l := range view {
		if strings.HasPrefix(stripANSI(l), cursorGutterBlank+"Author: ") && authorLine == "" {
			authorLine = l
		}
		if strings.Contains(l, "commit "+m.commits[0].SHA) {
			cursorHeader = l
		}
		if strings.Contains(l, "commit "+m.commits[1].SHA) {
			otherHeader = l
		}
	}
	if authorLine == "" || cursorHeader == "" || otherHeader == "" {
		t.Fatalf("期待行が見つからない:\n%s", m.View())
	}
	// 全行にカーソル溝 2 桁のマージンがあり、カーソル行だけ「→ 」が入る
	if !strings.HasPrefix(stripANSI(cursorHeader), cursorGutterMark) {
		t.Errorf("カーソル行が %q で始まっていない: %q", cursorGutterMark, cursorHeader)
	}
	if !strings.HasPrefix(stripANSI(otherHeader), cursorGutterBlank) {
		t.Errorf("非カーソル行に溝の空白マージンがない: %q", otherHeader)
	}
	if !strings.Contains(cursorHeader, ansiCursorBg) {
		t.Errorf("カーソル行に bg 塗りがない: %q", cursorHeader)
	}
	if strings.Contains(otherHeader, ansiCursorBg) {
		t.Errorf("非カーソル行に bg が付いている: %q", otherHeader)
	}
	// bg は行内のリセット後も維持される (途切れると sha の直後で塗りが切れる)
	if strings.Contains(cursorHeader, ansiReset+" ") && !strings.Contains(cursorHeader, ansiReset+ansiCursorBg) {
		t.Errorf("リセット後に bg が張り直されていない: %q", cursorHeader)
	}
	// 色なしは bg が使えないため「→ 」のみに degrade (溝は全行にあるのでずれない)
	m.colored = false
	m.invalidateLines()
	if !strings.Contains(m.View(), cursorGutterMark) {
		t.Errorf("NO_COLOR で %q マーカーが出ていない", cursorGutterMark)
	}
}

// pullBlockedByDirtyTree: tracked の未コミット変更 (staged/unstaged) だけを検知し、
// untracked (??) は rebase を阻まないため無視する (u の dirty-tree 事前検知の要)。
func TestPullBlockedByDirtyTree(t *testing.T) {
	cases := []struct {
		name      string
		porcelain string
		want      bool
	}{
		{"クリーン", "", false},
		{"untracked のみは無害", "?? new.go\n?? tmp/\n", false},
		{"unstaged 変更", " M tui.go\n", true},
		{"staged 変更", "M  tui.go\n", true},
		{"untracked と tracked 混在", "?? new.go\n M tui.go\n", true},
	}
	for _, c := range cases {
		if got := pullBlockedByDirtyTree(c.porcelain); got != c.want {
			t.Errorf("%s: pullBlockedByDirtyTree()=%v, want %v", c.name, got, c.want)
		}
	}
}
