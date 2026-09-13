# 371 refactor: doctor の行 key / エントリ ID / item key が全部生の string で、取り違えが silent

- 起票: 2026-09-13
- 種別: `refactor` (挙動は変えない。型で境界を作る)
- 出典: audit skill に足した監査タイプ `encapsulation` (知識の帰属) の動作確認を glogx で回して出た 3 件目。
  分類は **E3 生の値の越境**
- 同時に出た E2 2 件は「`disk.Result.WithItems` / `disk.Report.WithResults` へ寄せる」で対応済み (別 commit)

## 何が漏れているか

`doctorView` が持つ選択・展開の map は**すべて `map[string]bool`** で、キーの名前空間は規約だけで
区別されている。規約を知っている箇所が呼び出し側に散っており、所有者 (キーを作る型) が居ない。

| map | キーの意味 | 生成 |
|---|---|---|
| `selected` | エントリ ID | `disk.Entry.ID` そのまま |
| `selectedItems` | エントリ + パス | `diskItemKey(id, path)` = `id + "\x00" + path` |
| `selectedActions` / `actionByCmd` | **コマンド文字列** | `row.copyPath` |
| `inspected` | エントリ ID | 同上 |
| `expanded` | **行 key** | `"disk:" + id` / `"diskitem:" + itemKey` / `fmt.Sprintf("brewact:%d:%d", i, j)` |

## 散在の実数

**非テストで 17 箇所**（`src/glogx/doctor_view.go` = 11 / `src/glogx/doctor_delete.go` = 6）。
母集合は `src/glogx/*.go` の非テスト `.go` を
`grep -nE '"disk:"|"diskitem:"|"brewact:|\\x00|diskItemKey'` で走査した全件。
内訳は「接頭辞を付けて作る」「`CutPrefix` / `Cut` で剥がす」「`HasPrefix` で種別を見る」の 3 形。

## 発火条件と壊れ方

- **新しい呼び出し側が増えたとき**に、別の名前空間のキーを渡しても **compile は通る**
  (`v.selected[rowKey]` / `v.expanded[entryID]` は型が同じ)
- 壊れ方は例外ではなく **map miss = 無言**。選択が畳まれない / 展開が効かない / 二重に数える、の形で出る
- 既に同じ blast radius の事故が記録されている: `selectionSummary` の
  「🚨 エントリ数で数えない: hint は 1 件、確認は 3 件になる (敵対レビュー 2026-09-03)」。
  原因は別 (数える単位) だが、**表に出る症状は同じ**
- 🚨 **現時点で実際に取り違えている箇所は見つかっていない** (17 箇所すべて正しい)。これは
  「今壊れている」ではなく「**次に足す人が silent に壊せる**」という発見

## 反証の試み

- `doctor_view.go:1226` の `diskItemKey` に「パスに `\x00` は現れない」とコメントがあり、キーの
  合成は意図的。**ただし「string のままにする」ことの理由はどこにも書かれていない**
- `selectedActions` は「行の key で持たない」理由がコメントに明記されている (再スキャンで並びが
  変わると別の手を指す)。これは**キーの選び方**の判断で、型を付けない判断ではない
- `_claude/rules/` と `docs/` に「glogx では ID を named type にしない」という取り決めは無い

## 修正方向

`src/glogx` に named type を入れて、コンパイラに名前空間を守らせる:

```go
type entryID string        // disk.Entry.ID
type itemKey string        // entryID + "\x00" + path
type rowKey string         // "disk:" / "diskitem:" / "brewact:i:j"
```

- 生成・分解を 1 箇所 (`rowKey` のコンストラクタと `parse`) へ寄せ、`map[entryID]bool` などに変える
- 影響は ~20 箇所。**挙動を変えないこと**が受け入れ条件なので、既存テストが全部緑のまま通ることで担保する

## 残タスク

- [ ] named type の導入 (未着手。上記 3 型)
- [ ] `rowKey` の生成・分解を 1 箇所へ寄せる (未着手)
- [ ] 寄せた後、迂回 (生の string から直接組む形) を機械で止めるか判断する — 参照: issue 372
