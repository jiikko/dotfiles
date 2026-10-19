# 838 (ux): push / pull / usage / git の失敗の表示から理由が落ちる (P2)

> 🚨 **担当中: air5 (glogx)**（2026-10-19〜）

起票日: 2026-10-19

## 概要

失敗を知らせる表示が、git や外部 CLI の出力の「最初の 1 行」や空文字しか出さず、ユーザーが理由と次の手を知れない形が 4 つある。
同じ失敗の形 (理由を捨てる・最初の 1 行に畳む) なので 1 本にまとめる。

## 詳細

### 1. push / pull の失敗 (P2)

- 該当: `src/glogx/external_commands.go` の `runGitPush` / `runGitPullRebase` / `abortRebase`、`tui.go` の `"pull に失敗: "+app.FirstLine(...)` と `"push に失敗: "+app.FirstLine(...)`。キーは一覧の `b` / `u`、status viewer の `b` / `p`
- push が non-fast-forward で弾かれると git の 1 行目は `To <remote>` で、トーストは「push に失敗: To github.com:…」になる。rejected とも「先に pull を」とも言わない
- pull --rebase が衝突すると 1 行目は `From <remote>` で、「conflict のため rebase を中断して元に戻しました: From /path/remote」になる (`abortRebase` のコメントは「衝突を見せる」と言う)
- `showWarning` は同じ 1 行を `lastWarning` に入れるので、`w` でコピーしても全文は戻らない
- 裏取り (監査の中で実施): scratchpad の repo と git 2.53.0 で再現。push の出力は 1 行目 `To …/remote.git`・2 行目 `! [rejected] … (fetch first)`。pull は 1 行目 `From …/remote`、`CONFLICT (add/add)` はもっと下

### 2. git の起動・時間切れの失敗 (P3)

- 該当: `src/glogx/internal/extcmd/git.go` の `RunGitCmd` と `GitExitError.Error()`、`main.go` の `exitGitError`
- `cmd.Run()` のエラーを種類を問わず `GitExitError{Stderr, Code}` に包み直し、`Error()` は stderr だけを返す。起動できない (`exec.ErrNotFound`)・`RunGitTimeout` の時間切れ (SIGKILL)・`exec.ErrWaitDelay` では理由が空文字になる
- 裏取り (監査の中で実施): `env -i HOME=$HOME PATH=/nonexistent <glogx> --no-pager -n 3` が rc=1 で stdout / stderr とも 0 バイト。TUI では「git status に失敗しました: 」「捨てられませんでした: 」の後ろが空になる (時間切れはコードからの推論)
- restore / add の途中で SIGKILL されたとき `index.lock` が残るかは**未確認リスク**

### 3. usage の初回取得の全滅 (P2)

- 該当: `src/glogx/internal/usageview/usage_overlay.go` の `BoxLines` (`o.err != nil` の枝)、`internal/ratelimitview/ratelimit_dashboard.go` の `centerLine("取得失敗")`
- 前回の値が無く両方の取得元が失敗すると、理由は U の箱・R のダッシュボード・トースト・`w` のどこにも出ない (claude が PATH に無い・429 の再試行時刻など)。コメントの「全文は R のダッシュボードに出る」は古い値がある場合にだけ当たる
- 裏取り (監査の中で実施): `Fixture().SetErr(2 行のエラー)` を幅 120 で描き、どちらも「取得失敗」だけ

### 4. status viewer の失敗と空の表示 (P3)

- 該当: `src/glogx/internal/statusview/status_view.go` の `emptyMessage` / `listLines` (`ClipToWidth(msg, listW)`、`statusListMaxWidth = 60`)、`headerLine`
- 幅 100 以上でも理由が一覧の列 (60) で切れ、右は空のまま。幅 140 で「git status に失敗しました: fatal: not a git repository …」、幅 200 で clean のときの案内も末尾が切れた
- 一度読めた後の読み直しが失敗すると、ヘッダは「🚨 status 取得失敗」だけで理由はどこにも出ない
- 最初に読めるまでヘッダは「unstaged 0 / staged 0」を出す (分からない数を 0 と見せる)
- 裏取り (監査の中で実施): `StubWorktree(…, err)` を幅 140 で描いた

## 対応方針

- 共通の口: `showWarning` (`tui.go`) は `SanitizePlainLine` を通すので改行を保てず、全文を `lastWarning` に入れても 1 行になる。**短い表示文と、行ごとに無害化したコピー用の詳細を別に持てる口**を先に作る (codex の反証レビューの指摘)
- push / pull: 出力から意味のある行 (`! [rejected]` / `CONFLICT` / `fatal:` / `error:`) を選んでトーストにし、全文をコピー用の詳細へ。「u で pull してから」は non-fast-forward (`fetch first` / `non-fast-forward`) のときだけ添える (hook・権限・保護ブランチの拒否には当てはまらない)
- `RunGitCmd`: 時間切れの SIGKILL も `*exec.ExitError` になりうるので、`ExitError` かどうかでは分けられない。`RunGitTimeout` 側で `ctx.Err()` を見て「時間切れ」を区別し、起動できない (`exec.ErrNotFound`) などは元のエラーを wrap して返す。`exitGitError` は stderr が空なら err を出す
- usage: R のダッシュボードに理由の 1 行目を出し、全文をコピー用の詳細へ
- status: 失敗の理由を一覧の列でなく全幅に出す。読めていない間は件数を出さない
- `FirstLine(` は非テストに 31 か所の呼び出しがある (出現は 32 で、うち 1 つは `internal/app/notice.go` の定義)。push / pull 以外は実 git で確かめていないので、直すときに同じ形を洗う

## 関連ファイル

- `src/glogx/external_commands.go` / `src/glogx/tui.go` / `src/glogx/main.go`
- `src/glogx/internal/extcmd/git.go`
- `src/glogx/internal/usageview/usage_overlay.go` / `src/glogx/internal/ratelimitview/ratelimit_dashboard.go`
- `src/glogx/internal/statusview/status_view.go`

## 進捗

- [ ] 1 push / pull
- [ ] 2 RunGitCmd
- [ ] 3 usage
- [ ] 4 status
