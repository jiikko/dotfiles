# av1ify: VFR 検出閾値が `AV1IFY_FRAME_TOLERANCE_PCT` に相乗りしていて、1 つのノブが 2 つの意味を逆向きに動かす

起票日: 2026-09-19
カテゴリ: bug / priority: **medium**
対象: `zshlib/_av1ify_encode.zsh` の `__av1ify_detect_vfr`、`zshlib/_av1ify_postcheck.zsh` のフレーム数チェック
出典: [issue 394](394-bug-av1ify-vfr-passthrough-breaks-quicktime.md) の実装への敵対的レビュー (回帰観点)
反証レビュー: 実施。**下記の連動はレビュー時の実測**

## 問題

`__av1ify_detect_vfr` は VFR 判定の閾値に、**postcheck のフレーム数許容%** である
`AV1IFY_FRAME_TOLERANCE_PCT` (既定 0.5) をそのまま流用している:

```sh
local tol_pct="${AV1IFY_FRAME_TOLERANCE_PCT:-0.5}"
...
print (d / a * 100 > tol) ? 1 : 0
```

コード上は「閾値を揃える」意図としてコメントされているが、この 1 つのノブは
**2 つの検査を逆向きに動かす**。

### 実測 (r=30/1, avg=1000/33 = 30.303、乖離 1.01% のソース)

```
PCT=0    → VFR_DETECTED=1
PCT=0.5  → VFR_DETECTED=1   (既定)
PCT=2    → VFR_DETECTED=0   ← 正規化が止まる
PCT=5    → VFR_DETECTED=0
```

### 緩める向き (PCT を上げる)

postcheck のフレーム数チェックを緩めるのは、長尺素材では**正当な調整**として
コード側のコメントも想定している。しかし同時に **VFR 検出が鈍る**ので、乖離 1〜2% の
VFR ソースが**黙って正規化対象から外れ、issue 394 のバグが復活する**。
ログにも出ない (`>> ソースは可変フレームレート...` が出なくなるだけ)。

### 締める向き (PCT=0) — こちらの方が悪い

- postcheck 側: `rel_tolerance = src × 0 / 100 = 0` になり許容が絶対フロア 24 まで下がる (最も厳しい)
- 検出側: `d > 0` で発火するので、**`r_frame_rate=30/1` / `avg_frame_rate=30000/1001` という
  ごく普通の NTSC メタデータ** (30.000 vs 29.970 = 乖離 0.1%) が VFR 判定になる
- → `-fps_mode cfr` が広く付き、[23bb19fd](394-bug-av1ify-vfr-passthrough-breaks-quicktime.md) が
  「91 本中 36 本で出力フレーム数が変わる」として**明示的に避けた経路へ戻る**
- → しかも `_fps_changed=1` でフレーム数チェックが無効化されるので、**その変化は誰にも見えない**
  ([issue 397](397-bug-av1ify-vfr-wrong-r-value-undetected-frame-loss.md))

## 何が壊れているか

「従来 OK だったファイルへの影響を無くす」という 23bb19fd の保証は、
**`AV1IFY_FRAME_TOLERANCE_PCT` が既定 0.5 のときにだけ成立する**。
ノブが動いた瞬間に保証が崩れることが、コードにもコミットメッセージにも書かれていない。

## 受け入れ条件

- [ ] VFR 検出閾値を独立した変数 (`AV1IFY_VFR_DETECT_PCT` 等) に分離する。既定値を 0.5 に
      揃えるのは構わないが、**同じ変数を読ませない**
- [ ] 分離後、`AV1IFY_FRAME_TOLERANCE_PCT` を動かしても VFR 検出結果が変わらないことを
      テストで固定する (PCT=0 / PCT=5 の両端で検出結果が不変)
- [ ] 分離しないと判断する場合は、「この値は postcheck の許容と VFR 検出の鋭さを**逆向きに**動かす」
      を `__av1ify_detect_vfr` の 🚨 コメントへ書く (`pending-issue-rationale-in-code.md` の対象)

## 未計測

- PCT=0 にしたとき実コーパス 91 本のうち何本が VFR 判定へ転ぶかは、コーパスが手元に無いため未計測。
  「36 本の退行が戻る」は論理的帰結であって実測ではない


## 進捗 / 結果 (2026-09-19)

- [x] `AV1IFY_VFR_DETECT_PCT` へ分離 (`fix(av1ify,397,398,399): …`)
- [x] `AV1IFY_FRAME_TOLERANCE_PCT` を両端 (0 / 5) へ振っても VFR 検出結果が変わらないことを
      Test 8 で固定。検出閾値そのものが効くことは Test 9 で固定
- 変異検証: 相乗りへ戻す変異 (M3) で Test 8 / Test 9 が red

🚨 **同型の欠陥を、この修正の兄弟 (issue 397 の密度検査) が一度再生産した**。密度検査のフロアが
ユーザー向けに文書化済みの `AV1IFY_FRAME_TOLERANCE` を兼ねており、緩めると破壊的判定が黙って
無効化される状態になっていた (敵対レビュー 2 周目が検出)。`AV1IFY_DENSITY_FLOOR` へ分離済み。
**「1 つのノブに 2 つの意味を持たせない」は、閾値を新設するたびに確認すること。**
