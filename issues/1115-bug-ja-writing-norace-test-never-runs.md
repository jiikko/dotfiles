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
