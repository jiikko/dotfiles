package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
)

// doctorTestView は走査を fake に差し替えた doctor (実データ・実 launchd・実 brew を触らない)。
// disk は偽 HOME の下に 1 エントリだけのカタログ、svc は空ディレクトリ、brew は fake runner。
func doctorTestView(t *testing.T) *doctorView {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	target := filepath.Join(home, "Library", "Caches", "thing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := func(context.Context, string, ...string) (string, string, int, error) {
		return "", "Warning: Some installed casks are deprecated or disabled.\n", 1, nil
	}
	v := &doctorView{
		diskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }, AppDirs: nil},
				Run: fake,
				Catalog: []disk.Entry{{ID: "thing", Label: "Thing キャッシュ", Tier: 1, Risk: disk.RiskSafe, DeleteVia: "rm",
					Recover: "再生成されます", Paths: []string{"~/Library/Caches/thing"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		svcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		brewRun: fake,
	}
	return v
}

// runDoctorCmds は Cmd (Batch 含む) を実行して Msg を集め、view へ届ける。disk は完了まで再アームする。
func runDoctorCmds(t *testing.T, v *doctorView, cmd tea.Cmd) {
	t.Helper()
	pending := []tea.Cmd{cmd}
	deadline := time.Now().Add(20 * time.Second)
	for len(pending) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("doctor の走査が終わらない")
		}
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				pending = append(pending, sub)
			}
		case doctorDiskMsg:
			if next := v.receiveDisk(msg); next != nil {
				pending = append(pending, next)
			}
		case doctorSvcMsg:
			v.receiveSvc(msg)
		case doctorBrewMsg:
			v.receiveBrew(msg)
		case nil:
		default:
			t.Fatalf("知らない Msg: %T", msg)
		}
	}
}

func doctorTestOpts(page int) doctorRenderOpts {
	return doctorRenderOpts{width: 100, page: page, colored: false, spinner: "⠋", now: time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)}
}

// 全画面 viewer の契約: どの状態でもちょうど page 行、どの行も width を超えない。
func TestDoctorLinesFillsPage(t *testing.T) {
	v := doctorTestView(t)
	cmd := v.open()
	check := func(name string) {
		for _, page := range []int{40, 12, 4, 1} {
			o := doctorTestOpts(page)
			got := v.lines(o)
			if len(got) != page {
				t.Errorf("%s page=%d: 行数 %d", name, page, len(got))
			}
			for i, ln := range got {
				if w := dispWidth(ln); w > o.width {
					t.Errorf("%s page=%d: %d 行目の幅 %d > %d", name, page, i, w, o.width)
				}
			}
		}
	}
	check("走査中")
	if !v.scanning() {
		t.Error("開いた直後は scanning のはず")
	}
	runDoctorCmds(t, v, cmd)
	if v.scanning() {
		t.Error("全セクション完了後も scanning")
	}
	check("完了")
	out := strings.Join(v.lines(doctorTestOpts(40)), "\n")
	for _, want := range []string{"ディスク占有", "Thing キャッシュ", "✅ 安全", "サービス", "壊れた登録は見つかりませんでした", "Homebrew", "deprecated or disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("完了後の表示に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[d]") || strings.Contains(v.hint(), " d:") {
		t.Error("段階 ③ では削除キーを出さない")
	}
}

// 走査完了でキャッシュが書かれ、次回起動のトーストの材料になる。中断 (Esc) は partial で保存する。
func TestDoctorSavesCacheOnCompleteAndPartialOnClose(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	c, ok := loadDoctorDiskCache()
	if !ok || c.Partial || len(c.Entries) != 1 || c.Entries[0].ID != "thing" || c.Total == 0 {
		t.Fatalf("完了時のキャッシュ: ok=%v %+v", ok, c)
	}
	// 途中で閉じる: 1 件届いた状態で close → partial
	v2 := doctorTestView(t)
	cmd := v2.open()
	// disk の最初の 1 件だけ受け取る (Batch から disk の Cmd を探す)
	var got bool
	for _, sub := range cmd().(tea.BatchMsg) {
		if msg, ok := sub().(doctorDiskMsg); ok && msg.ev.r != nil {
			v2.receiveDisk(msg)
			got = true
			break
		}
	}
	if !got {
		t.Fatal("disk の 1 件目が取れない")
	}
	v2.close()
	c2, ok := loadDoctorDiskCache()
	if !ok || !c2.Partial || len(c2.Entries) != 1 {
		t.Fatalf("中断時のキャッシュが partial で保存されない: ok=%v %+v", ok, c2)
	}
}

// 閉じた後に届く古い世代の Msg は捨てる (再スキャンで開き直した画面を汚さない)。
func TestDoctorIgnoresStaleGeneration(t *testing.T) {
	v := doctorTestView(t)
	cmd := v.open()
	stale := v.gen
	v.close()
	if cmd := v.receiveDisk(doctorDiskMsg{gen: stale, ev: doctorDiskEvent{r: &disk.Result{}}}); cmd != nil || len(v.diskResults) != 0 {
		t.Error("閉じた後の disk Msg を受け取った")
	}
	v.receiveSvc(doctorSvcMsg{gen: stale, rep: svc.Report{Scanned: 9}})
	if v.svcRep != nil {
		t.Error("閉じた後の svc Msg を受け取った")
	}
	_ = cmd
}

// キー: D/q/esc で閉じる、r は再スキャンの信号、他は飲む (裏の一覧へ素通りしない)。
func TestDoctorHandleKey(t *testing.T) {
	v := doctorTestView(t)
	_ = v.open()
	if v.handleKey("j", 20) != doctorSwallow || v.offset != 1 {
		t.Error("j がスクロールしない / 飲まない")
	}
	if v.handleKey("x", 20) != doctorSwallow {
		t.Error("未知のキーを素通りさせた")
	}
	if v.handleKey("r", 20) != doctorRescan {
		t.Error("r が再スキャンの信号を返さない")
	}
	for _, k := range []string{"D", "q", "esc"} {
		_ = v.open()
		if v.handleKey(k, 20) != doctorClosed || v.visible() {
			t.Errorf("%s で閉じない", k)
		}
	}
}

// browseModel への配線: D で開く / Msg が届く / cancelAll で走査が止まる / hint と viewLines が doctor になる。
func TestDoctorWiredThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	tv := doctorTestView(t)
	m.doctorOv = *tv
	_, cmd := m.handleKey("D")
	if !m.doctorOv.visible() {
		t.Fatal("D で doctor が開かない")
	}
	if !strings.Contains(m.hintLine(), "再スキャン") {
		t.Errorf("doctor の hint が出ない: %q", m.hintLine())
	}
	if !strings.Contains(stripANSI(m.View().Content), "ディスク占有") {
		t.Error("View が doctor を描いていない")
	}
	if !m.spinnerActive() {
		t.Error("走査中なのに spinnerActive が false (進捗が止まって見える)")
	}
	// Msg を Update 経由で届ける
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				pending = append(pending, sub)
			}
		case nil:
		case tickMsg:
		default:
			_, next := m.Update(msg)
			pending = append(pending, next)
		}
	}
	if m.doctorOv.scanning() {
		t.Fatal("Update 経由で全セクションが完了しない (case の配線漏れ)")
	}
	// 裏の一覧のキーを飲む
	before := m.cursor
	m.handleKey("j")
	if m.cursor != before {
		t.Error("doctor 表示中に j が裏の一覧を動かした")
	}
	// 再スキャン → 走査中に cancelAll (quit 相当) で止まる
	m.handleKey("r")
	if !m.doctorOv.scanning() {
		t.Fatal("r で再スキャンが始まらない")
	}
	m.cancelAll()
	if m.doctorOv.cancel != nil {
		t.Error("cancelAll が doctor の走査を止めていない")
	}
	m.handleKey("esc")
	if m.doctorOv.visible() {
		t.Error("esc で閉じない")
	}
	if strings.Contains(m.hintLine(), "再スキャン") {
		t.Error("閉じた後も doctor の hint が残る")
	}
}

func TestDoctorStartupToast(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)
	big := doctorDiskCache{ScannedAt: now.Add(-time.Hour), Total: 45 << 30, Entries: []doctorDiskCacheEntry{
		{ID: "a", Label: "Xcode", Size: 30 << 30, Status: "ok"}, {ID: "b", Label: "npm", Size: 10 << 30, Status: "ok"}, {ID: "c", Label: "go", Size: 5 << 30, Status: "ok"},
		{ID: "d", Label: "Chrome", Size: 3 << 30, Status: "blocked"}}}
	got := doctorStartupToast(big, true, now)
	for _, want := range []string{"45.0GB 解放できます", "Xcode 30.0GB / npm 10.0GB ほか", "D で doctor を開く"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い: %q", want, got)
		}
	}
	if strings.Contains(got, "日前") || strings.Contains(got, "Chrome") {
		t.Errorf("新しい結果に日数が付いた / blocked が上位に入った: %q", got)
	}
	if doctorStartupToast(doctorDiskCache{}, false, now) != "" {
		t.Error("結果が無いのにトーストが出た (初回は沈黙する)")
	}
	small := big
	small.Total = 9 << 30
	if doctorStartupToast(small, true, now) != "" {
		t.Error("閾値未満でトーストが出た")
	}
	recent := big
	recent.LastNotifiedAt = now.Add(-24 * time.Hour)
	if doctorStartupToast(recent, true, now) != "" {
		t.Error("再通知抑止 (3 日) が効いていない")
	}
	old := big
	old.ScannedAt = now.Add(-10 * 24 * time.Hour)
	if got := doctorStartupToast(old, true, now); !strings.Contains(got, "(10 日前の診断)") || !strings.Contains(got, "45.0GB") {
		t.Errorf("古い結果でも数字を出し、日数を添える: %q", got)
	}
}

// 壊れたキャッシュはクラッシュせず「結果なし」。保存は atomic (temp が残らない)。
func TestDoctorCacheCorruptAndAtomic(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := doctorDiskCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDoctorDiskCache(); ok {
		t.Error("壊れたキャッシュを読めたことにした")
	}
	if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 1}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("一時ファイルが残った: %s", e.Name())
		}
	}
	if _, ok := loadDoctorDiskCache(); !ok {
		t.Error("保存したキャッシュが読めない")
	}
	markDoctorNotified(time.Now())
	if c, _ := loadDoctorDiskCache(); c.LastNotifiedAt.IsZero() {
		t.Error("通知時刻が記録されない")
	}
}

func TestParseBrewDoctor(t *testing.T) {
	stderr := `Please note that these warnings are just used to help the Homebrew maintainers
with debugging if you file an issue. If everything you use Homebrew for is
working fine: please don't worry or file an issue; just ignore this. Thanks!

Warning: Some installed casks are deprecated or disabled.
You should find replacements for the following casks:
  foo

Warning: Unbrewed header files were found in /usr/local/include.
`
	res := parseBrewDoctor("", stderr, 1)
	if len(res.Warnings) != 2 || !strings.HasPrefix(res.Warnings[0], "Warning: Some installed casks") || !strings.Contains(res.Warnings[0], "  foo") {
		t.Fatalf("見出し単位にまとまらない: %+v", res)
	}
	if strings.Contains(strings.Join(res.Warnings, "\n"), "Please note") {
		t.Error("定型の前置きが残っている")
	}
	if !parseBrewDoctor("Your system is ready to brew.\n", "", 0).Clean {
		t.Error("警告なしが Clean にならない")
	}
	if parseBrewDoctor("", "", 1).Unavailable == "" {
		t.Error("非 0 で本文なしを診断できずにしない (0 件に畳んでいる)")
	}
	if runBrewDoctor(context.Background(), func(context.Context, string, ...string) (string, string, int, error) {
		return "", "", -1, errors.New("brew not found")
	}).Unavailable == "" {
		t.Error("brew 不在を診断できずにしない")
	}
}
