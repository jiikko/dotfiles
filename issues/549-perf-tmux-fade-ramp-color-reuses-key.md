# 549 (perf): 放置フェードのランプ色でも段の鍵を使い回し、@fade-bucket の 2 回目の展開を消す

> 🚨 **担当中: dotfiles-c2**（2026-09-27〜）

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

- [ ] 描画が変わらない: 今の conf と新しい conf で window-status-format を展開し、差分 0 (issue 547 と同じ 868 通りの組み合わせ)
- [ ] ランプ色の式 (`colour#{e|+:16,#{e|*:37,…}}`) が `_tmux.conf` に 1 か所だけある (grep で数える)
- [ ] 点火アニメの開始色 (`tmux_ignite_current.sh` の `$1`) が変わらない
- [ ] `check_cost` が @fade-bucket 1 回を固定し、2 回目を戻す変更を当てると red になる
- [ ] before / after の実測を本文に書く (セル 1 個と全面再描画 1 回)

## 関連ファイル

- `_tmux.conf` (`@fade-ramp-color` / `@fade-tpl` / `@fade-key`、ignite の set-hook)
- `scripts/tmux_ignite_current.sh`
- `tests/tmux/test_window_fade_render.sh`
- `docs/tmux-window-fade.md`
- issue 547 (元の調査と PR #16 の実測)

## 進捗

- 2026-09-27: 起票。試作で効果を実測した (上表)。本体への変更はまだ
