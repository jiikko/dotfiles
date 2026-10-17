# 810 (feat): zundamon-kaisetsu に、台本をユーザーに見せる一覧を出すコマンドを足す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-17〜）

起票日: 2026-10-17

## 概要

SKILL.md の手順 3-4 (台本をユーザーに見せる) のために、話者・表情・チャプター・図解の出し入れを 1 行ずつ並べた一覧を
`zundamon-kaisetsu view script.json` のようなサブコマンドで出す。

## 詳細

- 2026-10-17 の「シャボン玉」の動画 (92 行) で、台本の JSON のままではどの行で図解が出て・段が進み (`"next"`)・消える (`null`) かが
  読めず、main agent が jq で一覧を手組みした (`行番号 / 【チャプター】話者(表情) / 字幕` と、図解の行に `［図解］…` / `［図解を消す］`)
- 同じ一覧は動画ごとに要るので、手組みを毎回繰り返すことになる

## 対応方針

- 既存の `times` (`src/zundamon-kaisetsu/internal/build/line_times.go` の `CmdTimes`) と並ぶサブコマンドにする。合成 (wav) は要らない
- 出すもの: 行番号 (lines の添字) / チャプターの頭の印 / 話者 / 表情 / 字幕 / 図解 (種類と中身の要約、`next` で何段目か、`null`)
- SKILL.md の手順 3-4 と README のコマンド一覧に載せる (`new-tool-requires-entrypoint-docs.md`)

## 関連ファイル

- `src/zundamon-kaisetsu/internal/build/line_times.go`
- `_claude/skills/zundamon-kaisetsu/SKILL.md` (手順 3-4)

## 進捗

- [ ] サブコマンドの実装とテスト
- [ ] SKILL.md・README の入口に載せる

反証レビューは通していない (低優先の feat。改善点の振り返りからの起票で、主張は今回の作業の出来事だけ)。
