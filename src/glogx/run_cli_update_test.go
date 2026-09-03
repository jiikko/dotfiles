package main

import (
	"context"
	"strings"
	"testing"
)

// runCLIUpdate は成功時にも CLI 出力の末尾行を返す。🚨 ここが空だと呼び出し側は
// before == after の理由を語れず、「CLI が更新をサボった」を「最新版です」と誤訳する経路が
// 復活する (実測 2026-09-03 の codex の症状)。run*Update を差し替えるテストは updateMsg より
// 手前を通らないので、この配線はここでだけ検証できる。
//
// name に "echo" を渡すと `echo update` が走り、stdout に "update" が出て exit 0 する
// (= 更新しなかった CLI と同じ形の成功)。実 CLI を起動せずに成功経路を通せる。
func TestRunCLIUpdateReturnsLastOutputLineOnSuccess(t *testing.T) {
	versions := []string{"1.0.0", "1.0.0"}
	i := 0
	fetch := func(context.Context) string {
		v := versions[i]
		i++
		return v
	}

	before, after, note, err := runCLIUpdate("echo", fetch)
	if err != nil {
		t.Fatalf("echo update が失敗した: %v", err)
	}
	if before != "1.0.0" || after != "1.0.0" {
		t.Fatalf("before/after = %q/%q, want 1.0.0/1.0.0", before, after)
	}
	if !strings.Contains(note, "update") {
		t.Fatalf("成功時の note に CLI 出力の末尾行が入っていない: note=%q", note)
	}
}
