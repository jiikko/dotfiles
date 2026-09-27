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
- 見た目が変わる (scratch だけ点火しない) → 2026-09-27 ユーザー了承 (「やってみて」)

## 進捗

- [x] 隔離サーバで原因の層を切り分けた (上の表)
- [x] 対応方針をユーザーが決める (上の案で了承)
- [x] 実装: hook 2 本が `#{q:session_name}` を渡し、`tmux_ignite_current.sh` が `TT_POPUP_SESSION_RE` に当たる session なら最初に抜ける。
  テスト `tests/tmux/test_ignite_skips_popup_sessions.sh`、`docs/theme-colors.md` に 1 行
- [x] 変異で red を確認: 早期 return を外す (scratch / claude-fork が red) / hook 1 本から session 名を外す (配線が red) /
  session 名なしで呼ぶ bind を足す (配線が red) / lib の source を壊す (main 等が red)
- [x] 効果の実測 (上と同じ隔離サーバ): popup で 1 回の切替 **569KB → 27KB** (中央値、n=15)。到達は 10ms で変わらず。何もしないときの 47KB/秒は変わらない (status-interval 由来で対象外)
- [x] `make test-dir DIR=tests/tmux` rc=0 (skip は以前からの test_fork_scratch.sh 1 件)
- [x] 敵対的レビュー (opus、read-only、隔離サーバで実測つき): P1 なし。
  P3 の 3 件を直した (`dirname` の fork を `${0%/*}` に / lib の利用者コメントの誤り / 配線検査の本数の固定をやめ、bind など全呼び出し行を見る)。
  修正は判定ロジックの新設でなく各々を変異で直接確かめたので 2 周目は回していない

## 残った既知の穴 (直していない)

- 判定の鍵は「切り替えた先の session 名」で「popup が表示中か」ではない。popup を開いたまま別の端末の client が普通の session で window を切り替えると、
  global の `@ignite` の更新で popup も描き直される (変更前から同じ。単一 client の通常の操作では踏まない。頻度は未測定)
- popup の中から jump 系で普通の session へ移ると、以降の切替は点火する (popup が普通の session を表示しているので名前では区別できない。popup 内で jump が効くかは未確認)
- 色の式 ($1) が空に展開されると $2 が $1 にずれて skip しない。今の `@fade-ramp-tpl` は `colour…` で始まるので空にならない
- popup を通さず scratch / claude-fork に直接 attach しても点火しない (名前で判定しているため。scratch は popup 専用の前提)
