package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Claude の利用枠の主な出所は、statusline (`_claude/statusline-command.sh` の write_rate_limits) が書き出す
// `claude-rate-limits.json` (issue 627)。Claude Code は推論の応答ヘッダで利用枠を受け取り、statusline の
// 入力の rate_limits に載せる。これを読む限りサーバを余計に叩かない。
// `claude -p /usage` (FetchShared の run) は、このファイルが無いか古いときだけの予備: あちらは 1 回ごとに
// /api/oauth/usage を叩き、429 になると枠が取れない (対話の /usage は応答ヘッダの値で答えるので 429 でも見える)。
//
// 欠点 (ユーザー判断 2026-10-02 で受け入れた): モデル別の週の枠 (7d(Fable) 等) は statusline に来ないので出ない。
// Claude Code のセッションが動いていない間は更新されない (statuslineMaxAge を過ぎたら予備へ落ちる)。
const (
	statuslineFile = "claude-rate-limits.json"
	// statuslineMaxAge: これより古い観測は使わず予備 (`claude -p /usage`) へ落ちる。セッションが動いていれば
	// 描画のたびに書き直されるので、古いのはセッションが無い間だけ。その間の取得は共有ゲートが間引く。
	statuslineMaxAge = time.Hour
)

// statuslineLimit は 1 つの窓。値の無い項目は null で書かれる (statusline 側の printf)。
type statuslineLimit struct {
	UsedPercentage *int   `json:"used_percentage"`
	ResetsAt       *int64 `json:"resets_at"`
}

type statuslineState struct {
	ObservedAt int64            `json:"observedAt"`
	FiveHour   *statuslineLimit `json:"five_hour"`
	SevenDay   *statuslineLimit `json:"seven_day"`
}

// readStatusline は statusline が書き出した利用枠を Snapshot にする。使えない (無い・壊れている・未来の時刻・
// statuslineMaxAge より古い・枠が 1 つも無い) ときは nil。
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
	snap := &Snapshot{}
	for _, w := range []struct {
		label string
		span  time.Duration
		l     *statuslineLimit
	}{{"5h", 5 * time.Hour, st.FiveHour}, {"7d", 7 * 24 * time.Hour, st.SevenDay}} {
		if w.l == nil || w.l.UsedPercentage == nil {
			continue
		}
		win := Window{Label: w.label, Percent: *w.l.UsedPercentage, WindowMins: int64(w.span / time.Minute)}
		switch {
		case w.l.ResetsAt == nil:
			// 窓が開いていない (最初の消費で開く)。リセット時刻を持たない枠は Unused で表す (Window.Unused の doc)
			win.Percent, win.Unused = 0, true
		case !time.Unix(*w.l.ResetsAt, 0).After(now):
			// 観測の後にリセットを過ぎた: その後の消費は観測していないが、窓は空から始まっている
			win.Percent, win.Unused = 0, true
		default:
			win.ResetAt = time.Unix(*w.l.ResetsAt, 0).Local()
		}
		snap.Windows = append(snap.Windows, win)
	}
	if len(snap.Windows) == 0 {
		return nil
	}
	return snap
}
