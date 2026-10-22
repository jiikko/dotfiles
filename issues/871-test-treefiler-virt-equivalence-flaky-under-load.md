# 871 (test): treefiler の仮想化の等価性のテストが負荷の下で揺れる

起票日: 2026-10-22

## 概要

`src/treefiler/filer/virt_test.go` の `TestVirtualizedRandomOpsMatchFull` (仮想化した Model と全部を動かす Model の毎フレームの画面を比べる)
が、線の 1 セルの**色だけ**違って落ちることがある。issue 870 の作業中に `TREEFILER_VIRT_SEEDS=300` で回して見つけた。

## 詳細

観測 (2026-10-22。どれも同じマシン・go1.26.0。load average は 20〜90 の間で揺れていた):

- 870 の変更 (線の格子のセルを値で持つ) を入れた木で、300 本の 1 回目に seed=23 が赤:
  `[キー . → キー 0 → 速さ slow → キー j] キー j の遅いフレーム 1: (23,7) が違う`。文字は同じ `╚` で、fg だけ
  仮想化 `[163.75 186.5 255]` / 全部 `[146.54 99.44 117.6]`
- 同じ木で seed=23 を単独で回すと緑。300 本の 2 回目は全部緑。origin/master (870 の変更の前) でも単独・300 本とも緑
- **波紋の肘の切り詰め (870 の 1) より前の commit (`e9506403^`) では、seed=23 は単独で毎回赤** (色の向きは上と逆)

見立て (**未確認**): 線の色は持ち主のフォルダの「配下の新しさ」の熱の色で、それを数える裏の走査 (`filer/walk.go` の walker) は時計を
`time.Now` で持ち、Model に注入した時計 (`Options.Now`) を使わない。2 つの Model で走査の結果が届くフレームが実時間の速さ次第でずれ、
片方だけ熱の色が変わる。1 フレームが重かった旧版 (切り詰めの前) では、ずれが毎回起きる時間になっていた、という読み。

## 対応方針

1. 見立ての確認: seed=23 の失敗のフレームで、2 つの Model の walker の結果 (`recs`) と熱の年齢 (`heatAge`) を出して比べる
2. 確かなら、比べる前に 2 つの Model の走査の結果が全部届くまで待つ (色を決めるのは結果の到着の順なので、walker の時計を `Options.Now`
   にそろえるだけでは直らない見込み。熱の年齢 `heatAge` は結果 (`recs`) の有無と `m.now` で決まる)

再開の条件: 次にこのテストが CI か手元で落ちたとき、または treefiler の walker / 熱の色を触るとき。

## 関連ファイル

- `src/treefiler/filer/virt_test.go` の `TestVirtualizedRandomOpsMatchFull`
- `src/treefiler/filer/walk.go` (walker の時計)
- issue 870 (見つけた経緯)

## 進捗

- 2026-10-22: 起票 (870 の作業中の観測)
