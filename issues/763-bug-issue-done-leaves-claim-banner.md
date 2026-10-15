# 763 (bug): scripts/issue_done.sh が担当者バナーを外さず、done/ に「担当中」が残る

> 🚨 **担当中: claude (opus, dotfiles 762 のセッション)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

issue を done へ送る入口は 2 つある。glogx の issues viewer で送ると担当者バナー (`> 🚨 **担当中: …**`) が外れるが、
`scripts/issue_done.sh` で送るとバナーが本文に残る。done/ の issue を開いた人には「誰かが作業中」と読める。

## 詳細

- viewer: `src/glogx/issues/move.go` の移動は、next から出るとき (`iss.Status == StatusNext && subdir != NextDirName`) に
  `clearClaimBanner` で冒頭のバナーを外す。コメントは「バナーは next にいる間だけの表示」。外す規則は `src/glogx/issues/banner.go` の `removeBanner`
  (最初の `## ` より前・コードフェンスの外・行頭の `**担当中` / `**着手中`。バナー行と直後の空行 1 つを消す)
- `scripts/issue_done.sh`: 手順は 4 つ (移動 / claim symlink の削除 / 本文リンクの張り直し / 他からの参照の張り直し) で、バナーには触れない。
  claim の rule (`_claude/issue-rules.d/claim-issue-in-next-and-push.md`) は完了を「手でやらず issue_done.sh に寄せる」としているので、規約どおりに完了させるとバナーが残る
- 2026-10-15 に数えた: done/ (global と `epic/*/done`) で、最初の `## ` より前の行頭にバナーが残っているのは 13 件
  (717 / 385 / 308 / 670 / 305 / 588 / 586 / 658 / 620 / 686 / 681 / 680 / 682)。本文の途中でバナーの書式に触れているだけの 403 / 348 / 351 / 537 は数えていない
- 検査は next/ 側 (`tests/issues/test_next_claims_have_banner.sh`: claim があるのにバナーが無い) だけで、逆向き (done/ にバナーが残っている) を見る検査は無い

## 対応方針

- `scripts/issue_done.sh` の手順 3 (移したファイルを書き換える段) で、`moved.bak` を取った**後**に冒頭のバナー行と直後の空行 1 つを外す。
  判定が落ちたときは既存の rollback が `moved.bak` から本文を戻すので、バナーも戻る。外したときは結果の行に「担当者バナーを削除」と出す
- バナーの判定規則は shell 側では `test_next_claims_have_banner.sh` の has_banner (awk) にしか無い。外す側を足すと shell に 2 つ目ができるので、
  awk を `scripts/lib/claim_banner.awk` (判定と除去のモード) に寄せ、検査と issue_done.sh の両方から `BASH_SOURCE` 基準のパスで読む。
  フェンスの判定は has_banner の `^ {0,3}(```|~~~)` に揃える (issue_done.sh の `AWK_REBASE` のリンク書き換え用の判定とは別物で、そちらは触らない)。
  Go 版 (`banner.go`) は言語が違うので残し、コメントの参照先を lib へ直す
- 逆向きの検査を足す。新しいファイルにせず `test_next_claims_have_banner.sh` に「claim の目印が無い issue の冒頭にバナーがある」を足す。
  不変条件は「バナーがある ⇔ next/ に目印がある」で、viewer は next から pending へ出すときもバナーを外すので、done に限らない
  (2026-10-15 の数え直しで、done 以外の違反は 0 件)。既存の 13 件は同じ commit で外す。
  新しいファイルにしないのは、pre-push の `CHECKS` / `EXCLUDED` (`githooks/pre-push`) への登録と、その同期の検査 (`tests/githooks/test_pre_push.sh`) を増やさないため
- 配線: `tests/issues/test_issue_done.sh` の `make_fake_root` は issue_done.sh のコピーだけを置くので、`scripts/lib` も張る。
  `.github/workflows/src_glogx.yml` の paths は `test_next_claims_have_banner.sh` だけを載せているので、lib も足す (Go と shell の判定の一致を見る `banner_test.go` が走るように)
- claim の rule の「完了したら」の項に、issue_done.sh がバナーも外すことを書く

## 受け入れ条件

- [ ] issue_done.sh で送った issue の冒頭のバナーが外れる。バナーの無い issue・本文の途中やコードフェンスの中にバナーの書式がある issue は変わらない
- [ ] 判定に失敗して rollback したとき、バナーも元に戻る
- [ ] claim の目印の無い issue の冒頭にバナーがあると検査が落ちる。既存の 13 件は外れていて検査は緑
- [ ] 上の 3 つを変異で red にして確かめた

## 関連ファイル

- `scripts/issue_done.sh` / `tests/issues/test_issue_done.sh`
- `tests/issues/test_next_claims_have_banner.sh`
- `src/glogx/issues/banner.go` / `src/glogx/issues/move.go`
- `_claude/issue-rules.d/claim-issue-in-next-and-push.md`

## 進捗

- 2026-10-15: 起票 (dotfiles issue 705 を issue_done.sh で閉じたとき、バナーを手で外す必要があって気づいた)
- 2026-10-15: 反証レビュー (sonnet 1 本、read-only)。事実の主張 (13 件の番号・viewer の経路・issue_done.sh がバナーに触れない) は反証されず。
  対応方針への指摘 4 件を反映した: 変異テストの fake root に lib が無い (P1) / 検査が done だけでは不変条件の半分 (P2) / 新しい検査ファイルは pre-push への登録が要る (P2) /
  CI の paths に lib が無い (P2)。P3 の「フェンス判定は has_banner に揃える」「除去は moved.bak の後」も方針に書いた
