# 524 (refactor): pro-con と schedkeys の端末文字列の幅・切り詰め・切り出しを tuikit/termwidth に寄せる

起票日: 2026-09-26

親: [523](523-design-asm-fast-paths.md)

## 概要

asm の速い道を入れる場所を 1 つにするため、`x/ansi` を直接呼んでいる所を `src/tuikit/termwidth` 経由にする (523 の段 1)。
あわせて、同じ行を何度も先頭から走査している所を 1 回にする (520 の Phase 1)。pure Go だけで、asm は入れない。

## 対象 (2026-09-26 の数。`grep -rn 'ansi.StringWidth\|ansi.Cut\|ansi.Truncate' --include='*.go' src` の production)

- pro-con 44 か所 (`ui/view.go` の `fit`・`ui/motion.go` の `splice`・`ui/cursor.go` の `cellsOf` が同じ行を何度も走査する)
- schedkeys 25 か所
- tuikit の中 22 か所・glogx 9 か所・ratelimit 1 か所も、寄せる価値があるかを見る (glogx は既に `termwidth.Of` を使う所がある)

## 対応方針

- `termwidth` に、今 `x/ansi` を直接呼んでいる操作 (幅・切り詰め・切り出し) の口を足す。中身は今の `x/ansi` を呼ぶ (振る舞いを変えない)
- `fit` / `splice` / `cellsOf` のように同じ行を何度も走査する所は、1 回の走査で幅と切れ目を返す口へ寄せる
- 寄せた後に、`x/ansi` の直接の呼び出しを増やさない検査を置くかを決める (lint か grep の検査)

## 受け入れ条件

- [ ] pro-con と schedkeys の production から `ansi.StringWidth` / `Cut` / `Truncate` の直接の呼び出しが無くなる (残すなら理由をその行に書く)
- [ ] 表示が変わらない (既存のテストと、pro-con の見本の .ans の一致)
- [ ] 寄せる前と後で、pro-con の演出のフレームと glogx の View の benchmark を測って記録する (523 の段 2 の材料)
- [ ] `go test ./...` と `make lint` が通る (pro-con・schedkeys・tuikit)

## 関連ファイル

- `src/tuikit/termwidth/termwidth.go` / `src/pro-con/ui/view.go` / `src/pro-con/ui/motion.go` / `src/pro-con/ui/cursor.go` / `src/schedkeys/`

## 順番の見積もり (PM, 2026-09-26。C-081 は順番を付けずに積んだ)

- 触る場所: pro-con の `ui/` の広い範囲 (`fit` / `splice` / `cellsOf` と `ansi.*` の直接の呼び出し 44 か所)、schedkeys、`tuikit/termwidth`
- 変える判断: 「端末の文字列の幅・切り詰め・切り出しは `termwidth` を通す」という約束 (振る舞いは変えない)
- 順番を付けなかった理由: 画面を触るカードで未着手なのは C-079 (519。C-078 と見た目の選択を待つ) だけで、すぐには動かない。レビュー中のカード (C-072 / C-074 / C-078 / C-080) は先に入る見込み
- 🚨 並べた代わりに: 後から入るカードが `x/ansi` の直接の呼び出しを持ち込むと、受け入れ条件 (直接の呼び出しが無い) が黙って崩れる。
  **「直接の呼び出しを増やさない検査」(対応方針の 3 つ目) を入れる方に倒す**と、後のカードもそこで止まる。review の前に origin/master へ rebase して、数え直してから出す

## 進捗

(まだ無い)
