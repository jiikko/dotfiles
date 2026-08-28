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

---

## 進捗 (2026-08-28): (1) 解決 — 分割も x/ansi へ寄せた

**(1) は (2)(3) と独立** (端末が何セル割り当てるかを知らなくても、glogx の内部整合は決められる)
ので先に閉じた。

`termwidth.FirstCluster` を足し、**分割と幅を同じ呼び出しから同時に受け取る**形にした
(`ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)`)。x/ansi が分割 API を持っていたので、
uniseg を上げる / `dropToColumn` を作り替える、の 2 案は採らなくて済んだ。

直した呼び出し地点は 2 つ。issue が挙げていたのは `render.go:dropToColumn` だけだが、
**`issues/wrap.go:flattenSpans` が同型**だった (uniseg で切って `termwidth.Of` で測る。
折り返しはセル幅の総和で行幅を決めるので、総和が行全体の幅とずれると limit を超えた行が出る)。

`clusterWidth` は呼び出し元が消えたので削除した (幅は分割器が返す値をそのまま使う。
測り直す形は「分割と幅が別経路」を復活させる入口になる)。

### 実測 (2026-08-28、この repo の依存で)

| 分割器 | 2-rune クラスタ全域走査の違反 |
|---|---|
| uniseg (旧) | **744 件 / 93 rune** |
| x/ansi (新) | **0 件** |

走査は base 8 種 (`a x あ 漢 1 空白 🚀 ⚠`) × 第 2 rune `U+0020..U+2FFFF` × 全列。
違反 rune は issue 本文の一覧と一致 (U+0897 / U+1ACF..U+1ADD / U+113B8 群 / U+1E5EE 群 等)。

性能も測った (dropToColumn の micro bench、3 入力 × 6 列 × 3 run):
**11.8µs/op → 4.6µs/op (2.55 倍速)**、確保は 440 B / 16 allocs で不変。
frame 系の bench (`view_panel_alloc_kb` 35.57 / 予算 36.7、`view_diff_alloc_kb` 46.37 / 予算 47.8) も予算内。

### 回帰の守り方

- `render_test.go`: 明示ケースに U+0897 等を追加 + **全域走査テスト**を新設。
  手で列挙した rune の集合は依存を上げると動く (uniseg が 16 に上がれば 0 件になり次の版でまた出る)
  ので、列挙を追わずに範囲を総当たりして 0 件を維持する。走査が空振りしていないことも件数で見る
- `issues/wrap_test.go`: セル幅の総和 == 行全体の幅 / 折り返し行が limit を超えない
- `.golangci.yml`: depguard の uniseg 除外から `render.go` / `issues/wrap.go` を外した
  (本体から uniseg が消えたので、import ごと止まる)
- 変異検証: HEAD (uniseg 分割) に新テストだけを載せて 2 箇所とも red を確認 (worktree で実施)

## 進捗 (2026-08-28): 測れるようにした

(3)「判断に必要な実測ができない」を解いた。**probe に文字を足すだけでは足りなかった** —
測っても「どのモデルへ揃えるべきか」が決まらないので、`tools/width-probe` を作り替えた。

### 3 モデル併記にした

| 列 | 中身 |
|---|---|
| `grapheme` | `ansi.StringWidth` = glogx の `dispWidth` |
| `wc` | `ansi.StringWidthWc` = **bubbletea v2 描画エンジンの既定** |
| `uniseg` | `uniseg.StringWidth` = 分割に使っているライブラリ |
| `got` | 端末の実測 |

「判定」列にどのモデルと一致したかを出すので、1 回の実行で揃える先の候補が絞れる。

### 敵対的レビューが直させたもの

| 重要度 | 内容 |
|---|---|
| P1 | **CPR に再同期が無く、1 バイト紛れ込むだけで「揺れた」という結論を捏造できた**。決定論的な偽端末に `R` を 1 バイト注入すると「8 文字が揺れた」と断定した。ESC から読み直す形にし、**基準文字 `ASCII x` が 1 でなければ run 全体を無効**と宣言するようにした |
| P1 | **2 秒 deadline が実 TTY で no-op だった**。`SetReadDeadline` は darwin で `file type does not support deadline` を返す (実測)。CPR が返らないと raw mode のまま無限ハングし、ISIG が落ちているので Ctrl-C も効かない。goroutine + select の実効するタイムアウトに置き換え、**stdout が TTY でなければ起動を止める**ようにした (レビューのハング再現手順が exit 2 で止まることを確認) |
| P1 | **`RUNEWIDTH_EASTASIAN` ガードが無かった**。本体と全 TestMain は `widthenv.ExitIfUnsupported` を通るのに、**揃える先を決めるこの道具だけが素通り**して誤った判定列を静かに出していた |
| P2 | **私の README の主張が偽だった**。「絵文字だけでは何も分からない」と書いたが、既存の `⚠+VS16` こそ (2,1,2) で**一覧中で最も決定力のある probe**。真に受けると唯一の決め手を落とす |
| P2 | **追加した 6 文字は割れる 32 種のうち 5 種しかカバーせず、2 文字は判別に無情報**だった (`U+09BE` / `x+U+0897` は grapheme==wc)。RI 単独 (2,1,2) と keycap (2,1,1) を追加した |
| P2 | **`wc` 列は固定の座標系ではない**。`ansi.StringWidthWc` は `mattn/go-runewidth` (indirect) を使い、版で答えが変わる (`ಕಾ` は v0.0.23 で 1、v0.0.27 で 2)。解決版を出力に載せるようにした |
| P2 | 診断用 probe が「食い違い N 件」に混ざり、結語の「使用をやめる」が `ಕಾ` にも掛かっていた。`ui` フラグで分けた |

### ⚠️ この測定でも決まらないこと (レビューの P1-3。issue に残す)

bubbletea v2 は起動時に **mode 2027 を交渉し、対応端末では端末側の幅解釈も切り替える**
(`tea.go` の `RequestModeUnicodeCore` → `setWidthMethod` → `SetModeUnicodeCore`)。
width-probe は 2027 を要求も設定もしないので、**glogx が実際に描画する構成とは別の構成を
測っている**。さらに `shouldQuerySynchronizedOutput` は `TERM_PROGRAM` 等で分岐し、
Apple 系では 2027 を問い合わせすらしない。

つまり今回の測定は「**2027 OFF 側の答え**」。ここで `wc` と一致するなら揃える先の議論に進み、
そのとき初めて DECRQM (`CSI ? 2027 $p`) を測る価値が出る。

### 次

→ [issue 136](136-human-verify-glogx-width-model.md) で実端末の測定を依頼した (2026-08-28 に 127 から改番)。
**(2) はこれが返るまで決められない** ((1) は上のとおり測定なしで閉じた)。
残っているのはこの issue では (2) だけ。
