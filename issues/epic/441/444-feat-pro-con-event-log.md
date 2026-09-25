# 444 (feat): 出来事の記録 — dispatcher の判断を構造化して残し、外から読む

起票日: 2026-09-25

親: [441](441-design-pro-con-viewer.md)

## 概要

dispatcher が何を判断したか (箱の依頼を適用した / PG を起動・再開・停止した / 取り込んだ / 止め直した / 枠で絞った / watchdog / 画面の数)
を、あとから・外から読める形で残す。今は Tick の notes を標準出力に出すだけで、画面が起こした dispatcher なら dispatcher.log、
手で起動したら nohup の先に自由文で残る (2026-09-25 の dogfooding で、止め直しが続く理由を log を tail して推理した)。

## 対応方針 (候補)

- 状態の置き場に `events.jsonl` (1 行 1 出来事: 時刻・種類・カード・session・理由)。大きさの上限で回す
- `pro-con log [--card <カード>] [--follow] [--since <時刻>]` が読む。`--follow` は socket の購読で即時
- 画面の側の出来事 (開いた・閉じた・quit で止めた / 止めなかった) も同じ記録へ (受付の箱を通さず追記でよいかは 445 で決める)
