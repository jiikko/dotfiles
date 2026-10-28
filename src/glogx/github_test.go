package main

import (
	"context"
	"errors"
	"glogx/internal/gitdata"
	"glogx/internal/gitdata/gitdatatest"
	"glogx/internal/term"
	"slices"
	"testing"
)

func TestSanitizeDetailLineDropsNonSGREscapes(t *testing.T) {
	// SGR 以外の ESC シーケンスは端末制御注入の経路 (CI 側の第三者が混入可能) なので
	// シーケンスごと落とす
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"CSI 画面消去", "a\x1b[2Jb", "ab"},
		{"CSI カーソル移動", "a\x1b[10;20Hb", "ab"},
		{"CSI プライベートモード", "a\x1b[?25lb", "ab"},
		{"OSC52 クリップボード (ST 終端)", "a\x1b]52;c;ZXZpbA==\x1b\\b", "ab"},
		{"OSC タイトル変更 (BEL 終端)", "a\x1b]0;evil\ab", "ab"},
		{"DCS", "a\x1bPq#0\x1b\\b", "ab"},
		{"2 文字エスケープ", "a\x1b7b", "ab"},
		{"末尾の裸 ESC", "ab\x1b", "ab"},
		// 🚨 途切れたシーケンスは「導入子 (ESC) だけ落として残りは本文として出す」。
		// 行末まで捨てる実装にしていたが、それだと `BUILD <ESC>]FAILED: 12 tests failed` の
		// ような入力で失敗の記録が黙って消える = 制御シーケンスを使わずに文字を隠せる。
		// ESC さえ落とせば残りはただの文字列で端末は
		// 何も解釈しないので、安全性を落とさずに情報を保てる。
		{"途切れた CSI", "ab\x1b[12", "ab[12"},
		{"途切れた OSC", "ab\x1b]0;title", "ab]0;title"},
		{"SGR は残る (混在)", "\x1b[31mred\x1b[0m \x1b[2Jx", "\x1b[31mred\x1b[0m x"},
	}
	for _, tt := range tests {
		if got := term.SanitizeDetailLine(tt.in); got != tt.want {
			t.Errorf("%s: sanitize(%q) = %q; want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestSanitizeDetailLine(t *testing.T) {
	if got := term.SanitizeDetailLine("plain text"); got != "plain text" {
		t.Errorf("素の行が変更された: %q", got)
	}
	// SGR (色/装飾) は残す (枠側の幅計算が対応済み)
	colored := "\x1b[36;1mmake test\x1b[0m"
	if got := term.SanitizeDetailLine(colored); got != colored {
		t.Errorf("SGR が落ちた: %q", got)
	}
	if got := term.SanitizeDetailLine("a\tb\rc\ufeffd"); got != "a    bcd" {
		t.Errorf("sanitize = %q; want %q", got, "a    bcd")
	}
}

// jobRerun は gh run rerun --job を正しい引数で叩き、失敗時は stderr 末尾行をエラーにする。
func TestJobRerun(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		gotName, gotArgs = name, args
		return nil, nil, nil
	}
	if err := jobRerun(context.Background(), run, gitdata.Repo{Owner: "o", Name: "r"}, 42); err != nil {
		t.Fatalf("jobRerun() = %v", err)
	}
	want := []string{"run", "rerun", "--job", "42", "-R", "o/r"}
	if gotName != "gh" || !slices.Equal(gotArgs, want) {
		t.Fatalf("gh 呼び出しが違う: %s %v; want gh %v", gotName, gotArgs, want)
	}
	// 失敗: stderr の末尾行がエラーメッセージになる
	err := jobRerun(context.Background(),
		gitdatatest.FakeRunner("", "some detail\nrun 123 cannot be rerun\n", errors.New("exit status 1")),
		gitdata.Repo{Owner: "o", Name: "r"}, 42)
	if err == nil || err.Error() != "run 123 cannot be rerun" {
		t.Fatalf("stderr 末尾行がエラーにならない: %v", err)
	}
	// stderr が空なら元のエラーを使う
	err = jobRerun(context.Background(), gitdatatest.FakeRunner("", "", errors.New("exit status 1")),
		gitdata.Repo{Owner: "o", Name: "r"}, 42)
	if err == nil || err.Error() != "exit status 1" {
		t.Fatalf("stderr 空で元エラーが使われない: %v", err)
	}
}
