# 1097 (test): devtool の mutate が読む obaket の出力の形と、dotfiles の fixture・テストのずれを検出する

起票日: 2026-11-19

## 概要

`devtool/bin/mutate` は、obaket の道具が出す行の形を読む。書き手 (obaket) と読み手 (dotfiles) が別の repo に分かれ、
dotfiles のテストはその形を手で写した行で試している。obaket が形を変えても dotfiles のテストは緑のまま通り、
ずれても mutate がエラーを出さずに働かなくなる形がある。

**今は書き手と読み手を突き合わせる検査が、自動ではどこでも走っていない** (下の「今の検出」)。統合の前は、obaket の Xcode Cloud が
`make lint-gates` の self-test で毎回突き合わせていた。統合で読み手が dotfiles に移り、Xcode Cloud には dotfiles が無いので、
その区間を飛ばすようにした (obaket `c86ba946f`)。統合で検出が弱くなった分を取り戻すのがこの issue。

## 読み手が頼っている形 (2026-11-19 に確認)

| 形 | 書き手 (obaket) | 読み手 (dotfiles `devtool/bin/mutate`) | ずれたときに起きること |
|---|---|---|---|
| ① `<label>: parallel test cases: N (passed P, failed F, skipped S, expected failure X, unreadable U)` | `macOS/bin/run-logged-command` の `COUNT_LINE_AWK` の END | `summarize` の python の `par_summary` の正規表現。`failed F` は `run=failed` の根拠にもなる | 要約が読めないと、失敗時の末尾に残った `Test case '` の行と他の集計だけで数える。成功の run で並列の要約しか件数の手がかりが無ければ抽出できず判定不能、直列と並列が混ざる run では並列の分だけ少なく数え、baseline と食い違う |
| ② `<label>: complete log: <絶対パス>` (成功時と失敗時で出す箇所が別) | `macOS/bin/run-logged-command` | `keep_complete_logs` (`: complete log: ` で探す) と、lock 待ちの判定 (この行があれば lock は取れている) | 目印が消えると全文のログを run の置き場へ写さない。lock 待ちの判定の目印にもなっている (下の組み合わせ) |
| ③ 行頭の `run-with-test-profile-residue: another checked run owns ` (末尾の `; retry after it ends` は読まない) | `macOS/bin/run-with-test-profile-residue` | `lock_busy_line` | 判定不能のときに「lock 待ち。回し直せばよい」と案内しなくなる |
| ④ xcodebuild の行のうち要約に残すもの: Swift Testing の `Test run with …`、XCTest の `Test Suite 'All tests'` / `'Selected tests'` と直後の `Executed …`、失敗した個別の行の再掲、`** TEST FAILED **`、`Failing tests:` のブロック | `macOS/bin/run-logged-command` が要約にどの行を残すか | `summarize` の件数・落ちたテストの名前・`run=failed` | 件数や名前が欠けると、変異でテストが落ちても RED を出せない |

組み合わせで起きる誤り: 失敗時の ② だけがずれ、全文の末尾に行頭から始まる ③ の文言が引用されていて、件数も名前も取れない run では、
mutate が「テストを始めなかったので回し直す」と誤って案内しうる (dotfiles の `test_mutate.sh` は偽の runner でこの誤りを止めるケースを持つが、本物の書き手では見ていない)。

## 今の検出 (2026-11-19)

- ①④ は obaket の `macOS/bin/run-logged-command-test` が、本物の `run-logged-command` の要約を `bin/mutate --summarize` (呼び口 → dotfiles の本体) に通し、
  件数・落ちた件数・代表の名前を突き合わせる。`** TEST FAILED **` や `Failing tests:` のブロックが消えただけで落ちる検査は無い
- **この self-test が走る場所**: 手元の `make lint-gates`、`OBAKET_GATE_SELFTEST=run` を付けた push。**pre-push は既定で gate の self-test を飛ばす**
  (`.githooks/pre-push` の `OBAKET_GATE_SELFTEST` が未設定なら skip)。**Xcode Cloud では dotfiles が無いので、mutate で読む区間を飛ばす**。
  つまり今は、誰かが手元で `make lint-gates` を打たない限り走らない
- ② は `run-logged-command-test` が行の形 (成功時に絶対パスで出ること) を見るが、mutate がそれを読んで写すこと・失敗時の行は見ていない
- ③ は書き手側の `run-with-test-profile-residue-test` が rc=75 と `another checked run owns` を見るが、読み手が要る行頭全体と、mutate に通したときの案内は見ていない
- dotfiles の `tests/devtool/test_mutate.sh` は ①②③ を手で写した行で試す (偽の runner の echo)。obaket が形を変えても緑のまま

## 対応方針 (案。着手時に決める)

- **書き手の repo (obaket) で、本物の書き手の出力を本物の読み手 (`bin/mutate`) に通す検査を ②③④ にも足す**のを第一候補にする。① の今の形 (`run-logged-command-test`) に揃える
  - ②: 本物の `run-logged-command` を通した検証コマンドで `bin/mutate` を回し、成功時と失敗時の両方で全文のログが run の置き場へ写ったことを見る (本物の xcodebuild は要らない。fixture を流す)
  - ③: `run-with-test-profile-residue` が lock を取れなかったときの出力を `bin/mutate` に通し、lock 待ちの案内が出ることを見る。本物の書き手を通った失敗 (②の行がある) を lock 待ちと扱わないことも見る
  - ④: `** TEST FAILED **` と `Failing tests:` のブロックだけが手がかりになる形を本物の書き手に通す
- **どこで走らせるかを先に決める** (置き場より大事)。今の self-test の置き場では、pre-push も Xcode Cloud も走らない。候補:
  pre-push で飛ばさない gate (self-test でなく通常の gate) にする / dotfiles の make test から obaket の書き手を起こす (dotfiles に obaket が無い環境では飛ばす) など。
  所要 (pre-push の窓を延ばさない) と、dotfiles の側だけを変えたとき (読み手の正規表現を直したとき) にも掛かるかで選ぶ
- dotfiles の `test_mutate.sh` の手で写した行には、「形の正本は obaket の道具。ずれは obaket の検査が見る」と書く (今は書いていない)
- 採らない案: dotfiles に obaket の形の写しを 2 つ目として置いて比べる (写しが増えるだけで、obaket が変えたことを dotfiles は知らない)
- 1095 (別の repo への切り出し) のときも、この検査は obaket に残る (読み手の呼び口が obaket にあるため)。そのとき、obaket のテストが直接読んでいる fixture の置き場と `test_mutate.sh` の参照先も決め直す

## 関連

- 1093 (mutate の本体の統合。done) で ① の読み取りを持つ本体が dotfiles に移り、書き手と読み手が別の repo になった
- obaket の `bin/mutate-test`・`macOS/bin/run-logged-command-test`・`bin/lib/xcode-cloud.bash`

## 反証レビュー (2026-11-19、codex 1 周)

採った指摘: pre-push は既定で self-test を飛ばす (「pre-push で落ちる」は誤り) / 読み手が頼る行に ④ が漏れていた / ② と ③ の組み合わせの誤った案内 /
① の影響の書き過ぎ / ③ で読み手が要るのは行頭までで、書き手側に部分的な検査がある / 1095 のときの fixture の参照先。
反証できなかった: ② の実ログを mutate が写す検査と、③ の実際の出力を mutate に通す検査は無い。dotfiles の固定の入力だけでは obaket の形の変更を検出できない。

## 進捗

(未着手)
