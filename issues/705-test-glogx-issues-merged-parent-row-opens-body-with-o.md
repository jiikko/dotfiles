# 705 (test): issues viewer の統合された親 issue 行で `o` が本文を開くことをテストで固定する

起票日: 2026-10-09

## 概要

glogx の issues viewer では、epic の親 issue（`epic/<name>/<name>-*.md` など。判定は `issues.IsGroupParent`）が親行に統合される。
この行では、`Enter` / `Space` が子リストの開閉になり、本文は `o` で開く（`docs/issues-viewer-spec.md` の「親行の `Enter` / `Space` は…」の段落。2026-09-05 のユーザー要望）。
ところが、**`o` で本文が開くことを確かめるテストが無い**。

## 詳細

- 今あるのは、反対側を確かめるテストだけ:
  - `TestIssuesViewGroupParentActionsAreNoopWithNotice`: **合成の**親行（親 issue の無い group）で `o` を含むキーが何もしない
  - `TestIssuesViewGroupParentIssueMergesIntoHeaderRow`: 統合された親行の `Enter` が本文を開かず、子リストを開く
  - 統合された親行で開くことを確かめるのは、パスのコピー（`TestIssuesViewGroupHeadRowStillCopiesPath`）だけ
- 2026-10-09 に `src/glogx` の `*_test.go` を `handleKey("o"` と `"o"` で grep して数え直した。issues viewer で `o` を押すテストは上の no-op の 1 本だけだった
- 壊れたときに起きること:
  - `openBody` が先頭で `row.kind == displayRowGroup` を断っている（`issues_view.go` の `openBody`）
  - 統合された行が `displayRowGroup` として扱われるように変わったり、`currentIsGroup` 系の判定が `groupHead` を含むように変わったりすると、epic の親 issue の本文を viewer から開く手段が無くなる
  - それでもテストは全部緑のまま通る
- 発端: blog repo の epic 001 で、ユーザーが「epic の親 issue の本文は viewer から開けない」と受け取った。調べると `Enter` が仕様どおり開閉になっていただけで、`o` では開けた
  - 確認は、使い捨ての worktree に置いた一時的なテストで行った
  - テストの issue は `epic/001/001-…` と `002-…`。`fakeEpicIssue` に、実際に本文のファイルを書いた構成にした
  - 結果は PASS。そのテストは worktree ごと消した

## 対応方針

`issues_group_view_test.go` に次のテストを足す。

- 本文ファイルを `t.TempDir()` に実際に書いた親 issue と子 issue で `loadedView` を作る
- 親行が `groupHead` で、`issue == parent` であることを前提として確かめる
- `handleKey("o")` で `v.open == parent` になる
- あわせて、断りの notice が出ないこと、子リストの開閉状態が変わらないことも確かめる

追加したら、変異で red になることを確かめる。当てる変異は、`openBody` の先頭の判定を `groupHead` でも断るように変えるもの。

統合された行を `displayRowGroup` で作るように変える変異は、検知力の確認に使わない。その変異では新しいテストの前提の確認が先に落ちるうえ、既存の `TestIssuesViewGroupParentIssueMergesIntoHeaderRow` が既に `head.kind != displayRowIssue` で落とすので、新しいテストが何を捕まえたのかを示せない（反証レビューの指摘。`issues_view.go` の行の組み立てで確認した）。

## 関連ファイル

- `src/glogx/issues_view.go` — `openBody` / `handleKey` の `"o"` と `"enter"`
- `src/glogx/issues_group_view_test.go` — 既存の group 行のテスト
- `docs/issues-viewer-spec.md` — 親行のキー操作の仕様

## 進捗

- 2026-10-09: 起票
- 2026-10-09: 反証レビューを 1 本（sonnet の読み取り専用）に通した。主張は反証されず。2 つ目の変異が検知力を示さないという P3 の指摘を、コードで確かめて反映した
