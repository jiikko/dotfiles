# 879 (bug): serialrun の hook が包む・包まないを誤る形

> **waiting**: B4 (重いコマンドの語彙を足す) と B7 (hook とシェルの PATH の食い違い) が観測待ち。869 の計測の日に、列の外で走る重い
> process と hook.log の skip 理由・rc=127 の有無を見る。それで足すべき語彙と食い違いの有無が分かる。他の 9 項目は commit「fix(serialrun): hook の判定の誤りを直す (issue 879)」で直した

起票日: 2026-10-23

## 概要

serialrun の監査で見つかった、グループ B の 11 項目。

- 監査: 2026-10-22〜23 に audit skill の 22 観点を 1 体ずつ直列で回した (sonnet・読み取りのみ。直接実行)。全数勘定と、直さないと決めたものの理由は [886](../done/886-research-serialrun-audit-2026-10-23.md)
- 設計の正本: [869](../869-feat-serial-run-machine-wide-heavy-command-queue.md)。下の `869:NNN` は 869 の行番号 (2026-10-23 時点)
- 各項目の「確認済み / 推測」は監査の報告の申告。直す前に実コードで裏を取る (`~/.claude/CLAUDE.md`「主張は証拠ではない」)
- 出典の「観点名 P2-1」は監査の報告の番号 (報告そのものは tmp に置いた使い捨てで残らない。要点は各項目に写した)

## 項目

### B1 hook の全体ガード `strings.Contains(cmd, "serial-run" / "PATH=" / "<<" / "((")` が部分文字列で効き、無関係な重い部分まで直列化から外れる (黙って素通し)
- 重要度: P2 (responsibility) / 確認済み (実バイナリの hook に流した): `GOPATH=/x go test ./...`、`PKG_CONFIG_PATH=/a make test`、`make test && git commit -m "serial-run を直す"`、`git diff issues/869-...serial-machine.md | head; make test`、`make test 2>&1 | grep PATH=` が全部書き換わらない。対照 `make test > out.log 2>&1` は書き換わる
- 根拠: hook.go `wrapCommand` の冒頭ガード (語や引用の外かを見ない)。判定の正本は `isHeavy` + `scanner` と謳うのに、拒否の一部が scanner を通らない。serial-run を触る作業ほど当たる。害は fail-open (壊れはしないが重い 2 本が同時に走る)
- 直し方の案: 拒否を scanner の中へ寄せる (代入語を読むときに `PATH` と完全一致か、コマンド位置の語が `serial-run` かを見る)。`<<` / `((` は scanner が読み切れないとき既に ok=false なので、残すなら理由を書く。テストに `GOPATH=` と引用内の serial-run の 2 件を足す。変異は B1 の枝を戻す形で red を確かめる
- 出典: responsibility P2-1 / broken-code P3-1

### B2 実際には重くない呼び出し (`--help`・dry-run・一覧表示) も順番待ちに入れる。実機の history.log に出ている
- 重要度: P2 / 確認済み (hook の実出力: `make -n test` / `make --dry-run test` / `make -q test` / `make -p test-lint` / `mutate-verify --help` / `make test --help` / `go test -h` / `swift test --list-tests` / `make help test` が全部書き換わる。実機 history.log の seq 14 `make -n test-unused-excluding-tests`・seq 19・20 `mutate-verify --help`)
- 根拠: hook.go `isHeavy` (make は target 語があれば true で `-n --dry-run -q -p --help` を見ない。mutate-verify 系は無条件 true)。情報表示の除外表 `xcodeInfoFlags` は xcodebuild だけにある。前に 11 分の mutate-verify がいれば同じ 11 分待つ (Bash の既定 timeout の前に切れる)
- 直し方の案: make は `-n/--dry-run/--just-print/--recon/-q/--question/-p/--print-data-base/--help/-h` を見たら false、mutate-verify 系・go・swift も `--help` / `-h` / `--list-tests` を false。除外表を xcodebuild のものと 1 本にまとめる。変異は「-n を見ない」で red を確かめる
- 出典: general P2-1

### B3 コマンドごとの引数文法の読みが浅く、包まれない形がある (安全側の取りこぼし)
- 重要度: P3 / `go -C` は確認済み (`go -C src/serialrun test ./...` は書き換わらず、`make -C` は書き換わる)。`env -C d make test` は報告が読んで確認 (encapsulation)。runtimeout のフラグが増えたとき (`-f` / `-k` 以外) のずれは推測 (polymorphism の trigger は「包まれずに走った重いコマンドが 3 種を超えたとき」)
- 根拠: hook.go `isHeavy` の `case "go": words[1]=="test"` / `unwrapPrefix` (env の `-C` の値を語として数える・nice・runtimeout の文法を写している)。害は直列化の抜けだけ (fail-open)
- 直し方の案: go のグローバルオプション `-C <dir>` を飛ばしてから `test` を見る (`makeValueOpts` と同じ形)。env の `-C` も値付きとして扱う。表にしても分岐の量は変わらないので設定表化はしない。runtimeout の usage との照合をテストに足す案もある
- 出典: general P3-2 / polymorphism U-1 / encapsulation U-2

### B4 「重いコマンド」の語彙が名指しの 5 系統で閉じ、`make build` (Swift の repo では xcodebuild を起こす)・`go build`・`go vet`・`golangci-lint`・`codex-fanout` などは対象外
- 重要度: P3 / 推測 (どの repo で `make build` が xcodebuild を呼ぶかは数えていない)
- 根拠: hook.go `isHeavy`。869 の動機の 3 件は make test と mutate-verify
- 直し方の案: まず観測する。計測の日 (869:332 の項目 5) に load average を取り、列の外で走る重い process (`ps` で xcodebuild / go / swift-frontend) があるか見る。語彙を足すのはその後
- 出典: general P3-3

### B5 fast path (`serial-run-wrap.sh` の `case`) が hook.go の語彙の二重持ちで、jq に依存し、粗くて偽陽性も出す
- 重要度: P2 (performance。ただし報告は「体感できる差ではない」と書く) / 確認済み (実測。軽いコマンドで net 約 16 ms、jq 部分 約 7 ms、偽陽性は 38 ms で本物の 38 ms と同じ。語彙の一致は読んで確認、jq が無いと黙って全部止まる経路はコードで確認)
- 根拠: `_claude/hooks/serial-run-wrap.sh` の `*make*|*go*test*|*xcodebuild*|*swift*|*mutate-verify*` (isHeavy に語を足しても fast path を足さないと silent に包まれない。テストは既存 5 語の例だけ) / `_claude/hooks/lib/hook_io.sh` の `hook_read_bash_command` (jq が無いと return 1 → `|| exit 0` で直列化が黙って全部止まる。macOS 15 は /usr/bin/jq を持つかもしれず未確認) / `*go*test*` は離れた語にも当たる
- 直し方の案 (食い違う 2 案): (a) fast path を外す (速さの根拠は hook の起動 1 回分で、bin/serial-run は 0.02〜0.04 秒。responsibility の案で、複雑性が一番下がる)。(b) 残すなら jq を外し `HOOK_INPUT=$(cat)` の全文に同じ `case` を当て、含むときだけ Go に渡す (performance・dependency の案。偽陰性は出ない。依存が 1 つ減る)。手がかり語を境界つきに狭める案は、偽陰性 = 直列化の抜けになるので報告も「現状のままでよい可能性が高い」と言う。どれを採るか決める
- 出典: responsibility P3-1 / performance P2-1・P2-2 / dependency P3-2

### B6 hook が「包めなかった」「包まなかった」「重くないので包まなかった」を外から区別できず、効いたかを後から確かめる手段が無い
- 重要度: P2 / 一部確認済み (hook の PATH 照合行を削る変異は red)。ビルド失敗が全 session で無音になる経路 (go が PATH に無い・ソース編集の途中) は未実機・コードから
- 根拠: `serial-run-wrap.sh` (`... || exit 0` が 6 箇所、最後の `"$bin" hook` の rc も捨てて `exit 0`。hook の stderr は Claude Code に出ない) / hook.go `runHook` (どの失敗も無出力で rc=0) / main.go `execDirect` (入れ子は札も history も出さない) / `runQueued` (待ち 1 秒未満は行を出さない)。シェルテストの `expect ""` 5 件は別の理由で空でも緑。869:332 の計測 (history.log を読む) が「記録が少ない = 並んでいない」と「包まれていない」を区別できない
- 直し方の案: `serial-run hook` が「差し込んだ / 見送った (理由)」を state 下の hook.log (history.log とは別) に追記し、`serial-run status` が最後の見送り・ビルド失敗を出す。シェル hook はビルド失敗 (`bin/serial-run` の rc≠0) のときだけ `additionalContext` か hook.log に残す。execDirect は history.log に `direct` 行を残す (D3 と同じ commit にしてよい)
- 出典: false-green P2-2

### B7 hook が PATH を照合するのは hook プロセスの PATH で、差し込んだコマンドを走らせるのは Bash tool のシェル。食い違うと rc=127 (fail-open が効かない側)
- 重要度: 未確認 / 推測 (3 本とも実測していない。`_zshrc` が `PATH="$HOME/dotfiles/bin:$PATH"` を持つので通常の起動では揃うはず)
- 根拠: `serial-run-wrap.sh` の `command -v serial-run` と、差し込む `serial-run -- `
- 直し方の案: 差し込む語を `bin/serial-run` の絶対パスにする案 (hook.go の `wrapPrefix`)。ただし許可ルールの照合 (I1) が変わるので、実測してから決める
- 出典: general U-2 / leaky-abstraction U-1 / broken-code U-1

### B8 hook は fail-open と言うが、差し込んだ serial-run 自体は fail-closed (状態の置き場を作れないと包んだコマンドが実行されない)
- 重要度: P2 / 確認済み (`SERIAL_RUN_STATE_DIR=<書けない所> serial-run -- echo RAN` → rc=125 で RAN は出ない。`XDG_STATE_HOME=/nonexistent/x` でも同じ)
- 根拠: main.go `run` (stateBase / openQueue の失敗 → rcTool=125) / `runQueued` (enqueue 失敗 → 125) と、hook.go の注記「直列化は便宜であって、止める理由にならない」の食い違い。発火: 書けない `~/.local/state`・ディスク満杯 (seq の WriteAt の ENOSPC)・Bash sandbox が write を許さない場合・EMFILE。LLM には「テストが 125 で落ちた」と見える
- 直し方の案: 「置き場・採番の失敗」(待ちの上限や flock の異常ではなく、キューを使えない種類) は stderr に警告して `execDirect` に落とす (排他を保証できない旨を出す)。または hook が差し込む前に置き場への書き込み可否を見る。「排他の保証 vs 便宜」の判断なので 869 に決めを書く
- 出典: design P2-1

### B9 パイプラインの両端が重いコマンドだと、`--max-wait` まで互いに待つ
- 重要度: P3 / 確認済み (`serial-run -- sh -c 'yes | head -c 300000' | serial-run -- wc -c` が相手の札を待って抜けるまで動かない)。頻度は低い
- 根拠: hook.go `wrapCommand` (パイプの各段を独立に包む)
- 直し方の案: パイプの 2 段目以降は差し込まない
- 出典: design P3-4

### B10 `scanner.inCmdPos` は `cmdAt < 0` と常に同値で、`if !s.inCmdPos { continue }` は到達不能 (死んだ分岐)
- 重要度: P3 / 確認済み (構成を読み、`go test -coverprofile` で該当ブロックの実行 0 回)
- 根拠: hook.go `scanner.scan` の default 節 / `scanner.endSimple` / フィールド `inCmdPos`。`(` の節の `!s.inCmdPos` は `cmdAt >= 0` を意味して到達可能なので、フィールドごと消さず `s.cmdAt >= 0` に置き換える
- 直し方の案: フィールドを消し、`(` の節は `s.cmdAt >= 0`、default 節の到達不能の分岐と 3 つの `s.inCmdPos &&` を削る。挙動は hook_test.go (wrapCommand の全形) が守る
- 出典: dead-code P3-1

### B11 make の「数値を省略できるオプション」の枝 (`makeOptionalNumOpts` / `numRe`) が全入力で等価で、それを守る表の 3 件が何も守っていない
- 重要度: P3 (反証レビューで P2 から下げた。実行時の害が無い死んだ枝) / 確認済み (枝の `i++` だけ消す・本体を空にする・枝ごと無くす、のどれでも `go test -count=1` が全緑で、全入力で等価)
- 根拠: hook.go `isHeavy` の `case makeOptionalNumOpts[a]:` と `numRe`。守っているつもりの hook_test.go の正の表 `make -j test` / `make -j 8 test`、負の表 `make -j 8 build`。`-j4 test` は `-` 始まりの語の素通しが守る
- 直し方の案: production の枝 + `makeOptionalNumOpts` + `numRe` を消し、表の `-j 8` 2 件と `-j 8 build` を消す。`make -j test` は `-j` を makeValueOpts に入れる退行を red にするので 1 件残す (その退行で red になるかは未実行 = 推測)。869 の「`make -j test` が包まれない → 直した」の記述と整合を取る
- 出典: test-cleanup P2-1

---

## 反証レビュー (2026-10-23、Opus 1 体・読み取りのみ。実験はしていない)

本文の重要度と案は下の訂正を反映済み。訂正の根拠を残す。

- **B1 の案を直す** (P2 の指摘): 「`<<` は scanner が読み切れないとき既に ok=false」は誤り。`scanner.scan` の `<` の節は `<` を 1 つ読んで `>&|` だけを飛ばし、2 つ目の `<` は
  空の語として通って、heredoc の本文の行を単純コマンドとして読み続ける。全体ガードを外すと `git commit -F - <<'EOF'` の本文の `make test` に差し込む。
  **`<<` のガードは残し、理由をコメントに書く**。PATH の照合を scanner へ寄せるなら、`export PATH=…` (代入語ではなく引数) と `PATH+=` も拾う
- **B9 の案を直す**: 「2 段目以降は差し込まない」だと `cat x | make test` (重いのが 2 段目) が外れる。**「1 つのパイプラインで包むのは最初の重い段 1 つだけ」**
- **B11 を P3 へ**: 等価な死んだ枝であることは正しいが、実行時の害は無い
- 反証できなかった: B2・B3・B4・B5・B6・B7・B8・B10

## 進捗

- 2026-10-23: B4・B7 以外の 9 項目に手を入れた (commit「fix(serialrun): hook の判定の誤りを直す (issue 879)」)
  - [x] B1 部分文字列の拒否をやめ、字句の中で見る: 手で書いた `serial-run -- …` はコマンド語で重いと読まない / PATH の見送りは前置きの代入 (`PATH=` / `PATH+=`) と
    `export` などの引数だけ (`GOPATH=`・`grep PATH=`・`make test PATH=/x`・`env PATH=` は包む)。`<<` と `((` は字句の前に打ち切る (heredoc の本文を単純コマンドと読むため。反証レビューの訂正どおり)
  - [x] B2 make の dry-run 等 (`-n` `--dry-run` `--just-print` `--recon` `-q` `-p` `-h` `--help` `-v` `-t`。短いオプションの束は左から読み、値を取る文字で止める) と、
    go / swift / mutate-verify の help (go は `-args` より前だけ) を待たせない
  - [x] B3 `go -C dir test` / `go -C=dir test` と env の `-C` を読む。runtimeout の usage との照合は足していない
  - [ ] B4 **見送り**: 語彙を足すのは観測の後 (869 の計測の日に、列の外で走る重い process を見る)
  - [x] B5 hook の入口から jq を外し、入力の全文に手がかりの語を当てる。語彙のずれは tests/claude/test_serial_run_wrap.sh が「対象の例が入口を通り抜ける」で見る
  - [x] B6 hook の判断 (差し込んだ / 重いのに見送った理由) を置き場の `hook.log` に残し、`serial-run status` が最後の 5 行を出す。入口のビルドの失敗は記録できていない (stderr は Claude Code に出ない)
  - [ ] B7 **見送り**: hook の PATH と Bash のシェルの PATH の食い違いは未実測
  - [x] B8 置き場を作れない・採番できない・run.lock を開けないときは、🚨 を出して順番を待たずに走らせる (status は 125)。`TestUnusableStateDirRunsUnqueued` /
    `TestUnopenableRunLockRunsUnqueued` / `TestUnreadableQueueRunsUnqueued`
  - [x] B9 1 つのパイプラインで包むのは最初の重い段 1 つだけ。手で書いた `serial-run --` の段があるパイプラインには差し込まない (位置はパイプラインの終わりで決める)
  - [x] B10 `inCmdPos` を消した (`cmdAt < 0` と同じ)
  - [x] B11 make の「数値を省略できるオプション」の枝を消した (等価だった)。`noglob` を包みのコマンドから外した (zsh の修飾子で exec できず rc=127。敵対的レビュー P2-4)。`$'…'` は見送る
- 変異 (全部 red): PATH の見送りを外す / `serial-run` の部分文字列の拒否を戻す / PATH の正規表現の `^` を外す / make の dry-run を見ない / 束を見ない / help を見ない / go -C を読まない /
  パイプの全段を包む / `||` をパイプと読む / hook.log を書かない / 待ち列を使えないと 125 / 入口に jq を要る / 入口の語から swift を落とす /
  手で書いた serial-run の段を見ない / PATH を全部の引数で見る / `$'` を読む / noglob を戻す / `-args` の後ろも help と読む / 束の値を読み飛ばさない
- 敵対的レビュー 1 周目 (Opus): P2 4 / P3 3 = 7 件。全部対応した (退行 3 件を含む):
  P2 手で書いた `serial-run --` の段がある別の段に差し込み互いの札を待つ (退行・再現済み) / P2 B1 の例 (`grep PATH=` 等) が今も見送られる (再現済み) / P2 `$'…\'…'` で引用の中を書き換える (前から・再現済み) /
  P2 `noglob make test` が rc=127 (前から・再現済み) / P3 重いコマンドの頭の deny / ask が効かなくなる (今の設定では 0 件) → README に書く / P3 `go test -args -h` を help と読む (退行) /
  P3 `make -Cscripts test` を dry-run と読む (退行)。`make -t` も待たせない側へ。壊せなかった観点: 差し込む位置のずれ・意味の書き換え (`FOO=1` / `time` / `!` / `{ }` / `( )` / `&` / リダイレクト / `$( )` / `[[ ]]` / `#` / 継続行)・fast path の偽陰性
- 敵対的レビュー 2 周目 (Opus): P1 1 / P3 2。P1 パイプの片側が `(…)` / `{…}` / 制御構文 / `|` の後の改行だと両方の段に付き、待ちの上限まで止まる (再現済み) →
  手書きの serial-run があれば全体を見送る・パイプと複合コマンドを見送る (3 周目で絞った) / P3 `builtin export PATH=` 等・`unset PATH`・`eval`・`path=` → PATH の書き換えとして見送る /
  P3 `make -kC dir build` → 束の最後の値を取る文字の次の語を値として飛ばす
- 敵対的レビュー 3 周目 (Opus): P1 1 / P2 2 / P3 1。P1 `make test | # c⏎make lint` で両方に付く → | の後のコメントも読み飛ばして改行を見る / P2 `exec serial-run`・`xargs serial-run`・
  置換の中の serial-run を手書きと読まない → パイプがあれば serial-run の文字列で見送る / P2 改行の分岐を守るテストが無い → 足した / P3 見送りが広く
  `for … do go test … | tail; done` を取りこぼす → 複合コマンドは境目が | と隣り合うときだけ見送る
- 敵対的レビュー 4 周目 (Opus): P1 1 / P2 1 / P3 1。P1 `(make test) 2>&1 | go test` で 2 本 (番号付きのリダイレクトの数字を語と読んで閉じた印を消す) → 数字の直後の < > をリダイレクトとして読む /
  P2 zsh の `| time (…)` で 2 本 → time / ! の後も | の直後のまま / P3 `(cd x && make test) 2>&1 | tail` は見送りに回る (受け入れる。hook.log の skipped:parse の件数で見る)。
  **周ごとに字句の別の隙間から同じ種類 (1 つのパイプラインに 2 本) が出たので、構造の歯止めを足した: パイプを含むコマンドでは差し込みを 1 か所まで**
  (2 か所なら全体を見送る。取りこぼすのは重いパイプラインが 2 つ並ぶ形だけ)。変異: 歯止めを外す / 番号付きのリダイレクトを語と読む → red。
  `time` / `!` の後の | の扱いを戻す変異は緑 (同じ形を歯止めが先に見送る。2 つの防御が重なる)
- 敵対的レビューは 4 周で打ち切った。4 周目の修正 (歯止めを含む) には 5 周目を当てていない
