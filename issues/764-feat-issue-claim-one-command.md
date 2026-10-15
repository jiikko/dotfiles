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

## 対応方針 (反証レビュー 2026-10-15 を反映)

- **バナーと目印は Go の既存の本体を使う**: glogx の module に `cmd/issue-claim` を足し、`issues.Scan` で番号から issue を 1 件に確定して
  `issues.MoveToSubdir(iss, NextDirName)` (viewer の `n` と同じ。目印を置き、バナーを書き、失敗したら目印を戻す) を呼ぶ。
  awk に `add` のモードを移植して Go と一致テストで追いかける案は採らない (正本が 2 つになる。反証レビュー P2-6)。ビルドは既存の `go_autobuild` / `go_tool_resolve`
- **commit と push は一時 worktree で行う** (`scripts/issue_claim.sh <NNN>`): `git fetch` → `origin/<既定のブランチ>` から一時 worktree を切る →
  その中で `issue-claim` → `git add -A -- issues` (新品の worktree なので自分の変更しか無い) → commit → `git push origin HEAD:<既定>` → worktree を捨てる。
  - 手元の checkout の状態 (detached HEAD・未 push の作業 commit・古い本文) に左右されない (反証レビュー P1-1 / P1-2)。手元には後から rebase / pull で入る
  - 失敗時の戻しは「一時 worktree を捨てる」だけで、手元の木と origin には何も残らない (P2-4。commit 後の reset が要らない)
  - non-fast-forward で弾かれたら、取り直して一時 worktree を作り直し、上限つきで再試行する
  - pre-push の issue 検査で落ちたら (無関係の赤でも止まる。P2-3) `--no-verify` は使わず、検査の出力を見せて止める
- 既に claim されている (目印がある / next の実ファイル) ・done にある・番号の実体が 0 件 / 複数・epic の予約外の深さは、Go 側で拒否して何も commit しない
- バナーの書式は viewer の `ClaimBanner` のまま (`<ホスト名> (glogx)`)。誰の claim かはマシンで表す (viewer と同じ理由。P3-9 の書式の分裂を作らない)
- rule (`_claude/issue-rules.d/claim-issue-in-next-and-push.md`) の手順を「`scripts/issue_claim.sh <NNN>` を使う」に置き換え、手の手順は道具が使えない repo 向けに残す

## 受け入れ条件

- [ ] 1 コマンドで目印・バナー・commit・push が済む。手元の checkout は触らない
- [ ] 既に claim されている issue / done の issue / 番号の衝突では何も push しない
- [ ] origin が先へ進んでいても (non-fast-forward) 取り直して claim できる
- [ ] pre-push で落ちたら何も push せず、一時 worktree も残さない
- [ ] 上をテストで固定し、変異で red を確かめた

## 関連ファイル

- `scripts/issue_done.sh` (対になる道具) / `scripts/lib/claim_banner.awk`
- `src/glogx/issues/banner.go` / `src/glogx/issues/move.go` (viewer の `n`)
- `_claude/issue-rules.d/claim-issue-in-next-and-push.md` / `issues/README.md`

## 進捗

- 2026-10-15: 起票 (763 の採番と claim を手で打って崩した経験から)
- 2026-10-15: 反証レビュー (sonnet 1 本、read-only)。事実の主張 (claim の道具は無い / バナーの挿入は Go にしか無い) は反証されず。
  採用: P1 2 件 (detached HEAD の worktree では push 先と「claim の 1 本だけか」が決まらない / origin の先端に乗らない commit) → 一時 worktree 方式 /
  P2 (pre-push の無関係な赤・途中失敗の戻し・awk 移植の二重実装) → 上の方針 / P3 (hook との関係・epic・書式) → 方針に反映。
  記録: issue_done.sh は commit も push もしないので「対になる道具」は非対称 (claim は他マシンへ見せるのが目的なので push まで持つ)
