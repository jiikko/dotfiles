# 1044 (chore): 予算検査の「他の repo 層」の定数が実測から遅れ、検査が通るまま obaket のセッションが上限を超える

起票日: 2026-11-08

## 概要

`scripts/check_instruction_budget.sh` は global 層の上限を `LIMIT - OTHER_REPO_RESERVE` (150,000 − 39,000 = 111,000) で守る。
`OTHER_REPO_RESERVE` は手で測って置いた定数で、obaket の `macOS/` で起動したセッションの repo 層は、2026-11-08 の実測でこれを 719 字上回る。
global 層がこの検査に通っていても、そのセッションは上限を超えうる。

## 詳細

### 実測 (2026-11-08、umbrella `40821e2` / obaket `064efead5`)

数え方はスクリプト冒頭と同じ (`paths:` の無い CLAUDE.md と `.claude/rules/**/*.md` を `LC_ALL=en_US.UTF-8 wc -m`)。

| 層 | 2026-10-30 (スクリプトのコメント) | 2026-11-08 |
|---|---|---|
| my-products (umbrella) | 14,089 | 14,988 |
| obaket | 19,573 | 19,220 |
| obaket/macOS | 5,011 | 5,511 |
| 合計 | 38,673 | **39,719** |

- 9 日で 1,046 字増え、定数 39,000 を超えた。
- 同じ日の claude-md-compact の前は、global 層 110,979 + repo 層 40,752 = 151,731 字で、上限 150,000 を超えていた。
  global 層は 111,000 を下回っていたので、`make test-lint` の `test-instruction-budget` は通っていた。
- compact の後は global 層 109,476 + 39,719 = 149,195 字で、残りは 805 字。

### 構造の問題

- スクリプトのコメントと issue 414 は、定数にする理由を「CI からは他の repo を読めない」と書いている。
  測り直しの trigger は「他の repo で上限超えの警告が出たら」で、事後にしか効かない。
- 他の repo の repo 層は、dotfiles の外の commit で増える。dotfiles の CI はその増加を見ないので、定数は黙って古くなる
  (スクリプトの「検出しないもの」に既に書いてある)。

## 対応方針 (案)

CI は他の repo を読めない、という前提は変えない。そのうえで、取れる手は次の 3 つ。

1. **すぐやる**: 今の実測に合わせて `OTHER_REPO_RESERVE` を上げる (今の手順どおりの対処)。
   39,719 なら global 層の上限は 110,281 になり、global 層 109,476 は通る。残りは 805 字。
2. **測り直しを手から外す**: `gh api` で各 repo の層を取り、最も重いセッションの値を出すスクリプトを作る (clone は要らない)。
   gh の認証がある手元の入口 (`make test-lint` の手元の実行・pre-push) でだけ走らせ、定数より大きければ落とす。gh が使えないとき (CI) は、その旨を出して飛ばす。
3. **起点を他の repo の側へ**: obaket など重い repo の側に、自分の repo 層 + global 層の実測が 150,000 を超えたら落ちる検査を置く。
   増やした側の push で止まるので、事後の警告を待たない。

- 2 と 3 は重ねられる。どこまでやるか (2 と 3 のどちらか、または両方) はユーザーに決めてもらう。
- 1 だけでは、次に他の repo が増えたときにまた遅れる。

## 関連ファイル

- `scripts/check_instruction_budget.sh` (冒頭のコメント・`OTHER_REPO_RESERVE`)
- `Makefile` の `test-instruction-budget`
- [issue 414](done/414-docs-scope-global-rules-with-paths-under-150k.md) (上限の経緯と再開の trigger)

## 進捗

- [ ] 方針を決める (1 / 2 / 3 のどれを入れるか)
