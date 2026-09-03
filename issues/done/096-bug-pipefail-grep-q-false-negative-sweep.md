# 096 bug: `… | grep -q` が pipefail 下で判定を反転させる箇所を横断で潰す

起票日: 2026-08-22
種別: bug
優先度: **P2** (CI が正しい実装に対してランダムに red を出す。原因が実装側に見えるので調査が逸れる)

## 事実 (CI 実測)

run **32570242557** の `tests/claude/test_human_tasks_due.sh` が 1 件 red。
ログの証拠:

```
tests/claude/test_human_tasks_due.sh: line 47: printf: write error: Broken pipe
NG: pending/ の期限切れは出す — /期限切れ 2026-08-01.*016-human-blocked.*\[保留\]/ が出力に無い:
  ...
  期限切れ 2026-08-01  issues/pending/016-human-blocked.md [保留]   ← 期待パターンは出力に**ある**
```

`printf '%s' "$got" | grep -Eq "$want"` の形。**`grep -q` は一致した瞬間に exit する**ため
書き手が EPIPE を受け、`set -o pipefail` 下では**一致していてもパイプライン全体が非 0** になる。
判定が反転し、正しい実装が red になる。

手元で決定的に再現できる (書き手が 2 回に分けて書けば必ず起きる):

```sh
set -euo pipefail
slow() { printf "MATCH\n"; sleep 0.3; printf "tail\n"; }
slow | grep -Eq MATCH && echo OK || echo "NG (一致しているのに偽)"   # => NG
```

CI で出やすいのは、スケジューリング次第で書き込みが分割されるため。

## 対応済み

`tests/claude/test_human_tasks_due.sh` と `tests/claude/test_retro_open.sh` の 5 箇所を
here-string (`grep -Eq "$want" <<<"$got"`) に直した。判定ヘルパー `check()` には
再発防止のコメントを残した。

## 残っている作業 — 同型の横断

**この罠は repo が既に 2 箇所で警告コメントを残していたのに、横展開されていなかった**:

- `scripts/lib/tmux_resurrect_guards.sh:43` 「ここを `printf … | grep -q` のパイプに戻さないこと」
- `tests/tmux/test_log_kill_command.sh:157` 「`ps | grep -q` のパイプにしないこと」

`set -*pipefail` を持つファイルで `| grep -q` を**判定に使っている**箇所は他に 30 前後ある
(`tests/tmux/*` の `print -r -- "$x" | grep -q …` が多数、`tests/nvim/*`、`tests/setup/*`、
`scripts/check_ci_group_deps.sh` など)。危険度は書き手が「一致より後ろに書き続けるか」で決まる:

- **危険**: 書き手が複数回 write する / 出力が大きい / 外部コマンドが遅い
- **ほぼ無害**: 一致が最終行にある / 出力が数十バイト / 書き手が即終了する

ただし**無害な側も「たまたま」であって構造は同じ**なので、機械的に here-string へ倒すのが安全。

## 提案

1. 上記の全箇所を here-string / `[[ "$x" =~ … ]]` / `grep -q pattern file` へ機械的に置換する
   (zsh 側は `<<<` がそのまま使える)
2. **再発を lint で止める**: `set -*pipefail` を持つ shell script で
   `\| *grep[^|]*-[A-Za-z]*q` にマッチしたら落とす静的検査を `make test` に足す。
   🚨 1 を先に済ませないと大量に落ちるので順序が要る。
   🚨 検査自体が CI で実際に走っているかを、同じ commit で確認すること
   ([`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) の節 4)
3. 例外が要る箇所 (意図的にパイプが必要) は `# shellcheck` 風の明示コメントで除外する

## なぜ P2 か

落ちるのは「たまに」で、しかも**原因が実装側の不具合に見える**。今回も別セッションが
「自分の変更のせいか」を疑う往復が発生した。テストハーネスの故障を実装の誤りとして
報告する形なので、debug のコストが本来の 2 倍以上になる
([`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
の「ハーネス自身の失敗を緑・赤に畳まない」と同型の問題)。

## 出典

`dotfiles-6c` セッションからの指摘 (2026-08-22)。原因の特定は先方、実ログでの裏取りと
`tests/claude` の修正・横断調査は `dotfiles-87` (本 issue の起票者)。

## 対応 (2026-08-22 完了)

「提案」の 1〜3 をすべて実施した (`dotfiles-6c` セッション。着手前に起票者へ担当確認済み)。

### 1. 全置換 (74f8a62) — 45 箇所

  printf '%s' "$x" | grep -q PAT   →  grep -q PAT <<< "$x"
  print -r -- "$x" | grep -q PAT   →  grep -q PAT <<< "$x"
  cmd | grep -q PAT                →  grep -q PAT <<< "$(cmd)"

内訳: tests/tmux 26 / tests/nvim 6 / scripts 5 / tests/setup 3 /
tests/zshrc/tmux-session 2 / bin 2 / tests/bin 1。

**件数の訂正**: 本文の「30 前後」は過少で、実際に pipefail 下で該当したのは 46 箇所。
一方で素朴に grep すると 97 箇所出る。差の 51 箇所は**テストが作る mock スクリプトの
heredoc 本文**で、別プロセスの sh で走り pipefail を継承しないため判定は反転しない
(`tests/zshrc/av1ify/test_helper.sh` の 24 件など)。46 のうち 1 件は誤検知
(`tests/tmux/test_tmux.sh:81` の `|| grep -qi "error" "$log"` はパイプではなくファイル引数)。

### 2. 静的検査 (d6f96b0)

`scripts/check_pipefail_grep_q.sh` を新設し `make test-lint` へ配線した
(= `make test` と CI の Lint job が叩く)。185 ファイルを走査する。

除外: heredoc 本文 / `||`・`&&` 形 / pipefail 無しファイル / 行内 `pipefail-grep-q: allow`。
「検査できなかったのに緑」を作らないため、依存コマンド不在・発見の失敗・対象 20 件未満・
読めないファイル・awk の失敗はすべて失敗にした。

**CI で実際に走ることを確認済み**: `make test-lint` の出力に検査行が出る。
**落ちることも変異で確認済み**: 置換を 1 箇所戻すとその行を指して red、復元で green。

### 3. 例外の逃げ道

行内 `pipefail-grep-q: allow` (理由を添える)。検査器自身の説明文が自己検出されたため
実際に使っている。🚨 **マーカーは同じ行に置く**こと (行単位で見るので前行では効かない)。

## 学び (次に同型を作らないために)

- **機械変換は失敗した**。正規表現の一括置換で 2 箇所、壊れた出力を作った:
  多段パイプの分断 (`grep -vE '^#' | grep -q probe <<< 'probe'` = 前段が入力を失う) と、
  here-string の付与漏れ (`grep -qaE "…"` が**入力を持たない grep** になる)。
  どちらも構文エラーにならず、テストが通り得る形だった。全 revert して 1 行ずつ
  明示的に書き換え直した (アンカーの一意一致を assert する方式)
- 素朴な grep の件数 (97) と実危険の件数 (45) は **2 倍以上ずれる**。heredoc 内の mock を
  数に入れると「大量にある」と誤認する
