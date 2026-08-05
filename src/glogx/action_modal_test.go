package main

import "testing"

// ownsKeys は「handleKey が consumed=true を返す条件」と一致していなければならない。
//
// ⚠️ この一致がこのテストの主張そのもの。他のモーダル (再起動ダイアログ) は「自分を出してよいか」を
// ownsKeys で判断するので、ずれると「最前面に出ているのにキーが別のモーダルへ行く」状態が生まれる。
// 実際に起きた事故: 再起動ダイアログが running() だけを見ていたため push 確認 (y/N) 中に最前面へ
// 重なり、画面の「その他のキー: 後で」に従って押した y が push を実行した。
//
// 状態を足したとき (新しい確認・新しい実行中) にここが落ちるので、片方だけ更新できない。
func TestActionModalOwnsKeysMatchesHandleKey(t *testing.T) {
	states := []struct {
		name  string
		apply func(*actionModal)
	}{
		{"何もしていない", func(a *actionModal) {}},
		{"push 確認", func(a *actionModal) { a.pushConfirm = true }},
		{"pull 確認", func(a *actionModal) { a.pullConfirm = true }},
		{"rerun 確認", func(a *actionModal) { a.rerunConfirm = true }},
		{"push 実行中", func(a *actionModal) { a.pushing = true }},
		{"pull 実行中", func(a *actionModal) { a.pulling = true }},
		{"rerun 実行中", func(a *actionModal) { a.rerunning = true }},
		{"update 実行中", func(a *actionModal) { a.updating = true }},
	}
	// 確認は y/Enter で実行へ進み、それ以外はキャンセルする = キーによって遷移が変わるので
	// 代表を複数試す。どのキーでも「consumed か否か」は ownsKeys と一致すること。
	for _, key := range []string{"y", "enter", "n", "j", "r", "q"} {
		for _, st := range states {
			var a actionModal
			st.apply(&a)
			want := a.ownsKeys()
			consumed, _ := a.handleKey(key)
			if consumed != want {
				t.Errorf("%s / key=%q: handleKey の consumed=%v, ownsKeys()=%v (一致していない)",
					st.name, key, consumed, want)
			}
		}
	}
}
