# 552 (feat): worktree clean が tmp/ の使い捨てファイルだけを理由に worktree を残し続ける

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27、550 の初回の予定で `pro-con worktree clean --yes` を本物に回した) で分かったこと。
残した 26 個のうち 14 個は「消すと戻せない無視されたファイルがある」が理由で、その中身はほぼ `tmp/` の見本・変異のスクリプト・ログ
(`tmp/mut-456.log`・`tmp/pro-con-455-sample.py`・`tmp/493/*.ans` 等)。PG は作業のたびに `tmp/` へ書くので、
予定が毎日回るようになっても、この 14 個は減らず、同じ形で増え続ける。

## 今の形

- 492 の敵対的レビュー (P0) で、`git worktree remove` が --force なしでも無視されたファイルを消すことが分かり、無視されたファイルは
  空のディレクトリと go_autobuild の産物だけを通す形にした (`wtclean/git.go` の `rebuildable`。それ以外は `wtclean/judge.go` が残す側にする)。`.env`・`settings.local.json`・入れ子の repo を守るため
- `tmp/` は `~/.gitignore_global` で ignore されていて、dotfiles の `.gitignore` には無い (CLAUDE.md「一時ファイルの配置」)

## 対応方針 (案)

- `tmp/` の下だけは「使い捨て」として扱い、消してよい側に入れる案。その場合も、消す前に `refs/pro-con/removed/` のような退避が要るか
  (見本や計測のログを後で見たくなることはある) を決める
- 退避するなら置き場 (例: 状態の置き場の下に worktree の名前ごと) と、いつ消すか (N 日) を決める
- 🚨 `tmp/` の外の無視されたファイル (.env 等) は今までどおり残す。判定を広げすぎない
- 🚨 `tmp/` を使い捨てとするのは dotfiles の決まり (CLAUDE.md「一時ファイルの配置」)。pro-con が扱うほかの repo の `tmp/` が同じ意味とは限らない
  (本物のデータを置いている repo がありうる)。repo ごとに決める口 (config.toml の repo の設定か、repo 側の印) が要るかを先に決める

## 受け入れ条件

- [ ] `tmp/` の下のファイルだけが理由で残っていた worktree が、予定の片付けで消える (か退避してから消える)
- [ ] `tmp/` の外の無視されたファイルがある worktree は今までどおり残る

## 関連

- 492 (worktree clean) / 550 (予定) / `wtclean/git.go` (`rebuildable`) / `wtclean/judge.go`
