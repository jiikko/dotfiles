# 744 (bug): make 経由のテストが、継承した GIT_DIR などのまま本物の git を起こし、別の repo の設定とブランチを書き換える

起票日: 2026-10-12

## 概要

issue 740 は Go のテストだけを直した (9 パッケージの TestMain で外す)。`tests/` の shell のテストで `git init` / `commit` / `clone` /
`worktree` / `add` の grep に当たったものは 22 本で、うち 3 本は文字列やコメントに当たっただけ (test_deny_bare_tmux_kill /
test_deny_piped_push_then_destroy / test_git_tests_unset_location_env)。repo を作るのは 19 本で、GIT_DIR を外しているのは 4 本
(test_skill_eval / test_ci_log / test_pull_main_checkout / test_pre_push) だけ、15 本は外していない。git を使うスクリプトを通す間接のものは数えていない。

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
- make の `$(shell ...)` (lint の対象の発見など)。読むだけで、書く経路ではない (3 つとも git を呼ばない。レビューで確認)
- make のコマンドライン / MAKEFLAGS で渡した `GIT_DIR=…`。環境の継承ではなく、`run_go_projects` の `$(MAKE) -C src/<x>` の sub-make へ届く
  (make 3.81 で再現)。行き先の go test は 740 の TestMain で外すので、外へ書く再現は無い

## 進捗

- [x] Makefile の先頭に `unexport` (--local-env-vars の 15 個 + GIT_NAMESPACE)。make 3.81 で recipe と sub-make から外れ、`$(shell)` には残ることを実測
- [x] 検査 `tests/scripts/test_make_unexports_git_location_env.sh`: 本物の Makefile を include した probe の recipe の env を、
  本物の git の --local-env-vars (+ GIT_NAMESPACE) と突き合わせる。対照の変数が届くことも見る (make 3.81 には `--eval` が無いので include の形)
- [x] 変異 (mutate-verify-list): unexport の行を消す / GIT_SHALLOW_FILE を消す / GIT_CONFIG_COUNT を消す → 3 本とも red。
  レビュー後に GIT_NAMESPACE を消す → red
- [x] E2E: 修正後に同じ 4 つを回すと 4 つとも rc=0、scratch の repo の .git の中身 (index 以外の全ファイルの shasum) は不変・ブランチは master のみ
- [x] 敵対的レビュー (opus 1 本・反証も兼ねる): P1・P2 無し。P3 3 件を全部採った
  - P3-1 make のコマンドライン / MAKEFLAGS の `GIT_DIR=…` は src/*/Makefile の sub-make へ届く (再現あり) → 「検出しない形」に書いた (行き先の go test は 740 で守られ、外へ書く再現は無い)
  - P3-2 GIT_NAMESPACE が検査で固定されていなかった → 検査の集合に足し、変異で red を確認
  - P3-3 本文の 22 本に文字列・コメントだけのものが 3 本 → 19 本 / 15 本に直した
  - 壊せなかった攻め口: unexport が効かない make の形 (環境・`-e`・MAKEFLAGS の中・再帰)・`$(shell)` からの書き込み・GIT_CONFIG_KEY_<n>・
    GIT_CONFIG_GLOBAL / HOME 系 (tests/ に `config --global` で書く箇所 0 件)・make の外からの起動 (pre-push は自分で unset)・検査の素通り・回帰
  - 2 周目は回さない: 直しは一覧への 1 個の追加とコメントだけで判定ロジックを新設せず、変異で直接確かめた (adversarial-review-own-safeguards §7 の例外)
- [x] make test rc=0 (新しい検査も並列腕で [ok])
