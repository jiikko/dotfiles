package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"pro-con/card"
)

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
	// Roles は dispatcher が起こさない役 (設定 pm / integrator = "off")。画面と card list が人の番 (card.Turn) を決めるのに読む
	Roles card.Roles `json:"roles"`
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
