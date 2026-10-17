package filerview

import (
	"time"

	"glogx/internal/app"
	"treefiler/filer"
)

// F (treefiler) を開いたまま glogx を閉じたら、次の起動で同じ場所の F を開き直す (issue 710)。
//
// なぜ: glogx は tmux の `bind -n C-g` popup で**トグルとして**開閉する。閉じるたびにプロセスが終わるので、filerView が
// プロセスの中に持っている状態 (開いたフォルダ・カーソル) は開き直しに効かない (ユーザー要望 2026-10-11)。
// 作法は issues / doctor / ratelimit の記憶と同じ: 同じ TTL / 未来の時刻を弾く / **開いていないまま終了したら消す**。
//
// 照合は repo ではなく「F を開いた dir」(F は repo root でなく cwd で開く。openFullScreen)。サブディレクトリで開いた F を、
// 別のサブディレクトリで開いた glogx に出さない。
// 記憶はファイル 1 本 (他の 3 画面と同じ)。別の dir の glogx を F を出さずに閉じても消える。F の中で root を親へ上げて (`-`)
// から閉じると、復元の root は起動した dir に戻り、その外のフォルダとカーソルは飛ばす。
// treefiler の Remember place (設定。既定 off) とは別の機能で、そちらの設定には依存しない (あちらは TTL 無しで「次に同じ場所で
// 起動したとき」)。

// filerScreen は閉じたときの F の場所。Place.Start が照合のキー (F を開いた dir)。
type filerScreen struct {
	Place   filer.Place `json:"place"`
	SavedAt time.Time   `json:"saved_at"`
}

var filerMemory = app.ScreenMemory[filerScreen]{File: "filer-screen.json", SavedAt: func(s filerScreen) time.Time { return s.SavedAt }}

func saveFilerScreen(s filerScreen) error { return filerMemory.Save(s) }

func removeFilerScreen() { filerMemory.Remove() }

// loadFilerScreen は dir で開いた F の記憶を返す。読めない / 壊れている / 期限切れ / 未来の時刻 (app.ScreenMemory.load) と
// 別の dir はすべて「復元しない」に倒す (勝手に全画面を出さない方が安全側)。
func LoadFilerScreen(now time.Time, dir string) (filer.Place, bool) { //nolint:unparam // main の restoreFilerScreen が Place を使う (check-exported はパッケージの外の利用を数えない)
	s, ok := filerMemory.Load(now)
	if !ok || s.Place.Start == "" || s.Place.Start != dir {
		return filer.Place{}, false
	}
	return s.Place, true
}

// RememberScreen は終了時の保存。F を出していれば覚え、出していなければ消す (消さないと、一覧を見て閉じた次の起動で
// 2 回前の F が蘇る)。🚨 F の q / Esc は hide の前にここを通す (hide の後だと「出していない」になる)。
func (v *FilerView) RememberScreen(now time.Time) {
	if p, ok := v.Place(); ok {
		_ = saveFilerScreen(filerScreen{Place: p, SavedAt: now}) // 保存できなくても終了は妨げない
		return
	}
	removeFilerScreen()
}
