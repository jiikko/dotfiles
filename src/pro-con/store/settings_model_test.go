package store

import (
	"testing"

	"pro-con/metrics"
)

// 設定で選べるモデルは、どれも料金の換算 (metrics) に単価がある (無いと、そのモデルの PG の枠が USD・% で出なくなる)。
func TestModelsArePriced(t *testing.T) {
	for _, m := range Models {
		if !metrics.Priced(m) {
			t.Errorf("model %q の単価が metrics/usage.go の prices に無い", m)
		}
	}
}

func TestModelEffortSetting(t *testing.T) {
	t.Run("既定", func(t *testing.T) {
		var s Settings
		if s.SessionModel() != "claude-opus-5-5" || s.SessionEffort() != "medium" {
			t.Fatalf("既定が %q / %q (claude-opus-5-5 / medium のはず)", s.SessionModel(), s.SessionEffort())
		}
	})
	t.Run("設定した値を使う", func(t *testing.T) {
		var s Settings
		for _, c := range []struct{ key, value string }{{SettingModel, "claude-fable-5-1"}, {SettingEffort, "high"}} {
			f, err := CheckSetting(c.key, c.value)
			if err != nil {
				t.Fatal(err)
			}
			f(&s)
		}
		if s.SessionModel() != "claude-fable-5-1" || s.SessionEffort() != "high" {
			t.Fatalf("設定した値が使われない: %q / %q", s.SessionModel(), s.SessionEffort())
		}
	})
	t.Run("選べない値を受けない", func(t *testing.T) {
		for _, c := range []struct{ key, value string }{{SettingModel, "opus"}, {SettingModel, "opus-5-5"}, {SettingModel, "claude-haiku-4-5"}, {SettingEffort, "mid"}} {
			if _, err := CheckSetting(c.key, c.value); err == nil {
				t.Errorf("%s に %q を受けた", c.key, c.value)
			}
		}
	})
	t.Run("unset で設定なしに戻る", func(t *testing.T) {
		s := Settings{Model: "claude-fable-5-1", Effort: "high"}
		for _, key := range []string{SettingModel, SettingEffort} {
			f, err := CheckSetting(key, "")
			if err != nil {
				t.Fatal(err)
			}
			f(&s)
		}
		if s.Model != "" {
			t.Errorf("model が残った: %q", s.Model)
		}
		if s.Effort != "" {
			t.Errorf("effort が残った: %q", s.Effort)
		}
	})
}

// 出どころの判定は生の値どうしで比べる (「claude-」を付けずに書いた "opus-5-5" は選べない値。claude には既定を渡す)。
func TestSessionSource(t *testing.T) {
	for _, c := range []struct {
		v    string
		want Source
	}{{"", SourceDefault}, {"claude-opus-5-5", SourceSet}, {"claude-fable-5-1", SourceSet}, {"opus-5-5", SourceInvalid}, {"opus", SourceInvalid}} {
		if got := SessionSource(c.v, SessionModelOf(c.v)); got != c.want {
			t.Errorf("SessionSource(%q) = %d, want %d", c.v, got, c.want)
		}
	}
}

// 効く時点の文: model / effort は PG・PM・取り込みの係の次の起動・再開、それ以外は dispatcher の次の Tick (出来事の文も同じ文を使う)。
func TestEffectNote(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{SettingModel, "PG・PM・取り込みの係の次の起動・再開から効く"},
		{SettingEffort, "PG・PM・取り込みの係の次の起動・再開から効く"},
		{SettingLimit, "dispatcher の次の Tick から効く"},
	} {
		if got := EffectNote(c.key); got != c.want {
			t.Errorf("EffectNote(%s) = %q, want %q", c.key, got, c.want)
		}
		if got, want := configNote(c.key, "x"), "設定 "+c.key+" を x にした ("+c.want+")"; got != want {
			t.Errorf("configNote(%s) = %q, want %q", c.key, got, want)
		}
	}
}
