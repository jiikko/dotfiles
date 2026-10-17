package doctorview

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"doctor/disk"
	"glogx/internal/term"
)

// 対象が 0 件の行は選べない (行そのものが一覧に出ないので、判定を単体で見る)。
func TestDoctorDeletableRejectsEmptyItems(t *testing.T) {
	v := &DoctorView{}
	r := disk.Result{Status: disk.StatusOK, Entry: disk.Entry{ID: "x"}}
	if ok, why := v.Deletable(r); ok || !strings.Contains(why, "削除対象はありません") {
		t.Errorf("ok=%v why=%q", ok, why)
	}
}

// doctor を止める (popup を閉じる / 終了) と、削除も ctx で中断される。
func TestDoctorStopCancelsDelete(t *testing.T) {
	v := &DoctorView{}
	cancelled := false
	v.Del = DoctorDelete{Running: true, Cancel: func() { cancelled = true }}
	v.Stop()
	if !cancelled {
		t.Error("stop() が削除の ctx を切っていない (cli: の子が孤児になる)")
	}
}

// risk: confirm は Inspect が付いていなくてもゲートに掛かる
// (カタログが Inspect を付け忘れた瞬間にゲートが消える形を防ぐ)。
func TestDeletableGatesRiskConfirmWithoutInspect(t *testing.T) {
	v := &DoctorView{}
	r := disk.Result{Status: disk.StatusOK, Items: []disk.Item{{Path: "/x"}},
		Entry: disk.Entry{ID: "x", Risk: disk.RiskConfirm, Inspect: false}}
	if ok, why := v.Deletable(r); ok || !strings.Contains(why, "中身を確認") {
		t.Errorf("ok=%v why=%q", ok, why)
	}
}

// 語彙と桁の対応 (取り違えると「消えていないのに数字が出る」)。
func TestDeleteVocabulary(t *testing.T) {
	for _, tc := range []struct {
		o        disk.Outcome
		wantWord string
		e        disk.EntryOutcome
		wantSize string
	}{
		{disk.OutcomeDeleted, "✅ 削除した", disk.EntryOutcome{Outcome: disk.OutcomeDeleted, Freed: 1024}, "1.0KB"},
		{disk.OutcomeTrashed, "🚮 ゴミ箱へ", disk.EntryOutcome{Outcome: disk.OutcomeTrashed, Trashed: 2048}, "2.0KB"},
		{disk.OutcomeIncomplete, "🚨 未完了", disk.EntryOutcome{Outcome: disk.OutcomeIncomplete, Freed: 4096}, "---"},
		{disk.OutcomeSkipped, "🚫 触れず", disk.EntryOutcome{Outcome: disk.OutcomeSkipped, Freed: 4096}, "---"},
		{disk.OutcomeFailed, "❌ できず", disk.EntryOutcome{Outcome: disk.OutcomeFailed, Freed: 4096}, "---"},
		{disk.OutcomeProposed, "📋 表示のみ", disk.EntryOutcome{Outcome: disk.OutcomeProposed}, "---"},
	} {
		if got := DoctorOutcomeWord(tc.o); got != tc.wantWord {
			t.Errorf("%s の語 = %q, want %q", tc.o, got, tc.wantWord)
		}
		// 🚨 消えていない / 消えたか分からないものに数字を出さない
		if got := deleteResultSize(tc.e); got != tc.wantSize {
			t.Errorf("%s の桁 = %q, want %q", tc.o, got, tc.wantSize)
		}
	}
	for method, want := range map[string]string{"rm": "✅ 削除", "trash": "🚮 ゴミ箱へ", "cli": "📋 コマンド", "propose": "📋 表示のみ"} {
		if got := deleteMethodWord(method); got != want {
			t.Errorf("%s の語 = %q, want %q", method, got, want)
		}
	}
	// 記号は単独で幅 2 のものだけ (端末で右端が動かないように)
	for _, s := range []string{"✅", "🚮", "🚨", "🚫", "❌", "📋"} {
		if w := term.DispWidth(s); w != 2 {
			t.Errorf("%q の幅 = %d (2 でない記号は使わない)", s, w)
		}
	}
	// 🚨 語は行の**最後の列**なので、deleteWordW を超えるとラベルの予算が破れて語自体が切れる。
	// 出典は deleteMethodWord と DoctorOutcomeWord / doctorPlanOutcomeWord の 3 つある
	words := []string{deleteMethodWord("rm"), deleteMethodWord("trash"), deleteMethodWord("cli"),
		deleteMethodWord("propose"), doctorPlanOutcomeWord(disk.OutcomeSkipped)}
	for _, o := range []disk.Outcome{disk.OutcomeDeleted, disk.OutcomeTrashed, disk.OutcomeIncomplete,
		disk.OutcomeSkipped, disk.OutcomeProposed, disk.OutcomeFailed, disk.OutcomePlanned} {
		words = append(words, DoctorOutcomeWord(o))
	}
	for _, w := range words {
		if got := term.DispWidth(w); got > deleteWordW {
			t.Errorf("語 %q の幅 = %d (deleteWordW = %d を超えると行末で切れる)", w, got, deleteWordW)
		}
	}
}

// 結果 / エラーの画面で y は出力をコピーする (閉じない)。失敗を LLM へ投げるため。
func TestDeleteResultCopiesCommandOutput(t *testing.T) {
	rep := disk.DeleteReport{HistoryPath: "/tmp/h.json", Entries: []disk.EntryOutcome{
		{Label: "ランタイム", Outcome: disk.OutcomeFailed, Reason: "exit 24",
			Items: []disk.ItemOutcome{{Path: "/L/a", Outcome: disk.OutcomeFailed, Reason: "exit 24"}}}}}
	v := &DoctorView{Del: DoctorDelete{Result: &rep,
		Log: CommandLogLines(disk.CommandRecord{Name: "xcrun", Args: []string{"simctl"}, RC: 24, Stderr: "retry"})}}
	act, taken := v.HandleDeleteKey("y")
	if !taken || act != DoctorCopyLog {
		t.Fatalf("act=%v taken=%v (y は出力をコピー)", act, taken)
	}
	if v.Del.Result == nil {
		t.Fatal("y で閉じてしまった (コピーは閉じない)")
	}
	got := v.CopyPayload()
	for _, want := range []string{"$ xcrun simctl", "exit 24", "2| retry", "/L/a", "/tmp/h.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("コピー文に %q が無い:\n%s", want, got)
		}
	}
	// y 以外は閉じて再スキャン
	if act, _ := v.HandleDeleteKey("j"); act != DoctorRescan || v.Del.Active() {
		t.Errorf("他のキーで閉じない: act=%v", act)
	}
}

// 🚨 重いエントリの計測値を再利用しても「画面ごと復元した」印は立てない。
// 立つと、今回走査した画面なのに削除が「前回の結果を表示しています」と断り、
// snapshotAt は zero なので再スキャンにも倒れず、その行だけ行き止まりになる。
func TestReuseDoesNotCarryFromSnapshot(t *testing.T) {
	now := time.Now()
	sn := DoctorSnapshot{ScannedAt: now.Add(-10 * time.Minute), Disk: disk.Report{Results: []disk.Result{{
		Entry: disk.Entry{ID: "heavy"}, Status: disk.StatusOK, MeasuredAt: now.Add(-10 * time.Minute),
		Elapsed: 5 * time.Second, FromSnapshot: true, Items: []disk.Item{{Path: "/x", Size: 1}}, Size: 1,
	}}}}
	reuse := DoctorReuseFrom(sn, true, now)
	if reuse == nil {
		t.Fatal("再利用が効いていない (前提が崩れている)")
	}
	got := reuse(disk.Entry{ID: "heavy"})
	if got == nil {
		t.Fatal("再利用されない")
	}
	if got.FromSnapshot {
		t.Error("再利用に「画面ごと復元した」印が残っている")
	}
}

// トーストは一度出したら消える (残ると、次の再スキャンの理由として使い回される)。
func TestDoctorToastIsConsumedOnce(t *testing.T) {
	v := &DoctorView{PendingToast: "一度だけ"}
	if got := v.TakeToast(); got != "一度だけ" {
		t.Fatalf("takeToast = %q", got)
	}
	if got := v.TakeToast(); got != "" {
		t.Errorf("2 回目でも文言が残っている: %q", got)
	}
}

// 前回の結果の画面でも、削除に関係ない行では再スキャンを起こさない。
func TestSnapshotRescanOnlyOnDiskRows(t *testing.T) {
	v := &DoctorView{SnapshotAt: time.Now().Add(-time.Minute),
		Rows: []DoctorRow{{Key: "brew:0:x", Selectable: true}}, Cur: rowCursor{Index: 0}}
	if _, ok := v.snapshotRescan(); ok {
		t.Error("brew の行で再スキャンを起こした")
	}
	v.Rows = []DoctorRow{{Key: "disk:thing", Selectable: true}}
	if _, ok := v.snapshotRescan(); !ok {
		t.Error("ディスクの行で再スキャンを起こさない")
	}
}

// 裏取りコマンドの switch が見ているカタログ ID は、実在するものだけ
// (リネームすると裏取りが黙って消えるだけで build も test も赤くならない)。
func TestDiskVerifyCommandsIDsExistInCatalog(t *testing.T) {
	src, err := os.ReadFile("doctor_view.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func diskVerifyCommands(")
	if start < 0 {
		t.Fatal("diskVerifyCommands が見つからない (走査が壊れている)")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("関数の終わりが見つからない")
	}
	// 🚨 `case "([a-z0-9-]+)"` で拾うと **case 直後の 1 個目のリテラルにしか当たらない**。
	// `case "a", "b":` の 2 個目以降が突合を素通りし、11 ID 中 7 件しか見ていなかった
	// (実測 2026-09-03)。case 行に出る全リテラルを拾うこと。
	// この switch の case 行は ID しか持たない前提。別の文字列を並べたら
	// 「カタログに無い ID」で落ちる (fail-closed 側なので、落ちたら走査を直す)
	lit := regexp.MustCompile(`"([a-z0-9-]+)"`)
	var ids []string
	for _, line := range strings.Split(body[start:start+end], "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "case ") {
			continue
		}
		for _, m := range lit.FindAllStringSubmatch(line, -1) {
			ids = append(ids, m[1])
		}
	}
	if len(ids) < 10 {
		t.Fatalf("case を %d 件しか拾えていない (走査が壊れている)", len(ids))
	}
	for _, id := range ids {
		if !disk.CatalogHasID(id) {
			t.Errorf("カタログに無い ID を見ている: %q (リネームで裏取りが黙って消える)", id)
		}
	}
}

// パスは**末尾を残して**詰める (issue 239)。末尾から切ると DerivedData のハッシュ部が落ち、
// 同一プロジェクトの旧世代を確認画面で見分けられないまま y を押すことになる。
func TestDeleteConfirmKeepsPathTail(t *testing.T) {
	long := "/Users/koji/Library/Developer/Xcode/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz"
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{{
		Label: "DerivedData", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 1 << 30,
		Items: []disk.ItemOutcome{{Path: long, Size: 1 << 30, Outcome: disk.OutcomePlanned}},
	}}}
	v := &DoctorView{Del: DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(DoctorRenderOpts{Width: 77, Page: 24}), "\n")
	if !strings.Contains(out, "ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz") {
		t.Errorf("末尾 (世代を見分ける識別子) が落ちている:\n%s", out)
	}
	if strings.Contains(out, "/Users/koji/Library/Developer/Xcode/DerivedData/Thumb") {
		t.Errorf("先頭から出ている = 末尾を切っている:\n%s", out)
	}
}

// 一覧の対象パス行も同じ規律 (Space で選ぶ対象そのものなので、どれか分からないと選べない)。
func TestDiskItemRowKeepsPathTail(t *testing.T) {
	long := "/Users/koji/Library/Developer/Xcode/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz"
	r := disk.Result{Entry: disk.Entry{ID: "dd", Label: "DerivedData", Risk: disk.RiskSafe},
		Status: disk.StatusOK, Items: []disk.Item{{Path: long, Size: 1 << 30}}}
	v := &DoctorView{}
	rows := v.DiskItemRows(DoctorRenderOpts{Width: 77, Page: 24}, r)
	if len(rows) != 1 {
		t.Fatalf("行数 = %d", len(rows))
	}
	if !strings.Contains(rows[0].Text, "ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz") {
		t.Errorf("末尾が落ちている: %q", rows[0].Text)
	}
	// 🚨 **この層で予算に収めていること**まで見る。行が予算を超えたまま返ると、後段の lines() が
	// 行末を切るので末尾が落ちる (この assert が無いと「切らずに返す」変異が緑のまま通った。実測)
	if w := term.DispWidth(rows[0].Text); w > 77-term.CursorGutterWidth {
		t.Errorf("行が予算を超えている: 幅 %d > %d: %q", w, 77-term.CursorGutterWidth, rows[0].Text)
	}
	// 🚨 コピーは**切っていない実パス**であること (表示の都合で壊さない)
	if rows[0].CopyPath != long {
		t.Errorf("copyPath が切られている: %q", rows[0].CopyPath)
	}
}

// 中断キーの表記の組み立ては、壊れた入力でも panic しない (issue 244 の敵対レビュー)。
//
// 🚨 deleteAbortKeysWord は `var … = func(){…}()` なので**パッケージ init で評価される**。
// ここで panic すると、削除パネルを開くまでもなく glogx が一切起動しない。
func TestAbortKeysWordSurvivesBrokenKeyNames(t *testing.T) {
	for _, keys := range [][]string{{""}, {"ctrl+"}, {"+c"}, {"ctrl+c", ""}, {}} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("keys=%q で panic した: %v", keys, r)
				}
			}()
			_ = abortKeysWord(keys)
		}()
	}
	// 正常な入力では今の表記のままであること (壊れた入力への手当てで表記を変えていない)
	if got := abortKeysWord([]string{"ctrl+c", "ctrl+g"}); got != "Ctrl-C / Ctrl-G" {
		t.Errorf("abortKeysWord = %q", got)
	}
}

// 相の口は「最新だけ残す」。溢れたときに古い相が残ると、経過秒がその古いエントリに
// 紐づいて伸び続け、「16 番目の走査が 97 秒続いている」という**具体的な嘘**になる。
func TestProgressSlotKeepsLatest(t *testing.T) {
	prog := make(chan DoctorDeleteEvent, 1)
	mk := func(i int, label string) DoctorDeleteEvent {
		return DoctorDeleteEvent{Prog: &DoctorProgress{I: i, Total: 40, Label: label, Known: true}}
	}
	for i := 1; i <= 20; i++ { // 読み手が居ないあいだに 20 件流す
		sendLatestProgress(prog, mk(i, "fast"))
	}
	sendLatestProgress(prog, mk(40, "HEAVY"))
	select {
	case ev := <-prog:
		if ev.Prog.I != 40 || ev.Prog.Label != "HEAVY" {
			t.Errorf("古い相が残った: %d/%s (最新は 40/HEAVY)", ev.Prog.I, ev.Prog.Label)
		}
	default:
		t.Fatal("相が 1 件も残っていない")
	}
}

// 選択が空のとき、hint の経路 (selectionSummary -> selectedResults) が Item を 1 つも
// 舐めないことを確かめる (issue 274)。
//
// 🚨 判定に**確保回数も時間も使えない**。実測 (2026-09-06):
//   - diskItemKey の結果は map の添字にしか使われずエスケープしないので、6,400 Item を
//     舐めても **0 allocs**。AllocsPerRun での assert は恒真になる (最初にそう書いて踏んだ)
//   - 時間は壁時計依存なので合否に使わない (avoid-wall-clock-assertions)
//
// 使えるのは「早期 return したか」を区別する観測 = **戻り値が nil か**。
// ループ側は `make([]disk.Result, 0, n)` で始まるので、舐めた場合は必ず**非 nil の空**を返す。
//
// 参考の実測 (合否には使わない): 6,400 Item で 108.8µs/呼び出し -> 3ns、実機規模 29 Item で
// 0.60µs -> 3ns。hint は毎フレームここを通る。
func TestDoctorSelectedResultsSkipsWalkWhenNothingSelected(t *testing.T) {
	v := &DoctorView{Shown: true, Expanded: map[RowKey]bool{}}
	res := make([]disk.Result, 0, 8)
	for e := range 8 {
		items := make([]disk.Item, 0, 200)
		for i := range 200 {
			items = append(items, disk.Item{Path: fmt.Sprintf("/c/e%02d/i%05d", e, i), Size: int64(i + 1)})
		}
		res = append(res, disk.Result{
			Entry:  disk.Entry{ID: fmt.Sprintf("e%02d", e), Label: "L", Risk: disk.RiskSafe},
			Status: disk.StatusOK, Size: 200, Items: items,
		})
	}
	v.DiskRep = &disk.Report{Results: res}

	if len(v.Selected) != 0 || len(v.SelectedItems) != 0 {
		t.Fatal("前提が崩れている (選択が空でない)")
	}
	if got := v.SelectedResults(); got != nil {
		t.Errorf("選択が空なのに nil でない (len=%d)。全 Item を舐めている = "+
			"selectedResults の早期 return が外れている (issue 274)", len(got))
	}
	if n, total := v.SelectionSummary(); n != 0 || total != 0 {
		t.Errorf("選択ゼロなのに summary が %d 件 / %d B", n, total)
	}

	// 陽性対照: 選択があるときは走査して拾う (上の assert が「常に nil」ではないことの担保)
	v.SelectedItems = map[itemKey]bool{diskItemKey("e00", "/c/e00/i00000"): true}
	got := v.SelectedResults()
	if len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("選択が 1 件のとき selectedResults が %d 件 (走査が死んでいる)", len(got))
	}
	if n, _ := v.SelectionSummary(); n != 1 {
		t.Fatalf("選択が 1 件のとき summary が %d 件", n)
	}
}

// 🚨 進捗と完了が同時に ready でも、完了より先に進捗・コマンドの出力を返す。
// select は ready なものを無作為に選ぶので、出し切らずに完了を返すと残りを誰も読まない
// (TestDoctorDeleteRunsAndShowsResult が 1000 回に 8 回「進捗が 1 件も届いていない」で落ちた原因)。
// 200 回とも先に進捗が出ることを見る (壊れていれば毎回 1/2 で完了が先に出る)。
func TestAwaitDeleteEventDrainsBeforeDone(t *testing.T) {
	for i := range 200 {
		ch := make(chan DoctorDeleteEvent, 16)
		prog := make(chan DoctorDeleteEvent, 1)
		done := make(chan DoctorDeleteEvent, 1)
		rep := disk.DeleteReport{}
		prog <- DoctorDeleteEvent{Prog: &DoctorProgress{I: 1, Total: 1, Known: true}}
		ch <- DoctorDeleteEvent{Cmd: &disk.CommandRecord{Name: "brew"}}
		done <- DoctorDeleteEvent{Rep: &rep}
		var order []string
		for range 3 {
			switch ev := awaitDeleteEvent(ch, prog, done); {
			case ev.Cmd != nil:
				order = append(order, "cmd")
			case ev.Prog != nil:
				order = append(order, "prog")
			case ev.Rep != nil:
				order = append(order, "done")
			}
		}
		if order[2] != "done" {
			t.Fatalf("%d 回目: 完了が出力・進捗より先に出た: %v", i, order)
		}
		if len(done) != 0 {
			t.Fatalf("%d 回目: 完了が done に残った", i)
		}
	}
}
