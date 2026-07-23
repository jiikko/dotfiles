package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

	// C で確認を挟まず即実行 (updating=true & cmd 返却)
	_, cmd := m.handleKey("C")
	if cmd == nil || !m.actModal.updating {
		t.Fatalf("C で claude update が始まらない: cmd=%v updating=%v", cmd != nil, m.actModal.updating)
	}
	// 実行中は spinner モーダルが出て、終了できない旨も表示する
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "claude update") || !strings.Contains(v, "updating") ||
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
	// 変わった場合は結果ダイアログに "vX → vY" が出る
	if !strings.Contains(m.actModal.updateResult, "v2.1.216 → v2.2.0") {
		t.Fatalf("バージョン変化が結果ダイアログに出ない: %q", m.actModal.updateResult)
	}
	// ダイアログは何かキーで閉じる (キーは消費)
	if _, cmd := m.handleKey("j"); cmd != nil || m.actModal.updateResult != "" {
		t.Fatalf("結果ダイアログが任意キーで閉じない: cmd=%v result=%q", cmd != nil, m.actModal.updateResult)
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
	if !strings.Contains(m2.actModal.updateResult, "最新版") || !strings.Contains(m2.actModal.updateResult, "v2.2.0") {
		t.Fatalf("最新版が結果ダイアログに出ない: %q", m2.actModal.updateResult)
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
	if !m.actModal.updating {
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

	if m.actModal.updating {
		t.Fatal("更新失敗後も updating のまま (無限ブロックから復帰できない)")
	}
	if !strings.Contains(m.actModal.updateResult, "更新に失敗しました") || !strings.Contains(m.actModal.updateResult, "タイムアウト") {
		t.Fatalf("失敗理由が結果ダイアログに出ない: %q", m.actModal.updateResult)
	}
	// updating が解けたので、結果ダイアログは任意キーで閉じられる (無反応から復帰済み)。
	m.handleKey("q")
	if m.actModal.updateResult != "" || m.done {
		t.Fatalf("q で結果ダイアログが閉じない: result=%q done=%v", m.actModal.updateResult, m.done)
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
	if v := stripANSI(m.View()); !strings.Contains(v, "git push") || !strings.Contains(v, "push します") {
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
	if v := stripANSI(m.View()); !strings.Contains(v, "pull --rebase") {
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
	if !m.actModal.pushConfirm {
		t.Fatal("b で push 確認に入らない")
	}
	m.handleKey("ctrl+t")
	if m.actModal.pushConfirm || m.prefixPending {
		t.Fatalf("確認モーダル中の C-t がキャンセルにならない: confirm=%v pending=%v", m.actModal.pushConfirm, m.prefixPending)
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
	if m.actModal.pushConfirm {
		t.Fatal("未 push なしで push 確認に入った")
	}
	// hint 行でなく警告モーダルが出る (ユーザー要望)
	m.width, m.height = 80, 20
	if v := stripANSI(m.View()); !strings.Contains(v, "未 push のコミットはありません") {
		t.Fatal("未 push なしの警告モーダルが出ない")
	}
	// 何かキーで閉じ、そのキーは消費される (カーソルが動かない)
	m.handleKey("j")
	if m.actModal.pushWarn != "" {
		t.Fatal("キーで警告モーダルが閉じない")
	}
	if m.cursor != 0 {
		t.Fatal("モーダルを閉じたキーが消費されずカーソルが動いた")
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
	if v := stripANSI(m.View()); !strings.Contains(v, "CI 再実行") || !strings.Contains(v, "lint") {
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
	if m.actModal.rerunConfirm || !strings.Contains(m.notice, "GitHub Actions") {
		t.Fatalf("StatusContext job で確認に入った / notice が出ない: %q", m.notice)
	}
	// 失敗以外の job は再実行不可
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.statuses = statusesFor(m2, StateSuccess)
	withFailedJob(m2, 0, 7, StateSuccess)
	m2.openPanel()
	m2.handleKey("j")
	m2.handleKey("r")
	if m2.actModal.rerunConfirm || !strings.Contains(m2.notice, "失敗") {
		t.Fatalf("成功 job で確認に入った / notice が出ない: %q", m2.notice)
	}
	// タイトル行フォーカス (job 未選択) では何も起きない
	m2.notice = ""
	m2.panelCursor = -1
	m2.handleKey("r")
	if m2.actModal.rerunConfirm || m2.notice != "" {
		t.Fatal("タイトル行フォーカスで r が反応した")
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
