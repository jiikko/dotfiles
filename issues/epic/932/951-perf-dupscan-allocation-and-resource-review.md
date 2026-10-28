# 951 (perf): dupscan のコードを読んだ結果 — リソースリークは見つからず、正規化の確保が大きい

起票日: 2026-10-28

> 反証レビュー未実施 (起票時点。数字は 2026-10-28 に `go test -bench` と pprof で取った 1 台 (Apple M5・10 コア) の値)。

## 概要

dupscan のコードを、リソースリークと性能の観点で読んだ。**リークは見つからなかった**。性能は、`report` が `src` (単位 8705・ファイル 1269) で 1 回あたり **135.6 ms・376.9 MB・682 万回の確保**で、確保の約 3 割が `norm` (正規化) の使い捨てだった。直しやすい 2 点を先に直す。

## リソース面の読み (リークの有無)

| 対象 | 確かめたこと | 結果 |
|---|---|---|
| tree-sitter (Swift) の parser・tree・cursor | `Parser.Close` / `t.Close()` (defer) / `cursor.Close()` (defer)、worker ごとに `defer p.Close()` | 閉じている |
| `parseAll` の goroutine | worker は CPU 数だけ。`close(jobs)` と `wg.Wait()` で全部回収 | リークなし |
| `git cat-file --batch` (`Commit.ReadAll`) | `WithTimeout` + `defer cancel()`、読みかけで止める経路は `cancel()` の後に `Wait()`、成功時も `Wait()` | プロセスは残らない |
| ファイル handle | `os.ReadFile` (自前で閉じる)、`report/gate.go` の `f.Close()` | 閉じている |
| 巨大な保持 | `scanned.contents` が全ファイルの中身を最後まで保持する | リークではないが、`report` / `gate` は parse の後に使わない (`src` で約 13 MB。Swift の大きい repo では未測定) |

- **リークではないが気づいたこと**: `parseAll` は 1 つのファイルが失敗しても、残りを全部 parse し終えるまで止まらない (失敗は集計で初めて返る)。失敗は稀なので、効果は小さい。

## 性能の実測 (`report -limit 0`、dotfiles の `src`)

| 指標 | 値 |
|---|---|
| 1 回あたり | 135.6 ms / 376.9 MB / 6,821,205 回の確保 |
| CPU の内訳 (累積) | Go の parse 約 26% / `norm.Normalize` 約 16% / 確保・GC まわり (`mallocgc`・`memclr`・`madvise`) が大きい |
| 確保の上位 (`alloc_space`) | `norm.markRefs` 343 MB (10.5%) / `bufio.NewWriterSize` 326 MB (10.0%) / `norm.markActions` 317 MB (9.7%) / `golang.node` 301 MB (9.2%) / `golang.name` 148 MB (4.5%) |

## 候補 (効果の大きい順。まだ直していない)

| # | 内容 | 期待 | リスク |
|---|---|---|---|
| P1 | `norm.normalize` の `bufio.NewWriterSize(h, 4096)` を、単位ごとに作らず使い回す (`sync.Pool`) | 確保 約 10% 減 | 低い。出力の hash は変わらない |
| P2 | `markRefs` / `markActions` が単位ごとに作る `map[*tree.Node]bool` を、使い回す (`sync.Pool` + `clear`) | 確保 約 20% 減 | 低い。節の旗は単位ごとの局所の名前に依存するので、`Node` に直接持たせない |
| P3 | `golang.node` / `name` / `conv` の確保 (AST の変換) | 確保 約 15% | 中。構造に触る。今回は対象外 |
| P4 | `sweep` 節 3 の総当たり (6128 単位で 約 4.5 秒) を、近い順に絞る (転置索引など) | 数倍 | 中。結果が変わらないことの確認が要る |
| P5 | `parseAll` が失敗後に残りを読み続ける | 小さい | 低い |

## 対応と実測 (2026-10-28)

同じ条件 (dotfiles の `src`、`go test -bench`、同じ機械で前後を各 3 回) の値。**出力は前後で byte 同一** (`report -limit 0 -format json` と `sweep -similar-max-units 9000 -limit 0` を `cmp` で確認。直す前の版も 2 回の実行で一致 = 決定的)。

| 指標 | 前 | 後 |
|---|---|---|
| `report` の確保量 | 376.9 MB/op | **262.8 MB/op (-30%)** |
| `report` の確保回数 | 6,821,205 | 6,690,6xx〜6,690,756 (-2%) |
| `report` の所要 | 145〜153 ms | 131〜139 ms (負荷が低い間に前後を各 3 回。その後 load average が 7 に上がると同じ版が 167〜243 ms まで揺れた。**時間の改善は断定しない**。確保量は揺れない) |
| `sweep` の所要 | 回帰を含む 7.5 秒 → 直して 5.0 秒 (差が大きく、負荷の揺れより確か) | 3.5〜4.0 秒 (負荷の揺れの内側かもしれない。大きく変わるのは bag の歩き方を替えた分の理屈上の削減) |

- **P1・P2 (実施)**: `norm.normalize` の作業領域 (節の旗の表 2 つと 4 KB の書き出し buffer) を `sync.Pool` で使い回す。巨大な単位が育てた表は pool に戻さない (`scratchKeepMax`)。確保量は 30% 減るが、確保**回数**はほとんど減らず、所要の改善は小さい。
- **P4 (一部実施)**: `SubtreeBag.Dice` が小さい方の bag を歩かず、毎回両方の合計も数えていた。`Shared` (小さい方だけ歩く) と `Total` に分け、`sweep` は bag ごとの合計を 1 度だけ数えて使い回す。単位 6128 の総当たりが 5.0 秒 → 3.5〜4.0 秒 (負荷で揺れる測定なので、削減幅は断定しない)。bag を sorted slice にして merge する案は未実施 (まだ 3.5 秒が `Shared` の map 参照)。
- **自分で入れた回帰の発見**: `sweep` に先日足した許可印の判定 (`ex.Allowed`) を、対の総当たりの内側で毎回呼んでいた。確保が **4.5 GB・83M 回**で、`sweep` が 5.0 → 7.5 秒に遅くなっていた。単位ごとに 1 度だけ引く形に直し、**確保は 365 MB・8.4M 回**に戻った。許可印を足したときに、総当たりの内側のコストを測っていなかった。
- **ベンチ**: `bench_test.go` (`DUPSCAN_BENCH_DIR` が無ければ skip) を足した。次の性能の変更で同じ条件を再現できる。
- **P3・P5**: 未対応 (P3 は構造に触る。P5 は効果が小さい)。リークは見つからなかった。

## 守ること

- **出力を変えない**。直す前後で `report -limit 0 -format json` / `sweep` / `gate` の出力が byte 同一であることを、`src` で確かめる。
- 効果は `go test -bench` の前後（同じ条件で複数回）で、確保量と所要を記録する。測れなければ「未実測」と書く。

## 受け入れ条件

- [x] P1・P2 を直し、前後の確保量・所要を表で記録する (上の「対応と実測」)。
- [x] 出力が前後で同一であることを確かめる (`report` / `sweep`。`cmp`)。
- [x] P4 を一部直した (`Shared` / `Total`。5.0 → 3.5〜4.0 秒。負荷で揺れる)。sorted slice で merge する案は、まだ遅さが問題になるまで見送る (理由: 節 3 は `-similar-max-units` で止められ、既定は `sweep` の入口だけ)。
- [ ] P3・P5 は、P1・P2 の後の実測を見て採否を決める。

## 関連

- [932](932-epic-dupscan-accuracy.md) (epic)、[948](948-research-dupscan-meets-its-aims.md) (狙いの判定。context と待ちの観点)、[946](done/946-feat-dupscan-recall-from-real-modules.md) (`sweep` 節 3 の所要)
