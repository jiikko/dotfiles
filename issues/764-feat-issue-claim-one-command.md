# 764 (feat): issue の claim を 1 コマンドにする (scripts/issue_claim.sh)

> 🚨 **担当中: claude (opus, dotfiles 762 のセッション)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

issue の claim (next/ の目印 + 本文冒頭の担当者バナー + その 2 つだけの pathspec commit + 即 push) は、glogx の viewer の `n` 以外では手作業になっている。
完了の側は `scripts/issue_done.sh` に寄せてあるのに、着手の側は rule (`_claude/issue-rules.d/claim-issue-in-next-and-push.md`) が手順の sh を載せているだけで、手で打つたびに崩れる。

## 詳細

- 2026-10-15 (このセッション): 763 の採番と claim を 1 本の Bash で打ったところ、先頭の `git pull` が detached HEAD で失敗し、
  `&&` で繋いだ本文の書き出しが走らず、symlink だけの commit ができた。push は pre-push の `test_next_links_valid` が
  「目印の指す先が無い」で止めたので外へは出なかったが、手順が 4 つの別々の操作に分かれていることがそのまま崩れ方になっている
- バナーの書き方は glogx の `addBanner` (`src/glogx/issues/banner.go`) にしか無い。front matter・H1 の有無・CRLF を扱っており、手の `sed` はこれを再現しない
- 完了側の `scripts/issue_done.sh` は「手でやらない」と rule が名指ししている。着手側に同じ道具が無い

## 対応方針 (案。反証レビュー前)

- `scripts/issue_claim.sh <NNN>` を足す:
  1. `git fetch` してから、origin の木で既に claim されていないか (その group の next/ に目印があるか / 本文冒頭にバナーがあるか) を見る。されていれば何もしない
  2. 実体を 1 件に確定する (issue_done.sh と同じ規則: done/ は対象外、0 件・複数件は拒否)
  3. group root の next/ に `../<同名>` の symlink を置き、本文冒頭にバナーを足す。バナーの差し込みは `scripts/lib/claim_banner.awk` に `add` のモードを足し、
     Go の `addBanner` との一致テストを `banner_test.go` に足す (判定・除去と同じ方式)
  4. その 2 つだけを pathspec で commit する
  5. push は、origin に無い commit が claim の 1 本だけのときに限り行う。ほかにもあるなら push せず、`git log --oneline origin/<branch>..HEAD` の出力をそのまま出して止める (rule の「飛ばしてよいかを先に確かめる」)
- 誰の claim かは引数か既定 (ホスト名 + セッションを表す文字列) で受ける
- rule の手順の sh を「`scripts/issue_claim.sh <NNN>` を使う」に置き換え、手の手順は道具が使えない repo 向けに残す

## 受け入れ条件 (案)

- [ ] 1 コマンドで目印・バナー・commit・push が済み、pre-push の issue 検査が通る
- [ ] 既に claim されている issue / done の issue / 番号の衝突では何もしない
- [ ] push に他の未 push commit が混ざるときは push しない
- [ ] バナーの差し込みが Go の addBanner と一致する (front matter・H1 無し・CRLF を含む)
- [ ] 途中で失敗したら目印とバナーを戻す (半端な claim を残さない)

## 関連ファイル

- `scripts/issue_done.sh` (対になる道具) / `scripts/lib/claim_banner.awk`
- `src/glogx/issues/banner.go` / `src/glogx/issues/move.go` (viewer の `n`)
- `_claude/issue-rules.d/claim-issue-in-next-and-push.md` / `issues/README.md`
