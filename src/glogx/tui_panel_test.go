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

	tea "charm.land/bubbletea/v2"
)

// pending なコミットは「どんな状況でも」追い続ける: パネルを閉じても、そもそも一度も開いて
// いなくても、glogx が開いている間はポーリングが続く (追従条件は statuses だけが決める)。
func TestCIPollFollowsPendingWithoutPanel(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	// パネルを一度も開かずに追従チェーンが張れる
	if cmd := m.ensureCIPoll(); cmd == nil || !m.ciPolling {
		t.Fatal("pending なのにパネル無しで追従チェーンが張られない")
	}
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil || !m.ciPollInFlight {
		t.Fatal("pending の周期で再取得が走らない")
	}
	// パネルを開いて閉じても追従は途切れない (旧実装はパネル依存で止まっていた)
	m.ciPollInFlight = false
	m.openPanel()
	m.closePanel()
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil || !m.ciPollInFlight {
		t.Fatal("パネルを閉じたら追従が止まった (pending の間は続く契約)")
	}
}

// 追従結果 (ciPollResultMsg) の着地では、生きているチェーンに 2 本目を張らない (ポーリング倍化の防止)。
// n=1 で maybeFetchETABasis を nil に隔離し、返り Cmd が poll 決定だけを反映するようにする。
func TestCIPollResultDoesNotDoubleSchedule(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	if cmd := m.ensureCIPoll(); cmd == nil {
		t.Fatal("前提: チェーンが張れない")
	}
	// pending のままの結果が着地。チェーンは生きているので新しい timer は張らない
	_, cmd := m.Update(ciPollResultMsg{targets: []string{sha}, batch: CIBatch{Statuses: map[string]CIState{sha: StatePending}}})
	if cmd != nil {
		t.Error("結果着地で二重に poll を張った (single-flight の契約)")
	}
	if m.ciPollInFlight {
		t.Error("結果着地で in-flight が下りていない")
	}
}

// 追従は details の到着を待たない: openPanel 時点で details が未取得でも、statuses が pending なら
// その場でチェーンが張られる (追従条件は statuses だけが決める)。details が後から届いても
// single-flight で 2 本目は張らない。
func TestCIPollStartsAtOpenPanelWithoutDetails(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	m.openPanel()
	if m.panelHasRunningJob() {
		t.Fatal("前提: details 未取得なので running 判定は false のはず")
	}
	if !m.ciPolling {
		t.Error("details 未取得の pending でチェーンが張られない (details 到着を待ってしまっている)")
	}
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	// n=1 で maybeFetchETABasis=nil。details 到着で返る Cmd が nil = 2 本目を張っていない証跡。
	if _, cmd := m.Update(detailMsg{sha: sha, batch: CIBatch{Details: map[string][]CheckDetail{sha: running}}}); cmd != nil {
		t.Error("details 到着で 2 本目のチェーンを張った (single-flight の契約)")
	}
}

// 世代ガード: リロード (ciPollGen が進む) 後に届いた旧世代の ciPollMsg は、追従対象があっても
// 破棄される (リロードで対象そのものが入れ替わるため、旧タイマーが 2 本目のチェーンにならない)。
// 🚨 追従対象 (pending) を残しておくのが要: 対象が無いと世代比較の後の「対象なしで停止」経路で
// 先に return し、世代ガードを消しても PASS してしまう。
func TestCIPollGenGuardDiscardsStaleGeneration(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StatePending)
	oldGen := m.ciPollGen
	m.ciPollGen++ // reloadAfterPull 相当
	if len(m.ciPollTargets()) == 0 {
		t.Fatal("前提: 追従対象が無い (世代ガードに到達できずテストが無意味化する)")
	}
	if _, cmd := m.Update(ciPollMsg{gen: oldGen}); cmd != nil {
		t.Error("旧世代の ciPollMsg が破棄されず新しい poll を発行した (二重ポーリングの温床)")
	}
	if m.ciPollInFlight {
		t.Error("旧世代の ciPollMsg で再取得が走った")
	}
}

func TestBrowseCIPolling(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	running := []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Now().Add(-time.Minute)}}
	m.details[sha] = running
	// pending のコミットがあるパネルを開く → ポーリング timer が仕掛かる
	if cmd := m.openPanel(); cmd == nil {
		t.Fatal("pending ありでポーリング timer が仕掛からない")
	}
	// 周期発火 → 再取得 (in-flight) + 次回予約
	_, cmd := m.Update(ciPollMsg{gen: m.ciPollGen})
	if cmd == nil || !m.ciPollInFlight {
		t.Fatalf("poll で再取得が走らない: cmd=%v inFlight=%v", cmd != nil, m.ciPollInFlight)
	}
	// in-flight 中の poll は fetch を重ねない (timer 予約のみ)
	m.Update(ciPollMsg{gen: m.ciPollGen})
	if !m.ciPollInFlight {
		t.Fatal("in-flight 中の poll で状態が壊れた")
	}
	// 結果の到着: in-flight が解除され、job 縮小でカーソルがクランプされる
	m.panelCursor = 0
	m.Update(ciPollResultMsg{targets: []string{sha}, batch: CIBatch{
		Statuses: map[string]CIState{sha: StatePending},
		Details:  map[string][]CheckDetail{sha: {}},
	}})
	if m.ciPollInFlight {
		t.Fatal("ciPollResultMsg で in-flight が解除されない")
	}
	if m.panelCursor != -1 {
		t.Fatalf("job 0 件への縮小でカーソルがクランプされない: %d", m.panelCursor)
	}
	// 決着 (success) の poll はポーリングを止める
	m.statuses[sha] = StateSuccess
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd != nil {
		t.Fatal("CI 決着後も poll が続く")
	}
	if m.ciPolling {
		t.Fatal("決着後もチェーンが生きたままになっている")
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
	view := m.View().Content
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
	view := strings.Split(m.View().Content, "\n")
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
		t.Fatalf("ヘッダー行 (%d) かパネル (%d) が見つからない:\n%s", headerIdx, panelIdx, m.View().Content)
	}
	if panelIdx != headerIdx+1 {
		t.Errorf("パネル位置 = %d 行目; want ヘッダー直下 %d 行目:\n%s", panelIdx, headerIdx+1, m.View().Content)
	}
}

func TestBrowsePanelClampedToViewport(t *testing.T) {
	// 対象コミットが画面下部でも、パネルはビューポート内へ収まる位置に出る
	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	withJobs(m, 4)
	m.handleKey("G") // 末尾コミットへ (ヘッダーはビューポート下端付近)
	m.openPanel()
	view := m.View().Content
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
	before := strings.Count(m.View().Content, "\n")
	m.openPanel()
	after := strings.Count(m.View().Content, "\n")
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
	opened := stubBrowser(t)
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
	if *opened != "https://github.com/o/r/runs/1" {
		t.Errorf("開いた URL = %q", *opened)
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
	if !strings.Contains(m.toast.text, "URL がありません") {
		t.Errorf("トーストが出ていない: %q", m.toast.text)
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
	if !strings.Contains(m.View().Content, "取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View().Content)
	}
	// 取得完了メッセージで反映される
	m.Update(detailMsg{sha: sha, batch: CIBatch{
		Statuses: map[string]CIState{sha: StateSuccess},
		Details:  map[string][]CheckDetail{sha: {{Name: "build", State: StateSuccess}}}}})
	if m.detailsLoading[sha] {
		t.Errorf("取得完了後も loading のまま")
	}
	if !strings.Contains(m.View().Content, "✓ build") {
		t.Errorf("取得した詳細がパネルに出ていない:\n%s", m.View().Content)
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
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: shas, batch: CIBatch{
		Statuses: map[string]CIState{shas[0]: StateSuccess},
		Details:  map[string][]CheckDetail{shas[0]: {{Name: "build", State: StateSuccess}}},
	}})
	if m.detailsLoading[shas[0]] {
		t.Errorf("一括取得完了後も loading のまま")
	}
	if !strings.Contains(m.View().Content, "✓ build") {
		t.Errorf("一括取得の詳細がパネルに出ていない:\n%s", m.View().Content)
	}
}

func TestBrowseCIResultMergesAndStopsSpinner(t *testing.T) {
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	if !m.fetching {
		t.Fatalf("toFetch ありで fetching が立っていない")
	}
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: shas, batch: CIBatch{
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

// 一括取得はチャンクへ割って並列に投げるので ciResultMsg は複数回届く。最後のチャンクが
// 着弾するまで fetching (= スピナー・invalidate gate・パネルガードの出典) を下ろさず、
// 着弾ぶんだけ toFetch を縮めること。
func TestBrowseCIResultChunksLandIncrementally(t *testing.T) {
	const n = 20 // chunkSHAs が複数チャンクへ割る件数
	m := newTestBrowse(t, n, map[string]CIState{}, nil)
	shas := make([]string, n)
	for i, c := range m.commits {
		shas[i] = c.SHA
	}
	m.startCIFetch(shas)
	chunks := chunkSHAs(shas)
	if len(chunks) < 2 {
		t.Fatalf("チャンク数 = %d; このテストは複数チャンク前提", len(chunks))
	}
	for i, chunk := range chunks {
		statuses := map[string]CIState{}
		for _, sha := range chunk {
			statuses[sha] = StateSuccess
		}
		m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunk, batch: CIBatch{Statuses: statuses}})

		last := i == len(chunks)-1
		if m.fetching == last {
			t.Errorf("チャンク %d/%d 着弾後の fetching = %v", i+1, len(chunks), m.fetching)
		}
		// 着弾したチャンクの SHA は toFetch から消え、未着のぶんだけ残る
		for _, sha := range chunk {
			if slices.Contains(m.toFetch, sha) {
				t.Errorf("着弾済み SHA が toFetch に残っている (パネルが無駄に待つ)")
			}
			if m.statuses[sha] != StateSuccess {
				t.Errorf("チャンク %d の SHA が反映されていない: %q", i+1, m.statuses[sha])
			}
		}
	}
	if len(m.toFetch) != 0 {
		t.Errorf("全チャンク着弾後の toFetch = %v; want 空", m.toFetch)
	}
}

// 回帰: 未着チャンクの SHA 列を壊さない。chunkSHAs は元スライスの部分スライスを返すので、
// toFetch を in-place に縮める実装 (slices.DeleteFunc) だと共有配列のゼロ埋めで
// 「まだ飛んでいるチャンク」の SHA が空文字へ潰れ、その結果が届いても反映されなくなる。
func TestBrowseCIResultKeepsPendingChunkSHAs(t *testing.T) {
	const n = 20
	m := newTestBrowse(t, n, map[string]CIState{}, nil)
	shas := make([]string, n)
	for i, c := range m.commits {
		shas[i] = c.SHA
	}
	m.startCIFetch(shas)
	chunks := chunkSHAs(shas)
	if len(chunks) < 2 {
		t.Fatalf("チャンク数 = %d; このテストは複数チャンク前提", len(chunks))
	}
	// 後続チャンクの内容を先に写しておく (先頭チャンクの処理で壊れたら検出できる)
	want := make([][]string, len(chunks))
	for i, c := range chunks {
		want[i] = slices.Clone(c)
	}
	first := map[string]CIState{}
	for _, sha := range chunks[0] {
		first[sha] = StateSuccess
	}
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunks[0], batch: CIBatch{Statuses: first}})
	for i := 1; i < len(chunks); i++ {
		if !slices.Equal(chunks[i], want[i]) {
			t.Fatalf("未着チャンク %d の SHA 列が壊れた:\n got  %q\n want %q", i+1, chunks[i], want[i])
		}
	}
	// 壊れていなければ、後続チャンクの着弾も従来どおり反映される
	for i := 1; i < len(chunks); i++ {
		statuses := map[string]CIState{}
		for _, sha := range chunks[i] {
			statuses[sha] = StateFailure
		}
		m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunks[i], batch: CIBatch{Statuses: statuses}})
		for _, sha := range chunks[i] {
			if m.statuses[sha] != StateFailure {
				t.Errorf("チャンク %d の SHA %q が反映されていない: %q", i+1, sha[:7], m.statuses[sha])
			}
		}
	}
}

// 成功チャンクが先に失敗したチャンクの警告を消さないこと (逆に新しい取得の開始では消えること)。
// 世代ガード: pull/push の再取得 (startCIFetch) 後に届いた旧世代チャンクは丸ごと捨てる。
// 捨てないと新世代の pendingFetches を誤減算して fetching が早期に下り (スピナー消灯・
// 同一 SHA 並行リクエストガードの無効化)、取得中 SHA を toFetch から誤って間引く。
func TestBrowseCIResultDiscardsStaleEpoch(t *testing.T) {
	const n = 20
	m := newTestBrowse(t, n, map[string]CIState{}, nil)
	shas := make([]string, n)
	for i, c := range m.commits {
		shas[i] = c.SHA
	}
	m.startCIFetch(shas) // 旧世代
	oldEpoch := m.fetchEpoch
	m.startCIFetch(shas) // 再取得で世代が進む (pull/push 相当)
	if m.fetchEpoch == oldEpoch {
		t.Fatal("前提: startCIFetch で世代が進んでいない")
	}
	pending, toFetch := m.pendingFetches, slices.Clone(m.toFetch)
	stale := map[string]CIState{}
	for _, sha := range chunkSHAs(shas)[0] {
		stale[sha] = StateSuccess
	}
	m.Update(ciResultMsg{epoch: oldEpoch, shas: chunkSHAs(shas)[0], batch: CIBatch{Statuses: stale}})
	if m.pendingFetches != pending || !m.fetching {
		t.Errorf("旧世代チャンクが pendingFetches を減算した: %d → %d", pending, m.pendingFetches)
	}
	if !slices.Equal(m.toFetch, toFetch) {
		t.Errorf("旧世代チャンクが toFetch を間引いた: %d → %d 件", len(toFetch), len(m.toFetch))
	}
	if len(m.statuses) != 0 {
		t.Errorf("旧世代チャンクのデータが反映された (新世代が取り直す契約): %v", m.statuses)
	}
	// 新世代のチャンクは従来どおり反映される
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunkSHAs(shas)[0], batch: CIBatch{Statuses: stale}})
	if len(m.statuses) == 0 {
		t.Error("新世代のチャンクまで捨てられた")
	}
}

func TestBrowseCIResultKeepsChunkError(t *testing.T) {
	const n = 20
	m := newTestBrowse(t, n, map[string]CIState{}, nil)
	shas := make([]string, n)
	for i, c := range m.commits {
		shas[i] = c.SHA
	}
	m.startCIFetch(shas)
	chunks := chunkSHAs(shas)
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunks[0], batch: emptyBatch(), ghErr: &GHError{Kind: GHOther, Detail: "chunk 1 failed"}})
	ok := map[string]CIState{}
	for _, sha := range chunks[1] {
		ok[sha] = StateSuccess
	}
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: chunks[1], batch: CIBatch{Statuses: ok}})
	if m.ghErr == nil || !strings.Contains(m.ghErr.Detail, "chunk 1 failed") {
		t.Errorf("成功チャンクが失敗チャンクの警告を消した: %+v", m.ghErr)
	}
	// 新しい取得の開始では警告をリセットする (sticky 警告の防止)
	m.startCIFetch(shas)
	if m.ghErr != nil {
		t.Errorf("取得開始で警告がリセットされていない: %+v", m.ghErr)
	}
}

func TestBrowseCIResultNegativeCachesUnknown(t *testing.T) {
	// API から結果が返らなかった SHA は unknown 表示 + 負キャッシュ対象 (fetched) に入る
	shas := []string{strings.Repeat("a", 40)}
	m := newTestBrowse(t, 1, map[string]CIState{}, shas)
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: shas, batch: emptyBatch()})
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
	if !strings.Contains(m.View().Content, "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View().Content)
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
	stubBrowserFunc(t, func(string) error { called = true; return nil })
	if _, cmd := m.handleKey("o"); cmd != nil {
		t.Errorf("file:// URL で Cmd が返った")
	}
	if called {
		t.Errorf("file:// URL がブラウザに渡された")
	}
	if !strings.Contains(m.toast.text, "http(s) 以外") {
		t.Errorf("トーストが出ていない: %q", m.toast.text)
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
	cmds := stubEditorCapture(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withJobs(m, 0)
	m.openPanel()
	m.handleKey("j")
	m.detailOv.open = true
	m.panelCursor = 0
	m.detailOv.cache.store(m.detailKey(), []string{ansiRed + "boom" + ansiReset, "at foo.go:10"}, m.detailKey())

	_, cmd := m.handleKey("v")
	if cmd == nil || len(*cmds) != 1 {
		t.Fatal("v で nvim 起動コマンドが組まれない")
	}
	// nvim -R ... - (readonly、stdin から読む)
	if (*cmds)[0].Args[0] != "nvim" || (*cmds)[0].Args[len((*cmds)[0].Args)-1] != "-" {
		t.Fatalf("nvim ... - で起動していない: %v", (*cmds)[0].Args)
	}
	if !slices.Contains((*cmds)[0].Args, "-R") {
		t.Fatalf("readonly (-R) で開いていない: %v", (*cmds)[0].Args)
	}
	// 🚨 -c の scratch 設定まで見る。-R だけ守っても、buftype=nofile / noswapfile /
	// nomodifiable が落ちると「誤編集できず :q が常にクリーンに閉じる」(openJobLogInEditor の
	// doc。素の nvim - で :q がエラーになるというユーザー報告 2026-07-21 由来) が破れる。
	// 実測: nomodifiable を落としても -c を丸ごと消しても全テストが green だった。
	var scratch string
	for i, a := range (*cmds)[0].Args {
		if a == "-c" && i+1 < len((*cmds)[0].Args) {
			scratch = (*cmds)[0].Args[i+1]
		}
	}
	for _, want := range []string{"buftype=nofile", "noswapfile", "nomodifiable"} {
		if !strings.Contains(scratch, want) {
			t.Errorf("scratch 設定に %q が無い: -c %q", want, scratch)
		}
	}
	// stdin に ANSI 除去済みログが載っている (ファイルは作らない)
	buf, _ := io.ReadAll((*cmds)[0].Stdin)
	if string(buf) != "boom\nat foo.go:10\n" {
		t.Fatalf("stdin の中身 = %q", string(buf))
	}
	// エラーで閉じたら失敗トースト、成功なら無し。🚨 文言はツール名を名指ししない
	// (起動対象は $VISUAL/$EDITOR で変わる。ここの job ログ経路だけは nvim 固定だが、
	// トーストは editorClosedMsg で共通なので総称になる)。原因は err がそのまま載る
	m.Update(editorClosedMsg{err: errors.New("nvim: not found")})
	if m.toast.ok || !strings.Contains(m.toast.text, "エディタを開けません") ||
		!strings.Contains(m.toast.text, "nvim: not found") {
		t.Errorf("起動失敗の失敗トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}

	// ログが空なら起動しない
	m.detailOv.cache.store(m.detailKey(), nil, m.detailKey())
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
	if !m.detailOv.open || !m.detailOv.cache.loading(m.detailKey()) {
		t.Fatalf("詳細が開いていない / busy でない")
	}
	if !strings.Contains(m.View().Content, "詳細を取得中") {
		t.Errorf("取得中表示がない:\n%s", m.View().Content)
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
	if !strings.Contains(m.View().Content, "log line 29") {
		t.Errorf("末尾行が見えていない (低い端末でも末尾は見える):\n%s", m.View().Content)
	}
	// 詳細ボックスは job パネルの子であることが分かるよう段差付き (ユーザー要望)
	indented := false
	for line := range strings.SplitSeq(m.View().Content, "\n") {
		if strings.HasPrefix(stripANSI(line), detailIndent+"┌") {
			indented = true
			break
		}
	}
	if !indented {
		t.Errorf("詳細ボックスに段差がない:\n%s", m.View().Content)
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
	if !strings.Contains(m.View().Content, "log line 29") {
		t.Errorf("再オープンで末尾行が見えない:\n%s", m.View().Content)
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
	if !m.detailOv.cache.loading(m.detailKey()) {
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
	m.detailOv.cache.store("stale/0", []string{"old log"}, "stale/0")
	// 🚨 札は未キャッシュのキーで立てる: begin はキャッシュ済みなら何もしないので、
	// 上と同じキーに対して呼ぶと busy が空のまま = 下の busy 検査が空振りになる
	if !m.detailOv.cache.begin("inflight/1") {
		t.Fatal("走行中の札を立てられない (前提が崩れた)")
	}
	m.reloadAfterPull()
	if len(m.detailOv.cache.entries) != 0 || len(m.detailOv.cache.busy) != 0 {
		t.Errorf("reloadAfterPull で job 詳細キャッシュが残った: cache=%d busy=%d",
			len(m.detailOv.cache.entries), len(m.detailOv.cache.busy))
	}
}

func TestBrowsePanelShowsJobDuration(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.details[sha] = []CheckDetail{{Name: "dotfiles-tests", State: StateFailure, Duration: 2*time.Minute + 39*time.Second}}
	m.openPanel()
	if !strings.Contains(m.View().Content, "(2m39s)") {
		t.Errorf("job 行に所要時間が出ていない:\n%s", m.View().Content)
	}
}

func TestBrowsePanelShowsRunningElapsed(t *testing.T) {
	stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.visible = false // 右上 usage モーダルの "残り / リセット" 見出しが「残り」不在アサートに紛れるのを避ける
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	// 開始 90 秒前・ETA basis なし (履歴が画面に無い) → 経過時間だけ出る
	m.details[sha] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(910, 0)}}
	m.openPanel()
	view := m.View().Content
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
	stubClock(t)

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	running, prev := m.commits[0].SHA, m.commits[1].SHA
	m.statuses[running] = StatePending
	m.statuses[prev] = StateSuccess
	// 実行中 job: 開始 60 秒前。直近の同名完了 job は 100 秒 → 残り ~40s
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(940, 0)}}
	m.details[prev] = []CheckDetail{{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}
	m.openPanel()
	view := m.View().Content
	if !strings.Contains(view, "1m00s 経過") {
		t.Errorf("経過時間が出ていない:\n%s", view)
	}
	if !strings.Contains(view, "残り ~40s") {
		t.Errorf("ETA (残り ~40s) が出ていない:\n%s", view)
	}
}

func TestBrowsePanelRunningETAOverrun(t *testing.T) {
	stubClock(t)

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	running, prev := m.commits[0].SHA, m.commits[1].SHA
	m.statuses[running] = StatePending
	// 経過 120 秒 > 前回 100 秒 → 予定超過
	m.details[running] = []CheckDetail{{Name: "build", State: StatePending, StartedAt: time.Unix(880, 0)}}
	m.details[prev] = []CheckDetail{{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}
	m.openPanel()
	if !strings.Contains(m.View().Content, "予定超過") {
		t.Errorf("前回所要時間を超えたら予定超過を出すべき:\n%s", m.View().Content)
	}
}

func TestBrowseRunningETASkipsCancelled(t *testing.T) {
	stubClock(t)

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
	view := m.View().Content
	if strings.Contains(view, "予定超過") {
		t.Errorf("cancel run (3s) を basis に拾って誤って超過判定している:\n%s", view)
	}
	if !strings.Contains(view, "残り ~40s") {
		t.Errorf("cancel をスキップして正常完了 (100s) を basis にすべき:\n%s", view)
	}
}

func TestBrowseRunningETAFetchesMissingBasis(t *testing.T) {
	stubClock(t)

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
	if strings.Contains(m.View().Content, "残り") {
		t.Errorf("basis 未着なのに ETA が出ている:\n%s", m.View().Content)
	}
	// basis (prev の完了 job 100s) が届く → 残り ~40s
	m.Update(basisMsg{targets: []string{prev}, batch: CIBatch{
		Statuses: map[string]CIState{prev: StateSuccess},
		Details:  map[string][]CheckDetail{prev: {{Name: "build", State: StateSuccess, Duration: 100 * time.Second}}},
		PRs:      map[string]*PRRef{},
	}})
	if !strings.Contains(m.View().Content, "残り ~40s") {
		t.Errorf("basis 補充後に ETA が出ていない:\n%s", m.View().Content)
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
	if !strings.Contains(m.View().Content, "Check はありません") {
		t.Errorf("Check なし表示がない:\n%s", m.View().Content)
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

// 猶予ポーリング: rerun 直後は状態がまだ pending に映らないので、panelGrace の残回数だけ
// パネル SHA を追従対象に留め、尽きたら止まる。pending が映れば通常追従へ引き継がれる。
func TestCIPollRerunGrace(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses = statusesFor(m, StateFailure) // pending でない = 通常は追従対象外
	withFailedJob(m, 0, 7, StateFailure)
	m.openPanel()
	m.panelGrace = 2
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil {
		t.Fatal("猶予中なのにポーリングが止まった")
	}
	if m.panelGrace != 1 || !m.ciPollInFlight {
		t.Fatalf("猶予が減らない / 再取得が走らない: grace=%d inFlight=%v", m.panelGrace, m.ciPollInFlight)
	}
	// 猶予が尽きたら (pending も見えないままなら) 停止する
	m.ciPollInFlight, m.panelGrace = false, 0
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd != nil {
		t.Fatal("猶予が尽きたのにポーリングが続いた")
	}
	// rerun が GraphQL に映った (pending) 後は猶予に依らず追従が続く
	m.statuses[sha] = StatePending
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil || !m.ciPollInFlight {
		t.Fatal("pending が見えたのに追従されない")
	}
	// closePanel で猶予は破棄される (パネル SHA を狙う猶予なので)
	m.panelGrace = 5
	m.closePanel()
	if m.panelGrace != 0 {
		t.Fatal("closePanel で猶予が破棄されない")
	}
}

// 回帰 (レビュー確定 medium): ciPollMsg の自己更新チェーンは single-flight。開始点が複数
// (起動時の ciResultMsg / detailMsg / refetchAfterPush / rerunMsg / openPanel) あっても
// 二重チェーンを張らない (GraphQL ポーリング倍化の防止)。追従対象が無いときは張らない。
func TestEnsureCIPollSingleFlight(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess
	if cmd := m.ensureCIPoll(); cmd != nil || m.ciPolling {
		t.Fatal("追従対象が無いのにチェーンを張った")
	}
	m.statuses[sha] = StatePending
	if cmd := m.ensureCIPoll(); cmd == nil || !m.ciPolling {
		t.Fatal("初回 ensureCIPoll がチェーンを張らない")
	}
	if cmd := m.ensureCIPoll(); cmd != nil {
		t.Fatal("チェーンが生きているのに 2 本目を張った (二重化)")
	}
	// 決着でチェーンが止まった後は再アームできる
	m.statuses[sha] = StateSuccess
	m.Update(ciPollMsg{gen: m.ciPollGen})
	if m.ciPolling {
		t.Fatal("決着後もチェーンが生きている")
	}
	m.statuses[sha] = StatePending
	if cmd := m.ensureCIPoll(); cmd == nil {
		t.Fatal("停止後に再アームできない")
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
	m.statuses[m.commits[0].SHA] = StatePending // 実行中 = 追従対象
	m.openPanel()                               // chain #1 が張られる
	if !m.ciPolling {
		t.Fatal("pending で openPanel がチェーンを張らない")
	}
	m.Update(rerunMsg{sha: m.commits[0].SHA}) // rerun 成功
	if !m.ciPolling {
		t.Fatal("rerun 後に ciPolling が落ちた")
	}
	if m.ensureCIPoll() != nil {
		t.Fatal("rerun 後もチェーンは 1 本のはず (二重化した)")
	}
}

// job 詳細ポップアップの本文にスクロールバー列が出ても枠が崩れない (全行が同じ表示幅) ことと、
// 全行が収まるときはバー列が出ないこと。withScrollbar の単体テストではなく描画経路 (boxLines)
// で幅の均一性を見る (rows と len(lines) の噛み合わせが崩れる回帰を捕まえる)。
func TestJobDetailBoxLinesScrollbar(t *testing.T) {
	logLines := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "log line"
		}
		return out
	}
	const width, rows = 50, 8
	o := newJobDetailOverlay()
	o.open = true

	// 溢れる: バー列あり + 幅は均一 + offset に応じて thumb が動く
	o.cache.store("over", logLines(50), "over")
	o.offset = 20
	box := o.boxLines(width, false, "", "job", "over", rows)
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
	uniformWidth(t, o.boxLines(width, true, "", "job", "over", rows))

	// 収まる: バー列なし (本文幅が戻る)。幅は溢れる場合と同じ (枠幅は不変)
	o.cache.store("fit", logLines(rows-2), "fit")
	o.offset = 0
	fit := o.boxLines(width, false, "", "job", "fit", rows)
	if w := uniformWidth(t, fit); w != dispWidth(stripANSI(box[0])) {
		t.Fatalf("収まる場合の枠幅 = %d, 溢れる場合 = %d", w, dispWidth(stripANSI(box[0])))
	}
	for i, l := range fit[1 : len(fit)-2] {
		if strings.Contains(l, scrollbarThumbGlyph) {
			t.Fatalf("収まるのに thumb が出ている (行 %d): %q", i, l)
		}
	}
}

// エディタが 0 以外で終了したとき (nvim の :cq 等) は、ファイルが保存済みなので取り直す。
// 🚨 起動失敗 (PATH に無い等) と分けるのが要点。混ぜて「エラーなら reload しない」にすると、
// 保存済みの編集が出ないまま古い内容を最新として表示する (editorClosedMsg の doc)。
func TestBrowseEditorExitErrorStillReloads(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv = *loadedView(realIssue(t))

	// 実物の *exec.ExitError を作る (errors.As の分岐を本物で通す)
	exitErr := exec.Command("sh", "-c", "exit 1").Run()
	if exitErr == nil {
		t.Fatal("前提が崩れた: exit 1 がエラーにならない")
	}
	// 🚨 戻り値の cmd != nil では検証にならない (maybeTick も Cmd を返すので、reload を
	// 飛ばす変異が通ってしまった)。取り直しが実際に走ったか = scanning が立ったかを見る。
	m.Update(editorClosedMsg{err: exitErr})
	if !m.issuesOv.scanning {
		t.Error("異常終了で取り直しが走らない (保存済みの編集が反映されない)")
	}
	if m.toast.ok || !strings.Contains(m.toast.text, "異常終了") {
		t.Errorf("異常終了の通知が出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}

	// 起動失敗はファイルが変わっていないので取り直さない (分岐が効いていること)
	m2 := newTestBrowse(t, 1, nil, nil)
	m2.issuesOv = *loadedView(realIssue(t))
	m2.Update(editorClosedMsg{err: errors.New("exec: \"zz\": executable file not found in $PATH")})
	if m2.issuesOv.scanning {
		t.Error("起動失敗で取り直しが走った (ファイルは変わっていないので不要)")
	}
	if !strings.Contains(m2.toast.text, "開けませんでした") {
		t.Errorf("起動失敗の通知が出ない: %q", m2.toast.text)
	}
}

// 並行取得ガード: in-flight 中 / 一括取得中の周期発火は timer だけ繋いで fetch を重ねない
// (同一 SHA への GraphQL 並行は完了順で statuses/details が上書きされるため)。
func TestCIPollSkipsFetchWhileBusy(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StatePending
	calls := 0
	orig := ciPollFetch
	ciPollFetch = func(Repo, []string, func(CIBatch, *GHError) tea.Msg) tea.Cmd {
		calls++
		return nil
	}
	t.Cleanup(func() { ciPollFetch = orig })

	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil {
		t.Fatal("周期発火で timer が繋がらない")
	}
	if calls != 1 {
		t.Fatalf("1 回目で fetch が走らない: calls=%d", calls)
	}
	// in-flight のまま次の周期が来ても fetch は増えない
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil {
		t.Fatal("in-flight 中に timer が切れた (追従が止まる)")
	}
	if calls != 1 {
		t.Fatalf("in-flight 中に fetch を重ねた: calls=%d", calls)
	}
	// 一括取得 (fetching) 中も重ねない
	m.ciPollInFlight, m.fetching = false, true
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil {
		t.Fatal("一括取得中に timer が切れた")
	}
	if calls != 1 {
		t.Fatalf("一括取得と fetch を重ねた: calls=%d", calls)
	}
}

// CI が 1 つも現れない (workflow を持たない repo で push した) ケースは ciAwaitMaxAttempts で
// 諦める。諦めた後は追従対象が無くなるのでチェーンも止まる (永久ポーリングしない)。
func TestCIPollAwaitCapGivesUp(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.awaitCI = map[string]bool{sha: true}
	none := CIBatch{Statuses: map[string]CIState{sha: StateNone}}
	orig := ciPollFetch
	ciPollFetch = func(Repo, []string, func(CIBatch, *GHError) tea.Msg) tea.Cmd { return nil }
	t.Cleanup(func() { ciPollFetch = orig })

	// 打ち切りは周期 (ciPollMsg) で数える = ciPollInterval × ciAwaitMaxAttempts の時間予算。
	// 結果着弾では数えない (一括取得が複数チャンクに割れた回で余分に進まないこと)。
	m.Update(ciPollResultMsg{targets: []string{sha}, batch: none})
	if m.awaitAttempts != 0 {
		t.Fatalf("結果着弾で試行回数が進んだ: %d", m.awaitAttempts)
	}
	for i := range ciAwaitMaxAttempts - 1 {
		m.Update(ciPollMsg{gen: m.ciPollGen})
		m.ciPollInFlight = false // 結果が着弾した相当 (CI はまだ現れない)
		m.Update(ciPollResultMsg{targets: []string{sha}, batch: none})
		if !m.awaitCI[sha] {
			t.Fatalf("%d 周期目で諦めた (上限は %d)", i+1, ciAwaitMaxAttempts)
		}
	}
	m.Update(ciPollMsg{gen: m.ciPollGen})
	if len(m.awaitCI) != 0 {
		t.Fatalf("上限に達しても諦めない: awaitCI=%v attempts=%d", m.awaitCI, m.awaitAttempts)
	}
	if len(m.ciPollTargets()) != 0 {
		t.Fatal("諦めた後も追従対象が残っている (永久ポーリング)")
	}
}

// 起動時にキャッシュ済みの pending がある (= 初回 fetch が走らない) ケースでも追従を始める。
// ciResultMsg 起点の開始点をどれも踏まないため、Init 自身が張らないと「pending なのに追わない」
// 状態になる (pending の cache TTL 内に再起動した場合に踏む)。
func TestCIPollArmedAtInitFromCachedPending(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StatePending
	m.fetching = false // キャッシュヒットで取得なし
	m.Init()
	if !m.ciPolling {
		t.Error("キャッシュ済み pending で起動したのに追従チェーンが張られない")
	}
}

// 回帰: 「1 件失敗 + 1 件実行中」のコミットは aggregateRollup が失敗を優先するため statuses が
// StateFailure になり、pending 判定だけでは追従から漏れる (パネルの経過時間・job 状態が固まる)。
// 複数失敗のうち 1 つを rerun した直後もこの形になるので、猶予 (panelGrace) では代替できない。
func TestCIPollFollowsRunningJobUnderFailureRollup(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateFailure
	m.details[sha] = []CheckDetail{
		{Name: "lint", State: StateFailure, CheckID: 7},
		{Name: "test", State: StatePending, CheckID: 8, StartedAt: time.Now().Add(-time.Minute)},
	}
	if !m.hasRunningJob(sha) {
		t.Fatal("前提: 実行中 job が居ない (この形を作れていないとテストが無意味化する)")
	}
	if got := m.ciPollTargets(); len(got) != 1 || got[0] != sha {
		t.Fatalf("実行中 job があるのに追従対象にならない: %v", got)
	}
	// 猶予 0 (rerun 直後ではない) でも周期で追い続ける
	m.panelGrace = 0
	if _, cmd := m.Update(ciPollMsg{gen: m.ciPollGen}); cmd == nil || !m.ciPollInFlight {
		t.Fatal("実行中 job があるのに周期で再取得されない")
	}
	// 全 job が決着したら対象から外れる (永久ポーリングしない)
	m.details[sha] = []CheckDetail{
		{Name: "lint", State: StateFailure, CheckID: 7},
		{Name: "test", State: StateSuccess, CheckID: 8},
	}
	if got := m.ciPollTargets(); len(got) != 0 {
		t.Fatalf("決着後も追従対象に残っている: %v", got)
	}
}

// 回帰: リロード (ciPollGen が進む) を跨いで着弾した古い poll 結果は捨てる。マージすると
// 入れ替わった statuses を古い観測で巻き戻す。ただし in-flight は必ず下ろす — 下ろさないと
// 以降の周期が永久に fetch を見送る (reloadAfterPull は「結果着弾まで in-flight 維持」が前提)。
func TestCIPollResultFromStaleGenerationDropped(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	sha := m.commits[0].SHA
	m.statuses[sha] = StateSuccess // リロード後に取り直した新しい観測
	m.ciPollInFlight = true
	staleGen := m.ciPollGen
	m.ciPollGen++ // reloadAfterPull 相当

	m.Update(ciPollResultMsg{gen: staleGen, targets: []string{sha}, batch: CIBatch{
		Statuses: map[string]CIState{sha: StatePending}, // 古い観測
	}})
	if m.statuses[sha] != StateSuccess {
		t.Fatalf("古い結果で statuses が巻き戻った: %v", m.statuses[sha])
	}
	if m.ciPollInFlight {
		t.Fatal("世代不一致で in-flight が下りない (以降の周期が永久に fetch を見送る)")
	}
}
