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

### 決めたこと (2026-11-19)

- **どこで走らせるか**: obaket の lint-gates に、self-test ではない通常の gate `check-mutate-format-contract` として置く (`run-gate-selftest` に通さない)。
  pre-push でも走る (self-test を飛ばす 2026-10-17 のユーザー決定は、self-test の所要が理由。この gate は 4〜5 秒 (手元の実測。素の実行で 3.9〜4.6 秒、red team が bash 3.2・4 本同時で 3〜5 秒))。Xcode Cloud では dotfiles が無いので飛ばしたことを 1 行出す
- **dotfiles の側だけを直したとき**: `tests/devtool/test_mutate_obaket_contract.sh` が、obaket の checkout (`OBAKET_ROOT`、無ければ `~/src/my-products/apps/obaket`) の gate に
  この dotfiles の作業ツリーを読み手として渡して回す。checkout が無い・gate が無い (CI もこれ) なら exit 77 で skip
- `run-logged-command-test` の、要約を `bin/mutate` で読む区間 (①④) は gate へ移した (同じ突き合わせを 2 か所に置かない)

### 受け入れ条件

- [x] ① 並列の要約・④ 集計行と落ちたテストの名前 (移した区間)
- [x] ④ `Failing tests:` のブロックだけが名前の手がかりになる形 / `** TEST FAILED **` だけが失敗を言う形を本物の書き手に通す
- [x] ② 本番の `make test` / `test-only` と同じ `run-with-test-profile-residue -- run-logged-command` の連鎖を検証コマンドにして `bin/mutate` を通しでまわし、成功の run (baseline) と失敗の run (対象を壊した run) の全文が run の置き場に写ることを `cmp` で見る
- [x] ③ 使い捨ての HOME の lock を別の子に持たせ、本物の `run-with-test-profile-residue` が始めずに終わった run を `bin/mutate` が lock 待ちと読む。
  `run-logged-command` を通った失敗の末尾に行頭から lock の文言があっても lock 待ちと読まず、注記を出す
- [x] `test_mutate.sh` と `devtool/bin/mutate` の冒頭に、手で写した行の正本は obaket の道具で、ずれは上の 2 つが見ると書く
- [x] obaket の `OBAKET_GATE_SELFTEST=run make lint-gates` が緑 (71 gates passed)。pre-push (self-test を飛ばす) でも `✓ check-mutate-format-contract` が出た
- [x] 敵対的レビュー (red team、opus 2 周) の指摘の裏取りと対応 (下節)
- [x] obaket の push (commit「test(lint): 書き手の出力を本物の bin/mutate に通して形のずれを止める gate を足す」)、umbrella の参照の更新、dotfiles の push。
  dotfiles の `make test` で `test_mutate_obaket_contract.sh` が skip ではなく `[ok]` で走った (obaket の共有 checkout を fast-forward した後)

### 実装をわざと壊して検査が落ちるかを確かめたもの (mutate-verify。どれも狙った assert が最初に落ちた)

書き手 (obaket): 並列の要約の文言 (`parallel test cases` → `parallel cases`) / 失敗の経路の `complete log:` / 成功の経路の `complete log:` /
lock の文言 (`another checked run owns` → `another run owns`) / `** TEST FAILED **` を要約から落とす / 末尾の窓を 40 行 → 3 行・20 行 (`Failing tests:` が窓の外) /
residue が command の出力を飲み込む (要約と `complete log:` が mutate に届かない)。
読み手 (dotfiles の `devtool/bin/mutate`、`test_mutate_obaket_contract.sh` で): `par_summary` の文言・行頭への固定 (label に空白が無い前提) / `keep_complete_logs` の照合 / `lock_busy_line` の文言。

### 敵対的レビュー (red team、opus 2 周)

1 周目で採った指摘 (どれも再現を確かめてから直した):
- P2: `--summarize` の区間が label `summary` だけで、読み手が label の形 (空白の有無) に依存する変更が緑で通った → 本番と同じ `"make test-only"` にした
- P2: residue を lock を取れない経路でしか起こさず、本番の residue → run-logged-command の連鎖で residue が command の出力を飲み込む変更が緑で通った →
  検証コマンドを本番と同じ連鎖にした (residue は写しを一時 dir に置き、checker は何もしない stub。本物の checker は実ユーザーの保存先を走査するため)
- P3: lock 待ちの行が、検査が持たせた lock を指すことを見ていない → パスを照合する
- P3: `Failing tests:` の窓を 8 行の作った入力でしか試していない (窓を 20 行にしても緑) → 本物の xcodebuild の失敗 (306 行) で読めることを見る
- P3: 落ちたときに読み手の dotfiles の HEAD が分からない → 案内に出す

1 周目で採らなかった指摘: 「要約の行の failed F>0 を run=failed に読む」を gate で固定するケースが無い → 読み手の規則なので
`tests/devtool/test_mutate.sh` の「要約の行が複数あれば合計し、failed が 1 以上なら run=failed」が既に固定している。gate のコメントにそう書いた。

2 周目 (直した差分に対して): P1 / P2 なし。採った P3: 一時 dir が論理パスのままで、residue が lock のパスを実パスで出すと照合が偽の赤になる
(`cd -P` で物理パスにした) / HEAD の案内が要約の検査 10 か所に付いていない (EXIT の trap で出すようにした) / 継承した GIT_DIR で
案内が obaket の HEAD を出す (gate の git と dotfiles のテストで継承した変数を外した)。

### 残る制約 (直していない)

- gate の連鎖 (residue → run-logged-command、label、`--log`) は `macOS/Makefile` の test / test-only の recipe を手で写したもの。recipe の包む順を変えても追わない
  (gate のコメントに書いた。相対の `--log` を絶対パスにする変換は `run-logged-command-test` が見るが、これは self-test なので pre-push では走らない)
- 読み手の表示の文言 (`判定: RED`・`出力の置き場:` など) を dotfiles で変えると、各マシンの `~/dotfiles` を pull するまで obaket の pre-push が赤くなる時期がある
  (2 つの repo を同時には更新できない。落ちたときの案内に読み手の HEAD を出して、pull で直ることを見えるようにした)
- dotfiles の CI には obaket が無いので `test_mutate_obaket_contract.sh` は常に skip (exit 77)。dotfiles の側だけの変更を見るのは、obaket の checkout がある手元の `make test` だけ。
  手元でも obaket の共有 checkout が古い・他のセッションの WIP 中なら、その状態の書き手を相手にする
- gpgsign を有効にしたマシンでは、gate の一時 repo の commit が落ちうる (red team の推測。このマシンでは未設定で、確かめていない)

### 評価

統合の後、書き手と読み手の突き合わせは手で `make lint-gates` を打ったときしか走らず、②③ と ④ の一部は突き合わせ自体が無かった。
今は obaket の push のたび (pre-push) に ①〜④ と組み合わせの誤りを本物の書き手・読み手で突き合わせ、読み手を dotfiles で直したときも手元の `make test` で掛かる。
加えたもの: pre-push の所要が 4〜5 秒延びる / gate の約 300 行 / Makefile の recipe の手の写し (上の制約)。
