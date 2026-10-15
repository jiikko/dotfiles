# 764 (feat): issue の claim を 1 コマンドにする (scripts/issue_claim.sh)

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
- 2026-10-15: 実装 (commit「issue_claim.sh: issue の claim を 1 コマンドにする (一時 worktree で目印・バナー・commit・push。issue 764)」)
  - `src/glogx/cmd/issue-claim` (Go): `issues.Scan` で番号から接頭辞の無い issue を 1 件に確定し、claim 済み (`StatusNext` / 目印あり)・直下 (open) に無いものを拒否してから
    `issues.MoveToSubdir(iss, NextDirName)`。本文と目印のパスを stdout に出す。ラッパーは `bin/issue-claim` (`go_autobuild` の `--pkg cmd/issue-claim`)
  - `scripts/issue_claim.sh`: `git ls-remote --symref` で既定のブランチを引き、`origin/<既定>` から一時 worktree を切って claim → 本文と目印の 2 つだけを add・commit →
    `push origin HEAD:<既定>` → origin の対象のブランチに載ったかを確かめる → worktree を捨てる。`(fetch first)` / `(non-fast-forward)` は取り直して再試行 (既定 3 回)、
    pre-push の失敗は止める。シグナルは印だけを立て、push の後は結果を確かめてから終わる。rc は 0 / 1 (何も残っていない) / 2 (引数・環境) / 3 (push は通ったが確かめられない)
  - 入口: claim の rule の手順を「`scripts/issue_claim.sh` を使う」に置き換え (手の手順は道具が使えない環境向けに残した)、`issues/README.md` に「着手」の節。763 で足したバナーの除去も README の完了の節に追記
  - テスト `tests/issues/test_issue_claim.sh` (bare の origin と clone を一時ディレクトリに作る): 正常系 (手元の HEAD と作業ツリーを触らない) / claim 済み・done・pending・番号の衝突・実体なし・接頭辞つきの ID の拒否 /
    epic / 再試行 2 経路と上限 / pre-push の失敗 / commit は本文と目印だけ (改行コードの正規化で変更扱いになるファイルを巻き込まない) / pushurl で rc=3 / 継承した `GIT_DIR` / push 中の SIGTERM。
    どの終わり方でも一時 worktree と一時ディレクトリを残さないことを毎回見る。pre-push の `EXCLUDED` に登録した (道具自身のテストで repo の状態を見ない)
  - 変異 (すべて red を確認して戻した): worktree の後始末を外す / non-fast-forward を再試行しない / 何でも再試行する / origin でなく手元の HEAD から切る / Go の claim 済みの拒否を外す / Go の open の確認を外す /
    接頭辞の確認を外す / `add -A` に戻す / TERM で即 exit / rc=3 を 1 に / `GIT_DIR` の unset を外す / `(non-fast-forward)` の文言を外す。
    緑のまま: 「変更の無い commit を拒む」ガード (Go 側が壊れたときだけ通る多層防御。Go 側の変異では別の理由で red) / push 後の origin の確認 (pushurl のテストが rc=3 の経路を通すが、確認そのものを外す変異は rc=0 で載ったと誤認し、pushurl のテストが red になるかは未確認)
  - 敵対的レビュー 1 周目 (opus、read-only) の結果: P2 (再現つき) 3 件を採用 — 接頭辞つきの ID (`PRO-105`) を番号で claim する / `add -A` が正規化で変更扱いのファイルを巻き込む / push 中の SIGTERM で載ったのに無言で rc=143。
    P3 を採用 — pushurl が別のとき「載ったのに rc=1」→ rc=3 / `worktree add` の途中失敗で登録が残る → 後始末で prune / origin/HEAD が古い → ls-remote / `GIT_COMMON_DIR` 等の継承 → unset に足した / Scan の警告を捨てていた → stderr へ /
    テスト専用の環境変数の名前を `ISSUE_CLAIM_TEST_BEFORE_PUSH` に。記録: SIGKILL では一時 worktree が残る (ヘッダに書いた)。git の文言の日本語訳は、この機械の git に訳が無く未確認 (de_DE では `(fetch first)` は訳されなかった)
- 2026-10-15: 敵対的レビュー 2 周目 (opus、read-only。1 周目の修正の差分)。シグナルの印の方式・stdout の 2 行・ls-remote の失敗・二重のシグナルは壊せず。採用した指摘:
  - 重大 (再現つき): ref を更新した後に push が rc≠0 で終わる (プロセスグループごとの Ctrl-C で push と受け側が一緒に死ぬ等) と、claim は載っているのに「何も push していない」と rc=130 / 1 を返す
    → push の rc に関係なく先に fetch + merge-base で載ったかを確かめ、載っていれば rc=0、確かめられなければ rc=3、載っていなければ中断・再試行・失敗に分ける。
    テスト 7.10 (origin の post-receive で待たせ、プロセスグループごと SIGINT) で固定。変異: 確かめるのを rc=0 のときだけに戻す → 7.10 が rc=130 で red
  - 軽微: 中断の最中の子の失敗が別の理由と rc=1 で出る → die は中断の合図を受けていれば中断の rc で終わる / `git add` と `rev-parse` の失敗が `set -e` で git の rc のまま漏れる → 理由つきの rc=1 に
  - 記録 (推測): single-branch の clone など、既定のブランチを fetch していない repo では `origin/<既定>` が無く rc=2 で止まる
- 2026-10-15: 敵対的レビュー 3 周目 (opus、read-only。2 周目の修正の差分)。再試行の分岐から fetch を省いた影響と、die と中断の rc の組み合わせは壊せず。採用した指摘:
  - 中 (再現つき): 確かめの `git fetch origin` が関係の無い ref (別の fetch が持つ lock 等) で rc≠0 になると、対象のブランチは更新されたのに「確かめられない」で rc=3
    → fetch を push 先のブランチだけに絞った (最初の fetch も)。single-branch の clone でも追跡 ref が新しくなる。テスト 7.11 で固定。変異: 絞りを外す → 7.11 が red
  - 低 (再現つき): 同じホスト・同じ秒の二重 claim で commit が丸ごと同じになり、後の側の push が up-to-date で通って両方が claim したと報告する
    → commit message に `Claim-Id: <ホスト>-<pid>-<乱数>` を入れて sha を一意にした。変異: 外す → 7.11 の Claim-Id の確認が red
  - 推測 (採用): ssh の push を中断した直後は、確かめた時点では載っていなくても後から載ることがある → push を試みた後の文言を「確かめた時点では origin に載っていない」に弱めた
  - 文書: ヘッダの rc=3 の定義を今の実装 (push の後に載ったかを確かめられない) に合わせた
  - 敵対的レビューを 3 周で閉じた理由: 3 周目の指摘は範囲の狭い 2 件と推測 1 件まで縮み、どちらもテストと変異で固定した
- 2026-10-15: `make test` rc=0 (最後の編集の後の木。lint の 3 件 = zsh のラッパーを `ZSH_SYNTAX_FILES` へ / trap 経由の関数の注記 / パイプ終端の `grep -q` と、`src/PACKAGES.md` への登録を直した後)。`src/glogx` の `make lint` は 0 issues
