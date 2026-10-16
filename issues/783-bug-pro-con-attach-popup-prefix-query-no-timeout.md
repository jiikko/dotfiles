# 783 (bug): pro-con の attach の popup が外の tmux の prefix を時間の上限なしで問い合わせる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

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

## 進捗

- 前提の訂正: `outerPrefix` は Update から同期では呼ばれない。`execAttach` → `m.child` (裏の処理) の中の `PopupAttach.run` から呼ばれる。
  なので画面は止まらない。止まるのは attach の裏の処理で、数え上がったままの裏の処理が ctrl+r の切り替え (`PrepareSwitch` が `WaitChildren` で待つ) も止める
- [x] `subproc.CommandContext` + 上限 5 秒 (glogx の `localCmdTimeout` と同じ値) にした。超えたら "" (今までの「読めない」と同じ扱い)
- 方針の変更: 読めないときに既定の C-b を仮定しない。外の prefix でないキーを入れ子のサーバに bind し、見出しにも出すことになる。
  "" なら見出しは「Ctrl+Z で pro-con に戻る」だけになり、戻るキーは残る。toast も出さない (見出しが既に読めた範囲だけを案内する)
- テスト `TestOuterPrefixGivesUpOnHungTmux` (応答する偽の tmux から読める / `exec sleep 3600` の偽の tmux では上限 50ms で "" が返る。合否は戻ったかで、所要は測らない)
- 変異で red を確認 (`mutate-verify`): `subproc.Command` (ctx なし) に戻す → hang guard で「聞いたまま戻らない」の red
