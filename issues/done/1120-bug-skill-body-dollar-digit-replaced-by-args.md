# 1120 (bug): skill の本文の `$` + 数字が、起動時の引数に置き換わって式や例が壊れる

起票日: 2026-11-28

## 概要

Claude Code は skill を読み込むとき、SKILL.md の本文の `$0`・`$1` などを、起動時に渡した引数で置き換える。コードブロックの中も置き換わる。
awk の `$0` のように本文に書いたコマンドが、引数つきで起動したときだけ壊れる。

## 詳細（実測）

使い捨ての skill（本文に `[$0]` `[$1]` `[$99/yr]` `[${0:A:h}]` `[$NF]` `[${BASH_SOURCE%/*}]`）を作り、
`claude -p --model haiku "/dollar-probe ALPHA BRAVO"` で起こした（2026-11-28）。transcript の `isMeta` の user メッセージ（読み込まれた本文）は次のとおり。

| 書き方 | 読み込まれた本文 |
|---|---|
| `$0` | `ALPHA`（1 つ目の引数） |
| `$1` | `BRAVO`（2 つ目の引数） |
| `$99/yr` | そのまま（引数が 100 個無いので置き換わらない） |
| `${0:A:h}`・`$NF`・`${BASH_SOURCE%/*}` | そのまま |

- 置き換わるのは、引数のある位置の `$` + 数字だけ。`$` の後に `{` や英字が続く書き方は置き換わらない
- 引数なしで起動したときに `$0` がどうなるかは、この実測では見ていない（refresh-on-session を引数なしで起動した回は `$0` のまま読み込まれた）

## 見つけた箇所と対応

`git grep -nE '\$[0-9]' -- '_claude/skills/*/SKILL.md'` で 5 か所（4 skill）。

| skill | 箇所 | 引数つきで起動すると | 対応 |
|---|---|---|---|
| refresh-on-session | 綴りの揺れを拾う awk の `$0` 2 か所 | 式が構文エラーになる（pj_energy_matching で実際に踏んだ） | `$NF` で取る。別のセッションの commit「fix(refresh-on-session): 綴りの揺れを拾う awk で行を $NF で取る」 |
| claude-md-compact | 節ごとの大きさを出す awk の `$0` 2 か所 | 式が壊れる | `-F'\n'` で 1 行を 1 フィールドにし、`$NF` と `length()` で取る。dotfiles の CLAUDE.md・pj_energy_matching の CLAUDE.md・refresh-on-session の SKILL.md で、直す前と後の出力が同じことを diff で確かめた |
| mac-uninstall | 説明文の `$(dirname "$0")` | 例の `$0` が引数の語になる | 道具（`src/doctor/uninstall/usage.go` の `selfDirRe`）が拾う形を、置き換わらない書き方で並べ直した（`${0:A:h}`・`${BASH_SOURCE%/*}`・`dirname` でスクリプトの dir を取る形） |
| ios-app-developer | 表の `($99/yr)` | 引数が 100 個無いと置き換わらない | 対応しない（実測で壊れないと分かった） |

`_claude/commands/` の本文には `$` + 数字は無かった。

## 残り

- 再発を止める検査は入れていない。`$` + 数字を SKILL.md の本文で禁じる検査（`tests/claude/` の 1 本）を足すなら、`$99` のように実害の無い形と、
  コードブロックの中の正当な `$1`（awk・シェルの例）をどう扱うかを決めてから。今は 0 件
- 反証レビューは通していない（同じセッションで直して閉じた小さい bug。実測は上の表）
