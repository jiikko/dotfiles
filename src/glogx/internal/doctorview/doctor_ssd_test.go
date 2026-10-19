package doctorview

import (
	"doctor/disk"
	"doctor/docker"
	"doctor/ssd"
	"doctor/svc"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"strings"
	"testing"
	"time"
)

func openSSDTab(t *testing.T, withSmartctl bool) *DoctorView {
	t.Helper()
	v := testView(t)
	v.deps.SSDOpts = doctortest.FakeSSDOptions(withSmartctl)
	runCmds(t, v, v.open())
	if v.ssd == nil {
		t.Fatal("前提が崩れている: SSD の結果が届いていない")
	}
	v.tab = TabSSD
	return v
}

// 見本 どおりに、SMART の各行と APFS の未検査を出す。
func TestDoctorSSDTabRendersChecks(t *testing.T) {
	v := openSSDTab(t, true)
	out := testText(v, 30)
	for _, want := range []string{
		"[SSD ✅]", "▌SSD (APPLE SSD AP1024Z / 内蔵)", "SMART: ok / APFS: 未検査",
		"SMART Status", "✅ ok", "Verified", "予備領域", "100% (閾値 99%)", "摩耗", "7% 使用",
		"書き込み総量", "120 TB", "通電時間", "3,993 h", "安全でない電源断", "11 回",
		"APFS (Data)", "・ 未検査",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていない:\n%s", want, out)
		}
	}
	// 🚨 SSD の節だけを描く (buildRows が default で Disk に落ちていた形を止める)
	if strings.Contains(out, "▌ディスク占有") {
		t.Errorf("SSD のタブにディスクの節が出ている:\n%s", out)
	}
	if strings.Contains(out, "FAKESERIAL") {
		t.Errorf("シリアル番号が画面に出た:\n%s", out)
	}
}

// APFS の行: y は検査コマンド、Y は LLM に渡す解説 (シリアル番号を含まない)。doctor からは実行しない。
func TestDoctorSSDCopyAPFSCommand(t *testing.T) {
	v := openSSDTab(t, true)
	_ = v.Lines(testOpts(30))
	if v.cur.key != ssdAPFSRowKey {
		t.Fatalf("カーソルが APFS の行に無い: %q", v.cur.key)
	}
	if act := v.HandleKey("y", 30); act != DoctorCopyPath || v.CopyPayload() != ssd.APFSCommand {
		t.Fatalf("y = %v %q, want %q", act, v.CopyPayload(), ssd.APFSCommand)
	}
	v.HandleKey("Y", 30)
	text := v.CopyPayload()
	testenv.ContainsAll(t, "解説", text, "SMART: ok / APFS: 未検査", "予備領域: 100% (閾値 99%) [ok]", ssd.APFSCommand)
	if strings.Contains(text, "FAKESERIAL") {
		t.Errorf("シリアル番号が解説に入った:\n%s", text)
	}
	// 実行の手は持たない
	if act := v.HandleKey("x", 30); act != DoctorToast {
		t.Errorf("x = %v, want DoctorToast (SSD タブは実行しない)", act)
	}
}

// smartctl が無ければ判定不能 (ok に丸めない) + 入れ方を y でコピーできる。
func TestDoctorSSDWithoutSmartctl(t *testing.T) {
	v := openSSDTab(t, false)
	out := testText(v, 30)
	for _, want := range []string{"[SSD ❓]", "SMART: 判定不能 / APFS: 未検査", "❓ 判定不能", ssd.InstallHint} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていない:\n%s", want, out)
		}
	}
	_ = v.Lines(testOpts(30))
	v.cur.key = ssdSmartctlRowKey
	_ = v.Lines(testOpts(30))
	if h := v.Hint(200); !strings.Contains(h, "y: コマンドをコピー") {
		t.Errorf("smartctl の入れ方の行の hint = %q (コマンドをコピーする行)", h)
	}
	if v.HandleKey("y", 30); v.CopyPayload() != ssd.InstallHint || v.CopyNoun() != "コマンド" {
		t.Fatalf("y = %q (語 %q), want %q (コマンド)", v.CopyPayload(), v.CopyNoun(), ssd.InstallHint)
	}
}

// 🚨 snapshot を復元する経路でも SSD を走らせる (走らせないとタブが永久にスピナーのまま)。
func TestDoctorSSDRunsOnSnapshotRestore(t *testing.T) {
	v := testView(t)
	writeTestSnapshot(t, doctorSnapshot{ScannedAt: time.Now()})
	cmd := v.open()
	if v.snapshotAt.IsZero() {
		t.Fatal("前提が崩れている: snapshot から復元していない")
	}
	runCmds(t, v, cmd)
	if v.ssd == nil {
		t.Fatal("snapshot の復元経路で SSD を走らせていない")
	}
	if v.Scanning() {
		t.Fatal("復元の後もスキャン中のまま")
	}
}

// 🚨 5 タブで幅 60 に入れるとき、末尾を切って SSD の要約を消さない。
// 幅を超えないことだけを見ると、末尾切り (最後の手段) でも通ってしまう。fixture の値は
// **実在しうる最大級** (HumanSize の最長 "1023.9GB"・件数 2 桁) にする: 短い値では詰める段が
// 1 つも要らず、詰め方を壊しても緑のまま (実際にそうだった)
func TestDoctorTabBarKeepsAllTabsAtNarrowWidth(t *testing.T) {
	const big = int64(1023)<<30 + int64(900)<<20 // HumanSize で "1023.9GB" (最長の表記)
	v := openSSDTab(t, true)
	v.diskRep = &disk.Report{Results: []disk.Result{{Status: disk.StatusOK, Size: big}}}
	v.svcRep = &svc.Report{Findings: make([]svc.Finding, 99)}
	v.brew = &brewDoctorResult{Warnings: make([]string, 99)}
	v.docker = &docker.Report{Installed: true, Groups: []docker.Group{{Reclaimable: big}}}
	o := testOpts(20)
	o.Width = 60
	// 🚨 期待する印はリテラルで書く (ssdTabSummary から作ると、関数を壊しても期待値が一緒に変わる)。
	// 行の印 (ssdMark) の先頭と同じ = タブ行の 🚨 を追って開くと 🚨 の行がある
	wantMark := map[ssd.Status]string{ssd.StatusOK: "✅", ssd.StatusWarn: "🚨", ssd.StatusFail: "⛔", ssd.StatusUnknown: "❓"}
	for _, active := range []DoctorTab{TabDisk, TabSSD} {
		for _, st := range []ssd.Status{ssd.StatusOK, ssd.StatusWarn, ssd.StatusFail, ssd.StatusUnknown} {
			v.tab = active
			v.ssd.SMART = st
			line := v.tabBarLine(o)
			if w := term.DispWidth(line); w > o.Width {
				t.Fatalf("タブ行が幅 %d を超えた (%d 桁): %q", o.Width, w, line)
			}
			if strings.Contains(line, "…") {
				t.Errorf("tab=%v SMART=%s: 末尾が切られた: %q", active, st, line)
			}
			if want := "SSD " + wantMark[st]; !strings.Contains(line, want) {
				t.Errorf("tab=%v SMART=%s: %q が見えない: %q", active, st, want, line)
			}
		}
	}
}

// SSD の行は開くものを持たないので、hint に Enter を出さない (押せないキーを案内しない)。
func TestDoctorSSDHintHasNoEnter(t *testing.T) {
	v := openSSDTab(t, true)
	_ = v.Lines(testOpts(30))
	if h := v.Hint(200); strings.Contains(h, "Enter") || !strings.Contains(h, "y: コマンドをコピー") {
		t.Fatalf("SSD タブの hint = %q", h)
	}
	// Enter と y の案内はカーソル行から決まるので、タブを替えたら描き直してディスクの行の上で見る
	v.tab = TabDisk
	_ = v.Lines(testOpts(30))
	if h := v.Hint(200); !strings.Contains(h, "Enter: 開閉") || !strings.Contains(h, "y: パスをコピー") {
		t.Fatalf("ディスクのタブから Enter / y: パスをコピー が消えた: %q", h)
	}
}

// 狭い幅でも見出しの「SMART / APFS」の要約を落とさない (型番の方を削る)。
// SMART と APFS を並べて出すのはここだけなので、消えると SMART の結果だけが残って見える。
// 🚨 幅を掃引する: 要約が入るかの判定が 1 桁ずれると、ちょうどその 1 幅でだけ消える (3 周目で幅 58)
func TestDoctorSSDNarrowHeaderKeepsSummary(t *testing.T) {
	for _, withSmartctl := range []bool{true, false} {
		v := openSSDTab(t, withSmartctl)
		want := "SMART: " + ssdWord(v.ssd.SMART) + " / APFS: 未検査"
		for w := 40; w <= 120; w++ {
			o := testOpts(30)
			o.Width = w
			if out := strings.Join(v.Lines(o), "\n"); !strings.Contains(out, want) {
				t.Fatalf("smartctl=%v 幅 %d で要約 %q が消えた:\n%s", withSmartctl, w, want, out)
			}
		}
		o := testOpts(30)
		o.Width = 120
		if out := strings.Join(v.Lines(o), "\n"); !strings.Contains(out, "APPLE SSD AP1024Z") {
			t.Fatalf("広い幅で型番が消えた:\n%s", out)
		}
	}
}

// 空白を詰めれば入る幅では、タブ名を短くしない (段を飛ばさない)。
func TestDoctorTabBarCompactsBeforeShortNames(t *testing.T) {
	v := openSSDTab(t, true)
	v.tab = TabDisk
	o := testOpts(20)
	o.Width = 1000
	padded := strings.TrimSuffix(v.tabBarLine(o), "  (tab / h l で切替)")
	o.Width = term.DispWidth(padded) - 1 // 空白ありでは入らず、空白を詰めれば入る
	line := v.tabBarLine(o)
	if !strings.Contains(line, "ディスク") || strings.Contains(line, "…") {
		t.Fatalf("幅 %d: 空白を詰める段を飛ばした: %q (空白あり %q)", o.Width, line, padded)
	}
}
