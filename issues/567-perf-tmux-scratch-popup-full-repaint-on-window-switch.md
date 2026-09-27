# 567 (perf): scratch の popup で window を移すと、点火のアニメが popup 全体を 8 回描き直す (1 回の切替で約 570KB)

起票日: 2026-09-27

## 概要

ユーザー報告「tmux の scratch 上で window を移動すると、バッファのロードがもっさりしがち」。
隔離サーバで測ると、scratch の popup (入れ子の tmux client) で window を 1 回切り替えるたびに、外側の端末へ
**約 570KB** が流れる。大半は `after-select-window[1]` の点火のアニメ (`scripts/tmux_ignite_current.sh`) で、
止めると **約 30KB** (約 19 分の 1) になる。popup は中身が少しでも変わると popup 全体を描き直すため、
アニメの 1 フレーム (`refresh-client -S`) ごとに popup 全体 (約 324×76) が送り直される。

## 実測 (2026-09-27、KOJIm2-MacBook-Air / macOS 27.0 / tmux 3.7c)

- 方法: `-L` + 使い捨ての `TMUX_TMPDIR` / `HOME` の隔離サーバに本物の `_tmux.conf` を読ませ、外側の client を pty (408×105) で attach。
  scratch (324×76 相当) に画面いっぱいの色付きテキストの window を 4 つ置き、`select-window` を打ってから
  「次の window の最下行の目印が外側の pty に届くまで」と「その後 350ms を含むバイト数」を測った (各 15 回の中央値)。
  popup の中身は本物の `tmux_scratch_popup.sh` と同じ `display-popup -E -w 80% -h 75%` で、attach だけ `-L` 付きに差し替えた
  (本物は `unset TMUX_TMPDIR` で本番サーバへ attach するため)
- 🚨 測ったのは pty に届くまで。**Terminal.app がそのバイトを描く時間は測っていない** (体感の遅さはこちらで効くと見ているが未実測)

| 形 | 目印の到達 (中央値) | 1 回の切替のバイト数 | 何もしないときの流量 |
|---|---|---|---|
| popup (今の scratch) | 11ms | 569KB | 47KB/秒 (popup 全体を 1.2 回/秒描き直す) |
| popup + 点火のアニメなし | 32ms (p90 177ms。外れ値 1 件) | 30KB | 47KB/秒 |
| popup + 点滅なしの 1 行 status | 10ms | 563KB | 45KB/秒 |
| popup + アニメなし + 点滅なし | 9ms | 25KB | 45KB/秒 |
| popup + `status-interval 0` (global) | 10ms | 569KB | 13KB/秒 |
| 直接 attach (`switch-client`) | 20ms | 148KB | 6.8KB/秒 |
| 直接 attach + アニメなし | 10ms | 25KB | 6.7KB/秒 |

読み方:
- **切替 1 回のバイト数は、点火のアニメがほぼ全部**。popup では 569KB → 30KB、直接でも 148KB → 25KB
- 何もしないときの流量は `status-interval` の毎秒の更新で popup 全体が描き直されることから来る (点滅をやめても変わらず、interval 0 で 47 → 13KB/秒)。
  interval は global でアニメ・フェードの駆動源なので、これは下げない
- 到達時間は pty 上ではどの形も 10〜30ms で差が小さい。遅さは「届くまで」ではなく「届いた大量のバイトを端末が描き切るまで」と見ている (未実測)

## 対応方針 (案)

- scratch / claude-fork の popup session (`scripts/lib/tmux_popup_sessions.sh` の `TT_POPUP_SESSION_RE`) では、
  `tmux_ignite_current.sh` を最初に抜ける。`tmux_agent_panel.sh follow` が同じ正規表現で抜けているのと同じ形
- 見た目が変わる (scratch だけ点火しない) ので、ユーザーの了承を待つ

## 進捗

- [x] 隔離サーバで原因の層を切り分けた (上の表)
- [ ] 対応方針をユーザーが決める
