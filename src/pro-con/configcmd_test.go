package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/relay"
	"pro-con/store"
)

func configCmd(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	rc := runConfig(args, dir, &out, &errOut)
	return rc, out.String(), errOut.String()
}

// set は受付の箱に置くだけ (設定のファイルは dispatcher が書く)。規則に反する値は箱に置く前に弾く (PM は 1 だけ)。
func TestConfigSetSubmits(t *testing.T) {
	dir := t.TempDir()
	if rc, _, e := configCmd(t, dir, "set", "limit", "3"); rc != 0 {
		t.Fatalf("set limit 3 が rc=%d: %s", rc, e)
	}
	if rc, _, e := configCmd(t, dir, "unset", "limit"); rc != 0 {
		t.Fatalf("unset limit が rc=%d: %s", rc, e)
	}
	for _, bad := range [][]string{{"set", "pm", "2"}, {"set", "pm", "0"}, {"set", "limit", "0"}, {"set", "pg", "3"}, {"set", "limit", ""}, {"set", "limit"}, {"get"}} {
		if rc, _, _ := configCmd(t, dir, bad...); rc != 2 {
			t.Fatalf("%q を受けた (rc=%d)", bad, rc)
		}
	}
	reqs := store.PendingRequests(dir)
	if len(reqs) != 2 || reqs[0].Key != store.SettingLimit || reqs[0].Value != "3" || reqs[1].Value != "" {
		t.Fatalf("箱の依頼が違う: %+v", reqs)
	}
	if _, err := os.Stat(filepath.Join(dir, store.SettingsFile)); !os.IsNotExist(err) {
		t.Fatalf("CLI が設定のファイルを書いた (書き手は dispatcher だけ): %v", err)
	}
	// dispatcher が適用すると show に出る
	if _, err := store.Apply(dir, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	configCmd(t, dir, "set", "limit", "4")
	if _, err := store.Apply(dir, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDispatcherState(dir, store.DispatcherState{Tick: time.Now(), Limit: 4, LimitFrom: dispatcher.LimitFromSetting, Cap: 1, Why: "枠 80%: 同時に 1 本まで"}); err != nil {
		t.Fatal(err)
	}
	rc, out, _ := configCmd(t, dir, "show")
	if rc != 0 || !strings.Contains(out, "設定 4") || !strings.Contains(out, "上限 4 (設定)") || !strings.Contains(out, "今の枠 1") {
		t.Fatalf("show が違う (rc=%d):\n%s", rc, out)
	}
}

// review (514) も set で箱に置き、show に設定と dispatcher が使っている担い手・codex の実体を出す。claude / codex 以外は置く前に弾く。
func TestConfigReview(t *testing.T) {
	dir := t.TempDir()
	if rc, _, _ := configCmd(t, dir, "set", "review", "opus"); rc != 2 {
		t.Fatalf("review = opus を受けた (rc=%d)", rc)
	}
	if rc, _, e := configCmd(t, dir, "set", "review", "codex"); rc != 0 {
		t.Fatalf("set review codex が rc=%d: %s", rc, e)
	}
	if _, err := store.Apply(dir, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := configCmd(t, dir, "show"); !strings.Contains(out, "review 設定 codex / dispatcher の担い手はまだ分からない") {
		t.Fatalf("dispatcher が回る前の review が違う:\n%s", out)
	}
	save := func(ds store.DispatcherState) {
		t.Helper()
		ds.Tick, ds.Review, ds.ReviewFrom = time.Now(), "codex", dispatcher.ReviewFromSetting
		if err := store.SaveDispatcherState(dir, ds); err != nil {
			t.Fatal(err)
		}
	}
	save(store.DispatcherState{Codex: "/opt/homebrew/bin/codex"})
	if _, out, _ := configCmd(t, dir, "show"); !strings.Contains(out, "review 設定 codex / dispatcher が使っている担い手 codex (設定) / codex /opt/homebrew/bin/codex") {
		t.Fatalf("show に review が出ない:\n%s", out)
	}
	save(store.DispatcherState{CodexErr: "codex が PATH に無い"})
	if _, out, _ := configCmd(t, dir, "show"); !strings.Contains(out, "codex を解けない") || !strings.Contains(out, "codex が PATH に無い") {
		t.Fatalf("解けない codex を show に出さない:\n%s", out)
	}
}

// ps は pro-con が起動したものだけを役ごとに出し、状態の置き場に何も書かない。
func TestPSListsOwnedRolesOnly(t *testing.T) {
	root := t.TempDir()
	dir := dispatcher.E2E{Root: root}.StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	write := func(name string, v any) {
		t.Helper()
		data, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, dispatcher.LockFile), []byte("100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(store.DispatcherStateFile, store.DispatcherState{Tick: now.Add(-2 * time.Second), Limit: 2, Cap: 2})
	write(live.RegistryFile, []live.Owned{
		{ID: "pg1", SessionID: "s-pg1", PID: 200, CardID: "C-001", StartedAt: now.Add(-time.Hour)},
		{ID: "pm1", SessionID: "s-pm1", PID: 300, CardID: dispatcher.PMCardID, StartedAt: now.Add(-2 * time.Hour)},
		{ID: "pg2", SessionID: "s-pg2", PID: 201, CardID: "C-002", StartedAt: now.Add(-time.Hour)},
	})
	write(store.StateFile, store.State{NextID: 3, Cards: []card.Card{
		{ID: "C-001", State: card.Running, Exec: card.Exec{Command: "make test", Since: now.Add(-time.Minute), RunID: "r1"}},
		{ID: "C-002", State: card.Running},
	}})
	w, err := relay.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	procs := map[int]string{
		100: "/x/pro-con dispatcher --exit-without-screens 1m",
		200: "claude bg-spare pg1",
		300: "claude bg-spare pm1",
		400: "bash pro-con-run:r1 -c make test",
		401: "make test pro-con-run:r1", // 同じ印の子孫 (親を出す)
		999: "claude bg-spare outside",  // pro-con の外の session
	}
	before := snapshotTree(t, root)
	var out, errOut bytes.Buffer
	rc := runPS([]string{"--json"}, dir, func() time.Time { return now }, func(context.Context) (map[int]string, error) { return procs, nil }, &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errOut.String())
	}
	var rows []Proc
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]Proc{}
	for _, p := range rows {
		got[p.Role+" "+p.Card+" "+p.Session] = p
		if p.PID == 999 || strings.Contains(p.Command, "outside") {
			t.Fatalf("pro-con の外の session を出した: %+v", p)
		}
	}
	for key, want := range map[string]Proc{
		"dispatcher  ":  {PID: 100, Command: "/x/pro-con dispatcher --exit-without-screens 1m"},
		"PM  pm1":       {PID: 300, State: "動いている"},
		"PG C-001 pg1":  {PID: 200, State: card.Running.Label(), Command: "make test"},
		"PG C-002 pg2":  {PID: 201, State: "止まっている"},
		"テストの係 C-001 ":  {PID: 400, State: "実行中", Command: "make test"},
		"画面  " + w.ID(): {State: "開いている"},
	} {
		p, ok := got[key]
		if !ok || p.PID != want.PID || (want.State != "" && p.State != want.State) || p.Command != want.Command {
			t.Fatalf("%q の行が違う: %+v (ok=%v)\n全部: %+v", key, p, ok, rows)
		}
	}
	if !strings.HasPrefix(got["dispatcher  "].State, "動いている") {
		t.Fatalf("dispatcher が動いていない扱い: %+v", got["dispatcher  "])
	}
	after := snapshotTree(t, root)
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("pro-con ps が %s を変えた", p)
		}
	}
	if len(before) != len(after) {
		t.Fatalf("置き場のファイルが増減した: 前 %d / 後 %d", len(before), len(after))
	}
	// lock / 起動の記録の pid が別のプロセスに使い回されていたら、止まっている
	procs[100], procs[300] = "vim", "vim"
	out.Reset()
	runPS([]string{"--json"}, dir, func() time.Time { return now }, func(context.Context) (map[int]string, error) { return procs, nil }, &out, &errOut)
	if !strings.Contains(out.String(), `"role": "dispatcher",
    "state": "止まっている"`) {
		t.Fatalf("pid を使い回した別のプロセスを dispatcher とみなした:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"role": "PM",
    "pid": 300,
    "age": 7200000000000,
    "state": "止まっている"`) {
		t.Fatalf("pid を使い回した別のプロセスを PM とみなした:\n%s", out.String())
	}
}
