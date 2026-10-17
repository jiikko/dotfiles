package main

// issues viewer の「閉じる演出」(板が 1 枚まるごと等速で右へ抜ける) の検査。
//
// 🚨 他のテストは newTestIssuesView / newTestBrowse で演出を切っている (close したら即座に
// 畳まれている前提で書かれているため)。ここだけ明示的に on にして演出そのものを見る。

import (
	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/testenv"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
)

// 閉じ始めの 1 フレームで目に見えて動く。開く動きをそのまま逆再生すると easeOutCubic の緩やかな
// 尾が立ち上がりの平坦部になり、最初のフレームでは 1 桁も動かない (esc の反応が鈍く見える)。
// 「動いたか」ではなく「1 フレームでどれだけ動いたか」を縛らないとこの平坦さを検出できない。
// 境は幅の 5%: 等速なら 1 拍で 7 桁 (33ms / 450ms) 動き、逆再生なら 0 桁で止まる。
func TestIssuesCloseMovesOnFirstFrame(t *testing.T) {
	const width = 100
	window := make([]string, 20)
	for i := range window {
		window[i] = strings.Repeat("x", width)
	}
	// tick 1 拍 (scrollInterval) ぶん進んだ時点の進捗。閉じるので 1 から落ちる
	p := 1 - float64(scrollInterval)/float64(issuesview.IssuesCloseDuration)

	closing := maxRowShift(layout.SlideIn(window, p, width, true, issuesview.IssuesAnimStagger))
	if closing < width/20 {
		t.Fatalf("閉じ始めの 1 フレームで %d 桁しか動いていない (立ち上がりが潰れている)", closing)
	}
	// 開く側は右外から入ってくる = 1 フレーム目はまだ大きくずれている (逆向きの回帰よけ)
	if opening := maxRowShift(layout.SlideIn(window, 1-p, width, false, issuesview.IssuesAnimStagger)); opening <= closing {
		t.Fatalf("開き始めが右外から入ってきていない (ずれ %d 桁)", opening)
	}
}

// 板が画面外へ出る時刻と viewer を畳む時刻が揃っている。ずれると「もう何も無い画面」を見せてから
// git log へ戻ることになる。🚨 終端で減速するカーブを使うと必ずこれが起きる (残り数 % の距離に
// 時間の後半を使うため。easeOutCubic + 700ms のとき 280 桁端末で 100ms の白画面が出ていた)。
func TestIssuesCloseLeavesNoBlankFrame(t *testing.T) {
	advance := testenv.StubClock(t)

	v := loadedView(issuesviewtest.SampleIssues()...)
	v.CloseAnimOff = false
	v.Close()

	for elapsed := time.Duration(0); elapsed < 5*issuesview.IssuesCloseDuration; elapsed += scrollInterval {
		v.SettleClose() // browseModel の tick と同じ順 (畳んでから描く)
		if !v.Visible() {
			return
		}
		if strings.TrimSpace(strings.Join(v.Lines(renderOpts(12)), "")) == "" {
			t.Fatalf("%v 時点で画面が空なのに viewer が畳まれていない (ここが白画面になる)", elapsed)
		}
		advance(scrollInterval)
	}
	t.Fatal("演出が終わらない")
}

// maxRowShift は窓の各行の右へのずれ (先頭の空白幅) の最大値。空行は演出で消えた行なので除く。
func maxRowShift(window []string) int {
	shift := 0
	for _, ln := range window {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		shift = max(shift, len(ln)-len(strings.TrimLeft(ln, " ")))
	}
	return shift
}

// キーは演出を即着地させ、かつ飲み込まれない (q で閉じた直後の q が効かない時間を作らない)。
func TestIssuesCloseLandsOnKeyWithoutSwallowing(t *testing.T) {
	testenv.StubClock(t)

	m := openAnimBrowse(t)
	if !m.issuesOv.Visible() {
		t.Fatal("前提が崩れた: viewer が開いていない")
	}

	m.issuesOv.Close()
	if !m.issuesOv.Visible() {
		t.Fatal("前提が崩れた: 演出なしで即閉じた")
	}
	releaseKey(m)
	m.handleKey("q") // 閉じる演出の途中に来たキー
	if m.issuesOv.Visible() {
		t.Fatal("キーで演出が着地しない (入力が効かない時間ができる)")
	}
	// 🚨 飲み込まれず通常処理まで届く。届かないと「q で閉じた直後の q が効かない」ことになる。
	// newTestBrowse は終了演出も切っているので、q が届いていれば m.done が立つ
	if !m.done {
		t.Fatal("着地させたキーが飲み込まれた (viewer を閉じた直後の q が効かない)")
	}
}

// 閉じる演出の途中に来たキーは、viewer を畳んでから通常のキーとして処理される。
//
// 🚨 viewer へ回してはいけない: 畳んだ後の view にモードを持つキーの状態 (/ の絞り込み・n の
// 確認モーダル) が残り、次に i で開いた瞬間に蘇る。絞り込みが蘇ると全キーが検索語として飲まれて
// q でも閉じられなくなり、確認モーダルが蘇ると y/Enter で実ファイルが動く。
// 実体は browseModel が routing 前に finishClose すること (tui.go)。
func TestIssuesCloseLandsBeforeKeyReachesViewer(t *testing.T) {
	advance := testenv.StubClock(t)

	for _, key := range []string{"/", "n"} {
		m := openAnimBrowse(t)
		m.issuesOv.Close()
		advance(issuesview.IssuesCloseDuration / 4) // まだ演出の途中

		releaseKey(m)
		m.handleKey(key)

		if m.issuesOv.Visible() {
			t.Fatalf("%q で演出が着地していない", key)
		}
		if m.issuesOv.NumFilter.Active || m.issuesOv.MarkNext.Active {
			t.Fatalf("%q が畳んだ後の viewer に状態を残した (絞り込み=%v 確認=%v)",
				key, m.issuesOv.NumFilter.Active, m.issuesOv.MarkNext.Active)
		}
	}
}

// 閉じる演出中の e は捨てる。🚨 素通しさせると板がまだ見えているのに git log 一覧側の e
// (openEditorAtRoot = `nvim .`) が全画面で起動し、「見ている issue を開いたつもりが repo root」に
// なる。q/Esc は素通しのまま (飲むと「閉じた直後の q が効かない」窓ができる) なので、
// e を捨てても q が死なないことまで見る。
func TestBrowseCloseAnimSwallowsEditorKeyOnly(t *testing.T) {
	cmds := testenv.StubEditorCapture(t)

	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv.CloseAnimOff = false // 本番の既定 (演出あり) に戻す
	m.issuesOv.Toggle(t.TempDir())
	m.issuesOv.FinishAnim() // 開く演出は着地させる
	if !m.issuesOv.Visible() {
		t.Fatal("前提が崩れた: viewer が開いていない")
	}

	m.handleKey("i") // 閉じ始める (演出中は visible のまま)
	if !m.issuesOv.Closing || !m.issuesOv.Visible() {
		t.Fatalf("閉じる演出に入っていない: closing=%v visible=%v", m.issuesOv.Closing, m.issuesOv.Visible())
	}

	m.handleKey("e")
	if len(*cmds) != 0 {
		t.Errorf("閉じる演出中の e がエディタを起動した: %v", (*cmds)[0].Args)
	}
}

// 上の例外で q が死んでいないこと (演出中の q は素通しして通常処理へ届く)。
func TestBrowseCloseAnimStillPassesQuitKey(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.issuesOv.CloseAnimOff = false
	m.issuesOv.Toggle(t.TempDir())
	m.issuesOv.FinishAnim()
	m.handleKey("i") // 閉じ始める
	if !m.issuesOv.Closing {
		t.Fatal("閉じる演出に入っていない")
	}
	if _, cmd := m.handleKey("q"); cmd == nil {
		t.Error("閉じる演出中の q が素通しされず終了しない (飲む対象を広げすぎている)")
	}
}

// openAnimBrowse は閉じる演出を有効にした issues viewer を開いた browseModel を返す (開く演出は着地済み)。
func openAnimBrowse(t *testing.T) *browseModel {
	t.Helper()
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.issuesOv.CloseAnimOff = false
	m.issuesOv.Toggle(t.TempDir())
	m.issuesOv.FinishAnim()
	return m
}
