# 1116 (cleanup): ja-writing の production のファイルに、テストからしか呼ばれない関数が 59 件ある

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-26〜）

起票日: 2026-11-26

## 概要

`deadcode` (golang.org/x/tools/cmd/deadcode v0.52.0。テストを含めず、module の全部の `main` から辿る: `go run golang.org/x/tools/cmd/deadcode@v0.52.0 ./...`) を `src/ja-writing` に掛けると、辿れない関数が 115 件出る。
そのうち本家の記録を取るテストの道具 (`oracle`・`oracle/reportshape`・`oracle/replay`) の 56 件を除く 59 件が、製品の package にある。どれもテスト (`*_test.go`) からだけ呼ばれる入口と、その下請けだった。issue 1113 の監査 (dead-code) の発見。

## 詳細

製品の package の内訳 (2026-11-26):

| package | 件数 | 中身 |
|---|---|---|
| diff | 37 | 本家の関数と同じ名前の入口 (`AdditionRisks`・`NegationRisks`・`FullDiff`・`Tone` など) と下請け (`typeChangesOf`・`meaningInputs` など)。関数の記録と突き合わせるテスト (`*_oracle_test.go`) が呼ぶ |
| visibility | 4 | `UnicodeWhitespace`・`ReferenceDefinitionEnd`・`CurrentDirectory`・`WordsLabel` |
| internal/pyre | 6 | `Match.GroupDict`・`Match.Expand`・`Match.Pos`・`Match.EndPos`・`Pattern.Pattern`・`Lazy.Flags` |
| internal/pystr | 5 | `IsAlpha`・`IsAlnum`・`IsDecimal`・`IsDigit`・`IsNumeric` |
| check | 3 | `Similarity`・`DirFiles.Tool`・`DirFiles.List` (`DirFiles` は `check/base_recording_test.go` だけが使う) |
| lists | 2 | `Names`・`SHA256` |
| internal/pyjson・internal/difflib | 1・1 | `RoundInt`・`SequenceMatcher.FindLongestMatch` |

repo の検査が拾わない理由: `make test-unused-excluding-tests` (`scripts/check_unused_excluding_tests.sh`。staticcheck の U1000 を `-tests=false` で回す) は、export した名前を報告しない (script の冒頭の「検出しないと決めた形」)。
export していない下請け (`typeChangesOf` など) も、export した入口から呼ばれるので使われていると数えられる。

根の `main` (`.`) だけから辿ると 61 件になる。差の 2 件 (`visibility.DocTypeValues`・`ReaderValues`) は `oracle/cmd/golden` の `main` (`make golden`) から呼ばれるので、移す対象ではない。

害: production のバイナリと package の公開面に、使われない関数が載っている。本当に使われなくなった関数が出ても、この 59 件に紛れて見分けられない。
silent か: コンパイルもテストも通るので、増えても誰も気づかない。

意図: 関数の記録の入口は、本家の関数と 1 対 1 で並べるために作ったもの (README の「本家の出力を正解にする仕組み」)。消すのではなく、置き場所の話。

## 対応方針

- テストからしか呼ばれない入口は、同じ package のテストのファイル (`export_test.go` か、使うテストのファイル) へ移す。同じフォルダのテスト (外部テスト package `<名前>_test` を含む) からは、呼び方は変わらない
- 移せないもの: 別の package のテストから呼ばれる関数 (`export_test.go` は自分のフォルダのテストにしか入らない) と、`oracle` の production のコード (`oracle/cmd/*` を含む) から呼ばれる関数。1 件ずつ呼び出し元の package を確かめてから移す
- 移した後に製品の package で `deadcode` が 0 件になったら、製品の package に 1 件でも出たら落とす検査を足す (unused の検査が見ない export の名前を見る。`oracle` はテストの道具なので除く)。検査を足すなら、移した関数を 1 つわざと production のファイルへ戻して、検査が落ちることを確かめる
- `internal/` の標準ライブラリの写し (`pystr.IsAlpha` など) は、本家の対応表として揃えて置いた可能性がある。移す前に、揃えて置いた理由がコメントや README にあるかを確かめ、あれば残して検査の除外に理由つきで載せる

## 関連ファイル

- `src/ja-writing/diff/*.go` (入口)・`src/ja-writing/diff/*_oracle_test.go` (呼ぶ側)
- `scripts/check_unused_excluding_tests.sh` (export を見ないと決めた検査)

## 進捗

- 2026-11-26: issue 1113 の監査で見つけて起票した。反証レビュー (opus) で、根の `main` だけから辿っていた (`oracle/cmd/golden` の `main` が抜けていた) ことと、別の package のテストから呼ばれる関数は移せないことを直した。全件は同じコマンドで出し直せる
