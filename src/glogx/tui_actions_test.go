package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// b → y/N → git push (glogx の独自機能)。
// push/pull 確認は Enter を y と同じ「実行」として扱う (ユーザー要望 2026-07-21)。
func TestBrowseConfirmEnterConfirms(t *testing.T) {
	// push: Enter で実行
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	var pushed int
	origPush := runGitPush
	runGitPush = func(context.Context) error { pushed++; return nil }
	t.Cleanup(func() { runGitPush = origPush })
	m.handleKey("b")
	if !m.actModal.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	if _, cmd := m.handleKey("enter"); cmd == nil || !m.actModal.pushing || m.actModal.pushConfirm {
		t.Fatalf("Enter で push が実行されない: cmd=%v pushing=%v confirm=%v", cmd != nil, m.actModal.pushing, m.actModal.pushConfirm)
	}
	// pull: Enter で実行
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	origPull := runGitPullRebase
	runGitPullRebase = func(context.Context) error { return nil }
	t.Cleanup(func() { runGitPullRebase = origPull })
	m2.handleKey("u")
	if !m2.actModal.pullConfirm {
		t.Fatal("u で pull 確認に入らない")
	}
	if _, cmd := m2.handleKey("enter"); cmd == nil || !m2.actModal.pulling || m2.actModal.pullConfirm {
		t.Fatalf("Enter で pull が実行されない: cmd=%v pulling=%v confirm=%v", cmd != nil, m2.actModal.pulling, m2.actModal.pullConfirm)
	}
}

// C → claude update (確認なし即実行。glogx の独自機能)。
func TestBrowseUpdateFlow(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	var calls int
	orig := runClaudeUpdate
	runClaudeUpdate = func() (string, string, error) { calls++; return "2.1.216", "2.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate = orig })

	// C 直後は「既に latest か」の判定中で、モーダルはまだ出ない (早期リターン時に
	// 一瞬モーダルが光るのを防ぐ 2 段構え。ユーザー指摘 2026-08-12)
	_, cmd := m.handleKey("C")
	if cmd == nil || m.actModal.updating {
		t.Fatalf("C 直後にモーダルが立っている (判定前に updating が立つと早期リターンで光る): cmd=%v updating=%v", cmd != nil, m.actModal.updating)
	}
	// 判定 Cmd を実行 → テスト環境はキャッシュ無しなので updateBeginMsg → 実更新開始
	var begin tea.Msg
	var walk func(tea.Msg)
	walk = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					walk(c())
				}
			}
		case updateBeginMsg:
			begin = v
		}
	}
	walk(cmd())
	if begin == nil {
		t.Fatal("判定 Cmd が updateBeginMsg を返さない")
	}
	_, runCmd := m.Update(begin)
	if runCmd == nil || !m.actModal.updating {
		t.Fatalf("updateBeginMsg で claude update が始まらない: cmd=%v updating=%v", runCmd != nil, m.actModal.updating)
	}
	cmd = runCmd
	// 実行中は spinner モーダルが出て、終了できない旨も表示する
	m.width, m.height = 80, 20
	if v := stripANSI(m.View().Content); !strings.Contains(v, "claude update") || !strings.Contains(v, "updating") ||
		!strings.Contains(v, "完了まで終了できません") {
		t.Fatal("claude update 実行中モーダルが描画されない")
	}
	// update 中は Ctrl-G/Ctrl-C で終了できない (自己更新の途中 kill を防ぐ)
	if _, qcmd := m.handleKey("ctrl+g"); qcmd != nil || m.done || !m.actModal.updating {
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
	if m.actModal.updating {
		t.Fatal("updateMsg 後も updating のまま")
	}
	// 変わった場合は成功トーストに "vX → vY" が出る (旧: キー待ちの結果ダイアログ)
	if !m.toast.visible() || !m.toast.ok || !strings.Contains(m.toast.text, "v2.1.216 → v2.2.0") {
		t.Fatalf("バージョン変化がトーストに出ない: visible=%v ok=%v text=%q", m.toast.visible(), m.toast.ok, m.toast.text)
	}
	// トーストなのでキー待ちで塞がらない: 通常キーは本来の動作 (カーソル移動) をする
	if _, _ = m.handleKey("j"); m.cursor != 0 {
		t.Fatalf("トースト表示中に j が消費された (cursor=%d)", m.cursor)
	}

	// 変わらなかった場合は「変更なし」
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	runClaudeUpdate = func() (string, string, error) { return "2.2.0", "2.2.0", nil }
	_, cmd2 := m2.handleKey("C")
	deliverUpdateMsg(m2, cmd2)
	if !m2.toast.visible() || !strings.Contains(m2.toast.text, "最新版") || !strings.Contains(m2.toast.text, "v2.2.0") {
		t.Fatalf("最新版がトーストに出ない: visible=%v text=%q", m2.toast.visible(), m2.toast.text)
	}
}

// 更新失敗 (runClaudeUpdate が err を返す) 経路: updating が必ず解けて error トーストに
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
	deliverUpdateMsg(m, cmd)

	if m.actModal.updating {
		t.Fatal("更新失敗後も updating のまま (無限ブロックから復帰できない)")
	}
	if !m.toast.visible() || m.toast.ok || !strings.Contains(m.toast.text, "更新に失敗") || !strings.Contains(m.toast.text, "タイムアウト") {
		t.Fatalf("失敗理由が error トーストに出ない: visible=%v ok=%v text=%q", m.toast.visible(), m.toast.ok, m.toast.text)
	}
	// 失敗文言は w でコピーできるよう lastWarning にも残る (showWarning 経由。issue 026 の規律)
	if !strings.Contains(m.lastWarning, "更新に失敗") {
		t.Fatalf("更新失敗が lastWarning に残らない (w でコピーできない): %q", m.lastWarning)
	}
	// updating が解けたので q は本来の終了として効く (トーストはキー待ちで塞がない)
	m.handleKey("q")
	if !m.done {
		t.Fatal("updating 解除後に q で終了できない")
	}
}

// quit (Ctrl-C) 時に走行中の push/pull が孤児化しないよう actModal.stop() が実行中の git を
// cancel する (leak 監査 2026-07-23: stall 中に Ctrl-C で抜けると git 子プロセスが孤児化する穴)。
// stub を ctx.Done() で block させ、stop() で解除されることを確認する。
func TestActionModalStopCancelsRunningPush(t *testing.T) {
	orig := runGitPush
	runGitPush = func(ctx context.Context) error {
		<-ctx.Done() // cancel されるまでブロック (stall した git を模す)
		return ctx.Err()
	}
	t.Cleanup(func() { runGitPush = orig })

	a := &actionModal{pushConfirm: true}
	consumed, action := a.handleKey("y")
	if !consumed || action == nil || !a.pushing {
		t.Fatalf("push が始まらない: consumed=%v action=%v pushing=%v", consumed, action != nil, a.pushing)
	}
	if a.cancel == nil {
		t.Fatal("走行中 push の cancel が保持されていない (quit で中断できない)")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- action() }() // action は runGitPush(ctx) で block
	a.stop()                         // quit 相当: 走行中 push を cancel
	select {
	case msg := <-done:
		if pm, ok := msg.(pushMsg); !ok || pm.err == nil {
			t.Errorf("cancel 後の結果 = %#v; want pushMsg{err=context.Canceled}", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop() が走行中 push を cancel しなかった (git が孤児化する)")
	}
}

// push 演出の時間経過が timeNow シームを通ることを固定時計で pin する (issue 038)。上の
// TestBrowsePushFlow は pushAnimNext のゼロ値書き換えで段を進めるため、実装が time.Now 直呼びに
// 退行しても green のままになる。こちらは時計を凍結し「実時間がいくら経っても、凍結時計が
// pushAnimStep 進むまでは段が進まない」ことでシーム経由を守る。
func TestPushAnimUsesClockSeam(t *testing.T) {
	advance := stubClock(t)
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.statuses[m.commits[1].SHA] = StateUnpushed
	if !m.startPushAnim() {
		t.Fatal("未 push が 2 件あるのに startPushAnim が始まらない")
	}
	m.Update(tickMsg{})
	if _, ok := m.statuses[m.commits[1].SHA]; !ok {
		t.Fatal("凍結時計で pushAnimStep 経過前なのに演出が進んだ (time.Now 直呼びの疑い)")
	}
	advance(pushAnimStep)
	m.Update(tickMsg{})
	if _, ok := m.statuses[m.commits[1].SHA]; ok {
		t.Fatal("凍結時計を pushAnimStep 進めても最古の unpushed が消えない")
	}
	if _, ok := m.pushSlides[m.commits[1].SHA]; !ok {
		t.Fatal("境界通過した区画の沈み込み開始時刻が記録されない")
	}
	// 沈み込みの掃除も凍結時計基準: pushSlideDuration 経過で区画が通常表示へ戻る
	advance(pushSlideDuration)
	m.Update(tickMsg{})
	if _, ok := m.pushSlides[m.commits[1].SHA]; ok {
		t.Fatal("pushSlideDuration 経過後も沈み込みが掃除されない")
	}
}

func TestBrowsePushFlow(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.statuses[m.commits[1].SHA] = StateUnpushed // 2 コミットまとめて push するケース
	var pushed int
	orig := runGitPush
	runGitPush = func(context.Context) error { pushed++; return nil }
	t.Cleanup(func() { runGitPush = orig })
	// b で確認に入り、n でキャンセル (push されない)
	m.handleKey("b")
	if !m.actModal.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	// 確認中は中央モーダルが出る (幅より狭いボックス + 左パディングでセンタリング)
	m.width, m.height = 80, 20
	if v := stripANSI(m.View().Content); !strings.Contains(v, "git push") || !strings.Contains(v, "push します") {
		t.Fatal("push 確認モーダルが描画されない")
	}
	m.handleKey("n")
	if m.actModal.pushConfirm || pushed != 0 {
		t.Fatalf("n でキャンセルされない: confirm=%v pushed=%d", m.actModal.pushConfirm, pushed)
	}
	// y で push が走り、成功で未 push が unknown へ落ちて再取得に乗る
	m.handleKey("b")
	_, cmd := m.handleKey("y")
	if cmd == nil || !m.actModal.pushing {
		t.Fatal("y で push が始まらない")
	}
	deliverMsgs(m, cmd(), func(msg tea.Msg) bool { _, ok := msg.(pushMsg); return ok })
	if pushed != 1 {
		t.Fatalf("push 実行回数 = %d, want 1", pushed)
	}
	if m.actModal.pushing {
		t.Fatal("pushMsg 後も pushing のまま")
	}
	// push 成功でまず演出が始まる: 未 push が古い順に 1 tick ごとにスピナー表示へ落ち、
	// push 境界の罫線が 1 段ずつ上へスライドする (startPushAnim)
	if !m.pushAnimating {
		t.Fatal("push 成功で演出が始まらない")
	}
	// pushAnimStep 経過前の tick では進まない (80ms tick ごとに 1 段進むと目で追えない)
	m.Update(tickMsg{})
	if _, ok := m.statuses[m.commits[1].SHA]; !ok {
		t.Fatal("pushAnimStep 経過前に演出が進んだ")
	}
	// 以降は経過済みとして 1 tick = 1 段で進める
	m.pushAnimNext = time.Time{}
	m.Update(tickMsg{})
	if _, ok := m.statuses[m.commits[1].SHA]; ok {
		t.Fatal("1 tick 目で最古の unpushed が消えない")
	}
	if m.statuses[m.commits[0].SHA] != StateUnpushed {
		t.Fatal("tip が先に消えた (演出は古い順のはず)")
	}
	m.pushAnimNext = time.Time{}
	m.Update(tickMsg{}) // 残る tip が消える
	m.pushAnimNext = time.Time{}
	m.Update(tickMsg{}) // 全部消えた次の段で演出終了 → 再取得へ
	if m.pushAnimating {
		t.Fatal("演出が終わらない")
	}
	// 演出後はリスト全体のキャッシュが破棄され、全 SHA が再取得に乗る
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
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: []string{newSHA, m.commits[1].SHA}, batch: CIBatch{Statuses: map[string]CIState{
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
	m.Update(ciResultMsg{epoch: m.fetchEpoch, shas: []string{newSHA}, batch: CIBatch{Statuses: map[string]CIState{newSHA: StatePending}}})
	if m.pushPoll[newSHA] {
		t.Fatal("CI が見えてもポーリングが止まらない")
	}
	if m.statuses[newSHA] != StatePending {
		t.Fatalf("pending が反映されない: %v", m.statuses[newSHA])
	}
	if !m.toast.visible() || !m.toast.ok || !strings.Contains(m.toast.text, "push") {
		t.Fatalf("push 完了トーストが出ない: visible=%v ok=%v text=%q", m.toast.visible(), m.toast.ok, m.toast.text)
	}
}

// u → y/N → git pull --rebase → 一覧の全面リロード (glogx の独自機能)。
func TestBrowsePullFlow(t *testing.T) {
	newTempRepo(t, []string{"first", "second"}) // reloadAfterPull が実 git を読むため
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.opts = &Options{MaxCount: 20}
	var pulled int
	orig := runGitPullRebase
	runGitPullRebase = func(context.Context) error { pulled++; return nil }
	t.Cleanup(func() { runGitPullRebase = orig })
	// u で確認に入り、n でキャンセル
	m.handleKey("u")
	if !m.actModal.pullConfirm {
		t.Fatal("u で pull 確認に入らない")
	}
	m.width, m.height = 80, 20
	if v := stripANSI(m.View().Content); !strings.Contains(v, "pull --rebase") {
		t.Fatal("pull 確認モーダルが描画されない")
	}
	m.handleKey("n")
	if m.actModal.pullConfirm || pulled != 0 {
		t.Fatalf("n でキャンセルされない: confirm=%v pulled=%d", m.actModal.pullConfirm, pulled)
	}
	// y で pull が走り、成功で一覧が実 repo の内容にリロードされる
	m.handleKey("u")
	_, cmd := m.handleKey("y")
	if cmd == nil || !m.actModal.pulling {
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
	if m.actModal.pulling {
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
	runGitPullRebase = func(context.Context) error {
		return errors.New("conflict のため rebase を中断して元に戻しました")
	}
	m2.handleKey("u")
	m2.handleKey("y")
	m2.Update(pullMsg{err: errors.New("conflict のため rebase を中断して元に戻しました")})
	if m2.toast.visible() == false || m2.toast.ok || !strings.Contains(m2.toast.text, "conflict") {
		t.Fatalf("pull 失敗トーストが出ない: visible=%v ok=%v text=%q", m2.toast.visible(), m2.toast.ok, m2.toast.text)
	}
}

// tmux prefix (popup 内では tmux に届かない) の誤爆フィードバック。
// prefix キー自体だけを飲んで右下 toast で通知し、続くキーは通常操作として処理する。
func TestBrowseTmuxPrefixFeedback(t *testing.T) {
	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.width, m.height = 80, 20
	m.Update(prefixMsg{key: "ctrl+t"})
	// prefix 単体: toast を出す (カーソルは動かない)
	m.handleKey("ctrl+t")
	if !strings.Contains(m.toast.text, "効きません") || m.toast.info || m.toast.ok {
		t.Fatalf("prefix の失敗 toast が出ない: text=%q info=%v ok=%v", m.toast.text, m.toast.info, m.toast.ok)
	}
	if m.cursor != 0 {
		t.Fatal("prefix 単体でカーソルが動いた")
	}
	// prefix に続くキーは飲み込まない = 押し間違い後の打ち直しがそのまま効く
	// (ユーザー要望 2026-08-16。以前は 1 キー飲んで「prefix+j」の 2 枚目 toast を出していた)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatal("prefix 直後のキーが飲み込まれてカーソルが動かない")
	}
	if strings.Contains(m.toast.text, "prefix+j") {
		t.Fatalf("prefix に続くキーで失敗 toast が出た: %q", m.toast.text)
	}
	// prefix 連打 (tmux のリテラル送信の癖) は毎回同じ案内を出すだけ
	m.handleKey("ctrl+t")
	m.handleKey("ctrl+t")
	if !strings.Contains(m.toast.text, "効きません") || m.cursor != 1 {
		t.Fatalf("prefix 連打の案内が出ない: text=%q cursor=%d", m.toast.text, m.cursor)
	}
	// y/N 確認モーダル中はモーダルの語彙を優先: C-t は「任意キー = キャンセル」で
	// prefix 検知は発動しない (続く y が飲み込まれる事故の防止)
	m.statuses[m.commits[0].SHA] = StateUnpushed
	m.handleKey("b")
	if !m.actModal.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	m.handleKey("ctrl+t")
	if m.actModal.pushConfirm {
		t.Fatalf("確認モーダル中の C-t がキャンセルにならない: confirm=%v", m.actModal.pushConfirm)
	}
	// tmux 外 (prefix 不明) では機能オフ = ctrl+t は何もしない
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.Update(prefixMsg{key: ""})
	m2.handleKey("ctrl+t")
	if m2.toast.visible() {
		t.Fatalf("tmux 外で prefix 案内が出た: %q", m2.toast.text)
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
	if m.actModal.pushConfirm {
		t.Fatal("未 push なしで push 確認に入った")
	}
	// キー待ちのモーダルでなく右下トーストで出す (ユーザー要望 2026-07-25)
	if !m.toast.visible() || m.toast.ok || !strings.Contains(m.toast.text, "未 push のコミットはありません") {
		t.Fatalf("未 push なしの通知がトーストで出ない: visible=%v ok=%v text=%q", m.toast.visible(), m.toast.ok, m.toast.text)
	}
	// 入場スライドを holding まで進めてから描画を見る (entering の途中は shown=0 で未描画)
	m.width, m.height = 80, 20
	m.maybeTick()
	for i := 0; i < 200 && m.toast.phase != toastHolding; i++ {
		m.Update(tickMsg{})
	}
	if v := stripANSI(m.View().Content); !strings.Contains(v, "未 push のコミットはありません") {
		t.Fatalf("トーストが描画されない (phase=%d shown=%d)", m.toast.phase, m.toast.shown)
	}
	// トーストはキーを消費しない (モーダルと違い、次のキーが本来の動作をする)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("トースト表示中に j が消費された (cursor=%d, want 1)", m.cursor)
	}
}

// 実行中 (pushing/pulling) ガードは一般キーを飲むが、quit だけは updating のときのみブロック
// される (pushing/pulling 中の Ctrl-C は終了できる)。この非対称は claude update だけ自己
// バイナリ更新の中断が危険なため。抽出でこの分岐が壊れないよう固定する。
// push/pull 実行中の終了ガード (ユーザー選定 2026-07-23): 途中終了は不整合を招くので 1 回目の
// Ctrl-C はブロックし、2 回目で強制終了する。update は自己更新中断が危険なので常にブロック。
func TestBrowseRunningQuitGuard(t *testing.T) {
	// pushing 中: 一般キー (j) は飲まれてカーソルは動かない
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateSuccess)
	m.actModal.pushing = true
	m.handleKey("j")
	if m.cursor != 0 {
		t.Errorf("pushing 中に j が飲まれずカーソルが動いた: cursor=%d", m.cursor)
	}
	// pushing 中の 1 回目 Ctrl-C はブロック (終了しない) し、force-quit を arm する
	if _, _ = m.handleKey("ctrl+c"); m.done {
		t.Error("pushing 中の 1 回目 Ctrl-C で終了してしまった (1 回目はブロックする契約)")
	}
	if !m.actModal.forceQuitArmed {
		t.Error("1 回目 Ctrl-C で force-quit が arm されていない")
	}
	// 2 回目の Ctrl-C で強制終了 (quit() が actModal.stop() で走行中 git を cancel)
	if _, _ = m.handleKey("ctrl+c"); !m.done {
		t.Error("pushing 中の 2 回目 Ctrl-C で強制終了できない")
	}

	// pulling も同じ (1 回目ブロック → 2 回目で終了)
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.actModal.pulling = true
	if _, _ = m2.handleKey("ctrl+c"); m2.done {
		t.Error("pulling 中の 1 回目 Ctrl-C で終了してしまった")
	}
	if _, _ = m2.handleKey("ctrl+c"); !m2.done {
		t.Error("pulling 中の 2 回目 Ctrl-C で強制終了できない")
	}

	// updating 中: Ctrl-C は何回押しても終了しない (自己更新中断が危険。escape は updateTimeout のみ)
	m3 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m3.actModal.updating = true
	m3.handleKey("ctrl+c")
	if _, _ = m3.handleKey("ctrl+c"); m3.done {
		t.Error("updating 中は Ctrl-C 2 回でも終了してはいけない (常にブロック)")
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

func TestBrowseRerunFlow(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	var gotJobID int64
	var gotRepo Repo
	orig := runJobRerun
	runJobRerun = func(_ context.Context, repo Repo, jobID int64) error {
		gotRepo, gotJobID = repo, jobID
		return nil
	}
	t.Cleanup(func() { runJobRerun = orig })
	m.openPanel()
	m.handleKey("j") // job へフォーカス
	// r で確認に入り、n でキャンセル (実行されない)
	m.handleKey("r")
	if !m.actModal.rerunConfirm || m.actModal.rerunJobName != "lint" {
		t.Fatalf("r で再実行確認に入らない: confirm=%v name=%q", m.actModal.rerunConfirm, m.actModal.rerunJobName)
	}
	if v := stripANSI(m.View().Content); !strings.Contains(v, "CI 再実行") || !strings.Contains(v, "lint") {
		t.Fatal("再実行確認モーダルが描画されない")
	}
	m.handleKey("n")
	if m.actModal.rerunConfirm || gotJobID != 0 {
		t.Fatalf("n でキャンセルされない: confirm=%v jobID=%d", m.actModal.rerunConfirm, gotJobID)
	}
	// y で実行され、成功でトースト + 猶予ポーリングが始まる
	m.handleKey("r")
	_, cmd := m.handleKey("y")
	if cmd == nil || !m.actModal.rerunning {
		t.Fatal("y で再実行が始まらない")
	}
	deliverMsgs(m, cmd(), func(msg tea.Msg) bool { _, ok := msg.(rerunMsg); return ok })
	if gotJobID != 7 || gotRepo.Owner != "o" || gotRepo.Name != "r" {
		t.Fatalf("gh run rerun の対象が違う: repo=%+v jobID=%d", gotRepo, gotJobID)
	}
	if m.actModal.rerunning {
		t.Fatal("rerunMsg 後も rerunning のまま")
	}
	if !m.toast.visible() || !strings.Contains(m.toast.text, "再実行") {
		t.Fatalf("成功トーストが出ない: %q", m.toast.text)
	}
	if m.panelPollGrace != rerunPollGrace {
		t.Fatalf("猶予ポーリングが張られない: grace=%d want %d", m.panelPollGrace, rerunPollGrace)
	}
}

func TestBrowseRerunGuards(t *testing.T) {
	orig := runJobRerun
	called := false
	runJobRerun = func(context.Context, Repo, int64) error { called = true; return nil }
	t.Cleanup(func() { runJobRerun = orig })
	// StatusContext (CheckID=0) は再実行不可
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 0, StateFailure)
	m.openPanel()
	m.handleKey("j")
	m.handleKey("r")
	if m.actModal.rerunConfirm || !strings.Contains(m.toast.text, "GitHub Actions") {
		t.Fatalf("StatusContext job で確認に入った / トーストが出ない: %q", m.toast.text)
	}
	// 失敗以外の job は再実行不可
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.statuses = statusesFor(m2, StateSuccess)
	withFailedJob(m2, 0, 7, StateSuccess)
	m2.openPanel()
	m2.handleKey("j")
	m2.handleKey("r")
	if m2.actModal.rerunConfirm || !strings.Contains(m2.toast.text, "失敗") {
		t.Fatalf("成功 job で確認に入った / トーストが出ない: %q", m2.toast.text)
	}
	// タイトル行フォーカス (job 未選択) では確認に入らず、選択を促すトーストを出す
	// (o/Y と挙動を揃える。UX 統一のためユーザー判断で無言 no-op から変更)
	m2.toast = toast{}
	m2.panelCursor = -1
	m2.handleKey("r")
	if m2.actModal.rerunConfirm {
		t.Fatal("タイトル行フォーカスで r が確認に入った")
	}
	if !strings.Contains(m2.toast.text, "job を選択") {
		t.Fatalf("タイトル行フォーカスの r で選択を促すトーストが出ない: %q", m2.toast.text)
	}
	// o (ブラウザで開く) も同様に選択を促す (o/r/Y の挙動統一)
	m2.toast = toast{}
	m2.handleKey("o")
	if !strings.Contains(m2.toast.text, "job を選択") {
		t.Fatalf("タイトル行フォーカスの o で選択を促すトーストが出ない: %q", m2.toast.text)
	}
	if called {
		t.Fatal("ガード経路で runJobRerun が呼ばれた")
	}
}

func TestBrowseRerunFailureShowsToast(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.statuses = statusesFor(m, StateFailure)
	withFailedJob(m, 0, 7, StateFailure)
	orig := runJobRerun
	runJobRerun = func(context.Context, Repo, int64) error { return errors.New("run cannot be rerun") }
	t.Cleanup(func() { runJobRerun = orig })
	m.openPanel()
	m.handleKey("j")
	m.handleKey("r")
	_, cmd := m.handleKey("y")
	if cmd == nil {
		t.Fatal("y で再実行が始まらない")
	}
	deliverMsgs(m, cmd(), func(msg tea.Msg) bool { _, ok := msg.(rerunMsg); return ok })
	if m.actModal.rerunning {
		t.Fatal("失敗後も rerunning のまま")
	}
	if !m.toast.visible() || m.toast.ok || !strings.Contains(m.toast.text, "失敗") {
		t.Fatalf("失敗トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	if m.panelPollGrace != 0 {
		t.Fatal("失敗なのに猶予ポーリングが張られた")
	}
}

// version 通知 (issue 024) は先行トーストを潰さない。⚠️ 以前は「1 枠を譲り合う」ために専用
// タイマーで遅延再送していたが、トーストが積めるようになったので両方が同時に出るのが正しい姿
// (ユーザー要望 2026-07-31)。
func TestBrowseClaudeUpdateToastStacksWithExisting(t *testing.T) {
	// 空きトースト → version 通知 (成功色) を表示
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.Update(claudeUpdateAvailableMsg{latest: "9.9.9"})
	if !m.toast.visible() || !m.toast.ok || !strings.Contains(m.toast.text, "9.9.9") {
		t.Fatalf("空きトーストで version 通知が出ない: visible=%v ok=%v text=%q", m.toast.visible(), m.toast.ok, m.toast.text)
	}

	// 先行 error トースト表示中 → 上に積まれ、先行も残る (どちらも読める)
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.height = 24 // ⚠️ 2 枚出すには窓の高さが要る (低い窓では行数上限で古い方を出さない。toast の doc)
	m2.toast.show("先行警告: ...", false)
	m2.Update(claudeUpdateAvailableMsg{latest: "9.9.9"})
	if !strings.Contains(m2.toast.text, "9.9.9") {
		t.Errorf("最上段が version 通知でない: %q", m2.toast.text)
	}
	if len(m2.toast.older) != 1 || !strings.Contains(m2.toast.older[0].text, "先行警告") {
		t.Errorf("先行 error が消えた (積まれていない): %+v", m2.toast.older)
	}
	// 描画にも両方出る (上が新しい = version 通知)。滑り込みを進めてから見る
	for range 40 {
		if !m2.toast.animating() {
			break
		}
		m2.toast.advance(m2.colored)
	}
	out := stripANSI(m2.View().Content)
	if !strings.Contains(out, "9.9.9") || !strings.Contains(out, "先行警告") {
		t.Errorf("両方が画面に出ていない:\n%s", out)
	}
}

// w で直近の警告をクリップボードへコピー (issue 026)。核となる不変条件は「トーストが消えた後も
// コピーできる」= lastWarning が表示ライフサイクルと独立に保持されること。
func TestBrowseCopyLastWarning(t *testing.T) {
	var copied string
	stubbed := false
	stubClipboardFunc(t, func(text string) error { copied = text; stubbed = true; return nil })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)

	// 警告がまだ無い → コピーせず error トースト (ユーザー要望 2026-07-23)。lastWarning は汚さない
	m.handleKey("w")
	if stubbed || m.toast.ok || !strings.Contains(m.toast.text, "コピーできる警告はありません") {
		t.Fatalf("警告無しで w が誤動作: copied=%q toast=%q ok=%v", copied, m.toast.text, m.toast.ok)
	}
	if m.lastWarning != "" {
		t.Fatalf("『警告なし』トーストが lastWarning を汚した: %q", m.lastWarning)
	}

	// 失敗トースト発行 → lastWarning に残る
	m.showWarning("push に失敗: boom")
	if m.lastWarning != "push に失敗: boom" {
		t.Fatalf("showWarning が lastWarning に残さない: %q", m.lastWarning)
	}

	// 不変条件の核: トースト表示状態をリセット (消滅) しても w でコピーできる
	m.toast = toast{}
	m.handleKey("w")
	if copied != "push に失敗: boom" || !strings.Contains(m.toast.text, "警告をコピーしました") {
		t.Fatalf("トースト消滅後に w でコピーできない: copied=%q toast=%q", copied, m.toast.text)
	}
	if !m.toast.ok {
		t.Fatalf("コピー成功トーストが緑 (ok) でない: ok=%v", m.toast.ok)
	}
}

// コピー失敗時は理由を error トーストに出す。lastWarning は汚さない (コピー対象の警告を保持)。
func TestBrowseCopyLastWarningError(t *testing.T) {
	stubClipboardFunc(t, func(string) error { return errors.New("clipboard down") })
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.showWarning("なにか失敗")
	m.handleKey("w")
	if m.toast.ok || !strings.Contains(m.toast.text, "コピーに失敗しました") {
		t.Fatalf("コピー失敗の error トーストが出ない: %q ok=%v", m.toast.text, m.toast.ok)
	}
	if m.lastWarning != "なにか失敗" {
		t.Fatalf("コピー失敗トーストが lastWarning を汚した: %q", m.lastWarning)
	}
}

// 成功トースト (toast.show(…, true)) は showWarning を通さないので lastWarning を汚さない。
func TestBrowseSuccessToastDoesNotClobberLastWarning(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.showWarning("失敗A")
	m.toast.show("push しました", true) // 成功トースト (showWarning 非経由)
	if m.lastWarning != "失敗A" {
		t.Fatalf("成功トーストで lastWarning が上書きされた: %q", m.lastWarning)
	}
}

// ghErr (CI 取得失敗の sticky 警告) は lastWarning に無くても w で fallback コピーできる (issue 026 #1)。
func TestBrowseCopyWarningGhErrFallback(t *testing.T) {
	copied := stubClipboard(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.ghErr = &GHError{Kind: GHOther, Detail: "boom detail"}
	m.handleKey("w")
	if !strings.Contains(*copied, "取得に失敗") {
		t.Fatalf("ghErr fallback で w がコピーしない: copied=%q", *copied)
	}
}

// 取得失敗系エラー (showWarning 経由) は表示トーストが消えても w でコピーできる (issue 026 #1)。
func TestBrowseCopyWarningFromShowWarning(t *testing.T) {
	copied := stubClipboard(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.showWarning("diff の取得に失敗しました: boom")
	if m.lastWarning != "diff の取得に失敗しました: boom" {
		t.Fatalf("showWarning が lastWarning に残さない: %q", m.lastWarning)
	}
	m.toast = toast{} // トースト消滅をシミュレート
	m.handleKey("w")  // トーストが消えても lastWarning は残る
	if !strings.Contains(*copied, "diff の取得に失敗") {
		t.Fatalf("showWarning が w でコピーされない: copied=%q", *copied)
	}
}

// フレーム ON の browseModel (newTestBrowse は NoFrame:true 固定なので直接構築)。
func newFramedBrowse(t *testing.T, w, h int) *browseModel {
	t.Helper()
	sha := strings.Repeat("a", 40)
	commits := []Commit{{SHA: sha, ShortSHA: sha[:7], Subject: "subject", Author: "koji", AuthorEmail: "k@x", Date: "Thu Jul 16 19:12:47 2026 +0900", RelDate: "now", Message: "subject"}}
	m := newBrowseModel(commits, map[string]CIState{sha: StateSuccess}, nil, Repo{Owner: "o", Name: "r"}, true, &Options{}, false, w, h)
	t.Cleanup(m.cancel)
	return m
}

// 最外周フレーム有効時の寸法と View 出力 (issue 025)。
func TestBrowseFrameView(t *testing.T) {
	m := newFramedBrowse(t, 64, 16)
	if !m.frameActive() {
		t.Fatal("64x16 で frameActive が false")
	}
	if m.contentWidth() != 64-frameHOverhead {
		t.Errorf("contentWidth = %d; want %d", m.contentWidth(), 64-frameHOverhead)
	}
	if m.pageSize() != 16-frameVOverhead {
		t.Errorf("pageSize = %d; want %d", m.pageSize(), 16-frameVOverhead)
	}
	v := m.View().Content
	lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
	// View 出力の行数 = m.height (板 + hint でビューポート一杯)
	if len(lines) != 16 {
		t.Fatalf("View 行数 = %d; want 16 (= m.height)", len(lines))
	}
	// 幅 canary: 全行の実効幅 <= m.width (contentWidth() 置換漏れの最短検出)
	for i, l := range lines {
		if w := dispWidth(l); w > 64 {
			t.Errorf("行 %d の実効幅 = %d > 64: %q", i, w, l)
		}
	}
	// 枠 (上辺 ┌) と下辺/影 (▁) が描かれている
	// 上辺は二重罫線 ╔、下辺は接地ブロック ▁ (ユーザー要望 2026-07-24)
	if !strings.Contains(v, "╔") || !strings.Contains(v, "▁") {
		t.Error("フレーム (二重上辺 + 接地下辺) が描かれていない")
	}
}

// --no-frame / 極小端末では自動 OFF し従来寸法へフォールバック (issue 025)。
func TestBrowseFrameAutoOffAndNoFrame(t *testing.T) {
	// --no-frame 相当 (showFrame=false): 従来寸法
	m := newFramedBrowse(t, 64, 16)
	m.showFrame = false
	if m.frameActive() {
		t.Fatal("showFrame=false でも frameActive")
	}
	if m.contentWidth() != 64 || m.pageSize() != 15 {
		t.Errorf("no-frame で寸法が従来と違う: cw=%d ps=%d; want 64/15", m.contentWidth(), m.pageSize())
	}
	// width < frameMinWidth → 自動 OFF
	if newFramedBrowse(t, 50, 16).frameActive() {
		t.Error("width<frameMinWidth で自動 OFF されない")
	}
	// height < frameMinHeight → 自動 OFF
	if newFramedBrowse(t, 64, 10).frameActive() {
		t.Error("height<frameMinHeight で自動 OFF されない")
	}
}

// 押しっぱなし (キーリピート) は 1 回の入力として扱う (ユーザー報告 2026-08-01)。
//
// ⚠️ 端末はキーを離したことを教えてくれないので、離鍵の代わりに時間で判定する。窓は押される
// たびに更新するので、押し続けている限り 1 回にまとまり、指を離して窓が切れてから次の 1 回になる。
func TestKeyRepeatIsOneInput(t *testing.T) {
	advance := stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	if !m.issuesOv.visible() {
		t.Fatal("前提が崩れた: i で viewer が開かない")
	}
	// 実機の自動リピート (最初 225ms → 以降 30ms。実測) を模して押しっぱなしにする
	advance(225 * time.Millisecond)
	m.handleKey("i")
	for range 20 {
		advance(30 * time.Millisecond)
		m.handleKey("i")
	}
	if !m.issuesOv.visible() {
		t.Fatal("押しっぱなしで viewer が開閉を繰り返した (1 回の入力として扱われていない)")
	}
	// 指を離す = 窓が切れる。次の 1 回は効く
	advance(keyRepeatGuard + time.Millisecond)
	m.handleKey("i")
	if m.issuesOv.visible() {
		t.Fatal("押し直しが効かない (離しても 1 回扱いのまま)")
	}
}

// s (status viewer) も i と同じ toggle なので押しっぱなしを 1 回に丸める (ユーザー報告 2026-08-07)。
func TestKeyRepeatIsOneInputStatusViewer(t *testing.T) {
	advance := stubClock(t)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	stubWorktreeStatus(t, statusRec(" M a.go"), nil)
	m.handleKey("s")
	if !m.statusOv.visible() {
		t.Fatal("前提が崩れた: s で viewer が開かない")
	}
	advance(225 * time.Millisecond)
	m.handleKey("s")
	for range 20 {
		advance(30 * time.Millisecond)
		m.handleKey("s")
	}
	if !m.statusOv.visible() {
		t.Fatal("押しっぱなしで viewer が開閉を繰り返した (1 回の入力として扱われていない)")
	}
	advance(keyRepeatGuard + time.Millisecond)
	m.handleKey("s")
	if m.statusOv.visible() {
		t.Fatal("押し直しが効かない (離しても 1 回扱いのまま)")
	}
}

// ⚠️ 移動系は潰さない: 押しっぱなしでスクロールし続けるのは期待される動作で、潰すと
// 「押しても動かない」壊れ方になる。
func TestKeyRepeatDoesNotBlockMovement(t *testing.T) {
	advance := stubClock(t)

	m := newTestBrowse(t, 5, map[string]CIState{}, nil)
	for range 3 {
		advance(30 * time.Millisecond) // 自動リピートの間隔
		m.handleKey("j")
	}
	if m.cursor != 3 {
		t.Fatalf("j の押しっぱなしでスクロールが止まった: cursor=%d (3 のはず)", m.cursor)
	}
}

// deliverUpdateMsg は startUpdate 系 tea.Cmd の返す Batch を展開し、updateBeginMsg /
// updateMsg を Update へ配送してチェーンを完走させる (判定 → 実更新 → 結果の 2 段構えを
// テストから 1 呼び出しで進める)。
func deliverUpdateMsg(m *browseModel, cmd tea.Cmd) {
	var dl func(tea.Msg)
	dl = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					dl(c())
				}
			}
		case updateBeginMsg, updateMsg:
			_, next := m.Update(v)
			if next != nil {
				dl(next())
			}
		}
	}
	dl(cmd())
}

// writeVersionCache は latest キャッシュを fetchedAt 時点の取得としてテスト用に書く。
func writeVersionCache(t *testing.T, cacheFile, latest string, fetchedAt time.Time) {
	t.Helper()
	base, err := cacheBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveClaudeVersionCache(filepath.Join(base, cacheFile), latest, fetchedAt); err != nil {
		t.Fatal(err)
	}
}

// C / X: 起動時チェックのキャッシュで既に latest と分かるときは自己更新プロセスを起動せず
// 「すでに最新版です」で早期リターンする (ユーザー要望 2026-08-12。従来は毎回 update が走り
// モーダルにロックされた)。
func TestBrowseUpdateSkipsWhenAlreadyLatest(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	// claude 側: installed == cached latest → runClaudeUpdate は呼ばれない
	writeVersionCache(t, claudeVersionCacheFile, "2.2.0", time.Now())
	origFetch := fetchInstalledClaudeVersion
	fetchInstalledClaudeVersion = func(context.Context) string { return "2.2.0" }
	t.Cleanup(func() { fetchInstalledClaudeVersion = origFetch })
	origRun := runClaudeUpdate
	var runCalls int
	runClaudeUpdate = func() (string, string, error) { runCalls++; return "2.2.0", "2.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate = origRun })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.handleKey("C")
	// C 直後にモーダルが立たない (判定前に立てると早期リターン時に一瞬光る。ユーザー指摘 2026-08-12)
	if m.actModal.updating {
		t.Fatal("C 直後 (判定前) に updating モーダルが立っている")
	}
	deliverUpdateMsg(m, cmd)
	if runCalls != 0 {
		t.Fatalf("latest 一致でも claude update が実行された (calls=%d)", runCalls)
	}
	if m.actModal.updating {
		t.Fatal("早期リターン後も updating のまま")
	}
	// トーストは主語 (CLI 名) 付き (ユーザー要望 2026-08-12)
	if !m.toast.visible() || !strings.Contains(m.toast.text, "claude") || !strings.Contains(m.toast.text, "最新版") || !strings.Contains(m.toast.text, "v2.2.0") {
		t.Fatalf("主語付きの最新版トーストが出ない: visible=%v text=%q", m.toast.visible(), m.toast.text)
	}

	// codex 側も同じ早期リターン (鏡像)
	writeVersionCache(t, codexVersionCacheFile, "0.144.6", time.Now())
	origCodexFetch := fetchInstalledCodexVersion
	fetchInstalledCodexVersion = func(context.Context) string { return "0.144.6" }
	t.Cleanup(func() { fetchInstalledCodexVersion = origCodexFetch })
	origCodexRun := runCodexUpdate
	var codexCalls int
	runCodexUpdate = func() (string, string, error) { codexCalls++; return "0.144.6", "0.144.6", nil }
	t.Cleanup(func() { runCodexUpdate = origCodexRun })

	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd2 := m2.handleKey("X")
	deliverUpdateMsg(m2, cmd2)
	if codexCalls != 0 {
		t.Fatalf("latest 一致でも codex update が実行された (calls=%d)", codexCalls)
	}
	if !m2.toast.visible() || !strings.Contains(m2.toast.text, "codex") || !strings.Contains(m2.toast.text, "最新版") {
		t.Fatalf("codex 最新版トーストが出ない: visible=%v text=%q", m2.toast.visible(), m2.toast.text)
	}
}

// 早期リターンしない側の保証: キャッシュが stale / installed が古い場合は従来どおり update を実行する。
func TestBrowseUpdateRunsWhenNotConfirmedLatest(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	origFetch := fetchInstalledClaudeVersion
	t.Cleanup(func() { fetchInstalledClaudeVersion = origFetch })
	origRun := runClaudeUpdate
	var runCalls int
	runClaudeUpdate = func() (string, string, error) { runCalls++; return "2.1.0", "2.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate = origRun })

	// installed が cached latest より古い → 実行される
	writeVersionCache(t, claudeVersionCacheFile, "2.2.0", time.Now())
	fetchInstalledClaudeVersion = func(context.Context) string { return "2.1.0" }
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.handleKey("C")
	deliverUpdateMsg(m, cmd)
	if runCalls != 1 {
		t.Fatalf("installed が古いのに update が実行されない (calls=%d)", runCalls)
	}

	// キャッシュが stale (TTL 切れ) → latest 不明扱いで実行される (オフライン等で塞がない)
	writeVersionCache(t, claudeVersionCacheFile, "2.2.0", time.Now().Add(-2*claudeVersionTTL))
	fetchInstalledClaudeVersion = func(context.Context) string { return "2.2.0" }
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd2 := m2.handleKey("C")
	deliverUpdateMsg(m2, cmd2)
	if runCalls != 2 {
		t.Fatalf("stale キャッシュなのに update が実行されない (calls=%d)", runCalls)
	}

	// installed 取得失敗 ("") → 実行される
	writeVersionCache(t, claudeVersionCacheFile, "2.2.0", time.Now())
	fetchInstalledClaudeVersion = func(context.Context) string { return "" }
	m3 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd3 := m3.handleKey("C")
	deliverUpdateMsg(m3, cmd3)
	if runCalls != 3 {
		t.Fatalf("installed 不明なのに update が実行されない (calls=%d)", runCalls)
	}
}

// 比較不能なバージョン形式 (pre-release 等) が latest キャッシュに入っても「最新扱い」で
// update を塞がない (敵対レビュー指摘 2026-08-12: versionLess の比較不能=false を否定すると
// 「比較できない = skip」に反転する罠の回帰テスト)。
func TestBrowseUpdateRunsWhenVersionsIncomparable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeVersionCache(t, claudeVersionCacheFile, "2.2.0-beta.1", time.Now())
	origFetch := fetchInstalledClaudeVersion
	fetchInstalledClaudeVersion = func(context.Context) string { return "2.1.0" }
	t.Cleanup(func() { fetchInstalledClaudeVersion = origFetch })
	origRun := runClaudeUpdate
	var runCalls int
	runClaudeUpdate = func() (string, string, error) { runCalls++; return "2.1.0", "2.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate = origRun })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.handleKey("C")
	deliverUpdateMsg(m, cmd)
	if runCalls != 1 {
		t.Fatalf("比較不能な latest 形式で update が塞がれた (calls=%d)", runCalls)
	}
}

// editorCommand は $VISUAL → $EDITOR → nvim の順に解決し、値の引数部分を保つ。
// issue を開く経路 (issuesView.editCmd) だけがこれを使う。
func TestEditorCommand(t *testing.T) {
	for _, tt := range []struct {
		name         string
		visual       string
		editor       string
		wantPath     string   // 実行ファイル (末尾一致で見る: 絶対パス指定も許す)
		wantArgsTail []string // path を含む引数列 (Args[1:])
	}{
		{name: "どちらも未設定なら nvim", wantPath: "nvim", wantArgsTail: []string{"/tmp/a.md"}},
		{name: "EDITOR を使う", editor: "vim", wantPath: "vim", wantArgsTail: []string{"/tmp/a.md"}},
		{
			name: "VISUAL が EDITOR より優先", visual: "hx", editor: "vim",
			wantPath: "hx", wantArgsTail: []string{"/tmp/a.md"},
		},
		{
			name: "引数つきの指定を語分割して保つ", editor: "code -w",
			wantPath: "code", wantArgsTail: []string{"-w", "/tmp/a.md"},
		},
		{
			name: "空白だけの指定は未設定と同じ", editor: "   ",
			wantPath: "nvim", wantArgsTail: []string{"/tmp/a.md"},
		},
		{
			name: "VISUAL が空白なら EDITOR に落ちる", visual: " ", editor: "vim",
			wantPath: "vim", wantArgsTail: []string{"/tmp/a.md"},
		},
		{
			// ⚠️ PATH で解決できない絶対パスにする。実在するエディタの絶対パスを使うと
			// 「指定を捨てて PATH 上の同名を起動する」実装でも Path が偶然一致して assert が
			// 空振りする (実測: /opt/homebrew/bin/nvim だと変異が通った)。
			name: "絶対パス + 引数", editor: "/opt/glogx-test/bin/myeditor -p",
			wantPath: "/opt/glogx-test/bin/myeditor", wantArgsTail: []string{"-p", "/tmp/a.md"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", tt.editor)
			cmd := editorCommand("/tmp/a.md")
			// exec.Command は PATH 解決するので Path は絶対パスになりうる。⚠️ HasSuffix で見ると
			// want="vim" が ".../bin/nvim" にも一致して別物を通すので、Base の完全一致で見る。
			if got, want := filepath.Base(cmd.Path), filepath.Base(tt.wantPath); got != want {
				t.Errorf("実行ファイル名が違う: got=%q want=%q (Path=%q)", got, want, cmd.Path)
			}
			// ⚠️ 絶対パス指定は Path そのものを見る。Base 一致だけだと「指定を捨てて PATH 上の
			// 同名を起動する」実装 (= ユーザーが指定した実体が無視される) を通してしまう。
			if filepath.IsAbs(tt.wantPath) && cmd.Path != tt.wantPath {
				t.Errorf("絶対パス指定の実体が起動されない: Path=%q want=%q", cmd.Path, tt.wantPath)
			}
			if got := cmd.Args[1:]; !slices.Equal(got, tt.wantArgsTail) {
				t.Errorf("引数が違う: got=%v want=%v", got, tt.wantArgsTail)
			}
			if cmd.Args[0] != tt.wantPath {
				t.Errorf("Args[0] が指定どおりでない: got=%q want=%q", cmd.Args[0], tt.wantPath)
			}
		})
	}
}
