# 568 (research): tmux で分割 pane を行き来すると少し詰まる — nvim が居る pane ではフォーカスの通知で 11ms → 27ms

起票日: 2026-09-28

## 概要

ユーザー報告「tmux で分割 pane にしているときに、pane を行き来すると、ちょっと詰まる感じがある」。
隔離サーバで測ると、tmux だけの切替は約 11ms で、hook・`window-style`・`focus-events` のどれを外しても変わらない。
**nvim が居る pane への出入りは約 27ms** に伸び、`focus-events off` にすると約 11ms に戻る。
nvim が受け取るフォーカスの通知 (FocusGained / FocusLost) への反応が差の全部。

## 実測 (2026-09-28、KOJIm2-MacBook-Air / macOS 27.0 / tmux 3.7c / nvim は本物の設定と plugin)

方法: `-L` + 使い捨ての `TMUX_TMPDIR` / `HOME` の隔離サーバに本物の `_tmux.conf` を読ませ、外側の client を pty (408×105) で attach。
左右 2 pane で `select-pane` を交互に 20〜30 回打ち、「打ってから外側の pty に最後のバイトが届くまで (450ms の窓の中)」と
バイト数の中央値を取った。nvim は `HOME` / `XDG_CONFIG_HOME` / `XDG_DATA_HOME` を本物に向け、状態 (`XDG_STATE_HOME` 等) は一時 dir、
`-n -i NONE` で `_nviminit.lua` を開いた。🚨 **Terminal.app がバイトを描く時間は測っていない**。

| 形 | 描き終わり (中央値) | 1 回のバイト数 |
|---|---|---|
| 中身が動かない pane どうし (sleep) | 11ms | 37KB |
| 同上 + `window-style` を active と同じに / `focus-events off` / `after-select-pane` hook を外す | 11〜12ms | 37KB |
| nvim (本物の設定) の pane を出入り | 26〜30ms | 68KB |
| 同上 + `focus-events off` | 11〜17ms | 44KB |
| `nvim --clean` の pane を出入り | 20ms | 40KB |
| `nvim --clean` + `focus-events off` | 9ms | 40KB |

読み方:
- tmux 自身の処理 (hook・pane の色・枠の format) は犯人ではない
- nvim はフォーカスの通知を受けると、素の状態でも約 10ms 遅れて何かを出す (`--clean` で 9 → 20ms、バイトは同じ)
- 本物の設定ではさらに約 7ms と約 25KB (nvim の画面全体の描き直し 1 回ぶん) が乗る。FocusGained / FocusLost に登録されているのは
  vimade (2 本) / which-key (2 本) / `nvim/lua/dotfiles/basic.lua` の checktime (1 本)。which-key と checktime を外しても変わらなかった。
  vimade は send-keys の `autocmd!` / `nvim_clear_autocmds` で外せず (一覧の数が減らない)、**未確認**。バイトが増える形 (全体の描き直し) は
  「フォーカスを失ったら全ウィンドウを暗くする」vimade の動きと合うが、切り分けていない

## 対応の候補 (未着手・ユーザーの判断待ち)

1. `focus-events off` (tmux 側 1 行)。nvim の pane も 11ms 前後に戻る。代償: nvim の FocusGained の checktime
   (他の pane で Claude が書き換えたファイルの再読込) が効かなくなる。ただし CursorHold (updatetime 500ms) の checktime は残る。
   vimade の「tmux の別 pane にいる間は nvim 全体を暗くする」も消える
2. vimade の FocusLost / FocusGained への反応を止める (nvim 側)。効くかは未確認 (上の切り分けが済んでいない)
3. 先に体感で確かめる: `tmux set -g focus-events off` を一時的に打って行き来し、戻すなら `tmux set -g focus-events on`

## 未確認

- Terminal.app の描画時間 (体感の大半はこちらかもしれない)
- Claude Code の pane がフォーカスの通知にどう反応するか (測っていない)
- 差の 7ms / 25KB が vimade かどうか

## 進捗

- [x] 隔離サーバで tmux 側と nvim 側を切り分けた (上の表)
- [x] 対応の候補から選ぶ: 1 (`focus-events off`) を採用 (2026-09-28 ユーザー判断。「buffer の更新が遅れるくらいいい」)
- [x] 実装: `_tmux.conf` を `set -g focus-events off` にして理由を書いた。同じ前提 (focus-events on) に触れていた
  `src/glogx/tui.go` の spinnerActive のコメントと `docs/glogx-bubbletea-v2.md` の表を直した
- [x] 実測 (上と同じ隔離サーバ、設定の上書きなし): nvim の pane の出入り 27ms → **11ms** (中央値、n=30)、1 回 68KB → 45KB

- [x] 本番で確認: 2026-09-28 ユーザーの体感「早くなった気がする」。buffer の読み直しの遅れも「リアルタイムである必要はない」で許容

## 受け入れた代償 (洗い出し 2026-09-28)

- nvim の FocusGained の checktime が効かない。外で書き換えられたファイルは、キーを押して 0.5 秒止まる (CursorHold) か
  buffer / window を移る (BufEnter) まで読み直されない。別アプリから Terminal.app へ戻ったときも同じ
- which-key: フォーカスを失って 5 秒後に popup を自動で閉じる処理が効かない
- Claude Code: 端末のフォーカス状態 (`isTerminalFocused` / `terminalFocusState`) が「不明 / フォーカスあり」のままになる。
  何に使っているかは未確認 (気づくとしたら「別の pane にいる間の完了・入力待ちの知らせ」)。tmux 枠の 🔔 は hook 由来で影響なし
- 影響なし: vimade (`enablefocusfading` は既定 off で未設定なので、フォーカスでは一時停止 / 再開だけ)・tmux の hook・glogx・pro-con・zsh
