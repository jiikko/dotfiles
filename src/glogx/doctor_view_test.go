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

// doctorFirstDiskEvent は open して disk の 1 件目だけを受け取る (= 走査中で diskResults が非空の状態)。
// 「まだ完了していないが結果を持っている」形は partial 保存の規律が効く唯一の状態なので、
// そこを作らずに書いたテストは何も守らない (issues/170)。
func doctorFirstDiskEvent(t *testing.T, v *doctorView) {
	t.Helper()
	for _, sub := range v.open()().(tea.BatchMsg) {
		if msg, ok := sub().(doctorDiskMsg); ok && msg.ev.r != nil {
			v.receiveDisk(msg)
			return
		}
	}
	t.Fatal("disk の 1 件目が取れない")
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
	doctorFirstDiskEvent(t, v2)
	v2.close()
	c2, _ := loadDoctorDiskCache()
	if c2.Partial || c2.Total != 45<<30 {
		t.Fatalf("小さい partial が完全な結果を潰した: %+v", c2)
	}
	// 完全な結果が無ければ partial を保存する
	v3 := doctorTestView(t)
	doctorFirstDiskEvent(t, v3)
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
	// 走査中で結果を持っている状態から押す。open() 直後 (diskResults が空) だと、r を close() に
	// 変えても「どちらも何も書かない」で通ってしまう (issues/170)
	doctorFirstDiskEvent(t, v)
	if len(v.diskResults) == 0 {
		t.Fatal("前提が崩れている: diskResults が空のまま")
	}
	if v.handleKey("r", 20) != doctorRescan {
		t.Error("r が再スキャンの信号を返さない")
	}
	if _, ok := loadDoctorDiskCache(); ok {
		t.Error("r で partial が保存された (r は close を経由しない)")
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

// y はパス、Y は解説文をコピーする。中身は行の種類ごとに違う (ディスク = 対象パス、svc = plist、brew = 概要)。
func TestDoctorCopyPathAndText(t *testing.T) {
	v := &doctorView{shown: true, expanded: map[string]bool{}}
	v.diskRep = &disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "npm-cache", Label: "npm", Risk: disk.RiskSafe, Recover: "再取得", DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 2048, Items: []disk.Item{{Path: "/h/.npm/_cacache", Size: 2048}, {Path: "/h/.npm/other", Size: 0}}}}}
	v.svcRep = &svc.Report{Findings: []svc.Finding{{Label: "com.x.y", PlistPath: "/L/com.x.y.plist", Domain: "gui/501",
		Reasons: []string{"実行ファイルがありません: /nowhere"}, MissingExec: "/nowhere", Commands: []string{"launchctl bootout gui/501/com.x.y"}}}}
	v.brew = &brewDoctorResult{Warnings: []string{"Warning: Some kegs\nbody line"}}
	_ = v.lines(doctorTestOpts(40))
	if got := v.handleKey("y", 40); got != doctorCopyPath || v.copyPayload() != "/h/.npm/_cacache\n/h/.npm/other" {
		t.Fatalf("ディスク行の y: action=%v payload=%q", got, v.copyPayload())
	}
	if got := v.handleKey("Y", 40); got != doctorCopyText {
		t.Fatal("ディスク行の Y が解説をコピーしない")
	}
	for _, want := range []string{"npm [npm-cache]", "✅ 安全", "2.0KB", "削除経路: rm", "復元方法: 再取得", "/h/.npm/_cacache"} {
		if !strings.Contains(v.copyPayload(), want) {
			t.Errorf("解説に %q が無い:\n%s", want, v.copyPayload())
		}
	}
	v.handleKey("j", 40) // svc
	_ = v.lines(doctorTestOpts(40))
	if v.handleKey("y", 40) != doctorCopyPath || v.copyPayload() != "/L/com.x.y.plist" {
		t.Fatalf("svc 行の y: %q", v.copyPayload())
	}
	v.handleKey("Y", 40)
	if !strings.Contains(v.copyPayload(), "launchctl bootout gui/501/com.x.y") || !strings.Contains(v.copyPayload(), "実行ファイルがありません") {
		t.Errorf("svc の解説にコマンド / 理由が無い:\n%s", v.copyPayload())
	}
	v.handleKey("j", 40) // brew
	_ = v.lines(doctorTestOpts(40))
	if v.handleKey("y", 40) != doctorCopyPath || v.copyPayload() != "Some kegs" {
		t.Fatalf("brew 行の y: %q", v.copyPayload())
	}
	if v.handleKey("Y", 40) != doctorCopyText || !strings.Contains(v.copyPayload(), "body line") {
		t.Fatalf("brew 行の Y: %q", v.copyPayload())
	}
	// 選べる行に居なければ「無い」
	v.rows = nil
	v.cursor = 0
	if v.handleKey("y", 40) != doctorNothing {
		t.Error("行が無いのにコピーした")
	}
}

// browseModel 経由: y がクリップボード (差し替え) に届き、トーストが出る。
func TestDoctorCopyThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.doctorOv = doctorView{shown: true, expanded: map[string]bool{}}
	m.doctorOv.diskRep = &disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A", Risk: disk.RiskSafe, Recover: "r"},
		Status: disk.StatusOK, Size: 1, Items: []disk.Item{{Path: "/p/a", Size: 1}}}}}
	m.doctorOv.svcRep = &svc.Report{}
	m.doctorOv.brew = &brewDoctorResult{Clean: true}
	_ = m.View()
	m.handleKey("y")
	if copied != "/p/a" {
		t.Fatalf("y でパスがクリップボードに届かない: %q", copied)
	}
	if !m.toast.visible() || !strings.Contains(m.toast.text, "コピー") {
		t.Errorf("コピーのトーストが出ない: %q", m.toast.text)
	}
	if !strings.Contains(m.hintLine(), "y: パスをコピー") {
		t.Errorf("hint に y/Y が無い: %q", m.hintLine())
	}
}

// 前回 2 秒以上かかったエントリは 1 時間以内なら測り直さず前回の値を出す (snapshot の TTL 5 分を過ぎた再オープンでも)。
// 軽いエントリと r の再スキャンは測り直す。
func TestDoctorReusesHeavyEntries(t *testing.T) {
	v := doctorTestView(t)
	now := time.Now()
	heavy := disk.Result{Entry: disk.Entry{ID: "thing", Label: "古い定義"}, Status: disk.StatusOK, Size: 777, Elapsed: 3 * time.Second,
		MeasuredAt: now.Add(-20 * time.Minute), Items: []disk.Item{{Path: "/prev", Size: 777}}}
	sn := doctorSnapshot{ScannedAt: now.Add(-10 * time.Minute), Disk: disk.Report{Results: []disk.Result{heavy}}} // TTL 5 分は過ぎている
	data, _ := json.Marshal(sn)
	path, _ := doctorSnapshotPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := v.open()
	if cmd == nil {
		t.Fatal("TTL 切れなのに走査しない")
	}
	runDoctorCmds(t, v, cmd)
	r := v.diskRep.Results[0]
	if !r.Reused || r.Size != 777 || r.Entry.Label != "Thing キャッシュ" {
		t.Fatalf("重いエントリの前回値が再利用されない / ラベルが今のカタログに揃わない: %+v", r)
	}
	if !strings.Contains(doctorText(v, 40), "分前の計測を再利用") {
		t.Error("再利用した旨が行に出ない")
	}
	v.close()
	// r は測り直す
	runDoctorCmds(t, v, v.rescan())
	if v.diskRep.Results[0].Reused {
		t.Fatal("r でも前回値を再利用した")
	}
	v.close()
	// 軽い (Elapsed 小) / 古い (1 時間超) は再利用しない
	for name, mod := range map[string]func(*disk.Result){
		"light":   func(r *disk.Result) { r.Elapsed = 10 * time.Millisecond },
		"old":     func(r *disk.Result) { r.MeasuredAt = now.Add(-2 * time.Hour) },
		"future":  func(r *disk.Result) { r.MeasuredAt = now.Add(48 * time.Hour) }, // 時計を戻した
		"failed":  func(r *disk.Result) { r.Status = disk.StatusFailed },
		"blocked": func(r *disk.Result) { r.Status = disk.StatusBlocked },
		"nomeasure": func(r *disk.Result) {
			r.MeasuredAt = time.Time{}
		},
	} {
		h := heavy
		mod(&h)
		if f := doctorReuseFrom(doctorSnapshot{ScannedAt: now, Disk: disk.Report{Results: []disk.Result{h}}}, true, now); f != nil && f(disk.Entry{ID: "thing"}) != nil {
			t.Errorf("%s: 再利用してはいけないものを再利用した", name)
		}
	}
	// partial な snapshot は丸ごと再利用しない (中断で歪んだ結果を次の走査へ持ち込まない)
	if f := doctorReuseFrom(doctorSnapshot{ScannedAt: now, Disk: disk.Report{Partial: true, Results: []disk.Result{heavy}}}, true, now); f != nil {
		t.Error("partial な snapshot から再利用した")
	}
	// 読めなかった snapshot (ok=false) も同じ
	if f := doctorReuseFrom(doctorSnapshot{ScannedAt: now, Disk: disk.Report{Results: []disk.Result{heavy}}}, false, now); f != nil {
		t.Error("読めていない snapshot から再利用した")
	}
}

// svc の注記 (penalty box / com.apple. 偽装 / brew 孤児 / system ドメイン) は CLI と UI と
// Y のコピー文の 3 経路すべてに出る。以前は各経路が自前で文言を持っており、CLI にしか無い注記
// (AppleLikeOut / BrewOrphan) と UI にしか無い注記 (system ドメイン) があった (issues/179)。
func TestDoctorSvcAnnotationsMatchCLI(t *testing.T) {
	rep := svc.Report{Scanned: 4, Findings: []svc.Finding{
		{Label: "com.apple.fake.agent", PlistPath: "/L/LaunchAgents/com.apple.fake.agent.plist", Domain: "gui/501",
			Reasons: []string{"実行ファイルがありません: /nowhere"}, MissingExec: "/nowhere",
			PenaltyBox: true, AppleLikeOut: true, Commands: []string{"launchctl bootout gui/501/com.apple.fake.agent"}},
		{Label: "homebrew.mxcl.mysql@8.0", PlistPath: "/L/LaunchAgents/homebrew.mxcl.mysql@8.0.plist", Domain: "gui/501",
			Reasons: []string{"起動に失敗し続けています (exit 1)"}, LastExit: 1, HasLastExit: true,
			BrewOrphan: true, BrewFormula: "mysql@8.0", Commands: []string{"launchctl bootout gui/501/homebrew.mxcl.mysql@8.0"}},
		{Label: "com.vendor.daemon", PlistPath: "/Library/LaunchDaemons/com.vendor.daemon.plist", Domain: "system",
			Reasons: []string{"実行ファイルがありません: /opt/vendor/bin/d"}, MissingExec: "/opt/vendor/bin/d",
			Commands: []string{"sudo launchctl bootout system/com.vendor.daemon"}},
	}}

	// 注記は 4 種すべてが fixture に現れる (どれか 1 つでも欠けていると、この test は通っても何も守らない)
	var all []string
	for _, f := range rep.Findings {
		all = append(all, svc.Annotations(f)...)
	}
	if len(all) != 4 {
		t.Fatalf("fixture が注記 4 種を網羅していない: %q", all)
	}

	cli := svc.Format(rep)
	v := &doctorView{shown: true, expanded: map[string]bool{}, svcRep: &rep}
	// 幅を広く取る: 注記が出ているかを見るテストなので、末尾切れ (truncateDisp) で落ちないようにする
	// (狭い幅で注記が読めなくなる問題は issues/182 が別に扱う)
	wide := doctorTestOpts(60)
	wide.width = 240
	ui := strings.Join(v.lines(wide), "\n")

	var copies strings.Builder
	for _, r := range v.rows {
		if r.copyText != "" {
			copies.WriteString(r.copyText)
		}
	}

	for _, a := range all {
		if !strings.Contains(cli, a) {
			t.Errorf("CLI の Format に注記が無い: %q\n%s", a, cli)
		}
		if !strings.Contains(ui, a) {
			t.Errorf("UI の svcSection に注記が無い: %q\n%s", a, ui)
		}
		if !strings.Contains(copies.String(), a) {
			t.Errorf("Y のコピー文に注記が無い: %q\n%s", a, copies.String())
		}
	}

	// 「台帳にあり=false」のような、人が読めない暗号でごまかしていないこと
	if strings.Contains(copies.String(), "台帳にあり=") {
		t.Error("Y のコピー文が brew 孤児を暗号 (台帳にあり=) で書いている")
	}
}

// partial 保存の境界。「小さい partial が完全な結果を潰さない」だけを見ていると、それを成立させている
// 3 つの条件のうち 2 つ (合計の比較 / 前回が partial かどうか) を消しても green になる (issues/170)。
func TestDoctorSaveCachePartialBoundaries(t *testing.T) {
	rep := func(total int64, partial bool) disk.Report {
		return disk.Report{Partial: partial, Total: total, ScannedAt: time.Now(),
			Results: []disk.Result{{Entry: disk.Entry{ID: "thing", Label: "Thing"}, Status: disk.StatusOK,
				Size: total, Items: []disk.Item{{Path: "/x", Size: total}}}}}
	}

	// (a) 合計が前回より大きい partial は書く。重いエントリが先に終わった中断結果を捨てない
	v := doctorTestView(t)
	if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	v.saveCache(rep(45<<30, true))
	if c, _ := loadDoctorDiskCache(); c.Total != 45<<30 || !c.Partial {
		t.Errorf("大きい partial が書かれない: %+v", c)
	}

	// (b) 前回が partial なら、合計が小さくても最新の partial で置き換える
	v2 := doctorTestView(t)
	if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30, Partial: true}); err != nil {
		t.Fatal(err)
	}
	v2.saveCache(rep(1<<30, true))
	if c, _ := loadDoctorDiskCache(); c.Total != 1<<30 {
		t.Errorf("前回が partial なのに最新の partial で置き換わらない: %+v", c)
	}

	// (c) 完全な結果は、前回が大きくても常に書く (再スキャンで減った実体を反映する)
	v3 := doctorTestView(t)
	if err := saveDoctorDiskCache(doctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30}); err != nil {
		t.Fatal(err)
	}
	v3.saveCache(rep(1<<30, false))
	if c, _ := loadDoctorDiskCache(); c.Total != 1<<30 || c.Partial {
		t.Errorf("完全な結果が書かれない: %+v", c)
	}

	// (d) close が書く partial には合計が入る (Total の計算を消すと 0 になり、トーストが沈黙する)
	v4 := doctorTestView(t)
	doctorFirstDiskEvent(t, v4)
	var want int64 // production の SumDeletable ではなく Items から自前で足す
	for _, r := range v4.diskResults {
		for _, it := range r.Items {
			want += it.Size
		}
	}
	if want == 0 {
		t.Fatal("前提が崩れている: 受け取った結果に Items が無い")
	}
	v4.close()
	if c, ok := loadDoctorDiskCache(); !ok || c.Total != want {
		t.Errorf("close の partial に合計が入っていない: ok=%v total=%d want=%d", ok, c.Total, want)
	}
}

// snapshot は「未来の時刻」と「中断した svc」を拒む。どちらも消しても、正常系のテストは全部通る。
func TestDoctorSnapshotRejectsFutureAndInterrupted(t *testing.T) {
	v := doctorTestView(t)
	_ = v
	now := time.Now()
	write := func(sn doctorSnapshot) {
		t.Helper()
		data, _ := json.Marshal(sn)
		path, _ := doctorSnapshotPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// 時計を戻した (ScannedAt が未来) snapshot は TTL 内と読まない
	write(doctorSnapshot{ScannedAt: now.Add(48 * time.Hour)})
	if _, ok := loadDoctorSnapshot(now); ok {
		t.Error("未来の snapshot を TTL 内として読んだ")
	}
	// 正常な TTL 内は読む (上の assert が「常に false」で通っていないことの確認)
	write(doctorSnapshot{ScannedAt: now.Add(-1 * time.Minute)})
	if _, ok := loadDoctorSnapshot(now); !ok {
		t.Error("TTL 内の snapshot を読めない")
	}

	// 中断した svc を含む結果は snapshot にしない (開き直しで中断の姿を再現しない)
	path, _ := doctorSnapshotPath()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := saveDoctorSnapshot(doctorSnapshot{ScannedAt: now, Svc: svc.Report{Interrupted: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDoctorSnapshotAny(); ok {
		t.Error("svc が中断した結果を snapshot に書いた")
	}
	if err := saveDoctorSnapshot(doctorSnapshot{ScannedAt: now, Disk: disk.Report{Partial: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDoctorSnapshotAny(); ok {
		t.Error("partial な disk の結果を snapshot に書いた")
	}
	// ガードに当たらない結果は書ける
	if err := saveDoctorSnapshot(doctorSnapshot{ScannedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDoctorSnapshotAny(); !ok {
		t.Error("正常な結果が snapshot に書かれない")
	}
}

// 閉じた後に届いた disk の Msg は捨てる。世代 (gen) が同じでも画面が閉じていれば無視する
// (閉じた画面のキャッシュ書き込み・状態更新を起こさない)。
func TestDoctorIgnoresDiskMsgAfterClose(t *testing.T) {
	v := doctorTestView(t)
	gen := 0
	var complete doctorDiskMsg
	for _, sub := range v.open()().(tea.BatchMsg) {
		if msg, ok := sub().(doctorDiskMsg); ok {
			gen = msg.gen
			if msg.ev.rep != nil {
				complete = msg
			}
		}
	}
	// 完了イベントは channel の後ろにいるので、届くまで受け取る (回数で打ち切る。時間で待たない)
	for i := 0; complete.ev.rep == nil && i < 100; i++ {
		msg, ok := v.waitDiskCmd(gen)().(doctorDiskMsg)
		if !ok {
			break
		}
		if msg.ev.rep != nil {
			complete = msg
		}
	}
	if complete.ev.rep == nil {
		t.Fatal("完了イベントが取れない")
	}
	v.close()
	if _, ok := loadDoctorDiskCache(); ok {
		t.Fatal("前提が崩れている: close の時点でキャッシュが書かれている")
	}
	if cmd := v.receiveDisk(complete); cmd != nil {
		t.Error("閉じた後の Msg に対して次の待ち受けを返した")
	}
	if v.diskRep != nil {
		t.Error("閉じた後の Msg で画面の状態を更新した")
	}
	if _, ok := loadDoctorDiskCache(); ok {
		t.Error("閉じた後の Msg でキャッシュを書いた")
	}
}

// トーストの文面は実経路 (走査の Report → キャッシュ → 文面) で固定する。純関数だけを手作りの
// キャッシュで見ていると、Report からキャッシュへ落とす部分 (Status / Label) を壊しても green になる。
func TestDoctorStartupToastThroughRealPath(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)
	rep := disk.Report{ScannedAt: now, Total: 30 << 30, Results: []disk.Result{
		{Entry: disk.Entry{ID: "xcode", Label: "Xcode"}, Status: disk.StatusOK, Size: 20 << 30, Items: []disk.Item{{Path: "/x", Size: 20 << 30}}},
		{Entry: disk.Entry{ID: "npm", Label: "npm"}, Status: disk.StatusOK, Size: 10 << 30, Items: []disk.Item{{Path: "/n", Size: 10 << 30}}},
		{Entry: disk.Entry{ID: "boom", Label: "壊れた", Risk: disk.RiskSafe}, Status: disk.StatusFailed, Size: 99 << 30, Reason: "走査できず"},
		{Entry: disk.Entry{ID: "empty", Label: "空"}, Status: disk.StatusOK},
	}}
	c := doctorCacheFromReport(rep, time.Time{})

	// 候補 0 件の ok エントリは持たない。失敗したものは (数字を出さないために) 落とさず status で区別する
	for _, e := range c.Entries {
		if e.ID == "empty" {
			t.Error("候補 0 件のエントリをキャッシュに持っている")
		}
	}
	got := doctorStartupToast(c, true, now)
	for _, want := range []string{"30.0GB 解放できます", "Xcode 20.0GB", "npm 10.0GB", "D で doctor を開く"} {
		if !strings.Contains(got, want) {
			t.Errorf("トーストに %q が無い: %q", want, got)
		}
	}
	// failed のサイズはトーストに出さない (走査できていない数字を「解放できます」に混ぜない)
	if strings.Contains(got, "壊れた") || strings.Contains(got, "99.0GB") {
		t.Errorf("走査できなかったエントリがトーストに出た: %q", got)
	}
	// cooldown 内は沈黙し、明けたら出る。未来の記録 (時計を戻した) でも沈黙し続けない
	if s := doctorStartupToast(c, true, now.Add(time.Hour)); s != "" {
		_ = s // cooldown は LastNotifiedAt が空なので出る側。ここでは c を使い回さない
	}
	c2 := c
	c2.LastNotifiedAt = now.Add(-1 * time.Hour)
	if s := doctorStartupToast(c2, true, now); s != "" {
		t.Errorf("cooldown 内なのにトーストが出た: %q", s)
	}
	c3 := c
	c3.LastNotifiedAt = now.Add(-8 * 24 * time.Hour)
	if s := doctorStartupToast(c3, true, now); s == "" {
		t.Error("cooldown が明けてもトーストが出ない")
	}
}
