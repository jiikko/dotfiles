package main

import (
	"tuikit/listnav"
)

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
// キーの語彙・offset の計算・glide を立てるかの判断は tuikit の listnav.Pager が持つ。ここに残すのは
// less の Enter (1 行送り) と glide のフレーム数だけ。shift+space が届かない端末がある件
// (上スクロールの確実な経路は b / ctrl+u / pgup) の実測と切り分けは
// docs/issues-viewer-spec.md「半ページ移動はカーソルが滑る」の末尾。
// スクロールキーでなければ何もしない (呼び出し側は自分の語彙のキーを先に捌く)。
func pagerScrollKey(key string, p *listnav.Pager, rows, total int) {
	m := listnav.MotionOf(key)
	if key == "enter" {
		m = listnav.Down // less 流儀
	}
	p.Move(m, total, rows, scrollAnimFrames)
}

// cursorAnimFrames は issues 一覧の半ページ移動でカーソルが滑るフレーム数
// (× scrollInterval 16ms ≒ 320ms)。100ms / 200ms / 320ms の 3 案から 320ms をユーザーが選定
// (2026-09-17)。scrollAnimFrames (100ms) より長いのは、こちらが「移動したことを見せる」演出で、
// あちらが「飛んだことを馴染ませる」演出だから (主役が違うので同じ数にしない)。
const cursorAnimFrames = 20
