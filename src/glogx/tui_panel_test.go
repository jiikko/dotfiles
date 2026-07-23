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
)

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

// 二重 timer 防止: refresh in-flight (panelRefresh=true) 中に到着した detailMsg は、実行中 job が
// まだ居ても新しい poll を張らない (panelPollMsg 側が既に次を予約済み)。wasRefresh を panelRefresh
// クリア前に捕捉する順序が「1 open 世代につきポーリング鎖 1 本」の核。n=1 で maybeFetchETABasis を
// nil に隔離し、返り Cmd が poll 決定だけを反映するようにする。
func TestBrowsePanelRefreshArrivalDoesNotDoubleSchedulePoll(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	m.details[sha] = running
	m.openPanel() // running job 付き → poll 予約
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd == nil || !m.panelRefresh {
		t.Fatal("poll で refresh が起動しない")
	}
	// refresh 結果 (detailMsg) が到着。wasRefresh=true なので新しい poll は張らない。n=1 で
	// maybeFetchETABasis=nil のため、返り Cmd が nil であることが「poll を張っていない」の証跡。
	_, cmd := m.Update(detailMsg{sha: sha, batch: CIBatch{Details: map[string][]CheckDetail{sha: running}}})
	if cmd != nil {
		t.Error("refresh 着地で二重に poll を張った (wasRefresh のとき poll は張らない契約)")
	}
	if m.panelRefresh {
		t.Error("refresh 着地で panelRefresh が下りていない")
	}
}

// 遅延 poll 開始: openPanel 時に details 未取得だった実行中コミットは、detailMsg 初到着
// (wasRefresh=false) かつ実行中 job があるとき初めて poll を張る (openPanel 時点では details 未取得で
// 判定できないため)。上のテストと対で「初回到着で張る／refresh 着地では張らない」を固定する。
func TestBrowsePanelStartsPollOnFirstDetailArrival(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	m.openPanel() // details 未取得 → poll はまだ張られない
	if m.panelHasRunningJob() {
		t.Fatal("前提: details 未取得なので running 判定は false のはず")
	}
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	_, cmd := m.Update(detailMsg{sha: sha, batch: CIBatch{Details: map[string][]CheckDetail{sha: running}}})
	// n=1 で maybeFetchETABasis=nil。返り Cmd が非 nil なのは poll (schedulePanelPoll) が張られた証跡。
	if cmd == nil {
		t.Error("初回 detailMsg 到着で poll が張られない (openPanel 時は details 未取得で判定できないため遅延)")
	}
}

// panelPollSeq 世代ガード (パネルが開いている状態): 開き直しで世代が進んだ後、旧世代の
// panelPollMsg{seq:旧} は panelSHA が非空でも seq 不一致で破棄される (開き直しで残タイマーが
// 二重ポーリングにならないための不変条件の直接検証)。
func TestBrowsePanelPollSeqGuardDiscardsStaleGenerationWhileOpen(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StatePending)
	// ⚠️ commit1 に実行中 job を持たせるのが要: これが無いと panelPollMsg ハンドラが seq 比較の
	// 直後の `if !panelHasRunningJob()` で先に return し、seq ガードを踏まずにテストが通って
	// しまう (seq ガードを削除しても PASS = 何も pin しない)。running job で panelHasRunningJob()==true
	// にして初めて「seq 不一致だから破棄」経路を実際に検証できる。
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	m.openPanel() // commit0
	oldSeq := m.panelPollSeq
	m.closePanel()
	m.cursor = 1
	m.details[m.commits[1].SHA] = running
	m.openPanel() // commit1: 世代が進む・panelSHA 非空・実行中 job あり
	if m.panelPollSeq == oldSeq {
		t.Fatal("前提: 開き直しで世代が進んでいない")
	}
	if !m.panelHasRunningJob() {
		t.Fatal("前提: commit1 に実行中 job がない (seq ガードに到達できずテストが無意味化する)")
	}
	// 旧世代の panelPollMsg: 実行中 job があっても seq 不一致で破棄される (cmd==nil)。
	if _, cmd := m.Update(panelPollMsg{seq: oldSeq}); cmd != nil {
		t.Error("旧世代の panelPollMsg が破棄されず新しい poll を発行した (二重ポーリングの温床)")
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
	if cmd == nil || !m.detailOv.open {
		t.Fatalf("job 行の Enter で詳細が開かない (cmd=%v detailOpen=%v)", cmd, m.detailOv.open)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"line"}})
	m.handleKey("enter")
	if m.detailOv.open {
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
	m.detailOv.open = true
	m.panelCursor = 0
	m.detailOv.cache[m.detailKey()] = []string{ansiRed + "boom" + ansiReset, "at foo.go:10"}

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
	m.detailOv.cache[m.detailKey()] = nil
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
	if !m.detailOv.open || !m.detailOv.busy[m.detailKey()] {
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
	if m.detailOv.offset != 30-rows {
		t.Errorf("detailOffset = %d; want 末尾表示 %d", m.detailOv.offset, 30-rows)
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
	if m.detailOv.offset != 30-rows-1 {
		t.Errorf("k 後の offset = %d", m.detailOv.offset)
	}
	m.handleKey("g")
	if m.detailOv.offset != 0 {
		t.Errorf("g 後の offset = %d", m.detailOv.offset)
	}
	// h で job フォーカスへ戻る (パネルは開いたまま)
	m.handleKey("h")
	if m.detailOv.open || m.panelSHA == "" || m.panelCursor != 0 {
		t.Errorf("h 後の状態: detailOpen=%v panelSHA=%q cursor=%d", m.detailOv.open, m.panelSHA, m.panelCursor)
	}
	// 再度 l → キャッシュ済みなので fetch なしで即表示
	if _, cmd := m.handleKey("l"); cmd != nil {
		t.Errorf("キャッシュ済み詳細で再 fetch した")
	}
	if !m.detailOv.open {
		t.Errorf("2 回目の l で開かない")
	}
}

// キャッシュ済み job 詳細を開き直すと offset がログ末尾へ飛ぶ (先頭ではない)。抽出で
// startOpen() を diffOv.open (offset=0) の clone にすると『開いた瞬間に最新ログ』が壊れる。
func TestBrowseJobDetailReopenScrollsToTail(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.handleKey("l")
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("log line %d", i)
	}
	m.Update(jobDetailMsg{key: m.detailKey(), lines: lines})
	m.handleKey("g") // 先頭へ
	if m.detailOv.offset != 0 {
		t.Fatalf("g で先頭に来ていない: offset=%d", m.detailOv.offset)
	}
	m.handleKey("h") // 閉じる (cache は残る)
	if m.detailOv.open {
		t.Fatal("h で閉じていない")
	}
	m.handleKey("l") // 再オープン (キャッシュヒット)
	rows := m.visibleDetailRows()
	if m.detailOv.offset != 30-rows {
		t.Errorf("再オープン時の offset = %d; want 末尾 %d (最新ログを表示)", m.detailOv.offset, 30-rows)
	}
	if !strings.Contains(m.View(), "log line 29") {
		t.Errorf("再オープンで末尾行が見えない:\n%s", m.View())
	}
}

// jobDetailMsg の末尾スクロールは「今開いている詳細 (detailOpen かつ detailKey()==msg.key)」
// のときだけ発火する。別 key の遅延結果・詳細非表示中の結果は offset を動かさない (identity
// 非所有なので、抽出で receive が live key を受け取らないと誤発火/不発火する)。
func TestBrowseJobDetailStaleMsgDoesNotMoveOffset(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0) // job0=build, job1=lint
	m.openPanel()
	m.handleKey("j") // job0 (key = panelSHA/0)
	m.handleKey("l")
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"a", "b", "c"}})
	m.handleKey("g") // offset=0
	if m.detailOv.offset != 0 {
		t.Fatalf("前提: offset=0 でない (%d)", m.detailOv.offset)
	}
	// 別 job (job1) 宛の遅延結果が届いても、今開いている job0 の offset は動かない
	staleKey := m.panelSHA + "/1"
	longLines := make([]string, 50)
	for i := range longLines {
		longLines[i] = fmt.Sprintf("stale %d", i)
	}
	m.Update(jobDetailMsg{key: staleKey, lines: longLines})
	if m.detailOv.offset != 0 {
		t.Errorf("別 key の遅延結果で offset が動いた: %d; want 0", m.detailOv.offset)
	}
	// 詳細を閉じた状態でも jobDetailMsg は offset を動かさない
	m.handleKey("h")
	m.Update(jobDetailMsg{key: m.detailKey(), lines: longLines})
	if m.detailOv.offset != 0 {
		t.Errorf("詳細非表示中に offset が動いた: %d; want 0", m.detailOv.offset)
	}
}

// job 詳細ポップアップ表示中の Space は「閉じる」(diff の Space=半ページ下スクロールとは逆)。
// tig 流の「詳細→job 一覧へ戻る」。抽出で diffOv.scroll を素朴コピーすると Space が化ける。
func TestBrowseJobDetailSpaceCloses(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.handleKey("l")
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"x", "y", "z"}})
	m.handleKey(" ")
	if m.detailOv.open {
		t.Error("Space で詳細が閉じない (job 詳細では Space=閉じる)")
	}
	if m.panelSHA == "" {
		t.Error("Space で詳細を閉じたら job 一覧に戻る (パネルは開いたまま)")
	}
}

// closePanel は panel-frame と detail クラスタの両方を落とす唯一の choke point。詳細を開いた
// まま閉じる経路 (reloadAfterPull 等) で detailOpen/detailOffset が確実に落ちる。抽出で
// closePanel の detailOv.close() 化を漏らすと、次に開いたパネルの下に前 job のログが stale 表示。
func TestBrowseClosePanelClosesOpenDetail(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.handleKey("l")
	m.Update(jobDetailMsg{key: m.detailKey(), lines: []string{"a", "b", "c", "d", "e"}})
	m.handleKey("G") // offset を非 0 に
	if !m.detailOv.open {
		t.Fatal("前提: 詳細が開いていない")
	}
	m.closePanel()
	if m.detailOv.open || m.detailOv.offset != 0 || m.panelSHA != "" {
		t.Errorf("closePanel が詳細を落とさない: detailOpen=%v detailOffset=%d panelSHA=%q",
			m.detailOv.open, m.detailOv.offset, m.panelSHA)
	}
}

// job 詳細取得中 (jobDetailBusy に key) は spinnerActive() が true を返し tick が回り続ける。
// 抽出で spinnerActive を detailOv.fetching() 参照に変え忘れると取得中スピナーが固まる。
func TestBrowseJobDetailFetchKeepsSpinnerActive(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.handleKey("l") // 未取得なので busy が立つ
	if !m.detailOv.busy[m.detailKey()] {
		t.Fatal("前提: 取得中フラグが立っていない")
	}
	if !m.spinnerActive() {
		t.Error("job 詳細取得中に spinnerActive() が false (tick が止まりスピナーが固まる)")
	}
}

// reloadAfterPull は job 詳細ログキャッシュ (jobDetail/jobDetailBusy) も破棄する。抽出で
// detailOv.reset() の配線を漏らすと、pull 後 (SHA 不変のコミット) に旧ログ残骸が残る。
func TestBrowseReloadAfterPullResetsJobDetailCache(t *testing.T) {
	newTempRepo(t, []string{"first", "second"})
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.opts = &Options{MaxCount: 20}
	m.detailOv.cache["stale/0"] = []string{"old log"}
	m.detailOv.busy["stale/0"] = true
	m.reloadAfterPull()
	if len(m.detailOv.cache) != 0 || len(m.detailOv.busy) != 0 {
		t.Errorf("reloadAfterPull で job 詳細キャッシュが残った: cache=%d busy=%d",
			len(m.detailOv.cache), len(m.detailOv.busy))
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

// 猶予ポーリング: 実行中 job がまだ見えなくても panelPollGrace の残回数だけリフレッシュを続け、
// 実行中 job が見えたら猶予を終えて通常追従へ、尽きたら止まる。
func TestPanelPollGrace(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure) // 実行中 job なし
	m.openPanel()
	m.panelPollGrace = 2
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd == nil {
		t.Fatal("猶予中なのにポーリングが止まった")
	}
	if m.panelPollGrace != 1 || !m.panelRefresh {
		t.Fatalf("猶予が減らない / リフレッシュが走らない: grace=%d refresh=%v", m.panelPollGrace, m.panelRefresh)
	}
	// 実行中 job が見えたら猶予は 0 に戻り、通常の追従が続く
	m.panelRefresh = false
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "lint", State: StatePending, CheckID: 7, StartedAt: timeNow()},
	}
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd == nil {
		t.Fatal("実行中 job があるのにポーリングが止まった")
	}
	if m.panelPollGrace != 0 {
		t.Fatalf("実行中 job が見えても猶予が残っている: %d", m.panelPollGrace)
	}
	// 猶予 0 + 実行中 job なしで停止
	m.panelRefresh = false
	withFailedJob(m, 0, 7, StateFailure)
	if _, cmd := m.Update(panelPollMsg{seq: m.panelPollSeq}); cmd != nil {
		t.Fatal("猶予が尽きたのにポーリングが続いた")
	}
	// closePanel で猶予も破棄される
	m.panelPollGrace = 5
	m.closePanel()
	if m.panelPollGrace != 0 {
		t.Fatal("closePanel で猶予ポーリングが破棄されない")
	}
}

// 回帰 (レビュー確定 medium): panelPollMsg の自己更新チェーンは single-flight。開始点が複数
// (openPanel / detailMsg / rerunMsg) あっても二重チェーンを張らない (GraphQL ポーリング倍化の防止)。
func TestEnsurePanelPollSingleFlight(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.panelSHA = m.commits[0].SHA
	if cmd := m.ensurePanelPoll(); cmd == nil || !m.panelPolling {
		t.Fatal("初回 ensurePanelPoll がチェーンを張らない")
	}
	if cmd := m.ensurePanelPoll(); cmd != nil {
		t.Fatal("チェーンが生きているのに 2 本目を張った (二重化)")
	}
	m.closePanel()
	if m.panelPolling {
		t.Fatal("closePanel で panelPolling が戻らない")
	}
	if cmd := m.ensurePanelPoll(); cmd == nil {
		t.Fatal("closePanel 後に再アームできない")
	}
}

// 回帰 (レビュー確定 medium): 実行中 job があるパネルで rerun しても、既存の polling チェーンに
// 加えて 2 本目を張らない。
func TestBrowseRerunNoDoublePoll(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	m.details[m.commits[0].SHA] = []CheckDetail{
		{Name: "test", State: StatePending, CheckID: 1, StartedAt: timeNow()},
		{Name: "lint", State: StateFailure, CheckID: 7},
	}
	m.openPanel() // 実行中 job あり → chain #1 が張られる
	if !m.panelPolling {
		t.Fatal("実行中 job で openPanel がチェーンを張らない")
	}
	m.Update(rerunMsg{sha: m.commits[0].SHA}) // rerun 成功
	if !m.panelPolling {
		t.Fatal("rerun 後に panelPolling が落ちた")
	}
	if m.ensurePanelPoll() != nil {
		t.Fatal("rerun 後もチェーンは 1 本のはず (二重化した)")
	}
}
