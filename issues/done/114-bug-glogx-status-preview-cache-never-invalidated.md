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

---

## 対応 (2026-08-27)

**「最小修正 + 契約の明文化」**を採った。構造案 (キーに内容の指紋を混ぜる) を採らなかったのは、
`previewKey` が**描画経路でも呼ばれる**ため — mtime/size を混ぜると毎フレーム syscall になる。

### 無効化の契機

| 契機 | 変更前 | 変更後 |
|---|---|---|
| 自動更新 (1.5 秒ポーリング) | 据え置き | **据え置き (意図的。変えない)** |
| `r` (明示的な再読込) | 据え置き | `gen++` → `clearEntries` → 取り直しを予約 |
| 閉じる → 開き直し | 据え置き (永久) | `clearEntries` (`gen++` は元からあった) |

`lineCache` の doc に**キーの契約**を書いた (「キーは内容を一意に決めること。決められない
caller は内容が変わりうる契機で自分で `clearEntries` する」)。3 者のうち diff と job 詳細は
契約を満たし、`statusView.preview` だけが満たさない、という非対称も明記した。

### ⚠️ 敵対的レビューが「修正が目的を達していない」ことを示した (P1)

**`statusPreviewMsg` は 4 兄弟で唯一 `gen` を持っていなかった** (`statusLoadMsg` /
`statusPollMsg` / `statusPreviewTickMsg` は全部持つ)。`tui.go` は世代も `visible()` も見ずに
`receivePreview` を呼ぶので、

```
取得が飛ぶ → 閉じる (clearEntries) → 飛んでいた取得が着地 → キャッシュが復活
→ 開き直し → begin() が has(key) で false → 取り直しが一度も走らない → 編集前の diff が永久に出る
```

**レース窓は狭くない**: `git diff` 4〜7ms + 色付け (5000 行で 211ms)。`r` も同じ理由で負ける
(`clearEntries` は札を残すので、古い結果が着地して `r` の予約が `begin()` に弾かれる)。

**塞ぎ方**: `statusPreviewMsg` に `gen` を足し、`receivePreview` で世代違いを捨てる
(他の 3 兄弟と同じ形)。`r` でも `gen++` する。捨てるときは **`lineCache.cancel` で札を必ず降ろす**
(残すと `begin()` がそのキーを永久に弾く)。

レビューが提案した「`lineCache` に無効化エポックを持たせる」案は採らなかった:
共有型の `begin`/`store` の署名が変わり、この問題を持たない他 2 caller (キーが内容不変) まで
巻き込むため。**この file 内の既存パターン (兄弟メッセージの `gen`) に揃える方を選んだ。**

### 敵対的レビューで塞いだテストの穴 3 つ

| 穴 | 内容 |
|---|---|
| (a) | `cmd != nil` に判別力が無く、`r` から `loadCmd` を落とす変異が green だった → `v.loading` で見る |
| (b) | `clearEntries` を `reset` に変える変異が green だった (doc が名指しで禁じている二重取得が起きる) → 札が残ることを pin |
| (c) | fixture が 1 行で「全部捨てる」と「カーソル行だけ捨てる」を区別できなかった → 2 行に |

加えて、隣の `clearBusy` も守った (`clearEntries` を隣に足したことで
「2 行まとめて `reset()` で」という編集が誘発されやすくなったが、片方だけ消す編集は無音で通る)。

### 変異検証 8/8 red

gen ガードを外す / 捨てるとき札を降ろさない / `r` で `gen++` しない / `clearEntries`→`reset` /
`r` から `loadCmd` を落とす / `r` を修正前へ / `finishClose` の `clearEntries` を削る /
同 `clearBusy` を削る / `changed` の早期 return を壊す。

⚠️ 途中で**変異が別の同名箇所に当たっていた**ことに気づいた (`if msg.gen != v.gen {` が
`receive` と `receivePreview` の 2 箇所にあり、先頭マッチを掴んでいた)。
「green のまま」を検知力不足と誤診しかけた。**変異が意図した箇所に当たったかを確認すること。**

### 残課題 (このセッションでは直さない)

- **全画面 pager (`d`) の中では `r` が無反応** (`pagerKeyPress` の switch に `"r"` が無く
  `pagerScrollKey` へ落ちる)。外部編集しても `OLD-DIFF` が出続ける。pager を閉じてから `r` で
  回復するので実害は限定的だが、「明示的な再読込では捨てる」を掲げた以上ここだけ例外なのは穴
