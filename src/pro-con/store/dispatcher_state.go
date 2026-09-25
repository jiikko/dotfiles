package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DispatcherLockFile は dispatcher の排他のロックのファイル (dispatcher.Lock が flock し、持っているプロセスの pid を書く)。
const DispatcherLockFile = "dispatcher.lock"

// DispatcherGone は、ロックのファイルに書かれた pid のプロセスが居ない (= 最後に動いた dispatcher が終わっている) と言えるか (issue 483)。
// ファイルが無い・pid を読めない・プロセスが居る (pid が使い回された別のプロセスでも) は偽 (言えない。画面は Tick の古さで判じる)。
// 🚨 flock を試して確かめない: 画面が一瞬ロックを持つと、その間に起動した dispatcher が「既に動いている」で抜ける
func DispatcherGone(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, DispatcherLockFile))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// DispatcherStateFile は dispatcher が Tick ごとに書く自分の様子。画面が「dispatcher が回っているか」と今の上限を読む (415 要件 13)。
// 書くのは dispatcher だけ。
const DispatcherStateFile = "dispatcher-state.json"

// DispatcherState は DispatcherStateFile の中身。
type DispatcherState struct {
	Tick  time.Time `json:"tick"`  // 最後に回った時刻
	Limit int       `json:"limit"` // 同時に動かす PG の人の上限 (設定か --limit。LimitFrom)
	// LimitFrom は Limit がどこから来たか ("設定" = pro-con config set / "起動の引数" = --limit か既定)
	LimitFrom string `json:"limit_from,omitempty"`
	Cap       int    `json:"cap"` // 今の同時に動かす数 (利用枠の残量で絞る。dispatcher/usage.go)
	Why       string `json:"why"` // Cap を絞った / 枠を読めない理由
	// 最後に読めた利用枠の使用率 (%)。UsageAt が zero なら読めたことが無い
	UsageSession int       `json:"usage_session"`
	UsageWeek    int       `json:"usage_week"`
	UsageAt      time.Time `json:"usage_at"`
	// Startup は起動時の確かめの要約 (issue 483。起動から 10 分だけ。無ければ空)。StartupAlert は復旧した・判定できないものがある
	Startup      string `json:"startup,omitempty"`
	StartupAlert bool   `json:"startup_alert,omitempty"`
}

// SaveDispatcherState は様子を書く (書きかけを読ませない)。
func SaveDispatcherState(dir string, s DispatcherState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, DispatcherStateFile), data)
}

// LoadDispatcherState は様子を読む。無ければ (まだ 1 度も回っていない) ok = false。
func LoadDispatcherState(dir string) (DispatcherState, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, DispatcherStateFile))
	if os.IsNotExist(err) {
		return DispatcherState{}, false, nil
	}
	if err != nil {
		return DispatcherState{}, false, err
	}
	var s DispatcherState
	if err := json.Unmarshal(data, &s); err != nil {
		return DispatcherState{}, false, err
	}
	return s, true, nil
}
