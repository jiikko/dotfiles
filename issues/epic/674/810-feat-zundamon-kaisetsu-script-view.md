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

- [x] サブコマンドの実装とテスト (zundamon-kaisetsu: 台本をユーザーに見せる一覧 view (810) / mp4 のときに概要欄へ貼るクレジットを出す (811))
  - `internal/build/script_view.go` の `CmdView`。図解の範囲と段は build と同じ `show.LineShows` で決めるので wav は要らない
  - `script_view_test.go` の `TestViewCommand` (wav を消した台本で、見出し・既定の表情・図解を出す / 段を進める / 消す行と、図解の行が書いた行の直後に出ることを見る)
  - 変異: 段を進める行の段を常に 1 段目にする (`stepLabel(sd, *steps[i])` → `stepLabel(sd, 0)`) → TestViewCommand が red (mutate-verify rc=0)
  - `make test` rc=0 / `make lint` rc=0 (最後の編集の後)。シャボン玉の台本 (92 行) で手で出して、手組みの jq の一覧と同じ情報が出ることを目で確かめた
- [x] SKILL.md・README の入口に載せる (SKILL.md 手順 3-4・src の README のコマンド一覧とパッケージの地図・usage)

## 結果

- 敵対的レビューは省いた: 表示だけのコマンドで、判定・状態遷移・外部 I/O を持たない (図解の段の判定は既存の `show.LineShows` をそのまま使う)

反証レビューは通していない (低優先の feat。改善点の振り返りからの起票で、主張は今回の作業の出来事だけ)。
