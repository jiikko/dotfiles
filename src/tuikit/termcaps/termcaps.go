// Package termcaps は、アプリの出力を最後に描く端末 (tmux の中なら外側のクライアント) が何に対応しているかを判定する。
//
// 🚨 tmux の中のアプリが端末へ問い合わせても、答えるのは tmux で、外側の端末ではない。tmux は同期出力 (DECSET 2026) に
// 対応していると答えるが、tmux から外側へ書くときに同期出力を使うのは、外側のクライアントの機能に sync があるときだけ
// (2026-10-25 に隔離した tmux 3.7c で確かめた: DECRQM に 2026;2 が返り、Terminal.app のクライアントの機能には sync が無い)。
// そこで外側のクライアントの機能 (#{client_termfeatures}) を直接聞く。
package termcaps

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Runner はコマンドを起こして標準出力を返す (使う側の外部プロセスの口。WaitDelay などの規律は使う側が持つ)。
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// TmuxClientFeatures は tmux の中で、外側の端末の機能の名前 (`#{client_termfeatures}`) を返す。
// tmux の外 (tmuxEnv が空) と、聞けなかったときは ok=false。クライアントが 1 つも attach していないと
// tmux は rc=0 で空を返すので、空も聞けなかったに数える (外側の端末が無いのに「sync が無い」と読まない)。
func TmuxClientFeatures(tmuxEnv string, run Runner) (features []string, ok bool) {
	if tmuxEnv == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := run(ctx, "tmux", "display", "-p", "#{client_termfeatures}")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, false
	}
	return ParseFeatures(out), true
}

// ParseFeatures は `#{client_termfeatures}` の出力 (カンマ区切り) を名前の列にする。
func ParseFeatures(out string) []string {
	return strings.Split(strings.TrimSpace(out), ",")
}

// SyncedOutput は、1 フレームを書き終えるまで画面に出さない (同期出力) が外側の端末まで効くか。
// false のとき、端末は書きかけのフレームを描くので、画面全体が動くアニメは乱れて見える。
//
// tmux の中は外側のクライアントの機能に sync があるかで決める。tmux の外は Terminal.app だけを非対応とする
// (bubbletea v2 が同期出力を問い合わせない端末。tea.go の shouldQuerySynchronizedOutput。それ以外は bubbletea が問い合わせて使う)。
// 判定できないとき (tmux に聞けない) は true: 動きを今までどおりに保つ側へ倒す。
func SyncedOutput(getenv func(string) string, run Runner) bool {
	if tmux := getenv("TMUX"); tmux != "" {
		f, ok := TmuxClientFeatures(tmux, run)
		return !ok || slices.Contains(f, "sync")
	}
	return !strings.Contains(getenv("TERM_PROGRAM"), "Apple")
}
