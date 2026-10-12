# 740 (bug): 本物の git を起こすテストが GIT_DIR / GIT_WORK_TREE を外さず、継承した repo に書き込みうる

> 🚨 **担当中: dotfiles-4e**（2026-10-12〜）

起票日: 2026-10-12

## 概要

src/ の Go のテストのうち、一時ディレクトリに repo を作って本物の git を走らせるものの多くが、repo の場所を決める環境変数
(GIT_DIR / GIT_WORK_TREE など) を外していない。git hook の中のように GIT_DIR を継承した環境で go test を走らせると、
`git -C <一時dir> init / add / commit` が一時 dir ではなく**継承した repo**を再 init し、そこへ commit する
(`~/.claude/rules/sandbox-real-destructive-test-apis.md` の冒頭の罠)。issue 739 の敵対的レビューで指摘された (subproc の 2 ファイル)。

## 詳細 (再現。git 2.55.0、2026-10-12)

```sh
# victim は普通の repo。tmprepo は空の一時ディレクトリ
GIT_DIR=$S/victim/.git git -C $S/tmprepo init -q                     # rc=0。tmprepo には何もできない (victim を再 init した)
GIT_DIR=$S/victim/.git GIT_WORK_TREE=$S/tmprepo git -C $S/tmprepo add x && ... commit -qm injected
git -C $S/victim log --oneline                                          # injected が victim に入っている
```

- 該当: `_test.go` で `exec.Command("git", ...)` を起こし、GIT_DIR に触れていないファイルが 16 (2026-10-12 に grep で数えた。
  glogx 5・glogx/issues 1・pro-con 7 (dispatcher 3・gitx 1・monitor 2・worktreecmd 1)・subproc 2・treefiler/filer 1。
  glogx の waitdelay_discipline_test.go も当たるが、文字列リテラルの中で git は起こさない。内訳は敵対的レビューで数え直した)。外しているのは glogx の pull_rebase_real_test・issues の filelink_test・pro-con の wtclean_test の 3 つだけ
- 今の pre-commit / pre-push hook は go test を回さないので、実害はまだ出ていない。発火条件は「GIT_DIR を継承した環境 (hook・`git rebase -x`・`git bisect run` など) からテストを走らせる」

## 対応方針

- 外す変数の一覧の正本を subproc に置く (今は pro-con の gitx が production 用に `scrubbed` を持っている。同じ一覧を共有する)
- 本物の git を起こすテストのパッケージは TestMain で一度だけ外す (ファイルごと・テストごとに書かない)
- 外し忘れたパッケージを検査で止める (本物の git を起こす `_test.go` があるパッケージに、外す呼び出しがあるか)

## 関連

- issue 739 (出どころ)・692・`src/pro-con/gitx/gitx.go` の `Env`

## 進捗

- 2026-10-12: 起票 (dotfiles-4e)。反証レビューは修正の敵対的レビューと兼ねる
- 2026-10-12 (dotfiles-4e): 対応 (commit「fix(test): 本物の git を起こすテストのパッケージで、継承した GIT_DIR などを TestMain で外す」)
  - 一覧の正本を `src/subproc/gitenv.go` (`IsGitLocationEnv` / `UnsetGitLocationEnv`) に置き、pro-con gitx の production 用の `scrubbed` をそこへ寄せた
  - 本物の git を起こすテストのある 9 パッケージ (glogx・glogx/issues・pro-con・pro-con/dispatcher・gitx・monitor・wtclean・subproc・treefiler/filer) の TestMain で m.Run の前に外す
  - `tests/scripts/test_git_tests_unset_location_env.sh` が外し忘れを止める (9 パッケージを見る。monitor から呼び出しを消す変異で rc=1)
  - 実測: GIT_DIR / GIT_WORK_TREE を scratch の victim repo に向けて 4 module の go test を走らせた。修正前 (master) は全部 rc=1 で victim に 112 行の commit が入り、修正後は全部 rc=0 で victim 無変更
- 2026-10-12 (dotfiles-4e) 敵対的レビュー (opus。issue の反証を兼ねる): P1 なし
  - P3-1 (採用): 一覧が `git rev-parse --local-env-vars` (2.55.0) の部分集合だった (GIT_IMPLICIT_WORK_TREE・GIT_GRAFT_FILE・GIT_NO_REPLACE_OBJECTS・GIT_REPLACE_REF_BASE・GIT_SHALLOW_FILE。
    読み取りだけに効き、GIT_GRAFT_FILE を victim に向けると glogx のテストが偽赤になった)。5 つを足し、本物の `--local-env-vars` を正解役にする `TestGitLocationEnvCoversLocalEnvVars` を置いた
    (5 つを消す変異で 5 件 red)。pro-con の gitx.Env も同じ一覧を外すようになる (production で外す変数が 5 つ増える)
  - P3-2 (採用): 本文の内訳の誤り → 上で直した
  - P3-3 (記録): 検査の正規表現は文字列リテラルにも当たる。git を起こさないパッケージに外す呼び出しを求める向き (安全側) なので直さず、検査のヘッダに書いた
  - 壊せなかったもの: 9 パッケージを GIT_DIR / GIT_WORK_TREE / GIT_INDEX_FILE / GIT_SHALLOW_FILE / GIT_REPLACE_REF_BASE を victim に向けて走らせても victim は無変更。
    git を起こすパッケージを import する他のテストのパッケージ (pro-con/ui・backend・live・treefiler) は git の呼び出しが 0 回。gitx.Env の挙動は移設の前後で同じ。
    hook の中で継承される場所の変数は pre-commit の GIT_INDEX_FILE・GIT_PREFIX で、一覧で覆われる
  - 2 周目は回していない。修正は一覧に 5 つの名前を足したことと、本物の git を正解役にしたテスト・文書だけで、判定のロジックは変えていない (adversarial-review-own-safeguards §7 の例外)
