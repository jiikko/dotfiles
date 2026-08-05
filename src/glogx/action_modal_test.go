package main

import (
	"context"
	"reflect"
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
	{"update 実行中", "updating", func(a *actionModal) { a.updating = true }},
	// forceQuitArmed はモーダルの表示にもキーの消費にも関与しない (Ctrl-C の 2 段階ガード)。
	// active() が false のままであることを確かめるために一覧へ入れる。
	{"強制終了アーム済み", "forceQuitArmed", func(a *actionModal) { a.forceQuitArmed = true }},
}

// active() は「描かれる条件」であると同時に「handleKey が consumed を返す条件」でなければ
// ならない = 見えているモーダルがキーを持つ。
//
// ⚠️ この一致がこのテストの主張そのもの。他のモーダル (再起動ダイアログ) は「自分を出してよいか」を
// active() で判断するので、ずれると「最前面に出ているのにキーが別のモーダルへ行く」状態が生まれる。
// 実際に起きた事故: 再起動ダイアログが running() だけを見ていたため push 確認 (y/N) 中に最前面へ
// 重なり、画面の「その他のキー: 後で」に従って押した y が push を実行した。
func TestActionModalActiveMatchesHandleKey(t *testing.T) {
	// 確認は y/Enter で実行へ進み、それ以外はキャンセルする = キーによって遷移が変わるので
	// 代表を複数試す。どのキーでも「consumed か否か」は active() と一致すること。
	for _, key := range []string{"y", "enter", "n", "j", "r", "q"} {
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
// ⚠️ これが無いと上の対応テストは「私が思い出した状態」しか見ない。新しい確認/実行中フラグを
// 足した人が active() の更新を忘れても気づけないので、フィールドを足したらここで落として
// 「対応を確認して一覧に足す」ことを強制する (テストが守れる範囲を実際に広げるための一段)。
func TestActionModalStateListCoversAllFlags(t *testing.T) {
	listed := make(map[string]bool, len(modalStates))
	for _, st := range modalStates {
		if st.field != "" {
			listed[st.field] = true
		}
	}
	typ := reflect.TypeFor[actionModal]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Bool {
			continue // cancel / rerunAction / rerunJobName は状態フラグではない
		}
		if !listed[f.Name] {
			t.Errorf("actionModal.%s が modalStates に無い。active() が「描かれる ⇔ キーを消費する」を"+
				"保てているか確認して、一覧へ足すこと", f.Name)
		}
	}
}
