# 633 (test): bash 3.2 で `$var` の直後に全角文字を置く形を lint で落とす

起票日: 2026-10-03

## 概要

bash 3.2 は UTF-8 のロケールで `"…$rc。…"` の `rc` に続く全角の先頭バイトまで変数名として読む。
`set -u` の下では `rc�: unbound variable` でスクリプトがその行で死ぬ。
`set -u` が無いと、エラーも出ないまま `$rc` が空になり、全角の先頭バイトも消える (`echo "$rc。x"` は `\200\202x` を出す)。
この形は、テストの失敗メッセージ (assert が落ちたときにだけ走る行) に出やすい。
その場合、テストが落ちた理由 (rc や出力の末尾) を出す前に死ぬので、CI のログから原因が消える。

rule (`mutation-verify-new-tests.md` の「platform / シェル」節) に「`${var}` にする」と書いてあるが、
書いた後も再発している。機械で止める検査を `make test-lint` に足したい。

## 詳細

### 再発の記録

- 2026-09-27: retro 566 の項目 1。テストの `"($rc。2 は…)"` が `rc�: 未割り当ての変数です` で落ちた。
  対応は rule への追記だけだった
- 2026-10-02: CI の Tests (run 37031373755) で `tests/bin/test_mutate_verify.sh` の 48 節の失敗メッセージ
  `(rc=$rc。期待 4 …)` が `rc�: unbound variable` で死んだ。assert の発火そのものは別の原因 (プローブの race) で、
  その rc と `out.log` の末尾がログに残らなかった。commit「test(mutate-verify): INT のプローブを前景の子の自己 kill にし、$var の直後の全角を ${} で囲む」で直した
- 同じ commit で、同じ形の `「$E2E_ALONE_EXIT」` (`tests/pro-con/e2e_screen_killed.sh`) も直した。
  bash の shebang を持つ追跡ファイル 172 本を perl で走査して、該当はこの 2 か所だった (2026-10-03。走査に
  bash の shebang を持たない source 用のファイル、たとえば `tests/lib/*.sh` は入っていない)

### 起きる条件 (2026-10-03 に実測)

`env -i PATH=/usr/bin:/bin LC_ALL=<L> <bash> -c 'set -u; rc=3; echo "(rc=$rc。x)"'`:

| bash | LC_ALL=C | en_US.UTF-8 | ja_JP.UTF-8 |
|---|---|---|---|
| `/bin/bash` 3.2.57 | 通る | `rc�: unbound variable` | `rc�: unbound variable` |
| Homebrew bash 5.3.20 | 通る | 通る | 通る |

- 開発機の対話シェルから `bash x.sh` と打つと brew の 5 系が走るので、手元では再現しない。
  `#!/bin/bash` の shebang で直接起動される場合や、`/bin/bash` を名指しで起動する場合に 3.2 が走る
- `/bin/sh` (macOS では bash 3.2 の POSIX モード) でも同じく `rc�: unbound variable` で死ぬ。`LC_ALL` を付けず `LANG=en_US.UTF-8` だけでも起きる
- zsh 5.9.2 は 3 つのロケールとも `(rc=3。x)` を出して通る
- `。` に限らない。`…` `—` `é` `「` `・` `✓` `🚨` も同じく死ぬ (反証レビューの実測)

### shellcheck では止まらない

shellcheck 0.11.0 に、この形専用のルールは無い。`-o all` で出る `SC2250` (変数を常に `${}` で囲む) は
全ての `$var` に出る style の警告なので、そのまま gate にすると repo 全体を書き換えることになる。

## 対応方針

- `scripts/check_test_sleeps.sh` / `scripts/check_pipefail_grep_q.sh` と同じ形の検査
  `scripts/check_var_before_multibyte.sh` を足し、`make test-lint` の並列の一覧に `test-var-multibyte` として配線する
  - 検出: `$` + 識別子 (`[A-Za-z_][A-Za-z0-9_]*`) の直後に ASCII 以外のバイトがある箇所。`\$` でエスケープした `$` と、
    `${…}` の形は対象外
  - 対象: bash / sh で走るファイル。`scripts/discover_shell_scripts.sh` の結果に `tests/` 配下を足す
    (`check_pipefail_grep_q.sh` と同じ集め方)。source されるだけで shebang の無いファイルも bash で読まれるので含める
  - 🚨 **zsh のファイルは除く**。`discover_shell_scripts.sh` の結果には shebang の無い `zshlib/*.zsh` (20 件) が入り、
    そこには同じ形が 5 か所ある (`zshlib/_av1ify.zsh` 2 / `_av1ify_encode.zsh` 1 / `_av1ify_lock.zsh` 2。zsh では正しく動く)。
    拡張子 `.zsh` と zsh の shebang で除く (shebang だけでは zshlib を見分けられない)。
    除かないと、受け入れ条件の「今の repo で 0 件」が成り立たない
  - `set -u` の有無では対象を絞らない (無くても値が黙って化ける)
  - 意図的な例外は行内に `var-multibyte: allow` を書く (他の検査と同じ逃げ道)
  - **判定は文字列の中に限らない**。`echo $rc。` のような引用の外の形も同じく死ぬ
- 検査しないと決める形 (ヘッダに書く): heredoc の本文のうち、引用付きの区切り (`<<'EOF'`) で展開されないもの。
  コメント行 (`#` で始まる行)。引用の無い heredoc (`<<EOF`) の本文は展開されて死ぬので対象に含める
- 行単位の字句の検査なので誤検出する形がある (実測で通るのに当たる): 単引用符の中 (`'$rc。'`)、ANSI-C 引用 (`$'…$rc。'`)、
  行末コメント (`cmd # $x。`)。どれも今の repo では 0 件。単引用符を剥がしてから判定するか、`var-multibyte: allow` の逃げ道で
  受けるかを、検査を書くときに決める。`$1。` (位置パラメータ) / `$((rc))。` / `\$rc。` は通るので、検出規則の
  `[A-Za-z_]` 始まりと `\$` の除外で当たらない
- 検出 0 件の今の状態で緑になることに加え、fixture (上の 2 か所の旧形) で赤になることを変異で確かめる
- `mutation-verify-new-tests.md` の該当の行は、検査ができたら「dotfiles では `make test-lint` が止める」に寄せる

## 受け入れ条件

- [x] 検査が `make test-lint` から走り、走った証拠 (対象の件数) を出力する
- [x] 旧形 (`"(rc=$rc。期待"` / `「$E2E_ALONE_EXIT」`) を戻すと赤になる
- [x] 今の repo では 0 件で緑 (zsh のファイルを除いたうえで。コメント行の 2 件、`_claude/hooks/human-tasks-due.sh` と `tests/claude/test_git_state_verify.sh` は対象外)
- [ ] CI (Lint の workflow) のログに検査の出力行が出る

## 関連ファイル

- `scripts/check_pipefail_grep_q.sh` / `scripts/check_test_sleeps.sh` (同じ形の検査の手本)
- `Makefile` の `test-lint`
- `~/.claude/rules/mutation-verify-new-tests.md` の「platform / シェル」節

## 進捗

- 2026-10-03: 起票
- 2026-10-03: 反証レビュー (sonnet 1 体、読み取りのみ) を通した。実測表と参照は反証されなかった。指摘 4 件を反映し、うち 3 件は自分でも再現した
  (zshlib の 5 件 / `set -u` 無しで値が化ける / `/bin/sh` でも死ぬ)。反映した指摘: zsh の除外 (P1) / 単引用符などの誤検出 (P2) /
  `set -u` に限らない (P2) / `/bin/sh`・zsh・他の文字の実測 (P3)
- 2026-10-03: 実装した (commit「test(lint): bash 3.2 で $var の直後に全角を置く形を落とす検査を足す (633)」)
  - `scripts/check_var_before_multibyte.sh` を `make test-lint` (`test-var-multibyte`) に配線。今の repo で 224 ファイル 0 件。
    対象は discover + tests/ の *.sh・*.bats・拡張子なし + githooks/ (起票時の方針から githooks と .bats を足した)。
    zsh の shebang と、.sh / .bats 以外で sh / bash の shebang でないもの (zshlib/*.zsh) は除く
  - canary: 直す前の 2 行 (`4bd6999d^` の `test_mutate_verify.sh:862` と `e2e_screen_killed.sh:31`) を渡すと 2 行とも落ちる
  - 判定は行単位の正規表現ではなく、`$(…)` / `${…}` / `((…))` / `` `…` `` を文脈として積む字句の状態機械 (perl)。
    🚨 末尾で文脈か引用が閉じていないファイルは失敗にする (fail-closed。状態を見失うと後ろが黙って素通りするため)
  - テスト `tests/scripts/test_check_var_before_multibyte.sh` (81 件): fixture の判定に加え、各 fixture を本物の /bin/bash 3.2 で
    `LC_ALL=C` と `en_US.UTF-8` の 2 回走らせ、出力か rc が食い違う = 本物の違反、として検査の判定と 57 件で突き合わせる
    (近似の検査を、近似でない正解役で確かめる。正解役が構文エラーか無出力なら判定不能として落とす)
  - 変異 45 本 (状態機械の各枝・対象の選び方・失敗を緑にしない経路・正解役の guard) がすべて red
  - `make test-lint` / `make test` rc=0
- 2026-10-03: 敵対的レビュー (opus、読み取りのみ) を 3 周。全数勘定:
  - 1 周目: 素通り 4 (`${x% #*}` の `#` をコメントと読む / `find tests` の失敗が緑 / `<<!` など識別子でない区切り / heredoc の本文の `\\$rc`)・
    誤検出 1 (算術の `<<`)・対象漏れ (githooks・.bats)・空振りの変異 11。全部直した。行の継続だけは「検出しない形」へ
  - 2 周目: P1 1 (算術の中の `}` が `${` を閉じず、後ろが単引用符の中に見えて素通り。実ファイル 4 本の末尾 200 行あまりが無検査だった)・
    誤検出 1 (`$$rc`)・空振りの変異 6。同じ不変条件 (状態を見失わない) を 2 周続けて破られたので、近似の直しをやめて
    ①字句解析を文脈のスタックにする ②末尾の fail-closed ③bash 3.2 を正解役にした突き合わせ、に作り直した
  - 3 周目: 素通り 2 (二重引用符の中の入れ子の `${` で `'` が引用を開く / heredoc を始めた行で複数行の引用を開く)・
    誤検出 2 (`!((` / `$(case … a) …)`)・正解役の空振り 4・空振りの変異 6。`$(…)` の中の case の `)` は「検出しない形」へ、残りは直した
  - 3 周目の後は回していない: 字句の gate は迂回が原理的に無限で、周を重ねても収束しない (rule `adversarial-review-own-safeguards.md` §8)。
    代わりに軸を本物 (bash 3.2 の正解役 + 末尾の fail-closed) へ移した。3 周目の修正はどれも fixture を足し、正解役で直接確かめた
  - 却下・記録: 2 周目の「`$(…)` / `${…}` の中の入れ子の引用はヘッダの『検出しない形』」はスタック化で追えるようにした。
    3 周目の「`` ` `` の pop で引用の状態を見ない方が bash に近いかもしれない」と「算術の中の `$'`」は、実害が無く確かめていないので変えていない
- 2026-10-03: CI (034fd6ab) の Lint / Tests / Bench / unused / Karabiner Lint がすべて success。Lint のログに `✓ $var の直後の ASCII 以外の文字: 224 ファイルに該当なし` が出ている。done へ
