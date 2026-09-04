package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// doctor を開いたまま glogx を閉じたら、次の起動でその画面を復元する。
//
// なぜ: glogx は tmux の `bind -n C-g` popup で**トグルとして**開閉する (開くキーと同じキーで
// 閉じる。tui.go の C-g の注記が出典)。トグルなら「閉じる」は「隠す」に近いので、開き直した
// ときに見ていた画面が消えていると往復のたびに D を押し直すことになる (ユーザー要望 2026-09-04)。
//
// 🚨 **走査結果は持ち越さない。** ここで覚えるのは「どの画面・どのタブを見ていたか」だけで、
// 結果は既存の doctor-snapshot.json (TTL つき) が担う。2 つを混ぜると、画面の復元が
// snapshot の信頼境界の話に巻き込まれる。
//
// 作法は issues viewer の記憶 (issues_state.go) に揃えてある: 同じ TTL / 未来の時刻を弾く /
// **開いていないまま終了したら消す**。片方だけ違う規律にすると、どちらかの理由が腐る。

// doctorStateTTL は記憶の有効期限。issues 側と同じ値 — 根拠も同じで、C-g のトグル感覚
// (閉じてすぐ開き直す) に効かせ、時間が空いた起動では素の画面から始める。
const doctorStateTTL = issuesStateTTL

// doctorScreen は閉じたときの doctor の画面。
type doctorScreen struct {
	Tab     int       `json:"tab"`
	SavedAt time.Time `json:"saved_at"`
}

func doctorStatePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "doctor-screen.json"), nil
}

func saveDoctorScreen(s doctorScreen) error {
	path, err := doctorStatePath()
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
	return os.WriteFile(path, data, 0o600)
}

func removeDoctorScreen() {
	if path, err := doctorStatePath(); err == nil {
		_ = os.Remove(path)
	}
}

// loadDoctorScreen は復元するタブを返す。読めない / 壊れている / 期限切れ / 未来の時刻は
// 「復元しない」に倒す (勝手に画面を出さない方が安全側)。
//
// 🚨 **範囲外のタブは既定へ畳む。** このファイルは一般ユーザー権限で書き換えられるので、
// 保存値をそのまま添字に使わない (doctor-snapshot.json と同じ信頼境界)。
func loadDoctorScreen(now time.Time) (doctorTab, bool) {
	path, err := doctorStatePath()
	if err != nil {
		return tabDisk, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tabDisk, false
	}
	var s doctorScreen
	if err := json.Unmarshal(data, &s); err != nil {
		return tabDisk, false
	}
	if s.SavedAt.IsZero() || now.Sub(s.SavedAt) >= doctorStateTTL || s.SavedAt.After(now) {
		return tabDisk, false
	}
	if s.Tab < 0 || s.Tab >= numDoctorTabs {
		return tabDisk, true // 復元はするが、タブは既定へ
	}
	return doctorTab(s.Tab), true
}
