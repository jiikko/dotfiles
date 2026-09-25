package toast

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"tuikit/termwidth"

	"github.com/charmbracelet/x/ansi"
)

// show → entering、tick で右画面外から左へ滑り込み (shown 0→boxWidth)、入場完了で holding +
// 退場タイマー、startLeaving → leaving、tick で右へ滑り出て hidden、という一連の状態遷移。
func TestToastLifecycle(t *testing.T) {
	var to Stack
	if to.Visible() || to.Animating() {
		t.Fatal("初期は非表示・非アニメ")
	}
	to.Show("done", true)
	if !to.Visible() || to.phase != Entering || !to.ok || to.text != "done" {
		t.Fatalf("show 後: phase=%d visible=%v ok=%v text=%q", to.phase, to.Visible(), to.ok, to.text)
	}
	boxW := to.boxWidth(false)
	if boxW < 10 {
		t.Fatalf("箱幅が不足: %d", boxW)
	}
	// 入場: holding になるまで advance。退場タイマー (holdCmd) は入場完了時に 1 回だけ返る
	var holdCmds, guard int
	for to.phase == Entering && guard < 100 {
		if ts := to.Advance(false); len(ts) > 0 {
			holdCmds++
		}
		guard++
	}
	if to.phase != Holding || to.shown != boxW {
		t.Fatalf("入場完了後: phase=%d shown=%d (want holding/%d)", to.phase, to.shown, boxW)
	}
	if holdCmds != 1 {
		t.Errorf("退場タイマーは入場完了時に 1 回だけ返るべき: %d", holdCmds)
	}
	if to.Animating() {
		t.Error("holding 中は animating=false (tick 不要)")
	}
	// holding 明け → leaving
	to.StartLeaving(Msg{seq: to.seq})
	if to.phase != Leaving || !to.Animating() {
		t.Fatalf("startLeaving 後: phase=%d animating=%v", to.phase, to.Animating())
	}
	// 退場: hidden まで
	guard = 0
	for to.Visible() && guard < 100 {
		to.Advance(false)
		guard++
	}
	if to.phase != Hidden || to.Visible() || to.text != "" {
		t.Fatalf("退場完了後: phase=%d visible=%v text=%q", to.phase, to.Visible(), to.text)
	}
}

// advanceToHolding は entering のトーストを holding まで tick で進める (テスト用ヘルパー)。
func advanceToHolding(to *Stack) {
	for guard := 0; to.phase == Entering && guard < 100; guard++ {
		to.Advance(false)
	}
}

// 連続 push/pull: 前のトーストの退場タイマー (古い seq) は後のトーストを leaving にしない。
// 新トーストを holding まで進めた状態で試すことで、phase 条件では弾けず seq ガードだけが
// 分岐を左右する場面を作る (startLeaving の `msg.seq == t.seq` を消すと最初の assert が落ちる)。
func TestToastStaleTimerDoesNotLeaveNewer(t *testing.T) {
	var to Stack
	to.Show("first", true)
	oldSeq := to.seq
	advanceToHolding(&to)    // 1つ目を holding へ
	to.Show("second", false) // 上書き (seq 前進・entering へリセット)
	advanceToHolding(&to)    // 2つ目も holding へ = phase==holding は満たされ、残る守りは seq のみ

	// 古い世代のタイマー (oldSeq) が届いても、seq 不一致なので新トーストを退場させない。
	to.StartLeaving(Msg{seq: oldSeq})
	if to.phase != Holding || to.text != "second" {
		t.Errorf("古い seq のタイマーが新トーストを退場させた: phase=%d text=%q", to.phase, to.text)
	}
	// 正しい世代のタイマーなら退場に入る (seq ガードは一致時に通す、の対検証)。
	to.StartLeaving(Msg{seq: to.seq})
	if to.phase != Leaving {
		t.Errorf("正しい seq で退場に入らない: phase=%d", to.phase)
	}
}

// 入場途中は箱の左 shown カラムだけ (可視幅=shown、全幅未満)、holding で全幅が出る。横スライド。
func TestToastBoxLinesRevealsLeftColumns(t *testing.T) {
	var to Stack
	to.Show("pushed", true) // ASCII のみ (全角境界の半端幅を避け、可視幅=shown を厳密比較)
	boxW := to.boxWidth(false)
	full := to.fullBox(false)
	// 入場 1 フレーム: 全行が出るが、各行の可視幅は shown (<boxW) に切られている
	to.Advance(false)
	got := to.boxLines(false) // 1 枚分の描画を見る (スタックの行数上限とは別)
	if len(got) != len(full) {
		t.Errorf("スライド中も全行が出るべき: got=%d 行 want=%d 行", len(got), len(full))
	}
	wv := termwidth.Of(got[0])
	if wv != to.shown || wv >= boxW {
		t.Errorf("入場途中の可視幅が左スライドでない: 可視幅=%d shown=%d boxW=%d", wv, to.shown, boxW)
	}
	// holding まで進めると全幅 + ✓/text
	advanceToHolding(&to)
	lines := to.boxLines(false)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if termwidth.Of(lines[0]) != boxW || !strings.Contains(plain, "✓") || !strings.Contains(plain, "pushed") {
		t.Errorf("全表示に ✓/pushed が無い / 全幅でない:\n%s", plain)
	}
	// 失敗は ✗
	var ng Stack
	ng.Show("failed", false)
	advanceToHolding(&ng)
	if !strings.Contains(ansi.Strip(strings.Join(ng.boxLines(false), "\n")), "✗") {
		t.Error("失敗トーストに ✗ が無い")
	}
	// 非表示は nil
	var empty Stack
	if empty.boxLines(false) != nil {
		t.Error("非表示で nil を返さない")
	}
}

// 新しい通知は上に積まれ、古い通知は下から抜けていく (ユーザー要望 2026-07-31)。
func TestToastStacksNewestOnTop(t *testing.T) {
	var s Stack
	s.Show("1 番目", true)
	s.Show("2 番目", true)
	s.Show("3 番目", true)

	items := s.items()
	if len(items) != 3 {
		t.Fatalf("枚数 = %d, want 3", len(items))
	}
	// items は上から下。最新が上、最古が下
	for i, want := range []string{"3 番目", "2 番目", "1 番目"} {
		if items[i].text != want {
			t.Errorf("上から %d 枚目 = %q, want %q", i+1, items[i].text, want)
		}
	}
	// 埋め込みは常に最新を指す (呼び出し側とテストが t.text で読む前提)
	if s.text != "3 番目" {
		t.Errorf("埋め込みが最新でない: %q", s.text)
	}
}

// 上限を超えたら一番古い (一番下) を捨てる。画面を覆わないための guard。
func TestToastStackCapsOldest(t *testing.T) {
	var s Stack
	for _, txt := range []string{"1", "2", "3", "4", "5"} {
		s.Show(txt, true)
	}
	items := s.items()
	if len(items) != StackMax {
		t.Fatalf("枚数 = %d, want %d (上限)", len(items), StackMax)
	}
	if items[0].text != "5" {
		t.Errorf("最上段 = %q, want 5 (最新)", items[0].text)
	}
	for _, it := range items {
		if it.text == "1" || it.text == "2" {
			t.Errorf("古い通知が残っている: %q", it.text)
		}
	}
}

// 抜けた枚は取り除かれ、残りが繰り上がる (下から抜けていく)。
func TestToastStackRemovesFinishedFromBottom(t *testing.T) {
	var s Stack
	s.Show("古い", true)
	s.Show("新しい", true)
	// 入場を終わらせる
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	// 古い方 (下) の静止が明けて退場 → 抜け切るまで進める
	oldSeq := s.older[0].seq
	s.StartLeaving(Msg{seq: oldSeq})
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	if len(s.older) != 0 {
		t.Errorf("抜けた枚が残っている: %+v", s.older)
	}
	if s.text != "新しい" || !s.Visible() {
		t.Errorf("残るべき枚が消えた: text=%q visible=%v", s.text, s.Visible())
	}
}

// 退場タイマーは枚ごとに独立 (seq が一致した枚だけ動く)。
func TestToastStackLeavingIsPerItem(t *testing.T) {
	var s Stack
	s.Show("古い", true)
	s.Show("新しい", true)
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	s.StartLeaving(Msg{seq: s.older[0].seq})
	if s.older[0].phase != Leaving {
		t.Errorf("下の枚が退場に入らない: %v", s.older[0].phase)
	}
	if s.phase == Leaving {
		t.Error("関係ない上の枚まで退場に入った (seq の取り違え)")
	}
}

// 描画は上から下に並ぶ (新しいものが上)。
func TestToastStackBoxLinesOrder(t *testing.T) {
	var s Stack
	s.Show("古い通知", true)
	s.Show("新しい通知", true)
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	out := strings.Join(s.BoxLines(false, 100), "\n")
	iNew, iOld := strings.Index(out, "新しい通知"), strings.Index(out, "古い通知")
	if iNew < 0 || iOld < 0 {
		t.Fatalf("両方描かれていない:\n%s", out)
	}
	if iNew > iOld {
		t.Errorf("新しい通知が下に来ている (上に積むはず):\n%s", out)
	}
}

// 進行中トースト (…シアン) は新しい通知が来たら退く。🚨 積んだままにすると「PR を検索中...」の
// 下に結果が並び、終わったのに検索中と書いてある状態が数秒残る (実測 2026-07-31)。
func TestToastInfoIsSupersededByResult(t *testing.T) {
	var s Stack
	s.ShowInfo("PR を検索中...")
	s.Show("PR #123 を開きます", true)
	items := s.items()
	if len(items) != 1 {
		t.Fatalf("枚数 = %d, want 1 (進行中は退く): %+v", len(items), items)
	}
	if items[0].text != "PR #123 を開きます" {
		t.Errorf("残ったのが結果でない: %q", items[0].text)
	}
	// 下に積まれていた進行中も落ちる
	var s2 Stack
	s2.Show("先行の結果", true)
	s2.ShowInfo("検索中...")
	s2.Show("新しい結果", true)
	for _, it := range s2.items() {
		if it.info {
			t.Errorf("進行中が残っている: %q", it.text)
		}
	}
	if len(s2.items()) != 2 {
		t.Errorf("枚数 = %d, want 2 (結果 2 枚)", len(s2.items()))
	}
}

// 箱の行数は一定 (上罫線 + 内容 + 下罫線 + 影)。行数上限の計算がこれに依存している。
func TestToastBoxLineCount(t *testing.T) {
	var s Stack
	s.Show("x", true)
	advanceToHolding(&s)
	if got := len(s.boxLines(false)); got != BoxHeight {
		t.Errorf("箱の行数 = %d, want %d (BoxHeight を直すこと)", got, BoxHeight)
	}
}

func TestToastBoxLinesShowsTwoWarningsWithEightLineBudget(t *testing.T) {
	var s Stack
	s.Show("警告A", false)
	s.Show("警告B", false)
	for range SlideFrames + 2 {
		s.Advance(false)
	}

	out := strings.Join(s.BoxLines(false, BoxHeight*2), "\n")
	if !strings.Contains(out, "警告A") || !strings.Contains(out, "警告B") {
		t.Fatalf("予算 8 行で重要警告 2 枚が描かれない:\n%s", out)
	}
}

func TestToastBoxLinesDropsOldestWarningWhenThreeDoNotFit(t *testing.T) {
	var s Stack
	for _, text := range []string{"警告A", "警告B", "警告C"} {
		s.Show(text, false)
	}
	for range SlideFrames + 2 {
		s.Advance(false)
	}

	out := strings.Join(s.BoxLines(false, BoxHeight*2), "\n")
	if strings.Contains(out, "警告A") {
		t.Fatalf("警告 3 枚で最古が表示対象に残っている:\n%s", out)
	}
	if !strings.Contains(out, "警告B") || !strings.Contains(out, "警告C") {
		t.Fatalf("新しい警告 2 枚が表示されていない:\n%s", out)
	}
}

// 行数上限を超える古い枚は出さない。箱の途中で切らない。最新は上限を超えても出す。
func TestToastBoxLinesRespectsMaxLines(t *testing.T) {
	var s Stack
	for _, txt := range []string{"1", "2", "3"} {
		s.Show(txt, true)
	}
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	for _, c := range []struct {
		maxLines  int
		wantBoxes int
	}{
		{100, 3},
		{BoxHeight * 2, 2},
		{BoxHeight, 1},
		{1, 1}, // 上限より箱が大きくても最新 1 枚は出す (見えない通知より覆う通知)
	} {
		got := len(s.BoxLines(false, c.maxLines))
		if want := c.wantBoxes * BoxHeight; got != want {
			t.Errorf("maxLines=%d: %d 行 (%d 箱), want %d 行 (%d 箱)",
				c.maxLines, got, got/BoxHeight, want, c.wantBoxes)
		}
	}
}

// 溢れたときの追い出しは重要度を見る。🚨 年齢だけで捨てると、起動時の警告の後に成功通知が
// 3 回来ただけで警告が消える (実測 2026-08-13。issue 028 P2 が要求していた
// 「重要度 error > info の逆転を防ぐ」が、スタック化後は満たされていなかった)。
func TestToastEvictionKeepsWarningOverSuccess(t *testing.T) {
	var s Stack
	s.Show("警告: 未 push があります", false) // ok=false = 警告/拒否理由
	for i := 1; i <= 3; i++ {
		s.Show(fmt.Sprintf("ok%d", i), true) // 成功通知で埋める
	}

	items := s.items()
	texts := make([]string, 0, len(items))
	for _, it := range items {
		texts = append(texts, it.text)
	}
	if !slices.Contains(texts, "警告: 未 push があります") {
		t.Errorf("成功通知に押し出されて警告が消えた: %v", texts)
	}
	// 同じ重要度どうしは従来どおり年齢順 (後勝ち) — 最古の成功が捨てられる
	if slices.Contains(texts, "ok1") {
		t.Errorf("最古の成功通知が残っている (重要でない最古を捨てていない): %v", texts)
	}
	if len(s.items()) != StackMax {
		t.Errorf("保持枚数が上限と違う: %d want %d", len(s.items()), StackMax)
	}
}

// 全部が警告なら従来どおり最古を捨てる (重要度が同じなら年齢順)。
func TestToastEvictionFallsBackToAgeWhenAllWarnings(t *testing.T) {
	var s Stack
	for i := 1; i <= StackMax+1; i++ {
		s.Show(fmt.Sprintf("warn%d", i), false)
	}
	items := s.items()
	texts := make([]string, 0, len(items))
	for _, it := range items {
		texts = append(texts, it.text)
	}
	if slices.Contains(texts, "warn1") {
		t.Errorf("全部警告のとき最古が捨てられていない: %v", texts)
	}
	if !slices.Contains(texts, fmt.Sprintf("warn%d", StackMax+1)) {
		t.Errorf("最新の警告が残っていない: %v", texts)
	}
}

// 描画予算に入らないときも、落とす順は追い出しと同じ規則にする。
// 🚨 保持と表示で規則が違うと「保持はしているのに重要な通知だけ画面に出ない」状態になる
// (実測 2026-08-13: 警告の後に成功通知が来た狭い端末で、警告が 1 行も描かれなかった)。
func TestToastBoxLinesKeepsWarningWithinBudget(t *testing.T) {
	var s Stack
	s.Show("警告: 未 push があります", false) // 最古 = 予算で最初に落ちる位置
	s.Show("ok1", true)
	s.Show("ok2", true)
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	if len(s.items()) != 3 {
		t.Fatalf("前提が崩れた: 3 枚保持していない (%d)", len(s.items()))
	}

	// 2 枚ぶんの予算しかない = 1 枚落とす。落ちるのは重要でない最古 (ok1) で、警告は残る
	out := strings.Join(s.BoxLines(false, BoxHeight*2), "\n")
	if !strings.Contains(out, "未 push") {
		t.Errorf("予算内に警告が描かれない (保持しているのに見えない):\n%s", out)
	}
	if strings.Contains(out, "ok1") {
		t.Errorf("重要でない最古 (ok1) が残っている = 落とす順が追い出しと違う:\n%s", out)
	}
	if !strings.Contains(out, "ok2") {
		t.Errorf("最新が描かれない:\n%s", out)
	}
	// 🚨 残す枚の並びは元のまま (上が新しい)。重要な枚を上へ繰り上げない
	if strings.Index(out, "ok2") > strings.Index(out, "未 push") {
		t.Errorf("並び順が入れ替わっている (上が新しいという読み方が崩れる):\n%s", out)
	}

	// 1 枚ぶんの予算では最新 1 枚だけ (既存の不変条件: 見えない通知より覆う通知)。
	// 🚨 行数だけの assert にしない: 最新が落ちて古い警告が残っても行数は同じ 4 行で通る。
	// 実際この fixture は issue 057 のバグ (最新 ok2 が消え「未 push」だけが描かれる) を
	// 再現していたのに、行数 assert だったため green を返し続けていた
	one := strings.Join(s.BoxLines(false, BoxHeight), "\n")
	if got := len(s.BoxLines(false, BoxHeight)); got != BoxHeight {
		t.Errorf("予算 1 枚のとき %d 行 (want %d)", got, BoxHeight)
	}
	if !strings.Contains(one, "ok2") {
		t.Errorf("予算 1 枚のとき最新 (ok2) が描かれない (issue 057):\n%s", one)
	}
}

// 最新のトーストは重要度に関係なく必ず描かれる (issue 057 の回帰テスト)。
// 「最新が成功/進行中・残りが警告」の組み合わせで、狭い端末の予算 (fit=1 / fit=2) でも
// 最新のテキストが出力に含まれることを**中身で** assert する (行数 assert はこの穴を通す)。
func TestToastBoxLinesAlwaysDrawsNewest(t *testing.T) {
	cases := []struct {
		name     string
		maxLines int
		build    func(*Stack)
		want     string
	}{
		{"fit=1: 警告 1 枚 + 成功", BoxHeight*2 - 1, func(s *Stack) {
			s.Show("git push に失敗しました", false)
			s.Show("コピーしました", true)
		}, "コピーしました"},
		{"fit=2: 警告 2 枚 + 成功", BoxHeight*3 - 2, func(s *Stack) {
			s.Show("警告A", false)
			s.Show("警告B", false)
			s.Show("コピーしました", true)
		}, "コピーしました"},
		{"fit=2: 警告 2 枚 + 進行中 (info)", BoxHeight*3 - 2, func(s *Stack) {
			s.Show("警告A", false)
			s.Show("警告B", false)
			s.ShowInfo("PR を検索中...")
		}, "PR を検索中"},
	}
	for _, c := range cases {
		var s Stack
		c.build(&s)
		for range SlideFrames + 2 {
			s.Advance(false)
		}
		got := strings.Join(s.BoxLines(false, c.maxLines), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: 最新のトースト %q が 1 行も描かれない:\n%s", c.name, c.want, got)
		}
	}
}

// Clear は積んでいた通知を即座に消す。🚨 世代は巻き戻さない: 消す前の枚の退場タイマーが、消した後に積んだ枚を退場させない。
func TestClearKeepsGenerationAndShadow(t *testing.T) {
	s := Stack{Shadow: "\x1b[38;5;232m"}
	s.Show("消す前", true)
	var timers []Timer
	for range SlideFrames + 2 {
		timers = append(timers, s.Advance(false)...)
	}
	if len(timers) != 1 {
		t.Fatalf("前提: 静止に入った枚の退場タイマーが 1 つ: %d", len(timers))
	}
	s.Clear()
	if s.Visible() || len(s.Entries()) != 0 {
		t.Fatalf("Clear の後も通知が残る: %+v", s.Entries())
	}
	if s.Shadow == "" {
		t.Fatal("Clear が影の色まで消した")
	}
	s.Show("消した後", true)
	for range SlideFrames + 2 {
		s.Advance(false)
	}
	s.StartLeaving(timers[0].Msg) // 消す前の枚のタイマーが届く
	if s.Phase() != Holding || s.Text() != "消した後" {
		t.Fatalf("消す前の枚のタイマーが、消した後の枚を退場させた: phase=%d text=%q", s.Phase(), s.Text())
	}
}

// Entries は表示中の枚を上から (新しい順に) 返す。
func TestEntriesNewestFirst(t *testing.T) {
	var s Stack
	s.Show("A", true)
	s.Show("B", false)
	s.ShowInfo("C")
	got := s.Entries()
	if len(got) != 3 || got[0] != (Entry{Text: "C", Info: true}) || got[1] != (Entry{Text: "B"}) || got[2] != (Entry{Text: "A", OK: true}) {
		t.Fatalf("Entries: %+v", got)
	}
}
