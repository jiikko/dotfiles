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

---

## 対応 (2026-08-27)

`clusterWidth` を `uniseg.StringWidth` → **`dispWidth`** に変更し、`width.go` から uniseg の
import を削除した (`ansi.StringWidth` 直呼びでなく `dispWidth` にしたのは、fast-path を通せて
ASCII が速くなるため。両者が全単一 rune・2-rune 組み合わせで等価なことは敵対的レビューが総当たりで確認)。

### 二重の防御

| 手段 | 何を止めるか |
|---|---|
| `width_test.go:TestNoSecondWidthEngine` | シンボル名の走査。`uniseg.StringWidth` / `ansi.StringWidthWc` / uniseg を import したファイル内の `.Width()` |
| `.golangci.yml` の depguard `uniseg-split-only` | **import ごと禁止** (別名 import もここで止まる)。分割が要る 4 ファイルだけ path 例外 |
| `render_test.go:TestDropToColumnWidthInvariantWhereEnginesDisagree` | 列不変条件そのもの。ここが本命の防御 |

### 敵対的レビューで直した 4 点

1. **理由づけが誤っていた**。当初コメントは「x/ansi = 描画エンジンの幅モデルだから」と書いたが、
   v2 の既定は WcWidth で**この層とは既に食い違っている** (width.go 自身が記録済み)。
   正しい理由は「**幅の出典を 1 本にする**」なので書き直した
2. **検査が 4 通りで迂回できた**。特に `issues/wrap.go` の `dispWidth(c)` を `g.Width()` に
   変えるだけで全テスト green のまま uniseg へ戻る経路があった → 塞いで red を確認
3. **列不変条件テストが straddle 分岐 (全角境界) と SGR 経路を通っていなかった**。
   全角埋めを丸ごと削っても green だったので、全角入りと SGR 入りを足して red を確認
4. **走査の除外が緩かった** (`tools` を深さ問わず除外・`width_test.go` で終わる任意のパスを除外)

### 変異検証 (すべて red を確認)

| 変異 | 結果 |
|---|---|
| `clusterWidth` を uniseg に戻す | 列不変条件・単一エンジン検査とも red |
| `wrap.go` を `g.Width()` に変える | red (修正前は全テスト green だった) |
| `ansi.StringWidthWc` に変える | red |
| straddle の空白埋めを削る | red (修正前は green だった) |
| 別名 import (`useg "…/uniseg"`) | depguard が検出 |

### 性能 (dropToColumn。毎フレーム走る経路)

| 入力 | 修正前 (uniseg) | 修正後 (dispWidth) |
|---|---|---|
| ASCII | 432 ns/op | **300 ns/op** |
| CJK | 1022 ns/op | 1043 ns/op |
| 絵文字 | 1089 ns/op | 1151 ns/op |

alloc はいずれも 0 のまま。ASCII は fast-path が効いて改善、CJK/絵文字は誤差範囲。

### 積み残し → [issue 124](124-bug-glogx-split-engines-and-width-model-mismatch.md)

**列不変条件は 0 にはなっていない (6720 件 → 757 件)。** 幅エンジンは 1 本になったが
**分割器が 2 本ある** (uniseg=Unicode 15 / x/ansi=16) ため。加えて「揃える先が描画エンジンと
ずれている」問題 (2115 件、`dispWidth` の全経路に効く) も残る。どちらも 112 の範囲外。
