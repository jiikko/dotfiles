# glogx issues viewer: 閉じた epic（親 issue も done）の畳み方が決まっていない

起票日: 2026-09-06
カテゴリ: feat / 対象: `src/glogx/issues_view.go`（group 行の構築）、`docs/issues-viewer-spec.md`

## 何が決まっていないか

issue 291 で group 内の完了は `epic/<name>/done/` へ移すことにした。ここで
「**epic そのものが終わった**」ときの見え方が未定:

- 親 issue（group 名と同じ番号の issue。統合親行になる）を `epic/<name>/done/` へ移すと、
  親行が **done な子の 1 つ**になり、group の親としては合成行に戻る（`▸ <name> (N ✓N)`）。
  終わった epic が open な epic と同じ見た目で一覧の上位に残り続ける
- 子ごと global `issues/done/` へ流すと、epic 所属がパスから消える（291 で否定した形）

## もう 1 つの軸: 並び順

`sortDisplayUnits` は**子の最大番号**で親行の位置を決める。done の子も番号に数えるので、
**子が全部 done の epic が既定の一覧の先頭に居座る**（2026-09-06 の敵対的レビュー P3-3 で実測）:

```
→ ▸ zeta (2 ✓2)        ← 子は 999/998 が両方 done
  101 ○ feat  real work2
  100 ○ feat  real work
```

「畳む」を決めれば連動して解ける可能性はあるが、**別の軸**（畳まないと決めた場合も、
並び順を「未完了の子の最大番号」にするかは残る）。

## 案

1. **子が全部 done なら親行を畳んで `✓` 側へ寄せる**（状態フィルタ `a` の対象にする）。
   「open な epic の中だけ done を既定表示する」という 291 の説明とも整合する
2. `epic/done/<name>/` のような「終わった epic 置き場」を作る（2 段契約が 3 段になるので重い）
3. 何もしない（終わった epic は人が `epic/` から出す運用にする）

## 受け入れ条件

- [ ] 案を決めて `docs/issues-viewer-spec.md` 3 節・4 節に書く
- [ ] 決めた挙動を固定するテスト（子が全部 done の group / 一部 done の group）

## 関連

- [`291`](done/291-feat-glogx-epic-shows-done-children-by-default.md) — この論点の出所
