# 428 (feat): attach 中に人間が打った指示をカードに残す

起票日: 2026-09-24

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

`a` (attach) で PG の session を開いて直接話した内容は、今はカードの履歴に残らない。
あとからカードを見ても「人間が何を指示したか」が分からない。

## 対応方針 (候補)

- attach していた時間帯の transcript から、**人間のメッセージだけを原文で**カードの履歴へ追記する (LLM で要約しない)
- 採らない場合は「attach の内容はカードに残らない」を既知の制約として README に書く

## 未実測

- Desktop と `--bg` の transcript の形式が同じか ([424](done/424-feat-pro-con-readonly-real-backend.md) で測る)

## 関連ファイル

- `src/pro-con/ui/model.go` の `attach`

## 進捗

- [ ] 未着手 (424 の実測の後)
