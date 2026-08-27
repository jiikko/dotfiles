# 114 bug: status viewer のプレビューキャッシュが `r` でも開き直しでも捨てられない

起票日: 2026-08-27 / 出典: leaky-abstraction 監査 (L5 暗黙契約) / priority: medium

## 事実

`src/glogx/line_cache.go:lineCache` の doc は「状態 3 点と手順を型に閉じ込める」ことだけを述べ、
**キーが内容を一意に決めること**を要求していない。3 つの caller のうち 2 つは契約を満たすが、
1 つは満たさない:

| caller | キー | 内容不変か | `reset()` されるか |
|---|---|---|---|
| `diffOverlay` | SHA | ✅ | ✅ `tui.go` の pull 後 |
| `jobDetailOverlay` | `SHA/cursor` | ✅ | ✅ 同上 |
| **`statusView.preview`** | `section + XY + path` | ❌ **内容が変わってもキーが動かない** | ❌ **呼び出し元 0 件** |

`reset()` の呼び出しは 5 箇所あり、すべて `diffOv` / `detailOv` / `prStatusOv` 向け (grep で確認)。

## 発火条件と壊れ方

すでに ` M` (unstaged modified) の行でプレビューを出した状態で、**同じファイルを外部で編集し直す**
(XY が ` M` のまま動かない)。以後そのプレビューは編集前の diff を出し続ける。

**silent**。プレビュー欄は正常に見え、内容だけが古い。

## 「自動更新で取り直さない」は意図的だが、範囲が違う

`status_view.go:receive` にはこの問題を**名指しした**コメントがある:

> 内容が変わったら古い diff は捨てる (キーに XY を含めているので大半は当たらないが、
> **同じ XY のまま中身だけ変わる編集 = 保存し直しでは当たってしまう**)

ただしその `clearEntries()` は `if !changed { return nil }` の**後ろ**にあり、`changed` は
section/path/XY の比較なので、**まさにそのケースでは到達しない**。

`status_view_test.go:TestStatusReceiveSchedulesPreviewRefetchOnChange` が
「内容が変わっていないのにキャッシュを捨てた」と assert しており、
**1 ポーリング分の据え置きは意図として pin されている** (毎 1.5 秒 `git diff` を走らせないため)。
→ **自動更新で取り直さないのは設計どおり**。

**しかし spec も README も「`r` (再読込) と viewer の閉じ→開き直しまで据え置く」とは言っていない。**
`finishClose()` は `preview.clearBusy()` しか呼ばず `entries` を残すため、
プロセスが生きている限り古い diff が消えない。

## 対応 (2 案)

- **最小**: 据え置きを自動更新だけに限る = `r` の経路 (`loadCmd` → `receive`) と `finishClose()` で
  `preview.clearEntries()` する
- **構造で閉じる**: `lineCache` の doc に**キー契約**(「キーは内容を一意に決めること」) を書き、
  `previewKey` に内容の指紋 (mtime+size か `git diff` 結果のハッシュ) を混ぜる。
  キー契約を書いておけば 4 人目の caller が同じ穴を掘らない
