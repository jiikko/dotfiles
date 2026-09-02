package main

import (
	"context"
	"encoding/json"
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
		return "", "Warning: Some installed casks are deprecated or disabled.\nYou should find replacements:\n  foo\n", 1, nil
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

func doctorText(v *doctorView, page int) string {
	return strings.Join(v.lines(doctorTestOpts(page)), "\n")
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
	out := doctorText(v, 40)
	for _, want := range []string{"▌ディスク占有", "Thing キャッシュ", "✅ 安全", "▌サービス", "壊れた登録は見つかりませんでした", "▌Homebrew", "Some installed casks are deprecated or disabled.", "(3 行)"} {
		if !strings.Contains(out, want) {
			t.Errorf("完了後の表示に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "You should find replacements") {
		t.Error("brew の本文が展開前から出ている (一覧は概要のみ)")
	}
	if strings.Contains(out, "[d]") || strings.Contains(v.hint(), " d:") {
		t.Error("段階 ③ では削除キーを出さない")
	}
}

// Enter で選んだ行の詳細をインライン展開し、もう一度で畳む。カーソルは選べる行だけに止まる。
func TestDoctorCursorAndExpand(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(40))
	if !v.rows[v.cursor].selectable || !strings.Contains(v.rows[v.cursor].text, "Thing キャッシュ") {
		t.Fatalf("初期カーソルが最初の選べる行 (ディスク 1 件目) にない: %q", v.rows[v.cursor].text)
	}
	v.handleKey("enter", 40)
	out := doctorText(v, 40)
	if !strings.Contains(out, "削除経路: rm") {
		t.Errorf("ディスク行の Enter で内訳が出ない:\n%s", out)
	}
	v.handleKey("enter", 40)
	if strings.Contains(doctorText(v, 40), "削除経路: rm") {
		t.Error("もう一度 Enter で畳まれない")
	}
	// brew の行まで j で降りる (補足行・見出し行は飛ばす)
	for range 20 {
		v.handleKey("j", 40)
	}
	_ = v.lines(doctorTestOpts(40))
	if !strings.Contains(v.rows[v.cursor].text, "Some installed casks") {
		t.Fatalf("j で brew の概要行に着かない: %q", v.rows[v.cursor].text)
	}
	v.handleKey("enter", 40)
	if out := doctorText(v, 40); !strings.Contains(out, "You should find replacements") || !strings.Contains(out, "  foo") {
		t.Errorf("brew の Enter で本文が出ない:\n%s", out)
	}
	if v.handleKey("x", 40) != doctorSwallow {
		t.Error("未知のキーを素通りさせた")
	}
}

// 「診断できず」は UI に出る (削ると 0 件 / 警告なしに化ける経路。敵対レビュー 2026-09-02 P1)。
func TestDoctorShowsUndiagnosedStates(t *testing.T) {
	v := &doctorView{shown: true, diskTotal: 3}
	v.diskRep = &disk.Report{Partial: true, Results: []disk.Result{
		{Entry: disk.Entry{ID: "a", Label: "A", Risk: disk.RiskSafe, Recover: "x"}, Status: disk.StatusFailed, Reason: "権限がありません (EACCES)"},
		{Entry: disk.Entry{ID: "b", Label: "B", Risk: disk.RiskSafe, Recover: "y"}, Status: disk.StatusOK, Size: 10,
			Items: []disk.Item{{Path: "/p", Size: 10}}, Failures: []string{"走査できず: /q: permission denied"}},
		{Entry: disk.Entry{ID: "c", Label: "C", Risk: disk.RiskSafe, Recover: "z"}, Status: disk.StatusBlocked, Reason: "Chrome 起動中のため対象外"},
	}}
	v.svcRep = &svc.Report{Scanned: 2, Interrupted: true, StatusErr: "launchctl: not found", BrewErr: "brew: not found",
		DirErrs: []string{"/Library/LaunchDaemons: permission denied"}, Undiagnosed: []svc.Undiagnosed{{PlistPath: "/x.plist", Reason: "plist を解釈できない"}}}
	v.brew = &brewDoctorResult{Unavailable: "brew not found"}
	out := doctorText(v, 60)
	for _, want := range []string{"---", "権限がありません (EACCES)", "❓ 走査できず", "一部走査できず", "permission denied", "(中断: 部分結果)",
		"途中で中断されました", "診断できず (launchctl)", "診断できず (brew)", "走査できず: /Library/LaunchDaemons", "❔ 診断できず: /x.plist",
		"Chrome 起動中のため対象外", "▌Homebrew", "診断できず", "brew not found"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が UI に出ない:\n%s", want, out)
		}
	}
	if strings.Contains(out, "壊れた登録は見つかりませんでした") {
		t.Error("診断できずがあるのに「見つかりませんでした」を出した")
	}
	if strings.Contains(out, "警告 0 件") || strings.Contains(out, "警告なし") {
		t.Error("brew の診断できずを警告 0 件 / なしに畳んだ")
	}
	if !strings.Contains(out, "合計 10B 解放可能") {
		t.Errorf("合計に failed / blocked を足した:\n%s", out)
	}
}

// 走査完了でキャッシュが書かれ、次回起動のトーストの材料になる。中断 (Esc) は partial で保存する。
// ただし partial は完全な結果を潰さない (数件だけの partial で 45GB の結果を消してトーストを黙らせない)。
func TestDoctorSavesCacheOnCompleteAndPartialPolicy(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	c, ok := loadDoctorDiskCache()
	if !ok || c.Partial || len(c.Entries) != 1 || c.Entries[0].ID != "thing" || c.Total == 0 {
		t.Fatalf("完了時のキャッシュ: ok=%v %+v", ok, c)
	}
	// 完全な結果 (Total 大) がある状態で、小さい partial を close で書こうとしても潰さない
	// (doctorTestView は XDG_CACHE_HOME を作り直すので、キャッシュは view を作った後にその置き場へ書く)
	v2 := doctorTestView(t)
	if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30, Entries: []doctorDiskCacheEntry{{ID: "big", Size: 45 << 30, Status: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	firstDisk := func(v *doctorView) {
		t.Helper()
		for _, sub := range v.open()().(tea.BatchMsg) {
			if msg, ok := sub().(doctorDiskMsg); ok && msg.ev.r != nil {
				v.receiveDisk(msg)
				return
			}
		}
		t.Fatal("disk の 1 件目が取れない")
	}
	firstDisk(v2)
	v2.close()
	c2, _ := loadDoctorDiskCache()
	if c2.Partial || c2.Total != 45<<30 {
		t.Fatalf("小さい partial が完全な結果を潰した: %+v", c2)
	}
	// 完全な結果が無ければ partial を保存する
	v3 := doctorTestView(t)
	firstDisk(v3)
	v3.close()
	if c3, ok := loadDoctorDiskCache(); !ok || !c3.Partial || len(c3.Entries) != 1 {
		t.Fatalf("完全な結果が無いのに partial が保存されない: ok=%v %+v", ok, c3)
	}
}

// 閉じて開き直した後に届く古い世代の Msg は捨てる (再スキャンで開き直した画面を汚さない)。
func TestDoctorIgnoresStaleGeneration(t *testing.T) {
	v := doctorTestView(t)
	_ = v.open()
	stale := v.gen
	v.close()
	_ = v.open() // gen が進む。shown は true
	if stale == v.gen {
		t.Fatal("open で世代が進まない")
	}
	if cmd := v.receiveDisk(doctorDiskMsg{gen: stale, ev: doctorDiskEvent{r: &disk.Result{}}}); cmd != nil || len(v.diskResults) != 0 {
		t.Error("旧世代の disk Msg を受け取った")
	}
	v.receiveSvc(doctorSvcMsg{gen: stale, rep: svc.Report{Scanned: 9}})
	if v.svcRep != nil {
		t.Error("旧世代の svc Msg を受け取った")
	}
	v.receiveBrew(doctorBrewMsg{gen: stale, res: brewDoctorResult{Clean: true}})
	if v.brew != nil {
		t.Error("旧世代の brew Msg を受け取った")
	}
	v.close()
}

// キー: D/q/esc で閉じる、r は再スキャンの信号 (走査は止めるが partial は書かない)。
func TestDoctorHandleKey(t *testing.T) {
	v := doctorTestView(t)
	_ = v.open()
	if v.handleKey("r", 20) != doctorRescan {
		t.Error("r が再スキャンの信号を返さない")
	}
	if _, ok := loadDoctorDiskCache(); ok {
		t.Error("r で partial が保存された")
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
	before := m.cursor
	m.handleKey("j")
	if m.cursor != before {
		t.Error("doctor 表示中に j が裏の一覧を動かした")
	}
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

// 壊れたキャッシュはクラッシュせず「結果なし」。保存は temp + rename で、失敗しても temp を残さない。
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
	// 中断で壊れた JSON が残らない: 書き込み先が読めないディレクトリだと保存は失敗するが、既存ファイルは無傷
	if err := os.Chmod(filepath.Dir(path), 0o500); err == nil {
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o755) })
		if os.Getuid() != 0 {
			if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 2}); err == nil {
				t.Error("書けないディレクトリで保存が成功した")
			}
			if c, ok := loadDoctorDiskCache(); !ok || c.Total != 1 {
				t.Errorf("失敗した保存が既存ファイルを壊した: ok=%v %+v", ok, c)
			}
		}
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
	// stdout に警告、stderr は前置きだけ → 両方連結して読む (片方だけ選ぶと警告を落とす)
	if res := parseBrewDoctor("Warning: X\n", "Please note that these warnings are just used to help\n", 1); len(res.Warnings) != 1 {
		t.Errorf("stdout の警告を落とした: %+v", res)
	}
	if parseBrewDoctor("", "", 1).Unavailable == "" {
		t.Error("非 0 で本文なしを診断できずにしない (0 件に畳んでいる)")
	}
	if res := parseBrewDoctor("", "Error: Homebrew must be run under Ruby 3.4\n", 1); res.Unavailable == "" || !strings.Contains(res.Unavailable, "Error: Homebrew") {
		t.Errorf("Error: 行を警告扱いにした: %+v", res)
	}
	if res := parseBrewDoctor("", "Warning: rc0 でも警告\n", 0); len(res.Warnings) != 1 {
		t.Errorf("rc=0 で stderr に Warning があるのに Clean にした: %+v", res)
	}
	if runBrewDoctor(context.Background(), func(context.Context, string, ...string) (string, string, int, error) {
		return "", "", -1, errors.New("brew not found")
	}).Unavailable == "" {
		t.Error("brew 不在を診断できずにしない")
	}
}

// 直近 5 分以内の完全な結果があれば、開いたときに走査せずそれを出す (popup の開閉ごとにスキャンしない)。
// r は snapshot を無視して走査し直す。partial は snapshot にならない。
func TestDoctorReusesRecentSnapshot(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.close()
	if _, ok := loadDoctorSnapshot(time.Now()); !ok {
		t.Fatal("完了後に snapshot が書かれない")
	}
	if cmd := v.open(); cmd != nil {
		t.Fatal("TTL 内の再オープンで走査 Cmd が返った (毎回スキャンしている)")
	}
	if v.scanning() || v.diskRep == nil || v.svcRep == nil || v.brew == nil || v.snapshotAt.IsZero() {
		t.Fatalf("snapshot から 3 セクションが復元されない: %+v", v.scanning())
	}
	if out := doctorText(v, 40); !strings.Contains(out, "分前の結果 (r で再スキャン)") || !strings.Contains(out, "Thing キャッシュ") {
		t.Errorf("snapshot 表示のヘッダー / 中身が出ない:\n%s", out)
	}
	if v.handleKey("r", 40) != doctorRescan {
		t.Fatal("r が再スキャンの信号を返さない")
	}
	if cmd := v.rescan(); cmd == nil || !v.scanning() || !v.snapshotAt.IsZero() {
		t.Fatal("r が snapshot を無視して走査し直さない")
	}
	v.close()
	// TTL 切れは走査する
	path, _ := doctorSnapshotPath()
	old := doctorSnapshot{ScannedAt: time.Now().Add(-doctorSnapshotTTL - time.Minute)}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if cmd := v.open(); cmd == nil {
		t.Fatal("TTL 切れの snapshot を使った")
	}
	v.close()
	// partial は snapshot にならない
	if err := saveDoctorSnapshot(doctorSnapshot{ScannedAt: time.Now(), Disk: disk.Report{Partial: true}}); err != nil {
		t.Fatal(err)
	}
	if sn, ok := loadDoctorSnapshot(time.Now()); ok && sn.Disk.Partial {
		t.Fatal("partial が snapshot として保存された")
	}
}
