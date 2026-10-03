# 543 (ux): 持ち主の画面が居ないとき、--join の画面にその旨と dispatcher が止まっていることを大きく出す

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

2026-09-26 21:05 ごろ、開いていたのは `--join` の画面だけで、dispatcher を止めると誰も起こし直さなかった (481 の決定どおり。`live/live.go` の `keep` は join で返る)。
PM と取り込みの係は動いたまま、カードは進まない状態になったが、join の画面からはそれが分かりにくかった。原因の特定には `pro-con ps` とコードを読む必要があった。

- 持ち主の画面が 0 になると、dispatcher は 1 分で PG を止めて抜ける (`--exit-without-screens 1m`)。join の画面はこの数に入らない

## 期待する動作

- join の画面は、持ち主の画面が 0 のとき、ヘッダに目立つ形で「持ち主の画面が無い (dispatcher は N 秒後に止まる / 止まっている)。持ち主の画面を開くと起きる」を出す
- dispatcher が止まっている間は、カードが進まないことをはっきり出す (今の「dispatcher n 秒」の表示に頼らない)
- (任意) join の画面から 1 キーで持ち主の画面に切り替える (y/N で確かめる)

## 受け入れ条件

- [x] 持ち主が 0 の join の画面で、ヘッダにその旨と dispatcher の状態が出る
- [x] 持ち主が居る間は出ない

## 関連

- 481 (join の画面) / 506 (supervisor。持ち主の画面が起こす) / 519 (終了のダイアログ) / `src/pro-con/live/live.go` (`keep` / `dispatcherIdle`)

## 進捗

- 2026-09-27 (C-102): `feat(pro-con): 持ち主の画面が無い join の画面に、罫線の上の帯で dispatcher の状態を出す (issue 543)`
  - 見本 (`src/pro-con/samples/543-join-no-owner/`) の案 A / B / C からユーザーが **案 A (罫線の上に全幅の帯 1 行)** を選んだ。任意の「1 キーで持ち主の画面に切り替え」は「今回は作らない」と決まり、[new-ux-pro-con-join-switch-to-owner](548-ux-pro-con-join-switch-to-owner.md) へ分けた
  - 帯は 4 通り: 黄 = dispatcher は動いているが止まっても誰も (supervisor も) 起こし直さない / 赤 = 落ちた (持ち主の画面を開くと起きる)・1 度も回っていない・人が止めた (持ち主の画面の c) / 赤 = プロセスは居るが回っていない (固まった。lock を持つので持ち主の画面を開いても起きない)。画面を数えられない・持ち主が居る・持ち主の画面では出さない (`ui/view.go` の `ownerlessBand`。ヘッダの行数は `headerRows()` で 4 / 5)
  - 🚨 概要の「join の画面はこの数に入らない」は `--exit-without-screens` については違う: dispatcher の `screensOpen` は join も数えるので、join が開いている間 dispatcher は無画面で抜けない。困るのは「止まったら誰も起こし直さない」(join は起こさない・supervisor は持ち主が居なければ起こし直さずに PG を止めて抜ける = `supervise.go` の `haltReason`) 方で、帯はこちらを出す
  - 実測: 隔離 HOME + tmux (-L) の本物の `--join` の画面で、dispatcher 未起動 → 赤の帯、持ち主の画面を開く → 帯が消える、持ち主の画面を kill → 黄の帯 (3 枚をカードに添付)。`TestOwnerlessJoinBand` (変異 4 つで落ちるのを確認)。`make test-changed` (src/pro-con + tests/issues) 緑
  - codex の敵対的レビュー: P2 2 件。固まった dispatcher を「開くと起きる」と言っていたのを直した。もう 1 件 (手で起こした dispatcher のとき見張りが戻らない) は、持ち主の画面の `keep()` が Tick ごとに起こし直すので誤検知
  - 残り: なし (切り替えは別 issue)
