package main

import "testing"

// C / X (claude / codex update) が git log 一覧以外の画面からも始められることの配線テスト。
// 以前は各 overlay の分岐が先にキーを飲み、一覧へ戻らないと update できなかった
// (ユーザー要望 2026-09-02)。update の中身 (判定 → 実行 → 結果) は tui_actions_test.go /
// codex_version_test.go が見るので、ここでは「キーが actionModal まで届き、実行関数まで到達するか」と
// 「overlay の語彙が先に立つ場面では届かないか」だけを pin する。
//
// ⚠️ 届いた証拠は stub した runClaudeUpdate / runCodexUpdate の呼び出し回数で取る。handleKey の
// 戻り値が非 nil かでは判定しない (overlay が飲んでも maybeTick が非 nil を返す経路があり、
// updateBeginMsg を直接 Update へ流す形も「届かなくても updating が立つ」ので証拠にならない。
// 敵対レビュー 2026-09-02 が変異で実証)。

// stubUpdates は claude / codex の update 実行を差し替え、呼び出し回数を返す。テスト環境は
// version cache が無いので、キーが届けば判定 → updateBeginMsg → 実行 (stub) まで進む。
func stubUpdates(t *testing.T) (claude, codex *int) {
	t.Helper()
	var c, x int
	origC, origX := runClaudeUpdate, runCodexUpdate
	runClaudeUpdate = func() (string, string, error) { c++; return "2.2.0", "2.3.0", nil }
	runCodexUpdate = func() (string, string, error) { x++; return "0.1.0", "0.2.0", nil }
	t.Cleanup(func() { runClaudeUpdate, runCodexUpdate = origC, origX })
	return &c, &x
}

// press は key を押して update の Cmd 連鎖を最後まで配送する (判定 → 開始 → 実行 → 結果)。
func press(m *browseModel, key string) {
	_, cmd := m.handleKey(key)
	if cmd != nil {
		deliverUpdateMsg(m, cmd)
	}
	releaseKey(m)
}

func TestUpdateKeysReachableFromOverlays(t *testing.T) {
	openByKey := func(key string, visible func(m *browseModel) bool) func(t *testing.T, m *browseModel) {
		return func(t *testing.T, m *browseModel) {
			m.handleKey(key)
			releaseKey(m)
			if !visible(m) {
				t.Fatalf("%q で overlay が開かない", key)
			}
		}
	}
	cases := []struct {
		name    string
		open    func(t *testing.T, m *browseModel)
		visible func(m *browseModel) bool
		// codexIsDiscard: その画面では X が「変更を捨てる」で、codex update に取らない
		codexIsDiscard bool
	}{
		{"issues", openByKey("i", func(m *browseModel) bool { return m.issuesOv.visible() }),
			func(m *browseModel) bool { return m.issuesOv.visible() }, false},
		// status は行が無いと X (破棄) が何もしないので、1 行持った viewer を直接差し込む
		// (TestUpdateKeysDoNotLeakIntoStatusViewer と同じ準備)
		{"status", func(t *testing.T, m *browseModel) { m.statusOv = *newTestStatusView(t, statusRec(" M a.go")) },
			func(m *browseModel) bool { return m.statusOv.visible() }, true},
		{"ratelimit", openByKey("R", func(m *browseModel) bool { return m.rlDash.visible() }),
			func(m *browseModel) bool { return m.rlDash.visible() }, false},
		{"doctor", openByKey("D", func(m *browseModel) bool { return m.doctorOv.visible() }),
			func(m *browseModel) bool { return m.doctorOv.visible() }, false},
		// diff / PR status のポップアップも同じ (以前は未知キーとして飲んでいた)
		{"diff", func(t *testing.T, m *browseModel) { m.diffOv.open(m.commits[0].SHA) },
			func(m *browseModel) bool { return m.diffOv.visible() }, false},
		{"pr-status", func(t *testing.T, m *browseModel) { m.prStatusOv.open(m.commits[0].SHA) },
			func(m *browseModel) bool { return m.prStatusOv.visible() }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			claude, codex := stubUpdates(t)
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			c.open(t, m)
			if !c.visible(m) {
				t.Fatalf("前提: %s が開いていない", c.name)
			}
			press(m, "C")
			if *claude != 1 {
				t.Fatalf("%s の上で C を押しても claude update が実行されない (calls=%d)", c.name, *claude)
			}
			if !c.visible(m) {
				t.Fatalf("C で %s が閉じた", c.name)
			}
			press(m, "X")
			if c.codexIsDiscard {
				if *codex != 0 || !m.statusOv.discarding {
					t.Fatalf("status viewer の X は破棄確認のはず (codexCalls=%d discarding=%v)", *codex, m.statusOv.discarding)
				}
				return
			}
			if *codex != 1 {
				t.Fatalf("%s の上で X を押しても codex update が実行されない (calls=%d)", c.name, *codex)
			}
			if !c.visible(m) {
				t.Fatalf("X で %s が閉じた", c.name)
			}
		})
	}
}

// overlay の入力モード中は C / X をそのモードへ渡す (絞り込みに C を打ちたい場面で update が
// 走ると、入力を奪った上に外部コマンドまで起動する)。
func TestUpdateKeysYieldToOverlayInputModes(t *testing.T) {
	claude, codex := stubUpdates(t)

	// issues viewer の番号絞り込み (/) 中
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.handleKey("i")
	releaseKey(m)
	m.handleKey("/")
	releaseKey(m)
	if !m.issuesOv.visible() || !m.issuesOv.ownsKeys() {
		t.Fatal("前提: / で issues viewer が入力モードに入っていない")
	}
	press(m, "C")
	press(m, "X")
	if *claude != 0 || *codex != 0 {
		t.Fatalf("絞り込み中の C / X が update を始めた (claude=%d codex=%d)", *claude, *codex)
	}

	// status viewer の破棄確認 (X → y/N) 中の C は確認への回答 (= キャンセル) で、update ではない
	m2 := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m2.statusOv = *newTestStatusView(t, statusRec(" M a.go"))
	m2.handleKey("X")
	releaseKey(m2)
	if !m2.statusOv.ownsKeys() {
		t.Fatal("前提: X で status viewer が破棄確認 (入力モード) に入っていない")
	}
	press(m2, "C")
	if *claude != 0 {
		t.Fatalf("破棄確認中の C が update を始めた (claude=%d)", *claude)
	}
	if m2.statusOv.ownsKeys() {
		t.Fatal("破棄確認中の C が確認を閉じていない (y/N 以外はキャンセルの語彙)")
	}
}
