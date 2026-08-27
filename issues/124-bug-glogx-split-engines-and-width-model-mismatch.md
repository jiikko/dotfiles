# 124 bug: 分割器が 2 本あるため列不変条件が完全には成立しない / 揃える先が描画エンジンとずれている

起票日: 2026-08-27 / 出典: issue 112 の修正に対する敵対的レビュー / priority: medium

112 で**幅**エンジンは 1 本になった。しかし残る 2 つの構造的な問題は 112 の範囲外なので分ける。

---

## (1) 分割器が 2 本ある (uniseg = Unicode 15 / x/ansi = 16)

`render.go:dropToColumn` はクラスタを **`uniseg.FirstGraphemeClusterInString`** で切り、
幅は `dispWidth` (= `ansi.StringWidth`、内部で別の分割器) で測る。
**両者のクラスタ境界が食い違うと、`clusterWidth` を何にしようが列不変条件は崩れる。**

- `rivo/uniseg v0.4.7` は `graphemerules.go:40` に **`// Unicode version 15.0.0.`** と明記
- `charmbracelet/x/ansi v0.11.7` の分割器は Unicode 16

**自分で再現 (2026-08-27、112 の修正後のコードで)**:

```
入力 "axࢗz" (U+0897 は Unicode 16 で追加された結合マーク)
  ansi.StringWidth(全体) = 3
  uniseg の分割         = 4 クラスタ / 幅の総和 = 4
  → 列不変条件が n=2 と n=3 で崩れる
```

**全域ブルートフォース** (base 8 種 × U+0020..U+2FFFF の 2 rune 組み合わせ、全列総当たり。
敵対的レビューの実測):

| `clusterWidth` の実体 | 不変条件違反 |
|---|---|
| 旧 `uniseg.StringWidth` | 6720 件 |
| **新 `dispWidth` (112 の修正後)** | **757 件** |
| 参考 `ansi.StringWidth` | 757 件 (新と完全一致) |

→ 112 は **89% 削減**したが 0 にはならない。残存は 99 rune
(`U+0897` / `U+1ACF..U+1ADD` / `U+1AE0..U+1AEB` / `U+10D69..U+10D6D` / `U+10EFA..U+10EFC` /
`U+113B8..U+113D2` 群 / `U+11B60..U+11B67` / `U+1611E..U+1612F` / `U+16D63,U+16D67..U+16D6A` /
`U+1E5EE..U+1E6F5` 群)。

⚠️ **件数は依存を上げるたびに変動する**。uniseg が Unicode 16 に上がれば 0 になり、
次の Unicode 版でまた出る。112 が足したテストはこの集合を 1 文字も含まないので検出しない。

**対応の方向 (要判断)**: 分割も x/ansi 側へ寄せる (`ansi` にクラスタ分割 API があるか要調査) /
uniseg を Unicode 16 対応版へ上げる / そもそも「クラスタごとの幅の総和 = 全体の幅」に
依存しない形へ `dropToColumn` を作り替える。

---

## (2) 「揃える先は ansi」が bubbletea v2 の既定と食い違う

`width.go` の設計意図は「描画エンジンの幅モデルと一致させる」。だが依存の実体は:

- `ultraviolet/buffer.go` — `Method: ansi.WcWidth` (**既定は WcWidth**)
- `bubbletea/v2 tea.go` — 端末が mode 2027 を報告したときだけ `setWidthMethod(ansi.GraphemeWidth)`

`dispWidth` は `ansi.StringWidth` = **GraphemeWidth** 側。**width.go 自身のコメントが既に
この乖離を記録している** (「Terminal.app + tmux は 2027 を報告しない見込みなので、実運用では
WcWidth 側になる」)。つまり既知の未解決事項。

112 が足したテストの入力で実測すると:

| 文字 | `ansi.StringWidth` (dispWidth) | `ansi.StringWidthWc` (**エンジン既定**) | `uniseg.StringWidth` (112 で消した側) |
|---|---|---|---|
| `ಕಾ` カンナダ | **1** | 2 | 2 |
| `؀` U+0600 | **0** | 1 | 1 |
| `का` デーヴァナーガリー | **1** | 2 | 2 |

→ **4 入力のうち 2 つで、112 が消した側 (uniseg) の方がエンジン既定と一致していた。**
112 の修正は「内部整合を取る」点では正しい (6720→757 が根拠) が、
**エンジンと揃えたわけではない**。

`ansi.StringWidth` と `ansi.StringWidthWc` が食い違う 2-rune クラスタは **2115 件**あり、
これは `clusterWidth` 1 箇所ではなく `dispWidth` を通す**全経路**
(`truncateDisp` / `truncateDispLeft` / `fillRight` / box 幅) に効く。
**112 で潰した範囲より、残した側の方が広い。**

## (3) 判断に必要な実測ができない

中心的な問いは「実端末 (Terminal.app + tmux) は `ಕಾ` に何セル割り当てるか」。
`src/glogx/tools/width-probe` は端末に CPR で問い合わせる道具だが、
**probe 一覧にインド系・Arabic format 文字が 1 つも無い**
(ASCII / あ / ⚠ / ✔ / ✓ ✗ ● ⊘ │ ╔ █ ▓ ▖ ⠋ ❯ のみ)。

`width.go` の v1/v2 比較表と `docs/glogx-bubbletea-v2.md` の表も
「食い違うのは国旗・keycap」としか書いておらず、**インド系が 1 文字も入っていない**。
これは 112 自身が「サンプルが不一致の出る範囲を含んでいない」と批判した欠陥そのもので、
112 は直していない。

**最初にやること**: `tools/width-probe` の probe 一覧にインド系 (`ಕಾ` `का` `கா`) と
`U+0600` を足し、実端末で測る。**それが取れるまで (1)(2) の方向は決められない。**
