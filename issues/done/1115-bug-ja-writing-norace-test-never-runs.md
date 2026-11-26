# 1115 (bug): ja-writing の !race のテスト 1 本が make test でも CI でも走らない

起票日: 2026-11-26

## 概要

`src/ja-writing/check/replay_recording_norace_test.go` の `TestReplayRecordingCoversOutcomes` は、`//go:build !race` のファイルにある。
`src/ja-writing/Makefile` の `test` は、`go test -race ./...` (この tag のファイルを外す) の後に、`NORACE_CHECKS` に挙げたテストだけを race なしで回す。
このテストは `NORACE_CHECKS` に無いので、どちらの段でも走らない。CI (`.github/workflows/src_ja-writing.yml` → `_go-project.yml`) も `make -C src/ja-writing test` を呼ぶだけなので同じ。
issue 1113 の監査 (false-green、F4: 走っていないのに緑) の発見。

## 詳細

確かめたこと (2026-11-26):

- `//go:build !race` のテストは 14 本、`NORACE_CHECKS` は 13 本。食い違いはこの 1 本だけ
- `go test -race -list '.*' ./check/` の 66 本に、このテストは出ない (同じ tag の `TestRunMatchesPythonRecording` も出ない。こちらは `NORACE_CHECKS` で回る)
- race なしで単独に回すと通る (`--- PASS: TestReplayRecordingCoversOutcomes`)
- このテストは 2026-11-21 の commit e060553f7 (verify --replay の写し) で入った。それ以来、一度も走っていない

このテストの役目は「空振りを止める」こと (作り直しの結果 ok・mismatch・timeout・skipped と、終了コード 0〜3 のどれもが記録に出ることを見る)。
記録が作り直されて、ある結果の case が消えても、今は誰も気づかない。

発火条件: verify --replay の記録 (`testdata/golden/` の replay の記録) を作り直して、ok・mismatch・timeout・skipped のどれか、または終了コード 0〜3 のどれかの case が消えたとき。

## 対応方針

- 根本: `NORACE_CHECKS` を手で書かない。`!race` の tag のファイルにあるテストを機械で集めて回す (例: `go test -list` を race なしで打ち、`-race` の一覧に無いものを回す)。手の一覧は、tag のファイルにテストを足すたびに漏れる
- すぐの手当てとして `NORACE_CHECKS` に足すだけでも走るようになるが、次の漏れは止まらない。上の形にするか、一覧と tag のファイルのテストが一致することを見る検査を置く
- 直した後に、一覧から 1 本わざと外して、検査が落ちることを確かめる

## 関連ファイル

- `src/ja-writing/Makefile` (`NORACE_CHECKS` と `test`)
- `src/ja-writing/check/replay_recording_norace_test.go`
- `.github/workflows/_go-project.yml` (`make -C <dir> test`)

## 進捗

- 2026-11-26: issue 1113 の監査で見つけて起票した。反証レビュー (opus) で、ほかの経路 (root の Makefile・他の workflow・scripts) でも race なしで回っていないことが確かめられた
- 2026-11-26: 手書きの一覧 `NORACE_CHECKS` をやめ、`src/ja-writing/norace_tests.sh` が tag のファイルからテストを集めて回す形にした。`make test` は `go test -race ./...` の後にこれを呼ぶ
  - 集め方は次のとおり。tag のファイルを持つ package ごとに、`go test -list` を race なしと `-race` で打つ。race なしにだけ出るテストを 1 本ずつ回し、テストごとに rc と `--- PASS: <名前> (` の行を見る
  - 空振りを止める検査として、集めた名前を、tag のファイルの `func Test…(` の行から読んだ名前と package ごとに突き合わせる。本数だけでなく名前も見る。tag のファイルが 0 本のときと、テストが 0 本のときも rc=1
  - 変えたファイルは `src/ja-writing/Makefile`・`src/ja-writing/norace_tests.sh` (新しく足した)・`src/ja-writing/README.md` (「開発」節に 1 行)
  - 確かめた結果 (手元の macOS、2026-11-26)
    - `make -C src/ja-writing test` は rc=0。「race なしで回すテストは 14 本」の後に `--- PASS:` が 14 行出た (`--- PASS: TestReplayRecordingCoversOutcomes (0.01s)` を含む)。終わりの行は「race なしのテスト 14 本が走って通った」
    - 集め方をわざと壊した (スクリプトの写しで、集めた名前の終わりの 1 本を落とす)。rc=1 で「集めたテストが tag のファイルの字面と合わない (./check)」
    - tag のファイルを探す正規表現をわざと壊した (写しで `race` を `racex` にする)。rc=1 で、tag のファイルが 0 本だという文が出た
    - `TestReplayRecordingCoversOutcomes` の先頭に `t.Fatal` を足した。rc=1 で「TestReplayRecordingCoversOutcomes (./check) が走っていないか落ちた (rc=1)」
    - 同じテストの先頭に `t.Skip` を足した。rc=1 で、同じ文の括弧の中は (rc=0)。`go test` は通っても `--- PASS:` の行が無いので落ちる
    - `/bin/bash` (3.2.57) でスクリプトを回しても 14 本が走って通った
    - わざと壊した箇所は全部戻した (`git diff` が壊す前と同じことを見た)。`shellcheck` と `scripts/check_comment_provenance.sh --list` は、スクリプトに指摘を出さなかった
  - 対応方針の 3 項目の結果
    - 一覧を手で書かない: 済み。tag のファイルにテストを足せば、ほかを直さなくても回る
    - 一覧と tag のファイルのテストが一致することを見る検査: 済み (上の突き合わせ)
    - 1 本わざと外して検査が落ちることを確かめる: 済み (上の「集め方をわざと壊した」)
  - 確かめていないのは、CI の runner での実行 (push していない)
- 2026-11-26: 敵対的レビュー (opus) の指摘 3 件を直した
  - repo の `make test` が赤になる指摘。`tests/scripts/test_enumerations_are_derived.sh` が、足したスクリプトを `LINT_DIRS` の外の shell script として落としていた。`scripts/discover_shell_scripts.sh` の `LINT_DIRS` へ、スクリプトのパスを足した。module ごと足すと testdata と third_party を掘るので、足さない。このテストは rc=0、`make test-lint` も rc=0
  - tag のファイルの Fuzz と Example の関数は、どちらの集め方にも出ない。回すのは Test だけのままにし、tag のファイルに `func Fuzz`・`func Example` があれば rc=1 で止める。写しに `FuzzX` を足して rc=1 を見た
  - `!(race && cgo)` のような tag は、スクリプトの探し方に当たらず package ごと外れる。`*_test.go` の `//go:build`・`// +build` の行が `race` の語を含み、集める対象から外れていれば rc=1 で止める。写しにこの tag のファイルを足して rc=1 を見た。race を肯定で使う tag のテストのファイルは今 0 本で、足せば同じく止まる
  - README の追記から「src/ja-writing で打つ」を消した (スクリプトは自分の置き場へ移ってから動く)
  - 直した後の `make -C src/ja-writing test` は rc=0 で、race なしの 14 本が走って通った
- 2026-11-26: 敵対的レビューの 2 周目 (opus) の指摘 1 件を直した
  - `func /* x */ FuzzA(` のように func と名前の間にコメントを挟むと、Fuzz・Example の字面の grep をすり抜け、走らないまま rc=0 になった (gofmt もこの形を直さない)
  - 字面の grep をやめ、package ごとに `go test -list '^(Fuzz|Example)'` を race なしと `-race` で打ち、race なしにだけ出るものがあれば rc=1 で止める形にした (Test を集めるのと同じ読み方)
  - `go test -list` の結果は変数に受けてから比べる (コマンド置換の引数の中で落ちると、外の rc に伝わらない)
  - 作業の木に `func /* x */ FuzzZProbe(` を持つ tag のファイルを一時に足して rc=1 (`… がある (./check)。… FuzzZProbe`) を見て、消した。直した後の `./norace_tests.sh` は 14 本が走って通り、`make test-lint` は rc=0。直す前の木で root の `make test` は rc=0
  - 2 周目で壊せなかった観点: 誤って止める形 (BSD と GNU の grep の違いを含む)・`LINT_DIRS` にファイルのパスを入れる形。gofmt が直す書き方 (行頭の空白・BOM・CRLF の tag) は、このスクリプト単独ではすり抜けるが CI の lint (gofmt) が止める
