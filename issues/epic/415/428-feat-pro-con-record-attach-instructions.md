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

- [ ] 未着手。424 の実測は済んだ (上の節)。427 で PG を起動できてから
