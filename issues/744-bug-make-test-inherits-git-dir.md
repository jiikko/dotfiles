# 744 (bug): make 経由のテストが、継承した GIT_DIR などのまま本物の git を起こし、別の repo の設定とブランチを書き換える

> 🚨 **担当中: dotfiles-wt-shellgit**（2026-10-12〜）

起票日: 2026-10-12

## 概要

issue 740 は Go のテストだけを直した (9 パッケージの TestMain で外す)。`tests/` の shell のテストのうち、本物の git で repo を作るもの
22 本 (`git init` / `commit` / `clone` / `worktree` / `add` を grep。直接起こすものだけで、git を使うスクリプトを通す間接のものは数えていない) のうち、
GIT_DIR を外しているのは 4 本 (test_skill_eval / test_ci_log / test_pull_main_checkout / test_pre_push) だけで、18 本は外していない。

`-C <dir>` は GIT_DIR / GIT_WORK_TREE に負ける (`_claude/rules/sandbox-real-destructive-test-apis.md`)。hook から起動された
`make test` のように、これらを継承した環境で走ると、テストの git は一時 dir ではなく継承した repo を向く。

## 実測 (2026-10-12・master 0fc95a6f の後)

scratch の repo を作り、`GIT_DIR=<scratch>/.git GIT_WORK_TREE=<scratch> make test-dir DIR=<x>` を 4 つ回した
(tests/issues・tests/zshrc/test_git_prompt.sh・tests/claude/test_retro_open.sh・tests/bin/test_mutate_verify.sh)。

- 4 つとも rc=2 (継承なしの対照では test_issue_done は rc=0)
- scratch の repo の `.git/config` に `[user] name = t / email = t@t` が書き込まれた
- scratch の repo に `other` ブランチが作られ、`COMMIT_EDITMSG` が上書きされた (commit 数は 1 のまま)

hook から `make test` が起動されれば、`~/dotfiles/.git/config` の user が書き換わる形になる。

## 対応方針

- テストごとに `unset` を足すのではなく、**`make` の recipe の環境から 1 か所で外す** (Makefile の `unexport`)。
  shell・bats・`make test-src` の go test が全部 recipe から起動されるので、テストを足す人が覚えなくてよい
- 外す一覧は `git rev-parse --local-env-vars` を正本にし、Makefile の一覧が漏れていないかをテストで突き合わせる
  (`src/subproc/gitenv.go` の gitLocationEnv と同じ集合。GIT_CONFIG_KEY_<n> / VALUE_<n> は GIT_CONFIG_COUNT を外せば読まれない)
- 検査は make を本当に起動し、recipe の環境に 1 つも残っていないことを見る (Makefile の字面ではなく効果を見る)

### 検出しない形

- `bash tests/x.sh` の直接実行 (`tests/CLAUDE.md` で禁止済み)。既に unset している 4 本はそのまま残す (直接実行でも守られる)
- make の `$(shell ...)` (lint の対象の発見など)。読むだけで、書く経路ではない

## 進捗

- [ ] Makefile に unexport
- [ ] 突き合わせのテスト・変異
- [ ] 修正前後の E2E (上の 4 つで scratch の repo が変わらない)
- [ ] 敵対的レビュー
- [ ] make test
