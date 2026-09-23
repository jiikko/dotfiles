package anim

import "math"

// ScrollGlide は「表示 offset を論理 offset へ数フレームで滑らせる」状態機械。zero value = 静止。
//
// 論理 offset はこの型が持たない (使う側が持つ)。描画のときだけ Offset に通し、tick で Advance を
// 呼ぶ。この分離により、論理 offset を動かす既存コード (clamp・カーソル追従) は glide を
// 意識しないままで済む。
//
// フレーム数は「速さ」ではなく「滑らかさ」のつまみ (ease-in の刻みの数)。速さは tick の周期で
// 決める。フレーム数を減らすと ease-in が数段しか出ずカクつく。
type ScrollGlide struct {
	from   int  // glide 開始時の表示 offset (進捗の基点)
	shown  int  // 現在の表示 offset (active のときだけ意味を持つ)
	frame  int  // 経過フレーム数
	frames int  // 総フレーム数
	active bool // glide 中か (使う側が tick を回す必要がある)
}

// Start は prev から target (現在の論理 offset) への glide を frames フレームで始める。
// 始めたら true。
//
// 距離 0 (ビューポートが動いていない) では何もしない。進行中の glide があれば積み上げずに
// 即時へ倒す: アニメの積み上げは「押した分だけ遅れて動く」最悪の体感を生むため (連打対策)。
func (g *ScrollGlide) Start(prev, target, frames int) bool {
	if prev == target {
		return false // 進行中の glide はそのまま続けさせる
	}
	if g.active {
		g.active = false // 連打中は積まず即時 (描画は論理 offset に戻る)
		return false
	}
	g.from, g.shown, g.frame, g.frames, g.active = prev, prev, 0, frames, true
	return true
}

// Advance は glide を 1 フレーム進める。ease-in (二次 t^2) で「最初ゆっくり → 終盤に加速」し、
// 最終フレームで論理 offset へスナップして止まる。
//
// target を毎フレーム受け取るのは、glide 中に論理 offset が動いても (resize・追加ロード) その
// 時点の着地点へ向かうため。
func (g *ScrollGlide) Advance(target int) {
	dist := target - g.from
	g.frame++
	if dist == 0 || g.frame >= g.frames {
		g.shown, g.active = target, false
		return
	}
	// prog = round(|dist| * frame^2 / frames^2)。符号は dist に合わせる (上下対称)
	mag := dist
	if mag < 0 {
		mag = -mag
	}
	f, total := g.frame, g.frames
	prog := (mag*f*f*2 + total*total) / (2 * total * total) // round-half-up
	if dist < 0 {
		prog = -prog
	}
	g.shown = g.from + prog
}

// Offset は描画に使う offset を返す。glide 中は途中位置、それ以外は論理 offset (target)。
func (g *ScrollGlide) Offset(target int) int {
	if !g.active {
		return target
	}
	return g.shown
}

// Active は glide 中か (tick を回し続ける判定に使う)。
func (g *ScrollGlide) Active() bool { return g.active }

// Stop は glide を捨てて即時表示へ倒す (端へのジャンプ・resize・リロード等)。
func (g *ScrollGlide) Stop() { g.active = false }

// CursorGlide は「表示カーソルを論理カーソルへ数フレームで滑らせる」状態機械。zero value = 静止。
//
// 🚨 ScrollGlide (窓を遅らせる) とは別物。窓が「カーソルを含む最小の窓」の導出値になっている
// 一覧では、窓を遅らせる余地が幾何的にゼロで、遅らせると描かれていない行が操作対象になる。
// こちらは窓に触らず**カーソルの描画位置だけ**を遅らせるので、「窓は論理カーソルを必ず含む」が
// 滑走中も生きたままになる。論理カーソルは即着地するので、決定キーは常に着地点へ効く。
type CursorGlide struct {
	from   int  // glide 開始時のカーソル行 (補間の起点)
	shown  int  // 現在の表示カーソル (active のときだけ意味を持つ。範囲外を取りうる)
	frame  int  // 経過フレーム数
	frames int  // 総フレーム数
	active bool // glide 中か
}

// cursorOvershoot は EaseOutBack の行き過ぎ量。半ページ (10 行以上) で 1〜2 行だけ行き過ぎ、
// 3 行以下の移動では四捨五入で 0 行になり素直な減速に見える。
const cursorOvershoot = 1.25

// Start は from から target への glide を frames フレームで始める。距離 0 では何もしない。
// 進行中の glide は捨てて新しい起点から始める (積み上げを避けたいなら、呼ぶ前に着地させる)。
func (g *CursorGlide) Start(from, target, frames int) bool {
	if from == target {
		return false // 端で止まったとき
	}
	*g = CursorGlide{from: from, shown: from, frames: frames, active: true}
	return true
}

// Advance は glide を 1 フレーム進める。最終フレームで論理カーソルへスナップして止まる。
func (g *CursorGlide) Advance(target int) {
	g.frame++
	if g.frame >= g.frames {
		g.shown, g.active = target, false
		return
	}
	t := float64(g.frame) / float64(g.frames)
	dist := float64(target - g.from)
	g.shown = g.from + int(math.Round(dist*EaseOutBack(t, cursorOvershoot)))
}

// Cursor は描画に使うカーソル行を返す (glide 中は途中位置、それ以外は論理カーソル)。
// 途中位置は行数の範囲外を取りうる (行き過ぎ) ので、使う側が clamp する。
func (g *CursorGlide) Cursor(target int) int {
	if !g.active {
		return target
	}
	return g.shown
}

// From は glide の起点 (開始時のカーソル行)。
func (g *CursorGlide) From() int { return g.from }

// Active は glide 中か。
func (g *CursorGlide) Active() bool { return g.active }

// Stop は glide を捨てて即時表示へ倒す。
func (g *CursorGlide) Stop() { g.active = false }
