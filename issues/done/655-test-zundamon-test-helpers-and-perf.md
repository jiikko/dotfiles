# 655 (test): zundamon-kaisetsu のテストのヘルパーの共通化と、性能の小さな改善

起票日: 2026-10-07

## テストのヘルパー

- 偽のコマンド (shim) の書き出し 9 か所 (engine_auto_test 5 / show_mermaid_test 3 / interrupt_test 1) と `t.Setenv("PATH")` 13 か所。
  `writeShim(t, dir, name, body)` と、PATH を「前に足す / 置き換える」を選べる helper にまとめる (場所ごとに意図して使い分けているので引数で選ぶ)
- 台本の JSON を書く `writeScript` (show_test.go) があるのに手で書いている箇所 4 つ (cli_test.go 1 / more_test.go 3。不正な JSON を意図的に書く箇所と testdata を加工する箇所は除く)
- testdata/build を写す処理が `buildWithShows` の外に 2 つ (show_mermaid_test.go。more_test.go / golden_test.go は一部の dir だけを写すので別物)

## 性能 (監査で実測。Apple M3 Max, go1.26.0, 300 行 × 4 秒 = 21.8 分の台本)

- Go 側の HTML build 全体は 88 ms / 707 MB alloc。mp4 の build は約 60 s で、内訳は Chrome の撮影 約 30 s (推定)・x264 23.1 s・AAC 3.9 s
- **アセンブラ / SIMD は使わない** (ユーザーが「速くなる余地があれば使ってよい」と言った件の結論): 対象になりうる JSON の escape と base64 は合わせて約 9 ms で、
  build 全体の 0.23% (HTML) / 0.015% (mp4)。最良で 4 倍にしても 7 ms 以下しか縮まない。memmove と sha256 は標準ライブラリが既に asm
- 小さな改善 (試作で実測): 連結後の大きさを見積もって pcm を先に確保し、writeWav を append しない形にすると assemble 47 → 40 ms / alloc 464 → 155 MB
  (長い台本のメモリのピークに効く)。`sortedStates` の挿入ソート (O(n²)、1201 状態で 1.3 ms) を `slices.SortFunc` に、`spokenText` の正規表現を台本ごとに 1 回だけ作る
- 実時間で効きそうなのは Chrome の 61 回の起動 (常駐させて CDP で撮る)。推定で未試作。`--jobs` を 4 より増やしても速くならないことは実測済み

## 関連ファイル

- `src/zundamon-kaisetsu/*_test.go` / `build.go` / `synth.go`。監査の記録: issue 656

## 進捗

- [x] ヘルパーの共通化 — test(zundamon-kaisetsu): テストの helper を helpers_test.go にまとめ…
- [x] 性能の小さな改善 (golden のテストで出力が変わらないことを確かめる) — perf(zundamon-kaisetsu): …

## 結果 (2026-10-07)

- helper: `helpers_test.go` に must / testEnv / writeScript / writeScriptFile / writeShim / prependPath (testing.TB で受ける)。
  偽のコマンドの書き出し 9 か所・PATH の先頭に足す 3 か所・台本を手で書く 4 か所を置き換えた。テストの実行数は前後とも 146
- 性能 (bench_test.go、-count=5 の平均、前 → 後): Assemble 29.95 → 19.32 ms / 438.4 → 124.4 MB、SortedStates 2.96 → 0.26 ms、
  SpokenText 4.81 → 0.10 ms、WriteWav 13.09 → 11.64 ms / 57.6 → 0 MB。build 全体では Go 側は 1% 未満で、体感は変わらない
- アセンブラ / SIMD は使わない (上の数字のとおり、対象になりうる処理が build 全体の 0.2% 未満)
- 見送り: Chrome を常駐させて CDP で撮る (実時間で効きうる唯一の候補。推定で未試作。撮影の仕組みを変える設計変更になる)
- 敵対的レビューは省略した (挙動を変えない置き換え。Python 版の golden との一致を含む全テストが通る。readings の正規表現のキャッシュは
  「台本を読み込んだ後に readings を書き換える呼び出し元が無い」前提で、コード直近に書いた)
