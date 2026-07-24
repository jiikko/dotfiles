package main

import (
	"strings"
	"testing"
)

// dropEmojiVS16 は VS16 (U+FE0F) を除去し、絵文字を text presentation (幅 1) へ倒す
// (Terminal.app + tmux の幅食い違いによる再描画ガタつき対策。issue: ⚠️ の幅が毎秒揺れる)。
func TestDropEmojiVS16(t *testing.T) {
	const vs16 = "️"
	// ⚠️ (U+26A0 + VS16) → ⚠ (VS16 除去)
	got := dropEmojiVS16("危険 ⚠" + vs16 + " 注意")
	if want := "危険 ⚠ 注意"; got != want {
		t.Fatalf("VS16 が除去されない: got %q want %q", got, want)
	}
	if strings.ContainsRune(got, '️') {
		t.Fatal("出力に VS16 が残っている")
	}
	// VS16 が無い文字列はそのまま (高速パス)
	if s := "plain 危険 ⚠ text"; dropEmojiVS16(s) != s {
		t.Fatalf("VS16 無しで変化した: %q", dropEmojiVS16(s))
	}
	// VS15 (U+FE0E, text 強制) は残す
	const vs15 = "︎"
	if s := "⚠" + vs15; dropEmojiVS16(s) != s {
		t.Fatalf("VS15 まで除去した: %q", dropEmojiVS16(s))
	}
}
