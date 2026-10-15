# 783 (bug): pro-con の attach の popup が外の tmux の prefix を時間の上限なしで問い合わせる

起票日: 2026-10-16

## 概要

`pro-con/ui/attachpopup.go` は attach のたびに外の tmux の prefix を `tmux show -gv prefix` で読む。issue 777 で `subproc.Command` を通すようにして WaitDelay は付いたが、
時間の上限 (ctx) は無い。tmux のサーバが応答しないと、attach のキーを押した画面がその問い合わせで止まる。issue 769 の調査で見つけ、範囲の外として残した。

## 詳細

- 問い合わせは Update の中から同期で呼ばれる (attach の操作)。上限が無いので、tmux が固まると画面ごと止まる
- 同じ問い合わせは glogx にもある (`external_commands.go` の `loadTmuxPrefix`。表記を bubbletea の形に直す点が違う)。上限の有無は未確認

## 対応方針

- `subproc.CommandContext` + 短い上限 (例: 2 秒) にし、読めなければ既定の prefix (C-b) で案内する
- 上限を超えたときの案内の文言を決める (prefix が読めなかったことを toast に出すか)

## 関連ファイル

- `src/pro-con/ui/attachpopup.go` / `src/glogx/external_commands.go` (`loadTmuxPrefix`) / issue 769・777 (done)
