// Package termcolor は、24bit の色をそのまま書き出してよいかの判定 (bubbletea の色の判定を tmux の中で補う)。
//
// 🚨 bubbletea v2 の色の判定 (colorprofile) は tmux の中では COLORTERM を見ず、`tmux info` (tmux の端末の定義) の Tc / RGB だけを見る。
// この repo の tmux は定義に RGB を持たない (missing) ので 256 色と判定され、24bit の色 (#2A2430 など) が近い 256 色 (紺の 48;5;17 など) に
// 落ちる (2026-10-15 に隔離した tmux で撮って確かめた。issue 747)。一方 _tmux.conf の terminal-features で外側の端末に RGB を付けていて、
// アプリが 24bit の色を書けば tmux はそのまま外側へ渡す。そこでクライアント (外側の端末) の機能を直接聞く。
// 使う側は true のとき `tea.WithColorProfile(colorprofile.TrueColor)` を渡す (この package は bubbletea を import しない)。
package termcolor

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Runner はコマンドを起こして標準出力を返す (使う側の外部プロセスの口。WaitDelay などの規律は使う側が持つ)。
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// TrueColorThroughTmux は tmux の中で、外側の端末が 24bit の色に対応しているか。tmux の外 (tmuxEnv が空) は false
// (colorprofile の判定 = COLORTERM に任せる)。聞けなければ false (今までどおり 256 色に落ちるだけで、壊れはしない)。
// NO_COLOR が真なら false (24bit を強制する WithColorProfile は colorprofile の NO_COLOR の判定ごと上書きする。red team の指摘 2026-10-15)。
// 真偽の読み方は colorprofile (env.go の envNoColor = strconv.ParseBool) に揃える: NO_COLOR=0 は色を出すので引き上げも外さない。
func TrueColorThroughTmux(tmuxEnv string, run Runner) bool {
	if noColor, _ := strconv.ParseBool(os.Getenv("NO_COLOR")); tmuxEnv == "" || noColor {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := run(ctx, "tmux", "display", "-p", "#{client_termfeatures}")
	if err != nil {
		return false
	}
	return HasRGB(out)
}

// HasRGB は `#{client_termfeatures}` の出力 (カンマ区切りの機能の名前) に RGB があるか。
func HasRGB(features string) bool {
	return slices.Contains(strings.Split(strings.TrimSpace(features), ","), "RGB")
}
