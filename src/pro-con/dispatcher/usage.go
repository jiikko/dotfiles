package dispatcher

// 自動スケーリング (415 論点 1 / 427 の段階 5): 同時に動かす PG の数を、利用枠の残量で絞る。
// 滞留の側は dispatch がもともと見ている (分解済みのカードが無ければ起動しない / 上限まで古い順に起動する)。
//
// 枠の残量は `claude -p /usage` の stdout から読む (モデルを呼ばずに返る。2.1.281 で実測 2026-09-25: 約 4 秒・rc=0・stderr 空)。
// 🚨 --no-session-persistence が無いと、読むたびに transcript が 1 本残る (5 分ごとで 1 日 288 本。resume の一覧と /usage の集計を汚す)。
// --setting-sources "" で user の hook を走らせない。どちらも付けて出力が変わらないことを実測した。
// 🚨 モデル別の週の枠 (「Current week (Fable)」等) は見ない: PG が使うモデルを固定していない (431)。その枠が尽きたら PG が 429 で止まり、watchdog が拾う
// 🚨 文面は Claude Code の版で変わりうる。読めなくなったら (行が無い) 読めないとして画面に出し、上限は --limit のまま動かす
// (起動を止める側に倒すと、文面が変わっただけで全部の作業が止まる。枠が本当に尽きていれば起動した PG が 429 で止まり、watchdog が拾う)。
// 🚨 PG を別の設定ディレクトリで動かすようになったら (431)、ここも同じ CLAUDE_CONFIG_DIR で読む (別のアカウントの枠を見ない)

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pro-con/store"
)

// Usage は利用枠の使用率 (%)。
type Usage struct {
	Session int       // 今の 5 時間の枠
	Week    int       // 週の枠 (全モデル)
	Missing string    // 読めなかった側の行 (片方だけ読めたとき。画面に出す)
	At      time.Time // 読んだ時刻
}

const (
	usageEvery   = 5 * time.Minute  // 読み直す間隔 (1 回 約 4 秒。Tick ごとには読まない)
	usageStale   = 30 * time.Minute // 読めない間、最後に読めた値を使い続ける長さ
	usageTimeout = 30 * time.Second
	usageSlowAt  = 80 // これ以上なら同時に 1 本まで
	usageStopAt  = 95 // これ以上なら新しく起動・再開しない (動いている PG は止めない)
)

var (
	reUsageSession = regexp.MustCompile(`(?m)^\s*Current session: <?(\d+)(?:\.\d+)?% used`)
	reUsageWeek    = regexp.MustCompile(`(?m)^\s*Current week \(all models\): <?(\d+)(?:\.\d+)?% used`)
)

// ParseUsage は `claude -p /usage` の stdout から使用率を読む (小数は切り捨て、「<1%」は 1 と読む)。
// 片方の行だけ無ければ、無い側は 0 として読める側で絞り、無い側を Missing に残す (週の枠が尽きかけているのに 5 時間の枠の行が
// 無いだけで全力で起動しない / 表示の変化を黙らせない)。両方とも無ければ誤り (0% と区別する。未認証のときもこの形)。
func ParseUsage(out string) (Usage, error) {
	s := reUsageSession.FindStringSubmatch(out)
	w := reUsageWeek.FindStringSubmatch(out)
	if s == nil && w == nil {
		return Usage{}, fmt.Errorf("使用率の行が無い (未認証か、Claude Code の表示が変わった): %q", firstLine(out))
	}
	var u Usage
	if s != nil {
		u.Session, _ = strconv.Atoi(s[1])
	} else {
		u.Missing = "5 時間の枠"
	}
	if w != nil {
		u.Week, _ = strconv.Atoi(w[1])
	} else {
		u.Missing = "週の枠"
	}
	return u, nil
}

// firstLine は誤りに添える 1 行目 (80 文字まで。画面と状態のファイルに出る)。
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

// ReadUsage は dir (状態の置き場) で `claude -p /usage` を実行して使用率を読む (claude は実体の絶対パス)。
func ReadUsage(claude, dir string) func(context.Context) (Usage, error) {
	return func(ctx context.Context) (Usage, error) { return readUsage(ctx, claude, dir) }
}

func readUsage(ctx context.Context, claude, dir string) (Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()
	cmd := usageCmd(ctx, claude, dir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Usage{}, fmt.Errorf("claude -p /usage: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return ParseUsage(string(out))
}

// usageCmd は枠を読むコマンド (引数と置き場はテストが固定する)。
// 🚨 --setting-sources "" は user の settings の env と plugin も落とす。今は認証を settings に置いていないので同じアカウントの枠を読むが、
// 置くようになったら (apiKeyHelper / CLAUDE_CONFIG_DIR) PG と別の枠を読む
func usageCmd(ctx context.Context, claude, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, claude, "-p", "--no-session-persistence", "--setting-sources", "", "/usage")
	cmd.Dir = dir // 空の project が 1 つだけできる (Claude Code が cwd ごとに memory の dir を作る)
	cmd.Env = withoutTmux(os.Environ())
	cmd.WaitDelay = time.Second // 子孫が stdout を握ったままでも timeout で戻る (launcher と同じ)
	return cmd
}

// refreshUsage は usageEvery ごとに枠を読み直す。読めなければ理由を残し、前に読めた値は残す。
func (d *Dispatcher) refreshUsage(ctx context.Context, now time.Time) {
	if d.Usage == nil || (!d.usageTried.IsZero() && now.Sub(d.usageTried) < usageEvery) {
		return
	}
	d.usageTried = now
	u, err := d.Usage(ctx)
	if err != nil {
		d.usageErr = err.Error()
		return
	}
	u.At, d.usage, d.usageErr = now, &u, ""
}

// limit は同時に動かす PG の人の上限と、その出どころ。設定 (pro-con config set) が起動の引数 --limit に勝つ (store/settings.go)。
func (d *Dispatcher) limit() (int, string) {
	if d.settings.Limit > 0 {
		return d.settings.Limit, LimitFromSetting
	}
	return d.Limit, LimitFromFlag
}

// loadSettings は変えた設定を読み直す (Tick ごと。箱の依頼を適用した後)。読めなければ起動の引数で動き、理由を様子に出す
// (壊れた設定で dispatcher を止めない。直すのは次の pro-con config set)。
func (d *Dispatcher) loadSettings() {
	s, err := store.LoadSettings(d.Dir)
	d.settings, d.settingsErr = s, ""
	if err != nil {
		d.settingsErr = "設定を読めない (起動の引数で動く): " + err.Error()
	}
}

// capacity は今の同時に動かす PG の数と、人の上限 (limit) より絞った理由 (絞っていなければ空)。
// 🚨 利用枠の絞りは人の上限より優先する (上限を上げても枠を超えて起動しない)。
func (d *Dispatcher) capacity(now time.Time) (int, string) {
	lim, _ := d.limit()
	if d.Usage == nil {
		return lim, ""
	}
	u := d.usage
	if u == nil && d.usageErr == "" {
		return lim, "" // まだ読みに行っていない (最初の Tick が一覧の失敗で抜けた等)
	}
	if u == nil || now.Sub(u.At) > usageStale {
		return lim, "枠を読めない (上限のまま): " + d.usageErr
	}
	pct := max(u.Session, u.Week)
	note := ""
	if u.Missing != "" {
		note = fmt.Sprintf(" (%sの行を読めない)", u.Missing)
	}
	switch {
	case pct >= usageStopAt:
		return 0, fmt.Sprintf("枠 %d%%: 新しく起動・再開しない", pct) + note
	case pct >= usageSlowAt && lim > 1:
		return 1, fmt.Sprintf("枠 %d%%: 同時に 1 本まで", pct) + note
	case note != "":
		return lim, "枠" + note
	}
	return lim, ""
}

// writeState は dispatcher の様子を書く (store.DispatcherStateFile)。
func (d *Dispatcher) writeState(now time.Time) error {
	c, why := d.capacity(now)
	if d.settingsErr != "" {
		why = strings.TrimPrefix(why+" / "+d.settingsErr, " / ")
	}
	lim, from := d.limit()
	s := store.DispatcherState{Tick: now, Limit: lim, LimitFrom: from, Cap: c, Why: why}
	if u := d.usage; u != nil {
		s.UsageSession, s.UsageWeek, s.UsageAt = u.Session, u.Week, u.At
	}
	s.Startup, s.StartupAlert = d.startupNote(now)
	return store.SaveDispatcherState(d.Dir, s)
}

// Limit の出どころ (store.DispatcherState.LimitFrom)。
const (
	LimitFromSetting = "設定"
	LimitFromFlag    = "起動の引数"
)
