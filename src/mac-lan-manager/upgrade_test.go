package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"mac-lan-manager/setup"
)

func TestUpgradeAdvert(t *testing.T) {
	old := setup.NameState{Running: true, Name: "air5.wired.local", IP: "192.168.100.11", Tag: "mac-lan-manager-wired-name-v2"}
	cases := []struct {
		name    string
		st      setup.NameState
		readErr error
		runErr  error
		plist   bool
		wantRC  int
		wantRun bool
	}{
		{name: "前の形は同じ名前と IP で入れ直す", st: old, wantRC: 0, wantRun: true},
		{name: "走っていない前の形も入れ直す (KeepAlive の job が落ちている間)", st: setup.NameState{Name: old.Name, IP: old.IP, Tag: old.Tag}, wantRC: 0, wantRun: true},
		{name: "今の形は触らない", st: setup.NameState{Running: true, Name: old.Name, IP: old.IP, Tag: setup.AdvertTag}, wantRC: 0},
		{name: "広告していなければ何もしない", st: setup.NameState{}, wantRC: 0},
		{name: "plist だけで job が読み込まれていなければ失敗を返す (入れ直しの bootstrap が落ちた跡)", st: setup.NameState{}, plist: true, wantRC: 1},
		{name: "読めなければ失敗", readErr: errors.New("x"), wantRC: 1},
		{name: "検査を通らない名前は入れない", st: setup.NameState{Running: true, Name: "a b.wired.local", IP: old.IP, Tag: old.Tag}, wantRC: 1},
		{name: "入れ直しの失敗を返す", st: old, runErr: errors.New("x"), wantRC: 1, wantRun: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got [][]string
			var out, errb bytes.Buffer
			rc := runUpgradeAdvert(nil, &out, &errb,
				func(context.Context) (setup.NameState, error) { return c.st, c.readErr },
				func() bool { return c.plist },
				func(_ context.Context, argv []string) error { got = append(got, argv); return c.runErr })
			if rc != c.wantRC {
				t.Errorf("rc = %d, want %d (stdout %q stderr %q)", rc, c.wantRC, out.String(), errb.String())
			}
			if !c.wantRun {
				if len(got) != 0 {
					t.Errorf("入れ直さないはずが起こした: %q", got)
				}
				return
			}
			want := setup.NameAdvert{Name: c.st.Name, IP: c.st.IP}.InstallScript()
			if len(got) != 1 || len(got[0]) != 3 || got[0][0] != "sh" || got[0][1] != "-c" || got[0][2] != want {
				t.Errorf("前の job の名前と IP で入れ直していない: %q", got)
			}
		})
	}
	if rc := runUpgradeAdvert([]string{"x"}, &bytes.Buffer{}, &bytes.Buffer{},
		func(context.Context) (setup.NameState, error) {
			t.Fatal("引数の誤りで読んだ")
			return setup.NameState{}, nil
		},
		func() bool { return false },
		func(context.Context, []string) error { t.Fatal("引数の誤りで起こした"); return nil }); rc != 2 {
		t.Errorf("引数の誤りの rc = %d", rc)
	}
}
