package main

import (
	"regexp"
	"strings"
	"testing"
)

// reapplyAfterResetRe は置き換える前の実装 (正規表現版)。**テスト専用**で、
// 手書き走査が旧実装と完全に同じ結果を返すことを差分で確かめるために残す。
// production から regexp を外したのは毎フレームの確保を削るためで、挙動は変えていない
// という主張を、この 2 実装の一致で機械的に固定する。
var reapplyAfterResetRe = regexp.MustCompile("\x1b\\[0?m")

// ⚠️ 旧 production は置換テンプレートに `"$0"+bg` を使っていたが、ここでは
// `"${0}" + $ をエスケープした bg` を使う。bg を置換テンプレートへ埋める形は bg 自身が
// テンプレートとして解釈されるためで、差分 fuzz が実際に 2 通りの化け方を見つけた:
//
//   - bg="0" → `$00` という別の変数参照になり、旧実装が "" を返す
//   - bg="$0" → bg 内の `$0` がマッチ全体に展開され、旧実装が "\x1b[m\x1b[m" を返す
//
// production の bg は必ず SGR 定数 (ansiCursorBg = "\x1b[48;5;24m") で `$` も英数字始まりも
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
		// ⚠️ 入れ子の ESC。走査の刻み幅 (i += 2) が load-bearing であることを固定する。
		// 「CSI の終端まで飛ばす」という一見自然な最適化に変えると、この入力でリセットを
		// 取りこぼす (R1 レビューの指摘。それまでの表 22 件では 1 件も捕まえられていなかった)
		"\x1b[\x1b[m",
		"\x1b[\x1b[0m",
		"a\x1b[\x1b[m b",
	}
	for _, bg := range []string{ansiCursorBg, "\x1b[48;5;238m", "", "\x1b[7m"} {
		for _, s := range cases {
			got := reapplyAfterReset(s, bg)
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
		f.Add(s, ansiCursorBg) // production が実際に渡す唯一の値
	}
	// fuzz が見つけた「bg が置換テンプレートとして解釈される」2 例を seed に固定する
	// (reapplyAfterResetOld の doc)。production の bg では起きないが、参照実装の
	// エスケープを外すと再発するので種として残す
	f.Add("\x1b[m", "0")
	f.Add("\x1b[m", "$0")
	f.Add("\x1b[0m\x1b[m", "$$1")
	f.Fuzz(func(t *testing.T, s, bg string) {
		if got, want := reapplyAfterReset(s, bg), reapplyAfterResetOld(s, bg); got != want {
			t.Fatalf("text=%q bg=%q\n got=%q\nwant=%q", s, bg, got, want)
		}
	})
}

// フレーム 1 枚の確保回数の上限。issue 047 で「1 行が 1 フレームで 4 回コピーされる」うち
// 最外周の余白付け (旧 wrapWindowFrame の `" "+l`) を組み立てへ織り込み、可視行ぶんの
// 確保を落とした。実測 178 → 135 / 40,144 → 30,745 B。内訳は **indent が 38・カーソル行の
// regexp 撤去が 5** (片方だけ戻して実測。43 は 2 つの変更の合計であって indent 単独ではない)。
//
// ⚠️ 上限は実測値の**すぐ上**に置く。緩い上限は退行を通す: 当初 list を 150 にしていたら
// 「38 行のうち 15 行を旧形に戻す」(削減の 35%) と「regexp 撤去だけを revert する」(+5) の
// どちらも素通りした (2026-08-14 の R3 レビューで実測)。余裕は -race の揺れ (下記) の分だけ。
//
// ⚠️ -race で値が変わるので、上限は **-race 側の実測**から採る。実測
// (darwin/arm64・GOMAXPROCS=14・-race・-count=10 の分布):
//
//	list / list-ja  135 (10/10)          → 上限 138
//	status-40       318 (10/10)          → 上限 322
//	diff-overlay    212 x1 / 213 x9      → 上限 217
//	job-panel       156 x3 / 157 x6 / 158 x1 → 上限 162
//
// overlay 系は -race で ±2 揺れるので余裕を少し広く採っている (素の実測は
// diff-overlay 211 / job-panel 153)。`make test` は -race 付きなのでそちらが本番のゲート。
// CI (linux/amd64) の -race も 2026-08-14 に docker (golang:1.25) で実測し、darwin と
// 同じ水準だった (list/list-ja 135 / status-40 317 / diff-overlay 213 / job-panel 157)。
// 上限を上げるときは「実測がいくらだったか」を必ずここに書き足すこと (黙って緩めない)。
//
// バイト側の上限を回数と**別に**持つ理由 (issue 051): `AllocsPerRun` は確保の回数しか
// 数えないので、「1 回の確保が大きくなる」形の退行を捕まえられない。実際 046 では allocs が
// 完全一致のまま B/op だけ +0.8〜1.0% 増えており、048 では回数版のガードが
// **メモ化を丸ごと revert しても PASS** した (R1 レビューが実証)。
//
// ⚠️ バイトの上限も -race 側の実測から採る (`make test` は -race 付き)。実測
// (darwin/arm64・GOMAXPROCS=14・-race・4 回の最大値):
//
//	list 30776 / list-ja 31488 / status-40 43047 / diff-overlay 47654 / job-panel 36366 B
//
// 回数と違い、バイトは -race でほぼ水増しされない (素の list は 30744 B = +0.1%)。上限は
// 実測の +3%: 4 回のばらつきは 0.03%、環境 (OS/arch/Go patch/GOMAXPROCS) を跨いでも 0.15%
// 以内だったため (測定条件は tests/glogx/bench_budgets.ci に記載)。
//
// CI 側の同じ主張は tests/glogx/bench_budgets.ci の *_alloc_kb が -race 無しで持つ
// (二重管理だが役割が違う: ここは `make test` 一発で回る -race 付きの門番、あちらは
// 経時比較つきで render_large_patch / model_init_200 など本テストが持たない経路も見る)。
// 確保が増える変更を意図して入れるときは**両方**を同じ commit で更新すること。
//
// ⚠️ 回数と同じく、この上限は `RUNEWIDTH_EASTASIAN=1` の環境では落ちる (issue 054)。実測で
// list が 135 回 30776 B → 322 回 41139 B になる (幅計算が変わり行の作り直しが増えるため)。
// その環境で赤くなったら、退行ではなく env 前提の方を疑うこと。
func TestFrameAllocBudget(t *testing.T) {
	// diff overlay / job パネルを含める理由: これらは buildShadowPanelBox を通る経路で、
	// 一覧と status だけでは **その経路がテストの視界に入らない**。実際「buildShadowPanelBox の
	// 返り値を全行作り直す」純粋な perf 退行が、一覧 + status の fixture では検出できなかった
	// (R3 レビューで実測)。
	cases := []struct {
		name   string
		build  func(testing.TB) *browseModel
		allocs int
		bytes  int64
	}{
		{"list", func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 20, 120, 40, false) }, 138, 31700},
		{"list-ja", func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 20, 120, 40, true) }, 138, 32500},
		{"status-40", func(tb testing.TB) *browseModel { return benchStatusBrowse(tb, 40, 120, 40) }, 322, 44400},
		{"diff-overlay", budgetDiffModel, 217, 49100},
		{"job-panel", budgetPanelModel, 162, 37500},
		// issues viewer / usage グランス / toast (issue 062)。それまで viewLines の全画面ビュー
		// 2 つのうち issues 側と、起動直後の実フレーム (usage グランス表示) がどのゲートの
		// 視界にも入っていなかった。実測 (darwin/arm64・GOMAXPROCS=14・-race・-count=10):
		// issues-40 209 (10/10) 33848〜33849 B / usage-glance 176 (10/10) 35624 B /
		// toast-holding 182 (10/10) 36864 B。回数の余裕は既存ケースと同じ +4、バイトは +3%
		{"issues-40", func(tb testing.TB) *browseModel { return benchIssuesBrowse(tb, 40, 120, 40) }, 213, 34900},
		{"usage-glance", budgetUsageGlanceModel, 180, 36700},
		{"toast-holding", budgetToastModel, 186, 38000},
	}
	for _, c := range cases {
		m := c.build(t)
		_ = m.View().Content // 遅延初期化を計測から外す
		got := int(testing.AllocsPerRun(50, func() { _ = m.View().Content }))
		if got > c.allocs {
			t.Errorf("%s: 1 フレームの確保が %d 回 (上限 %d)。行の作り直しが増えていないか",
				c.name, got, c.allocs)
		}
		gotBytes := frameAllocBytes(t, c.build)
		if gotBytes > c.bytes {
			t.Errorf("%s: 1 フレームの確保が %d B (上限 %d B)。回数は同じでも 1 本あたりが"+
				"太っていないか (バッファの作り直し・過大な pre-grow)", c.name, gotBytes, c.bytes)
		}
		t.Logf("%s: %d allocs/frame (上限 %d) / %d B/frame (上限 %d)",
			c.name, got, c.allocs, gotBytes, c.bytes)
	}
}

// frameAllocBytes は 1 フレームの確保バイト数を測る。`testing.AllocsPerRun` には B/op に
// 相当する API が無いので `testing.Benchmark` の AllocedBytesPerOp を使う (issue 048 の
// TestStatusFrameAllocBytesDoNotScaleWithFileCount と同じ形)。
//
// ⚠️ `testing.Benchmark` は同じプロセスの `-benchtime` を拾う。`go test -bench=. -benchtime=1x`
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
// visible で起動する (tui.go の usageOverlay{visible: true}) のに、bench fixture は
// それを消していた = アプリ起動直後のフレームがどのゲートの外だった (issue 062)。
// データ未着 (loading) の箱を重ねた状態で測る。
func budgetUsageGlanceModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.usageOv.visible = true
	return m
}

// budgetToastModel はトースト全幅表示 (holding) 中のフレーム (issue 062)。
// entering/leaving はアニメの一過性フレームなので測らず (issue 062 の方針)、静止する
// holding を advance で作ってから測る。
func budgetToastModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.toast.show("3 件の警告をコピーしました: ほか", true)
	for m.toast.animating() {
		m.toast.advance(m.colored)
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
	m.diffOv.sha = sha
	m.diffOv.cache.store(sha, lines, sha)
	m.diffOv.offset = 50
	return m
}

func budgetPanelModel(tb testing.TB) *browseModel {
	m := benchBrowseSubjects(tb, 20, 120, 40, false)
	m.panelSHA = m.commits[3].SHA
	m.details[m.panelSHA] = []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "lint", State: StateFailure, URL: "https://github.com/o/r/runs/2"},
	}
	return m
}

// indent が「余白を織り込むだけ」であることを、**全行の完全一致**で固定する。
//
// ⚠️ 「行頭が空白か」で見てはいけない: 下端の影行は shadowBottomOffset = 2 桁の空白で
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
			// 265 は buildPanelBoxImpl が padSpaces の n>256 fallback へ到達する最小幅
			// (258 では pad が 250 で届かない。R3 レビューで実測)
			for _, width := range []int{0, 1, 9, 10, 11, 40, 80, 200, 258, 265, 300} {
				for _, indent := range []int{0, 1, 2, 5} {
					st := panelBoxStyle{glyphs: borderDouble, color: ansiFrameBorder}
					base := buildPanelBoxImpl("", content, width, colored, st)
					st.indent = indent
					got := buildPanelBoxImpl("", content, width, colored, st)
					if len(got) != len(base) {
						t.Fatalf("indent=%d で行数が変わった: %d != %d (w=%d colored=%v)",
							indent, len(got), len(base), width, colored)
					}
					// ⚠️ 相対比較だけでは「両辺に同じ定数を足す」変異 (pre を
					// padSpaces(indent+1) にする等) がキャンセルして素通りする (R3 の指摘)。
					// indent=0 のときの**絶対**の姿を 1 点固定して基準を釘付けする:
					// 色なしの上辺は罫線の角で始まり、空白では始まらない。
					if indent == 0 && !colored && width >= minPanelWidth && len(base) > 0 {
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

// wrapWindowFrame の幾何 (行数・左余白 1 桁・端末幅に収まること)。
func TestWrapWindowFrameGeometry(t *testing.T) {
	content := []string{"aaa", "bbb", "ccc"}
	for _, termW := range []int{20, 40, 80, 200} {
		got := wrapWindowFrame(content, termW, false)
		if len(got) != len(content)+4 {
			t.Fatalf("termW=%d: 行数が %d (want %d = content+4)", termW, len(got), len(content)+4)
		}
		if got[0] != "" {
			t.Errorf("termW=%d: 先頭は上余白の空行のはず: %q", termW, got[0])
		}
		for i, l := range got[1:] {
			// ⚠️ HasPrefix(" ") では見ない: 下端の影行は 2 桁の空白で始まるので
			// 余白を落としても素通りする (R1 が false green の原因と特定した判定形)。
			// 「行頭の空白がちょうど 1 桁 + 影のオフセット分」を厳密に数える
			lead := len(l) - len(strings.TrimLeft(l, " "))
			wantLead := 1
			if i == len(got)-2 { // 下端の影行だけ余白 + shadowBottomOffset
				wantLead = 1 + shadowBottomOffset
			}
			if lead != wantLead {
				t.Errorf("termW=%d 行 %d の行頭空白が %d 桁 (want %d): %q",
					termW, i+1, lead, wantLead, l)
			}
			if w := dispWidth(l); w > termW {
				t.Errorf("termW=%d 行 %d が端末幅を超えた: %d (%q)", termW, i+1, w, l)
			}
		}
	}
}

// padSpaces が strings.Repeat(" ", n) と同値であること (n<=0 は "")。
//
// ⚠️ 256 桁を跨いで確かめる: padSpaces は 256 桁までは定数文字列のスライスを返し、
// それを超えると strings.Repeat に落ちる。047 で strings.Repeat の呼び出しを
// padSpaces へ置き換えた根拠が「同値だが無確保」なので、fallback 側の同値を
// 守るテストが無いと主張が裏付けられない (R3 の指摘。それまで直接テストは 0 本だった)。
func TestPadSpacesEquivalentToRepeat(t *testing.T) {
	for n := -3; n <= 300; n++ {
		want := ""
		if n > 0 {
			want = strings.Repeat(" ", n)
		}
		if got := padSpaces(n); got != want {
			t.Fatalf("padSpaces(%d) = %q (len %d); want len %d", n, got, len(got), len(want))
		}
	}
	// 256 桁以下は確保しない (定数文字列のスライス) ことも固定する
	if n := testing.AllocsPerRun(50, func() { _ = padSpaces(200) }); n != 0 {
		t.Errorf("padSpaces(200) が %v 回確保している (バッキング共有が壊れた)", n)
	}
}
