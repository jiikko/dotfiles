# 1020 (test): treefiler の仮想化の等価性のテストが GOMAXPROCS=1 の全体 run で seed 269 だけ落ちる (光のセル)

起票日: 2026-11-04

## 概要

`src/treefiler/filer/virt_test.go` の `TestVirtualizedRandomOpsMatchFull` が、`TREEFILER_VIRT_SEEDS=300` の全体 run で seed 269 だけ落ちる。
違うのは線の 1 セルの色だけで、全部を動かす側に経路の光 (pulse.go の頭。背景も明るい) が乗り、仮想化した側には乗っていない。
[871](done/871-test-treefiler-virt-equivalence-flaky-under-load.md) と同じ「負荷で 1 セルの色だけ違う」型だが、871 で直した 2 種類 (粒の速さ・走査の到着) とは別の部品 (光)。

## 詳細

観測 (2026-11-04。go1.26.0・`fc1a52c83`。1019 の監査で、変更の検証に 300 本を回して見つけた):

- 失敗の行: `[キー j → キー G → 高さ 30 → キー f → 並び Reverse → フォルダを上に → 並び S → modified → 速さ normal → 行の間隔 0 → キー tab →
  ライブ更新 +2 -2 → 行の間隔 1 → 行の間隔 2 → キー ctrl+u → 行の間隔 2 → キー / → ライブ更新 +2 -1 → キー r] キー r (View だけ): (23,17) が違う`。
  仮想化 `fg [160.7 159 217.74] bg [9 10 15]` / 全部 `fg [235 242 255] bg [54.85 65.3 99]` (全部の側だけ光の頭の背景)。
  最後の `r` は検索の欄への入力で、比べたのは Advance を挟まない View だけのフレーム。光は Advance の中 (`step` → `stepPulses` →
  `emitPulse`) でしか出ないので、全部の側の光は `r` の前から走っていた。`r` でカーソルが一致へ移り、Advance の前なので仮想化した側には
  新しい経路の項目のアニメが無く、光の走る線 (`pulseHeads` の track) が両側で違う、という読み (未確認)
- 再現の表 (どれも変更の無い本体の checkout で):

  | 回し方 | 結果 |
  |---|---|
  | `TREEFILER_VIRT_SEEDS=300` (GOMAXPROCS 既定)・1 回 | seed 269 だけ赤 (1019 の worktree。後の 3 本の単独 run で変更とは無関係と確認) |
  | `GOMAXPROCS=1 TREEFILER_VIRT_SEEDS=300 -count=2` の全体 | 2 回とも seed 269 だけ赤 (598 本は緑) |
  | `-run '.../seed=269$'` の単独 (既定 / GOMAXPROCS=1) | 3 回 / 5 回とも緑 (`-v` で seed=269 の PASS 行を数えた) |

- 単独では落ちず全体でだけ落ち、全体では 2/2 で同じ種が落ちるので、揺れではなく**前の種から決まった状態が持ち越されている**と見ている (未確認)。
  候補: パッケージ変数 (`readBulk`・`loadWaiter`・`bulkBuf` など。`order`・`labelMax`・配色は New の `applyGlobals` で入れ直す) /
  `virt_test.go` の `newAt` が作った Model を閉じない (`t.Cleanup(m.Close)` が無い)。裏の goroutine は薄い: walker は待ち行列が空になれば抜け、
  ライブ更新の見張りは `Changed()` を呼ばないと起きず、欄は切ってあり、fixture は git の repo ではない
- 製品の不具合か、テストの組み立てか (871 の 2 つ目と同じ) かは未切り分け

## 対応方針

1. 単独の run と全体の run の seed 269 の最初のフレームで、Model の状態 (パッケージ変数を含む) の違いを探す (決まった持ち越しなら、どこかが最初から違う)
2. 失敗のフレームと `r` の直前のフレームで、2 つの Model の光の状態 (`pulses` の depth・el・`holding`・`movedAt`・`lastAdv`)・経路 (`spineAt` の結果)・
   経路の項目が描く位置 (`now`) に入っているかを出して比べる (871 と同じく、状態の比較から始める。直しを先に打たない)
3. 持ち越しの出どころを切る: seed 268 → 269 の 2 本だけを順に回して再現するか (`-run 'seed=26[89]$'`)。しなければ範囲を広げて二分する

再開の条件: このテストが CI か手元で落ちたとき、または pulse.go / virt.go を触るとき。

## 関連ファイル

- `src/treefiler/filer/virt_test.go` の `TestVirtualizedRandomOpsMatchFull` / `quiet`
- `src/treefiler/filer/pulse.go` の `stepPulses` / `pulseHeads`
- issue 871 (同じ型の前例)・1019 (見つけた監査)

## 進捗

- 2026-11-04: 起票 (再現の表まで。観測は未着手)
