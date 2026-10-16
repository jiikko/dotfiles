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

## 進捗

- 前提の訂正: `outerPrefix` は Update から同期では呼ばれない。`execAttach` → `m.child` (裏の処理) の中の `PopupAttach.run` から呼ばれる。
  なので画面は止まらない。止まるのは attach の裏の処理で、数え上がったままの裏の処理が ctrl+r の切り替え (`PrepareSwitch` が `WaitChildren` で待つ) も止める
- [x] `subproc.CommandContext` + 上限 5 秒 (glogx の `localCmdTimeout` と同じ値) にした。~~超えたら "" (今までの「読めない」と同じ扱い)~~ → レビューで「上限切れなら popup を開かずに失敗で戻る」に変えた (下の節)
- 方針の変更: 読めないときに既定の C-b を仮定しない。外の prefix でないキーを入れ子のサーバに bind し、見出しにも出すことになる。
  "" なら見出しは「Ctrl+Z で pro-con に戻る」だけになり、戻るキーは残る。toast も出さない (見出しが既に読めた範囲だけを案内する)
- ~~テスト `TestOuterPrefixGivesUpOnHungTmux`~~ (レビューで `TestPopupGivesUpOnHungTmux` に作り直した) (応答する偽の tmux から読める / `exec sleep 3600` の偽の tmux では上限 50ms で "" が返る。合否は戻ったかで、所要は測らない)
- 変異で red を確認 (`mutate-verify`): `subproc.Command` (ctx なし) に戻す → hang guard で「聞いたまま戻らない」の red

### 敵対的レビュー (opus 1 本、2026-10-16)

- P2 (採用): 上限で "" を返しても、`run` はそのあと同じサーバへ `display-popup` を ctx 無しで打つ。サーバが応答しないとそれも戻らず、裏の処理は止まったまま (最初の版の doc は効き目を誇張していた)。
  → `outerPrefix` が上限切れ (`DeadlineExceeded`) を「読めない / None」と分けて返し、上限切れなら `run` は popup を開かずに「tmux が 5s 応答しないので popup を開かない」の失敗で戻る (attach の失敗として知らせが出る)。
  テストは `TestPopupGivesUpOnHungTmux` に作り直した (偽の tmux はどのサブコマンドでも `exec sleep 60`。`run` が失敗で戻ることを見る)。
  変異: `if hung` を `if false && hung` に → hang guard の「attach の裏の処理が戻らない」で red
- P3 (採用): red の経路で偽の tmux の子が 1 時間残る → `sleep 60` に縮めた
- 範囲の外: popup を開いた後にサーバが固まる場合 (display-popup は attach の間ずっと戻らないのが正常なので上限を付けられない)

### 敵対的レビュー 2 周目 (opus 1 本、2026-10-16。観点: 利用者に見える結果・プロセスと資源・時間と並行・文書の一貫性)

- P3 (採用): popup を開く前に失敗しても (上限切れ・tmux を起動できない・display-popup が断った) `RecordAttach` を起こし、上限切れの約 5 秒の窓に別の経路で入った発言を
  このカードの attach の指示として残しえた → `run` の開く前の失敗を `errPopupNotOpened` で包み、`attachDoneMsg` の処理はそのとき頼まない。テスト `TestAttachNotOpenedSkipsRecording`。
  変異: 判定を外す → red
- P3 (採用): `Output()` が読めた直後に上限が来ると「応答しない」と判定しうる (窓は µs。再現はしていない) → `err != nil` のときだけ上限切れと数える。テストは足していない (窓を作れない)
- P3 (採用): このノートの進捗の箇条が最終の挙動と食い違っていた → 打ち消し線で直した
- 壊せなかった観点: 偽の tmux の `sleep 60` は PASS の経路で残らない (ps で確認) / 応答する偽の tmux には 50ms の上限を付けていない (負荷で偽の red にならない) /
  README・help/usage.md・冒頭の注記に今のコードと食い違う記述は無い
