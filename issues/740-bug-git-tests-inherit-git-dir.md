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
  glogx 6・glogx/issues 1・pro-con 6・subproc 2・treefiler/filer 1)。外しているのは glogx の pull_rebase_real_test・issues の filelink_test・pro-con の wtclean_test の 3 つだけ
- 今の pre-commit / pre-push hook は go test を回さないので、実害はまだ出ていない。発火条件は「GIT_DIR を継承した環境 (hook・`git rebase -x`・`git bisect run` など) からテストを走らせる」

## 対応方針

- 外す変数の一覧の正本を subproc に置く (今は pro-con の gitx が production 用に `scrubbed` を持っている。同じ一覧を共有する)
- 本物の git を起こすテストのパッケージは TestMain で一度だけ外す (ファイルごと・テストごとに書かない)
- 外し忘れたパッケージを検査で止める (本物の git を起こす `_test.go` があるパッケージに、外す呼び出しがあるか)

## 関連

- issue 739 (出どころ)・692・`src/pro-con/gitx/gitx.go` の `Env`

## 進捗

- 2026-10-12: 起票 (dotfiles-4e)。反証レビューは修正の敵対的レビューと兼ねる
