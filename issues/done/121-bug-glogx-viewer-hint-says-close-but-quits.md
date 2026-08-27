# 121 bug: 両 viewer の hint が `q: 閉じる` と出すが、`q` は glogx ごと終了する

起票日: 2026-08-27 / 出典: ux 監査 / priority: medium

## 事実

- `issues_view.go:hint` / `status_view.go:hint` の一覧モードは末尾が **`q: 閉じる`**
- 実装は `case "q", "esc"` → `wantQuit`。`status_view.go` にはその根拠コメントもある —
  「viewer からの q/esc は **glogx ごと終了**する (ユーザー要望 2026-08-06: git log 一覧へは戻らない)」
- git log 一覧の hint (`tui.go:hintLine`) は同じ動作を **`q: 終了`** と書いている
- README も「`i` で閉じて一覧へ戻る」「**`q`/`Esc` は glogx ごと終了**」と 2 語を明確に使い分けている

commit `4f11b22` (q/esc を「一覧へ戻る」から「glogx ごと終了」へ変えた変更) の message は
「README / --help / 両 spec を追従」と列挙しており、**hint 文字列はその列挙に入っていない**。
README と `--help` は正しく直っているので、**画面上の hint だけが直し漏れ**。

## ユーザーに何が見えるか

「viewer を閉じて git log 一覧へ戻る」つもりで押した `q` で **glogx が終了してシェルへ戻る**。
しかも hint には実際の閉じるキー (`i` / `s`) が 1 つも出ていないので、
**画面上に「一覧へ戻る方法」が存在しない**。

## 対応

- 両 hint の `q: 閉じる` を **`q: 終了`** にする (git log 一覧の hint と同語)
- 幅に余裕があれば issues 側は `i: 一覧へ`、status 側は `s: 一覧へ` を足す
  (⚠️ `issues_view.go:hint` には幅テスト `TestIssuesViewHintFitsPopupWidth` があり、
  doc に「`a: pending も` (14 桁) では末尾の `q: 閉じる` が黙って切れる」と実測が残っている。
  足すなら幅を測ってから)
- `docs/status-viewer-spec.md` のモック図も同じ古い語 (`d: diff  q: 閉じる`) なので同時に直す

## 注意 (全画面 pager は対象外)

`status_view.go` の pager 用 hint (`d/q: 閉じる`) は**正しい** (そこでの `q` は pager を閉じる)。
直すのは一覧モードの 2 本だけ。

---

## 対応 (2026-08-28)

- `issues_view.go:hint` / `status_view.go:hint` の**一覧モード**を `q: 終了` へ
- `status_view.go` には戻り方 `s: 一覧へ` も追加
- `docs/status-viewer-spec.md` §6 のモック図も同じ古い語だったので直した
- 既存テスト `TestIssuesViewerHintIsNotPrefixed` の期待値も追随させた (`q: 閉じる` を探していた)

### ⚠️ pager の「d/q: 閉じる」は正しいので触らない

`status_view.go` の全画面 pager の hint は、そこでの `q` が pager を閉じるので**正しい**。
直したのは一覧モードの 2 本だけ。テストは**両方を pin** している — 片方しか見ていないと
「まとめて『閉じる』に戻す」変更が通ってしまう。

### issues 側に `i: 一覧へ` は入れられなかった

足すと最長モード (filter=2) で 85 桁になり `TestIssuesViewHintFitsPopupWidth` が落ちる (実測)。
**幅テストが正しく捕まえた**ので、issues 側は文言修正だけに留め、理由をコード側のコメントに残した
(戻り方は `--help` と README が正本、という既存の契約どおり)。

### 変異検証 4/4 red

status 一覧を「閉じる」へ戻す / 戻り方 (s) を落とす / **pager まで「終了」にする (取り違え)** /
issues 一覧を「閉じる」へ戻す。
