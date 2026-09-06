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

// hintSurfaceSetup は面 ID → 「その面を出すモデルの作り方」と「消えてはいけない語」。
//
// 🚨 **配列にしているのは登録漏れを機械で止めるため** (issue 289)。hintSurfaceID を足すと
// ここの要素が nil のまま残り、TestHintSurfacesCoverEveryID が落ちる。以前は非全画面ぶんが
// 手書きのリテラル列で、下限チェックは「登録の削除」しか見ておらず**分岐の追加は素通り**した
// (変異で実測: 溢れる分岐を 1 本足しても幅ゲート 4 本とも緑だった)。
//
// exit は「幅が足りなくても最後まで残るべき語」。抜ける手段がある面はその案内、
// 確認・進行中の面は「何を聞かれているか / 何を待っているか」(消えると操作不能になる)。
var hintSurfaceSetup = [hintSurfaceCount]struct {
	name string
	open func(*browseModel)
	exit string
}{
	hintSurfaceBase:         {"基底一覧", func(*browseModel) {}, "q: 終了"},
	hintSurfacePushConfirm:  {"push 確認", func(m *browseModel) { m.actModal.pushConfirm = true }, "push しますか?"},
	hintSurfacePullConfirm:  {"pull 確認", func(m *browseModel) { m.actModal.pullConfirm = true }, "pull --rebase しますか?"},
	hintSurfacePushing:      {"push 実行中", func(m *browseModel) { m.actModal.pushing = true }, "pushing..."},
	hintSurfacePulling:      {"pull 実行中", func(m *browseModel) { m.actModal.pulling = true }, "pulling..."},
	hintSurfaceRerunConfirm: {"再実行 確認", func(m *browseModel) { m.actModal.rerunConfirm = true }, "job を再実行しますか?"},
	hintSurfaceRerunning:    {"再実行 中", func(m *browseModel) { m.actModal.rerunning = true }, "rerunning..."},
	// 🚨 target は 2 件にする (敵対的レビュー 2026-09-06 の P3)。この面だけ項目が
	// 実行時に伸びる (strings.Join の連結) ので、1 件の fixture では最長形を測れない。
	// updateKeyTarget が返すのは claude / codex の 2 つだけなので、これが実在しうる最長。
	hintSurfaceUpdating: {"self-update 中", func(m *browseModel) {
		m.actModal.updating = map[string]bool{"claude": true, "codex": true}
	}, "update..."},
	hintSurfaceDiff:      {"diff overlay", func(m *browseModel) { m.diffOv.sha = m.commits[0].SHA }, "q/h: 閉じる"},
	hintSurfacePRStatus:  {"PR 状態", func(m *browseModel) { m.prStatusOv.sha = m.commits[0].SHA }, "P/q/h: 閉じる"},
	hintSurfaceJobDetail: {"job 詳細", func(m *browseModel) { m.detailOv.open = true }, "Enter/h/q: 戻る"},
	hintSurfacePanelCursor: {"job パネル(カーソルあり)", func(m *browseModel) {
		m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
	}, "h/q: 閉じる"},
	// 🚨 openPanel は panelCursor = -1 から始めるので、Enter を押した直後に必ず通るのは
	// こちら。そちらだけが幅検査の外にあり、実際に w=60〜69 で出口が切れていた (issue 279)。
	hintSurfacePanelNoCursor: {"job パネル(カーソル無し)", func(m *browseModel) {
		m.panelSHA, m.panelCursor = m.commits[0].SHA, -1
	}, "Enter/h/q: 閉じる"},
}

// hintSurface は「最下行の案内を持つ画面」1 つ。
type hintSurface struct {
	name string
	open func(*browseModel)
	exit string // 幅が足りなくても最後まで残るべき語
	// prefixed は「取得中 / gh の警告」の前置が付く面。前置は**意図的に頭打ちにする**
	// (予算の半分を超えたら前置の方を切る) ので、`…` が出るのは正常。
	prefixed bool
	// id は「この setup で出るはずの面」。checkID のときだけ突き合わせる。
	//
	// 🚨 **出口の語で面の同一性を判定しないこと** (敵対的レビュー 2026-09-06 の P1)。
	// exit は互いに部分文字列になりうる (`q/h: 閉じる` ⊂ `P/q/h: 閉じる` /
	// `h/q: 閉じる` ⊂ `Enter/h/q: 閉じる` の 2 組が実在する) ので、
	// `strings.Contains` は**面を取り違えても緑**になる。実測: activeHintSurface() の
	// `case m.diffOv.visible()` の戻り値を hintSurfacePRStatus に変えても全パッケージ緑だった。
	// 語は UI の文言なので変えられない。同一性は ID で直接見る。
	id      hintSurfaceID
	checkID bool
}

func hintSurfaces() []hintSurface {
	out := make([]hintSurface, 0, len(hintSurfaceSetup)+len(fullScreenCases)+2)
	// 非全画面の面はレジストリから駆動する (登録漏れは TestHintSurfacesCoverEveryID が落とす)
	for id, sc := range hintSurfaceSetup {
		out = append(out, hintSurface{
			name: sc.name, open: sc.open, exit: sc.exit,
			id: hintSurfaceID(id), checkID: true,
		})
	}
	// 前置つきの状態も面として持つ。fitHintItems が収めた後に前置を積むと末尾 = 出口が
	// 落ちるので、前置ぶんを予算から引いているかをここで見る。
	// 🚨 m.ghErr は次の取得開始までクリアされないので、gh 未導入の環境では常時この状態。
	out = append(out,
		hintSurface{name: "基底一覧 + gh 警告", open: func(m *browseModel) {
			m.ghErr = &GHError{Kind: GHOther, Detail: "gh が未認証のため CI 状態を取得できません (gh auth login)"}
		}, exit: "q: 終了", prefixed: true, id: hintSurfaceBase, checkID: true},
		hintSurface{name: "基底一覧 + 取得中", open: func(m *browseModel) {
			m.toFetch = []string{m.commits[0].SHA}
			m.pendingFetches = 1
		}, exit: "q: 終了", prefixed: true, id: hintSurfaceBase, checkID: true},
	)
	// 全画面ビューアぶんは fullScreenCases から駆動する — あちらは
	// TestFullScreenCasesCoverEveryID が「ID を足したら 1 行足す」を強制しているので、
	// **新しい全画面ビューアは自動でこの幅検査の対象になる**。
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

// レジストリの穴を塞ぐ: 面 ID を足したら、production の hint 項目と、テストの
// 「その面の出し方」の両方が要る。片方だけだと幅検査から静かに落ちる。
func TestHintSurfacesCoverEveryID(t *testing.T) {
	for id := hintSurfaceID(0); id < hintSurfaceCount; id++ {
		if hintBuilders[id] == nil {
			t.Errorf("hintSurfaceID %d に hintBuilders の項目が無い (hint_surfaces.go に足すこと)", id)
		}
		sc := hintSurfaceSetup[id]
		switch {
		case sc.name == "":
			t.Errorf("hintSurfaceID %d に hintSurfaceSetup の項目が無い (幅検査の対象から落ちる)", id)
		case sc.open == nil:
			t.Errorf("%s: その面を出す open が無い", sc.name)
		case sc.exit == "":
			t.Errorf("%s: 消えてはいけない語 (exit) が無い", sc.name)
		}
	}
}

// 最下行の案内は、どの画面でも「幅に収まる」かつ「抜ける手段が残る」。
//
// 155 / 201 / 264 が 3 度直してきた失敗モードで、そのたびに個別の画面へ手当てしていた。
// ここで全画面ぶんをまとめて掃く。
func TestEveryHintKeepsExitWithinWidth(t *testing.T) {
	surfaces := hintSurfaces()
	// 🚨 下限が実際に検出するのは **exits map の書き忘れで全画面ビューアが対象から落ちる**形だけ
	// (敵対的レビュー 2026-09-06 の P3)。レジストリぶんは hintSurfaceCount 件を無条件に
	// append するので、そちらの取りこぼしは TestHintSurfacesCoverEveryID が先に落とす。
	// 「走査 0 件 = 緑」を塞ぐ役目はその 2 本で分担している。
	want := int(hintSurfaceCount) + 2 + len(fullScreenCases)
	if len(surfaces) < want {
		t.Fatalf("検査対象が %d 面しかない (期待 %d)。exits の書き忘れで対象から落ちている",
			len(surfaces), want)
	}
	// 🚨 面ごとに t.Run で分ける (敵対的レビュー 2026-09-06 の P3)。二重ループ + t.Fatalf だと
	// 最初の 1 件で全体が止まり、**面ごとの pass/fail が読めない**。変異を当てたとき
	// 「どれか 1 面が落ちた」までしか分からず、残りの面が守られているかを示せない
	// (mutation-verify-new-tests.md の「テーブル駆動なら全ケースが red になるか」)。
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			for w := frameMinWidth; w <= 140; w++ {
				m := newTestBrowse(t, 1, map[string]CIState{}, nil)
				m.showFrame, m.width, m.height = true, w, 40
				s.open(m)
				// 🚨 幅を見る前に**面の同一性**を見る。出口の語は互いに部分文字列に
				// なりうるので、Contains だけでは面を取り違えても緑になる (P1)。
				if s.checkID {
					if got := m.activeHintSurface(); got != s.id {
						t.Fatalf("w=%d: この setup は面 %d を出すはずだが activeHintSurface() は %d を返した",
							w, s.id, got)
					}
				}
				got := stripANSI(m.hintLine())
				if dispWidth(got) > w {
					t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, dispWidth(got), got)
					return
				}
				if !s.prefixed && strings.Contains(got, "…") {
					t.Errorf("w=%d: 語の途中で切れている: %q", w, got)
					return
				}
				if !strings.Contains(got, s.exit) {
					t.Errorf("w=%d: 抜ける手段 %q が消えた: %q", w, s.exit, got)
					return
				}
			}
		})
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
