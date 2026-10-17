package doctorview

import (
	"glogx/internal/app"
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

// DoctorScreen は閉じたときの doctor の画面。
type DoctorScreen struct {
	Tab     int       `json:"tab"`
	SavedAt time.Time `json:"saved_at"`
}

var DoctorMemory = app.ScreenMemory[DoctorScreen]{File: "doctor-screen.json", SavedAt: func(s DoctorScreen) time.Time { return s.SavedAt }}

func SaveDoctorScreen(s DoctorScreen) error { return DoctorMemory.Save(s) }

func RemoveDoctorScreen() { DoctorMemory.Remove() }

// LoadDoctorScreen は復元するタブを返す (読めない・壊れている・期限切れ・未来の時刻は「復元しない」。app.ScreenMemory.load)。
//
// 🚨 **範囲外のタブは既定へ畳む。** このファイルは一般ユーザー権限で書き換えられるので、
// 保存値をそのまま添字に使わない (doctor-snapshot.json と同じ信頼境界)。
func LoadDoctorScreen(now time.Time) (DoctorTab, bool) {
	s, ok := DoctorMemory.Load(now)
	if !ok {
		return TabDisk, false
	}
	if s.Tab < 0 || s.Tab >= NumDoctorTabs {
		return TabDisk, true // 復元はするが、タブは既定へ
	}
	return DoctorTab(s.Tab), true
}
