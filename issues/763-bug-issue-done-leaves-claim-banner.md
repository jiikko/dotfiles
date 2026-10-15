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
- 2026-10-15: 実装 (commit「issue_done.sh: done へ送るときに担当者バナーを外し、目印の無いバナーを検査で落とす (issue 763)」)
  - `scripts/lib/claim_banner.awk` に判定と除去を寄せた (mode=find / strip / list)。`test_next_claims_have_banner.sh` の has_banner と逆向きの検査、`issue_done.sh` の手順 3 がこれを読む
  - 逆向きの検査: 目印の無い issue (直下 / done / pending / waiting、epic も同じ) の冒頭にバナーがあれば落とす。canary 1 件 (目印の無い done は拾い、目印のある issue は拾わない) を足した
  - 既存の 13 件からバナーを外した (13 ファイル、追加 0 行・削除 24 行 = バナー 13 行と直後の空行 11 行)
  - Go 側: `banner_test.go` に `TestRemoveBannerMatchesShellStrip` (Go の removeBanner と awk の strip が 10 ケースで同じ結果。CRLF・空白だけの次の行・見出しが無い本文を含む)。`banner.go` のコメントの参照先を lib へ
  - 配線: `test_issue_done.sh` の fake root に `scripts/lib` を張った。`.github/workflows/src_glogx.yml` の paths 2 か所に lib を足した。claim の rule の完了の項に 1 行
  - 変異 (すべて戻して green): 除去の後の空行の扱いを外す → issue_done の検査と Go の一致テストが red / フェンスの判定を外す → canary 経由で 3 つとも red /
    逆向きの検査を外す → canary が red / issue_done.sh の除去を外す → test_issue_done.sh の banner_check とバナー検査が red (同ファイルに固定) /
    除去を moved.bak の前へ動かす → 判定前に中断すると rollback 後の本文からバナーが消える (手で 1 回確かめた。sed の 1 行変異では書けないので常設していない。既存の「判定前の中断」変異の全ファイル比較がこの順序を見る)
- 2026-10-15: 敵対的レビュー 1 周目 (opus、read-only)。issue_done.sh の rollback は壊せなかった (除去の失敗・中断・symlink の issue・CRLF)。採用した指摘:
  - P1 (再現つき): UTF-8 のロケールで不正なバイトを含む issue があると、awk がそこで止まって同じ xargs の束の残りを読まず、逆向きの検査が緑のまま終わる
    → awk はすべて `LC_ALL=C` で読み (判定はバイト列の一致なので結果は変わらない)、xargs の rc を見る。canary に不正なバイトのファイルを足した
  - P2 (変異で再現): 逆向きの検査の置き場 (直下・pending・waiting・epic の直下と状態) を固める canary が無く、どの枝を外しても緑 → canary を置き場ごとに 1 件 (目印のある issue・next の実ファイル・番号の無い md は拾わない) にした
  - P2: glogx の `n` は目印を git add しないので、本文だけを commit した push を逆向きの検査が止める。claim の rule (目印とバナーを同じ commit で push) どおりの挙動なので止めるのは維持し、案内文を「作業中なら目印を git add して同じ push に含める」に直した
  - P3 (再現つき): awk の除去は改行で終わらない最後の行に改行を足し、NUL を含む行を切り詰める (Go の removeBanner と食い違う) → issue_done.sh は除去の前後を diff し、
    「1〜2 行の削除だけ」でなければ本文を書き換えずに中止する (trap が rollback)。テスト 2.8 で固定
  - P3: 番号が 4 桁になると拾わない / 拡張子の大文字 → `-iname '[0-9][0-9][0-9]*-*.md'` に
  - 記録 (直さない): Go と awk は U+3000 だけの行の扱いが違う (Go の TrimSpace は空行と読む)。issue_done.sh では diff の検査が本文の変化を止めるので、食い違っても中止になるだけ
  - 記録 (変更前からの性質): リンクを張り直す `rebase_file` の awk も、書き換えたファイルの改行で終わらない最後の行に改行を足し、NUL を切り詰める。
    リンクの書き換えは中身を変える操作なので検査は足していない。trigger: done へ送った issue の本文で、リンク以外の差が見つかったとき
  - 記録: pre-push の issue 検査は `issues/` を触る push でだけ走るので、lib だけを変える push は pre-push で検査されない (CI の tests.yml と src_glogx.yml の paths が拾う)
  - 変異 (すべて戻して green): 除去の前後の検査を外す → 2.8 が red / 置き場の枝を 1 つずつ外す (4 本) → canary が red / `LC_ALL=C` を外す → 不正なバイトの canary が red / 番号を 3 桁に戻す → canary が red。
    xargs の rc を見るのをやめる変異は緑のまま: `LC_ALL=C` の下で awk を失敗させる現実的な入力が無い (多層防御として残し、変異では示せていない)
- 2026-10-15: 敵対的レビュー 2 周目 (opus、read-only。1 周目の修正の差分)。banner_only_removed・claim_of・`-iname`・canary は壊せず、実 repo (master の issues/) の走査は外した 13 件だけを拾って余分な誤検出 0 件。採用した指摘:
  - P2 (再現つき): `LC_ALL=C` を当てたのが除去の awk だけで、リンクを張り直す awk (`rebase_file` / canary / `stale_ref_report`) は UTF-8 のロケールで不正なバイトに止まり、`|| true` で握り潰されて「リンクが解決しない」という別の理由で落ちる
    → 呼び出しごとに付けず、issue_done.sh の先頭で `export LC_ALL=C` にした。テスト 2.9 (不正なバイトを含む本文を UTF-8 のロケールで送る) で固定。変異: export を外す → 2.9 が red
  - P3 (再現つき): 検査の第 1 引数に symlink のディレクトリを渡すと、BSD の find が起点を辿らず 0 件の緑 → 起点の末尾に `/`。外すと同じ形が緑に戻ることを手で確かめた (検査スクリプト自身の頑健さなので常設のテストは置いていない)
  - 記録: バナー行が最後の行で改行で終わっていない本文は、除去としては正しくても diff の `\ No newline` で拒む。中止して元へ戻るだけ (手で外す)
- 2026-10-15: 敵対的レビュー 3 周目 (opus、read-only。2 周目の修正の差分)。`export LC_ALL=C` は壊せず (子の検査・diff の文言・母集合の grep・番号の引数・フェンスの判定・パスの扱い。母集合の grep は不正なバイトを含むファイルも拾う向きに広がった)。
  - 採用 (再現つき): 起点の末尾の `/` を付けたのが逆向きの find だけで、順方向 (目印があるのにバナーが無い) の find は symlink の起点を辿らず 0 件のまま → 同じく `/` を付けた
  - 記録 (変更前からの性質): 検査は `cd "$ROOT_DIR"` するので相対の引数は repo root から解決され、検査件数の下限も無い (issues の親を渡すと 0 件の緑)。trigger: 引数を差し替える使い方を足すとき
  - 敵対的レビューを 3 周で閉じた理由: 3 周目で issue_done.sh 側の指摘が尽き、検査側の残りは同じ形の直し残し 1 件だけになった
- 2026-10-15: `make test` は `tests/claude/test_claude_mods.sh` の tool-elapsed (`5 分ごとに toast が出る` が 5 秒で時間切れ) だけが赤。並列の負荷下でだけ出る形で、`make test-dir DIR=tests/claude` で単独に回すと 9 件とも pass した (この変更は mods に触れていない)。
  issue 運用の検査 (`test_next_claims_have_banner` / `_fixtures` / `test_issue_done`) と `glogx/issues` は緑。`src/glogx` の `make lint` は 0 issues (errorlint と gofmt の 2 件を直した後)
