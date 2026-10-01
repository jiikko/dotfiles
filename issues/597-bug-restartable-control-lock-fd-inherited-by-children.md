# 597 (bug): restartable の control の lock ファイルの fd が子に引き継がれ、runner が死んだ後もアプリが lock を握る

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
