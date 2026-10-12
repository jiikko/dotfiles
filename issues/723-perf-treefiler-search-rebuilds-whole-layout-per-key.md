# 723 (perf): treefiler の検索が打鍵のたびに木全体の配置を作り直して並べる (5 万件で 1 打鍵 約 24ms)

起票日: 2026-10-12

## 概要

`/` の検索は 1 打鍵ごとに `refreshMatches` → `column(s.origin)` を呼ぶ。`column` は木全体の配置 (`layoutAll()`。仮想化しない全部の配置の map) を作り、
そこから同じ深さの項目を集めて `sortByY` (`sort.Slice`、比較のたびに map を引く) で並べ直す (`src/treefiler/filer/search.go`)。列 1 本のために
木全体を配置し、配置の map の順 (ばらばら) から並べ直しているので、件数に比例する以上に重い。`tab` `↑` `↓` の `stepMatch` と `n` `N` の
`repeatSearch` も毎回 `column` を作り直す。

## 実測 (2026-10-12、Apple M3 Max・go1.26.0・画面 200x50)

1 フォルダにファイル n 件。`/` を開いた後の「backspace + 1 文字」(= `refreshMatches` 2 回) の所要。別 module から filer を呼ぶベンチ
(作り捨て。`tmp/` に置いたもので repo には無い。下の「測り方」を再現の手順として残す):

| n | ns/op (打鍵 2 回) | 1 打鍵あたり | B/op | allocs/op |
|---|---|---|---|---|
| 5,000 | 3.46ms | 約 1.7ms | 3.1MB | 6,493 |
| 50,000 | 47.2ms | 約 24ms | 30MB | 67,813 |

- 件数 10 倍で時間 13 倍 (線形より少し悪い。sort の n log n と map の参照)
- pprof (n=50,000): `HandleKey` の配下の 84% が `column` (うち `sort.Slice` 75%・map の参照 35%。重なりあり)、残りが `fuzzy` (候補ごとに `[]ch` を確保)
- `stepMatch` / `repeatSearch` の所要は**未実測** (同じ `column` を作るので同じ桁と読んだだけ)
- 測り方: `filer.New(root)` → `Resize(200, 50)` → `Advance` を 200 回 → `HandleKey("/")` `HandleKey("9")` → 計測区間で `HandleKey("backspace")` `HandleKey("9")` を繰り返す
  (`go test -bench -benchmem -benchtime 20x`)。既存の `perf_bench_test.go` (`BenchmarkViewHugeFolder` 等) の fixture に検索の版を足せば repo の中で再現できる

## 対応方針 (案)

1. 列を、配置の map ではなく見せる子の覚え (`kids.go` の `visKids`。並びが確定し、各子に順位が振られている) から作る。`layoutAll` と `sortByY` が要らなくなる。
   🚨 `column` は「同じ深さ」の項目を集めている (親が違う兄弟の列も含む)。`visKids` は親ごとなので、同じ深さで開いている親を上から辿って連結する形になる。
   今の結果 (dotfile を隠す・カーソルへの経路は見せる、の例外を含む) と同じ並びになることを、今の `column` を正解役にした突き合わせのテストで固定してから差し替える
   - 🚨 `layoutAll` は列を返すだけでなく配置の副作用を持つ (`layoutWith` の `layoutSeq++`・`resetVirt`・`noteGeo` の `geoPrev` / `openHold`・巨大なフォルダの `virtSeq`。
     `layout.go` のコメント「検索の layoutAll でも履歴を続ける」)。検索の打鍵で `layoutAll` を呼ばなくなると、`noteGeo` の「前回の配置の次か (`prev.seq != layoutSeq-1`)」の
     判定と、全部を動かす期間の起動が変わりうる。差し替える前に、検索の前後で仮想化の等価性のテスト (`virt_test.go`。乱数の操作列に `/` と打鍵がある) と
     `TREEFILER_VIRT_SEEDS=300` が通ることを確かめる
2. 検索中は列を 1 回作って持つ (打鍵のたびに作らない)。🚨 古くなる経路が 2 つある: ライブ更新で列が変わる (issue 717 の `restoreOrigin` と合わせ、消えた項目を指さない) と、
   検索で一致へ移るたびにカーソルへの経路が変わって「見せる子」が変わる (`kids.go` の `visible` は経路の上の dotfile を見せる)。後者があるので、持つなら
   見せる子の覚えの世代を鍵にする。1 だけで十分速ければ 2 はやらない
   - 小さいもの: 空欄になった `refreshMatches` は一致が必ず空なので列を作らずに返せる。`stepMatch` の `inCol` と `repeatSearch` の `idx` の map も列ごとに作っている
3. `fuzzy` の確保を減らす (名前ごとの `[]ch` を作らない。ASCII の名前は byte のまま比べる 等)
- 効きの見積もり (見積もり。未実測): 1 で sort が消え列の作成が O(列の長さ) になるので、5 万件の 1 打鍵は数 ms 以下に縮むはず。直したら同じ測り方で before / after を本文に書く
- 🚨 712 の仮想化との関係: 検索が `layoutAll` (仮想化しない配置) を使うのは「画面の外の項目も検索の対象」にするため (`column` のコメント)。`visKids` から作っても
  画面の外の項目は含まれるので、この性質は保てる

## 関連

- issue 691 (sortByY の O(n²) の挿入ソートを `sort.Slice` に直した。今回はその先の「そもそも並べ直さない」)
- issue 712 (仮想化。`visKids` を作った) / issue 717 (検索の起点が消えたときの戻り先)
- issue 714 (ディレクトリを読む I/O の N+1。別の層)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (sonnet・読み取りのみ) で訂正: `layoutAll` の副作用 (配置の履歴・全部を動かす期間) を方針に足した / 列を持つなら経路で見せる子が
  変わることも鍵にする / 空欄と map の小さいもの。反証できなかったもの: `column` は同じ深さを木全体から集める / 親を y 順に辿って `visKids` を連結すれば同じ並び
  (例外は `visible` 1 か所で共通) / 実測の再現 (5,000 件 3.51ms・50,000 件 48.6ms) / 1 打鍵 = `refreshMatches` 1 回 (ユーザー依頼「計算処理が高くて最適化する余地のある箇所を探して issue にして」。CPU のプロファイルの調査は sonnet、数字は main が走らせ直して再現)
