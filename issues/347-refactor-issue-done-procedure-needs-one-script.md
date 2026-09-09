# refactor: issue を `done/` へ移す手順が 3 つに分かれていて、実際に 2 回落とした

起票日: 2026-09-09
カテゴリ: refactor
優先度: 中（**落とすと CI が落ちる**。実測 1 回）
出典: [retro 345](345-retro-issue-backlog-consumption-2026-09-09.md) の気づき 5

## 何が起きているか

issue を `done/` へ移すには **3 つ**が要る:

1. `git mv issues/NNN-*.md issues/done/`
2. `issues/next/NNN-*.md` の **claim symlink を消す**（残すと dangling）
3. 本文の相対リンクを **1 段深く**する（`../_claude/…` → `../../_claude/…`）

**どれも人が覚えている**だけで、機械は「落とした後」にしか気づかせてくれない
（`tests/issues/test_next_links_valid.sh` と `test_issue_links_valid.sh`）。

## 実測 2026-09-09（1 セッションで）

| 落としたもの | 回数 | 結果 |
|---|---|---|
| ② claim symlink の削除 | **2 回**（313 / 324） | 313 の方は **CI の Tests が赤くなった** |
| ③ 相対リンクの深さ | **2 回**（310 / 336 ほか） | ローカルのリンク検査で気づけた |

同じセッションで 12 件を `done/` へ送り、そのうち **4 回**取りこぼしている。
「覚えている」に依存する形が限界に来ている。

## 対応案

**`scripts/issue_done.sh <NNN>` に寄せる。** やること:

- `issues/` 直下（または group の中）から実体を見つけ、対応する `done/` へ `git mv`
- `next/` に同名の symlink があれば `git rm`
- 本文の `](../` を移動後の深さへ合わせて書き換える（**深さは移動先から算出する**。
  `issues/done/` は 2 段、`issues/epic/<name>/done/` は 4 段）
- 最後に `test_issue_links_valid.sh` と `test_next_links_valid.sh` を回して、
  **失敗したら移動を戻す**（半端な状態で終わらせない）

🚨 **入口のドキュメントを同じ変更で更新する**
（[`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md)）:
`issue-sync` skill の手順、`claim-issue-in-next-and-push.md` の「完了したら目印を消してから
done へ移す」、`issues/README.md`。**ヘッダコメントは入口に数えない**。

## 受け入れ条件

- [ ] `scripts/issue_done.sh <NNN>` が 3 つを 1 コマンドで行う
- [ ] **失敗時に移動を戻す**（リンク検査が落ちたら元の位置へ）
- [ ] group issue（`issues/epic/<name>/`）でも深さを正しく算出する
- [ ] **変異検証**: symlink の削除を外すと `test_next_links_valid.sh` が red /
      リンクの depth 調整を外すと `test_issue_links_valid.sh` が red
- [ ] 入口 3 箇所（skill / rule / README）を同じ commit で更新した

## 関連

- [`claim-issue-in-next-and-push.md`](../_claude/rules/claim-issue-in-next-and-push.md) — ②の規範
- [retro 345](345-retro-issue-backlog-consumption-2026-09-09.md) — 出典（実測 4 回の取りこぼし）
