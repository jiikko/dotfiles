package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// macismCurrentWarn は macism (引数なし) の出力/エラーを (prev, warn) に分類する純関数。
func TestMacismCurrentWarn(t *testing.T) {
	// 正常: 現在ソース ID が取れれば prev にセット、warn なし
	prev, warn := macismCurrentWarn([]byte("com.apple.inputmethod.Kotoeri.Japanese\n"), nil)
	if prev != "com.apple.inputmethod.Kotoeri.Japanese" || warn != "" {
		t.Fatalf("正常系: prev=%q warn=%q", prev, warn)
	}

	// #2: exit 0 だが出力が空 → 「返さなかった」警告 (silent no-op にしない)
	prev, warn = macismCurrentWarn([]byte("  \n"), nil)
	if prev != "" || warn == "" || !strings.Contains(warn, "macism") {
		t.Fatalf("空出力で警告が出ない: prev=%q warn=%q", prev, warn)
	}

	// #3: 非ゼロ終了 + stderr → stderr の中身を詳細に含める ("exit status N" より有用)
	ee := &exec.ExitError{ProcessState: nil, Stderr: []byte("macism: no such input source\n")}
	prev, warn = macismCurrentWarn(nil, ee)
	if prev != "" || !strings.Contains(warn, "no such input source") {
		t.Fatalf("stderr が詳細に含まれない: warn=%q", warn)
	}

	// stderr が無いエラーは err.Error() (exit status 等) にフォールバック
	prev, warn = macismCurrentWarn(nil, errors.New("exec: \"macism\": executable file not found"))
	if prev != "" || warn == "" || !strings.Contains(warn, "取得に失敗") {
		t.Fatalf("stderr 無しエラーで警告が出ない: warn=%q", warn)
	}
}
