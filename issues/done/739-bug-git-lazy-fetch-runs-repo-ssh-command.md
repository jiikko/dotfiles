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

- 732 の 2 周目のレビューの仮説 (未実測): `remote.<x>.url` をローカルのパスにして `remote.<x>.uploadpack=<cmd>` を置く形でも、ssh 無しで走るはず
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
- 2026-10-12 (dotfiles-4e): 対応方針の「env を付けられない呼び出し」問題は、git 2.44 からの global option `--no-lazy-fetch` (GIT_NO_LAZY_FETCH=1 と同じ) で回避した。
  引数なので `gitHarden` に足すだけで GitArgs / GitCommand / GitRepoArgs / GitRepoCommand の全経路に効く (commit「fix(subproc): git に --no-lazy-fetch を付け…」)
  - 実測 (git 2.55.0): sshCommand と uploadpack (url をローカルのパスにした形。732 のレビューの未実測の仮説) の 2 経路 × status / `diff HEAD` の 4 通りで、
    オプション無しは全部走り、付けると全部走らない (`warning: lazy fetching disabled` で rc=128)。Apple git 2.54.0 も受け付ける
  - テスト: `src/subproc/lazyfetch_test.go` (対照として素の git status で罠が走るのを見てから、GitCommand の status / diff HEAD と GitRepoCommand の status を見る)。
    変異「gitHarden から --no-lazy-fetch を消す」で sshCommand / uploadpack の両方が red (bin/mutate-verify rc=0)
  - 割り切り: 本物の partial clone では、手元に無い blob を読む表示 (古い commit の diff など) が取りに行かずにエラーになる。`~/src` の 2 階層までに partial clone は 0 件だった
  - make test (root): rebase 後 rc=0。rebase 前の 1 回目は `test_git_calls_hardened.sh` (master で 5e2a9940 が直していた。worktree の base が古かった) と
    restartable の `TestGroupStopWaitsForDescendantAfterLeaderExit` (git を使わない。単独 3/3 で通る) が落ちた
- 2026-10-12 (dotfiles-4e) 敵対的レビュー 1 周目 (opus):
  - P1 (採用・再現した): 本物の blob:none clone で glogx の pull --rebase と pro-con の merge-tree が rc=128 になる (gitHarden は書き込みの操作にも付くため)
    → 既定は付けたままにし、遅延 fetch が要る操作だけ外す口 `subproc.GitArgsLazyFetch` / `GitCommandLazyFetch` と pro-con の `gitx.RunLazyFetch` を足した
    (commit「fix(subproc): pull / push と pro-con の merge-tree は遅延 fetch を許す…」)。付け忘れたときに partial clone でエラーが見える側 (安全側) に倒すため、レビューの案 (表示だけに付ける) とは逆の既定にした
  - 使い先: glogx の noPromptGitCmd (push / pull と、その前の status。どれも確認の後のキー操作)・pro-con monitor の MergeTree (自分で登録した repo)。
    rebase --abort はフラグ付きのままでも partial clone で通った (レビューの実測) ので外していない
  - 壊せなかったもの: 罠を仕込んだ repo に、status / diff / log -p / show / blame / cat-file / fsck / gc --auto / merge-tree / grep / archive などの 20 コマンドを打っても罠は走らなかった。submodule の子にも引き継がれた。設定で lazy fetch を再び有効にする鍵は無い。Apple git 2.54 も対応している
  - テスト: `TestPullRebaseWorksInPartialClone` (glogx)・`TestMergeTreeInPartialClone` (pro-con/monitor)・`TestGitArgsLazyFetchDropsOnlyNoLazyFetch` (subproc)。
    変異 6 本 (LazyFetch が --no-lazy-fetch を残す / 全部落とす・noPromptGitCmd を GitCommand に戻す・MergeTree を gitx.Run に戻す・gitx の分岐を殺す・gitHarden から外す) がすべて想定どおり red (mutate-verify-list)
- 2026-10-12 (dotfiles-4e) 敵対的レビュー 2 周目 (opus): P1 なし
  - P3-2 (採用): 遅延 fetch を許す口の使い先を固定する検査が無かった (gitx.Run を lazy に替えても pro-con のテストが全部通った)
    → `TestRunDoesNotLazyFetch` (pro-con/gitx。RunLazyFetch を対照に置く) と、`tests/scripts/test_git_calls_hardened.sh` の許可リスト (ファイル単位) を足した。
    変異「gitx.Run を run(…, true) に」→ TestRunDoesNotLazyFetch が red。変異「glogx の runGitTimeout を GitArgsLazyFetch に」→ hardened の検査が rc=1
  - P2 (割り切りとして記録): blob:none clone では、glogx の --stat / -p の一覧が全体として出ず (rc=128)、d の diff も出ない。d もキー操作だが、開いた他人の repo の表示なので
    textconv と同じく走らせない側に置く (subproc.go の gitHarden の doc に書いた)。手元の `~/src` に partial clone は 0 件。
    再評価の trigger: partial clone の repo を glogx で日常的に見るようになったとき
  - P3-1 (記録のみ): pro-con wtclean の patch-id 比較 (`log -p | patch-id`) は、partial clone の repo では毎回「比較に失敗」になり、rebase で取り込んだ worktree を消さずに残す (安全側に倒れる)。
    直すなら patchIDs の log を GitArgsLazyFetch にし、許可リストへ足す
  - 3 周目は回していない。2 周目の修正は、テスト側の検査 (挙動テストと字面の許可リスト) と doc だけで、production の判定ロジックを足していない。どちらも変異で red を直接確かめた (adversarial-review-own-safeguards §7 の例外)
