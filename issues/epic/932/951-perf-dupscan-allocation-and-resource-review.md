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

## 守ること

- **出力を変えない**。直す前後で `report -limit 0 -format json` / `sweep` / `gate` の出力が byte 同一であることを、`src` で確かめる。
- 効果は `go test -bench` の前後（同じ条件で複数回）で、確保量と所要を記録する。測れなければ「未実測」と書く。

## 受け入れ条件

- [ ] P1・P2 を直し、前後の確保量・所要を表で記録する。
- [ ] 出力が前後で同一であることを確かめる (`report` / `sweep`)。
- [ ] P4 を直すか、直さないと決めて理由を書く (節 3 の既定の上限を 8000 に上げたため、遅さが効く)。
- [ ] P3・P5 は、P1・P2 の後の実測を見て採否を決める。

## 関連

- [932](932-epic-dupscan-accuracy.md) (epic)、[948](948-research-dupscan-meets-its-aims.md) (狙いの判定。context と待ちの観点)、[946](done/946-feat-dupscan-recall-from-real-modules.md) (`sweep` 節 3 の所要)
