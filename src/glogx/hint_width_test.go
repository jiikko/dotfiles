package main

import (
	"strings"
	"testing"
)

// 最下行の hint は **popup の幅に収まる**。
//
// 🚨 収まらない hint は `clipToWidth` で末尾が切られ、**常に並びの最後が消える**。
// そこに置かれているのは「どう抜けるか」なので、狭い端末ほど出口が分からなくなる
// (issue 155 が status viewer で実測し、issue 201 の監査で doctor / job パネル /
// detailOv の 3 箇所が同じ状態だったことが分かった。doctor は 112 桁 / 予算 82 桁)。
//
// 直し方は「切る」ではなく **`fitHintItems` で入らない項目を落とす**。優先度 1 に
// 「抜ける手段」を置けば、幅が狭くてもそれだけは残る。
//
// ⚠️ この検査は**静的な hint 文字列だけ**を見る。`m.spinner()` の連結や `🚨 ghErr` の
// 前置のような動的合成は測れない (偽陰性が残る)。「これで hint の幅は保証された」とは
// 言えないが、固定文字列の作り忘れは止まる。
func TestHintsFitPopupWidth(t *testing.T) {
	// popup の実効幅。🚨 production の hintWidth() から導く (tui_helpers_test.go の testHintBudget)
	width := testHintBudget(t)

	t.Run("doctor", func(t *testing.T) {
		v := &doctorView{}
		assertFits(t, "doctorView.hint", v.hint(width), width)
		// 狭くしても「抜ける手段」は残る (優先度 1)
		if got := v.hint(20); !strings.Contains(got, "esc") {
			t.Errorf("幅 20 で抜ける手段が消えた: %q", got)
		}
	})

	t.Run("job パネル (カーソルあり)", func(t *testing.T) {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		m.width = testPopupTermWidth
		m.panelSHA = m.commits[0].SHA
		m.panelCursor = 0
		// ⚠️ 予算は **m.hintWidth()** で測る (固定値を書くと外れる)。frame の有無で
		// 左右余白 2 桁を引くかが変わり、frame 無効なら m.width そのもの。
		// frame 有効時は左余白 1 桁が前置されるので剥がしてから測る。
		budget := m.hintWidth()
		got := strings.TrimPrefix(stripANSI(m.hintLine()), " ")
		assertFits(t, "job パネル", got, budget)
		if !strings.Contains(got, "閉じる") {
			t.Errorf("job パネルの hint に抜ける手段が無い: %q", got)
		}
	})

	// ⚠️ detailOv は開くのに CI の応答が要るので、hintLine 経由では組み立てにくい。
	// 生成している fitHintItems の呼び出しと同じ項目をここで直接測る
	// (自己言及にならないよう**優先度と幅の主張だけ**を見る。文言が変わればこの表も直す)。
	t.Run("detailOv の項目", func(t *testing.T) {
		got := fitHintItems(width, []hintItem{
			{"j/k: スクロール", 3},
			{"J/K: 隣の job", 4},
			{"v: nvim で開く", 4},
			{"r: 再実行", 3},
			{"Enter/h/q: 戻る", 1},
			{"o: ブラウザ", 4},
			{"y: URL", 5},
			{"Y: 詳細コピー", 5},
		})
		assertFits(t, "detailOv", got, width)
		if !strings.Contains(got, "戻る") {
			t.Errorf("detailOv の hint に抜ける手段が無い: %q", got)
		}
	})
}

func assertFits(t *testing.T, name, hint string, width int) {
	t.Helper()
	if w := dispWidth(hint); w > width {
		t.Errorf("%s が %d 桁で幅 %d に収まらない (末尾が切られ、並びの最後の項目が消える):\n  %s",
			name, w, width, hint)
	}
}

// commit diff の hint は fitHintItems で組み、狭い幅でも抜ける手段が残る (issue 264)。
// 幅を掃くのは TestStatusHintUsesRenderBudget と同じ理由 (1 点では予算のずれが余白に吸われる)。
func TestDiffHintUsesRenderBudget(t *testing.T) {
	for w := frameMinWidth; w <= 140; w++ {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.showFrame, m.width, m.height = true, w, 40
		m.diffOv.open(m.commits[0].SHA)
		line := m.hintLine()
		got := stripANSI(line)
		if dispWidth(got) > w {
			t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, dispWidth(got), line)
		}
		if strings.Contains(got, "…") || !strings.Contains(got, "q/h: 閉じる") {
			t.Errorf("w=%d: 切り詰め or 抜ける手段が消えた: %q", w, line)
		}
	}
}

// hintSurface は「最下行の案内を持つ画面」1 つ。
//
// 🚨 **列挙表ではなくレジストリとして扱う** (issue 281)。issue 201 は「列挙すると兄弟を
// 足したときに追随を忘れる = この検査が守りたい事故を検査自身が踏む」として走査型を
// 指定していたが、実装は 3 件の t.Run 列挙になっており、基底一覧 / ratelimit / PR 状態の
// 3 画面が検査の外に落ちていた (issue 279 でその 3 つとも実際に切れていた)。
//
// 全画面ビューアぶんは fullScreenCases から駆動する — あちらは
// TestFullScreenCasesCoverEveryID が「ID を足したら 1 行足す」を強制しているので、
// **新しい全画面ビューアは自動でこの幅検査の対象になる**。
type hintSurface struct {
	name string
	open func(*browseModel)
	exit string // 優先度 1 = 抜ける手段。狭い幅でも必ず残ること
	// prefixed は「取得中 / gh の警告」の前置が付く面。前置は**意図的に頭打ちにする**
	// (予算の半分を超えたら前置の方を切る) ので、`…` が出るのは正常。
	// 出口が残ることと幅に収まることは、前置あり/なしで同じように要求する。
	prefixed bool
}

func hintSurfaces() []hintSurface {
	out := []hintSurface{
		{"基底一覧", func(*browseModel) {}, "q: 終了", false},
		{"PR 状態", func(m *browseModel) { m.prStatusOv.sha = m.commits[0].SHA }, "P/q/h: 閉じる", false},
		// 🚨 job パネルは**カーソルの有無で別の分岐**。openPanel は panelCursor = -1 から
		// 始めるので、Enter を押した直後に必ず通るのは「カーソル無し」の方。
		// そちらだけが幅検査の外にあり、実際に w=60〜69 で出口が切れていた (issue 279)。
		{"job パネル(カーソル無し)", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, -1
		}, "Enter/h/q: 閉じる", false},
		{"job パネル(カーソルあり)", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
		}, "h/q: 閉じる", false},
		// 🚨 前置がある状態も面として持つ。fitHintItems が収めた後に前置を積むと
		// 末尾 = 出口が落ちるので、前置ぶんを予算から引いているかをここで見る。
		// m.ghErr は次の取得開始までクリアされないので、gh 未導入の環境では常時この状態。
		{"基底一覧 + gh 警告", func(m *browseModel) {
			m.ghErr = &GHError{Kind: GHOther, Detail: "gh が未認証のため CI 状態を取得できません (gh auth login)"}
		}, "q: 終了", true},
		{"基底一覧 + 取得中", func(m *browseModel) {
			m.toFetch = []string{m.commits[0].SHA}
			m.pendingFetches = 1
		}, "q: 終了", true},
	}
	exits := map[fullScreenID]string{
		fullScreenRatelimit: "R/q/esc/h: 閉じる",
		fullScreenDoctor:    "D/q/esc: 閉じる",
		fullScreenStatus:    "q: 終了",
		fullScreenIssues:    "q: 終了",
	}
	for _, c := range fullScreenCases {
		exit, ok := exits[c.id]
		if !ok {
			continue // 対応する出口の語を書き忘れたら下の canary が落とす
		}
		out = append(out, hintSurface{name: c.name, open: c.show, exit: exit})
	}
	return out
}

// 最下行の案内は、どの画面でも「幅に収まる」かつ「抜ける手段が残る」。
//
// 155 / 201 / 264 が 3 度直してきた失敗モードで、そのたびに個別の画面へ手当てしていた。
// ここで全画面ぶんをまとめて掃く。
func TestEveryHintKeepsExitWithinWidth(t *testing.T) {
	surfaces := hintSurfaces()
	// 🚨 全画面ビューア (4 枚) + 基底一覧 + PR 状態。出口の語を書き忘れて対象から
	// 落ちても気づけるよう下限を置く (走査 0 件 = 緑 を塞ぐ)。
	if len(surfaces) < 2+len(fullScreenCases) {
		t.Fatalf("検査対象が %d 面しかない (期待 %d)。exits の書き忘れで対象から落ちている",
			len(surfaces), 2+len(fullScreenCases))
	}
	for _, s := range surfaces {
		for w := frameMinWidth; w <= 140; w++ {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.showFrame, m.width, m.height = true, w, 40
			s.open(m)
			got := stripANSI(m.hintLine())
			if dispWidth(got) > w {
				t.Fatalf("%s w=%d: hint 行が端末幅を超えた (%d 桁): %q", s.name, w, dispWidth(got), got)
			}
			if !s.prefixed && strings.Contains(got, "…") {
				t.Fatalf("%s w=%d: 語の途中で切れている: %q", s.name, w, got)
			}
			if !strings.Contains(got, s.exit) {
				t.Fatalf("%s w=%d: 抜ける手段 %q が消えた: %q", s.name, w, s.exit, got)
			}
		}
	}
}

// fitHintItems は「優先度 1 が入らないなら低優先で埋めない」(issue 279)。
//
// 優先度は**採る順序**を決めるだけで席を予約しないので、以前は出口 (17 桁) が入らない
// 幅で、より短い低優先 (9 桁) だけが残っていた。抜ける手段が消えた案内は短い案内より悪い。
func TestFitHintItemsReservesExit(t *testing.T) {
	items := []hintItem{
		{"j/k: 移動", 2},      // 9 桁
		{"D/q/esc: 閉じる", 1}, // 17 桁 = 出口
		{"y: URL コピー", 4},
	}
	// 出口が入らない帯: 低優先で埋めず、出口だけを返す
	for w := 1; w < dispWidth("D/q/esc: 閉じる"); w++ {
		got := fitHintItems(w, items)
		if !strings.Contains(got, "閉じる") {
			t.Fatalf("w=%d: 出口が消えた: %q", w, got)
		}
		if strings.Contains(got, "j/k") {
			t.Fatalf("w=%d: 出口が入らない幅なのに低優先が出ている: %q", w, got)
		}
	}
	// 出口が入る幅からは通常どおり (出口 + 入るものを並べる)
	wide := fitHintItems(80, items)
	for _, want := range []string{"D/q/esc: 閉じる", "j/k: 移動", "y: URL コピー"} {
		if !strings.Contains(wide, want) {
			t.Errorf("広い幅で %q が出ていない: %q", want, wide)
		}
	}
	// 🚨 優先度 1 が**複数**あるとき、狭い幅では**短い方**が残る (shortestPrio1 の主張)。
	// これを見ないと `w < bestW` を `w > bestW` (最長) へ変えても全テストが緑で通る
	// (敵対レビューが実測)。statusView.hint は実際に優先度 1 を 2 つ持つ表。
	two := []hintItem{
		{"j/k: 移動", 2},
		{"s: 一覧へ戻る (長い方)", 1}, // 21 桁
		{"q: 終了", 1},          // 7 桁
	}
	for w := 1; w < dispWidth("s: 一覧へ戻る (長い方)"); w++ {
		got := fitHintItems(w, two)
		if strings.Contains(got, "一覧へ戻る") {
			t.Fatalf("w=%d: 優先度 1 が 2 つあるとき長い方が返った: %q", w, got)
		}
	}

	// 優先度 1 が無い表では従来どおり (末尾へフォールバック)
	noExit := []hintItem{{"aaa", 2}, {"bb", 3}}
	if got := fitHintItems(1, noExit); got != "bb" {
		t.Errorf("優先度 1 が無いときのフォールバックが変わった: %q", got)
	}
}
