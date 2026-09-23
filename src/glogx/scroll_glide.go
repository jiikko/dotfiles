package main

import "tuikit/anim"

// 表示 offset / カーソルを数フレームで滑らせる glide の、glogx 側の設定とキー語彙。
// 状態機械そのものは tuikit の anim.ScrollGlide / anim.CursorGlide が持つ。
//
// scrollGlide はコミット一覧 / diff pager / issues 本文 / status viewer の pager が共有する。
// 面ごとに状態機械をコピーすると「カーブ・フレーム数・連打時の扱い」が散って必ず食い違う。
//
// 🚨 issues の一覧には scrollGlide を載せない (issue 031): あの面は cursor と窓を同時に動かし、
// 窓は「カーソルを含む最小の窓」の導出値なので、遅らせる余地が幾何的にゼロ (載せても瞬時に
// 着地点へ張り付くだけだった)。一覧は cursorGlide でカーソルの描画位置だけを滑らせる。

// scrollAnimFrames は glide の総フレーム数 (× scrollInterval 16ms ≒ 100ms)。少ないほど速い。
// 30fps 化 (12.5→30fps) に合わせて 3→6 に増やし、同程度の duration で ease-in カーブの刻みを
// 細かく = 滑らかにした。
// 🚨 2 倍速化 (2026-09-05) はここを削らず scrollInterval を半分 (33→16ms) にして達成した。
// フレーム数がそのまま滑らかさなので、6 → 3 にすると ease-in が 3 段しか出ず TestScrollGlideOffset
// が落ちる。以後もここは「速さ」のつまみではなく「滑らかさ」のつまみとして扱う。
const scrollAnimFrames = 6

// pagerScrollKey は less 流儀のスクロールキーを offset へ写す共有ロジック。diff pager (d) と
// status viewer の全画面 diff が同じ手触りを持つための 1 箇所。🚨 「閉じる」キーはここで扱わない:
// 面ごとに閉じる語彙が違う (diff は d / status は d と q) ため、呼び出し側で判定してから渡す。
//
// 半ページ移動だけ glide に載せるのは diffOverlay.scroll から引き継いだ判断: 1 行移動は距離 1 行で
// 滑らせる意味が無く、端ジャンプ (g/G) は距離が不定なので即時のまま。
// スクロールキーでなければ offset をそのまま返す (呼び出し側は自分の語彙のキーを先に捌く)。
func pagerScrollKey(key string, offset, rows, total int, glide *anim.ScrollGlide) (newOffset int) {
	maxOffset := max(total-rows, 0)
	switch key {
	case "j", "down", "ctrl+n", "enter":
		return min(offset+1, maxOffset)
	case "k", "up", "ctrl+p":
		return max(offset-1, 0)
	case "ctrl+d", "pgdown", " ", "f":
		next := min(offset+rows/2, maxOffset)
		glide.Start(offset, next, scrollAnimFrames)
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
		glide.Start(offset, next, scrollAnimFrames)
		return next
	case "g", "home":
		glide.Stop()
		return 0
	case "G", "end":
		glide.Stop()
		return maxOffset
	}
	return offset
}

// cursorAnimFrames は issues 一覧の半ページ移動でカーソルが滑るフレーム数
// (× scrollInterval 16ms ≒ 320ms)。100ms / 200ms / 320ms の 3 案から 320ms をユーザーが選定
// (2026-09-17)。scrollAnimFrames (100ms) より長いのは、こちらが「移動したことを見せる」演出で、
// あちらが「飛んだことを馴染ませる」演出だから (主役が違うので同じ数にしない)。
const cursorAnimFrames = 20
