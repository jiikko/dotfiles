package main

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
)

// toastHold は「にゅっと出た」あと引っ込むまでの静止時間。push/pull 完了の結果を見落とさない
// 程度。実時間 (3s) をテストで待たずに退場遷移 (holdCmd → toastMsg) を検証できるよう var に
// してある (本番値は不変、テストだけ短い値へ差し替える)。
var toastHold = 3 * time.Second

// toastSlideFrames は入場/退場の横スライドを何フレームで渡り切るか。frame を 0→N で進め、
// 表示カラム shown = easeOutCubic(frame/N) × 箱幅 とする (箱幅に依らずほぼ一定時間
// ~12frame × scrollInterval ≈ 400ms)。行 (縦) でなくカラム (横) を動かすため、箱が数行でも
// 解像度の高い滑らかなスライドになる。
const toastSlideFrames = 12

// easedShown は frame (0..toastSlideFrames) に対する表示カラム数を easeOutCubic で返す。
// 線形だと入場/退場の始点・終点で速度が急に切り替わり「カクッ」と見える。easeOutCubic は
// 終点付近で減速するので、入場 (frame 0→N) は「すっと収まり」、退場 (frame N→0 と逆走) は
// 曲線を逆に辿るため始めゆっくり→終わり加速で「すっと消える」自然な動きになる。
func easedShown(frame, w int) int {
	if frame <= 0 {
		return 0
	}
	if frame >= toastSlideFrames {
		return w
	}
	p := float64(frame) / float64(toastSlideFrames)
	q := 1 - p
	eased := 1 - q*q*q // easeOutCubic
	return int(math.Round(eased * float64(w)))
}

// toastMsg は静止 (holding) が終わって退場アニメを始める合図。seq で世代管理し、新しいトーストが
// 上書きした後に届く古いタイマーは無視する (連続 push/pull で前の退場が後のを消さないように)。
type toastMsg struct{ seq int }

type toastPhase int

const (
	toastHidden   toastPhase = iota // 非表示
	toastEntering                   // 右画面外から左へ 滑り込み中 (shown 0→boxWidth)
	toastHolding                    // 全幅表示で静止 (toastHold 後に leaving へ)
	toastLeaving                    // 右画面外へ 滑り出し中 (shown boxWidth→0)
)

// toastItem は右下に出す結果フィードバック 1 枚。右の画面外から左へ「にゅっと」滑り込んで現れ、
// 数秒静止し、また右へ「にゅっと」滑り出て消える横スライド (shown = 箱の左から見せているカラム数を
// tick で増減させ、右端揃えで overlay すると箱が水平移動して見える)。行単位でなくカラム単位で
// 動かすため、箱が数行でも滑らかなアニメになる。glogx は tmux の display-popup 内で動くため
// tmux-toast (floating pane) は popup に隠れて出せず、glogx 自身の TUI 内に描く。
type toastItem struct {
	text  string
	ok    bool // true=成功 (✓緑) / false=失敗 (✗赤)。info=true のときは無視される
	info  bool // true=進行中/中立 (…シアン)。ok より優先し、完了/失敗どちらでもない状態を表す
	seq   int  // 世代: 退場タイマーの有効性判定 + 再表示リセット
	phase toastPhase
	shown int // 現在見せている箱の左カラム数 (0=画面右外に収納 / boxWidth=全幅表示)
	frame int // スライドの進捗フレーム (入場 0→N / 退場 N→0)。shown = easedShown(frame)
}

// reset は 1 枚を「これから滑り込む状態」に作り直す (スタックが積むときに使う)。seq は世代管理
// (退場タイマーの有効性判定) に使うのでスタック側が採番して渡す。
func (t *toastItem) reset(text string, ok, info bool, seq int) {
	t.seq = seq
	t.text, t.ok, t.info = text, ok, info
	t.phase = toastEntering
	t.shown, t.frame = 0, 0
}

// animating は入場/退場アニメ中か (tick を回す必要がある + spinnerActive に含める)。holding は
// 全幅のまま静止 (tea.Tick の toastMsg 待ち) なので tick 不要。
func (t *toastItem) animating() bool { return t.phase == toastEntering || t.phase == toastLeaving }

// visible は表示中か (holding 含む)。
func (t *toastItem) visible() bool { return t.phase != toastHidden }

// boxWidth は箱の総カラム幅 (スライドの終点)。実描画幅と一致させるため fullBox の 1 行目の
// 表示幅を使う (buildShadowPanelBox の最小幅クランプ込み)。色に依らず一定。
func (t *toastItem) boxWidth(colored bool) int {
	full := t.fullBox(colored)
	if len(full) == 0 {
		return 0
	}
	return dispWidth(full[0])
}

// advance はアニメを 1 フレーム進める。frame を入場で 0→N、退場で N→0 に動かし、表示カラムは
// easedShown(frame) で求める (easeOutCubic)。入場完了で holding へ移り toastHold 後の退場
// タイマーを予約して返す。退場完了で hidden。
func (t *toastItem) advance(colored bool) (holdCmd tea.Cmd) {
	w := t.boxWidth(colored)
	switch t.phase {
	case toastEntering:
		t.frame++
		t.shown = easedShown(t.frame, w)
		if t.frame >= toastSlideFrames {
			t.shown = w
			t.phase = toastHolding
			seq := t.seq
			return tea.Tick(toastHold, func(time.Time) tea.Msg { return toastMsg{seq: seq} })
		}
	case toastLeaving:
		t.frame--
		t.shown = easedShown(t.frame, w)
		if t.frame <= 0 {
			t.shown = 0
			t.phase = toastHidden
			t.text = ""
		}
	case toastHidden, toastHolding:
		// advance の駆動対象外 (holding の退場開始は Tick が、hidden→entering は show が担う)
	}
	return nil
}

// startLeaving は holding の静止時間が明けたら (toastMsg) 退場アニメへ移す。世代一致時のみ。
func (t *toastItem) startLeaving(msg toastMsg) {
	if msg.seq == t.seq && t.phase == toastHolding {
		t.phase = toastLeaving
	}
}

// fullBox は内容幅にフィットした影付き小箱 (全行)。スライドの基準になる全幅・全行の算出にも使う。
func (t *toastItem) fullBox(colored bool) []string {
	mark, color := "✓", ansiGreen
	switch {
	case t.info:
		mark, color = "…", ansiCyan
	case !t.ok:
		mark, color = "✗", ansiRed
	}
	row := paint(mark+" "+t.text, color, colored)
	boxW := dispWidth(row) + shadowBoxChrome
	// 枠線も種別色 (成功=緑 / 失敗=赤 / 進行=シアン) で染めて一体感を出す。影は中立の dim のまま。
	return buildShadowPanelBox("", []string{row}, boxW, colored, color)
}

// boxLines は現フレームで見せる箱行 (全行) を返す。各行を箱の左 shown カラムに切り、右端揃えで
// overlay されると「右画面外から左へ滑り込む/右へ滑り出る」横スライドになる。左カラム切りで開いた
// SGR は行末で閉じる (右端揃え合成の背景に色がにじまないように)。非表示なら nil。
func (t *toastItem) boxLines(colored bool) []string {
	if t.phase == toastHidden {
		return nil
	}
	full := t.fullBox(colored)
	if len(full) == 0 {
		return nil
	}
	// 箱幅は full から直に導く (boxWidth を呼ぶと fullBox をもう一度組んでしまう。表示中は毎フレーム
	// 走るので二重構築を避ける)。
	v := min(max(t.shown, 0), dispWidth(full[0]))
	if v <= 0 {
		return nil
	}
	out := make([]string, len(full))
	for i, row := range full {
		clipped := truncateKeepANSI(row, v) // 箱の左 v カラム (右側は画面右端の外へ)
		if colored {
			clipped += ansiReset
		}
		out[i] = clipped
	}
	return out
}

// toastStackMax は同時に積む枚数の上限。⚠️ 上限が無いと通知が連続したとき画面を覆う (トーストは
// 内容の上に重なるので、下の一覧・本文が読めなくなる)。溢れたら一番古い (一番下) を捨てる。
const toastStackMax = 3

// toast は右下の通知スタック。新しい通知は**上に積まれ**、古い通知は**下から抜けていく**
// (ユーザー要望 2026-07-31)。
//
// 以前は 1 枠の後勝ちで、新しい通知が出るたび前の通知が消えていた。そのため「今それを消したくない」
// 場面ごとに呼び出し側が調停する必要があり、同じ問題に 3 つの実装ができていた (claude version 通知の
// 専用タイマー付き遅延再送 / autobuild の pending 保持 / 残り全部は調停なしの即上書き)。実測では
// 「新しい glogx をビルド中」の通知がコピー操作 1 回で消えていた。積めるようにすればどの経路も
// 素直に show() を呼ぶだけで済み、調停そのものが要らなくなる。
//
// ⚠️ 最新の 1 枚を埋め込みで持つ: 呼び出し側とテストが t.text / t.ok / t.phase を直接読む箇所が
// 多数あり (テストだけで ~150 箇所)、埋め込みなら「最新の通知」を指す既存の読み方をそのまま
// 保てる。older は上から 2 枚目以降 (index 0 が上寄り = 新しい側、末尾が一番下 = 最古)。
type toast struct {
	toastItem
	older  []toastItem
	seqGen int // 世代の採番 (退場タイマーの取り違え防止。枚数に依らず単調増加)
}

// show は新しい通知を最上段に積む。呼び出し側で maybeTick を Batch して tick を回すこと。
func (s *toast) show(text string, ok bool) { s.push(text, ok, false) }

// showInfo は進行中/中立の通知 (…シアン) を積む。
func (s *toast) showInfo(text string) { s.push(text, false, true) }

func (s *toast) push(text string, ok, info bool) {
	// ⚠️ ここで無害化する: 通知文は gh / git のエラー出力・claude のバージョン文字列といった
	// 外部由来をそのまま埋め込む呼び出しが多く (showWarning 経由だけで 10 箇所以上)、
	// 呼び出しごとに包むと必ずどこかが漏れる。status_view / issues_view の setNotice と同じ規律。
	text = sanitizePlainLine(text)
	s.seqGen++
	// 進行中トースト (…シアン) は「結果が出たら用済み」なので、新しい通知が来たら退かせる。
	// ⚠️ 積んだままにすると「PR を検索中...」の下に「PR #123 を開きます」が並び、終わったのに
	// 検索中と書いてある状態が数秒残る (実測 2026-07-31)。1 枠時代は上書きで自然に消えていた。
	s.dropInfo()
	if s.toastItem.visible() {
		// 今の最新を 1 段下へ押し下げてから、新しいものを最上段に置く
		s.older = append([]toastItem{s.toastItem}, s.older...)
	}
	var top toastItem
	top.reset(text, ok, info, s.seqGen)
	s.toastItem = top // 最上段を新しい 1 枚に差し替える
	if len(s.older) > toastStackMax-1 {
		s.older = s.older[:toastStackMax-1] // 溢れた分 (最古) を捨てる
	}
}

// dropInfo は進行中トースト (info) を取り除く (結果が出たら用済み)。
func (s *toast) dropInfo() {
	kept := s.older[:0]
	for i := range s.older {
		if !s.older[i].info {
			kept = append(kept, s.older[i])
		}
	}
	s.older = kept
	// 最上段が info ならそこを空けて、下があれば繰り上げる
	if s.info {
		s.toastItem = toastItem{}
		if len(s.older) > 0 {
			s.toastItem, s.older = s.older[0], s.older[1:]
		}
	}
}

// items は上から下の順に、表示中の 1 枚ずつを返す。
//
// 毎フレーム経路 (spinnerActive → animating / visible) はこれを使わず直接判定にしている。
// ⚠️ 「slice の alloc を避けるため」ではない — 実測では escape analysis が効いて items() 経由でも
// 0 allocs だった (2.99ns vs 1.86ns/回)。1 フレームに複数回通る判定を単純に保つだけの意図で、
// 性能上の効果はほぼ無い (フレームは ~200µs)。
func (s *toast) items() []*toastItem {
	out := make([]*toastItem, 0, len(s.older)+1)
	if s.toastItem.visible() {
		out = append(out, &s.toastItem)
	}
	for i := range s.older {
		out = append(out, &s.older[i])
	}
	return out
}

// animating は 1 枚でもスライド中か (tick を回す必要がある + spinnerActive に含める)。
// spinnerActive から毎フレーム呼ばれるので slice を作らない。
func (s *toast) animating() bool {
	if s.toastItem.animating() {
		return true
	}
	for i := range s.older {
		if s.older[i].animating() {
			return true
		}
	}
	return false
}

// advance は全ての枚を 1 フレーム進め、静止に入った枚の退場タイマーをまとめて返す。
// 抜け切った (hidden) 枚はここで取り除く = 下から抜けていく。
func (s *toast) advance(colored bool) tea.Cmd {
	var cmds []tea.Cmd
	if cmd := s.toastItem.advance(colored); cmd != nil {
		cmds = append(cmds, cmd)
	}
	kept := s.older[:0]
	for i := range s.older {
		if cmd := s.older[i].advance(colored); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if s.older[i].visible() {
			kept = append(kept, s.older[i])
		}
	}
	s.older = kept
	// 最新が抜け切ったのに下がまだ残っている場合 (下が先に抜ける通常順序の例外) は繰り上げる。
	// 埋め込みが「最新の 1 枚」を指す不変条件を保つため。
	if !s.toastItem.visible() && len(s.older) > 0 {
		s.toastItem = s.older[0]
		s.older = s.older[1:]
	}
	return tea.Batch(cmds...)
}

// startLeaving は静止時間が明けた枚を退場へ移す (seq で該当の枚を選ぶ)。
func (s *toast) startLeaving(msg toastMsg) {
	for _, it := range s.items() {
		it.startLeaving(msg)
	}
}

// toastBoxLines は 1 枚の箱の行数 (上罫線 + 内容 1 行 + 下罫線 + 落ち影)。fullBox が内容 1 行で
// buildShadowPanelBox を呼ぶので一定。⚠️ 箱の形を変えたらここも直す (テストで pin してある)。
const toastBoxLines = 4

// boxLines はスタック全体の描画行 (上から下)。maxLines を超える古い枚は出さない。
//
// ⚠️ 行数の上限が要る: 低い端末では 3 枚 (12 行) が窓 (11 行) を超え、一番下の箱が途中で切れて
// 壊れて見えた (実測 2026-07-31: 窓 11 行に対し 12 行)。枚数の上限 (toastStackMax) だけでは
// 窓の高さに対する占有を抑えられない。最新の 1 枚は上限を超えても出す — 見えない通知より
// 「窓を覆うが読める通知」を選ぶ。
func (s *toast) boxLines(colored bool, maxLines int) []string {
	var out []string
	for _, it := range s.items() {
		box := it.boxLines(colored)
		if len(box) == 0 {
			continue // まだ滑り込み前 (幅 0)
		}
		if len(out) > 0 && len(out)+toastBoxLines > maxLines {
			break // これ以上積むと窓を覆う。箱の途中で切らず、古い方を出さない
		}
		out = append(out, box...)
	}
	return out
}
