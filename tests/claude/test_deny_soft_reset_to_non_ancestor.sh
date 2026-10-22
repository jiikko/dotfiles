#!/usr/bin/env bash
# shellcheck disable=SC2016 # 検査に渡すコマンドの文字は展開させずにそのまま書く ($wt を含む)
# _claude/hooks/deny-soft-reset-to-non-ancestor.sh (PreToolUse(Bash): HEAD の祖先でない行き先への git reset --soft を deny) の
# unit テスト。使い捨ての repo に「HEAD の祖先でない origin/master」を作り、合成した hook JSON で deny / allow を pin する。
#
# なぜ: 祖先でない行き先へ soft reset すると、index が他のセッションの commit を打ち消す差分になる。脅威モデルと
# 「検出しない形」はフック冒頭が正本。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-soft-reset-to-non-ancestor.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
HOOK_TIMEOUT=10 # 本番の配線 (settings.json) と同じ上限
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
# 継承した GIT_DIR などが残ると、使い捨ての repo でなく継承した repo を書き換える (sandbox-real-destructive-test-apis.md)
# shellcheck source=bin/lib/git_location_env.sh
. "$ROOT_DIR/bin/lib/git_location_env.sh"
git_location_env_unset || { printf '✗ git の場所の環境変数を外せない\n'; exit 1; }
[ -x "$HOOK" ] || { printf '✗ フック本体が無い / 実行権限が無い: %s\n' "$HOOK"; exit 1; }
command -v jq >/dev/null 2>&1 || { printf '✗ jq が無いため判定を検査できない (フック自体も jq 不在時は無効)\n'; exit 1; }
jq -e '[.hooks.PreToolUse[]? | select((.matcher // "") | test("Bash")) | .hooks[]?.command // ""]
       | map(test("deny-soft-reset-to-non-ancestor")) | any' "$SETTINGS" > /dev/null \
  || { printf '✗ settings.json の PreToolUse(Bash) にフックが配線されていない (防御が丸ごと無効)\n'; exit 1; }
printf '✓ settings.json の PreToolUse(Bash) に配線されている\n'

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
repo="$WORK/repo"
other="$WORK/other"
mkdir -p "$repo" "$other"
g() { git -C "$repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false "$@" >/dev/null; }
g init -q -b master
g commit -q --allow-empty -m base
base=$(git -C "$repo" rev-parse HEAD)
g commit -q --allow-empty -m upstream # 他のセッションの commit
g update-ref refs/remotes/origin/master HEAD
g update-ref refs/remotes/origin/old "$base"
g reset -q --hard "$base"
g commit -q --allow-empty -m mine # 自分の commit (起点は base。origin/master は祖先でない)
git -C "$repo" merge-base --is-ancestor origin/master HEAD && { printf '✗ 前提: origin/master が HEAD の祖先になっている\n'; exit 1; }
git -C "$repo" merge-base --is-ancestor origin/old HEAD || { printf '✗ 前提: origin/old が HEAD の祖先でない\n'; exit 1; }
# 2 つ目の repo: origin/master が HEAD の祖先 (ここで確かめた結果を、別の repo の reset に使わせない)
repo2="$WORK/repo2"
mkdir -p "$repo2"
git -C "$repo2" init -q -b master
git -C "$repo2" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q --allow-empty -m one
git -C "$repo2" update-ref refs/remotes/origin/master HEAD

fail=0
checked=0
# judge <cwd> <command>: deny / allow / error:<rc> を出す
judge() {
  local out rc=0
  out=$(jq -n --arg c "$2" --arg d "$1" '{tool_name: "Bash", cwd: $d, tool_input: {command: $c}}' | "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$BASH_BIN" "$HOOK") || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "error:$rc"
  elif [ -z "$out" ]; then
    echo allow
  elif printf '%s' "$out" | jq -e '.hookSpecificOutput.permissionDecision == "deny"' > /dev/null 2>&1; then
    echo deny
  else
    echo "error:unexpected-output"
  fi
}
expect() { # expect <deny|allow> <cwd> <説明> <command>。本番の bash (PATH の bash) と /bin/bash 3.2 の両方で流す
  local got bin
  for bin in "$(command -v bash)" /bin/bash; do
    BASH_BIN=$bin
    got=$(judge "$2" "$4")
    checked=$((checked + 1))
    if [ "$got" = "$1" ]; then
      printf '✓ %s (%s): %s\n' "$1" "$bin" "$3"
    else
      printf '✗ %s を期待したが %s (%s): %s\n    %s\n' "$1" "$got" "$bin" "$3" "$4"
      fail=1
    fi
  done
}

# --- 止める形 -------------------------------------------------------------------------------------------
expect deny "$repo" "祖先でない origin/master へ soft reset" 'git reset --soft origin/master'
expect deny "$repo" "-q とリダイレクト付き" 'git reset -q --soft origin/master 2>/dev/null'
expect deny "$other" "別の dir から -C で" "git -C $repo reset --soft origin/master"
expect deny "$other" "cd してから" "cd $repo && git status --short && git reset --soft origin/master"
expect deny "$repo" "同じコマンドの中で fetch の後 (hook の時点の ref では判定できない)" 'git fetch -q origin && git reset --soft origin/master'
expect deny "$repo" "fetch の後は、今は祖先の ref でも判定できない" 'git fetch -q origin && git reset --soft origin/old'
expect deny "$repo" "dir が変数で決まらない" 'cd $wt && git reset --soft origin/master'
expect deny "$repo" "オプションが行き先の後ろ" 'git reset origin/master --soft'

expect deny "$repo" "行き先を引用符で囲む" 'git reset --soft "origin/master"'
expect deny "$repo" "--soft の省略形" 'git reset --so origin/master'
expect deny "$repo" "env -u で前置き" 'env -u GIT_DIR git reset --soft origin/master'
expect deny "$repo" "括弧の中" '(git reset --soft origin/master)'
expect deny "$repo" "if の中" 'if true; then git reset --soft origin/master; fi'
expect deny "$repo" "行の継続" 'git reset \
  --soft origin/master'
expect deny "/" "-C の後に -c" "git -C $repo -c x=y reset --soft origin/master"
expect deny "/" "--git-dir で repo を指す" "git --git-dir=$repo/.git --work-tree=$repo reset --soft origin/master"
expect deny "/" "GIT_DIR= で repo を指す" "GIT_DIR=$repo/.git git reset --soft origin/master"
expect deny "$repo" "merge-base の結果を || で使う (祖先でないときだけ reset)" 'git merge-base --is-ancestor origin/master HEAD || git reset --soft origin/master'
expect deny "$repo" "覚えた後に fetch した" 'git rebase origin/old && git fetch -q && git reset --soft origin/old'
expect deny "$repo" "別の repo で確かめた結果" "git -C $repo2 merge-base --is-ancestor origin/master HEAD && git reset --soft origin/master"
expect deny "$repo" "rebase --onto の upstream を覚えない" 'git rebase --onto origin/old origin/master && git reset --soft origin/master'
expect deny "$repo" "remote update は fetch の類" 'git remote update && git reset --soft origin/old'
expect deny "$repo" "pull_main_checkout は fetch の類" '/Users/koji/dotfiles/scripts/pull_main_checkout.sh && git reset --soft origin/old'
expect deny "$repo" "行き先にリダイレクトがくっつく" 'git reset --soft origin/master>/dev/null'
expect deny "$other" "pushd してから" "pushd $repo && git reset --soft origin/master"

# --- 通す形 ---------------------------------------------------------------------------------------------
expect allow "$repo" "HEAD からの相対" 'git reset --soft HEAD~1'
expect allow "$repo" "祖先の行き先" 'git reset --soft origin/old'
expect allow "$repo" "先に rebase した" 'git rebase origin/master && git reset --soft origin/master'
expect allow "$repo" "実行時に自分で確かめている" 'git fetch -q origin && git merge-base --is-ancestor origin/master HEAD && git reset --soft origin/master'
expect allow "$repo" "soft でない reset" 'git reset --hard origin/master'
expect allow "$repo" "パスの reset" 'git reset -- --soft'
expect allow "$repo" "解決できない行き先 (git reset 自体が失敗する)" 'git reset --soft no-such-ref'
expect allow "$repo" "commit message の途中の文" 'git commit -F - <<'"'"'M'"'"'
fix: `git reset --soft origin/master` を打つ前に rebase する
M'
expect allow "$repo" "reset を含まないコマンド" 'git log --oneline -3'
expect allow "$repo" "rebase の出力をリダイレクトしてから" 'git rebase origin/master > /tmp/x.log 2>&1 && git reset -q --soft origin/master'
expect allow "$repo" "dotfiles の作業の形" 'git fetch -q origin && git rebase origin/master > log 2>&1; echo rc=$?; git merge-base --is-ancestor origin/master HEAD && git reset -q --soft origin/master && git restore --staged .'

# --- 区切り・dir・書き方の変種 ----------------------------------------------------------------------------
expect deny "$repo" "; の後ろへは確かめを持ち越さない" 'git merge-base --is-ancestor origin/master HEAD; git reset --soft origin/master'
expect deny "$repo" "|| を挟んだ rebase は覚えない" 'git rebase origin/master || true && git reset --soft origin/master'
expect deny "$repo" "! で反転した確かめは覚えない" '! git merge-base --is-ancestor origin/master HEAD && git reset --soft origin/master'
expect deny "$repo" "分からない dir どうしで確かめを取り違えない" 'cd $a && git merge-base --is-ancestor origin/old HEAD && cd $b && git reset --soft origin/old'
expect deny "$repo" "--soft の最短の省略形" 'git reset --s origin/master'
expect deny "$repo" "popd の後は dir が分からない" 'popd && git reset --soft origin/old'
expect deny "$repo" "cd - の後は dir が分からない" 'cd - && git reset --soft origin/old'
expect deny "$repo" "括弧の中の cd の後" "(cd $other && git reset --soft origin/old)"
expect deny "$repo" "git を絶対パスで" '/usr/bin/git reset --soft origin/master'
expect deny "$repo" "git を \\ で alias 回避" '\git reset --soft origin/master'
expect deny "$repo" "time -p の前置き" 'time -p git reset --soft origin/master'
expect deny "$repo" "&> がくっつく" 'git reset --soft origin/master&>/dev/null'
expect deny "$repo" "^{commit} の } を落とさない" 'git reset --soft origin/master^{commit}'
expect deny "$other" "cd -P でも dir を解決する" "cd -P $repo && git reset --soft origin/master"
expect deny "$repo" "heredoc の本文の cd は数えない" 'cat <<'"'"'E'"'"' > /dev/null
cd / || exit 1
E
git reset --soft origin/master'
big=$(printf 'x%.0s' $(seq 1 30000))
expect deny "$repo" "3 万字の heredoc の後でも timeout 内に判定する" "cat <<'E' > /dev/null
$big
E
git reset --soft origin/master"
expect deny "$repo" "3 万字の 1 行の後でも timeout 内に判定する" "echo $big; git reset --soft origin/master"

expect allow "$repo" "if の条件で確かめてから" 'if git merge-base --is-ancestor origin/master HEAD; then git reset --soft origin/master; fi'
expect allow "$repo" "rebase -X の値を行き先と読まない" 'git rebase -X theirs origin/master && git reset --soft origin/master'
expect allow "$other" "cd -P で祖先の行き先" "cd -P $repo && git reset --soft origin/old"
expect allow "$repo" "dir が分からなくても -C の絶対パスで判定する" "cd \$wt && git -C $repo reset --soft origin/old"
expect allow "$repo" "{ } で囲む" '{ git reset --soft origin/old; }'

# --- 3 周目: 変数の行き先・--keep-base・wrapper の fetch・|| の後・env -C・command -p・行数 ---------------------
expect deny "$repo" "行き先がコマンド置換" 'git reset --soft $(git rev-parse origin/master)'
expect deny "$repo" "行き先が変数" 'base=$(git rev-parse origin/master); git reset --soft "$base"'
expect deny "$repo" "行き先がバッククォート" 'git reset --soft `git rev-parse origin/master`'
expect deny "$repo" "rebase --keep-base は祖先にしない" 'git rebase --keep-base origin/master && git reset --soft origin/master'
expect deny "$repo" "wrapper の後ろの fetch" 'runtimeout 60 git fetch && git reset --soft origin/old'
expect deny "$repo" "|| の後の rebase は走らないことがある" 'true || git rebase origin/master && git reset --soft origin/master'
expect deny "$other" "env -C で repo を指す" "env -C $repo git reset --soft origin/master"
expect deny "$repo" "command -p の前置き" 'command -p git reset --soft origin/master'
lines=$(printf 'x\n%.0s' $(seq 1 2500))
expect deny "$repo" "2500 行の後でも timeout 内に判定する" "git commit -m \"$lines\" && git reset --soft origin/master"
expect allow "$repo" "merge-base で作る行き先は必ず祖先" 'git reset --soft $(git merge-base HEAD origin/master)'
expect allow "$repo" "コメントだけの行で確かめを忘れない" 'git rebase origin/master && # 起点を揃える
git reset --soft origin/master'
expect allow "$repo" "soft でない reset の変数の行き先" 'git reset --hard $(git rev-parse origin/master)'

# --- 4 周目: merge-base の例外の範囲・コメントと空の区切り・wrapper の fetch の書き方・env の束・パイプの中の cd -----------
expect deny "$other" "merge-base は shell の cwd で走るので、-C 先の reset には使えない" "git -C $repo reset --soft \$(git merge-base HEAD origin/master)"
expect deny "$repo" "HEAD を相手にしない merge-base" 'git reset --soft $(git merge-base origin/master origin/old)'
expect deny "$repo" "コメントの中の merge-base を例外に数えない" 'git reset --soft "$base"  # base = --soft $(git merge-base HEAD origin/master)'
expect deny "$repo" "コメントの中の && で繋がない" 'git merge-base --is-ancestor origin/master HEAD # ok &&
git reset --soft origin/master'
expect deny "$repo" "|| と確かめの間の空行" 'true ||
git merge-base --is-ancestor origin/master HEAD && git reset --soft origin/master'
expect deny "$repo" "|| と確かめの間のコメント" 'true || # fallback
git rebase origin/master && git reset --soft origin/master'
expect deny "$repo" "\$( ) の中の fetch" 'out=$(git fetch origin 2>&1) && git reset --soft origin/old'
expect deny "$repo" "fetch にリダイレクトがくっつく" 'runtimeout 60 git fetch>/dev/null 2>&1 && git reset --soft origin/old'
expect deny "$repo" "wrapper の後ろの \\git fetch" 'runtimeout 60 \git fetch && git reset --soft origin/old'
expect deny "$other" "env -C に値がくっつく" "env -C$repo git reset --soft origin/master"
expect deny "$other" "env の束の -iC" "env -iC $repo git reset --soft origin/master"
expect deny "$repo" "env -P の値を前置きと読まない" 'env -P /usr/bin git reset --soft origin/master'
expect deny "$repo" "--keep-base の省略形" 'git rebase --keep-b origin/master && git reset --soft origin/master'
expect deny "$repo" "パイプの中の cd は外へ効かない" "cd $other | cat; git reset --soft origin/master"
expect deny "$repo" "commit message の # の後ろの reset も読む" 'git commit -m "fix #12" && git reset --soft origin/master'
expect allow "$repo" "引用符の中の ; と cd は区切りにしない" 'git commit --allow-empty -m "wip; cd $wt" && git reset --soft origin/old'
expect allow "$repo" "&& の後の改行で確かめを持ち越す" 'git merge-base --is-ancestor origin/master HEAD &&
  git reset --soft origin/master'

# --- 5 周目: bash と awk の引用符の読みのずれ・merge-base の相手の数・\\ の後の改行 ---------------------------------------
expect deny "$repo" "\$( ) の中で引用符を数え直す形" 'v="$(grep version f | sed '"'"'s/"//g'"'"')"; git reset --soft origin/master'
expect deny "$repo" "\$'…' の中の \\'" 'echo $'"'"'it\'"'"'s'"'"'; git reset --soft origin/master'
expect deny "$repo" "取り切れない heredoc の本文の apostrophe" 'cat > f <<'"'"'END-MSG'"'"'
don'"'"'t
END-MSG
git reset --soft origin/master'
expect deny "$repo" "merge-base の相手が 3 つ" 'git reset --soft $(git merge-base origin/master origin/master HEAD)'
expect deny "$repo" "merge-base --independent" 'git reset --soft $(git merge-base --independent origin/master HEAD)'
expect deny "$repo" "\\\\ の後の改行は継続でない" 'echo a\\
git reset --soft origin/master'
expect allow "$repo" "閉じない引用符の中に soft reset が無い" 'git reset --soft HEAD~1; echo "unterminated; git log'
expect allow "$repo" "HEAD を後ろに書く merge-base" 'git reset --soft $(git merge-base origin/master HEAD)'

expect allow "$repo" "閉じない引用符の中の --stat と mixed の reset" 'v="$(jq .version p.json | tr -d '"'"'"'"'"')"; git diff --stat; git reset HEAD f'

if [ "$checked" -lt 192 ]; then
  printf '✗ 検査の件数が足りない (%d)\n' "$checked"
  fail=1
fi
[ "$fail" -eq 0 ] && printf '✓ deny-soft-reset-to-non-ancestor: %d 件\n' "$checked"
exit "$fail"
