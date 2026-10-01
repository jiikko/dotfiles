# 597 (bug): restartable の control の lock ファイルの fd が子に引き継がれ、runner が死んだ後もアプリが lock を握る

> 🚨 **担当中: dotfiles-58**（2026-10-01〜）

起票日: 2026-10-01

出典: restartable の audit (codex のコードスキャン、2026-10-01。[601](601-research-restartable-audit-2026-10-01.md))。Claude がコードで裏を取った。

## 問題

`src/restartable/internal/control/control.go` の `Listen` は lock ファイルを `syscall.Open(…, O_CREAT|O_RDWR|O_NOFOLLOW, 0600)` で開き、`O_CLOEXEC` を付けていない
(`os.OpenFile` は既定で close-on-exec を付けるが、`syscall.Open` は付けない)。Go の exec は close-on-exec の無い fd を子へ引き継ぐので、build / run / stop-cmd / ready-cmd の子が
lock の open file description を共有し、`flock` を一緒に握る。

## 発火条件

runner が SIGKILL などで死に (後始末が走らない)、子 (アプリ) や子孫が生き残る。新しい runner を同じ checkout で起動すると、生き残った子が握っている lock のため「二重起動」として拒否される。
アプリを手で止めるまで runner を起動できない。

## 推奨対応

lock の fd を `O_CLOEXEC` 付きで開く (`syscall.O_CLOEXEC` を足す、または `os.OpenFile` に寄せて `O_NOFOLLOW` を保つ)。子に lock の fd が渡っていないことをテストで固定する
(子の中で `/dev/fd` か `lsof` 相当を見て lock の path が無いことを確かめる。修正を戻す変異で red)。

## 確認

コードで確認 (2026-10-01 Claude)。実際に SIGKILL して再現はしていない。

## 進捗

- 修正: `control.go` の `Listen` の lock の open に `syscall.O_CLOEXEC` を足した (open と同時に付くので fork/exec との race も無い)。
- テスト: `TestListenLockFDIsNotInheritedByChildren`。Listen 後にテストバイナリ自身を子として exec し、lock の fd 番号を env で渡す。子は `fcntl(F_GETFD)` が EBADF なら exit 0、開いていれば exit 3。
- 変異: `mutate-verify` で `O_CLOEXEC` を外すと、新しいテストだけが red (`exit status 3`、他のテストは baseline と同じ ok)。rc=0。
- 検証: `go test -race -count=1 ./...` rc=0 (4 package ok)、`golangci_lint.sh v2.5.0 run` は 0 issues。
- codex の指摘と採否:
  - 設計の反証 (gpt-6-luna): 指摘なし。
  - 通常レビュー (`--commit HEAD`): 指摘なし (sandbox が Unix socket の bind を拒否するためテストは codex 側では回せなかった)。
  - 敵対的レビュー: (1)「`exec.Command` は FD を選別するのでテストは O_CLOEXEC 無しでも通りうる」は不採用。Go の exec は close-on-exec の無い fd をそのまま継承する (issue 本文の通り) うえ、変異で実際に red になった。(2)「親環境に `RESTARTABLE_TEST_LOCK_FD` があると親が exit する」は不採用 (テストが自分で子にだけ設定する変数で、親に入る経路を示せない)。`fork` と `exec` の間の一時的な fd は O_CLOEXEC で exec 時に閉じるため問題にならない。
- 未再現: 実際の SIGKILL で二重起動が拒否される再現はしていない (テストは fd の継承そのものを固定)。
