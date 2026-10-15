# 769 (refactor): pro-con の UI のクリップボードと「外で開く」を subproc に寄せる

起票日: 2026-10-15

## 概要

pro-con の UI は外部コマンドを `subproc` を通さずに起こしている箇所が 2 つある。glogx と treefiler は issue 718 で
`subproc/clipboard` と `subproc` を通す形に寄せたが、pro-con は 718 の対象に入っていなかった。pro-con は既に subproc に依存している。

## 詳細 (2026-10-15 に本文で確認)

- `pro-con/ui/clipboard.go` の `pbcopy`: `subproc/clipboard.Copy` と同じ働き。違いは時間の上限 (2 秒 / WaitDelay 1 秒。subproc 側は 5 秒 / 2 秒) と、
  tmux のバッファへも積むかどうか (subproc 側は積む)
- `pro-con/ui/attachments.go` の `openWithSystem`: `exec.Command("open", "-a", "Preview", "--", ...).CombinedOutput()` で、時間の上限も WaitDelay も無い。
  pipe を持つのに WaitDelay を付けない形は subproc の doc が止めている形にあたる。ただし `open -a Preview` はすぐ戻るので実害は未確認
- treefiler の `filer/open.go` は subproc を通している

## 対応方針

- `pbcopy` を `subproc/clipboard.Copy` に置き換える。tmux のバッファへも積むのが pro-con で望ましいか (glogx と同じでよいか) を先に決める
- `openWithSystem` を subproc の起動 (上限と WaitDelay 付き) に置き換える
- 時間の上限が変わる (2 秒 → 5 秒) ので、UI のどの経路で待つかを確かめる (同期で待つなら画面が止まる時間が延びる)

## 関連ファイル

- `pro-con/ui/clipboard.go` / `pro-con/ui/attachments.go` / `src/subproc/clipboard/` / `treefiler/filer/open.go`
- issue 718 (glogx・treefiler の寄せ) / 調査の全体: 773
