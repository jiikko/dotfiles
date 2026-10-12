# 741 (refactor): zundamon-kaisetsu を責務ごとのパッケージに割る

> 🚨 **担当中: treefiler の行番号のセッション**（2026-10-12〜）

起票日: 2026-10-12

## 概要

`src/zundamon-kaisetsu` は `package main` の 1 パッケージに 20 ファイル (テストを除く。約 6,600 行) が平場で並び、
どの関数もどこからでも見える。ユーザー依頼 (2026-10-12「パッケージで割る方向で進めて。今やって」) で、責務ごとに
`internal/` のパッケージへ割る。目的は、パッケージの境界 (外から見えない名前) で 1 つの変更を理解するのに読む範囲を
狭めること。ファイルをサブディレクトリへ移すだけの案は、同じ 1 パッケージのままで何も局所化しないので採らない。

## 現状の依存 (go/ast でトップレベルの宣言の参照を数えた。2026-10-12)

- ほぼ全ファイルが `fail` (script.go)・`Env` / 中断 (`appCtx`) (main.go)・`py*` (pyjson.go) を使う → 土台の層に出す
- 循環は `script.go` ⇄ `show*.go` の 1 組だけ: script は `lineShows` で図解を検証し、show は script の
  パスの関数 (`resolveScriptRelative` / `workDir`) と `fail` だけを使う。パスの関数を下へ出せば切れる
- 小さな関数の置き場の食い違い: `firstRunes` / `runQuiet` (engine.go) を mp4 が、`lastRunes` (mp4.go) を
  show_mermaid が、`isFile` (build.go) を engine_auto / synth / show_mermaid が、`foldWidth` / `lineWords`
  (reading_check.go) を source_check が使う

## 対応方針 (パッケージの層。上は下だけを import する)

| 層 | パッケージ | 持つもの (元のファイル) |
|---|---|---|
| 0 | `internal/pyjson` | pyjson.go・`pyIter` (build.go) |
| 0 | `internal/textx` | `firstRunes` / `lastRunes` / `orStr` / `foldWidth` / `lineWords` |
| 0 | `internal/app` | `Env`・`errExit` / `fail`・中断 (`appCtx` / `sleepCtx` / `interruptedErr` / `ctxInterrupted`)・`runQuiet` |
| 0 | `internal/paths` | `resolvePath` / `expandUser` / `resolveScriptRelative` / `workDir` / `isFile` |
| 0 | `internal/wav` / `internal/outfile` / `internal/chrome` | wav.go / outfile.go / chrome.go |
| 1 | `internal/show` | show.go / show_image.go / show_mermaid.go |
| 2 | `internal/script` | script.go・`parseOverlays` (build.go) |
| 3 | `internal/build` | build.go / blink.go / caption_speed.go / chapter_card.go / mp4.go |
| 3 | `internal/engine` | engine.go / engine_auto.go (`check` が ffmpeg のエンコーダを見るので build を import する) |
| 4 | `internal/synth` | synth.go / reading_check.go |
| 3 | `internal/lint` | lint.go / source_check.go |
| — | `main` | main.go (引数・シグナル・コマンドの振り分け) |

- テストは、関数単位のものは移した先のパッケージへ、`run` を通す CLI のものは main に残す。共有の偽物
  (`writeShim` / `newFakeEngineEnv`) は `internal/testkit` へ
- **挙動を変えない commit として単独で入れる** (verify-design-intent-before-refactor)。等価の証拠は
  ①テストを変えずに通る (golden の一致を含む) ②分割の前後でテストの名前の集合が同じ (148 本。`go test -list`) ③lint
- Python 版との互換 (CLAUDE.md の `py*` / 行を `map[string]any` のまま渡す) は動かさない

## 進捗

- [ ] 設計のレビュー
- [ ] 分割の実装 (テスト・lint・名前の集合の一致)
- [ ] README / CLAUDE.md の地図の更新
