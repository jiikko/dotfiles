# 1116 (cleanup): ja-writing の production のファイルに、テストからしか呼ばれない関数が 59 件ある

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
- 2026-11-26: 一覧を出し直した (115 件。製品の package は 59 件)。47 件を同じフォルダの `export_test.go` へ移した。関数の中身は変えていない
  - 移した件数: diff 37・visibility 4・check 3・lists 2・internal/pyre 1 (`Match.Expand`)
  - 置き場: `diff/`・`visibility/`・`lists/`・`internal/pyre/` の `export_test.go` (新しく作った) と、前からある `check/export_test.go`
  - 一緒に移した下請け (check): 型 `DirFiles` と変数 `toolVersionPattern`。製品のコードから使われなくなり、unused の検査に出た
  - 一緒に移した下請け (diff): 型 `ChangeRisks`。`ChangeRisksOf` だけが使う
  - 呼び出し元の確かめ方: 59 件を全部いったんテストのファイルへ移し、`go vet ./...` を掛けた (全部の package のテストと `oracle` をコンパイルする)
    - 落ちたのは 2 件だけ。`Pattern.Pattern` は lint・visibility のテストが呼ぶ。`Lazy.Flags` は lint・diff のテストが呼ぶ
    - `oracle` の production のコードから呼ばれるものは無かった
  - 残した 12 件と理由 (`src/ja-writing/deadcode_check.sh` の `ALLOW` に同じ理由を書いた):
    - `pyre.Pattern.Pattern`・`pyre.Lazy.Flags`: 別の package のテストから呼ばれるので移せない
    - `pystr.IsAlpha`・`IsAlnum`・`IsDecimal`・`IsDigit`・`IsNumeric`: 標準ライブラリの写しとして揃えて置いたもの
    - `pyre.Match.GroupDict`・`pyjson.RoundInt`・`difflib.SequenceMatcher.FindLongestMatch`: 同じく揃えて置いたもの
    - 揃えて置いた根拠: README の「標準ライブラリを写す package」の表が、写す範囲に挙げている。挙げているのは `isspace` などの分類・`groupdict`・`round(x)`・`FindLongestMatch`。issue 1073 と 1081 の API の一覧にもあり、記録 (`pystr.json`・`pyre.json`・`json.json`・`difflib.json`) と比べている
    - `pyre.Match.Pos`・`Match.EndPos`: テストから呼ばれていない (概要の「どれもテストからだけ呼ばれる」は、この 2 件に当たらない)。移す先が無く、消すかどうかはこの issue で決めていないので残した
  - `pyre.Match.Expand` は移した。README の表にも issue 1073 の API の一覧にも無く、同じフォルダの `golden_test.go` だけが呼ぶ
  - 検査を足した: `src/ja-writing/deadcode_check.sh`。`make -C src/ja-writing lint` の最後に呼ぶ (CI の lint の job と、root の `make test` の `test-go-lint` が通る)
    - 落ちる条件: `oracle/` と `ALLOW` の 12 件を除いて 1 件でも出た。`ALLOW` の行が報告されなくなった。`oracle/` の行が 0 件 (道具は module を読めていない)
    - 道具の取り方: バージョンを固定した `deadcode` を、一度だけ `go install` でビルドする。置き場は `scripts/golangci_lint.sh` と同じ (`~/Library/Caches/dotfiles-go-tools`)
    - go.mod の `tool` には足していない (x/tools とその依存が ja-writing の go.sum に入る)。go.mod と go.sum は変えていない
    - CI の所要は未実測。`_go-project.yml` の道具のキャッシュの鍵は、golangci-lint のバージョンと `scripts/golangci_lint.sh` の中身だけで決まる。鍵が当たった run はキャッシュを保存し直さないので、`deadcode` を毎回ビルドし直す。push の後の lint の job で所要を見る
    - 見ないもの (script の冒頭に書いた): 型・変数・定数 (`deadcode` は関数だけを報告する)。テストからだけ使う export の型が製品のファイルに残っていても出ない
    - `export_test.go` へ移した関数は、`.golangci.local.yml` の forbidigo の対象から外れる (`_test.go` は除外)。移した 47 件に、forbidigo が止める呼び出しは無い
  - 確かめた結果 (作業の木。commit の前):
    - `make -C src/ja-writing lint test`: rc=0。lint は `0 issues.` と `deadcode_check: 製品の package に届かない関数なし`。test は `go test -race ./...` の全部の package で ok、`norace_tests: race なしのテスト 14 本が走って通った`
    - `make test-dupscan-gate`: rc=0 (新しい重複なし)。`make test-unused-excluding-tests`: rc=0
    - `make test-lint`・`make test-shellcheck`・`make test-go-project-lanes`: rc=0
    - 検査が落ちること (1): `lists.SHA256` を `lists/lists.go` へ戻すと rc=1 (`どの main からも届かない: lists/lists.go:SHA256`)。戻して rc=0
    - 検査が落ちること (2): `ALLOW` の `RoundInt` の行の名前を変えると rc=1 (`ALLOW の行がもう報告されない`)。戻して rc=0
    - 移した後の `deadcode` の製品の package の行は、残した 12 件だけ
- 2026-11-27: 敵対的レビュー (opus) で P1・P2 は見つからなかった (テストの本数は変更の前後で 269 本ずつ一致・generics の関数とメソッドも検査に出る・deadcode が落ちると rc=1・go.mod は書き換わらない)。
  P3 の 3 件 (テストの道具の main だけが呼ぶ関数・interface のメソッドと同じ名前のメソッド・中に go.mod を置いたフォルダ) は、検査が見ないものとして `deadcode_check.sh` の冒頭に書いた。root の `make test` は rc=0
- 残したもの: `pyre.Match.Pos`・`Match.EndPos` はテストからも呼ばれない。re の Match の写しとして ALLOW に残し、消すかは決めていない
