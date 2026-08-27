# 112 bug: glogx に幅エンジンが 2 本あり、「単一の出典」という宣言が実測で成立しない

起票日: 2026-08-27 / 出典: leaky-abstraction 監査 (L4 capability の嘘) / priority: high

## 事実

`src/glogx/width.go` の doc は「幅計算の単一情報源。glogx の表示幅は必ずこのファイルの関数を通す」
と宣言し、`clusterWidth` の doc は **`(dispWidth と同一の幅モデル)`** と断言している。実際は:

| 関数 | 実体 |
|---|---|
| `dispWidth` | `ansi.StringWidth` (charmbracelet/x/ansi) |
| `clusterWidth` | **`uniseg.StringWidth` (rivo/uniseg)** |

**実測 (2026-08-27、本 repo で再現)**: U+0020〜U+2FFFF の単一 rune クラスタで **434 件が不一致**。

```
U+0600 (Arabic number sign)  disp=0 / cluster=1
ಕಾ (U+0C95 U+0CBE)            disp=1 / cluster=2
া (U+09BE)                    disp=1 / cluster=0
```

ASCII / CJK / 国旗 / ZWJ 絵文字 / 肌色 / bare ⚠ は**全部一致する** (= 日常の入力では出ない)。

## 発火条件と壊れ方

`clusterWidth` の唯一の消費者は `render.go:dropToColumn`。合成側が当てにしている
**`dispWidth(dropToColumn(s, n)) == dispWidth(s) - n`** が崩れる。実測 (全列を総当たり):

| 入力 | 崩れた列 |
|---|---|
| `"கா கா கா தமிழ் commit"` (タミル) | 9/17 |
| `"ಕಾ ಕಾ commit"` (カンナダ) | 10/11 |
| `"ASCII only commit"` | 0/18 |
| `"日本語 commit"` | 0/14 |

浮動ボックス (job パネル / overlay / toast) を背景行の上に合成する経路で、背景行にインド系文字か
Arabic format 文字が含まれると**ボックス右側の背景が 1〜3 桁ずれる**。

**silent**。例外もログも出ない。毎フレーム再計算なので、README が記録している
「再描画のたびに桁がずれて行が揺れる」の再発経路そのもの。

## なぜ今まで検出されなかったか (ここが本題)

- **`.golangci.yml` の depguard `width-single-source` は `mattn/go-runewidth` だけを deny しており、
  `rivo/uniseg` は素通り**。「幅の唯一の出典は dispWidth」という不変条件は、
  **実際に使われている 2 本目のエンジンを 1 つも止めていない**
- `docs/glogx-bubbletea-v2.md` の x/ansi vs uniseg 比較表は **4 文字 (bare ⚠ / ⚠+VS16 / 国旗 / ●)
  で比べて「一致」と結論**している。サンプルが不一致の出る範囲を含んでいない
- `issues/done/054-*.md` に「`clusterWidth` が `uniseg.StringWidth` なので幅モデルが二重化している」
  という記述は**ある**が、文脈は `RUNEWIDTH_EASTASIAN=1` を支持するかの話で結論は「支持しない」。
  **素の環境でも 434 件ずれること・`dropToColumn` の列不変条件が実際に崩れることは 054 に無い**

## 対応

1. `clusterWidth` を `ansi.StringWidth(cluster)` に替える (1 行)
2. depguard の deny に `rivo/uniseg` を足す。**ただし分割用途** (`FirstGraphemeClusterInString` /
   `NewGraphemes` を使う `render.go` / `issues/wrap.go`) は path 例外にする
3. 回帰は「ansi と uniseg が食い違う文字を 1 つ埋めた `dropToColumn` の列不変条件テスト」で pin する。
   変異 = `clusterWidth` を uniseg に戻す、で red になることまで確認すること
