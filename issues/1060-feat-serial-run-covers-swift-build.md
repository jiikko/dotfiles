# 1060 (feat): swift build を serial-run の列に入れる

> 🚨 **担当中: air5 (glogx)**（2026-11-11〜）

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

shim が適用されない経路: PATH に `~/dotfiles/bin` が無い起動元 (Dock から起動した Xcode の GUI・launchd・cron)、絶対パス (`/usr/bin/swift`)、`xcrun swift`、`swift-build` の直叩き。

### 案 B: 各スクリプトで `serial-run` を呼ぶ

repo ごとに書き足す。shim は要らないが、人の端末は拾えず、新しいスクリプトで忘れる。

### 案 C: hook の見送りを減らす

複合コマンドだけを直す案。`swift` 以外 (`make`・`xcodebuild`) にも適用されるが、スクリプトの中と人の端末は拾えない。案 A と別の問題なので、やるなら別の issue にする。

## 確かめること

- Xcode の Run Script build phase の PATH に `~/dotfiles/bin` が入るか。入るなら、Xcode の GUI のビルド中に `swift` を呼ぶ phase が列を待って止まらないか。
- SwiftPM と `xcodebuild` が内部で `swift` を PATH から起動するか (絶対パスなら shim を通らない)。
- `/usr/bin/swift` は xcode-select の trampoline なので、`DEVELOPER_DIR` と `TOOLCHAINS` を渡したときの解決先を実測する。
- 1 つのパイプラインに `swift` の重い段が 2 つある形で、前段が列を持ったまま止まらないか。hook は `multi` で見送るが、shim は字面を見ない。
- `swift run` と `swift package` を対象に足すか。`swift run` は長く動くサーバーを起動しうるので、列を占有しないかを見る。

## 受け入れ条件

- [x] PATH に `~/dotfiles/bin` がある環境で、スクリプトの中の `swift build` と Claude の複合コマンドの中の `swift build` が `serial-run` の列に入る。(`bin/swift`。通しの確認は下の進捗)
- [x] 情報だけの呼び出しと `--help` は待たない (`isHeavy` と shim のテスト)。
- [x] `serial-run` の列の中から呼んでも止まらず、二重に並ばない (入れ子)。**読み替え**: 「`serial-run` を二重に起動しない」から変えた。`SERIAL_RUN_HELD` の有無だけで素通しにすると、終わった列から漏れて残った値で排他を外れるので、列の中でも serial-run を起こして札を照合させる (1 回 約 27 ms)
- [x] `serial-run` が壊れていても `swift` は動く。(`SERIAL_RUN_FAIL_OPEN`。`tests/bin/test_serial_run_fail_open.sh`)
- [x] 待ちの上限を超えたときに、ビルドの失敗と区別できる。(上限は既定の 1 時間のまま。rc 125 と stderr の `serial-run:` の行で区別する。`--max-wait 0` にしない理由は下の決着)
- [x] shim が自分自身を呼び続けない (実体の解決に失敗したら落ちる)。
- [x] shim の起動の遅れを `swift --version` の前後で測り、結果を本文に書く。
- [x] `swift run` と `swift package` を対象にするかを決め、`hook_test.go` の期待値に反映する。(どちらも対象にしない)
- [x] CI では shim が列を使わない。
- [x] `isHeavy` の対象の語を増やしたら hook の入口の語も揃える (`tests/claude/test_serial_run_wrap.sh`)。(コマンド名は増やしていない。swift / xcodebuild は入口の語に既にある)
- [x] 入口の文書 (`src/serialrun/README.md`、`docs/tools/serial-run.md`) を更新する。(あわせて `_claude/rules/no-concurrent-spm-build-during-xcodebuild.md`)
- [x] (2026-11-11 にユーザー指示で追加) xcodebuild にも同じ shim を置く。

## 関連

- issue 869 (serial-run の新設)
- `bin/tmux` (PATH shim の前例)

## 進捗

- 2026-11-10: 起票。read-only のサブエージェント 2 体 (事実・設計) の反証レビューを通し、`isHeavy` の情報表示の扱い、判定の口、rc 125 の衝突、待ちの上限、実体の解決、拾えない経路を反映した。実装は未着手。
- 2026-11-11: 実装した。
  - `bin/swift`・`bin/xcodebuild` (PATH shim。共通部分は `bin/lib/serial_run_shim.sh`)、`serial-run --if-heavy` (重くなければ置き場に触らず exec)、
    `isHeavy` の swift の情報表示 (`--show-bin-path` / `--show-codecov-path` 系 / `--version`。`-X…` の値は飛ばす) と xcodebuild の情報表示の追加
    (`-showBuildSettingsForIndex` / `-showComponent` / `-showTestPlans` / `-find-executable` / `-find-library`)、`bin/serial-run` の `SERIAL_RUN_FAIL_OPEN`
  - 通しの確認 (本物の swift / xcodebuild と worktree の serial-run。置き場は `SERIAL_RUN_STATE_DIR` で隔離): 列を別の 1 本が持つ間、shim 経由の
    `swift build` は待った (wait=3.1s)。shim 経由の xcodebuild のビルド中に打った `swift build` は wait=6.2s で、xcodebuild の後に走った。
    `swift build --show-bin-path` と `xcodebuild -version` は置き場を作らなかった。`serial-run -- swift build` は history.log に 1 行 (二重に並ばない)
  - 起動の遅れ (実体を `/usr/bin/true` にして 50 回の平均を 3 回): 素通しの swift 10〜35 ms、serial-run を通す軽い呼び出し 45〜85 ms、
    列の中の呼び出し 約 27 ms。`xcodebuild -version` は実物で 99〜114 ms が 140〜147 ms (20 回の平均を 3 回)
  - 変異: `--if-heavy` の分岐を外す / swift の情報表示の判定を外す / `-showTestPlans` を外す / 別の checkout の shim を飛ばす処理を外す (互いに呼び合い 10 秒の上限で止まる) /
    readlink を相対名に戻して解決の失敗を通す (同じく呼び合う) / 本体 checkout の serial-run に戻す / `zsh -f` を外す / serial-run へ渡す stdin を
    `/dev/null` にする / `$0` の正規化を外す / bin/serial-run の `unset SERIAL_RUN_FAIL_OPEN` を外す。全部 red (`bin/mutate-verify`)
  - codex (gpt-6.1-sol, high) の敵対的レビューを 2 周。採った指摘は上の実装に入れた。採らなかったものと未確認のものは下の決着

## 決着

確かめること の結果:

- Xcode の Run Script build phase: my-products の app の `project.yml` の build phase で `swift build` / `swift test` / `swift run` / `swift package` を呼ぶものは無かった (grep、2026-11-11)。
  Dock から起動した Xcode の GUI の PATH には `~/dotfiles/bin` が無いので shim は届かない (届かない経路として文書に書いた)
- SwiftPM / xcodebuild の中から `swift` を PATH で起こすか: 確かめていない。起こしても、列の中なら `SERIAL_RUN_HELD` と祖先の照合で素通しになる
- `DEVELOPER_DIR` / `TOOLCHAINS`: shim は環境を変えずに PATH の次の `swift` (普段は `/usr/bin/swift` の trampoline) を exec するだけ。実際の解決先は測っていない
- パイプラインの 2 段: 下の「採らなかった指摘」
- `swift run` / `swift package`: 対象にしない (`swift run` は長く動くサーバーを起こしうる。`swift package` は短い)

採らなかった指摘 (再提起するなら、理由を覆す実測を添える):

- 「stdin がパイプなら列を見送る」(1 つのパイプラインに重い段が 2 つあると、後ろの段が列で待つ間パイプを読まず止まる): fastlane (Ruby の Open3) などは
  子の stdin にパイプを渡すので、xcodebuild の主な経路が並ばなくなる。代わりに待ちの上限 (1 時間) を外さず、止まっても 125 で解けるようにした。
  再提起の条件は `bin/lib/serial_run_shim.sh` の冒頭
- 「hook が包んだ `serial-run -- swift build` の外側にも無期限待ちと fail-open を」: shim を入れる前と同じ挙動 (上限 1 時間・ビルドできなければ 125)

未確認リスク (観測ポイント):

- `xcodebuild test` が起こすテストのプロセスに `TEST_RUNNER_PATH` で shim の PATH を渡し、テストから同じ repo の `swift build` / `xcodebuild` を同期で打つと、
  テストのプロセスは外側の列の子孫ではない (`SERIAL_RUN_HELD` も渡らない) ので、外側の列を待って止まりうる。obaket のテストのコードは swift / xcodebuild を起こしていない (grep、2026-11-11)
- `xcodebuild -showBuildSettings` などの情報表示は列に並ばないが、package graph を解決する。同じ package の `swift build` と重なったときに 7.5 時間の停止と同じ形になるかは確かめていない (fastlane もこの呼び出しを打つ)

改善の評価: 前は Claude の Bash が 1 つのコマンドとして打った形だけが並び、スクリプトの中・人の端末・hook が見送った複合コマンドの swift / xcodebuild は列の外で重なっていた。
今は PATH に `~/dotfiles/bin` がある起動元なら、同じ repo の xcodebuild と swift build / test が 1 本ずつになる (通しの確認で、xcodebuild の後に swift build が待った)。
足した複雑さ: shim 2 本と共通の lib (約 80 行)、`--if-heavy`、fail-open の分岐。失うもの: swift / xcodebuild の起動のたびの遅れ (上の実測) と、列が混んでいるときの待ち
