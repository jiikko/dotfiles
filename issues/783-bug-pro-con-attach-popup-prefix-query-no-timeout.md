# 783 (bug): pro-con の attach の popup が外の tmux の prefix を時間の上限なしで問い合わせる

起票日: 2026-10-16

## 概要

`pro-con/ui/attachpopup.go` は attach のたびに外の tmux の prefix を `tmux show -gv prefix` で読む。issue 777 で `subproc.Command` を通すようにして WaitDelay は付いたが、
時間の上限 (ctx) は無い。tmux のサーバが応答しないと、attach のキーを押した画面がその問い合わせで止まる。issue 769 の調査で見つけ、範囲の外として残した。

## 詳細

- 問い合わせは Update の中から同期で呼ばれる (attach の操作)。上限が無いので、tmux が固まると画面ごと止まる
- 同じ問い合わせは glogx にもあり、**glogx は上限を持つ** (`external_commands.go` の `loadTmuxPrefix`: `context.WithTimeout(localCmdTimeout = 5 秒)` + `subproc.CommandContext`。
  失敗したら "" を返して機能を切る)。pro-con の `outerPrefix` だけが上限なし (呼び出しは attachpopup.go の 1 か所。Update から同期で呼ばれるかは未確認)

## 対応方針

- glogx と同じ形 (`subproc.CommandContext` + 上限) にし、読めなければ既定の prefix (C-b) で案内する
- 上限を超えたときの案内の文言を決める (prefix が読めなかったことを toast に出すか)

## 関連ファイル

- `src/pro-con/ui/attachpopup.go` / `src/glogx/external_commands.go` (`loadTmuxPrefix`) / issue 769・777 (done)
