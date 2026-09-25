# 428 (feat): attach 中に人間が打った指示をカードに残す

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

`a` (attach) で PG の session を開いて直接話した内容は、今はカードの履歴に残らない。
あとからカードを見ても「人間が何を指示したか」が分からない。

## 対応方針 (候補)

- attach していた時間帯の transcript から、**人間のメッセージだけを原文で**カードの履歴へ追記する (LLM で要約しない)
- 採らない場合は「attach の内容はカードに残らない」を既知の制約として README に書く

## 実測済み (424 で。2026-09-24 / Claude Code 2.1.281)

- Desktop と `--bg` の transcript は同じ形式 (1 行 1 レコードの JSON)。**人間の発言は `user` レコードの `origin.kind == "human"`**
  (ツールの結果・他 session からのメッセージは含まない)。読み方は `src/pro-con/live/transcript.go` の `parse` がすでに持っている
  (`Transcript.Prompts`)。attach の前後の時刻で絞れば、attach 中の発言だけを取れる見込み
- 🚨 `claude --bg` の最初の依頼 (起動の引数) には human の印が付かない (last-prompt には出る)
- attach できるのは pro-con が起動した session だけ ([424](done/424-feat-pro-con-readonly-real-backend.md) の範囲の変更)

## 関連ファイル

- `src/pro-con/ui/model.go` の `attach`

## 進捗

- [x] 実装 (2026-09-25, pro-con カード C-002)。「対応方針」の 1 つ目を採った
  - 画面 (`ui/model.go`) は端末を明け渡した時刻と戻った時刻を持ち、戻ったら裏で `backend.AttachRecorder.RecordAttach` を呼ぶ
    (attach が失敗で戻っても呼ぶ。残せなかったら消えない通知)
  - live (`live.RecordAttach`) は短い id から記録 (sessions.json、無ければ sessions-retired.json) の session id を引き、
    transcript の**全体**から窓の中の `origin.kind == "human"` の発言を拾って受付の箱に `kind: "attach"` で置く (`live.HumanPrompts`)
  - dispatcher (store.Apply の `attach`) が、打った時刻のまま「attach で人間が指示: <原文>」を履歴に足す。どの列でも受け、状態は変えない
  - ついでに直したもの: transcript の読みを `bufio.Scanner` から `bufio.Reader` に替えた (8MB を超える 1 行で黙って読むのを止め、
    後の発言を落としていた)。再開の文 (`RestartNote`) は human の印が付いていても発言に数えない。引き出しの履歴の時刻を手元の時間帯で出す
    (transcript の時刻は UTC)。終了のとき裏の処理を待ってから抜ける (main。bubbletea は走っている Cmd を待たない)
- 確かめたこと: 新しい検査 (`TestRecordAttachSubmitsHumanPromptsInWindow` / `TestAttachAppendsSaidVerbatim` /
  `TestAttachDoneRecordsInstructions` / `TestAttachDoneCarriesHandOverTime` / `TestDrawerHistoryInLocalTime`) は
  `bin/mutate-verify` で変異 13 本 (窓の上端・下端、ReadTail への差し替え、Scanner への戻し、再開の文の除外、原文の切り詰め、
  打った時刻→適用時刻、空の指示、session id の引き違い、戻った時刻の取り違え、明け渡した時刻の欠落、通知の種類、時間帯) がすべて red。
  敵対的レビュー (read-only サブエージェント) 1 周: 上の Scanner と再開の文は再現して直した、終了の競合はコードで確かめて直した
- 残り
  - [ ] 実機の E2E は未実施 (本物の `claude attach` で打った文が human の印付きで transcript に入り、カードの履歴に出るか)
  - [ ] attach の間に dispatcher が同じカードを再開して session が入れ替わると、短い id が引く transcript は新しい側だけになり、
    前の session で打った分を落としうる (未再現。コードを辿って見つけたもの)
  - [ ] 履歴は 1 行で出すので、改行を含む指示は空白 1 つに詰めて残る (transcript の読みの既存の作法。原文の改行は残らない)
  - [ ] `main.go` の終了時の待ちには検査が無い (1 行の配線)
- PM (Claude) のレビューと取り込み (2026-09-25、dogfooding の 1 回目 = 440): diff を読み、master へ cherry-pick して make test 緑で push (84cfc4e3)。
  レビューで見つけた小さな点: attach の指示は打った時刻のまま履歴の末尾に足すので、履歴が時刻の順に並ばなくなりうる (未起票。差し戻す口が無く返せなかった = 446。**446 で差し戻しの口は解消**、この点は未対応)
