package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// modalStates は actionModal の「モーダルとしての状態」を 1 つずつ立てたもの。
// フィールドの網羅は TestActionModalStateListCoversAllFlags が守る。
var modalStates = []struct {
	name  string
	field string // actionModal の bool フィールド名
	apply func(*actionModal)
}{
	{"何もしていない", "", func(a *actionModal) {}},
	{"push 確認", "pushConfirm", func(a *actionModal) { a.pushConfirm = true }},
	{"pull 確認", "pullConfirm", func(a *actionModal) { a.pullConfirm = true }},
	{"rerun 確認", "rerunConfirm", func(a *actionModal) { a.rerunConfirm = true }},
	{"push 実行中", "pushing", func(a *actionModal) { a.pushing = true }},
	{"pull 実行中", "pulling", func(a *actionModal) { a.pulling = true }},
	{"rerun 実行中", "rerunning", func(a *actionModal) { a.rerunning = true }},
	{"update 実行中", "updating", func(a *actionModal) { a.beginUpdate("claude") }},
	{"update 並走中", "updating", func(a *actionModal) { a.beginUpdate("claude"); a.beginUpdate("codex") }},
	// forceQuitArmed はモーダルの表示にもキーの消費にも関与しない (Ctrl-C の 2 段階ガード)。
	// active() が false のままであることを確かめるために一覧へ入れる。
	{"強制終了アーム済み", "forceQuitArmed", func(a *actionModal) { a.forceQuitArmed = true }},
}

// active() は「描かれる条件」であると同時に「handleKey が consumed を返す条件」でなければ
// ならない = 見えているモーダルがキーを持つ。
//
// 🚨 この一致がこのテストの主張そのもの。他のモーダル (再起動ダイアログ) は「自分を出してよいか」を
// active() で判断するので、ずれると「最前面に出ているのにキーが別のモーダルへ行く」状態が生まれる。
// 実際に起きた事故: 再起動ダイアログが running() だけを見ていたため push 確認 (y/N) 中に最前面へ
// 重なり、画面の「その他のキー: 後で」に従って押した y が push を実行した。
func TestActionModalActiveMatchesHandleKey(t *testing.T) {
	// 確認は y/Enter で実行へ進み、それ以外はキャンセルする = キーによって遷移が変わるので
	// 代表を複数試す。どのキーでも「consumed か否か」は active() と一致すること。
	// 🚨 C / X を必ず含めること。これらは update 走行中に「もう片方の CLI を始める」ため
	// 例外的に扱われるが、**消費はする** (素通しにすると viewer のキー語彙へ漏れて
	// 破棄確認を誤爆させた実測がある)。同値が崩れていないことをこのキーで確かめる。
	for _, key := range []string{"y", "enter", "n", "j", "r", "q", "C", "X"} {
		for _, st := range modalStates {
			var a actionModal
			st.apply(&a)
			want := a.active()
			consumed, _ := a.handleKey(key)
			if consumed != want {
				t.Errorf("%s / key=%q: handleKey の consumed=%v, active()=%v (一致していない)",
					st.name, key, consumed, want)
			}
		}
	}
}

// waitPullCleanup は走行中の pull (conflict 時は rebase --abort の後始末を含む) が終わるまで
// 戻らないこと。これが破れると、quit 直後の os.Exit が abort を走り切る前にプロセスごと消し、
// repo に rebase-merge が残る (bubbletea は tea.Cmd の goroutine を待たない)。
func TestWaitPullCleanupOutlivesQuit(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	orig := runGitPullRebase
	runGitPullRebase = func(context.Context) error { close(started); <-release; return nil }
	t.Cleanup(func() { runGitPullRebase = orig })

	var a actionModal
	a.askPull()
	_, cmd := a.handleKey("y")
	if cmd == nil {
		t.Fatal("pull 確認の y が実行 Cmd を返していない")
	}
	go cmd()
	<-started // latch (pullCleanup.Add) は closure 側なので、開始を見届けてから Wait する

	waited := make(chan struct{})
	go func() { waitPullCleanup(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("pull (後始末) が走行中なのに waitPullCleanup が戻った")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("pull 完了後も waitPullCleanup が戻らない")
	}
}

// modalStates が actionModal の bool フィールドを網羅していることを型から検査する。
//
// 🚨 これが無いと上の対応テストは「私が思い出した状態」しか見ない。新しい確認/実行中フラグを
// 足した人が active() の更新を忘れても気づけないので、フィールドを足したらここで落として
// 「対応を確認して一覧に足す」ことを強制する (テストが守れる範囲を実際に広げるための一段)。
func TestActionModalStateListCoversAllFlags(t *testing.T) {
	listed := make(map[string]bool, len(modalStates))
	for _, st := range modalStates {
		if st.field != "" {
			listed[st.field] = true
		}
	}
	// 🚨 「状態フィールドの型」で絞り込まないこと (bool 限定や、名前のハードコードを含む)。
	// map / slice / 独自型の状態は静かに網羅対象から落ちる (並列化で updating を map へ変えた
	// 2026-08-21 に実際に落ち、red team が「非 bool の状態を足すと active() との不一致が
	// 検出されない」ことを実測した)。除外リスト方式にして、**新設フィールドは必ず落ちる**
	// ようにする (落ちたら modalStates へ足すか、この除外リストへ理由付きで加える)。
	notState := map[string]bool{
		"cancel":         true,  // 走行中の cancel を握るだけ (モーダルの状態ではない)
		"rerunAction":    true,  // 確認 y で実行する Cmd の注入先
		"rerunJobName":   true,  // 確認モーダルの文言用
		"forceQuitArmed": false, // 一覧に入っている (active() が false のままであることの確認)
	}
	typ := reflect.TypeFor[actionModal]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if notState[f.Name] {
			continue
		}
		if !listed[f.Name] {
			t.Errorf("actionModal.%s が modalStates に無い。active() が「描かれる ⇔ キーを消費する」を"+
				"保てているか確認して、一覧へ足すこと", f.Name)
		}
	}
}

// claude と codex の自己更新は並走できること (ユーザー要望 2026-08-21、issue 074)。
// 🚨 単数の bool + 対象名に戻すと、片方の実行中に running() がもう片方の C/X を飲んで
// 直列化する。この test 群がその退行を捕まえる。
func TestActionModalParallelUpdates(t *testing.T) {
	t.Run("別の CLI は並走できる", func(t *testing.T) {
		var a actionModal
		if !a.beginUpdate("claude") {
			t.Fatal("1 本目の claude update が始まらない")
		}
		if !a.beginUpdate("codex") {
			t.Fatal("claude 実行中に codex update を始められない (直列化している)")
		}
		if got := a.updatingTargets(); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
			t.Errorf("走行中の一覧が %v (claude と codex の両方であるべき)", got)
		}
	})

	t.Run("同じ CLI の二重起動は弾く", func(t *testing.T) {
		var a actionModal
		a.beginUpdate("claude")
		if a.beginUpdate("claude") {
			t.Error("同じ CLI の update を二重に開始できた (自己更新が競合する)")
		}
		if got := len(a.updating); got != 1 {
			t.Errorf("走行中が %d 本 (1 本であるべき)", got)
		}
	})

	t.Run("片方が終わってもモーダルは残る", func(t *testing.T) {
		var a actionModal
		a.beginUpdate("claude")
		a.beginUpdate("codex")
		a.finishUpdate("claude")
		if !a.active() || !a.running() {
			t.Fatal("片方の完了でモーダルが閉じた (もう片方が走っているのに終了ガードが解けた)")
		}
		if a.isUpdating("claude") {
			t.Error("完了した claude が走行中に残っている")
		}
		if !a.isUpdating("codex") {
			t.Error("走行中の codex が消えた")
		}
		a.finishUpdate("codex")
		if a.active() || a.running() {
			t.Error("両方終わってもモーダルが閉じない")
		}
	})

	t.Run("target 空の完了は fail-safe で全部降ろす", func(t *testing.T) {
		// 実運用では runUpdate が必ず target を入れるが、降りないと running() が真のままで
		// Ctrl-C の終了ガードが解けず閉じられなくなる。閉じられない方向の失敗は避ける。
		var a actionModal
		a.beginUpdate("claude")
		a.beginUpdate("codex")
		a.finishUpdate("")
		if a.running() {
			t.Error("target 空の完了で走行中が残った (モーダルを閉じられなくなる)")
		}
	})

	t.Run("runUpdate は二重起動で nil を返す", func(t *testing.T) {
		var a actionModal
		if cmd := a.runUpdate("claude"); cmd == nil {
			t.Fatal("1 本目の runUpdate が nil (実行されない)")
		}
		if cmd := a.runUpdate("claude"); cmd != nil {
			t.Error("同じ CLI の runUpdate が 2 本目も Cmd を返した (自己更新が競合する)")
		}
	})
}

// update 実行中のキー配分。C / X は「もう片方の CLI の更新開始」として actionModal が
// **消費する** (Cmd を返す)。browseModel へ素通しすると全画面 viewer のキー語彙に漏れ、
// status viewer の X = 破棄確認を誤爆させる (red team 2026-08-21 が git restore の着弾を実測)。
func TestActionModalUpdateKeyOwnership(t *testing.T) {
	t.Run("別 CLI のキーは消費して更新を始める", func(t *testing.T) {
		for _, tc := range []struct{ running, key, want string }{
			{"claude", "X", "codex"},
			{"codex", "C", "claude"},
		} {
			var a actionModal
			a.beginUpdate(tc.running)
			consumed, cmd := a.handleKey(tc.key)
			if !consumed {
				t.Errorf("%s 走行中の %q を素通しした (viewer のキー語彙へ漏れる)", tc.running, tc.key)
			}
			if cmd == nil {
				t.Errorf("%s 走行中の %q が %s の更新を始めない (並列化が効いていない)",
					tc.running, tc.key, tc.want)
			}
		}
	})

	t.Run("同じ CLI のキーは消費して何もしない", func(t *testing.T) {
		// 判定 Cmd を走らせると、その早期リターンが走行中の update を降ろして
		// 終了ガードが解ける (自己更新の孤児化 / 二重起動)。
		for _, tc := range []struct{ running, key string }{{"claude", "C"}, {"codex", "X"}} {
			var a actionModal
			a.beginUpdate(tc.running)
			consumed, cmd := a.handleKey(tc.key)
			if !consumed || cmd != nil {
				t.Errorf("%s 走行中の %q: consumed=%v cmd!=nil=%v (消費して何もしないべき)",
					tc.running, tc.key, consumed, cmd != nil)
			}
		}
	})

	t.Run("update 中の他のキーは飲む", func(t *testing.T) {
		for _, key := range []string{"y", "enter", "n", "j", "q", "b", "u", "s", "i"} {
			var a actionModal
			a.beginUpdate("claude")
			if consumed, _ := a.handleKey(key); !consumed {
				t.Errorf("update 中の %q が素通りした (実行中は飲むべき)", key)
			}
		}
	})

	t.Run("git 実行中は C/X も飲んで更新を始めない", func(t *testing.T) {
		for _, st := range []struct {
			name  string
			apply func(*actionModal)
		}{
			{"push 実行中", func(a *actionModal) { a.pushing = true }},
			{"pull 実行中", func(a *actionModal) { a.pulling = true }},
			{"rerun 実行中", func(a *actionModal) { a.rerunning = true }},
		} {
			for _, key := range []string{"C", "X"} {
				var a actionModal
				st.apply(&a)
				a.beginUpdate("claude")
				consumed, cmd := a.handleKey(key)
				if !consumed || cmd != nil {
					t.Errorf("%s + update 中の %q: consumed=%v cmd!=nil=%v (git 操作中は重ねない)",
						st.name, key, consumed, cmd != nil)
				}
			}
		}
	})
}

// 並走モーダルの表示。走行中の CLI が行で分かること / 終了できない理由が画面にあること。
// 🚨 この 2 つは「並走中に片方の行しか出ない」「並走中だけ案内が消える」変異で緑になっていた
// (red team 2026-08-21)。Ctrl-C はブロックされ続けるのに理由が画面から消えるのが最悪の形。
func TestActionModalUpdateBoxLines(t *testing.T) {
	t.Run("単独走行", func(t *testing.T) {
		var a actionModal
		a.beginUpdate("codex")
		out := strings.Join(a.boxLines(80, false, "⠋", 0), "\n")
		for _, want := range []string{"codex update", "updating...", "完了まで終了できません"} {
			if !strings.Contains(out, want) {
				t.Errorf("単独走行の箱に %q が無い:\n%s", want, out)
			}
		}
		if strings.Contains(out, "claude") {
			t.Errorf("走っていない claude が出ている:\n%s", out)
		}
	})

	t.Run("並走", func(t *testing.T) {
		var a actionModal
		a.beginUpdate("claude")
		a.beginUpdate("codex")
		out := strings.Join(a.boxLines(80, false, "⠋", 0), "\n")
		for _, want := range []string{"CLI update", "claude updating...", "codex updating...", "完了まで終了できません"} {
			if !strings.Contains(out, want) {
				t.Errorf("並走の箱に %q が無い:\n%s", want, out)
			}
		}
	})
}

// 走行中の一覧は決定論的な順で返ること。🚨 2 要素だけで 1 回試す形にしないこと:
// sort を外しても map の反復順で 8/10 は昇順になり、退行検知として当てにならない
// (red team 2026-08-21 が 10 回実行で PASS 8 / FAIL 2 を実測)。
func TestUpdatingTargetsIsDeterministic(t *testing.T) {
	var a actionModal
	for _, tgt := range []string{"codex", "claude", "gemini", "aider"} {
		a.beginUpdate(tgt)
	}
	want := []string{"aider", "claude", "codex", "gemini"}
	for i := range 60 {
		if got := a.updatingTargets(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%d 回目で順が変わった: %v (期待 %v。モーダルの行が毎フレーム入れ替わる)", i, got, want)
		}
	}
}
