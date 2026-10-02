package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// Claude の利用枠の主な出所は、statusline (`_claude/statusline-command.sh` の write_rate_limits) が書き出す
// `claude-rate-limits.json` (issue 627)。Claude Code は推論の応答ヘッダで利用枠を受け取り、statusline の
// 入力の rate_limits に載せる。これを読む限りサーバを余計に叩かない。
// `claude -p /usage` (FetchShared の run) は、このファイルが無いか古いときだけの予備: あちらは 1 回ごとに
// /api/oauth/usage を叩き、429 になると枠が取れない (対話の /usage は応答ヘッダの値で答えるので 429 でも見える)。
//
// 欠点 (ユーザー判断 2026-10-02 で受け入れた): モデル別の週の枠 (7d(Fable) 等) は statusline に来ないので出ない。
// このマシンの対話セッションが送信しない間は新しい値が来ない (pro-con の PG は statusline を走らせない)。observedAt は
// 「誰かが新しい値を受け取った時刻」(書き手が値で新旧を比べ、新しいときだけ書く) なので、statuslineMaxAge のあいだ
// 新しい値が来なければ予備 (サーバの値。PG や他のデバイスの消費も見える) へ落ちる。
// 🚨 別のアカウントのセッション (CLAUDE_CONFIG_DIR を変えた等) も同じファイルに書く。今は同じアカウントだけを使う前提
// (pro-con の dispatcher/usage.go の 431 の注記と同じ)。別のアカウントを使うようになったら、ファイルを分ける。
const (
	statuslineFile = "claude-rate-limits.json"
	// statuslineMaxAge: これより古い観測は使わず予備 (`claude -p /usage`) へ落ちる。予備は共有ゲートで 5 分に 1 回まで
	// なので、落ちている間のサーバへの問い合わせは pro-con が元々していた頻度 (5 分ごと) に収まる。
	statuslineMaxAge = 15 * time.Minute
)

// statuslineLimit は 1 つの窓。値の無い項目は null で書かれる (statusline 側の printf)。
type statuslineLimit struct {
	UsedPercentage *int   `json:"used_percentage"`
	ResetsAt       *int64 `json:"resets_at"`
}

type statuslineState struct {
	ObservedAt int64            `json:"observedAt"`
	Version    string           `json:"version"`
	FiveHour   *statuslineLimit `json:"five_hour"`
	SevenDay   *statuslineLimit `json:"seven_day"`
}

// readStatusline は statusline が書き出した利用枠を Snapshot にする。使えない (無い・壊れている・未来の時刻・
// statuslineMaxAge より古い・使用率のある枠が 1 つも無い・使用率があるのにリセット時刻が無い) ときは nil。
// 使用率の無い窓 (Claude Code はリセットを過ぎた窓を rate_limits から落とす) は 0% (Unused) として返す。
func readStatusline(dir string, now time.Time) *Snapshot {
	data, err := os.ReadFile(filepath.Join(dir, statuslineFile))
	if err != nil {
		return nil
	}
	var st statuslineState
	if err := json.Unmarshal(data, &st); err != nil || st.ObservedAt <= 0 {
		return nil
	}
	observed := time.Unix(st.ObservedAt, 0)
	if observed.After(now) || now.Sub(observed) >= statuslineMaxAge {
		return nil
	}
	snap := &Snapshot{Version: termsafe.PlainLine(st.Version)}
	seen := false
	for _, w := range []struct {
		label string
		span  time.Duration
		l     *statuslineLimit
	}{{"5h", 5 * time.Hour, st.FiveHour}, {"7d", 7 * 24 * time.Hour, st.SevenDay}} {
		win := Window{Label: w.label, WindowMins: int64(w.span / time.Minute)}
		switch {
		case w.l == nil || w.l.UsedPercentage == nil:
			// 窓がリセットを過ぎて落とされた (その後の消費は観測していない)。リセット時刻を持たない枠は Unused (Window.Unused の doc)
			win.Unused = true
		case w.l.ResetsAt == nil:
			// 使用率はあるのにリセット時刻が無い: 想定していない形なので、推測で 0% にせず予備へ落とす
			return nil
		case !time.Unix(*w.l.ResetsAt, 0).After(now):
			// 観測の後にリセットを過ぎた: その後の消費は観測していないが、窓は空から始まっている
			win.Percent, win.Unused = 0, true
			seen = true
		default:
			win.Percent, win.ResetAt, seen = *w.l.UsedPercentage, time.Unix(*w.l.ResetsAt, 0).Local(), true
		}
		snap.Windows = append(snap.Windows, win)
	}
	if !seen {
		return nil
	}
	return snap
}
