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
| Claude の複合コマンド | hook が書き換えを見送る。理由は `parse`・`heredoc`・`arith` (字句を読み切れない)、`multi` (パイプがあり重い段が 2 つ以上)、`path` (PATH を書き換える)、`manual` (手で書いた serial-run) | 2026-11-10 の obaket で、関数定義を含むコマンドが `skipped:parse`、`for` の `make test-only` の後ろに `make test` と `\| cut` を持つコマンドが `skipped:multi` になった。`serial-run status` が「走っているものも待っているものも無い」と出ている間に `xcodebuild` が 2 本動いていた (ロードアベレージ 29〜34) |
| スクリプトの中・コマンド置換の中 | hook は字面しか見ず、`$(...)` の中も読まない | obaket の `bin/test-shared` / `bin/ftp-test-server` / `bin/sftp-test-server` / `bin/webdav-test-server` / `bin/r4-capture` の `swift test`、`bin/obaket-sftp` の `swift build` |
| 人の端末・git hook・codex・pro-con の PG | hook の対象外 (`src/serialrun/README.md`) | |
| `swift run` / `swift package` / `swift-build` / `swift-test` | `isHeavy` の対象外 | |

上の表の複合コマンドの実例は `make` のもので、`swift build` では同じ形を確かめていない。`parse`・`heredoc`・`arith` は語に依らないので同じになる。`multi` は重い段の数で決まる。

列は既定で repo ごとで、同じ repo の worktree は同じ列に入る (`scope.go` の git の common dir)。別の repo の `swift build` は並ばない。`SERIAL_RUN_SCOPE=machine` にするとマシン全体で 1 本になる。

## なぜ要るか

- `xcodebuild` の実行中に同じ checkout の SwiftPM で `swift build` / `swift test` を走らせると、無限待ちになることがある (`_claude/rules/no-concurrent-spm-build-during-xcodebuild.md`。obaket で 7.5 時間停止)。
- 負荷が重なると、時間に敏感なテストが落ちて切り分けの手間が増える。

## 対応方針

### 案 A (推奨): PATH の先頭に `bin/swift` の shim を置く

`bin/tmux` と同じ層で、スクリプトの中の呼び出しも含めて拾える。`swift build` と `swift test` だけ `serial-run --` を通して実体を起動し、他はそのまま実体へ渡す。

守ること:

- 実体を解決し、shim 自身でないことを確かめてから `exec` する (`_claude/rules/path-shim-must-resolve-real-binary.md`)。`/usr/bin/swift` への固定は swiftly などの選択を上書きするので、自分の dir を除く PATH の走査と比べて決める。
- `isHeavy` に swift の情報だけの呼び出し (`--show-bin-path` など) の除外を足す。今の `isHeavy` はこれを重いと判定する。`bin/obaket-sftp` の `swift build --show-bin-path` は実際に使われている。
- shim から判定を呼ぶ口を足す (`serial-run` の判定専用の subcommand など)。今は `serial-run hook` (hook JSON) しか無い。`isHeavy` を shim に写さない。
- `SERIAL_RUN_HELD` が立っていたら、`serial-run` を起動せずに実体を `exec` する。hook が包んだ `serial-run -- swift build` が shim を通るので、入口の起動が 2 回分かかるのを避ける。
- `bin/serial-run` は zsh と Go のビルド確認を通るので遅い。ソースを変えた直後は同期ビルドを待つ。
- fail-open を作り直す: `serial-run` の rc 125 は入口のビルド失敗・道具の失敗・待ちの上限切れで共通で、`exec` した後の shim には区別できない。事前に使えることを確かめるか、別の逃げ道 (環境変数) を用意する。
- 待ちの上限 (既定 1 時間) で 125 になると、呼び出し元のスクリプトはビルドやテストの失敗と読む (`bin/obaket-sftp` の `if ! swift build`、`bin/*-test-server` の `|| rc=$?`)。shim では上限を外すか、列の待ちで落ちたと分かる形にする。
- CI では shim を無効にする (`make/serial-run.mk` と同じ)。
- 引数の無い `swift` (REPL) と標準入力の扱いを変えない。

失うもの: `swift` を起動するたびに shim の分の時間がかかる (未測定)。列の待ちで、`swift build` を打った人の端末がすぐ始まらなくなる。

効かない経路: PATH に `~/dotfiles/bin` が無い起動元 (Dock から起動した Xcode の GUI・launchd・cron)、絶対パス (`/usr/bin/swift`)、`xcrun swift`、`swift-build` の直叩き。

### 案 B: 各スクリプトで `serial-run` を呼ぶ

repo ごとに書き足す。shim は要らないが、人の端末は拾えず、新しいスクリプトで忘れる。

### 案 C: hook の見送りを減らす

複合コマンドだけを直す案。`swift` 以外 (`make`・`xcodebuild`) にも効くが、スクリプトの中と人の端末は拾えない。案 A と別の問題なので、やるなら別の issue にする。

## 確かめること

- Xcode の Run Script build phase の PATH に `~/dotfiles/bin` が入るか。入るなら、Xcode の GUI のビルド中に `swift` を呼ぶ phase が列を待って止まらないか。
- SwiftPM と `xcodebuild` が内部で `swift` を PATH から起動するか (絶対パスなら shim を通らない)。
- `/usr/bin/swift` は xcode-select の trampoline なので、`DEVELOPER_DIR` と `TOOLCHAINS` を渡したときの解決先を実測する。
- 1 つのパイプラインに `swift` の重い段が 2 つある形で、前段が列を持ったまま止まらないか。hook は `multi` で見送るが、shim は字面を見ない。
- `swift run` と `swift package` を対象に足すか。`swift run` は長く動くサーバーを起動しうるので、列を占有しないかを見る。

## 受け入れ条件

- [ ] PATH に `~/dotfiles/bin` がある環境で、スクリプトの中の `swift build` と Claude の複合コマンドの中の `swift build` が `serial-run` の列に入る。
- [ ] 情報だけの呼び出しと `--help` は待たない (`isHeavy` と shim のテスト)。
- [ ] `serial-run` の列の中から呼んでも止まらず、`serial-run` を二重に起動しない (入れ子)。
- [ ] `serial-run` が壊れていても `swift` は動く。
- [ ] 待ちの上限を超えたときに、ビルドの失敗と区別できる。
- [ ] shim が自分自身を呼び続けない (実体の解決に失敗したら落ちる)。
- [ ] shim の起動の遅れを `swift --version` の前後で測り、結果を本文に書く。
- [ ] `swift run` と `swift package` を対象にするかを決め、`hook_test.go` の期待値に反映する。
- [ ] CI では shim が列を使わない。
- [ ] `isHeavy` の対象の語を増やしたら hook の入口の語も揃える (`tests/claude/test_serial_run_wrap.sh`)。
- [ ] 入口の文書 (`src/serialrun/README.md`、`docs/tools/serial-run.md`) を更新する。

## 関連

- issue 869 (serial-run の新設)
- `bin/tmux` (PATH shim の前例)

## 進捗

- 2026-11-10: 起票。read-only のサブエージェント 2 体 (事実・設計) の反証レビューを通し、`isHeavy` の情報表示の扱い、判定の口、rc 125 の衝突、待ちの上限、実体の解決、拾えない経路を反映した。実装は未着手。
