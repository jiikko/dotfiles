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
	"slices"
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
	SettingUsage = "usage" // 利用枠 (5 時間・週) を見て同時に動かす PG を絞るか (on = 既定 / off)
	SettingPM    = "pm"    // PM の数。🚨 受けるのは 1 だけなので dispatcher はまだ読まない (2 以上を受けるのは 415 の論点 6 の後。そのとき dispatcher に配線する)
	// SettingReview は敵対的レビューの担い手 (ReviewModes。issue 514)。~/.config/pro-con/config.toml の review より勝つ
	SettingReview = "review"
	// SettingSchedule は予定 (issue 550。決まった時刻に pro-con worktree clean --yes 等を回す) を回すか (on = 既定 / off)
	SettingSchedule = "schedule"
	// SettingModel / SettingEffort は PG・PM・取り込みの係の claude に渡す --model / --effort (Models / Efforts。次の起動・再開から効く)
	SettingModel  = "model"
	SettingEffort = "effort"
)

// SettingKeys は受け付ける設定の名前 (使い方の文と検査が引く)。
var SettingKeys = []string{SettingLimit, SettingUsage, SettingPM, SettingReview, SettingSchedule, SettingModel, SettingEffort}

// Models は model に書ける値 (先頭が既定)。claude の --model にそのまま渡す。
// 🚨 Claude Code の既定に任せない: 既定が変わると、PG・PM・取り込みの係のモデルと料金の形 (Opus 5.5 は cache の読みが 0.05 倍) が黙って変わる
var Models = []string{"claude-opus-5-5", "claude-fable-5-1", "claude-sonnet-5-5"}

// Efforts は effort に書ける値 (claude --effort が受ける順。画面の ← → はこの順に巡る)。既定は DefaultEffort
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// DefaultEffort は effort の既定。
const DefaultEffort = "medium"

// 敵対的レビューの担い手 (issue 514)。claude = PG が自分のサブエージェントで回す (既定。514 の前の動き) / codex = PG が codex exec で回す。
const (
	ReviewClaude = "claude"
	ReviewCodex  = "codex"
)

// ReviewModes は review に書ける値 (設定・config.toml の検査と画面の ← → が引く。先頭が既定)。
var ReviewModes = []string{ReviewClaude, ReviewCodex}

// CheckReview は review の値を検査する (空 = 設定なし は通す)。
func CheckReview(v string) error {
	if v == "" || slices.Contains(ReviewModes, v) {
		return nil
	}
	return fmt.Errorf("review は %s のどれか (%q)", strings.Join(ReviewModes, " / "), v)
}

// Settings は SettingsFile の中身。0 と空は「設定していない」(起動の引数・config.toml・既定を使う)。
type Settings struct {
	Limit int `json:"limit,omitempty"`
	PMs   int `json:"pms,omitempty"`
	// UsageOff は利用枠で絞らない (人が「枠を気にせず使う」と決めたとき。既定の false = 絞る)。
	UsageOff bool `json:"usage_off,omitempty"`
	// Review は敵対的レビューの担い手 (issue 514。空 = 設定なし = config.toml か既定)
	Review string `json:"review,omitempty"`
	// ScheduleOff は予定を回さない (既定の false = 回す)
	ScheduleOff bool `json:"schedule_off,omitempty"`
	// Model / Effort は PG・PM・取り込みの係の --model / --effort (空 = 設定なし = Models の先頭 / DefaultEffort)
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// SessionModel は PG・PM・取り込みの係に渡すモデル (設定が無い・選べる値でないなら既定)。
// 🚨 選べる値でないものを claude に渡さない: settings.json を手で直した値は CheckSetting を通らず、claude が引数で弾くと
// 再開・起動し直しは前の session を止めた後で起動に失敗する (止めてから起動する作り = dispatcher.stopThenRun)
func (s Settings) SessionModel() string { return SessionModelOf(s.Model) }

// SessionEffort は PG・PM・取り込みの係に渡す effort (設定が無い・選べる値でないなら既定)。
func (s Settings) SessionEffort() string { return SessionEffortOf(s.Effort) }

// Source は model / effort の設定の値の出どころ (画面と pro-con config show が同じ判定で出す。文言はそれぞれ)。
type Source int

const (
	SourceDefault Source = iota // 設定なし (既定を渡す)
	SourceSet                   // 設定の値を渡す
	SourceInvalid               // 設定の値は選べないので既定を渡す (settings.json を手で直した値)
)

// SessionSource は設定の値 v と、実際に渡す値 used (SessionModelOf / SessionEffortOf) から出どころを決める。
// 🚨 生の値どうしで比べる (「claude-」を外して比べると、手で書いた "opus-5-5" が既定の claude-opus-5-5 と一致して「設定」に見える)
func SessionSource(v, used string) Source {
	switch v {
	case "":
		return SourceDefault
	case used:
		return SourceSet
	}
	return SourceInvalid
}

// EffectNote は設定 key がいつ効くか (画面のトースト・出来事の文が使う)。どれも dispatcher が次の Tick で settings.json に書くが、
// model / effort は PG・PM・取り込みの係の起動・再開で claude に渡すので、効くのはその session の次の起動・再開
func EffectNote(key string) string {
	if key == SettingModel || key == SettingEffort {
		return "PG・PM・取り込みの係の次の起動・再開から効く"
	}
	return "dispatcher の次の Tick から効く"
}

// SessionModelOf / SessionEffortOf は設定の値 v から実際に渡す値を決める (画面も同じ値を出す)。
func SessionModelOf(v string) string {
	if slices.Contains(Models, v) {
		return v
	}
	return Models[0]
}

func SessionEffortOf(v string) string {
	if slices.Contains(Efforts, v) {
		return v
	}
	return DefaultEffort
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
	case SettingUsage:
		switch value {
		case "", "on":
			return func(s *Settings) { s.UsageOff = false }, nil
		case "off":
			return func(s *Settings) { s.UsageOff = true }, nil
		}
		return nil, fmt.Errorf("usage は on か off (%q)", value)
	case SettingSchedule:
		switch value {
		case "", "on":
			return func(s *Settings) { s.ScheduleOff = false }, nil
		case "off":
			return func(s *Settings) { s.ScheduleOff = true }, nil
		}
		return nil, fmt.Errorf("schedule は on か off (%q)", value)
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
	case SettingReview:
		if err := CheckReview(value); err != nil {
			return nil, err
		}
		return func(s *Settings) { s.Review = value }, nil
	case SettingModel:
		if value != "" && !slices.Contains(Models, value) {
			return nil, fmt.Errorf("model は %s のどれか (%q)", strings.Join(Models, " / "), value)
		}
		return func(s *Settings) { s.Model = value }, nil
	case SettingEffort:
		if value != "" && !slices.Contains(Efforts, value) {
			return nil, fmt.Errorf("effort は %s のどれか (%q)", strings.Join(Efforts, " / "), value)
		}
		return func(s *Settings) { s.Effort = value }, nil
	}
	return nil, fmt.Errorf("知らない設定 %q (%s)", key, strings.Join(SettingKeys, " / "))
}

// configNote は設定を変えた出来事の文。
func configNote(key, value string) string {
	when := EffectNote(key)
	if strings.TrimSpace(value) == "" {
		return fmt.Sprintf("設定 %s を消した (起動の引数・既定に戻す。%s)", key, when)
	}
	return fmt.Sprintf("設定 %s を %s にした (%s)", key, strings.TrimSpace(value), when)
}
