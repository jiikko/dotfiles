package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadSettings(t *testing.T, dir string) Settings {
	t.Helper()
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// 設定の依頼は置いた順に当てて settings.json に残す。規則に反するものは記録にも設定にも入れず、理由つきで除ける。
func TestApplyConfig(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: KindConfig, Key: SettingLimit, Value: "3"})
	submit(t, dir, Request{Kind: KindConfig, Key: SettingPM, Value: "1"})
	submit(t, dir, Request{Kind: KindConfig, Key: SettingLimit, Value: "4"})
	submit(t, dir, Request{Kind: KindConfig, Key: SettingReview, Value: ReviewCodex})
	for _, bad := range []Request{
		{Kind: KindConfig, Key: SettingReview, Value: "opus"}, // 担い手は claude / codex だけ (514)
		{Kind: KindConfig, Key: SettingPM, Value: "2"},        // 415 の論点 6 まで受けない
		{Kind: KindConfig, Key: SettingPM, Value: "0"},        // PM を止めるのは --pm=off
		{Kind: KindConfig, Key: SettingLimit, Value: "0"},     // --limit と同じく 1 以上
		{Kind: KindConfig, Key: SettingLimit, Value: "x"},
		{Kind: KindConfig, Key: "pg", Value: "3"},
	} {
		submit(t, dir, bad)
	}
	res := applyAll(t, dir)
	var errs []string
	for _, r := range res {
		if r.Err != "" {
			errs = append(errs, r.Err)
		}
	}
	if len(errs) != 6 || !strings.Contains(strings.Join(errs, "\n"), "論点 6") || !strings.Contains(strings.Join(errs, "\n"), "review は") {
		t.Fatalf("除けるべき依頼を通した / 理由が違う: %q", errs)
	}
	if s := loadSettings(t, dir); s.Limit != 4 || s.PMs != 1 || s.Review != ReviewCodex {
		t.Fatalf("置いた順に当たっていない: %+v", s)
	}
	if rj, _ := filepath.Glob(filepath.Join(dir, InboxDir, RejectedDir, "*.json")); len(rj) != 6 {
		t.Fatalf("除けた依頼が rejected/ に %d 件 (6 件のはず)", len(rj))
	}
	// 消す (値が空) と既定に戻る
	submit(t, dir, Request{Kind: KindConfig, Key: SettingLimit})
	applyAll(t, dir)
	if s := loadSettings(t, dir); s.Limit != 0 || s.PMs != 1 || s.Review != ReviewCodex {
		t.Fatalf("limit だけを消せない: %+v", s)
	}
}

// 壊れた settings.json は読む側に誤りとして見せ (ゼロ値と区別する)、次の設定の依頼で書き直す (直す口を塞がない)。
func TestApplyConfigRewritesBroken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(dir); !errors.Is(err, ErrSettingsBroken) {
		t.Fatalf("壊れた設定を読めたことにした: %v", err)
	}
	submit(t, dir, Request{Kind: KindConfig, Key: SettingLimit, Value: "2"})
	res := applyAll(t, dir)
	if len(res) != 1 || res[0].Err != "" || !strings.Contains(res[0].Note, "書き直した") {
		t.Fatalf("壊れた設定を書き直したことが出来事に無い: %+v", res)
	}
	if s := loadSettings(t, dir); s.Limit != 2 {
		t.Fatalf("書き直せない: %+v", s)
	}
}

// 壊れた settings.json は、検査に落ちた設定の依頼ではゼロ値に書き直さない (黙って設定を消さない)。
func TestApplyConfigRejectKeepsBroken(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, SettingsFile)
	if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	submit(t, dir, Request{Kind: KindConfig, Key: SettingPM, Value: "99"})
	if res := applyAll(t, dir); len(res) != 1 || res[0].Err == "" {
		t.Fatalf("pm 99 を受けた: %+v", res)
	}
	if b, _ := os.ReadFile(p); string(b) != "{" {
		t.Fatalf("除けた依頼で settings.json を書き直した: %q", b)
	}
}
