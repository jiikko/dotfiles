package usage

import (
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
	snap := dialTestSnap()
	snap.Version, snap.CodexVersion = "2.1.216", "0.144.6"
	all := strings.Join(RenderDashboard(snap, dialTestNow(), 120, 44, false), "\n")
	for _, want := range []string{"v2.1.216", "v0.144.6"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q が出ていない", want)
		}
	}
	none := strings.Join(RenderDashboard(dialTestSnap(), dialTestNow(), 120, 44, false), "\n")
	if strings.Contains(none, " v") {
		t.Error("バージョン未取得なのに v を出した")
	}
}
