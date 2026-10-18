# 821 (docs): コードコメントを Why not だけにする (経緯・出所の語を消し、check-comment-provenance を置く)

起票日: 2026-10-18

## 概要

CLAUDE.md の「何をどこに書くか」(2026-10-18 にユーザーが追記) で置き場所を分けた: コードは How、テストは What、コミットログは Why、
コードコメントは Why not (なぜ素直な別の書き方をしないか・何を壊すから避けたか)。コメントに書かないものは「何をしているかの説明」と
「変更の経緯・レビュー指摘の番号」(コミットログへ)。今のコードコメントには経緯・出所の語が大量にあるので、lint で増えるのを止めてから
領域ごとに書き直す。

🚨 起票の時点で、CLAUDE.md の追記は `~/dotfiles` の未コミットの変更 (master には未反映)。CLAUDE.md が名指しする
`make lint` の `check-comment-provenance` はまだ存在しない (`grep -rn 'check-comment-provenance\|comment_provenance' Makefile scripts/ src/*/Makefile` が 0 件)。

## 現状の数 (2026-10-18。`git grep` でコメント行を取って数えた概数)

🚨 下の表は過少: `git grep -E` は `\s` を空白として扱わず、インデントしたコメント (Go の関数の中など) を数え落としていた。
正しい母数は lint (`scripts/check_comment_provenance.sh`) が数えた **4,188 行 / 1,095 ファイル** (2026-10-18。下の「進捗」)

コメント行 (`//` / `#` / `--` で始まる行。`issues/`・`*.md`・`*.json` を除く) は 40,342 行。そのうち:

| 語 | 行数 | 注 |
|---|---|---|
| `issue NNN` | 2,112 | 領域別: src/pro-con 506・src/glogx 496・tests 297・src/lockman 108・src/treefiler 104・src/doctor 102・scripts 90 |
| 日付 `YYYY-MM-DD` | 1,276 | 実測の日付 (残してよいもの) と経緯の日付が混ざる |
| レビュー系 (レビュー / 反証 / codex / 敵対) | 1,398 | `codex` は CLI の名前 (codex_version.go など) でも出るので誤検出を含む |
| 「以前は / 前は / かつて / 旧実装 / 元は」 | 345 | 「前は」は「〜の前は」の誤検出を含む |
| `retro NNN` | 11 | |

数え方: `git grep -nE '^\s*(//|#|--)' -- ':!issues' ':!*.md' ':!*.json'` の出力を語の正規表現で数えた (1 行に複数の語があれば両方に数える。
行頭がコメントでない行末コメントは数えていない)。この母数はコメントでない行も含む: shebang `#!` 563 行・`//go:` 指令 35 行・`// Package` 120 行・
YAML の `---` など Lua 以外の `--` 55 行・`vendor/` 513 行・`testdata/` 34 行 (反証レビューの数え直し)。語の行数には影響しないが、
40,342 を割合の母数には使わない。`issue NNN` の行のうち「再評価・直るまで・open」の語を含むのは 19 行だけ

典型 (書き直しの見本にする):

- 経緯だけのもの: `src/treefiler/filer/regress691_test.go` の「issue 691 の回帰テスト (2026-10-09 の監査で再現した不具合)」→ テスト名と assert が What を言っていれば消してよい
- 「以前は X」: `src/glogx/tui.go` の「🚨 以前は「失敗は必ずこれを経由」と書いてあったが…」→ CLAUDE.md の規則どおり、X が今も選ばれうるなら「X にしない。X だと〜が壊れる」に直し、そうでなければ消す
- Why not と出所が混ざるもの: `src/doctor/svc/scan.go` の「実測 2026-09-03 (issue 178 の敵対レビュー): 旧実装は `;` を引用しないので…」→ 実測と壊れ方 (Why not) は残し、「issue 178 の敵対レビュー」を消す
- このセッションで足したもの: `tests/lib/go_testmain_guard.sh` の冒頭「(敵対的レビュー 2 本の結果。issue 817 の「CI の赤の修正」)」・`src/glogx/internal/testenv/main_test.go` の「(issue 817 の「CI の赤の修正」の敵対的レビュー)」

## 対応方針

### 1. lint を置く (`make test-lint` の `test-comment-provenance` = `scripts/check_comment_provenance.sh`)

- [ ] 語彙を決める: `issue NNN` / `retro NNN` / 「〜のレビュー」「指摘」「反証」/ 「以前は」「旧実装」/ 経緯の日付。
      残すもの (外部の制約・実測値・却下した指摘の再提起条件) と字面で区別できない語は、検出しない形として script 冒頭に書く
      (`~/.claude/rules/adversarial-review-own-safeguards.md` §8: 脅威モデルと検出しない形を先に書く)
- [ ] 走査の範囲: `vendor/`・`testdata/`・`issues/`・Markdown を外す。`--` は Lua のファイルでだけコメントとして扱う (YAML の `---`・usage の `--opt` を拾わない)。
      shebang `#!` と `//go:build` などの指令は数えない。行末コメントも見るなら、既存違反は上の表より増える
- [ ] 既存の違反はファイルごとの件数の許可リスト (下げる方向だけ動くラチェット) で受け、増えたら落とす。減ったのに一覧が古いのも落とす。
      初期値は lint 自身が数えた値で作る (上の表の数は数え方が違うので流用しない)
- [ ] `make lint` (CLAUDE.md の記述どおり) と CI の Lint に配線し、ログに検査名と件数が出ることを見る
- [ ] 変異で red を確認 (違反の行を足す・一覧の件数を上げる・一覧に実在しないファイルを足す)

### 2. 領域ごとに書き直す (1 領域 = 1 commit。件数の多い順)

src/pro-con → src/glogx → tests → src/lockman → src/treefiler → src/doctor → scripts → 残り。各領域で:

- [ ] 経緯だけのコメントは消す (その内容がコミットログに無くても足し戻さない)
- [ ] Why not と出所が混ざるものは、出所 (issue / retro / レビューの番号・「〜の監査で」) だけを消し、壊れ方・実測・再提起の条件を残す
- [ ] 「何をしているか」だけのコメントは、触った行の近くにあれば消す。今回の主眼は経緯・出所で、説明コメントの全面の掃除はしない
- [ ] 許可リストの件数を下げる
- [ ] 挙動は変えない (コメントだけの diff であることを `git diff` と、言語ごとのビルド・lint で確かめる)

### 先に決めること (ユーザーの判断)

- **open な issue の再開条件を指すコメントの番号**: 起票時の既定案は「中身を書いたうえで open の番号は残す」だったが、**lint では open の番号も数える**ことにした
  (2026-10-18)。issue の状態で分けると、issue を done へ送っただけの commit で、その番号を書いたファイルの件数が一覧を超えて Lint と Tests が赤になり、
  別の作業を止める (敵対的レビューの数え: 141 行 / 26 issue)。番号を残すこと自体は禁じていない: 書き直しでは open の issue の番号は中身を書いたうえで
  残してよく、残した分は一覧の件数で受ける。新しく書くなら `--update --allow-increase` でレビューに見せる
- `_claude/` の rule / skill の本文 (Markdown) は対象外 (lint も Markdown を見ない)
- テストのコメントも同じ基準 (lint は tests/ も数える)

## 関連ファイル

- `CLAUDE.md` の「何をどこに書くか」
- `~/.claude/rules/pending-issue-rationale-in-code.md` / `~/.claude/rules/comment-no-restate-enforced.md` (コメントに残すものの既存の規範)

## 進捗

- [x] 2026-10-18 起票 (骨組み → 本文)。反証レビュー (sonnet 1 体): 数・例のファイル・lint の不在・既存の同趣旨 issue の不在は反証できなかった
  (`issue NNN` は HEAD が進んで 2,117)。採用: 走査から vendor / testdata を外す・`--` を Lua に限る・shebang と指令を数えない・
  ラチェットの初期値は lint 自身の数で作る (P1 2 件・P2 3 件)。P3 2 件 (「前は」・`codex` の誤検出) は本文で断り済み
- [x] 2026-10-18 1 (lint): `scripts/check_comment_provenance.sh` / `scripts/comment_provenance_allowlist.txt` / `tests/scripts/test_check_comment_provenance.sh`、
  Makefile の `test-comment-provenance` (test-lint の並列)、`scripts/CLAUDE.md` の検査の一覧、ルートの CLAUDE.md の検査の名前 (`make lint` の `check-comment-provenance` と
  書いてあったのを実際の入口に直した)。一覧はファイルごとの <件数><TAB><指紋><TAB><path> (4,188 件 / 1,095 ファイル)。
  - 敵対的レビュー (opus) を 4 周: 1 周目 2 体 (素通り / 壊れ) で走査の漏れ (.awk .ci など)・表記ゆれ・一覧の重複・awk の失敗を緑に畳む・`--update` が
    増加を吸収・done への移動で赤になる罠。2 周目で「敵対レビュー」(的なし) の漏れ・減少で余った枠・rename で赤。3 周目で 0 件の減少が gone 扱い・
    件数の一致だけで rename を組む。それぞれ直して変異で red を確認 (1 周目の直しで 8 本、2 周目で 5 本、3 周目で 2 本)。
    LC_ALL=C の awk で日本語に `?` を掛けると最後の 1 バイトにしか効かない罠を踏んだ (`敵対的?` が「敵対レビュー」に当たらなかった)
  - 記録のみ (検出しない形として script の冒頭に書いた): 行末のコメント・1 桁の番号・複数形の issues・単独の (P2)・消して同じ名前で作り直す形 など
- [x] 2026-10-18 2 (書き直し): 領域ごとに sonnet に任せ (worktree を分けて 7 体)、差分がコメント行と空行だけであることを機械で確かめてから
  取り込んだ。commit (一覧のファイルを除いた数): pro-con (269 ファイル) / tests (154) / glogx (203) / mac-lan-manager・scripts・zundamon-kaisetsu (127) /
  tuikit・_claude・bin・zshlib・Makefile・.github ほか (210) / lockman・doctor・treefiler (132)。一覧は 4,188 → 24 件。
  検閲で直したもの: 出所の語だけを消して宙に浮いた「(2026-09-10 の)」「(実測は)」の 5 か所・仕様を裸のパス (`issues/done/091-…md`) で
  指した 1 行 (「仕様 091」に)。テスト: rest / heavy の 2 レーン・glogx / tuikit / lockman / doctor / treefiler の go test・make test-lint
  - 踏んだ事故: `git apply --3way` が変更を index に置き、未ステージの差分の一覧で commit のファイルを拾ったため、pro-con の 1 回目の commit
    (一覧を下げただけ) が 269 ファイルを漏らした。続けて直した (CI の Lint が赤だったのは 1 commit の間)。パッケージの説明から番号を消すと
    生成物の src/PACKAGES.md が古くなる (test-go-packages-index が赤になった。作り直した)
  - 残した 24 件 (一覧で受ける。経緯ではない): pro-con の設定 review の機能名「敵対的レビューの担い手」19 行・mac-lan-manager の open な issue 747
    (見た目と要件の正本) 3 行・tests/claude/test_issue_progress_check.sh の fixture のデータの番号 1 行・glogx の issuesview の
    貼り付ける参照文字列の例 (`issue 415`) 1 行
  - 検査が数えない形で残っているもの (未対応): 語彙の外の経緯 (「2026-09-01 に指摘された」「前の版では」など)・行末のコメント・
    裸の番号 (`438 の「記録からは消さない」` の形)。必要になったら語彙を広げる
- [x] 2026-10-18 CI: 書き直しの最後の commit (a1835748) で各 module の workflow 22 本・unused・doctor が緑、その後の d905fb36 で Lint と Tests が緑。完了
