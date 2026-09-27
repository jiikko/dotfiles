# 549 (perf): 放置フェードのランプ色でも段の鍵を使い回し、@fade-bucket の 2 回目の展開を消す

起票日: 2026-09-27

## 概要

PR #16 (issue 547) で、非 current window のセルは段の鍵 `@fade-key` をセルごとに 1 回だけ計算する形になった。
ただし **段 0〜4 (最近触った window) のときだけ**、`@fade-tpl` の中の `#{E:@fade-ramp-color}` が
`@fade-bucket` をもう 1 回展開している。`@fade-ramp-color` の中身が `#{E:@fade-bucket}` を含むため。
消灯 (段 5 以上) と busy はこの枝を通らない。

- 展開回数 (`display -v` の展開ログで `@fade-bucket` の本体を数えた、2026-09-27): 最近触った 2 回 / 消灯 1 回
- `tests/tmux/test_window_fade_render.sh` の `check_cost` は「@fade-bucket は 2 回まで」で、この 2 回目を許している

## 実測 (2026-09-27, macOS 実機)

環境・道具は issue 547 の「実測 2026-09-27 (macOS 実機)」節と同じ (M5 / tmux 3.7b / 隔離サーバ / 12 window × 3 pane)。
試作は `@fade-tpl` の `#[bg=#{E:@fade-ramp-color}]` を `#[bg=colour#{e|+:16,#{e|*:37,#{e|-:#{@fade-bucket-max},@K@}}}]`
に置き換えただけ。after / 試作 / after / 試作の順で 2 回ずつ。

| 指標 | 今 (`77faed68`) | 試作 | 変化 |
|---|---|---|---|
| セル 1 個の展開: 最近触った | 747 / 753µs | 450 / 450µs | −40% |
| 〃 消灯 / busy+claude | 223〜227 / 137µs | 227 / 140µs | 変わらない |
| 全面再描画 1 回のサーバ CPU (`refresh-client -S`) | 4.60 / 4.57ms | 3.70 / 3.70ms | −19% |
| hook `working` 1 回のサーバ CPU | 5.1ms | 4.2ms | −18% |

最近触った window が 450µs で、消灯 (225µs) のまだ 2 倍ある。残りは fg の dim 判定などの分と思われるが、内訳は測っていない。

## 対応方針

- `@fade-tpl` のランプ色を、鍵 `@K@` から直接組む。色の式の正本が `@fade-ramp-color` と `@fade-tpl` の 2 か所に分かれないようにする。
  例えば段を引数に取るテンプレート (`@fade-ramp-tpl`。`@K@` を置き換えて使う) を 1 つ置き、`@fade-ramp-color` の方は
  `#{s/@K@/#{E:@fade-bucket}/:@fade-ramp-tpl}` で作る
  (この形は `window-status-format` の `#{s/@FK@/#{E:@fade-key}/:@window-cell}` と同じ型だが、tmux での動作はまだ確かめていない。
  確かめるのは下の受け入れ条件の diff 0 の検査)
- 🚨 **`@fade-ramp-color` は消さない**。`after-select-window[1]` / `client-session-changed` の hook が、
  `scripts/tmux_ignite_current.sh` に渡す点火前の色としてこれを使っている (`_tmux.conf` の set-hook の行)
- `docs/tmux-window-fade.md` にある「bg の色式は `@fade-ramp-color` の 1 箇所」という記述を直す
- `test_window_fade_render.sh` の `check_cost` を「@fade-bucket は 1 回」に締める。締めたら、2 回目を戻すと red になることを確かめる

## 受け入れ条件

- [x] 描画が変わらない — 変更前 (origin/master) と変更後の conf で隔離サーバを 2 つ立て、216 通り (pane 1 / 3 × busy × zoom × claude の状態 4 種 × 経過 9 通り: 未スタンプ・段 0〜5・上限超え・負) の window-status-format と点火の開始色を比べて差分 0。bell は fade の式を通らないので組み合わせに入れていない。比較スクリプトの検出力は、色の係数を 37 → 36 にずらした conf で 72 通りの差分が出ることで確かめた (スクリプトは使い捨てで、commit していない)
- [x] ランプ色の式 (`colour#{e|+:16,#{e|*:37,…}}`) が `_tmux.conf` に 1 か所だけある — `grep -c` で 1 (`@fade-ramp-tpl`)。直す前は `@fade-hot-bg` (段 0 の色) にも同じ式があったので、これも `@fade-ramp-tpl` に段 0 を流し込む形に寄せた
- [x] 点火アニメの開始色 (`tmux_ignite_current.sh` の `$1`) が変わらない — hook と同じ式 `#{?#{E:@busy},#{E:@fade-hot-bg},#{E:@fade-ramp-color}}` を上の 216 通りで比べて差分 0
- [x] `check_cost` が @fade-bucket 1 回を固定し、2 回目を戻す変更を当てると red になる — 場面ごとの回数をぴったり固定 (busy 0 / 段 1 は 1 / 消灯 1 / 未スタンプ 0)。`@fade-tpl` を `#{E:@fade-ramp-color}` に戻す変異で「段 1 (ランプ色): @fade-bucket を 2 回展開した (期待 1)」の red を確認 (bin/mutate-verify)
- [x] before / after の実測を本文に書く — 下の「実測 (修正後)」

## 関連ファイル

- `_tmux.conf` (`@fade-ramp-color` / `@fade-tpl` / `@fade-key`、ignite の set-hook)
- `scripts/tmux_ignite_current.sh`
- `tests/tmux/test_window_fade_render.sh`
- `docs/tmux-window-fade.md`
- issue 547 (元の調査と PR #16 の実測)

## 進捗

- 2026-09-27: 起票。試作で効果を実測した (上表)。本体への変更はまだ
- 2026-09-27 (dotfiles-c2): 本体に入れた。色の式の正本を `@fade-ramp-tpl` (段を印 `@RK@` で受けるテンプレート) の 1 か所にし、
  `@fade-tpl` は `#{E:#{s/@RK@/@K@/:@fade-ramp-tpl}}` で計算済みの鍵を流し込む (外側の置き換えが `@K@` を鍵に書き換える。`@window-cell` の `@FK@` と同じ型)。
  `@fade-ramp-color` (点火の hook) は同じテンプレートに `@fade-bucket` を、`@fade-hot-bg` は段 0 を流し込む。`docs/tmux-window-fade.md` の色の式の出典の記述も直した。
  `make test` rc=0 (`tests/tmux/test_window_fade_render.sh` の実行を出力で確認)
  - 敵対的レビューは省いた: 式の置き換えだけで判定ロジックを足しておらず、見た目の同一性を 216 通りの全比較、回数を check_cost の変異で確かめたため

### 実測 (修正後、2026-09-27、この Mac / tmux 3.7b / 隔離サーバ)

変更前と変更後の conf でサーバを 1 つずつ立て、window を 31 個 (1 pane・zsh -f・status off) 作り、`display -p "#{W:<window-status-format>}"` (31 セルを 1 回で展開) を
100 回繰り返したときのサーバの CPU 時間 (`ps -o time`)。3 回ずつ交互に測った。

| 場面 | 変更前 | 変更後 | 1 セルあたり |
|---|---|---|---|
| 最近触った (段 1) | 2,840〜2,940 ms | 1,640〜1,650 ms | 約 922 → 529µs (**−42%**) |
| 消灯 | 770 ms | 760〜780 ms | 約 250µs (変わらない) |

最近触った window は、まだ消灯の約 2.1 倍かかる (fg の dim 判定などの分。内訳は測っていない)
