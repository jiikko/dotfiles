// Package toast は右下に数秒だけ出す通知 (トースト) のスタック。右の画面外から「にゅっと」滑り込み、数秒静止して、また右へ
// 滑り出て消える。新しい通知は上に積まれ、古い通知は下から抜けていく。glogx の toast.go から切り出した (glogx 以外の TUI でも使う)。
//
// 使い方: アプリのモデルに Stack を持ち、通知を Show / ShowInfo で積む。tick のたびに Advance を呼び (Animating の間は tick を回す)、
// Advance が返す Timer を After 後に Msg として戻し (bubbletea なら tea.Tick)、戻った Msg を StartLeaving に渡す。
// 描画は BoxLines の行を画面の右下に重ねる。影の色は Stack.Shadow で渡す。
// 🚨 描画フレームワークに依存しない (tuikit の約束。README の冒頭)。タイマーは自分で張らず、Timer として返す
package toast

import (
	"math"
	"time"

	"termsafe"
	"tuikit/layout"
	"tuikit/sgr"
	"tuikit/termwidth"
)

// Hold は「にゅっと出た」あと引っ込むまでの静止時間。push/pull 完了の結果を見落とさない
// 程度。実時間 (3s) をテストで待たずに退場遷移 (Timer → Msg) を検証できるよう var に
// してある (本番値は不変、テストだけ短い値へ差し替える)。
var Hold = 3 * time.Second

// SlideFrames は入場/退場の横スライドを何フレームで渡り切るか。frame を 0→N で進め、
// 表示カラム shown = easeOutCubic(frame/N) × 箱幅 とする (箱幅に依らずほぼ一定時間
// ~12frame × scrollInterval ≈ 200ms)。行 (縦) でなくカラム (横) を動かすため、箱が数行でも
// 解像度の高い滑らかなスライドになる。
const SlideFrames = 12

// easedShown は frame (0..SlideFrames) に対する表示カラム数を easeOutCubic で返す。
// 線形だと入場/退場の始点・終点で速度が急に切り替わり「カクッ」と見える。easeOutCubic は
// 終点付近で減速するので、入場 (frame 0→N) は「すっと収まり」、退場 (frame N→0 と逆走) は
// 曲線を逆に辿るため始めゆっくり→終わり加速で「すっと消える」自然な動きになる。
func easedShown(frame, w int) int {
	if frame <= 0 {
		return 0
	}
	if frame >= SlideFrames {
		return w
	}
	p := float64(frame) / float64(SlideFrames)
	q := 1 - p
	eased := 1 - q*q*q // easeOutCubic
	return int(math.Round(eased * float64(w)))
}

// Timer は「After 経ったら Msg をアプリへ戻して」という頼み (Advance が、入場を終えて静止に入った枚ごとに返す)。
// toast はタイマーを自分で張らない (描画フレームワークに依存しないため)。bubbletea なら tea.Tick(t.After, …) で Msg を戻す
type Timer struct {
	After time.Duration
	Msg   Msg
}

// Msg は静止 (holding) が終わって退場アニメを始める合図。seq で世代管理し、新しいトーストが
// 上書きした後に届く古いタイマーは無視する (連続 push/pull で前の退場が後のを消さないように)。
type Msg struct{ seq int }

type Phase int

const (
	Hidden   Phase = iota // 非表示
	Entering              // 右画面外から左へ 滑り込み中 (shown 0→boxWidth)
	Holding               // 全幅表示で静止 (Hold 後に leaving へ)
	Leaving               // 右画面外へ 滑り出し中 (shown boxWidth→0)
)

// item は右下に出す結果フィードバック 1 枚。右の画面外から左へ「にゅっと」滑り込んで現れ、
// 数秒静止し、また右へ「にゅっと」滑り出て消える横スライド (shown = 箱の左から見せているカラム数を
// tick で増減させ、右端揃えで overlay すると箱が水平移動して見える)。行単位でなくカラム単位で
// 動かすため、箱が数行でも滑らかなアニメになる。glogx は tmux の display-popup 内で動くため
// tmux-toast (floating pane) は popup に隠れて出せず、glogx 自身の TUI 内に描く。
type item struct {
	text   string
	ok     bool // true=成功 (✓緑) / false=失敗 (✗赤)。info=true のときは無視される
	info   bool // true=進行中/中立 (…シアン)。ok より優先し、完了/失敗どちらでもない状態を表す
	seq    int  // 世代: 退場タイマーの有効性判定 + 再表示リセット
	phase  Phase
	shown  int    // 現在見せている箱の左カラム数 (0=画面右外に収納 / boxWidth=全幅表示)
	shadow string // 落ち影の SGR 色 (積んだときの Stack.Shadow)
	frame  int    // スライドの進捗フレーム (入場 0→N / 退場 N→0)。shown = easedShown(frame)
}

// reset は 1 枚を「これから滑り込む状態」に作り直す (スタックが積むときに使う)。seq は世代管理
// (退場タイマーの有効性判定) に使うのでスタック側が採番して渡す。
func (t *item) reset(text string, ok, info bool, seq int, shadow string) {
	t.seq, t.shadow = seq, shadow
	t.text, t.ok, t.info = text, ok, info
	t.phase = Entering
	t.shown, t.frame = 0, 0
}

// animating は入場/退場アニメ中か (tick を回す必要がある + spinnerActive に含める)。holding は
// 全幅のまま静止 (Timer の Msg 待ち) なので tick 不要。
func (t *item) animating() bool { return t.phase == Entering || t.phase == Leaving }

// visible は表示中か (holding 含む)。
func (t *item) visible() bool { return t.phase != Hidden }

// boxWidth は箱の総カラム幅 (スライドの終点)。実描画幅と一致させるため fullBox の 1 行目の
// 表示幅を使う (layout.Panel の最小幅クランプ込み)。色に依らず一定。
func (t *item) boxWidth(colored bool) int {
	full := t.fullBox(colored)
	if len(full) == 0 {
		return 0
	}
	return termwidth.Of(full[0])
}

// advance はアニメを 1 フレーム進める。frame を入場で 0→N、退場で N→0 に動かし、表示カラムは
// easedShown(frame) で求める (easeOutCubic)。入場完了で holding へ移り Hold 後の退場
// タイマーを予約して返す。退場完了で hidden。
func (t *item) advance(colored bool) (hold *Timer) {
	w := t.boxWidth(colored)
	switch t.phase {
	case Entering:
		t.frame++
		t.shown = easedShown(t.frame, w)
		if t.frame >= SlideFrames {
			t.shown = w
			t.phase = Holding
			return &Timer{After: Hold, Msg: Msg{seq: t.seq}}
		}
	case Leaving:
		t.frame--
		t.shown = easedShown(t.frame, w)
		if t.frame <= 0 {
			t.shown = 0
			t.phase = Hidden
			t.text = ""
		}
	case Hidden, Holding:
		// advance の駆動対象外 (holding の退場開始は Tick が、hidden→entering は show が担う)
	}
	return nil
}

// startLeaving は holding の静止時間が明けたら (Msg) 退場アニメへ移す。世代一致時のみ。
func (t *item) startLeaving(msg Msg) {
	if msg.seq == t.seq && t.phase == Holding {
		t.phase = Leaving
	}
}

// fullBox は内容幅にフィットした影付き小箱 (全行)。スライドの基準になる全幅・全行の算出にも使う。
func (t *item) fullBox(colored bool) []string {
	mark, color := "✓", sgr.Green
	switch {
	case t.info:
		mark, color = "…", sgr.Cyan
	case !t.ok:
		mark, color = "✗", sgr.Red
	}
	row := mark + " " + t.text
	if colored {
		row = color + row + sgr.Reset
	}
	boxW := termwidth.Of(row) + layout.PanelChrome
	// 枠線も種別色 (成功=緑 / 失敗=赤 / 進行=シアン) で染めて一体感を出す。影は中立の dim のまま。
	return layout.Panel("", []string{row}, boxW, colored, layout.PanelStyle{Border: layout.BorderLight, Color: color, Shadow: t.shadow})
}

// boxLines は現フレームで見せる箱行 (全行) を返す。各行を箱の左 shown カラムに切り、右端揃えで
// overlay されると「右画面外から左へ滑り込む/右へ滑り出る」横スライドになる。左カラム切りで開いた
// SGR は行末で閉じる (右端揃え合成の背景に色がにじまないように)。非表示なら nil。
func (t *item) boxLines(colored bool) []string {
	if t.phase == Hidden {
		return nil
	}
	full := t.fullBox(colored)
	if len(full) == 0 {
		return nil
	}
	// 箱幅は full から直に導く (boxWidth を呼ぶと fullBox をもう一度組んでしまう。表示中は毎フレーム
	// 走るので二重構築を避ける)。
	v := min(max(t.shown, 0), termwidth.Of(full[0]))
	if v <= 0 {
		return nil
	}
	out := make([]string, len(full))
	for i, row := range full {
		clipped := termwidth.Cut(row, v) // 箱の左 v カラム (右側は画面右端の外へ)
		if colored {
			clipped += sgr.Reset
		}
		out[i] = clipped
	}
	return out
}

// StackMax は同時に積む枚数の上限。🚨 上限が無いと通知が連続したとき画面を覆う (トーストは
// 内容の上に重なるので、下の一覧・本文が読めなくなる)。溢れたら一番古い (一番下) を捨てる。
const StackMax = 3

// toast は右下の通知スタック。新しい通知は**上に積まれ**、古い通知は**下から抜けていく**
// (ユーザー要望 2026-07-31)。
//
// 以前は 1 枠の後勝ちで、新しい通知が出るたび前の通知が消えていた。そのため「今それを消したくない」
// 場面ごとに呼び出し側が調停する必要があり、同じ問題に 3 つの実装ができていた (claude version 通知の
// 専用タイマー付き遅延再送 / autobuild の pending 保持 / 残り全部は調停なしの即上書き)。実測では
// 「新しい glogx をビルド中」の通知がコピー操作 1 回で消えていた。積めるようにすればどの経路も
// 素直に show() を呼ぶだけで済み、調停そのものが要らなくなる。
//
// 🚨 最新の 1 枚を埋め込みで持つ: 呼び出し側とテストが t.text / t.ok / t.phase を直接読む箇所が
// 多数あり (テストだけで ~150 箇所)、埋め込みなら「最新の通知」を指す既存の読み方をそのまま
// 保てる。older は上から 2 枚目以降 (index 0 が上寄り = 新しい側、末尾が一番下 = 最古)。
type Stack struct {
	item
	older  []item
	seqGen int // 世代の採番 (退場タイマーの取り違え防止。枚数に依らず単調増加)
	// Shadow は箱の落ち影の SGR 色 (アプリのテーマの近黒など)。空なら layout.Panel の既定
	Shadow string
}

// Show は新しい通知を最上段に積む (ok=true は成功 ✓緑、false は失敗 ✗赤)。呼び出し側で tick を回すこと (Animating)。
func (s *Stack) Show(text string, ok bool) { s.push(text, ok, false) }

// ShowInfo は進行中/中立の通知 (…シアン) を積む。次の通知が来たら退く。
func (s *Stack) ShowInfo(text string) { s.push(text, false, true) }

func (s *Stack) push(text string, ok, info bool) {
	// 🚨 ここで無害化する: 通知文は gh / git のエラー出力・claude のバージョン文字列といった
	// 外部由来をそのまま埋め込む呼び出しが多く (showWarning 経由だけで 10 箇所以上)、
	// 呼び出しごとに包むと必ずどこかが漏れる。status_view / issues_view の setNotice と同じ規律。
	text = termsafe.PlainLine(text)
	s.seqGen++
	// 進行中トースト (…シアン) は「結果が出たら用済み」なので、新しい通知が来たら退かせる。
	// 🚨 積んだままにすると「PR を検索中...」の下に「PR #123 を開きます」が並び、終わったのに
	// 検索中と書いてある状態が数秒残る (実測 2026-07-31)。1 枠時代は上書きで自然に消えていた。
	s.dropInfo()
	if s.visible() {
		// 今の最新を 1 段下へ押し下げてから、新しいものを最上段に置く
		s.older = append([]item{s.item}, s.older...)
	}
	var top item
	top.reset(text, ok, info, s.seqGen, s.Shadow)
	s.item = top // 最上段を新しい 1 枚に差し替える
	if len(s.older) > StackMax-1 {
		s.older = evictOne(s.older)
	}
}

// important は「落とすなら最後にすべき通知」か。失敗・拒否理由 (ok も info も立っていない) を
// 重要とみなす。🚨 **追い出し (evictOne) と描画予算の選別 (boxLines) がこの 1 箇所を共有する** —
// 保持の規則と表示の規則が別々だと「保持しているのに重要な通知だけ画面に出ない」状態ができる
// (実測 2026-08-13: 狭い端末で最古の警告が描かれなかった)。
// 🚨 ただし**この判定は「最新は落とさない」を含まない**。最新の保護は呼び出し側の責務:
// evictOne は対象 (s.older) が構造的に最新を含まないので不要だが、boxLines は最新を含む列を
// 回すため index 0 を選別から外す必要がある。共有するのは重要度の判定だけで、保護の半分まで
// 共有された気にならないこと (issue 057: ここを見落として最新の成功/進行中が描かれなくなった)。
func (t *item) important() bool { return !t.ok && !t.info }

// evictOne は溢れた 1 枚を捨てる。🚨 年齢だけで捨てると重要な通知が消える: 起動時の警告
// (ok=false) の後に成功通知が 3 回来ると警告が落ちていた (実測 2026-08-13)。
// issue 028 P2 が要求していた「重要度 error > info の逆転を防ぐ」を、スタックの追い出し規則として
// 満たす — **成功/進行中 (ok または info) の最古を先に捨て、全部が警告なら最古を捨てる**。
// 同じ重要度どうしは従来どおり年齢順 (後勝ち) で、その性質は変えない。
//
// 🚨 これは「保持」の規則。実際に描かれる枚数は別に描画予算 (boxLines の maxLines) が握るので、
// 保持していても画面に出ないことはある (狭い端末での既知の穴。StackMax は保持数であって
// 表示数ではない)。
func evictOne(older []item) []item {
	drop := len(older) - 1 // 既定は最古
	for i := len(older) - 1; i >= 0; i-- {
		if !older[i].important() {
			drop = i // 重要でない最古を優先して捨てる
			break
		}
	}
	return append(older[:drop], older[drop+1:]...)
}

// dropInfo は進行中トースト (info) を取り除く (結果が出たら用済み)。
func (s *Stack) dropInfo() {
	kept := s.older[:0]
	for i := range s.older {
		if !s.older[i].info {
			kept = append(kept, s.older[i])
		}
	}
	s.older = kept
	// 最上段が info ならそこを空けて、下があれば繰り上げる
	if s.info {
		s.item = item{}
		if len(s.older) > 0 {
			s.item, s.older = s.older[0], s.older[1:]
		}
	}
}

// items は上から下の順に、表示中の 1 枚ずつを返す。
//
// 毎フレーム経路 (spinnerActive → animating / visible) はこれを使わず直接判定にしている。
// 🚨 「slice の alloc を避けるため」ではない — 実測では escape analysis が効いて items() 経由でも
// 0 allocs だった (2.99ns vs 1.86ns/回)。1 フレームに複数回通る判定を単純に保つだけの意図で、
// 性能上の効果はほぼ無い (フレームは ~200µs)。
func (s *Stack) items() []*item {
	out := make([]*item, 0, len(s.older)+1)
	if s.visible() {
		out = append(out, &s.item)
	}
	for i := range s.older {
		out = append(out, &s.older[i])
	}
	return out
}

// Animating は 1 枚でもスライド中か (tick を回す必要がある + spinnerActive に含める)。
// spinnerActive から毎フレーム呼ばれるので slice を作らない。
func (s *Stack) Animating() bool {
	if s.animating() {
		return true
	}
	for i := range s.older {
		if s.older[i].animating() {
			return true
		}
	}
	return false
}

// Advance は全ての枚を 1 フレーム進め、静止に入った枚の退場タイマーをまとめて返す。
// 抜け切った (hidden) 枚はここで取り除く = 下から抜けていく。
func (s *Stack) Advance(colored bool) []Timer {
	var timers []Timer
	if t := s.advance(colored); t != nil {
		timers = append(timers, *t)
	}
	kept := s.older[:0]
	for i := range s.older {
		if t := s.older[i].advance(colored); t != nil {
			timers = append(timers, *t)
		}
		if s.older[i].visible() {
			kept = append(kept, s.older[i])
		}
	}
	s.older = kept
	// 最新が抜け切ったのに下がまだ残っている場合 (下が先に抜ける通常順序の例外) は繰り上げる。
	// 埋め込みが「最新の 1 枚」を指す不変条件を保つため。
	if !s.visible() && len(s.older) > 0 {
		s.item = s.older[0]
		s.older = s.older[1:]
	}
	return timers
}

// StartLeaving は静止時間が明けた枚を退場へ移す (seq で該当の枚を選ぶ)。
func (s *Stack) StartLeaving(msg Msg) {
	for _, it := range s.items() {
		it.startLeaving(msg)
	}
}

// BoxHeight は 1 枚の箱の行数 (上罫線 + 内容 1 行 + 下罫線 + 落ち影)。fullBox が内容 1 行で
// layout.Panel を呼ぶので一定。🚨 箱の形を変えたらここも直す (テストで pin してある)。
const BoxHeight = 4

// BoxLines はスタック全体の描画行 (上から下)。maxLines に入らない枚は出さない。
// 予算の下限は呼び出し側 (toastDrawBudget) が窓の高さに応じて確保する。
//
// 🚨 行数の上限が要る: 低い端末では 3 枚 (12 行) が窓 (11 行) を超え、一番下の箱が途中で切れて
// 壊れて見えた (実測 2026-07-31: 窓 11 行に対し 12 行)。枚数の上限 (StackMax) だけでは
// 窓の高さに対する占有を抑えられない。最新の 1 枚は上限を超えても出す — 見えない通知より
// 「窓を覆うが読める通知」を選ぶ。
//
// 🚨 **落とす順は追い出し (evictOne) と同じ規則**にする: 重要でない (成功/進行中) 枚を先に落とし、
// 同じ重要度なら古い方から。保持と表示で規則が違うと「保持はしているのに重要な通知だけ画面に
// 出ない」状態ができる (実測 2026-08-13: 警告の後に成功通知が来た狭い端末で、警告が 1 行も
// 描かれなかった)。🚨 残す枚の並び順は元のまま (上が新しい) — 重要な枚を上へ繰り上げると、
// スタックの「新しいものが上」という読み方が崩れる。
func (s *Stack) BoxLines(colored bool, maxLines int) []string {
	items := s.items()
	boxes := make([][]string, 0, len(items))
	shown := make([]*item, 0, len(items))
	for _, it := range items {
		box := it.boxLines(colored)
		if len(box) == 0 {
			continue // まだ滑り込み前 (幅 0) は予算を食わない
		}
		boxes = append(boxes, box)
		shown = append(shown, it)
	}
	fit := max(maxLines/BoxHeight, 1) // 最新の 1 枚は上限を超えても出す
	// 🚨 i >= 1 で止めて最新 (index 0) を重要度選別から外す。「最新の 1 枚は出す」は
	// この保護 + 最終段の先頭切りの 2 つで成る: fit の下限 1 だけでは「残る 1 枚が最新」を
	// 保証しない (実測 2026-08-14: 最新が成功/進行中だとここで落ち、押したキーの
	// フィードバックが古い警告に覆い隠された。issue 057)。evictOne はこの保護を持たなくて
	// よい — あちらの対象 s.older は構造的に最新を含まない (important() の doc)
	for i := len(boxes) - 1; i >= 1 && len(boxes) > fit; i-- {
		if !shown[i].important() {
			boxes = append(boxes[:i], boxes[i+1:]...)
			shown = append(shown[:i], shown[i+1:]...)
		}
	}
	if len(boxes) > fit {
		boxes = boxes[:fit] // 全部重要なら古い方から落とす
	}
	var out []string
	for _, box := range boxes {
		out = append(out, box...)
	}
	return out
}

// Visible は 1 枚でも表示中か。
func (s *Stack) Visible() bool { return s.visible() || len(s.older) > 0 }

// Text / OK / Info / Phase / Shown は最新 (最上段) の 1 枚の中身と状態 (画面とテストが「最新の通知」を読むため)。
func (s *Stack) Text() string { return s.text }
func (s *Stack) OK() bool     { return s.ok }
func (s *Stack) Info() bool   { return s.info }
func (s *Stack) Phase() Phase { return s.phase }
func (s *Stack) Shown() int   { return s.shown }

// Entry は積まれている 1 枚の中身。
type Entry struct {
	Text     string
	OK, Info bool
}

// Entries は表示中の枚を上から (新しい順に) 返す。
func (s *Stack) Entries() []Entry {
	items := s.items()
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		out = append(out, Entry{Text: it.text, OK: it.ok, Info: it.info})
	}
	return out
}

// Clear は積んでいる通知をすべて消す (アニメなしで即座に)。
func (s *Stack) Clear() {
	shadow, seq := s.Shadow, s.seqGen
	*s = Stack{Shadow: shadow, seqGen: seq} // 世代は巻き戻さない (消す前の退場タイマーが後の通知を消さないように)
}

// Seq は最新の 1 枚の世代 (Msg.Seq と比べて、その退場タイマーが今の通知のものかを見る)。
func (s *Stack) Seq() int { return s.seq }

// Seq はこの退場タイマーが属する通知の世代。
func (m Msg) Seq() int { return m.seq }
