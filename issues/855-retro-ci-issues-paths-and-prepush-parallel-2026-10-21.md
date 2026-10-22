# 855 (retro): CI の issues/ だけの push の削減と pre-push の並列化

起票日: 2026-10-21

## 概要

「pre-push の gate を効率的に、冗長なものを消す・統合する」の依頼で、次の 3 commit を入れた。

- 04577f69: issues/ だけの push では Tests / Lint を走らせず、新設の Issues (issues.yml) で issue の整合だけを見る
- c6ea7b2b: pre-push の 6 本の検査を並列に回す (中断時はプロセスグループごと止める)
- a17a0dbd: 04577f69 で入った 120 字超の 2 行を折る (master の Lint を赤にしていた)

pre-push の 6 本は責務が別で、統合しても走査は減らない (表は下)。冗長だったのは CI の方で、
master の commit のおよそ半分 (49%) が issues/ だけの変更なのに、毎回 Tests (6〜10 分) と Lint (1.5〜3 分) が走っていた。
件数は 372715b1 (作業開始時の master) から数えて 1014 件中 492 件。作業中に数えた 1024 / 497 (workflow のコメントの値) とずれるのは、
`--since` の起点が実行時刻で動くため。数え方:

```sh
for c in $(git log --since=2026-10-07 --format=%H --first-parent 372715b1); do
  git diff-tree --no-commit-id --name-only -r "$c" | grep -qv '^issues/' || echo "$c"
done | wc -l
```

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

1. **「lint 緑」を証拠に数える前に、ログに skip / fallback の行が出ていないかを読む**
   - 出ていたら、その検査が見る種類のファイルを変えた変更では、道具を入れて回すか「その検査は未実施」と書く
   - 既存の rule でほぼ足りている。`verify-execution-not-just-exit-code.md` の表の「新規テスト」の行は「skip を実行の証拠に数えない」と書き、
     冒頭の「手元の検査が CI より狭い範囲しか回さないと知っている変更では、push の後に CI の結果を待つ」が直接の是正にあたる。
     今回は手元の lint が狭いと「知っていた」状態に無かった。足りなかったのは、それに気づくための 1 点 (ログの skip / fallback の行を読む) だけ
   - 切り出し先: `verify-execution-not-just-exit-code.md` の上の冒頭の項へ「skip / fallback の行が出ていたら、狭いと知っている状態に数える」を併合する (新しい項は立てない)
2. **シグナル・プロセスを観測するときの空振りの例を足す**
   - 主旨 (観測が空振りしても同じ値になる。必ず在るもので先に較正する) は、`verify-execution-not-just-exit-code.md` の
     「抽出・判定を書いたら、同じ経路を通る canary を先に置く」節と「有無で結果が変わらない観測を証拠に数えない」節に既にある
   - 新しいのは軸 1 つ (シグナルが届く相手か) と具体例だけ: 非対話シェルの `&` は SIGINT を無視した子を作る / macOS の `ps` は `-ww` が無いと
     コマンド行を切り詰める / 子の mktemp が渡した TMPDIR を使わないことがある
   - 切り出し先: 「有無で結果が変わらない観測を証拠に数えない」節に例として 1 行追記
3. **網を外す理由のコメントに書く「X は Y を読まない」も、`mutation-verify-new-tests.md` の「検査の不在を主張するときの裏取り」の対象に数える**
   - 方法を変えた列挙を最低 2 通り、という作法はその節に既にある。足りなかったのは発動点 (外す理由のコメントを書く瞬間) だけ
   - 切り出し先: その節の冒頭の発動の文に「網を外す理由のコメントを書くとき」を 1 語足す (`list-masked-failure-modes-before-removing-guard.md` の手順 1 は
     「呼ばれ方を grep」で母集合の数え方の置き場ではないので、そちらには足さない)

局所的なもの (切り出し先を提案しない):

- `make test-yaml` (Makefile の test-yaml。yamllint が無いと ruby の構文検査に落ちる) と `test-actionlint` (無いと skip を出して rc=0) は、
  道具の不在でも緑を返す。直していない。`scripts/check_skip_exit_code.sh` (skip を告げて exit 0 を落とす) は tests 用で、Makefile の lint の
  target には効かない。改善 1 を rule に入れれば足りるか、Makefile 側で fallback を目立たせる (`make test-lint` の末尾に「未実施の検査」の一覧を出す)
  かは、ユーザーの判断を待つ
- pre-push の並列化は、fork から `pids+=` までの数命令の間にシグナルが来ると検査が 1 本残る窓を受けた (c6ea7b2b の commit message)

却下:

- 却下: cancel された run を自分の commit の検証に数えかけた件。既存の rule (verify-execution-not-just-exit-code.md の「起動しなかった / 取り消された run」) で拾えたので、追記は要らない
  (改善 1 とは別の失敗: こちらは取り消しに気づいて後ろの run で確かめられた。改善 1 は、後ろの run を待つ前に「手元で緑」を根拠にした方)
- 却下: 並列化の退行を敵対レビューが出した件。既存の rule どおりに動いた (`mutation-verify-new-tests.md` の「関連」の「変異は自分が想定した
  不変条件しか試さない」と、`adversarial-review-own-safeguards.md` の敵対的レビューを最終ゲートにする規律)

## 進捗

- [x] 改善 1〜3 の切り出し (2026-10-22 ユーザー指示で実施。commit「rules: retro 855 の改善 1〜3 を既存の rule へ併合」)
  - 1: `verify-execution-not-just-exit-code.md` の「手元の検査が CI より狭い」項の子項に、skip / fallback の行を「狭いと知っている」に数える旨
  - 2: 同ファイルの「有無で結果が変わらない観測」節に、プロセス・シグナルの観測の 3 例
  - 3: `mutation-verify-new-tests.md` の「検査の不在を主張するときの裏取り」節の冒頭に、網を外す理由のコメントを発動点として
- [ ] 局所の `make test-yaml` / `test-actionlint` の扱い (ユーザーの判断待ち)
