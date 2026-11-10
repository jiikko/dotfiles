# 1060 (feat): swift build を serial-run の列に入れる

起票日: 2026-11-10

## 報告者の原文

> swiftbuldがdotfilesにあるserial-runになるようにしたいのでissues mdに書いといて

## 概要

`swift build` が、どこから打たれても dotfiles の `serial-run` の順番待ちを通るようにする。今は Claude の Bash が 1 つのコマンドとして打った形しか並ばない。

## 現状

`src/serialrun/hook.go` の `isHeavy` は、`swift build` と `swift test` を直列化の対象にしている (`--help` などは除く)。`_claude/hooks/serial-run-wrap.sh` が Claude の Bash の前に `serial-run -- ` を差し込む。

並ばない経路は次のとおり。

| 経路 | 並ばない理由 | 例 |
|---|---|---|
| Claude の複合コマンド | hook が字句を読み切れない形は書き換えない。`hook.log` の見送りの理由は `parse` / `multi` / `heredoc` / `arith` | 2026-11-10 の obaket で、`for` の中の `make test-only` は `skipped:multi` や `skipped:parse` になった。`serial-run status` が「走っているものも待っているものも無い」と出ている間に `xcodebuild` が 2 本動いていた (ロードアベレージ 29〜34) |
| スクリプトの中 | hook は字面しか見ない | obaket の `bin/ftp-test-server` / `bin/sftp-test-server` / `bin/r4-capture` の `swift test`、`bin/obaket-sftp` の `swift build` |
| 人の端末・git hook・codex・pro-con の PG | hook の対象外 (`src/serialrun/README.md`) | |
| `swift run` / `swift package` / `swift-build` | `isHeavy` の対象外 | |

上の表の複合コマンドの実例は `make` のもので、`swift build` では同じ形を確かめていない。hook の判定は語で分岐しないので、同じ見送りになると見ている。

列は既定で repo ごとで、同じ repo の worktree は同じ列に入る (`scope.go` の git の common dir)。別の repo の `swift build` は並ばない。`SERIAL_RUN_SCOPE=machine` にするとマシン全体で 1 本になる。

## なぜ要るか

- `xcodebuild` の実行中に同じ checkout の SwiftPM で `swift build` / `swift test` を走らせると、無限待ちになることがある (`_claude/rules/no-concurrent-spm-build-during-xcodebuild.md`。obaket で 7.5 時間停止)。
- 負荷が重なると、時間に敏感なテストが落ちて切り分けの手間が増える。

## 対応方針

### 案 A (推奨): PATH の先頭に `bin/swift` の shim を置く

`bin/tmux` と同じ層で、スクリプトの中の呼び出しも含めて拾える。`swift build` と `swift test` だけ `serial-run --` を通して実体を起動し、他はそのまま実体へ渡す。

守ること:

- 実体は絶対パスで解決し、shim 自身でないことを確かめてから `exec` する (`_claude/rules/path-shim-must-resolve-real-binary.md`)。
- `--help` と情報だけの呼び出しは待たせない。`bin/obaket-sftp` の `swift build --show-bin-path` は実際に使われている。判定は `isHeavy` を唯一の正本にして、shim に写さない。
- `serial-run` の中から呼ばれたら素通しにする (既存の入れ子の判定)。
- `serial-run` が使えないときは順番を待たずに実体を起動する (fail-open。README の方針と同じ)。
- 引数の無い `swift` (REPL) と標準入力の扱いを変えない。

失うもの: `swift` を起動するたびに shim の分の時間がかかる (未測定)。列の待ちで、`swift build` を打った人の端末がすぐ始まらなくなる。

### 案 B: 各スクリプトで `serial-run` を呼ぶ

repo ごとに書き足す。shim は要らないが、人の端末は拾えず、新しいスクリプトで忘れる。

### 案 C: hook の見送りを減らす

複合コマンドだけを直す案。`swift` 以外 (`make`・`xcodebuild`) にも効くが、スクリプトの中と人の端末は拾えない。案 A と別の問題なので、やるなら別の issue にする。

## 確かめること

- Xcode の Run Script build phase の PATH に `~/dotfiles/bin` が入るか。入るなら、Xcode の GUI のビルド中に `swift` を呼ぶ phase が列を待って止まらないか。
- SwiftPM と `xcodebuild` が内部で `swift` を PATH から起動するか (絶対パスなら shim を通らない)。
- `/usr/bin/swift` は xcode-select の trampoline なので、絶対パスの解決先が `DEVELOPER_DIR` を尊重するか。
- `swift run` と `swift package` を対象に足すか。`swift run` は長く動くサーバーを起動しうるので、列を占有しないかを見る。

## 受け入れ条件

- [ ] スクリプトの中の `swift build` と、Claude の複合コマンドの中の `swift build` が `serial-run` の列に入る。
- [ ] 情報だけの呼び出しと `--help` は待たない。
- [ ] `serial-run` の列の中から呼んでも止まらない (入れ子)。
- [ ] `serial-run` が壊れていても `swift` は動く。
- [ ] shim が自分自身を呼び続けない (実体の解決に失敗したら落ちる)。
- [ ] `isHeavy` の対象の語を増やしたら hook の入口の語も揃える (`tests/claude/test_serial_run_wrap.sh`)。
- [ ] 入口の文書 (`src/serialrun/README.md`、`docs/tools/serial-run.md`) を更新する。

## 関連

- issue 869 (serial-run の新設)
- `bin/tmux` (PATH shim の前例)
