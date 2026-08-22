package main

import "testing"

// 未実装のサブコマンドが「成功」や「空いている (exit 0/3)」に倒れないことを固定する。
// ここが緩むと、骨組みのまま呼んだスクリプトが「ロックを取れた」と誤解して走り出す。
func TestRunUnimplementedCommandsNeverLookLikeSuccessOrBusy(t *testing.T) {
	for name := range commands {
		got := run([]string{name, "/tmp/does-not-matter"})
		if got == exitOK || got == exitBusy {
			t.Errorf("%s: 未実装なのに exit %d を返した (成功・busy に倒してはいけない)", name, got)
		}
		if got != exitWithInvalid {
			t.Errorf("%s: exit %d (期待 %d)", name, got, exitWithInvalid)
		}
	}
}

func TestRunUnknownCommandIsError(t *testing.T) {
	if got := run([]string{"acquir"}); got != exitError {
		t.Errorf("未知のサブコマンド: exit %d (期待 %d)", got, exitError)
	}
	if got := run(nil); got != exitError {
		t.Errorf("引数なし: exit %d (期待 %d)", got, exitError)
	}
}

func TestHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		if got := run([]string{arg}); got != exitOK {
			t.Errorf("%s: exit %d (期待 %d)", arg, got, exitOK)
		}
	}
}

// 終了コードの番号が互いに衝突していないこと。with 系は子プロセスの終了コードと
// 区別できる必要があるため、120 より上に置く (issue 091 の表)。
func TestExitCodesDoNotCollide(t *testing.T) {
	seen := map[int]string{}
	for name, code := range map[string]int{
		"exitOK": exitOK, "exitError": exitError, "exitBusy": exitBusy,
		"exitNotOwner": exitNotOwner, "exitWithBusy": exitWithBusy,
		"exitWithLost": exitWithLost, "exitWithInvalid": exitWithInvalid,
	} {
		if prev, dup := seen[code]; dup {
			t.Errorf("exit code %d が %s と %s で重複している", code, prev, name)
		}
		seen[code] = name
	}
	for name, code := range map[string]int{
		"exitWithBusy": exitWithBusy, "exitWithLost": exitWithLost, "exitWithInvalid": exitWithInvalid,
	} {
		if code <= 120 {
			t.Errorf("%s = %d は子プロセスの終了コードと衝突しうる (> 120 にすること)", name, code)
		}
	}
}
