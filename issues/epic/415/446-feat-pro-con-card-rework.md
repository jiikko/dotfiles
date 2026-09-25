# 446 (feat): レビューで差し戻す — card rework

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

PM がレビュー待ちのカードを見て「ここを直して」と PG に返す操作が無い (2026-09-25 の dogfooding 1 回目で見つけた。440)。
今ある操作は add / plan / ask / answer / review / close / run / guide / list / show / wait で、レビュー待ち → 作業中へ戻す口が無い。
今は PM が自分で直すか、そのまま取り込むしかない。

## 対応方針 (候補)

- `pro-con card rework <カード> "<直してほしい点>"`: レビュー待ちのカードを作業中へ戻し、同じ PG の session を、直してほしい点を渡して再開する
  (回答 (answer) で再開するのと同じ経路。受付の箱に `rework` の依頼を置き、dispatcher が適用する = 426 の決定 1)
- 履歴に「差し戻した: <原文>」を残す (要約しない)
- pm-guide.md の役目 5 (レビュー) に「直してほしい点があれば rework」を足す
- 差し戻しの回数の上限は設けない (人が見る)

## 進捗

- [~] 2026-09-25 18:45: dogfooding の 3 枚目 (カード C-004) として PG が着手 (440)

