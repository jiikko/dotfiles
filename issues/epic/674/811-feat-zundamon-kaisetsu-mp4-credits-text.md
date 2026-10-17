# 811 (feat): zundamon-kaisetsu の mp4 の書き出しで、概要欄に貼るクレジットの文を出す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-17〜）

起票日: 2026-10-17

## 概要

VOICEVOX の規約でクレジット表記 (`VOICEVOX:<キャラ>`) が必須だが、mp4 には入らず、概要欄などに自分で書く必要がある
(SKILL.md「利用規約」)。build が mp4 を書いたときに、貼れる形のクレジットの文を出す。

## 詳細

- 2026-10-17 の「シャボン玉」の動画で、ユーザーに渡すクレジットを作るため、立ち絵の作者名を書き出し済みの `faces.json` の `credit` から grep で探した
- build は HTML のためにクレジットを既に組み立てている (`src/zundamon-kaisetsu/internal/build/build.go` の `credits` を作る箇所。声・立ち絵の `credit`・台本の `credits` を重複なく並べる)

## 対応方針

- mp4 を書いたら同じ一覧を標準出力に出すか、`<出力名>.credits.txt` に書く (どちらにするかは実装時に決める。ファイルなら成果物と一緒に渡せる)
- SKILL.md の「利用規約」と手順 7 の「mp4 は概要欄などに自分で書く」を、出力の場所を指す形に直す

## 関連ファイル

- `src/zundamon-kaisetsu/internal/build/build.go`
- `_claude/skills/zundamon-kaisetsu/SKILL.md` (「利用規約」)

## 進捗

- [x] 実装とテスト (zundamon-kaisetsu: 台本をユーザーに見せる一覧 view (810) / mp4 のときに概要欄へ貼るクレジットを出す (811))
  - 標準出力に 1 行 1 つで出す形にした (ファイルにすると出力先の書き込み検査・置き換えが 1 つ増える。見出しは標準エラー)。中身は HTML と同じ `Assemble` の `Credits`
  - `script_view_test.go` の `TestBuildMP4PrintsCredits` (偽の Chrome / ffmpeg で mp4 を書き、標準出力が `Credits` と一致する)
  - 変異: `printMP4Credits` の呼び出しを消す → TestBuildMP4PrintsCredits が red (mutate-verify rc=0)
  - `make test` rc=0 / `make lint` rc=0
- [x] SKILL.md の記述を直す (「利用規約」と手順 6。skill の README の利用規約の行も)

## 結果

- 敵対的レビューは省いた: 既にある一覧を標準出力へ出すだけで、判定・外部 I/O は変えていない

反証レビューは通していない (低優先の feat。改善点の振り返りからの起票で、主張は今回の作業の出来事だけ)。
