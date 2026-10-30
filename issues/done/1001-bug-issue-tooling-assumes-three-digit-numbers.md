# 1001 (bug): 番号が 1000 に達し、採番の手順・番号の重複の検査・issue_done.sh などが 3 桁を前提にしている

起票日: 2026-10-30

## 概要

dotfiles の issue の番号が 999 に達し、1000・1001 (この issue) を採番した。採番の手順と道具・検査のいくつかが 3 桁を前提にしていて、
1000 番以降で黙って誤る (番号の衝突・検査の素通り) か、拒否する。issue 999 の P-2 を 1000 で起票するときに見つけた。

## 3 桁を前提にしている所 (2026-10-30 の master で grep した)

番号の衝突を作るもの (急ぎ):
- `issues/README.md` の「採番」の手順 (14〜15 行。`_claude/issue-rules.md` は「git fetch して origin 側も数える」とだけ書き README を指す): `grep -E '^[0-9]{3}-' | sort | tail -1` で最大の番号を出す。
  1000 番以降のファイルはこの grep に掛からず、最大は 999 のまま。掛けても `sort` が文字列の順なので `999-…` が `1000-…` より後に来る。
  → 次に採番するセッションも 1000 を取る
- `scripts/issue_number_drafts.sh` (72〜74 行): `'[0-9][0-9][0-9]-*.md'` と `grep -E '^[0-9]{3}-' | cut -c1-3` で最大を出す。同じく 1000 番以降を数えない

番号の衝突を見逃すもの:
- `tests/issues/test_issue_numbers_unique.sh` (51 行): `sed 's|^\([0-9][0-9][0-9]\)-.*|\1|'` は 1000 番以降のファイル名を番号に切り出せず、
  ファイル名全体を番号として比べる → `1000-a.md` と `1000-b.md` の重複を検出しない (39 行の find は 4 桁を拾う)

拒否するもの:
- `scripts/issue_done.sh` (348 行): `[0-9][0-9][0-9]` 以外を「3 桁で指定する」で rc=2。1000 番以降を done にできない
  (`scripts/issue_claim.sh` は `^[0-9]{3,}$` で受ける)

素通りするもの:
- `tests/githooks/test_pre_push.sh` (38・66・68〜69 行): `[0-9][0-9][0-9]-*.md` で最大を出し `%03d` で空き番号を作る。4 桁の issue があると最大が 999 のままで
  空き番号が 1000 になり、既にある 1000 と衝突する
- `tests/issues/test_issue_path_refs_not_stale.sh` (68 行): `[0-9]{3}-` だけを見るので、1000 番以降の issue を指す古いパスを検査しない

手順・仕様の文書 (同じ前提を書いている):
- `_claude/skills/issue-sync/SKILL.md` (75 行): 「`^[0-9]{3}-` に一致するものだけを issue とみなす」
- `docs/issues-viewer-spec.md` (689 行): 次番号の確認のコマンドを `^[0-9]{3}-` として書いている

対象外 (確かめた): `_claude/hooks/issue-progress-check.sh` は `[0-9]{3,}` で受ける (コメントに「桁数を決め打たない」とある)。
`human-tasks-due.sh`・`retro-open.sh` の `[0-9]...` は日付の照合。glogx の issues の読み取り (`numberedRe = ^(\d{2,})-`) は桁数を問わない

## 対応方針

- 採番は最大を数値で比べる (`sort -n` / `awk` で数値化)。3 桁の切り出しをやめ、`[0-9]{3,}` で受ける
- 3 桁未満はゼロ埋めのまま、1000 以降は 4 桁 (ファイル名の 3 桁ゼロ埋めの規約を「3 桁以上」に直す)
- 各検査に 1000 番の fixture を足し、3 桁の前提に戻す変異で red を確かめる

## 進捗

- [x] 起票時の反証レビュー (sonnet、読み取りのみ): 衝突・見逃し・拒否・素通りの主張はどれも反証されなかった。
  `_claude/issue-rules.md` に採番のコマンドは無い (README だけ) → 直した。挙げ漏れ 3 か所 (tests/githooks/test_pre_push.sh・issue-sync の SKILL・
  issues-viewer-spec) を足した

- [x] 直すかの判断: ユーザー指示「1001 を直して」(2026-10-30)
- [x] 直した — commit「fix(issues): 番号が 1000 を超えても採番・重複の検査・issue_done が誤らないようにする (issue 1001)」
  - 最大の番号を出す処理を `scripts/lib/issue_numbers.sh` (`issue_max_number`・`issue_default_ref`) に寄せ、次の番号を出す
    `scripts/issue_next_number.sh` を足した。README の採番の手順はこれを打つ形にした。issue_number_drafts.sh もこの関数を使う
  - 番号は「3 桁以上の数字 + - + 英字」で数値として比べる (英字まで見るのは、issues/ の下に日付で始まる記録を置く repo で
    2026-02-20-….md を番号 2026 と読まないため)。ref 側は core.quotePath=false で非 ASCII の名前も数える
  - 番号の重複の検査・issue_done.sh・パスの参照の検査・pre-push のテストの fixture を 3 桁以上にした
  - 文書: issue 規約の NNN・issue-sync と claude-md-refresh の skill・issues-viewer-spec (skill-eval は 2 本とも PASS)
- [x] テスト: tests/issues/test_issue_numbers_four_digits.sh を足し、drafts・done・パスの参照の検査にも 1000 番のケースを足した。
  変異で red を確認 (番号の切り出しを 3 桁に・sort -n を sort に・重複の検査の切り出しを 3 桁に・issue_done の検査を 3 桁に・
  パスの参照を 3 桁に・英字の条件を外す・quotePath を外す・既定の ref を固定・lib の解決を cd の後に)。
  quotePath の変異は、最初はテストが利用者の `~/.gitconfig` (core.quotePath=false) を読んで緑のまま通った → テストで git の設定を読まないようにした
- [x] 敵対的レビュー (opus) 2 周: 1 周目の P2 (日付で始まる記録を番号と読む) と P3 (引用符の付いた名前・既定の ref の食い違い)、
  2 周目の P2 (サブディレクトリから相対パスで起動すると lib を読めない。旧版の drafts では起きない退行) と P3 (origin/HEAD の指す先が消えたとき) を直した。
  受けた (直していない): 先頭にゼロの付いた番号 (0999 と 999) の扱いが道具ごとに違う。採番の道具は付けないので起きる見込みは低い (再提起には実例を添える)
- [x] ルートの make test rc=0 (2026-10-30。1 回目は shellcheck の SC1091・cd の rc・コメントの issue 番号で赤 → 直した)
