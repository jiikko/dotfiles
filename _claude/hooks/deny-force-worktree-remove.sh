#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: `git worktree remove` に `--force` / `-f` を付けた Bash を deny する。
#
# なぜ: `--force` は未コミットの変更と untracked のファイルごと worktree を消し、object にも残らない。`--force` を付けなければ、
# git が実行の時点で「modified or untracked files」を見て拒否する (ignore されたファイル (tmp/) だけなら消せる。2026-11-06 に dotfiles の
# worktree で実測)。同じ Bash の中で commit してから消す形でも、commit が走らなかったときは git が止める。汚れているかの判定は git が
# 持っているので、hook は自前で判定せず `--force` だけを止める (adversarial-review-own-safeguards の §0-B)。
# 起点: commit が rebase の失敗で走らず、`Everything up-to-date` の push の後の `worktree remove --force` が未コミットの本文を消した。
#
# ## 脅威モデル (adversarial-review-own-safeguards.md §8)
#
# 守るのは「Claude が Bash ツールで直接打つ `git … worktree … remove|rm … --force|-f`」だけ。heredoc の本文を落とし、`\` + 改行を畳み、
# `;` `&` `|` 改行で区切った区切りごとに、空白で割った語の並びを見る:
#   1. 語が `git` (`/usr/bin/git` のようにパスの末尾・`out=$(git` のように代入やコマンド置換の後ろでもよい)
#   2. その後ろに `worktree`、さらに後ろに `remove` か `rm`
#   3. `worktree` より後ろに `--force` (git が受ける省略形 `--f`〜`--forc` と `--force=…` を含む) か、`-` 1 つで始まり f だけが続く語 (`-f` / `-ff`)。
#      または同じ区切りに、untracked を見えなくする設定の語 (`status.showUntrackedFiles`・`core.excludesFile`・`include.path` / `includeIf`。
#      `-c` / `--config-env` / `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_PARAMETERS` のどれでも。引用符は外して読む) か、設定ファイルを差し替える前置き
#      (`GIT_CONFIG=` `GIT_CONFIG_GLOBAL=` `GIT_CONFIG_SYSTEM=` `HOME=` `XDG_CONFIG_HOME=`) がある。untracked が見えないと、git は --force 無しでも
#      untracked ごと消す (status.showUntrackedFiles=no と、全部を ignore する core.excludesFile で 2026-11-06 に実測)
#   引用符で囲んだ語 (`"worktree"` / `"remove"`) も、引用符を剥がしてキーワードとして読む (引用符で囲んだサブコマンドで素通りさせない)。
#   git の大域オプションを構文どおりに読み飛ばさない: 値を空白で区切るオプション (`--attr-source HEAD`) や値の中のコマンド置換
#   (`-C "$(git rev-parse --show-toplevel)"`) で読み違え、後ろの worktree を見落とした
# 語の頭の `(` `{` `!` `$(` バッククォート・引用符と、末尾の閉じ括弧・引用符は剥がしてから比べる (`out=$(git …)` / `bash -c 'git …'` / `"--force"`)。
# 字面で見る粗い判定で、シェルの構文は真似しない (deny-piped-push-then-destroy.sh と同じ理由)。
#
# ## 検出しない形 (review で見る責務の側)
#   - 変数・関数・alias・eval・スクリプトの中の削除 (`G=git; $G worktree remove --force` / `bin/` のスクリプトが使い捨ての worktree を消す)
#   - シェルに食わせる heredoc の本文 (`bash <<'EOF'` の中の削除)。heredoc の本文を落とすのは commit message の文言で止めないため
#   - 字句を崩した書き方 (`status.showUntracked\Files` / `remove{,}`)
#   - 別の区切りで export した `GIT_CONFIG_*`、repo / global の設定にある `status.showUntrackedFiles=no` (その repo では --force 無しでも untracked ごと消える。dotfiles の _gitconfig は立てていないことをテストで固定)
#   - ignore されたファイル (--force の有無に関係なく消える。tmp/・global の gitignore が ignore するもの)
#   - 引用符で書いた `"git"`、git の大域オプションの値に空白を含む形、他の短いオプションと束ねた `-fq` の類
#   - `rm -rf <worktree のパス>` + `git worktree prune` での削除
#
# ## 分かっていて受ける偽陽性
#   - lock された worktree・submodule を初期化した worktree・中身を確かめて捨てると決めた worktree の削除。人が `!` で打つか、中身を commit してから消す
#   - 1 行の引用符の中の文言 (`echo "git worktree remove --force"`)・`git grep` / `rg` の検索語・コメントの中の文言。commit message 等は heredoc で渡し、
#     検索は語を崩して打つ (`git grep -n 'worktree.remove'`)
#   - 同じ区切りの後ろの語がたまたま rm (`git worktree move --force a rm`)・同じ区切りに status.showUntrackedFiles の語がある remove (`git worktree remove x # status.showUntrackedFiles`)
#   - 前置きで設定ファイルを差し替えた remove (`GIT_CONFIG_GLOBAL=/dev/null git worktree remove x`)。中身を読まずに止める
#
# 入力: PreToolUse の hook JSON (stdin)。.tool_input.command を検査する。
# 出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。jq が無い・読めないときは諦める (fail-open)。

set -uo pipefail

LIB="$(dirname "${BASH_SOURCE[0]}")/lib/hook_io.sh"
# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$LIB" 2>/dev/null || exit 0
hook_read_bash_command || exit 0
[ -n "$HOOK_CMD" ] || exit 0
case "$HOOK_CMD" in *worktree*) ;; *) exit 0 ;; esac

REASON="git worktree remove に --force / -f (または -c status.showUntrackedFiles) が付いています。--force は未コミットの変更と untracked のファイルごと消し、戻せません (同じ Bash で commit した後でも、commit が走らなければ消えます)。--force を外してください: git が実行時に、変更・untracked のある worktree の削除を拒否します (ignore されたファイルは消えます)。拒否されたら git -C <worktree> status で中身を見て、要るものは commit・push してから消してください。捨てると確かめたもの・lock された worktree は、人が ! で打ってください。規範: .claude/rules/worktree-per-session.md"

body=$(hook_strip_heredocs "$HOOK_CMD") || hook_deny "heredoc の本文を落とす処理が失敗した。判定できないので保守的に拒否した。git worktree remove を単独の呼び出しにすること"

if printf '%s\n' "$body" | LC_ALL=C awk '
  { buf = buf $0 "\n" }
  END {
    gsub(/\\\n/, " ", buf)          # 行の継続
    gsub(/[;&|]/, "\n", buf)         # 区切り
    n = split(buf, segs, "\n")
    for (s = 1; s <= n; s++) {
      m = split(segs[s], w, /[ \t]+/)
      g = 0; wt = 0; rm = 0; force = 0
      for (i = 1; i <= m; i++) {
        raw = w[i]
        t = raw; gsub(/^[(\{!$`"\047]+|[)\}`"\047]+$/, "", t)
        q = raw; gsub(/["\047]/, "", q); q = tolower(q)
        # status.showUntrackedFiles を変える指定 (-c / --config-env / GIT_CONFIG_KEY_n / GIT_CONFIG_PARAMETERS) と、設定ファイルを差し替える前置き
        if (q ~ /status\.showuntrackedfiles|core\.excludesfile|include(if)?[.]/ || q ~ /^(git_config(_global|_system)?|home|xdg_config_home)=/) force = 1
        if (!g) { if (t ~ /(^|[\/(`$=])git$/) g = 1; continue }
        if (!wt) { if (t == "worktree") wt = 1; continue }
        if (t == "remove" || t == "rm") rm = 1
        if (t ~ /^--f(o(r(ce?)?)?)?(=|$)/ || t ~ /^-f+$/) force = 1
      }
      if (g && wt && rm && force) { found = 1; break }
    }
    exit found ? 0 : 1
  }
'; then
  hook_deny "$REASON"
fi
exit 0
