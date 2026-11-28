# 1123 (bug): 採番・claim・完了の道具が入れ子の issues/ (obaket の macOS/issues/) を見ない

起票日: 2026-11-28

## 概要

`scripts/issue_next_number.sh`・`scripts/issue_claim.sh`・`scripts/issue_done.sh` は repo 直下の `issues/` だけを対象にする。
hook の lib (`issue_hook_resolve_dir`) が見る入れ子の 1 段 (`<root>/*/issues`) は数えず、obaket の `macOS/issues/` の issue を採番・claim・完了できない。

## 詳細

- 起点 (2026-11-28、obaket の 1591): `macOS/issues/1591-…` を done へ送ろうとして、`issue_done.sh 1591` が「実体が見つからない (既に done/ に在る?)」と答えた。
  obaket の `bin/issue-done` で移した。規約 (claim の rule の完了の項・issue-sync) は「どの repo でも dotfiles の道具を使う」と案内していた
- 同じ仕組み (repo 直下の `issues/` だけを見る) は採番 (`issue_next_number.sh`) と claim (`issue_claim.sh`) にもある。採番は obaket で
  `macOS/issues/` の番号を数えない。2026-11-28 は `issues/` の最大 1620 > `macOS/issues/` の最大 1591 で、`bin/next-issue-number` と同じ 1621 を返したが、
  入れ子の側が大きくなると重なる
- 入れ子の `issues/` の扱いは repo ごとに違う (2026-11-28 に数えた):
  - obaket: `issues/` と `macOS/issues/` で通しの番号。`bin/issue-done`・`bin/next-issue-number` を持つ
  - DualNoteApp: `issues/` (最大 209) と `macOS/issues/` は通しの番号 (macOS 203 → 直下 204 → macOS 205〜208 → 直下 209)、`iOS/issues/` だけ 001 からの別の列。自前の道具は無い
  - umbrella (my-products): `project-templates/issues/` は submodule (別の repo)
  - ThumbnailThumb: `tests/issues/` はテストの置き場で issue ではない

## 対応方針 (2026-11-28 のユーザー決定: 「repo の道具へ案内」)

入れ子の dir を一律に数えると、DualNoteApp では `iOS/` の列の番号を最大値に混ぜ、umbrella では submodule の issue を動かす。だから dotfiles の道具は入れ子を操作せず:

- repo が `bin/issue-done`・`bin/next-issue-number` を持つなら、何もせずにそれを案内して止まる (rc=2)。`issue_done.sh` は `ISSUES_DIR` を指定したときは案内しない
- 見つからない issue が入れ子の 1 段 (`<repo>/*/issues`・`*/issue`) にあれば、場所を出す (別の repo の dir は出さない)
- claim は repo 自前の道具を案内しない (dotfiles の `bin/issue-claim` は `issue_claim.sh` の部品で、dotfiles の中で自分を案内してしまう)
- 規約の 2 か所 (claim の rule の完了の項・`issue-rules.md` の採番) に「repo の道具があればそちら」を書く。issue-sync は触らない。issue-sync の例外は「`issues/README.md` が別の完了コマンドを定めていれば」で、obaket の README は `bin/issue-done` を名指していない (`.claude/rules/issue-done-on-completion.md` が名指す)。issue-sync の手順で obaket で打っても、道具が `bin/issue-done` を案内して止まる (rc=2)

採らなかった案: repo が設定ファイルで issue の dir と番号の範囲を宣言し、dotfiles の道具が入れ子も扱う。obaket の `bin/issue-done` と同じ仕事の実装が 2 つになり、obaket の道具だけが回す検査 (check-doc-links) を通らない

## 進捗

- 2026-11-28: commit「採番・完了の道具は repo 自前の道具があれば案内して止まり、入れ子の issues/ の issue は場所を出す」
  - 判定は `scripts/lib/repo_issue_tools.sh` に 1 つだけ置き、3 本から読む
  - 実物: obaket で `issue_next_number.sh`・`issue_done.sh 1591` が rc=2 で `bin/` の道具を案内し、作業ツリーは変わらなかった。DualNoteApp は 210、dotfiles は今までどおり番号を出した
  - テスト: `test_issue_done.sh` (入れ子の案内・別の repo の dir を出さない・repo の道具への案内・`ISSUES_DIR` を指定したら案内しない)・`test_issue_claim.sh` (入れ子の案内)・
    `test_issue_numbers_four_digits.sh` (repo の道具への案内)
  - 変異 (`mutate-verify`。どれも狙ったテストで red): done の案内を外す / 採番の案内を外す / 別の repo の dir を除く行を消す / done の入れ子の案内を外す / claim の入れ子の案内を外す
  - 変異を当てていないもの: `ISSUES_DIR` を指定したときに案内しない分岐 (テストはある)
  - 敵対レビュー (codex、P1 0・P2 5・P3 3)。直したもの: 採番の道具が継承した `GIT_DIR` / `GIT_WORK_TREE` を外していなかった (done と claim は外す。別の repo の番号を数え、道具の判定もすり抜ける) /
    `ISSUES_DIR` を指定したときのテストが rc しか見ていなかった / テストの案内のパスの照合 (`/var` と `/private/var`) / DualNoteApp の番号の列の事実 / issue-sync の記述
  - 記録だけで直さないもの (発火に作った形が要る。再提起するなら実在する repo の形を添える):
    - `<repo>/bin` が別の repo の `bin/` への symlink だと、案内した道具が別の repo を操作する
    - 入れ子の `issues/` の下に別の repo (submodule) があると、その中の issue も案内に出る (所属は入れ子の `issues/` の入口でだけ確かめる)
    - 直下に `issues/` が無い repo、入れ子の `issues/` が symlink の repo では、入れ子の案内が出ない。claim は origin から切った一時 worktree を探すので、手元にだけある issue も案内しない
    - claim のテストは手元と origin の両方に同じ issue を置くので、探す先を一時 worktree から手元に変える変異を見分けない
