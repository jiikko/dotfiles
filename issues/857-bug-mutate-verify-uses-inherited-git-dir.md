# 857 (bug): mutate-verify が継承した GIT_DIR の repo を検証する

起票日: 2026-10-22

## 概要

`bin/mutate-verify` は、起動時に継承した `GIT_DIR` / `GIT_WORK_TREE` などを外さない。cwd の repo (A) と別の repo (B) を
`GIT_DIR` が指していると、**B の HEAD から使い捨て worktree を作り、A の作業ツリーとの差を「未コミットの変更」として持ち込んだ
混ざった木で判定し、何も言わずに rc を返す**。全体のログだけは A の `tmp/` に置かれる (`bin/skill-tmpdir` は `GIT_DIR` を外すので)。

## 詳細 (再現手順と観測)

2026-10-22 に手元 (macOS) で再現した。A と B は別内容の commit を持つ素の repo:

```sh
# A: g.sh は "bad" を拒否する。B: g.sh は `echo B-version` だけ (B の HEAD は A と別の commit)
cd A && GIT_DIR=<B>/.git mutate-verify --file g.sh --syntax 'bash -n g.sh' \
  --verify 'out=$(bash g.sh bad); echo "got=$out"; [ "$out" = rejected ] || { echo FAIL; exit 1; }; echo PASS' \
  --baseline-expect PASS --apply 'perl -pi -e "s/= bad/= never/" "$MUTATE_FILE"' --expect FAIL
```

観測 (stderr):

- `Preparing worktree (detached HEAD 6558393)` — 6558393 は **B** の HEAD (A の HEAD は 2fc7fc6)
- `未コミットの変更を持ち込んだ (1 ファイル)` — A では何も変更していない。A の `g.sh` を B の HEAD に対する差分と読んでいる
- 「当てた変異」の diff の前側が `echo B-version` (B の中身) で、後ろ側は A の内容に変異を当てたもの
- rc=0 (「想定どおり red」)。判定に使ったのは A でも B でもない木
- 全体のログ: `A/tmp/mutate-verify/<session>/mutate-verify.g.sh.*` (置き場だけは A)
- 終わった後、A と B の `git status --short` は空、`git -C B worktree list` に残骸は無かった。B の `.git` の中
  (reflog・objects など) が書き換わったかは見ていない (**未確認**)

コード上の原因 (2026-10-22 の master、commit 3a8bb415):

- `bin/mutate-verify` も `bin/mutate-verify-list` も `scripts/lib/worktree_scratch.sh` も、`GIT_DIR` などを unset しない
  (`grep -n 'GIT_DIR\|unset GIT' bin/mutate-verify bin/mutate-verify-list scripts/lib/worktree_scratch.sh` が 0 件)
- `GIT_DIR` があって `GIT_WORK_TREE` が無いと、git は cwd を作業ツリーの根と見なす。`root="$(git rev-parse --show-toplevel)"`
  は A を返し、`wts_create HEAD` (worktree の作成) と未コミット差分の取得は B の object database を見る
- 同じ repo の中で、`GIT_DIR` を外している側: `Makefile` 先頭の `unexport GIT_DIR GIT_WORK_TREE ...` (issue 744)、
  `bin/skill-tmpdir` の `unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR`。規範は `~/.claude/rules/sandbox-real-destructive-test-apis.md`
  の「`-C <dir>` を渡したことを隔離と読まない」

## 起きる経路 (未確認)

- **通常の checkout の hook は `GIT_DIR` を付けない** (2026-10-22・git 2.55.0 で実測。pre-commit の中で `env` を取ると、
  repo の根からでもサブディレクトリからでも `GIT_INDEX_FILE=.git/index` だけが出た。反証レビューも pre-commit / post-checkout /
  `git rebase -x` で同じ結果)。issue 744 と `src/subproc/gitenv.go` のコメントは「hook から設定される」と書くが、744 の実測は
  `GIT_DIR` を明示して渡したもので、hook が付ける条件はどこでも確かめられていない
- 残る経路: `GIT_DIR` を明示して渡す呼び出し (スクリプト・`git --git-dir` を使う道具)、bare repo や linked worktree の hook (未検証)
- Claude Code の Bash ツールの環境には普段 `GIT_DIR` は無い。**実害が出た記録は無い**。起票の理由は、起きたときに誤った rc を
  黙って返す (検出手段が無い) ため。経路が確かめられるまでは優先度は低い
- cwd が A のサブディレクトリなら、`--show-toplevel` は A の根ではなく cwd を返す (反証レビューで実測)。その場合は `--file` の
  存在確認 (`[ -f "$root/$file" ]`) で先に止まることがあり、誤った rc を返す経路と止まる経路の両方がある

## 対応方針

- mutate-verify と mutate-verify-list の起動直後に、repo の場所を決める環境変数を unset する。
  cwd の repo を検証するのが既定の約束 (`git rev-parse --show-toplevel` を root にしている) なので、継承した値を尊重する
  理由は無い。一覧は写さず git 自身に出させる (`unset $(git rev-parse --local-env-vars)`)。`Makefile` の `unexport` は
  この集合と同じで、`src/subproc/gitenv.go` の `gitLocationEnv` にも写しがある。手で写すと乖離するため
  (`Makefile` 側は `tests/scripts/test_make_unexports_git_location_env.sh` が本物の git と突き合わせて止めている)。
  反証レビューの実測: `GIT_DIR` を継承した状態でも `--local-env-vars` の出力は継承なしと同じ 15 個で、/bin/bash 3.2 で unset した後は
  `--show-toplevel` が A を返した。`GIT_CONFIG_KEY_<n>` / `VALUE_<n>` はこの一覧に入らないが、`GIT_CONFIG_COUNT` (一覧に入る) を
  外せば読まれない
- 外す前に、`GIT_DIR` を明示して別の repo を検証したい使い方が無いかを確かめる (無ければ外すだけでよい。あるなら
  `--repo` のような明示の引数にする)
- テスト: 上の再現手順を `tests/bin/test_mutate_verify.sh` のケースにする (`GIT_DIR` が別の repo を指していても、
  worktree が cwd の HEAD から作られ、未コミットの変更を持ち込まない)。直した後、unset を外す変異で red を確かめる

## 関連ファイル

- `bin/mutate-verify` (`root=` の行、`wts_create HEAD`、未コミット差分の持ち込み)
- `bin/mutate-verify-list`
- `scripts/lib/worktree_scratch.sh`
- `bin/skill-tmpdir` (`GIT_DIR` を外している側の実例)
- `Makefile` 先頭の `unexport` (issue 744)

## 進捗

- [ ] 継承した `GIT_DIR` などを mutate-verify / mutate-verify-list で外す
- [ ] 再現手順をテストのケースにし、変異で red を確かめる
- [ ] B の `.git` の中が書き換わるか (reflog・objects) を確かめる (未確認)
- [ ] hook が `GIT_DIR` を付ける条件 (bare / linked worktree など) を確かめる。issue 744 と `src/subproc/gitenv.go` のコメントの記述の裏取りも兼ねる
