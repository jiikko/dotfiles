#!/usr/bin/env bash
# shellcheck disable=SC2016 # 検査に渡すコマンドの文字は展開させずにそのまま書く ($HOME / $PWD / $(...) を含む)
# _claude/hooks/deny-tmp-root-write.sh (git の作業ツリーの root の tmp/ 直下へ書くのを deny) の unit テスト。
# 一時ディレクトリに git の repo を作り、合成した hook JSON を stdin で流して deny / allow を pin する。配線 (settings.json) も見る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/deny-tmp-root-write.sh"
SETTINGS="$ROOT_DIR/_claude/settings.json"
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
# shellcheck source=bin/lib/git_location_env.sh
. "$ROOT_DIR/bin/lib/git_location_env.sh"
git_location_env_unset

[ -x "$HOOK" ] || { printf '✗ フックが無いか実行権限が無い: %s\n' "$HOOK"; exit 1; }
command -v jq >/dev/null 2>&1 || { printf '✗ jq が無いため判定を検査できない (フックも jq 不在時は無効)\n'; exit 1; }
for m in Bash Write; do
  jq -e --arg m "$m" '[.hooks.PreToolUse[]? | select((.matcher // "") | split("|") | index($m)) | .hooks[]?.command // ""]
         | map(test("deny-tmp-root-write")) | any' "$SETTINGS" >/dev/null \
    || { printf '✗ PreToolUse(%s) の配下にフックが配線されていない\n' "$m"; exit 1; }
  printf '✓ settings.json の PreToolUse(%s) に配線されている\n' "$m"
done

FIX="$(mktemp -d)"
trap 'rm -rf "$FIX"' EXIT
FIX="$(cd "$FIX" && pwd -P)"
R="$FIX/home/repo"
mkdir -p "$R/tmp/sub" "$R/src/tmp" "$FIX/home/notrepo/tmp" "$FIX/home/notmp" "$FIX/home/other/tmp" "$R/build"
git -C "$R" init -q
git -C "$FIX/home/notmp" init -q
git -C "$FIX/home/other" init -q
git -C "$R" commit -q --allow-empty -m init
W="$FIX/home/wt"
git -C "$R" worktree add -q --detach "$W"
mkdir -p "$W/tmp"
ln -s "$R/tmp" "$FIX/home/scratchlink"

decision() {  # $1=tool $2=cwd $3=コマンドか file_path → deny / allow / timeout / error / harness-error (HOOK_ENV に env の引数を足せる)
  local out rc json
  json="$(mktemp)"
  if [ "$1" = Write ] || [ "$1" = Edit ]; then
    jq -n --arg t "$1" --arg w "$2" --arg p "$3" '{tool_name:$t,cwd:$w,tool_input:{file_path:$p,content:"x"}}' >"$json" 2>/dev/null
  else
    jq -n --arg w "$2" --arg c "$3" '{tool_name:"Bash",cwd:$w,tool_input:{command:$c}}' >"$json" 2>/dev/null
  fi || { rm -f "$json"; echo harness-error; return; }
  # shellcheck disable=SC2086 # HOOK_ENV は env へ渡す NAME=値 の並び。語に割るのが意図
  out="$(env HOME="$FIX/home" ${HOOK_ENV:-} "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" <"$json" 2>/dev/null)"
  rc=$?
  rm -f "$json"
  if [ -n "$out" ]; then
    printf '%s' "$out" | jq -r '.hookSpecificOutput.permissionDecision // "error"' 2>/dev/null || echo error
    return
  fi
  case "$rc" in
    0) echo allow ;;
    124) echo timeout ;;
    *) echo error ;;
  esac
}

FAIL_COUNT=0
expect() {  # $1=期待 $2=説明 $3=tool $4=cwd $5=コマンドか file_path
  local got
  got="$(decision "$3" "$4" "$5")"
  if [ "$got" != "$1" ]; then
    printf '✗ %s\n  期待=%s 実際=%s\n  input: %.200s\n' "$2" "$1" "$got" "$5"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return
  fi
  printf '✓ %s (%s)\n' "$2" "$1"
}

# --- deny: root の tmp/ の直下へ書く ---
expect deny "相対パスへ > (cwd が repo の root)" Bash "$R" 'make test > tmp/maketest.log 2>&1'
expect deny "./tmp/ の形" Bash "$R" 'go test ./... > ./tmp/t.log'
expect deny ">> で追記" Bash "$R" 'echo x >> tmp/a.log'
expect deny "2> で stderr だけ" Bash "$R" 'make 2> tmp/err.log'
expect deny "&> で両方" Bash "$R" 'make &> tmp/all.log'
expect deny ">| で上書き" Bash "$R" 'make >| tmp/a.log'
expect deny "> の後に空白が無い" Bash "$R" 'make >tmp/a.log'
expect deny "絶対パス" Bash "/" "git push origin HEAD:master > $R/tmp/push.log 2>&1; rc=\$?"
expect deny "引用符で囲んだ絶対パス" Bash "/" "git push > \"$R/tmp/push.log\" 2>&1"
expect deny "~ から書く" Bash "/" 'git pull > ~/repo/tmp/pull.log 2>&1'
expect deny "\$HOME から書く" Bash "/" 'git pull > $HOME/repo/tmp/pull.log 2>&1'
expect deny "\${HOME} から書く" Bash "/" 'git pull > ${HOME}/repo/tmp/pull.log 2>&1'
expect deny "tee" Bash "$R" 'make 2>&1 | tee tmp/out.log'
expect deny "tee -a" Bash "$R" 'make 2>&1 | tee -a tmp/out.log'
expect deny "tee の 2 つ目の引数" Bash "$R" 'make | tee tmp/sub/a.log tmp/b.log'
expect deny "&& の後ろのコマンド" Bash "$R" 'mkdir -p tmp/sub && make > tmp/x.log'
expect deny "worktree の root の tmp/" Bash "$W" 'make > tmp/x.log'
expect deny "tmp/ を経由して .. で戻る" Bash "$R" 'make > tmp/sub/../x.log'
expect deny "symlink 越しでも実体が root の tmp" Bash "/" "make > $FIX/home/scratchlink/x.log"
expect deny "Write で root の tmp/ 直下" Write "$R" "$R/tmp/report.md"
expect deny "Write で相対パス" Write "$R" "tmp/report.md"
expect deny "先頭の cd で repo へ入ってから" Bash "/" "cd $R && make test > tmp/x.log 2>&1"
expect deny "\$PWD から書く" Bash "$R" 'make test > "$PWD/tmp/x.log"'
expect deny "\$(pwd) から書く" Bash "$R" 'make test > "$(pwd)/tmp/x.log"'
expect deny "\$(git rev-parse --show-toplevel) から書く" Bash "$R/src" 'make test > "$(git rev-parse --show-toplevel)/tmp/x.log"'
expect deny "tmp/ がまだ無い repo の最初の 1 本" Bash "$FIX/home/notmp" 'mkdir -p tmp && make > tmp/x.log'
expect deny "Write で tmp/ がまだ無い repo" Write "$FIX/home/notmp" "$FIX/home/notmp/tmp/x.md"
expect deny "\${PWD} から書く" Bash "$R" 'make test > "${PWD}/tmp/x.log"'
expect deny "コメントの don't の後ろの行" Bash "$R" $'# don\'t keep logs here\nmake > tmp/x.log'
expect deny "先頭の cd の後の \$PWD は cd 先" Bash "/" "cd $R && make > \"\$PWD/tmp/x.log\""
expect deny "語の途中の # (\${#arr[@]})" Bash "$R" 'echo ${#arr[@]} > tmp/x.log'
expect deny "URL の #" Bash "$R" 'curl http://a/b#c > tmp/x.log'
expect deny "先頭の cd の先が \$(git rev-parse --show-toplevel)" Bash "$R/src" 'cd "$(git rev-parse --show-toplevel)" && make > tmp/x.log'
expect deny "先頭の cd の先が \$PWD/.." Bash "$R/src" 'cd "$PWD/.." && make > tmp/x.log'
expect deny "cd を語に含まない引数 (abc.cd / --cd / cd-rom) は cd と数えない" Bash "$R" 'ls abc.cd --cd cd-rom > tmp/x.log'
expect deny ">& で file へ" Bash "$R" 'make >& tmp/x.log'
expect deny "/usr/bin/tee" Bash "$R" 'make | /usr/bin/tee tmp/x.log'
expect deny "変数を含むファイル名でも dir が root の tmp" Bash "$R" 'make > tmp/$name.log'
HOOK_ENV="GIT_DIR=$FIX/home/other/.git GIT_WORK_TREE=$FIX/home/other" expect deny "継承した GIT_DIR が別の repo を指していても" Bash "$R" 'make > tmp/x.log'
# --- allow: 作業ごとの dir・関係ない場所 ---
expect allow "tmp/<作業名>/ の中" Bash "$R" 'make test > tmp/sub/maketest.log 2>&1'
expect allow "絶対パスで tmp/<作業名>/ の中" Bash "/" "git push > $R/tmp/sub/push.log 2>&1"
expect allow "2>&1 だけ" Bash "$R" 'make 2>&1'
expect allow "/dev/null" Bash "$R" 'make > /dev/null 2>&1'
expect allow "/tmp (repo でない)" Bash "$R" 'make > /tmp/x.log'
expect allow "repo でない dir の tmp/" Bash "$FIX/home/notrepo" 'make > tmp/x.log'
expect allow "root でない dir の tmp/" Bash "$R" 'make > src/tmp/x.log'
expect allow "heredoc の本文の例" Bash "$R" $'git commit -F - <<\'M\'\nmake > tmp/x.log の形を止める\nM'
expect allow "読むだけ (< tmp/x)" Bash "$R" 'wc -l < tmp/x.log'
expect allow "cat で読む" Bash "$R" 'cat tmp/x.log'
expect allow "変数を含むパス (検出しない形)" Bash "$R" 'L=tmp; make > $L/x.log'
expect allow "比較の > (算術)" Bash "$R" '[ $((n > 0)) -eq 1 ] && ls tmp/'
expect allow "Write で tmp/<作業名>/ の中" Write "$R" "$R/tmp/sub/report.md"
expect allow "Write で repo の外" Write "$R" "$FIX/home/notrepo/tmp/x.md"
expect allow "tmp/ が無く、親が root でない dir" Bash "$R" 'mkdir -p build/tmp && make > build/tmp/x.log'
expect allow "二重引用符の中の \\\" の後ろ" Bash "$R" 'git commit -m "say \"hi\" > tmp/x.log is bad" -- a'
expect allow "複数行の commit message の中の例" Bash "$R" $'git commit -m "first line\nmake > tmp/x.log を止める" -- a'
expect allow "コメントの what's の後ろの grep パターン" Bash "$R" $'# what\'s this\ngrep \'foo > tmp/bar\' x'
expect allow "先頭以外の cd (cwd を決められないので通す)" Bash "$R" 'grep -n foo a.txt && cd src && make > tmp/x.log'
expect allow "サブシェルの cd" Bash "$R" '(cd src && make > tmp/x.log)'
expect allow "; で繋いだ cd" Bash "$R" 'cd src; make > tmp/x.log'
expect allow "2 行目の cd" Bash "$R/src" $'make > tmp/x.log\ncd .. && ls'
expect allow "先頭の cd の後の \$PWD は cd 先 (root でない)" Bash "$R" 'cd src && make > "$PWD/tmp/x.log"'
expect allow "先頭の cd と別の cd が両方ある (先頭の cd だけを採ると誤って止める)" Bash "$R/src" 'cd .. && make > x.log && cd src && make > tmp/x.log'
expect allow "行継続の後の cd" Bash "$R" $'git status && \\\n  cd src && make > tmp/x.log'
expect allow "字下げした行の cd" Bash "$R" $'for d in src; do\n  cd $d\n  make > tmp/x.log\ndone'
expect allow "pushd" Bash "$R" 'pushd src >/dev/null && make > tmp/x.log'
expect allow "引数無しの cd を ; に繋ぐ" Bash "$R" 'cd; make > tmp/x.log'
expect allow "{ cd" Bash "$R" '{ cd src; make > tmp/x.log; }'
expect allow "if cd" Bash "$R" 'if cd src; then make > tmp/x.log; fi'
expect allow "command cd" Bash "$R" 'command cd src && make > tmp/x.log'
expect allow "tee の名前を持つ別のコマンド" Bash "$R" 'make | mytee tmp/x.log'
expect allow "先頭の cd で root でない dir へ入ってから" Bash "$R" 'cd src && make > tmp/x.log'
expect allow "grep のパターンの中の例" Bash "$R" "grep -rn 'make > tmp/x.log' docs/"
expect allow "読むだけ (grep の > パターン)" Bash "$R" "grep -c '>' tmp/x.md"
expect allow "tee を検索語に持つ grep" Bash "$R" 'grep -n tee tmp/build.log'
expect allow "commit message の中の例" Bash "$R" "git commit -m 'hook: make > tmp/x.log を止める' -- a"
expect allow "矢印の ->" Bash "$R" "echo 'tmp/a.log -> tmp/b.log'"
expect allow "here-string の中身" Bash "$R" "cat <<< 'a > tmp/b'"
expect allow "cwd が tmp の中で 2>&1" Bash "$R/tmp" 'make 2>&1'
expect allow "cwd が tmp の中で awk の比較" Bash "$R/tmp" "awk '\$1>5' x"
expect allow "Edit は止めない (既にあるファイルの編集)" Edit "$R" "$R/tmp/old.log"

if ((FAIL_COUNT > 0)); then
  printf '\n%d 件失敗\n' "$FAIL_COUNT"
  exit 1
fi
printf '\nAll deny-tmp-root-write tests passed successfully!\n'
