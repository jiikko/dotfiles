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
2. 検索中は列を 1 回作って持つ (打鍵のたびに作らない)。ライブ更新で列が変わったときの扱いは issue 717 の `restoreOrigin` と合わせる (消えた項目を指さない)
3. `fuzzy` の確保を減らす (名前ごとの `[]ch` を作らない。ASCII の名前は byte のまま比べる 等)
- 効きの見積もり (見積もり。未実測): 1 で sort が消え列の作成が O(列の長さ) になるので、5 万件の 1 打鍵は数 ms 以下に縮むはず。直したら同じ測り方で before / after を本文に書く
- 🚨 712 の仮想化との関係: 検索が `layoutAll` (仮想化しない配置) を使うのは「画面の外の項目も検索の対象」にするため (`column` のコメント)。`visKids` から作っても
  画面の外の項目は含まれるので、この性質は保てる

## 関連

- issue 691 (sortByY の O(n²) の挿入ソートを `sort.Slice` に直した。今回はその先の「そもそも並べ直さない」)
- issue 712 (仮想化。`visKids` を作った) / issue 717 (検索の起点が消えたときの戻り先)
- issue 714 (ディレクトリを読む I/O の N+1。別の層)

## 進捗

- 2026-10-12: 起票 (ユーザー依頼「計算処理が高くて最適化する余地のある箇所を探して issue にして」。CPU のプロファイルの調査は sonnet、数字は main が走らせ直して再現)
