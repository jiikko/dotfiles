package store

import (
	"slices"
	"strings"
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

// model / effort の設定: 選べる値だけを受け、空は設定なし (= 既定 Opus 5.5 / medium) に戻す。
func TestModelEffortSetting(t *testing.T) {
	var s Settings
	if s.SessionModel() != "claude-opus-5-5" || s.SessionEffort() != "medium" {
		t.Fatalf("既定が %q / %q (claude-opus-5-5 / medium のはず)", s.SessionModel(), s.SessionEffort())
	}
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
	for _, c := range []struct{ key, value string }{{SettingModel, "opus"}, {SettingModel, "claude-haiku-4-5"}, {SettingEffort, "mid"}} {
		if _, err := CheckSetting(c.key, c.value); err == nil {
			t.Errorf("%s に %q を受けた (選べる値だけを受けるはず)", c.key, c.value)
		}
	}
	for _, key := range []string{SettingModel, SettingEffort} {
		f, err := CheckSetting(key, "")
		if err != nil {
			t.Fatal(err)
		}
		f(&s)
	}
	if got := configNote(SettingEffort, "high"); !strings.Contains(got, "次の起動・再開") {
		t.Errorf("effort の出来事の文が効く時点を言わない: %q", got)
	}
	if s.Model != "" || s.Effort != "" || !slices.Contains(SettingKeys, SettingModel) || !slices.Contains(SettingKeys, SettingEffort) {
		t.Fatalf("unset で設定なしに戻らない / SettingKeys に無い: %+v", s)
	}
}
