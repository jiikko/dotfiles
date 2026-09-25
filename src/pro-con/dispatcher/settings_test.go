package dispatcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/eventlog"
	"pro-con/store"
)

func putSetting(t *testing.T, dir, key, value string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: store.KindConfig, Key: key, Value: value}); err != nil {
		t.Fatal(err)
	}
}

// 受付の箱に置いた PG の枠を、止めずにその Tick から使う (起動の引数 --limit より優先。消せば --limit に戻る)。
func TestSettingLimitWithoutRestart(t *testing.T) {
	r := newUsageRig(t)
	r.d.Limit = 1 // 起動の引数 --limit 1
	r.tick(t)
	if len(r.l.starts) != 1 {
		t.Fatalf("--limit 1 で %d 本起動した", len(r.l.starts))
	}
	putSetting(t, r.d.Dir, store.SettingLimit, "3")
	notes := r.tick(t)
	if len(r.l.starts) != 3 {
		t.Fatalf("設定の枠 3 を使わない (%d 本)", len(r.l.starts))
	}
	st, _, _ := store.LoadDispatcherState(r.d.Dir)
	if st.Limit != 3 || st.LimitFrom != LimitFromSetting {
		t.Fatalf("様子の上限が設定の値でない: %+v", st)
	}
	if !hasEvent(notes, eventlog.KindConfig, "limit を 3") {
		t.Fatalf("設定を変えた出来事が無い: %+v", notes)
	}
	// 起動し直しても続く (設定は置き場に残る)
	d2 := newDispatcher(t, r.d.Dir, r.l, nil)
	d2.Limit = 1
	d2.Now = r.d.Now
	if _, err := d2.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := store.LoadDispatcherState(r.d.Dir); st.Limit != 3 {
		t.Fatalf("起こし直すと設定が消えた: %+v", st)
	}
	putSetting(t, r.d.Dir, store.SettingLimit, "")
	if _, err := d2.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := store.LoadDispatcherState(r.d.Dir); st.Limit != 1 || st.LimitFrom != LimitFromFlag {
		t.Fatalf("設定を消しても --limit に戻らない: %+v", st)
	}
}

// 利用枠の絞りは人の上限より優先する (上限を上げても 80% なら 1 本、95% なら 0 本)。
func TestSettingLimitUnderUsageCap(t *testing.T) {
	for _, tc := range []struct{ pct, starts int }{{80, 1}, {95, 0}} {
		r := newUsageRig(t)
		r.pct = tc.pct
		putSetting(t, r.d.Dir, store.SettingLimit, "5")
		r.tick(t)
		if len(r.l.starts) != tc.starts {
			t.Fatalf("枠 %d%% なのに上限 5 で %d 本起動した (%d 本のはず)", tc.pct, len(r.l.starts), tc.starts)
		}
		if st, _, _ := store.LoadDispatcherState(r.d.Dir); st.Limit != 5 || st.Cap != tc.starts {
			t.Fatalf("様子が違う: %+v", st)
		}
	}
}

// 設定のファイルが壊れていても dispatcher は止まらず、起動の引数で動いて理由を出す。
func TestSettingBrokenFallsBackToFlag(t *testing.T) {
	r := newUsageRig(t)
	r.d.Limit = 2
	if err := os.WriteFile(filepath.Join(r.d.Dir, store.SettingsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	st, _, _ := store.LoadDispatcherState(r.d.Dir)
	if len(r.l.starts) != 2 || st.Limit != 2 || !strings.Contains(st.Why, store.SettingsFile) {
		t.Fatalf("壊れた設定で止まった / 理由を出さない: starts=%d %+v", len(r.l.starts), st)
	}
}

func hasEvent(evs []eventlog.Event, kind, text string) bool {
	for _, e := range evs {
		if e.Kind == kind && strings.Contains(e.Reason, text) {
			return true
		}
	}
	return false
}
