package main

import "math"

// scrollGlide は「表示 offset を論理 offset へ数フレームで滑らせる」状態機械。コミット一覧 /
// diff pager / issues 本文の 3 面が共有する。
//
// 元は一覧の j/k 専用 (browseModel のフィールド 4 本 + advanceScroll) で、半ページ移動
// (Space / ctrl+d / pgdown) は snap だった。半ページもアニメにしたいという要望 (2026-07-31) に
// 対し、面ごとに同じ状態機械をコピーすると「カーブ・フレーム数・連打時の扱い」が散って必ず
// 食い違うため、型として切り出して 1 箇所に集約した。
//
// 🚨 issues の一覧には載せない (issue 031): あの面は cursor と窓を同時に動かし、窓は
// 「カーソルを含む最小の窓」の導出値なので、遅らせる余地が幾何的にゼロ (載せても瞬時に着地点へ
// 張り付くだけだった)。カーソルを持つ面へ広げるときは、まず「窓を遅らせてよいか」を確かめること。
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

// scrollAnimFrames は glide の総フレーム数 (× scrollInterval 16ms ≒ 100ms)。少ないほど速い。
// 30fps 化 (12.5→30fps) に合わせて 3→6 に増やし、同程度の duration で ease-in カーブの刻みを
// 細かく = 滑らかにした。
// 🚨 2 倍速化 (2026-09-05) はここを削らず scrollInterval を半分 (33→16ms) にして達成した。
// フレーム数がそのまま滑らかさなので、6 → 3 にすると ease-in が 3 段しか出ず TestScrollGlideOffset
// が落ちる。以後もここは「速さ」のつまみではなく「滑らかさ」のつまみとして扱う。
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

// pagerScrollKey は less 流儀のスクロールキーを offset へ写す共有ロジック。diff pager (d) と
// status viewer の全画面 diff が同じ手触りを持つための 1 箇所。🚨 「閉じる」キーはここで扱わない:
// 面ごとに閉じる語彙が違う (diff は d / status は d と q) ため、呼び出し側で判定してから渡す。
//
// 半ページ移動だけ glide に載せるのは diffOverlay.scroll から引き継いだ判断: 1 行移動は距離 1 行で
// 滑らせる意味が無く、端ジャンプ (g/G) は距離が不定なので即時のまま。
// スクロールキーでなければ offset をそのまま返す (呼び出し側は自分の語彙のキーを先に捌く)。
func pagerScrollKey(key string, offset, rows, total int, glide *scrollGlide) (newOffset int) {
	maxOffset := max(total-rows, 0)
	switch key {
	case "j", "down", "ctrl+n", "enter":
		return min(offset+1, maxOffset)
	case "k", "up", "ctrl+p":
		return max(offset-1, 0)
	case "ctrl+d", "pgdown", " ", "f":
		next := min(offset+rows/2, maxOffset)
		glide.start(offset, next)
		return next
	// 🚨 shift+space は「区別して送れる端末」でしか効かない。これはアプリ側で直せない —
	// 端末は shift が**文字を変えないキー**の修飾を落とすので、shift+space は素の 0x20 として
	// 届き、アプリからは Space と 1 バイトも違わない (shift+a が A になるのとは事情が違う。
	// space には shift 付きの符号が無い。矢印キーが shift 付きで届くのは、あちらが元から
	// エスケープシーケンスで ESC [ 1;2A という形式を持っているから)。
	// 修飾を復元するには kitty keyboard protocol か modifyOtherKeys が要り、bubbletea は起動時に
	// 両方を要求しているが、非対応の端末 (macOS の Terminal.app) は応じない。つまり下流
	// (tmux / この関数) では何をしても区別できないので、**この case を厚くしても解決しない**。
	// 上スクロールの確実な経路は b / ctrl+u / pgup なので、この 3 つを消さないこと。
	// 実測と切り分け手順は docs/issues-viewer-spec.md「半ページ移動はカーソルが滑る」の末尾。
	case "ctrl+u", "pgup", "b", "shift+space":
		next := max(offset-rows/2, 0)
		glide.start(offset, next)
		return next
	case "g", "home":
		glide.stop()
		return 0
	case "G", "end":
		glide.stop()
		return maxOffset
	}
	return offset
}

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

// windowOffsetFor は「カーソルを含む窓」へ offset を収束させる (キー処理と描画で行数が
// 食い違っても、カーソルが画面外に出ない)。clampScrollOffset と同じく「offset は導出値」の
// 規律の一部で、status viewer・issues viewer・commit 一覧が同じ式を通る。
func windowOffsetFor(offset, cursor, total, rows int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}
	return clampScrollOffset(offset, total, rows)
}

// cursorAnimFrames は issues 一覧の半ページ移動でカーソルが滑るフレーム数
// (× scrollInterval 16ms ≒ 320ms)。100ms / 200ms / 320ms の 3 案から 320ms をユーザーが選定
// (2026-09-17)。scrollAnimFrames (100ms) より長いのは、こちらが「移動したことを見せる」演出で、
// あちらが「飛んだことを馴染ませる」演出だから (主役が違うので同じ数にしない)。
const cursorAnimFrames = 20

// cursorGlide は「表示カーソルを論理カーソルへ数フレームで滑らせる」状態機械。issues 一覧の
// 半ページ移動 (Space / ctrl+d / b / ctrl+u / pgup / pgdown) だけが使う。
//
// 🚨 scrollGlide (窓を遅らせる) とは別物なので共通化しない。一覧の窓は「カーソルを含む最小の窓」の
// 導出値なので遅らせる余地が幾何的にゼロで、遅らせると描かれていない行が Enter/y/v の対象になる
// (issue 031 の実測と敵対レビュー P2)。こちらは窓に触らず**カーソルの描画位置だけ**を遅らせるので、
// 031 の不変条件 (窓は論理カーソルを必ず含む) が滑走中も生きたままになる。論理カーソル
// (v.cursor) は即着地するので、Enter/y/v/e は常に着地点へ効く。
type cursorGlide struct {
	fromCursor int  // glide 開始時のカーソル行 (補間の起点)
	shown      int  // 現在の表示カーソル (active のときだけ意味を持つ。範囲外を取りうる)
	frame      int  // 経過フレーム数
	active     bool // glide 中か (tick を回す必要がある = animating に含める)
}

// start は fromCursor から target への glide を開始する。距離 0 では何もしない。
//
// 進行中の glide を積まないための分岐は置かない: 呼び出し側 (handleKey) が finishAnim で
// 必ず先に着地させるので、ここへ来る時点で active は落ちている (scrollGlide の連打対策と
// 同じ結果を、状態を 1 つ減らして得ている)。
func (g *cursorGlide) start(fromCursor, target int) bool {
	if fromCursor == target {
		return false // カーソルは動いていない (端で止まったとき)
	}
	*g = cursorGlide{fromCursor: fromCursor, shown: fromCursor, active: true}
	return true
}

// advance は glide を 1 フレーム進める。最終フレームで論理カーソルへスナップして active を下ろす。
func (g *cursorGlide) advance(target int) {
	g.frame++
	if g.frame >= cursorAnimFrames {
		g.shown, g.active = target, false
		return
	}
	t := float64(g.frame) / float64(cursorAnimFrames)
	dist := float64(target - g.fromCursor)
	g.shown = g.fromCursor + int(math.Round(dist*cursorEaseOutBack(t)))
}

// cursor は描画に使うカーソル行を返す (glide 中は途中位置、それ以外は論理カーソル)。
// 途中位置は行数の範囲外を取りうる (下の行き過ぎ) ので、呼び出し側が clamp する。
func (g *cursorGlide) cursor(target int) int {
	if !g.active {
		return target
	}
	return g.shown
}

// stop は glide を捨てて即時表示へ倒す (次のキー・行集合の入れ替え・resize)。
func (g *cursorGlide) stop() { g.active = false }

// cursorEaseOutBack は ease-out-back (最初速く、着地の手前で 1〜2 行だけ行き過ぎて戻る)。
// t=0 で 0、t=1 で 1。行き過ぎの量は s が決める (大きいほど大きく戻る)。
//
// 半ページ (通常 10 行以上) では行き過ぎが 1〜2 行ぶん見えるが、移動距離が 3 行以下だと
// 四捨五入で 0 行になり素直な減速に見える。端に着地するときも clamp で行き過ぎは出ない。
func cursorEaseOutBack(t float64) float64 {
	const s = 1.25
	u := t - 1
	return 1 + u*u*((s+1)*u+s)
}
