# 403 (test): next/ に claim の目印があるのに、issue 本文 1 行目に担当者バナーが無い状態を検出する

起票日: 2026-09-19
工数: S (tests/issues/ に検査 1 本 + 変異確認)

## 概要

`claim-issue-in-next-and-push.md` は「claim したら issue 本文の 1 行目にも担当者と状態を書く」を要求しているが、
書き忘れを検出する仕掛けが無い。`next/` への push は hook (`next-claim-push.sh` / `next-claim-unshared.sh`) が
促すのでそこまでは到達し、**push した時点で claim が完了したように感じてバナーが落ちる** (retro 361。
2026-09-11 に 358 で発生し、照会 1 往復と推測による誤帰属を生んだ)。glogx の `n` も symlink を作るので hook では捕まらない。

## 対応方針

- `tests/issues/test_next_links_valid.sh` の隣に、`issues/next/` と `issues/epic/*/next/` の各 symlink について
  リンク先 issue の冒頭にバナー (書式は claim ルール側で確定させる) があるかを見る検査を足す
- 🚨 検査が名指しする退行 (バナーを消す変異) で red になるまでを 1 セットにする。対象 0 件を合格に畳まない

## 進捗

- [ ] バナーの書式を claim ルールで確定
- [ ] 検査を追加し、変異で red を確認
