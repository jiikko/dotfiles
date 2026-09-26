# 525 (perf): 完了して 24 時間以内のカードが溜まる日は、dispatcher の Tick 1 回が 25ms・11.7MB になる (478 の見送りの前提が崩れた)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md) / 見つけた監査: [513](513-research-pro-con-performance-audit-2026-09-26.md)

## 概要

478 は終えたカードを書庫へ移して記録を小さくし、「1 Tick の中で記録を 8 回読み直す」直しは見送った。見送りの理由は
「書庫へ移した後の記録は動いているカード + 完了して 24 時間以内のカードだけ」で、再評価の trigger は「動いているカードが数十枚を超えたら測り直す」。
ところが **完了して 24 時間以内のカードは書庫へ移らない** (`store.AutoClearAfter`) ので、よく使った日は動いているカードが少なくても記録が大きい。
2026-09-26 21:14 の本物の `cards.json` は 54 枚 (完了 50 / 動いている 4)・履歴 900 行・296KB だった (478 の見積もりの 1 枚 4KB より重く、1 枚 5.5KB)。

## 実測 (2026-09-26、module の一時コピーで。本物の cards.json の写しを使った。Apple M3 Max)

| 何を | 1 回 |
|---|---:|
| `store.Load` | 1.76 ms / 826 KB / 2,834 allocs |
| `store.Update` (何も変えない) | 3.60 ms / 2.26 MB / 4,504 allocs |
| dispatcher の `Tick` (偽の launcher・session 一覧は空、478 の `BenchmarkTickWithFinishedCards` と同じ組み立て) | **25.1 ms / 11.7 MB / 39,868 allocs** |

- 478 の直した後の bench (空の Tick 0.35ms) の約 70 倍。Tick は 3 秒ごと (`dispatchercmd.go` の `dispatcherInterval`) なので、1 コアの約 0.8% と毎秒約 3.9MB の確保
- Tick の所要は Load の約 14 回ぶん (所要からの割り算で、呼び出しは数えていない)。読み取り専用の走査では、1 Tick の中で `store.Load` を呼ぶ所が
  `register` / `trackDead` / `trackPrompts` / `checkAtStart` / `stopMarked` / `stopCrashing` / `requeueVanished` / `watch` / `tickRuns` (最大 2) / `deliverOrders` /
  `tickBtws` / `tellRole` (PM と取り込みの係で 2) / `dispatch` にあった。これに状態の変わったカードごとの `store.Update` (読み直し + `card.Check` 2 回 + 全体の書き直し) が足される
- 画面の側も記録を丸ごと読む (`live.Backend.refresh`。478 の「画面は 3 秒ごと + 知らせのたび」)。画面の分は測っていない
- 未確認: 偽の launcher で回したので、本物の Tick が同じ段を全部通るか (session が居る・PM が居るときに増えるか減るか) は測っていない

## 対応方針 (案)

- 478 の見送った案: 1 Tick の中では 1 度読んだ State を渡し回す (書き手は dispatcher だけ)。478 は「Tick の全段の署名を変える大きさ」を理由に見送った
- 別案: `Load` を、ファイルの (大きさ, 更新時刻) が同じなら前の結果を返す形にする (署名を変えずに読み直しの大半を消せる。書き手の `Update` は必ず読み直す)。
  🚨 返した State を呼び手が書き換えると共有が壊れるので、複製を返すか、読むだけの呼び手に限る
- 別案: 完了のカードは記録の中でも履歴を持たない形にする (書庫と二重に持たない)
- どれでも、効果はこの bench (本物の記録の写しでの Tick 1 回) の前後で測る

## 関連ファイル

- `src/pro-con/dispatcher/dispatcher.go` — `tick` と各段
- `src/pro-con/store/store.go` — `Load` / `Update`
- `src/pro-con/store/archive.go` — `AutoClearAfter` (完了から 24 時間は記録に残る)
- `issues/epic/415/done/478-perf-pro-con-cards-json-grows-and-is-reread-every-tick.md` — 見送りの決定と trigger
