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

~~**非テストで 17 箇所**~~ → **数え直して 21 箇所** (2026-09-14 に着手時に再勘定)。
コメント行を除いたコード実体で `src/glogx/doctor_view.go` = 12 / `src/glogx/doctor_delete.go` = 9。
母集合は `src/glogx/*.go` の非テスト `.go` を
`grep -nE '"disk:"|"diskitem:"|"brewact:|\\x00|diskItemKey'` で走査した全件 (ヒット 29 行のうち
コメント 4 行と、無関係な `\x00` の用途 4 行 = `worktree_status.go` の -z パース /
`status_view.go` の行キー / `issues_watch.go` を除いた数)。
内訳は「接頭辞を付けて作る」「`CutPrefix` / `Cut` で剥がす」「`HasPrefix` で種別を見る」の 3 形。

🚨 **prefix の種類も表より多い。実際は 10 種**: `disk:` / `diskitem:` / `diskfail:` / `svc:` /
`svcundiagnosed:` / `brew:i:summary` / `brewact:i:j` / `docker:` / `dockeritem:` / `dockerprune`
(`doctor_docker.go` の 3 種が表から漏れていた)。名前空間は表のとおり 4 つで正しい。

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

## 進捗 (2026-09-14)

- [x] named type の導入 — **3 型ではなく 4 型**にした。issue 自身の表が 4 つ目
      (コマンド文字列 = `selectedActions` / `actionByCmd`) を挙げているのに修正方向から漏れていた。
      ここは同じ `r` から `r.key` (行) と `r.copyPath` (コマンド) の 2 種を読む箇所があり、
      **取り違えの発火条件としては最も近い**ので落とせない
      → commit `refactor(glogx,371): doctor の行 key / エントリ ID / item key に名前付き型を入れる`
        / `refactor(glogx,371): 4 つ目の名前空間 (コマンド文字列) に cmdKey を入れる`
- [x] `rowKey` の生成・分解を 1 箇所へ寄せる — `src/glogx/doctor_keys.go` に 10 種のコンストラクタと
      分解を集約。`parentKeyOf` は `parentRowKey` として同ファイルへ移動、`diskItemKey` と
      `isDockerRowKey` も集約した
- [x] **迂回を機械で止めるか判断する → 不要**。372 と状況が違う: あちらが AST 走査テストを
      要したのは ruleguard が跨モジュールの型を解決できず**型で守る手段that自体が無かった**ため。
      371 は `map[rowKey]bool` にした時点で **`go build` がガードになる** (別の名前空間のキーを
      渡すと compile error)。2 本目の走査ガードは作らない
      - 🚨 **射程を凍結する**: 型が止めるのは *map の索引と代入の取り違え*まで。
        `v.expanded[rowKey("disk:"+string(id))]` のような**明示変換での迂回は止まらない**。
        そこは塞ぎに行かない (脅威モデルは `doctor_keys.go` の冒頭に書いた)
- [x] `parentRowKey` にテストを追加 — 旧 `parentKeyOf` は**テストが 1 本も無かった**
      (`collapseTargetAtCursor` / `collapsibleAtCursor` も直接のテスト無し)。
      disk / docker の両分解、分解できない形 (prefix だけ一致)、ID とパスに `:` が混ざる形を固定

## 結果 (実測)

- 型を入れた後の**コンパイルエラーは 4 件だけ**で、すべて map の初期化 (`map[string]bool{}`) だった。
  = **既存 21 箇所はすべて正しく使われていた**ことが機械で確定した (issue の「現時点で実際に
  取り違えている箇所は見つかっていない」を、grep でなくコンパイラで裏取りできた)
- `go test ./...` rc=0 / `make lint` 0 issues (版固定の golangci-lint v2.5.0) /
  repo root の `make test` rc=0 (`[derived-fields] … OK` も出ており、372 のガードも通っている)
- **射程を実装と突き合わせた結果** (ヘッダに書いた「検出しない形」は着手前の意図なので測り直した):
  - production に残る生の prefix 組み立ては **0 箇所** (`doctor_keys.go` の外にヒット無し)
  - production の `rowKey(...)` 明示変換も **0 箇所**。行 key の名前空間はコンストラクタで閉じている
  - 明示変換が残るのは `cmdKey(...)` 5 箇所 (コマンド文字列の境界。`copyPath` が二重の意味を
    持つので意図的に見えるようにしている) と `entryID(e.ID)` 1 箇所 (`disk.DeleteReport` の境界)
- 新規テストの変異検証 **5 本すべて RED** (repo 外のコピーで実施。各変異はビルド成功と diff を
  確認してから red/green を読んだ):
  | 変異 | 落ちた assert | 予測と一致 |
  |---|---|---|
  | docker の群を切り出さず `rest` 全部を使う | `docker_の候補は群を親に持つ` ほか 3 | ✓ |
  | `itemKey` の NUL 必須を落とす | `diskitem_で_\x00_が無い` のみ | ✓ |
  | `belongsTo` の NUL を落として前方一致だけにする | `TestItemKeyEntryID` | ✓ |
  | disk の親を docker の構成子で組む | `disk_の対象パスはエントリを親に持つ` ほか 2 | ✓ |
  | `diskItemKey` の区切りを `:` にする | 両テスト | ✓ |
  - 🚨 変異 2 本は**ビルド不能** (変数が未使用になる) で第 3 の結果として扱い、当て直した

## 残タスク

- (なし)
