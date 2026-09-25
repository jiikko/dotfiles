package store

// 動いている dispatcher を止めずに変える設定 (issue 456)。人間・PM・外の Claude は受付の箱に「設定を変える」依頼 (KindConfig) を置き、
// dispatcher が Apply の中で SettingsFile に書く (書き手は dispatcher だけ = 426 の決定 1)。dispatcher は Tick ごとに読み直す。
//
// 優先: 設定 (pro-con config set) > 起動の引数 --limit > 既定。--limit は「設定が無いときの値」で、設定を消す (unset) と戻る。
// 画面が起こす dispatcher は --limit を付けないので、起動し直しても変えた値が続くのはこの順だけ。
// 🚨 利用枠の絞り (80% で 1 本 / 95% で 0 本。dispatcher/usage.go) はこの上限より優先する (上限を上げても枠を超えて起動しない)。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SettingsFile は変えた設定の置き場 (dispatcher-state.json の隣)。
const SettingsFile = "settings.json"

// KindConfig は設定を変える依頼の種類 (Key と Value を使う。Value が空なら設定を消す)。
const KindConfig = "config"

// 設定の名前。
const (
	SettingLimit = "limit" // 同時に動かす PG の上限
	SettingPM    = "pm"    // PM の数。🚨 受けるのは 1 だけなので dispatcher はまだ読まない (2 以上を受けるのは 415 の論点 6 の後。そのとき dispatcher に配線する)
)

// SettingKeys は受け付ける設定の名前 (使い方の文と検査が引く)。
var SettingKeys = []string{SettingLimit, SettingPM}

// Settings は SettingsFile の中身。0 は「設定していない」(起動の引数・既定を使う)。
type Settings struct {
	Limit int `json:"limit,omitempty"`
	PMs   int `json:"pms,omitempty"`
}

// ErrSettingsBroken は SettingsFile が壊れているとき (読む側は起動の引数・既定で動き、理由を出す)。
var ErrSettingsBroken = errors.New("設定のファイルを読めない")

// LoadSettings は設定を読む。無ければゼロ値。壊れていたらゼロ値と ErrSettingsBroken を包んだ誤り。
func LoadSettings(dir string) (Settings, error) {
	p := filepath.Join(dir, SettingsFile)
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("%w (%s): %w", ErrSettingsBroken, p, err)
	}
	return s, nil
}

func saveSettings(dir string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, SettingsFile), data)
}

// CheckSetting は設定の名前と値を検査し、s に当てる関数を返す。CLI (置く前に弾く) と Apply (箱に直に置かれたものも弾く) の両方が使う。
// value が空なら設定を消す。
func CheckSetting(key, value string) (func(*Settings), error) {
	value = strings.TrimSpace(value)
	switch key {
	case SettingLimit:
		if value == "" {
			return func(s *Settings) { s.Limit = 0 }, nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("limit は 1 以上の整数 (%q)", value)
		}
		return func(s *Settings) { s.Limit = n }, nil
	case SettingPM:
		if value == "" {
			return func(s *Settings) { s.PMs = 0 }, nil
		}
		n, err := strconv.Atoi(value)
		switch {
		case err != nil || n < 0:
			return nil, fmt.Errorf("pm は PM の数 (%q。PM を起こさないのは dispatcher の --pm=off か、~/.config/pro-con/config.toml の pm = \"off\")", value)
		case n == 0:
			return nil, errors.New(`pm は 0 にできない (PM を起こさないのは dispatcher の --pm=off か、~/.config/pro-con/config.toml の pm = "off")`)
		case n > 1:
			return nil, fmt.Errorf("PM は今は 1 つだけ (%d は受けない。2 以上は 415 の論点 6 = PM の数と役割が決まってから)", n)
		}
		return func(s *Settings) { s.PMs = n }, nil
	}
	return nil, fmt.Errorf("知らない設定 %q (%s)", key, strings.Join(SettingKeys, " / "))
}

// configNote は設定を変えた出来事の文。
func configNote(key, value string) string {
	if strings.TrimSpace(value) == "" {
		return fmt.Sprintf("設定 %s を消した (起動の引数・既定に戻す)", key)
	}
	return fmt.Sprintf("設定 %s を %s にした (次の Tick から使う)", key, strings.TrimSpace(value))
}
