# 741 (refactor): zundamon-kaisetsu を責務ごとのパッケージに割る

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
| 0 | `internal/textx` | `firstRunes` / `lastRunes` / `foldWidth` / `lineWords` |
| 0 | `internal/app` | `Env`・`errExit` / `fail`・中断 (`appCtx` / `sleepCtx` / `interruptedErr` / `ctxInterrupted`) |
| 0 | `internal/paths` | `resolvePath` / `expandUser` / `resolveScriptRelative` / `workDir` / `isFile` |
| 0 | `internal/wav` / `internal/outfile` / `internal/extcmd` | wav.go (`sampleRate` も) / outfile.go / chrome.go・`runQuiet`・`h264Encoder` |
| 1 | `internal/show` | show.go / show_image.go / show_mermaid.go |
| 2 | `internal/script` | script.go・`parseOverlays` / `overlayKeys` (build.go) |
| 3 | `internal/build` | build.go / blink.go / caption_speed.go / chapter_card.go / mp4.go・`embedShowImages` (show_image.go。`*Script` を取るので show に置くと循環する) |
| 3 | `internal/engine` | engine.go / engine_auto.go |
| 4 | `internal/synth` | synth.go / reading_check.go |
| 3 | `internal/lint` | lint.go / source_check.go |
| — | `main` | main.go (引数・シグナル・コマンドの振り分け) |

- テストは全部 main に残す (レビューの P2 を受けて変更。下の「設計のレビュー」)
- **挙動を変えない commit として単独で入れる** (verify-design-intent-before-refactor)。等価の証拠は
  ①テストを変えずに通る (golden の一致を含む) ②分割の前後でテストの名前の集合が同じ (148 本。`go test -list`) ③lint
- Python 版との互換 (CLAUDE.md の `py*` / 行を `map[string]any` のまま渡す) は動かさない

## 設計のレビュー (sonnet 1 本の反証。2026-10-12)

- P1 循環 3 件 (`embedShowImages` が `*Script` を取る / wav が build の `sampleRate` を見る / script が build の `overlayKeys` を見る):
  採用。上の表の置き場所へ直した
- P2 テストの移設が成り立たない (testdata と skill のディレクトリを cwd 相対で読む・偽の ffmpeg / Chrome がテストのバイナリ自身を
  `TestFakeMP4Tool` で起こし直す・共有の偽物をほぼ全テストが使う): 採用。テストは全部 main に残し、テストから触る名前は公開名にした
- P2 パッケージ変数の書き換え: テストが main に残るので、書き換えは `app.Ctx` / `build.VideoSize` などの公開変数への代入として効く (テストで確認)
- P2 公開面の増え方 (約 81 個): 受け入れる。テストを main に置く限り避けられない。`internal/` なので module の外には出ない。
  パッケージの中だけで使う名前 (`showParsers` / `blinkBit` など) は非公開のまま残っている
- P3 engine → build (`h264Encoder`): 採用。外部コマンドの探索と実行を `internal/extcmd` に寄せて消した
- P3 textx が薄い・`orStr` は engine の中だけ: `orStr` は engine に残した。textx は 4 関数 (2 パッケージ以上から使う) で残した
- P3 app の混在: `runQuiet` は extcmd へ出した。`Env` / 失敗 / 中断は「CLI の実行の文脈」として app に残した
- 記述のずれ (`newFakeEngineEnv` は engine_auto_test.go にある): CLAUDE.md を直した

## 結果

- 分割の手順: 同じ package main のまま宣言を行き先ごとのファイルへ寄せ、go/packages の型情報で識別子を解決する使い捨ての道具で
  `internal/<pkg>` へ移した (パッケージを跨いで使う非公開の名前を公開名に・跨ぐ参照を `pkg.Name` に・パッケージ名を隠すローカル変数を改名)。
  そのあと `app.AppCtx` → `app.Ctx`・`pyjson.PyStr` → `pyjson.Str` などの重複を gopls rename で外した
- 🚨 非公開のフィールドを公開名にすると encoding/json に出るようになる (golden の `TestBuildDataMatchesPython` が `Mouth` の混入で赤になった)。
  JSON に出る構造体で公開名にした 2 フィールド (`show.Data.SrcWritten` / `build.TimelineLine.Mouth`) に `json:"-"` を付けた
  (道具は公開名にした全フィールドに付けたので、JSON に出ない `app.ErrExit.Msg` と faceSet の `Images` からは外した)
- 等価の確認:
  - `go test -race ./...` 緑 (golden を含む)・`make lint` 0 件・root の `make test` は `tests/claude/test_claude_mods.sh` (tool-elapsed の
    5 秒の toast の判定) だけ赤で、単独で回すと緑 (並列の負荷による揺れ。zundamon を触らない)
  - テストの名前の集合が分割の前後で同じ 148 本 (`go test -list '.*' ./...` の diff が空)
  - トップレベルの宣言のトークン列 (コメントを除く) を、改名と修飾を戻して前後で比べた: 不一致 49 件は全部、元から公開名だった識別子
    (`Write` / `Request` / `Mouth` など) を逆引きが旧名へ戻した誤差・`json:"-"` の 2 か所 (と外した 1 か所)・const / var の組をばらした所で、関数の中身の違いは無かった
  - gopls rename が別の識別子に当たったのを 2 件見つけて戻した (`isPyInt` → `Int`・`holdEngineInUse` → `InUse`)。上のトークン比較で、
    残る改名が想定の組だけであることを確かめた
- 敵対的レビュー (red team) は省略した: 判断ロジック・境界・状態遷移を変えない機械的な移動で、等価をトークン比較とテストで確かめたため

## 進捗

- [x] 設計のレビュー
- [x] 分割の実装 (テスト・lint・名前の集合の一致) — refactor(zundamon-kaisetsu): 責務ごとに internal/ のパッケージへ割る
- [x] README / CLAUDE.md の地図の更新 (skill の SKILL.md と player.html のコメントのパスも)

## 残タスク

- スコープ外: 関数単位のテストを各パッケージへ移す (cwd 相対のパスを `runtime.Caller` 基準にし、偽の mp4 の道具を使うパッケージごとに
  `TestFakeMP4Tool` を持たせる必要がある。CLAUDE.md に書いた)。移せばテストのためだけの公開名を減らせる。trigger: 次にどこかのパッケージの
  テストを大きく足すとき
