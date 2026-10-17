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

### 1. lint `check-comment-provenance` を置く (CLAUDE.md が既に名指ししている)

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

- **open な issue の再開条件を指すコメント** (「issue NNN で X が直るまでこの回避を残す」) の番号を残してよいか。
  `~/.claude/rules/pending-issue-rationale-in-code.md` は「issue 番号だけ書いて中身を書かない」を ❌ とし、中身つきの番号は禁じていない。
  CLAUDE.md の新しい表は「レビュー指摘の番号」をコミットログへ回す。既定案: 中身 (制約と再評価の条件) を書いたうえで、まだ open の issue の番号は残す。
  done の issue・レビューの番号は消す
- `_claude/` の rule / skill の本文 (Markdown) は対象外か。既定案: 対象外 (コードコメントではない)
- テストのコメントも同じ基準か。既定案: 同じ (テストは What をテスト名と assert で言い、コメントは Why not だけ)

## 関連ファイル

- `CLAUDE.md` の「何をどこに書くか」
- `~/.claude/rules/pending-issue-rationale-in-code.md` / `~/.claude/rules/comment-no-restate-enforced.md` (コメントに残すものの既存の規範)

## 進捗

- [x] 2026-10-18 起票 (骨組み → 本文)。反証レビュー (sonnet 1 体): 数・例のファイル・lint の不在・既存の同趣旨 issue の不在は反証できなかった
  (`issue NNN` は HEAD が進んで 2,117)。採用: 走査から vendor / testdata を外す・`--` を Lua に限る・shebang と指令を数えない・
  ラチェットの初期値は lint 自身の数で作る (P1 2 件・P2 3 件)。P3 2 件 (「前は」・`codex` の誤検出) は本文で断り済み
