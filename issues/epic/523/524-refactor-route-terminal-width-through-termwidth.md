# 524 (refactor): pro-con と schedkeys の端末文字列の幅・切り詰め・切り出しを tuikit/termwidth に寄せる (TUI の画面と CLI の出力の両方)

起票日: 2026-09-26

親: [523](523-design-asm-fast-paths.md)

## 概要

asm の速い道を入れる場所を 1 つにするため、`x/ansi` を直接呼んでいる所を `src/tuikit/termwidth` 経由にする (523 の段 1)。
あわせて、同じ行を何度も先頭から走査している所を 1 回にする (520 の Phase 1)。pure Go だけで、asm は入れない。

対象は TUI の画面に限らない (2026-09-26 のユーザーの確認「cui でも有効じゃないの」)。`termwidth` は描画の部品に依存しない関数で、
端末に出す文字列ならどこでも同じ数え方が要る。CLI の出力 (`ratelimit` の `usage/render.go` など) は既に `termwidth` を通している。

## 対象 (2026-09-26 の数。`grep -rn 'ansi.StringWidth\|ansi.Cut\|ansi.Truncate' --include='*.go' src` の production)

- pro-con 44 か所 (`ui/view.go` の `fit`・`ui/motion.go` の `splice`・`ui/cursor.go` の `cellsOf` が同じ行を何度も走査する)
- schedkeys 25 か所
- pro-con の CLI の出力: `cardview.go` (`card show` の表) が `ansi.StringWidth` を直接呼ぶ (上の 44 か所に含む)。`pscmd.go` など表を出すコマンドも見る
- tuikit の中 22 か所・glogx 9 か所・ratelimit 1 か所も、寄せる価値があるかを見る (glogx は既に `termwidth.Of` を使う所がある)

## 対応方針

- `termwidth` に、今 `x/ansi` を直接呼んでいる操作 (幅・切り詰め・切り出し) の口を足す。中身は今の `x/ansi` を呼ぶ (振る舞いを変えない)
- `fit` / `splice` / `cellsOf` のように同じ行を何度も走査する所は、1 回の走査で幅と切れ目を返す口へ寄せる
- 寄せた後に、`x/ansi` の直接の呼び出しを増やさない検査を置くかを決める (lint か grep の検査)

## 受け入れ条件

- [ ] pro-con (画面と CLI の両方) と schedkeys の production から `ansi.StringWidth` / `Cut` / `Truncate` の直接の呼び出しが無くなる (残すなら理由をその行に書く)
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

### 2026-09-26 (C-081): 寄せた

- `termwidth` に口を足した: `TruncateMeasure` (切った結果の幅も返す) / `Slice` (= `ansi.Cut`) / `SliceFrom` / `SplitAround` (差し込みの左右を 1 回の幅測りで返す)。
  中身は x/ansi のまま。**収まる行は速い道 (`fastDispWidth`) の 1 回で返す**: `ansi.Truncate` は収まる行でも冒頭で `ansi.StringWidth` (grapheme の走査) を 1 回走らせるので、そこを省いた
- x/ansi と違うのは 1 点だけ: `ansi.Truncate` の結果が `Of` で幅をはみ出す入力 (キーキャップ。issue 416) で、はみ出さない側へ直す (既存の `termwidth.Truncate` と同じ契約)。
  それ以外は `ansi.Truncate` / `ansi.Cut` と一致することを `termwidth/slice_test.go` が差分で確かめる (width <= 0・SGR だけの行・範囲外・全角の跨ぎを含む。はみ出す入力は「幅を 1 ずつ下げる素朴な参照」と完全一致を見る。変異 6 種で red)
- pro-con・schedkeys・tuikit/markdown の直呼びを寄せた (production の残りは 0。コメントの中の言及だけ残る)。
  `fit` / `splice` / `cellsOf` / `wrapLines` / toast の重ね合わせは同じ行の測り直しを減らした。schedkeys の `fitWidth` は `termwidth.Cut` と同じことをしていたので消した
- glogx (production の直呼びは 0。`tools/width-probe` はライブラリを比べるのが仕事) と ratelimit (コメントだけ) は触っていない
- 再発防止: pro-con / schedkeys / tuikit (termwidth 自身とテストを除く) の forbidigo で `ansi.(StringWidth|Cut|Truncate|TruncateLeft)` を禁止した
- 表示: pro-con の演出 (揺れ・枠の滑走 l / j・カードの移動) の全コマと `? r tab` の画面を幅 200 / 97 / 61 / 40 で書き出し、寄せる前と byte 単位で一致 (296 コマ・5.4MB)。
  `samples/` の .ans は python で作った見本で Go の出力ではないので、突き合わせの対象にしていない
- 敵対的レビュー (部品 23 種の全組み合わせ 29 万本で新旧を突き合わせ) の指摘で 2 点直した: はみ出しの直しで t = 0 の結果を ""
  と決め打ちしていた (ansi は幅 0 の SGR・タブを残す。旧 schedkeys の fitWidth と幅 1 で割れた) / キーキャップの入力で完全一致を飛ばしていたテスト。
  残した差: schedkeys の viewport で `SliceFrom` は左端が末尾以降なら ""、旧 `ansi.TruncateLeft` は SGR だけを残す。入力は
  `acceptable()` が制御文字を弾くので本番の値には ESC が入らない (テストの setValue だけ)
- 測定: BENCH_PLACEHOLDER
