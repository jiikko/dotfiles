package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/layout"

	"doctor/disk"
	"doctor/docker"
	"doctor/svc"
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/gitdata"
	"glogx/internal/term"
	"glogx/internal/testenv"

	"testwait"
)

// reapplyAfterResetRe は置き換える前の実装 (正規表現版)。**テスト専用**で、
// 手書き走査がこの実装と完全に同じ結果を返すことを差分で確かめるために残す。
// production から regexp を外したのは毎フレームの確保を削るためで、挙動は変えていない
// という主張を、この 2 実装の一致で機械的に固定する。
var reapplyAfterResetRe = regexp.MustCompile("\x1b\\[0?m")

// 🚨 旧 production は置換テンプレートに `"$0"+bg` を使っていたが、ここでは
// `"${0}" + $ をエスケープした bg` を使う。bg を置換テンプレートへ埋める形は bg 自身が
// テンプレートとして解釈されるためで、差分 fuzz が実際に 2 通りの化け方を見つけた:
//
//   - bg="0" → `$00` という別の変数参照になり、regexp 版が "" を返す
//   - bg="$0" → bg 内の `$0` がマッチ全体に展開され、regexp 版が "\x1b[m\x1b[m" を返す
//
// production の bg は必ず SGR 定数 (term.AnsiCursorBg = "\x1b[48;5;24m") で `$` も英数字始まりも
// 含まないため、この 2 つは**実運用では起こらない**。旧挙動の忠実な参照であることは変わらず、
// 曖昧さを消すことで bg を振った fuzz が「実装の差」だけを検出できるようになる。
func reapplyAfterResetOld(text, bg string) string {
	return reapplyAfterResetRe.ReplaceAllString(text, "${0}"+strings.ReplaceAll(bg, "$", "$$"))
}

func TestReapplyAfterResetMatchesRegexpImpl(t *testing.T) {
	cases := []string{
		"",
		"plain",
		"\x1b[m",
		"\x1b[0m",
		"\x1b[m\x1b[m",
		"\x1b[0m\x1b[m\x1b[0m",
		"a\x1b[mb",
		"a\x1b[0mb",
		"\x1b[33mcommit abc\x1b[m",
		"\x1b[38;5;214mcolored\x1b[m tail",
		"\x1b[1m\x1b[31mx\x1b[0my\x1b[mz",
		"\x1b[00m",      // 0 が 2 個 = リセットではない (旧実装も一致しない)
		"\x1b[1m",       // リセットでない SGR
		"\x1b[2K",       // 非 SGR CSI
		"\x1b",          // ESC 単体
		"\x1b[",         // 途中で切れた
		"\x1b[0",        // 途中で切れた
		"\x1b[0mx\x1b[", // 末尾が途中で切れた
		"no esc at all", // 早期 return 経路
		"\x1b]0;t\x07",  // OSC
		"日本語\x1b[m混在",   // マルチバイト
		"\x1b[m\x1b[1m\x1b[m",
		// 🚨 入れ子の ESC。走査の刻み幅 (i += 2) が load-bearing であることを固定する。
		// 「CSI の終端まで飛ばす」という一見自然な最適化に変えると、この入力でリセットを
		// 取りこぼす (表の他の 22 件では 1 件も捕まえられない)
		"\x1b[\x1b[m",
		"\x1b[\x1b[0m",
		"a\x1b[\x1b[m b",
	}
	for _, bg := range []string{term.AnsiCursorBg, "\x1b[48;5;238m", "", "\x1b[7m"} {
		for _, s := range cases {
			got := term.ReapplyAfterReset(s, bg)
			want := reapplyAfterResetOld(s, bg)
			if got != want {
				t.Errorf("bg=%q text=%q\n got=%q\nwant=%q", bg, s, got, want)
			}
		}
	}
}

// 差分 fuzz: 手書き走査は旧 regexp 実装と常に一致しなければならない。
//
//	go test -run '^$' -fuzz FuzzReapplyAfterReset -fuzztime=30s .
func FuzzReapplyAfterReset(f *testing.F) {
	for _, s := range []string{"", "a", "\x1b[m", "\x1b[0m", "\x1b[1mx\x1b[m",
		"\x1b[00m", "\x1b[", "\x1b", "\x1b[2K", "日\x1b[m"} {
		f.Add(s, "\x1b[7m")
		f.Add(s, term.AnsiCursorBg) // production が実際に渡す唯一の値
	}
	// fuzz が見つけた「bg が置換テンプレートとして解釈される」2 例を seed に固定する
	// (reapplyAfterResetOld の doc)。production の bg では起きないが、参照実装の
	// エスケープを外すと再発するので種として残す
	f.Add("\x1b[m", "0")
	f.Add("\x1b[m", "$0")
	f.Add("\x1b[0m\x1b[m", "$$1")
	f.Fuzz(func(t *testing.T, s, bg string) {
		if got, want := term.ReapplyAfterReset(s, bg), reapplyAfterResetOld(s, bg); got != want {
			t.Fatalf("text=%q bg=%q\n got=%q\nwant=%q", s, bg, got, want)
		}
	})
}

// フレーム 1 枚の確保回数の上限。「1 行が 1 フレームで 4 回コピーされる」うち
// 最外周の余白付け (term.WrapWindowFrame の `" "+l` 相当) を組み立てへ織り込み、可視行ぶんの
// 確保を落とした。実測 178 → 135 / 40,144 → 30,745 B。内訳は **indent が 38・カーソル行の
// regexp 撤去が 5** (片方だけ戻して実測。43 は 2 つの変更の合計であって indent 単独ではない)。
//
// 🚨 上限は実測値の**すぐ上**に置く。緩い上限は退行を通す: 当初 list を 150 にしていたら
// 「38 行のうち 15 行を旧形に戻す」(削減の 35%) と「regexp 撤去だけを revert する」(+5) の
// どちらも素通りした (2026-08-14 の R3 レビューで実測)。
//
// 🚨 **回数は -race なしの run で判定する** (2026-10-03)。-race の計装は描画と無関係の確保を呼ぶたびに違う数だけ
// 足すので、同じモデルを続けて測っても doctor-svc は 450〜460 と揺れた (-race なしは 15 回とも 380)。5 回の最小値を
// 採っても 577〜583 (doctor-disk) と揺れが残り、上限を揺れの上に置くと退行を通し、下に置くと `make test` の負荷の下で
// 偽の赤になる (doctor-svc 459 / 上限 458 で落ちた)。そこで Makefile の test-budget で -race なしでこのテストだけを
// 回し、回数はその run でだけ判定する (手元の make test は回さず CI でだけ回す。手元の GOCACHE の増え方のため)。バイトは -race 付きの run でだけ判定する (benchmark で 1 ケース約 1 秒かかり、
// 両方で回すと make test が 16 秒延びるため。バイトは -race でほぼ揺れない)。どちらの run かは raceEnabled (build tag)。
// 上限は -race なしの実測 + 3 (-race なしは 3 回とも全ケースで完全に一致した)。
// 上限を上げるときは「実測がいくらだったか」を必ずここに書き足すこと (黙って緩めない)。
//
// バイト側の上限を回数と**別に**持つ理由: `AllocsPerRun` は確保の回数しか
// 数えないので、「1 回の確保が大きくなる」形の退行を捕まえられない。実際 046 では allocs が
// 完全一致のまま B/op だけ +0.8〜1.0% 増えており、048 では回数版のガードが
// **メモ化を丸ごと revert しても PASS** した (R1 レビューが実証)。
//
// 🚨 バイトの上限は -race 側の実測から採る (-race 付きの run で見るため)。
// 回数と違い、バイトは -race でほぼ揺れない (4 回のばらつきは 0.03%、環境を跨いでも 0.15% 以内)。上限は実測の +3%
// (測定条件は tests/glogx/bench_budgets.ci に記載)。
//
// CI 側の同じ主張は tests/glogx/bench_budgets.ci の *_alloc_kb が -race 無しで持つ
// (二重管理だが役割が違う: ここは `make test` 一発で回る門番、あちらは
// 経時比較つきで render_large_patch / model_init_200 など本テストが持たない経路も見る)。
// 確保が増える変更を意図して入れるときは**両方**を同じ commit で更新すること。
//
// 🚨 この上限は `RUNEWIDTH_EASTASIAN=1` の環境では落ちる。実測で
// list が 135 回 30776 B → 322 回 41139 B になる (幅計算が変わり行の作り直しが増えるため)。
// その環境で赤くなったら、退行ではなく env 前提の方を疑うこと。
//
// 🚨 **上限だけでなく「現在値」も書く**。上限だけだと「どれだけ悪化したら red か」が読めない。
// 2026-10-03 の実測 (darwin/arm64・GOMAXPROCS=14・120x40。回数は **-race なし**・3 回とも同じ値、バイトは -race):
//
//	ケース          回数の現在値  上限   バイト現在値 (-race)  上限
//	list            127         130    30048                31700
//	list-ja         127         130    30768                32500
//	status-40       279         282    42366                44400
//	diff-overlay    209         212    47129                49100
//	job-panel       151         154    35649                37500
//	issues-40       210         213    33880                34900
//	usage-glance    153         156    32600                36700
//	toast-holding   167         170    35712                38000
//	ratelimit-dash  175         178    82024                84900
//	doctor-disk     476         479    63733                65200
//	doctor-svc      380         383    65954                67500
//	doctor-brew     179         182    37921                38700
//	doctor-docker   486         489    56495                57900
//
// doctor の 4 ケースは 2026-09-29 の SSD タブ でタブが 1 つ増え、-race なしで +5 (svc 375 → 380 / disk 471 → 476)。
//
// 🚨 **「どれだけ悪化したら red になるか」を実測する**。doctor の `lines()` に
// 「表示行ごとに 1 確保」を足す変異 (太る退行の最小の形) が、doctor の 4 ケースとも red になること。
// 2026-10-03 (上の上限で) の結果は commit message に残した。上限を動かすときは、この変異を当て直して red を確認すること。
func TestFrameAllocBudget(t *testing.T) {
	// diff overlay / job パネルを含める理由: これらは term.BuildShadowPanelBox を通る経路で、
	// 一覧と status だけでは **その経路がテストの視界に入らない**。実際「term.BuildShadowPanelBox の
	// 返り値を全行作り直す」純粋な perf 退行が、一覧 + status の fixture では検出できなかった
	// (R3 レビューで実測)。
	cases := []struct {
		name   string
		build  func(testing.TB) *browseModel
		allocs int
		bytes  int64
	}{
		{"list", func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 20, 120, 40, false) }, 130, 31700},
		{"list-ja", func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 20, 120, 40, true) }, 130, 32500},
		{"status-40", func(tb testing.TB) *browseModel { return benchStatusBrowse(tb, 40, 120, 40) }, 282, 44400},
		{"diff-overlay", budgetDiffModel, 212, 49100},
		{"job-panel", budgetPanelModel, 154, 37500},
		// issues viewer / usage グランス / toast。それまで viewLines の全画面ビュー
		// 2 つのうち issues 側と、起動直後の実フレーム (usage グランス表示) がどのゲートの
		// 視界にも入っていなかった。上限の取り方と現在値は上の doc の表
		{"issues-40", func(tb testing.TB) *browseModel { return benchIssuesBrowse(tb, 40, 120, 40) }, 213, 34900},
		{"usage-glance", budgetUsageGlanceModel, 156, 36700},
		{"toast-holding", budgetToastModel, 170, 38000},
		// 全画面ビューア 4 枚のうち doctor と ratelimit は、予算の枠組み (047 / 051 / 062) より
		// 後に足されたためどのゲートの視界にも入っていなかった。
		//
		// ratelimit-dash: メモ化前 1065 allocs / 239319 B → **メモ化後 175 / 82241**。
		// 🚨 時計を固定した fixture での値。実時計のままだと
		// 測定窓が秒境界をまたいだときだけ 1 回キャッシュミスして平均が 193 まで跳ね、
		// ~4%/run で flake した (145 窓で実測)。
		// 上限は必ず**今の実測**へ締め直すこと — 締めずに残すと「6 倍悪化しても緑」になり、
		// 「観測できない予算」を新設したことになる。
		// doctor-disk: 270 の遅延化 (畳まれた行の detail を組まない) で -race の値が 1506 → 601 に落ちたので締め直した。
		// 締めずに残すと「2.5 倍悪化しても緑」になる。
		{"ratelimit-dash", budgetRatelimitModel, 178, 84900},
		// 🚨 doctorMaxMarkWidth のメモ化の後に**下げた**上限。
		// バイトはメモ化前 83100〜83184 B → 後 63152〜63249 B (-race) で **-19,950 B (-24%)**。
		// 緩いまま残すと「メモ化を revert しても緑」= 改善を守らない予算になる。
		{"doctor-disk", budgetDoctorModel, 479, 65200},
		// doctor の残り 3 タブ (disk だけがゲートに載っていると、残りが見えない)。
		{"doctor-svc", budgetDoctorSvcModel, 383, 67500},
		{"doctor-brew", budgetDoctorBrewModel, 182, 38700},
		{"doctor-docker", budgetDoctorDockerModel, 489, 57900},
		// treefiler (F)。格子を毎フレーム作り直していたときは 330KB/frame だった。使い回して 154 / 43167 B (-race)。
		// 最下行に検索とキー一覧の案内 (/ find  ? keys) を足して 156 / 45601 B。案内を戻すと 43556 B に下がるのを実測した
		// (増えたのは描く文字列そのもので、確保の無駄ではない)。! cmd / s shell の案内を足して 45861 B
		// 158 / 46200 は動きの途中の画面の値だった (fixture が実時計で進めていて止まりきらない。budgetFilerModel の注記)。
		// 止まった画面で 220 / 50799 B (-race)。線の格子のセルを値で持ち map を使い回して 124 回 (-race なし) / 39790 B (-race)
		{"filer", budgetFilerModel, 126, 40500},
	}
	for _, c := range cases {
		m := c.build(t)
		_ = m.View().Content // 遅延初期化を計測から外す

		// 🚨 下界を先に見る。上界だけだと「何も描かないフレーム」が最良に見え、
		// 描画が丸ごと死んだ状態で 8 ケース全部が 0 allocs / 0 B を出して緑になる
		// (実測: View を tea.NewView("") にする 1 行の変異で全ケース PASS)。
		// 下界を**確保回数**で置かないのは、正当な最適化で確保が減ったときに落とさないため。
		// 見るのは「そのフレームが実際に描いたか」= alt screen を埋めたか。
		assertFrameDrawn(t, c.name, m)

		// 回数は -race なしの run (Makefile の test-budget) で、バイトは -race 付きの run (test) で判定する (上の doc)
		if !raceEnabled {
			got := int(testing.AllocsPerRun(50, func() { _ = m.View().Content }))
			if got > c.allocs {
				t.Errorf("%s: 1 フレームの確保が %d 回 (上限 %d)。行の作り直しが増えていないか",
					c.name, got, c.allocs)
			}
			t.Logf("%s: %d allocs/frame (上限 %d)", c.name, got, c.allocs)
			continue
		}
		gotBytes := frameAllocBytes(t, c.build)
		if gotBytes > c.bytes {
			t.Errorf("%s: 1 フレームの確保が %d B (上限 %d B)。回数は同じでも 1 本あたりが"+
				"太っていないか (バッファの作り直し・過大な pre-grow)", c.name, gotBytes, c.bytes)
		}
		t.Logf("%s: %d B/frame (上限 %d)", c.name, gotBytes, c.bytes)
	}
}

// frameAllocBytes は 1 フレームの確保バイト数を測る。`testing.AllocsPerRun` には B/op に
// 相当する API が無いので `testing.Benchmark` の AllocedBytesPerOp を使う
// (TestStatusFrameAllocBytesDoNotScaleWithFileCount と同じ形)。
//
// 🚨 `testing.Benchmark` は同じプロセスの `-benchtime` を拾う。`go test -bench=. -benchtime=1x`
// のように短く回すと b.N=1 の測定 (= 遅延初期化込みの 1 発目) が返り、絶対値の予算と
// 比べる意味が無くなる。**測れなかったときに緑を返さない**ため、反復が足りなければ落とす。
func frameAllocBytes(t *testing.T, build func(testing.TB) *browseModel) int64 {
	t.Helper()
	const minIters = 100
	r := testing.Benchmark(func(b *testing.B) {
		m := build(b)
		_ = m.View().Content // 遅延初期化を計測から外す
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = m.View().Content
		}
	})
	if r.N < minIters {
		t.Fatalf("フレームの確保バイトを測れていない (反復 %d 回 < %d)。-benchtime を短くしたまま"+
			"test を回していないか (既定の 1s なら数千回回る)", r.N, minIters)
	}
	return r.AllocedBytesPerOp()
}

// budgetUsageGlanceModel は起動直後の実フレーム: newBrowseModel は usage グランスを
// visible で起動する (tui.go の newBrowseModel の usageview.NewUsageOverlay()) のに、bench fixture は
// それを消していた = アプリ起動直後のフレームがどのゲートの外だった。
// データ未着 (loading) の箱を重ねた状態で測る。
func budgetUsageGlanceModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.usageOv.Fixture().SetVisible(true)
	return m
}

// budgetToastModel はトースト全幅表示 (holding) 中のフレーム。
// entering/leaving はアニメの一過性フレームなので測らず、静止する
// holding を advance で作ってから測る。
func budgetToastModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.toast.Show("3 件の警告をコピーしました: ほか", true)
	for m.toast.Animating() {
		m.toast.Advance()
	}
	return m
}

// budgetFilerModel は treefiler を開いた画面。木は 20 件のファイルと 3 つのフォルダ (1 つは開いて 10 件)。
func budgetFilerModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	dir := tb.TempDir()
	for i := range 20 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.go", i)), []byte("x\n"), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	for _, d := range []string{"alpha", "beta", "gamma"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			tb.Fatal(err)
		}
		for i := range 10 {
			if err := os.WriteFile(filepath.Join(dir, d, fmt.Sprintf("n%02d.md", i)), []byte("y\n"), 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}
	// 全部を commit した印の無い repo にする。git の印を描くかで確保が変わり (repo の外 209 / 外側の repo の untracked 245)、
	// 木を作る場所 (TMPDIR) がどこかの repo の中かをテストが決めていなかった。treefiler は .git を上へ辿って repo を探すので
	// GIT_CEILING_DIRECTORIES では外側の repo を切れない。利用者の global / system の設定ファイルは、裏で起こす git status にも読ませない
	// (既定の excludesfile ~/.config/git/ignore は読まれる)
	tb.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	tb.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitIn(tb, dir, "init", "-q", "-b", "main")
	gitIn(tb, dir, "add", ".")
	gitIn(tb, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
	m.filerV.Toggle(dir)
	m.filerV.HandleKey("l") // alpha を開いて中へ
	// 止まった画面を測る: 固定の時計を 50ms ずつ進め、動きと裏の処理 (走査・git) が止まるまで回す。
	// 🚨 実時計 (app.TimeNow) を渡さない: Advance は 1 回の経過を 0.05 秒で頭打ちにするので、毎回「今 + 1 秒」を渡すと 2 回目以降は
	// 呼び出しの間の実時間しか進まず、途中の画面を測る。CI の遅さで進み方が変わり、確保が 156 → 164 回と揺れた (2745215c の CI)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	testwait.Until(tb, "treefiler の動き・裏の処理が止まる", func() bool { return !m.filerV.Animating() && !m.filerV.Busy() },
		testwait.Step(func() { at = at.Add(50 * time.Millisecond); m.filerV.Advance(at) }), testwait.Tick(time.Millisecond))
	// git の取り込みが失敗すると印の無い軽い画面を測って緑になる (assertFrameDrawn は描いたかしか見ない)
	if v := term.StripANSI(m.View().Content); !strings.Contains(v, "⎇ main") {
		tb.Fatalf("前提: treefiler が repo のブランチを描いていない:\n%s", v)
	}
	return m
}

func budgetDiffModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	sha := m.commits[0].SHA
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "+ added line with some diff content"
	}
	(*m.diffOv.Fixture().Sha()) = sha
	m.diffOv.Fixture().Cache().Store(sha, lines, sha)
	m.diffOv.Fixture().Pager().Offset = 50
	return m
}

func budgetPanelModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.panelSHA = m.commits[3].SHA
	m.details[m.panelSHA] = []gitdata.CheckDetail{
		{Name: "build", State: gitdata.StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: gitdata.StateFailure, URL: "https://github.com/o/r/runs/2"},
	}
	return m
}

// indent が「余白を織り込むだけ」であることを、**全行の完全一致**で固定する。
//
// 🚨 「行頭が空白か」で見てはいけない: 下端の影行は 影の横ずれ (2 桁)の空白で
// 始まるため、indent を落としても行頭は空白のままで prefix 判定を素通りする。
// 実際その形の変異 (影行だけ pre を落とす) は出力を 880 行ぶん変えるのに、
// リポジトリ全体のテストが green だった (2026-08-14 の R1 レビューで検出)。
// そこで「indent=N の結果 == indent=0 の結果の全行に N 桁の空白を付けたもの」を主張する。
// これは上辺 / content / 下辺 / 下端の影の 4 行種すべてを同時に守る。
func TestPanelBoxIndentIsPureLeftPad(t *testing.T) {
	contents := [][]string{
		{"aaa", "bbb", "ccc"},
		{},
		{""},
		{"\x1b[32mcolored\x1b[m", "日本語の行", strings.Repeat("x", 400)},
	}
	for _, colored := range []bool{false, true} {
		for _, content := range contents {
			// 265 は term.BuildPanelBoxImpl が term.PadSpaces の n>256 fallback へ到達する最小幅
			// (258 では pad が 250 で届かない。R3 レビューで実測)
			for _, width := range []int{0, 1, 9, 10, 11, 40, 80, 200, 258, 265, 300} {
				for _, indent := range []int{0, 1, 2, 5} {
					st := layout.PanelStyle{Border: layout.BorderDouble, Color: term.AnsiFrameBorder}
					base := term.BuildPanelBoxImpl("", content, width, colored, st)
					st.Indent = indent
					got := term.BuildPanelBoxImpl("", content, width, colored, st)
					if len(got) != len(base) {
						t.Fatalf("indent=%d で行数が変わった: %d != %d (w=%d colored=%v)",
							indent, len(got), len(base), width, colored)
					}
					// 🚨 相対比較だけでは「両辺に同じ定数を足す」変異 (pre を
					// term.PadSpaces(indent+1) にする等) がキャンセルして素通りする (R3 の指摘)。
					// indent=0 のときの**絶対**の姿を 1 点固定して基準を釘付けする:
					// 色なしの上辺は罫線の角で始まり、空白では始まらない。
					if indent == 0 && !colored && width >= layout.PanelMinWidth && len(base) > 0 {
						if strings.HasPrefix(base[0], " ") {
							t.Fatalf("indent=0 の上辺が空白で始まっている (基準がずれている) w=%d: %q",
								width, base[0])
						}
					}
					pad := strings.Repeat(" ", indent)
					for i := range base {
						if want := pad + base[i]; got[i] != want {
							t.Fatalf("indent=%d w=%d colored=%v 行 %d が余白付けと一致しない\n got=%q\nwant=%q",
								indent, width, colored, i, got[i], want)
						}
					}
				}
			}
		}
	}
}

// term.WrapWindowFrame の幾何 (行数・左余白 1 桁・端末幅に収まること)。
func TestWrapWindowFrameGeometry(t *testing.T) {
	content := []string{"aaa", "bbb", "ccc"}
	for _, termW := range []int{20, 40, 80, 200} {
		got := term.WrapWindowFrame(content, termW, false, "")
		if len(got) != len(content)+4 {
			t.Fatalf("termW=%d: 行数が %d (want %d = content+4)", termW, len(got), len(content)+4)
		}
		if got[0] != "" {
			t.Errorf("termW=%d: 先頭は上余白の空行のはず: %q", termW, got[0])
		}
		for i, l := range got[1:] {
			// 🚨 HasPrefix(" ") では見ない: 下端の影行は 2 桁の空白で始まるので
			// 余白を落としても素通りする (R1 が false green の原因と特定した判定形)。
			// 「行頭の空白がちょうど 1 桁 + 影のオフセット分」を厳密に数える
			lead := len(l) - len(strings.TrimLeft(l, " "))
			wantLead := 1
			if i == len(got)-2 { // 下端の影行だけ余白 + shadowBottomOffset
				wantLead = 1 + 2 // 余白 + 影の横ずれ (layout の shadowBottomOffset)
			}
			if lead != wantLead {
				t.Errorf("termW=%d 行 %d の行頭空白が %d 桁 (want %d): %q",
					termW, i+1, lead, wantLead, l)
			}
			if w := term.DispWidth(l); w > termW {
				t.Errorf("termW=%d 行 %d が端末幅を超えた: %d (%q)", termW, i+1, w, l)
			}
		}
	}
}

// term.PadSpaces が strings.Repeat(" ", n) と同値であること (n<=0 は "")。
//
// 🚨 256 桁を跨いで確かめる: term.PadSpaces は 256 桁までは定数文字列のスライスを返し、
// それを超えると strings.Repeat に落ちる。047 で strings.Repeat の呼び出しを
// term.PadSpaces へ置き換えた根拠が「同値だが無確保」なので、fallback 側の同値を
// 守るテストが無いと主張が裏付けられない (R3 の指摘。それまで直接テストは 0 本だった)。
func TestPadSpacesEquivalentToRepeat(t *testing.T) {
	for n := -3; n <= 300; n++ {
		want := ""
		if n > 0 {
			want = strings.Repeat(" ", n)
		}
		if got := term.PadSpaces(n); got != want {
			t.Fatalf("term.PadSpaces(%d) = %q (len %d); want len %d", n, got, len(got), len(want))
		}
	}
	// 256 桁以下は確保しない (定数文字列のスライス) ことも固定する
	if n := testing.AllocsPerRun(50, func() { _ = term.PadSpaces(200) }); n != 0 {
		t.Errorf("term.PadSpaces(200) が %v 回確保している (バッキング共有が壊れた)", n)
	}
}

// assertFrameDrawn は「そのフレームが画面を埋めた」ことを確かめる (確保予算の下界)。
//
// 全画面 (AltScreen) の TUI なので View は必ず height 行を返す。2026-09-06 実測: 予算 8 ケース
// すべてが 40 行 / 8028〜9606 文字。文字数の下限は 1 行 20 文字相当で置く (空行だけの
// フレームは 40 文字にしかならないので、この下限で落ちる)。
func assertFrameDrawn(t *testing.T, name string, m *browseModel) {
	t.Helper()
	content := m.View().Content
	if lines := strings.Count(content, "\n") + 1; lines != m.height {
		t.Errorf("%s: フレームが %d 行しか描いていない (画面の高さ %d)。"+
			"確保の数字が良く見えていても、描画が死んでいる", name, lines, m.height)
	}
	if minChars := m.height * 20; len(content) < minChars {
		t.Errorf("%s: フレームの文字数が %d しかない (下限 %d)。行はあるが中身が無い",
			name, len(content), minChars)
	}
}

// budgetRatelimitModel は全画面 ratelimit ダッシュボードのフレーム。
func budgetRatelimitModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.rlDash.Fixture().SetShown(true)
	m.usageOv.Fixture().SetSnapshot(testenv.RatelimitTestSnap())
	// 🚨 時計を固定する (メモ化 + avoid-wall-clock-assertions)。
	// 盤のキャッシュ鍵は now.Unix() を含むので、実時計のままだと AllocsPerRun(50) の
	// 測定窓 (-race で ~41ms) が**秒境界をまたいだときだけ 1 回ミス**し、その 1069 allocs が
	// 50 で割られて平均に乗る (実測: 175 → 最大 193。145 窓中 5 回が上限 177 を超えた
	// = 1 実行あたり ~4% で flake する)。固定クロックでは 148 窓すべて 175 だった。
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	tb.Cleanup(func() { app.TimeNow = orig })
	return m
}

// budgetDoctorModelFor は全画面 doctor の実フレーム。
//
// 🚨 **4 タブすべてに実データを積む。** disk タブだけを測ると、
// 残り 3 タブ (svc / brew / docker) はどの予算の視界にも入らない。
// 空の Report を渡したタブを測っても「ほとんど何も描かないフレーム」の値しか出ず、
// そのタブの描画が太っても気づけない。件数は disk と揃えて 8 エントリ規模にしてある。
func budgetDoctorModelFor(tb testing.TB, tab doctorview.DoctorTab) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.doctorOv = doctorview.DoctorView{}
	m.doctorOv.Fixture().Show()

	res := make([]disk.Result, 0, 8)
	for e := range 8 {
		items := make([]disk.Item, 0, 4)
		for i := range 4 {
			items = append(items, disk.Item{Path: fmt.Sprintf("/c/e%02d/i%d", e, i), Size: int64(i + 1)})
		}
		res = append(res, disk.Result{
			Entry:  disk.Entry{ID: fmt.Sprintf("e%02d", e), Label: "Entry", Risk: disk.RiskSafe, Recover: "再取得", DeleteVia: "rm"},
			Status: disk.StatusOK, Size: 4, Items: items,
		})
	}
	diskRep := &disk.Report{Results: res}

	finds := make([]svc.Finding, 0, 8)
	for i := range 8 {
		finds = append(finds, svc.Finding{
			Label:       fmt.Sprintf("com.example.agent%02d", i),
			PlistPath:   fmt.Sprintf("/Library/LaunchAgents/com.example.agent%02d.plist", i),
			Domain:      "gui/501",
			Reasons:     []string{"実行ファイルが無い", "直近の終了コードが非 0"},
			MissingExec: "/opt/example/bin/agent",
			LastExit:    78,
			HasLastExit: true,
			RestartKeys: []string{"KeepAlive"},
		})
	}
	svcRep := &svc.Report{Findings: finds, Scanned: 64}

	warns := make([]string, 0, 8)
	for i := range 8 {
		warns = append(warns, fmt.Sprintf("Warning: unbrewed header files were found in /usr/local/include (%d)", i))
	}

	kinds := []docker.Kind{docker.KindContainers, docker.KindImages, docker.KindBuildCache, docker.KindVolumes}
	groups := make([]docker.Group, 0, len(kinds))
	for _, k := range kinds {
		items := make([]docker.Item, 0, 4)
		for i := range 4 {
			items = append(items, docker.Item{
				Name: fmt.Sprintf("%s-%02d", k, i), Detail: "Exited (0) 3 weeks ago",
				Size: int64(1 << 20 * (i + 1)), SizeText: "1.2GB",
				Age: time.Duration(i+1) * 24 * time.Hour, AgeKnown: true,
				Command: fmt.Sprintf("docker rm %s-%02d", k, i),
			})
		}
		groups = append(groups, docker.Group{
			Kind: k, Label: string(k), Total: 4, TotalSize: 1 << 22, Reclaimable: 1 << 21,
			Items: items, Size: 1 << 22, Command: fmt.Sprintf("docker %s prune -f", k),
		})
	}
	dockerRep := &docker.Report{Installed: true, Groups: groups, SystemPrune: "docker system prune -a -f", OldAfter: 14 * 24 * time.Hour}
	m.doctorOv.Fixture().Reports(tab, diskRep, svcRep, warns, dockerRep, nil)

	return m
}

// タブごとの入口。名前を分けるのは、予算テーブルの行と失敗メッセージでどのタブか分かるようにするため。
func budgetDoctorModel(tb testing.TB) *browseModel {
	return budgetDoctorModelFor(tb, doctorview.TabDisk)
}
func budgetDoctorSvcModel(tb testing.TB) *browseModel {
	return budgetDoctorModelFor(tb, doctorview.TabSvc)
}
func budgetDoctorBrewModel(tb testing.TB) *browseModel {
	return budgetDoctorModelFor(tb, doctorview.TabBrew)
}
func budgetDoctorDockerModel(tb testing.TB) *browseModel {
	return budgetDoctorModelFor(tb, doctorview.TabDocker)
}
