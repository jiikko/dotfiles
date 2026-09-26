# 543 (ux): 持ち主の画面が居ないとき、--join の画面にその旨と dispatcher が止まっていることを大きく出す

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-26 21:05 ごろ、開いていたのは `--join` の画面だけで、dispatcher を止めると誰も起こし直さなかった (481 の決定どおり。`live/live.go` の `keep` は join で返る)。
PM と取り込みの係は動いたまま、カードは進まない状態になったが、join の画面からはそれが分かりにくかった。原因の特定には `pro-con ps` とコードを読む必要があった。

- 持ち主の画面が 0 になると、dispatcher は 1 分で PG を止めて抜ける (`--exit-without-screens 1m`)。join の画面はこの数に入らない

## 期待する動作

- join の画面は、持ち主の画面が 0 のとき、ヘッダに目立つ形で「持ち主の画面が無い (dispatcher は N 秒後に止まる / 止まっている)。持ち主の画面を開くと起きる」を出す
- dispatcher が止まっている間は、カードが進まないことをはっきり出す (今の「dispatcher n 秒」の表示に頼らない)
- (任意) join の画面から 1 キーで持ち主の画面に切り替える (y/N で確かめる)

## 受け入れ条件

- [ ] 持ち主が 0 の join の画面で、ヘッダにその旨と dispatcher の状態が出る
- [ ] 持ち主が居る間は出ない

## 関連

- 481 (join の画面) / 506 (supervisor。持ち主の画面が起こす) / 519 (終了のダイアログ) / `src/pro-con/live/live.go` (`keep` / `dispatcherIdle`)

## 進捗

(まだ無い)
