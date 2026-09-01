package usage

import (
	"slices"
	"strings"
	"testing"

	"glogx/termwidth"
)

// AA は bannerRows 行で、各行の幅が bannerWidth と一致する (幅がずれると中央寄せが崩れる)。
func TestBannerLinesShape(t *testing.T) {
	for _, name := range []string{"Claude Code", "codex", "A"} {
		got := bannerLines(name)
		if len(got) != bannerRows {
			t.Fatalf("%q: %d 行 (want %d)", name, len(got), bannerRows)
		}
		for i, ln := range got {
			if w := termwidth.Of(ln); w != bannerWidth(name) {
				t.Errorf("%q の %d 行目: 幅 %d, want %d (%q)", name, i, w, bannerWidth(name), ln)
			}
		}
	}
}

// 収録外の字が 1 つでもあれば nil (呼び出し側が素のテキスト見出しへ落ちる)。欠けた字を
// 空白で埋めて出すと「見出しが壊れている」ようにしか見えない。
func TestBannerLinesRejectsUnknownRunes(t *testing.T) {
	for _, name := range []string{"Gemini", "claude-3", "", "コーデックス"} {
		if got := bannerLines(name); got != nil {
			t.Errorf("%q が AA になった: %q", name, got)
		}
	}
}

// 大文字小文字は問わない (CLI 名は "Claude Code" / "codex" と大小が混在する)。
func TestBannerLinesFoldsCase(t *testing.T) {
	if a, b := bannerLines("codex"), bannerLines("CODEX"); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Error("大文字小文字で AA が変わった")
	}
}

// 段の見出しは CLI 名の AA + バージョン。高さが足りないときだけ 1 行のテキストへ落ちる
// (盤を潰してまで見出しを大きくしない)。
func TestRenderDashboardBanner(t *testing.T) {
	tall := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false), "\n")
	for _, want := range append(bannerLines("Claude Code"), bannerLines("codex")...) {
		if !strings.Contains(tall, want) {
			t.Errorf("AA の行が出ていない: %q", want)
		}
	}
	short := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 20, false), "\n")
	if strings.Contains(short, bannerLines("codex")[0]) {
		t.Error("高さが足りないのに AA を出した (盤が潰れる)")
	}
	if !strings.Contains(short, "codex") {
		t.Error("AA を落としたのにテキストの見出しも無い")
	}
}

// バージョンは AA の脇に添える (取得できていないときは何も出さない)。
func TestRenderDashboardBannerVersion(t *testing.T) {
	all := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false), "\n")
	for _, want := range []string{"v2.1.216", "v0.144.6"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q が出ていない", want)
		}
	}
	bare := dialTestSnap()
	bare.Version, bare.CodexVersion = "", ""
	none := strings.Join(RenderDashboard(bare, dialTestNow(), 120, 44, false), "\n")
	if strings.Contains(none, " v") {
		t.Error("バージョン未取得なのに v を出した")
	}
}

// 半ブロックの詰め方: 上下とも点灯 = █ / 上だけ = ▀ / 下だけ = ▄ / 消灯 = 空白。
// 5 段目の下 (6 段目) は常に空なので、最下行は必ず ▀ か空白になる。
func TestPackPixelsHalfBlocks(t *testing.T) {
	got := packPixels([pixelRows]string{"##", "##", "# ", " #", "  "}, 2)
	want := [bannerRows]string{"██", "▀▄", "  "}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 盤の中央の数字は 4 桁幅の字形。3 桁だと 0 と 8、5 と 6 の見分けが付かない
// (ユーザー指摘 2026-09-01: 潰れて見えない)。字形そのものをここで固定する。
func TestDigitLinesGlyphs(t *testing.T) {
	cases := map[string][]string{
		"0": {"█▀▀█", "█  █", "▀▀▀▀"},
		"6": {"█▀▀▀", "█▀▀█", "▀▀▀▀"},
		"8": {"█▀▀█", "█▀▀█", "▀▀▀▀"},
		"9": {"█▀▀█", "▀▀▀█", "▀▀▀▀"},
	}
	for d, want := range cases {
		got := digitLines(d)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("digitLines(%q) = %q, want %q", d, got, want)
		}
	}
	// 0 と 8 は中段で、5 と 6 は下段側で分かれる (どちらも同じにならないこと)。
	for _, pair := range [][2]string{{"0", "8"}, {"5", "6"}, {"6", "8"}} {
		if strings.Join(digitLines(pair[0]), "|") == strings.Join(digitLines(pair[1]), "|") {
			t.Errorf("%s と %s の字形が同じ", pair[0], pair[1])
		}
	}
}

func TestDigitLinesShapeAndRejects(t *testing.T) {
	for _, s := range []string{"0", "62", "100"} {
		got := digitLines(s)
		if len(got) != bannerRows {
			t.Fatalf("%q: %d 行", s, len(got))
		}
		for i, ln := range got {
			if w := termwidth.Of(ln); w != digitWidth(s) {
				t.Errorf("%q の %d 行目: 幅 %d, want %d", s, i, w, digitWidth(s))
			}
		}
	}
	for _, s := range []string{"", "6a", "六"} {
		if got := digitLines(s); got != nil {
			t.Errorf("%q が AA になった: %q", s, got)
		}
	}
}

// 盤に入るなら 4 桁幅を使う。3 桁幅へ落ちるのは 4 桁が入らない盤だけ。
func TestRenderDashboardPrefersWideDigits(t *testing.T) {
	face := ""
	for _, ln := range RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false) {
		if slices.ContainsFunc([]rune(ln), isBrailleRune) {
			face += ln + "\n"
		}
	}
	for _, want := range digitLines("62") {
		if !strings.Contains(face, want) {
			t.Errorf("4 桁幅の字形が出ていない (%q):\n%s", want, face)
		}
	}
	// 普通の字の "62%" が盤に残っていたら AA が効いていない。
	if strings.Contains(face, "62%") {
		t.Errorf("普通の字のまま:\n%s", face)
	}
}

// バージョンは AA の右へ後付けするので、幅の予算に入れてから中央寄せしないとその行だけ
// width をはみ出す。入らない幅ではバージョンを落とす (見出しごと消さない)。
func TestRenderDashboardBannerVersionFitsWidth(t *testing.T) {
	for w := 1; w <= 130; w++ {
		for _, ln := range RenderDashboard(dialTestSnap(), dialTestNow(), w, 40, false) {
			if got := termwidth.Of(ln); got > w {
				t.Errorf("w=%d: 幅 %d の行が出た: %q", w, got, ln)
			}
		}
	}
	// AA は入るがバージョンまでは入らない幅では、AA が残ってバージョンだけ**丸ごと**消える。
	// ⚠️ ここは groupHead を直接呼ぶ。RenderDashboard 経由だと、その幅では 1 カラムに
	// なって「AA を出すと盤が潰れる」判定が先に効き、見出しごとテキストへ落ちるため
	// バージョンの落とし方を観測できない。
	// ⚠️ 幅を 1 点だけ見ない。最後の砦の切り詰めが効くと "  v2.1" のような途中で切れた版が
	// 残るが、切れる位置は幅次第で、たまたま "v" の手前で切れる幅を選ぶと素通りする
	// (実測 2026-09-01: bw+2 では予算に入れない変異が green のままだった)。
	g := dialGroup{cli: "Claude Code", version: "2.1.216"}
	bw := bannerWidth(g.cli)
	for w := bw; w <= bw+16; w++ {
		joined := strings.Join(groupHead(g, w, 40, false), "\n")
		if strings.Contains(joined, "v") && !strings.Contains(joined, "v2.1.216") {
			t.Errorf("w=%d: バージョンが途中で切れている (丸ごと落とすべき):\n%s", w, joined)
		}
		if !strings.Contains(joined, bannerLines(g.cli)[0]) {
			t.Errorf("w=%d: AA ごと消えている:\n%s", w, joined)
		}
	}
	// 入る幅では出る (落とす条件が広すぎないこと)。
	if wide := strings.Join(groupHead(g, bw+20, 40, false), "\n"); !strings.Contains(wide, "v2.1.216") {
		t.Errorf("入る幅なのにバージョンが出ていない:\n%s", wide)
	}
}

// バージョンは外部コマンドの出力なので、載せる前に無害化する (制御列を盤へ通さない)。
func TestRenderDashboardBannerVersionSanitized(t *testing.T) {
	snap := dialTestSnap()
	snap.Version = "\x1b[2J\x1b[H2.1.216"
	all := strings.Join(RenderDashboard(snap, dialTestNow(), 160, 44, false), "\n")
	if strings.Contains(all, "\x1b[2J") || strings.Contains(all, "[2J") {
		t.Errorf("制御列が素通りした:\n%q", all)
	}
	if !strings.Contains(all, "v2.1.216") {
		t.Error("無害化でバージョンごと消えた")
	}
}
