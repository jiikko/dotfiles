# 843 (refactor): glogx のテストの手組みの重複 (グローバルの差し替え・AST 走査の足場など)

起票日: 2026-10-19

## 概要

2026-10-19 の glogx 全体監査 (844) の test-helpers。母集合は `*_test.go` 161 ファイル (`func Test*` 1252 本)。
件数は grep と go/ast による機械集計 (監査担当の申告。main agent は抜き取りで照合した)。

## 詳細

1. **グローバル変数の差し替えと復元の手組み** (P2): `t.Cleanup(func() { X = orig })` の形が 19 ファイルに 55 か所 (`tui_actions_test.go` 21・`cli_health_test.go` 12 など)。`swapClaudeVersionFetchers` / `swapCodexVersionFetchers` が個別に同じことをしている。`testenv` に generics の `Swap[T any](t, &v, new)` を置く
2. **本番の Go ファイルを走査する AST 検査の足場の複製** (P2): `cache_write_discipline_test.go` / `vs16_literal_test.go` / `details_flags_test.go` / `width_test.go` (`TestNoSecondWidthEngine`) / `internal/issuesview/issues_rows_setter_test.go` の 5 か所が、`tools` / `testdata` を飛ばす → 非テストの `.go` → `ParseFile` → 件数の下限、を同じ形で持つ。既存の `walkOwnSources` (`own_sources_test.go`) と `mainNonTestFiles` (`structure_ratchet_test.go`) は一部でしか使われていない。1 つに寄せ、下限を実件数から決める (下限が遠いことは 842 の A6)
3. **hint が予算に収まるかを全幅で見るループの複製** (P3): `issues_view_test.go:TestIssuesHintUsesRenderBudget` / `status_view_test.go:TestStatusHintUsesRenderBudget` / `hint_width_test.go:TestDiffHintUsesRenderBudget` が「画面を準備する」1 行以外同形。`assertHintFitsAtEveryWidth(t, setup)` に寄せる
4. **GraphQL の成功応答の組み立て** (P3): `internal/gitdata/github_test.go` の `TestFetchCIStatusesChunksInParallel` と `TestFetchCIStatusesChunkPartialFailure` に同一のループ (約 12 行)。runner の手組みも 8 か所
5. **文字列包含の検査ループ** (P3): `for _, want := range []string{...} { if !strings.Contains(...) }` が 22 ファイルに 58 か所。`AssertContainsAll` / `AssertNotContains` を `testenv` に
6. **実 git の `exec.Command("git", ...)` の手組み** (P3): 9 ファイルに 22 か所。`testenv.GitInRepo` で置き換えられるものが多い。`git_harden_test.go` は git の hardening を素の exec で試している可能性があるので、置き換えの前に確かめる (**未確認**)
7. **823 の後も root と internal に残る同名・同内容の helper** (P3): 同名の定義が 12 名。`pagerShown` と `drainScanMsg` は逐語の複製で寄せられる
8. **小さな複製** (P3): `doctor_delete_test.go` の `slicesContains` は `slices.Contains` と同じ (2 か所)

## 進捗

- [ ] 1 〜 8 (trigger: その helper を使うテストを次に足すとき。1 と 2 は単独で寄せてよい)
