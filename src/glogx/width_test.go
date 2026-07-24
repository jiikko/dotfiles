package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// dispWidth は描画エンジン (Bubble Tea) が使う ansi.StringWidth と同一の幅を返す。
// この一致が崩れると ⚠️ 等で毎秒の再描画のたびに桁がずれてガタつく (2026-07-24 の回帰対象)。
func TestDispWidthMatchesRenderer(t *testing.T) {
	cases := map[string]int{
		"⚠️": 2, "❤️": 2, "✔️": 2, // 記号 + VS16 → emoji presentation (端末=2)
		"🇯🇵": 2, "🎉": 2, "✅": 2, // 国旗・絵文字本体
		"①": 1, "→": 1, "★": 1, // East Asian Ambiguous は locale 非依存で 1
		"日本": 4, "abc": 3,
	}
	for s, want := range cases {
		got := dispWidth(s)
		if got != want {
			t.Errorf("dispWidth(%q) = %d; want %d", s, got, want)
		}
		if eng := ansi.StringWidth(s); got != eng {
			t.Errorf("dispWidth(%q)=%d が描画エンジンの ansi.StringWidth=%d と食い違う", s, got, eng)
		}
	}
}

// wrapToWidth は grapheme クラスタ単位で折り、⚠️ を VS16 の手前で分断しない。
func TestWrapToWidthKeepsEmojiCluster(t *testing.T) {
	for _, seg := range wrapToWidth("⚠️警告⚠️警告", 3) {
		if strings.HasPrefix(seg, "️") || strings.HasSuffix(seg, "⚠") {
			t.Fatalf("⚠️ クラスタが VS16 の境界で分断された: %q", seg)
		}
	}
}

// dropToColumn は全角グリフが cut をまたぐとき空白で列を揃える (overlay 合成の整合)。
// 絵文字クラスタも幅 2 の 1 単位として扱う。
func TestDropToColumnStraddleAndCluster(t *testing.T) {
	// 日(幅2) が列 1 をまたぐ: 日 を落とし 1 空白で列 1 に揃え、残りを継ぐ
	if got := dropToColumn("日本X", 1); got != " 本X" {
		t.Errorf("dropToColumn(\"日本X\", 1) = %q; want %q", got, " 本X")
	}
	// ⚠️(幅2) が列 1 をまたぐ: クラスタごと落として 1 空白 + 残り
	if got := dropToColumn("⚠️x", 1); got != " x" {
		t.Errorf("dropToColumn(\"⚠️x\", 1) = %q; want %q", got, " x")
	}
	// 列 0 は素通り / 内容末尾以降は空
	if dropToColumn("abc", 0) != "abc" || dropToColumn("abc", 10) != "" {
		t.Error("dropToColumn の境界 (n<=0 / n>=幅) が壊れている")
	}
}

// dropEmojiVS16 は VS16 (U+FE0F) を除去し bare 記号 (双方幅 1 で端末と食い違わない) へ倒す。
func TestDropEmojiVS16(t *testing.T) {
	const vs16 = "️"
	got := dropEmojiVS16("危険 ⚠" + vs16 + " 注意")
	if want := "危険 ⚠ 注意"; got != want {
		t.Fatalf("VS16 が除去されない: got %q want %q", got, want)
	}
	// bare 記号は描画エンジンでも幅 1 (端末実測とも一致) — この不変条件が崩れたら再発
	if w := dispWidth("⚠"); w != 1 {
		t.Fatalf("bare ⚠ の dispWidth = %d; want 1", w)
	}
	// VS16 無しは同一文字列を素通り (高速パス)
	if s := "plain ⚠ text"; dropEmojiVS16(s) != s {
		t.Fatalf("VS16 無しで変化した: %q", dropEmojiVS16(s))
	}
	// VS15 (U+FE0E, text 強制) は残す
	if s := "⚠︎"; dropEmojiVS16(s) != s {
		t.Fatalf("VS15 まで除去した: %q", dropEmojiVS16(s))
	}
}

// sanitizeDetailLine (CI 由来テキストの funnel) も VS16 を正規化する。
func TestSanitizeDetailLineDropsVS16(t *testing.T) {
	if got := sanitizeDetailLine("job ⚠️ fail"); got != "job ⚠ fail" {
		t.Fatalf("sanitizeDetailLine が VS16 を残した: %q", got)
	}
}

// overlayCenteredBox: 背景行に ⚠️ が含まれても合成後の各行幅が端末幅と一致する
// (glogx と端末で幅が食い違うとこの行がガタついていた)。
func TestOverlayCompositeWidthWithEmoji(t *testing.T) {
	const width = 40
	// 実際の window 行は View で端末幅に clip 済み。絵文字入りの行を width ちょうどに整える。
	bg := []string{fillRight(clipToWidth(strings.Repeat("⚠️ commit ", 8), width), width)}
	if w := dispWidth(bg[0]); w != width {
		t.Fatalf("前提: bg[0] の幅 = %d; want %d", w, width)
	}
	box := buildPanelBox(" x ", []string{"body"}, 20, false)
	out := overlayCenteredBox(bg, box, width, 1, false)
	for i, l := range out {
		if w := dispWidth(l); w != width {
			t.Errorf("合成行 %d の幅 = %d; want %d: %q", i, w, width, l)
		}
	}
}
