# 855 (retro): CI の issues/ だけの push の削減と pre-push の並列化

起票日: 2026-10-21

## 概要

「pre-push の gate を効率的に、冗長なものを消す・統合する」の依頼で、次の 3 commit を入れた。

- 04577f69: issues/ だけの push では Tests / Lint を走らせず、新設の Issues (issues.yml) で issue の整合だけを見る
- c6ea7b2b: pre-push の 6 本の検査を並列に回す (中断時はプロセスグループごと止める)
- a17a0dbd: 04577f69 で入った 120 字超の 2 行を折る (master の Lint を赤にしていた)

pre-push の 6 本は責務が別で、統合しても走査は減らない (表は下)。冗長だったのは CI の方で、
2026-10-07〜21 の master の 1024 commit 中 497 件が issues/ だけの変更なのに、毎回 Tests (6〜10 分) と Lint (1.5〜3 分) が走っていた。

| 検査 | 見るもの | 所要 (2026-10-21) |
|---|---|---|
| test_issue_numbers_unique | 番号の重複 | 0.03 秒 |
| test_issue_links_valid | issues/ の中の相対リンク | 1.5 秒 (うち自己検査 0.6) |
| test_next_links_valid | next/ の symlink の形 | 0.13 秒 |
| test_next_claims_have_banner | claim と担当バナーの対応 | 0.26 秒 |
| test_human_issues_have_deadline | human の `期限:` | 0.11 秒 |
| test_issue_path_refs_not_stale | issues/ の外からの裸パス | 1.3 秒 |

## どこで踏んだか

### 1. 手元の lint が、道具が無いので弱い検査に落ちていた。赤は他のセッションの commit で初めて出た

- 手元に yamllint と actionlint が無かった。`make test-yaml` は ruby の構文検査に落ち、`test-actionlint` は skip を出して緑で返った
- そのため line-length (上限 120) を見ないまま push し、issues.yml:44 (138 字) と lint.yml:8 (143 字) で master の Lint を赤にした
- 自分の 2 commit の Lint の run は、後ろの push に cancel されて結果が出なかった
- 赤は別のセッションの commit (60b23772) の run で初めて出た。そのセッションが知らせてくれて気づいた
- skip の行はログに出ていた (`[actionlint] actionlint not found; skipping` / `yamllint not found; falling back to ruby -ryaml syntax check`)。「lint 緑」を証拠に数えたとき、その行を読んでいなかった

### 2. 中断時の後始末を観測しようとして、観測そのものが 3 回成立していなかった

レビューの「中断すると検査の子孫が残る」を再現しようとして、次の 3 回は結果ではなく観測の失敗だった。

- `ps -A -o command` はコマンド行を切り詰める。照合が当たらず「残り 0」に見えた (`-ww` が要る)
- hook の mktemp は、渡した TMPDIR ではなく `/var/folders` に作った。パスでの照合が外れた
- 非対話のシェルが `&` で起こした hook は SIGINT を無視した状態で始まる。そのため INT を送っても hook は最後まで走った (rc=0)。それを「SIGINT でも残りは 0」と読みかけた
  - Python から SIGINT を既定の扱いに戻して起こし直し、初めて有効な観測になった

どの回も「当たったか・届いたか」を見る前に「残り 0」という結果を読んでいた。

### 3. コメントに書いた不在の主張が 2 つとも誤りだった

- 書いた主張 1: tests.yml に「docs/ を読むテストがある」(docs/ を paths-ignore に入れない理由)。書いた直後に grep し、該当が無いと分かって直した
- 書いた主張 2: lint.yml に「check_*.sh も issues/ を読まない」「issues/ は md だけ」。敵対レビューが反証した
  - check_trigger_log_writers.sh は git ls-files の .md 以外を見ており、issues/audit-log を対象にしていた
  - discover_yaml_files.sh と test_enumerations_are_derived.sh ③ も issues/ を外していなかった
- どちらも「外してよい」理由として書いたコメントで、外した網が何を見ていたかの母集合を数える前に書いていた

### 4. 並列化で入った退行は、自分の変異では想定していない形だった

- bash 3.2 では subshell が exec を省く。そのため pid だけを kill すると、検査の本体が残る (TERM の直後に 3 本)
- 自分が当てた変異は「rc を見ない」「添字がずれる」だけで、中断の経路は想定に無かった
- 敵対レビューが出し、実測して直した (プロセスグループごと止める形に変え、ケース 14 で固定)

## 次に効きそうな改善

1. **検査が道具の不在で弱い経路に落ちた・skip したら、その検査が見る種類のファイルを変えた変更では、緑を証拠に数えない**
   - 道具を入れて回すか、「その検査は未実施」と報告に書く
   - 今回は workflow を変えたのに、yamllint と actionlint が無いまま `make test-lint` の緑で進めた
   - 別の repo・別の言語の lint (formatter の fallback・optional な linter) でも同じ形で起きる
   - 切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の「確認の仕方」の表へ 1 行追記
2. **シグナル・プロセスを観測するときは、結果を読む前に、観測が対象に当たったことを確かめる**
   - 確かめること: 照合が対象を拾ったか、シグナルが無視されていない相手に届いたか、測る間に対象がまだ生きていたか
   - 「残り 0」「止まった」は、観測が空振りしても同じ値になる
   - 切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の「有無で結果が変わらない観測を証拠に数えない」節へ追記
     (例として、非対話シェルの `&` は SIGINT を無視した子を作る / macOS の `ps` は `-ww` が無いと切り詰める)
3. **「X は Y を読まない / 含まない」を、検査や網を外す理由としてコメントに書く前に、Y の母集合を 2 通りの方法で数える**
   - 外す網が見ていた集合を、方法を変えて列挙し (発見式の展開・git ls-files・grep)、一致を確かめてから書く
   - 理由のコメントは、次に網を戻すかどうかの判断材料になる。誤りが残ると、戻す判断を逆に導く
   - 切り出し先: `_claude/rules/list-masked-failure-modes-before-removing-guard.md` の手順 1 へ追記
     (「呼ばれ方を grep」に「外す網の母集合を 2 通りで数え、理由のコメントはその結果だけで書く」を足す)

局所的なもの (切り出し先を提案しない):

- `make test-yaml` / `test-actionlint` が道具の不在で弱い経路に落ちても緑を返す。直していない。改善 1 を rule に入れれば足りるか、
  Makefile 側で fallback を目立たせる (末尾に「未実施の検査」の一覧を出す) かは、ユーザーの判断を待つ
- pre-push の並列化は、fork から `pids+=` までの数命令の間にシグナルが来ると検査が 1 本残る窓を受けた (c6ea7b2b の commit message)

却下:

- 却下: cancel された run を自分の commit の検証に数えかけた件。既存の rule (verify-execution-not-just-exit-code.md の「起動しなかった / 取り消された run」) で拾えたので、追記は要らない
- 却下: 並列化の退行を敵対レビューが出した件。既存の rule (adversarial-review-own-safeguards.md / mutation-verify-new-tests.md の「変異は自分が想定した不変条件しか試さない」) どおりに動いた

## 進捗

- [ ] 改善 1〜3 の切り出し (ユーザーの判断待ち)
- [ ] 局所の `make test-yaml` / `test-actionlint` の扱い (ユーザーの判断待ち)
