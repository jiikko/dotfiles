package main

// scrollGlide は「表示 offset を論理 offset へ数フレームで滑らせる」状態機械。コミット一覧 /
// diff pager / issues 一覧 / issues 本文の 4 面が共有する。
//
// 元は一覧の j/k 専用 (browseModel のフィールド 4 本 + advanceScroll) で、半ページ移動
// (Space / ctrl+d / pgdown) は snap だった。半ページもアニメにしたいという要望 (2026-07-31) に
// 対し、面ごとに同じ状態機械をコピーすると「カーブ・フレーム数・連打時の扱い」が 4 箇所に散って
// 必ず食い違うため、型として切り出して 1 箇所に集約した。
//
// 論理 offset はこの型が持たない (各面が自分で持つ)。描画のときだけ offset() に通し、tick で
// advance() を呼ぶ。この分離により、論理 offset を動かす既存コード (clampOffset・
// ensureCursorVisible・moveCursor 等) は glide を意識しないままで済む。
type scrollGlide struct {
	from   int  // glide 開始時の表示 offset (ease-in の進捗基点)
	shown  int  // 現在の表示 offset (active のときだけ意味を持つ)
	frame  int  // 経過フレーム数
	active bool // glide 中か (tick を回す必要がある = spinnerActive に含める)
}

// scrollAnimFrames は glide の総フレーム数 (× scrollInterval 33ms ≒ 200ms)。少ないほど速い。
// 30fps 化 (12.5→30fps) に合わせて 3→6 に増やし、同程度の duration で ease-in カーブの刻みを
// 細かく = 滑らかにした。
const scrollAnimFrames = 6

// start は prev から target (現在の論理 offset) への glide を開始する。開始したら true。
//
// 距離 0 (ビューポートが動いていない) では何もしない。進行中の glide があれば積み上げずに
// 即時へ倒す: アニメの積み上げは「押した分だけ遅れて動く」最悪の体感を生むため (連打対策)。
func (g *scrollGlide) start(prev, target int) bool {
	if prev == target {
		return false // ビューポートは動いていない (進行中の glide はそのまま continue させる)
	}
	if g.active {
		g.active = false // 連打中は積まず即時 (描画は論理 offset に戻る)
		return false
	}
	g.from, g.shown, g.frame, g.active = prev, prev, 0, true
	return true
}

// advance は glide を 1 フレーム進める。ease-in (二次 t^2) で「最初ゆっくり → 終盤に加速」する
// (ユーザー要望 2026-07-21)。進捗は開始位置 from からの経過フレーム割合
// t=frame/scrollAnimFrames で測り、表示 offset = from + dist*t^2。最終フレームで論理 offset へ
// スナップして active を下ろす。
//
// target を毎フレーム受け取るのは、glide 中に論理 offset が動いても (resize・追加ロード) その
// 時点の着地点へ向かうため。カーブを変えるならここ: t*(2-t) で ease-out (最初速く減速)、t で等速。
func (g *scrollGlide) advance(target int) {
	dist := target - g.from
	g.frame++
	if dist == 0 || g.frame >= scrollAnimFrames {
		g.shown, g.active = target, false
		return
	}
	// prog = round(|dist| * frame^2 / scrollAnimFrames^2)。符号は dist に合わせる (上下対称)
	mag := dist
	if mag < 0 {
		mag = -mag
	}
	f, total := g.frame, scrollAnimFrames
	prog := (mag*f*f*2 + total*total) / (2 * total * total) // round-half-up
	if dist < 0 {
		prog = -prog
	}
	g.shown = g.from + prog
}

// offset は描画に使う offset を返す。glide 中は途中位置、それ以外は論理 offset (target)。
func (g *scrollGlide) offset(target int) int {
	if !g.active {
		return target
	}
	return g.shown
}

// stop は glide を捨てて即時表示へ倒す (g/G のジャンプ・resize・pull リロード等)。
func (g *scrollGlide) stop() { g.active = false }

// clampScrollOffset は pager の offset を 0..max(total-rows, 0) へ収める。
//
// 「offset は独立した状態ではなく (カーソル・行数・表示行数) からの導出値」という規律を 1 箇所に
// 置くための関数。この式は論理 offset の収束 (キー処理・描画で行数が食い違ってもカーソルを含む窓に
// 落とす) と glide の途中位置の両方に効くため、手書きすると同じ面の中でも 2〜3 箇所に散る。
// 散った状態で上限の決め方を変えると (例: 末尾に余白を許す)、片方だけ直して「G が末尾に届かない」
// 「k を押しても動かない打鍵が生まれる」形で静かに壊れる。
func clampScrollOffset(offset, total, rows int) int {
	return max(min(offset, max(total-rows, 0)), 0)
}
