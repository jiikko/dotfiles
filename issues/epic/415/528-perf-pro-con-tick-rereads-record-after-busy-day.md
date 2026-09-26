# 528 (perf): 完了して 24 時間以内のカードが溜まる日は、dispatcher の Tick 1 回が 25ms・11.7MB になる (478 の見送りの前提が崩れた)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md) / 見つけた監査: [513](done/513-research-pro-con-performance-audit-2026-09-26.md)

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

## 進め方 (2026-09-27、ユーザー「refresh からしたほうがいいかな」)

- 画面の `live.Backend.refresh` も dispatcher と同じ `store.Load` で記録を丸ごと読む (`live/live.go` の `refresh`)。
  refresh は 3 秒ごと + 知らせのたびに走り、画面の動きを直接止める (画面が 2 つなら 2 倍)。**まだ測っていないので、最初に refresh も同じ本物の写しで測る**
- 直す場所は `store.Load` を第一候補にする (上の別案: ファイルの大きさと更新時刻が同じなら前の結果を返す)。dispatcher の Tick と画面の refresh の両方に 1 か所で効く。
  🚨 返した State を呼び手が書き換えないこと (複製を返すか、読むだけの呼び手に限る) を、書き換える変異で落ちるテストで固定する
- 効果は Tick 1 回と refresh 1 回の両方で、before / after を実測して書く

## 実測と直し (2026-09-27、C-097)

本物の `cards.json` の写し (2026-09-27 02:05。60 枚・履歴 1,076 行・402KB) で、Apple M3 Max、`-count 3`。
bench は `BenchmarkLoadBusyDay` (store) / `BenchmarkTickBusyDay` (dispatcher) / `BenchmarkRefreshBusyDay` (live)。
組み立ては `store/storetest.BusyDay` にまとめた: `PRO_CON_BENCH_CARDS=<写しのパス>` で本物の写しを、無ければ形を寄せた合成の記録を置く
(本物の記録は repo に入れない)。Tick は 478 の bench と同じ組み立てで、最初の Tick (起動・状態の変化) は測らない。

| 何を 1 回 | before | after |
|---|---:|---:|
| `store.Load` | 2.31〜2.37 ms / 1.05 MB / 3,989 allocs | 0.15〜0.17 ms / 173 KB / 427 allocs |
| 画面の `refresh` | 2.52〜2.99 ms / 1.25 MB / 4,167 allocs | 0.27〜0.31 ms / 370 KB / 605 allocs |
| dispatcher の `Tick` | 36.3〜36.9 ms / 15.9 MB / 59,971 allocs | 2.67〜2.88 ms / 2.63 MB / 6,514 allocs |

- **画面の refresh は、所要の 8〜9 割が `store.Load` だった** (2.3ms / 2.5〜3.0ms)。上の「画面の分は測っていない」はこれで埋まった
- Tick は before で Load 約 16 回ぶん。起票時 (296KB で 25.1ms) より記録が 402KB に増えたぶん重い

### 直したこと

- **`store.Load` は、記録のファイルの (inode, 大きさ, 更新時刻) が前に読んだときと同じなら解析し直さず、前の結果の深い複製を返す** (`store/loadcache.go`)
  - 同じ中身かの判定: 記録の書き手は `writeAtomic` (一時ファイル → rename) だけなので、書くたびに inode が変わる。鍵は開いた fd から取る (stat と read の間に差し替わっても、鍵と中身がずれない)
  - 呼び手に漏れないこと: キャッシュの State は外に出さず、返すたびに reflect で深い複製を作る (カードの型に欄が増えても追従する)。
    非公開の参照の欄は複製しないので、記録の型に足すと `TestLoadedStateHasNoUnexportedRefs` が落とす
  - 書き手の `Update` / `Apply` / `Archive` は、書く前に必ず読み直す (`loadFresh`)。「古い記録の上に書く」事故を鍵の判定に預けない
- profile で残りを見ると、Tick の大半は毎回読み直す書き手 2 つ (`Apply` と `Archive`) の空振りだった。**`Apply` は箱が空なら、`Archive` は移すものが無ければ、キャッシュの読みで判定して書かずに返す** (書くときだけ読み直して判定し直す)。
  これで Tick は 8.0〜9.2ms → 2.7〜2.9ms。箱が空の `Apply` も、記録が壊れていればエラーを返す (前と同じ)
- テスト: `TestLoadResultIsNotShared` (全部の欄を埋めた記録を使い、キャッシュに当たった読みで得た State の届く値をすべて書き換えてから、次の読みがファイルの中身のままか) /
  `TestLoadSeesRewrite` (Update・同じ大きさの上書き・削除の後に古い記録を返さない) / `TestBrokenStateIsAnError` に「読めた記録をキャッシュしてから壊す」「箱が空の Apply」を足した。
  変異で確かめた: 当たったら複製せずに返す → 落ちる / 浅い複製 (カードの slice だけ写す) → 落ちる / 鍵を見ない → 落ちる / 箱が空の Apply が記録を見ない → 落ちる。
  鍵から inode を外す変異は生き残る (更新時刻だけで検出できる。inode は保険)

### 残り

- Tick の残り 2.7ms は `live.LoadRegistry` (sessions.json) と、Load 十数回ぶんの深い複製 (1 回 0.15ms)。478 の案 (1 Tick の中で 1 度読んだ State を渡し回す) を入れれば複製も消えるが、今の大きさでは見送ってよい
- 偽の launcher で回した Tick なので、session / PM が居るときの段の通り方は測っていない (起票時の「未確認」のまま)

## 関連ファイル

- `src/pro-con/dispatcher/dispatcher.go` — `tick` と各段
- `src/pro-con/store/store.go` — `Load` / `Update`
- `src/pro-con/store/archive.go` — `AutoClearAfter` (完了から 24 時間は記録に残る)
- `issues/epic/415/done/478-perf-pro-con-cards-json-grows-and-is-reread-every-tick.md` — 見送りの決定と trigger

## 決着 (2026-09-27)

- ユーザーの判断で done (issue-sync)。直しの本体 (`8282a8cc`: Tick 36.9ms → 2.7ms / refresh 2.8ms → 0.28ms) は master にある。
  上の「残り」の 2 つ (残りの 2.7ms は今の大きさでは見送り / 本物の session・PM が居るときの Tick は未計測) は見送りとして残す。記録がまた大きくなったら測り直す
