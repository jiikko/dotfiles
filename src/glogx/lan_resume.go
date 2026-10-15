package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"mac-lan-manager/lanmgr"
)

// L (mac-lan-manager) を開いたまま glogx を閉じたら、次の起動で L を開き直す (ユーザー要望 2026-10-15: issues viewer などと揃える)。
//
// 作法は filer_resume.go (F) と同じ: 同じ TTL / 未来の時刻を弾く / **開いていないまま終了したら消す** / 他の全画面の記憶が
// 当たったら開かない。照合する dir は持たない (L は repo や cwd に依らず、この Mac の網を見る画面)。
// 戻すのはタブとカーソルの Mac。Mac は Bonjour の走査で後から見つかることがあるので、見つかった時点で合わせる (lanmgr.RestorePlace)。

// lanStateTTL は記憶の有効期限 (issues / doctor / ratelimit / F と同じ)。
const lanStateTTL = issuesStateTTL

type lanScreen struct {
	Place   lanmgr.Place `json:"place"`
	SavedAt time.Time    `json:"saved_at"`
}

func lanStatePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lan-screen.json"), nil
}

func saveLanScreen(s lanScreen) error {
	path, err := lanStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func removeLanScreen() {
	if path, err := lanStatePath(); err == nil {
		_ = os.Remove(path)
	}
}

// loadLanScreen は記憶を返す。読めない / 壊れている / 期限切れ / 未来の時刻は「復元しない」に倒す。
func loadLanScreen(now time.Time) (lanmgr.Place, bool) {
	path, err := lanStatePath()
	if err != nil {
		return lanmgr.Place{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lanmgr.Place{}, false
	}
	var s lanScreen
	if err := json.Unmarshal(data, &s); err != nil {
		return lanmgr.Place{}, false
	}
	if s.SavedAt.IsZero() || now.Sub(s.SavedAt) >= lanStateTTL || s.SavedAt.After(now) {
		return lanmgr.Place{}, false
	}
	return s.Place, true
}

// rememberLanScreen は終了時の保存。L を出していれば覚え、出していなければ消す (消さないと、一覧を見て閉じた次の起動で前の L が蘇る)。
func (m *browseModel) rememberLanScreen() {
	if lm := m.lanV.live(); lm != nil {
		_ = saveLanScreen(lanScreen{Place: lm.Place(), SavedAt: timeNow()}) // 保存できなくても終了は妨げない
		return
	}
	removeLanScreen()
}

// restoreLanScreen は起動時の復元。他の全画面の復元が当たっていないときだけ呼ぶ (全画面は同時に 1 枚)。
func (m *browseModel) restoreLanScreen() tea.Cmd {
	p, ok := loadLanScreen(timeNow())
	if !ok {
		return nil
	}
	cmd := m.openFullScreen(fullScreenLan)
	if lm := m.lanV.live(); lm != nil {
		lm.RestorePlace(p)
	}
	return cmd
}
