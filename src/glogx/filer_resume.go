package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

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

// filerStateTTL は記憶の有効期限。issues / doctor / ratelimit と同じ値で、根拠も同じ (C-g のトグル感覚に効かせる)。
const filerStateTTL = issuesStateTTL

// filerScreen は閉じたときの F の場所。Place.Start が照合のキー (F を開いた dir)。
type filerScreen struct {
	Place   filer.Place `json:"place"`
	SavedAt time.Time   `json:"saved_at"`
}

func filerStatePath() (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "filer-screen.json"), nil
}

func saveFilerScreen(s filerScreen) error {
	path, err := filerStatePath()
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
	return writeAtomic(path, data) // 素の os.WriteFile は途中まで書けた JSON を残す (ratelimit_resume.go と同じ理由)
}

func removeFilerScreen() {
	if path, err := filerStatePath(); err == nil {
		_ = os.Remove(path)
	}
}

// loadFilerScreen は dir で開いた F の記憶を返す。読めない / 壊れている / 期限切れ / 未来の時刻 / 別の dir はすべて
// 「復元しない」に倒す (勝手に全画面を出さない方が安全側)。
func loadFilerScreen(now time.Time, dir string) (filer.Place, bool) {
	path, err := filerStatePath()
	if err != nil {
		return filer.Place{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return filer.Place{}, false
	}
	var s filerScreen
	if err := json.Unmarshal(data, &s); err != nil {
		return filer.Place{}, false
	}
	if s.SavedAt.IsZero() || now.Sub(s.SavedAt) >= filerStateTTL || s.SavedAt.After(now) || s.Place.Start == "" || s.Place.Start != dir {
		return filer.Place{}, false
	}
	return s.Place, true
}

// rememberFilerScreen は終了時の保存。F を出していれば覚え、出していなければ消す (消さないと、一覧を見て閉じた次の起動で
// 2 回前の F が蘇る)。🚨 F の q / Esc は hide の前にここを通る (routeKeyToFiler。hide の後だと「出していない」になる)。
func (m *browseModel) rememberFilerScreen() {
	if p, ok := m.filerV.place(); ok {
		_ = saveFilerScreen(filerScreen{Place: p, SavedAt: timeNow()}) // 保存できなくても終了は妨げない
		return
	}
	removeFilerScreen()
}

// restoreFilerScreen は起動時の復元。他の全画面の復元が当たっていないときだけ呼ぶ (全画面は同時に 1 枚)。
func (m *browseModel) restoreFilerScreen() tea.Cmd {
	dir := currentDir()
	p, ok := loadFilerScreen(timeNow(), dir)
	if !ok {
		return nil
	}
	cmd := m.openFullScreen(fullScreenFiler)
	if f := m.filerV.live(); f != nil {
		f.RestorePlace(p)
	}
	return cmd
}
