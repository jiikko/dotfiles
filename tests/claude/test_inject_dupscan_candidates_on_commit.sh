#!/usr/bin/env bash
# _claude/hooks/inject-dupscan-candidates-on-commit.sh (PostToolUse(Bash): いま作った commit で足した関数に、名前の近い既存の定義を注入する) の unit テスト。
#
# なぜ: 注入が出なくなる退行は無音 (commit はそのまま通る)。別の repo・古い commit・commit でないコマンドで出る退行は雑音になる。どちらもテストでしか見えない。
# dupscan は偽物 (index の行を名前で返す)。本物と同じく、commit の後の索引は足した定義そのものも返す。返す定義は fixture の repo に実在するもの。
# -e を付けない: 検査を `if` の外で落とすと、✗ を出す前にスクリプトごと終わる (変異の検証で出力が空になる)
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/inject-dupscan-candidates-on-commit.sh"
EDIT_HOOK="$ROOT_DIR/_claude/hooks/inject-dupscan-candidates-on-new-func.sh"
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT" XDG_CACHE_HOME="$TMP_ROOT/cache"
# テストの repo を人の git の設定と、hook から継承した git の場所から切り離す
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMITTER_DATE GIT_AUTHOR_DATE

FAKE_BIN="$TMP_ROOT/bin"; mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/dupscan" <<'FAKE'
#!/bin/sh
# 本物と同じく、サブコマンド index と、最後の引数に実在する検索先を要る (無ければ何も返さない)
[ "$1" = index ] || exit 2
for last; do :; done
[ -d "$last" ] || exit 2
returns=""
while [ $# -gt 0 ]; do [ "$1" = "-name" ] && name="$2"; [ "$1" = "-returns" ] && returns="$2"; shift; done
echo "構文近似: 名前と引数ラベル/個数で照合。"
[ -n "$returns" ] && exit 0
case "$name" in
  # commit の後の索引は足した定義 (src/a/new.go) も返す。それが先頭に来ても、短い既存の名前 (collectWidgets) まで引けること
  collectWidgetsFast) echo "src/a/new.go:3-3 collectWidgetsFast() int (呼び出し ~0)" ;;
  collectWidgets) echo "src/a/new.go:3-3 collectWidgetsFast() int (呼び出し ~0)"; echo "src/a/c.go:3-3 collectWidgets(xs []int) []int (呼び出し ~3)" ;;
  loadSettings|loadSettingsFile) echo "src/b/s.go:3-3 loadSettingsFile(p string) error (呼び出し ~2)" ;;
  parseConfig|parseConfigFile) echo "src/b/s.go:4-4 parseConfigFile(p string) error (呼び出し ~2)" ;;
esac
FAKE
chmod +x "$FAKE_BIN/dupscan"
export PATH="$FAKE_BIN:$PATH"

new_repo() { # new_repo: 既存の関数を持つ repo を作り、そのパスを出す
  local r
  r=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$r" && git init -q . && mkdir -p src/a src/b &&
    printf 'package a\n\nfunc collectWidgets(xs []int) []int { return xs }\n' > src/a/c.go &&
    printf 'package b\n\nfunc loadSettingsFile(p string) error { return nil }\nfunc parseConfigFile(p string) error { return nil }\n' > src/b/s.go &&
    git add -A && git commit -q -m init ) >/dev/null
  printf '%s\n' "$r"
}
commit_file() { # commit_file <repo> <path> <内容>: ファイルを書いて、それだけを commit する
  ( cd "$1" && mkdir -p "$(dirname "$2")" && printf '%s' "$3" > "$2" && git add "$2" && git commit -q -m add -- "$2" ) >/dev/null
}

session=0
run_hook() { # run_hook <cwd> <command> [event] → $out (stdout)・$rc
  local cwd="$1" cmd="$2" event="${3:-PostToolUse}"
  session=$((session + 1))
  out=$(jq -n --arg c "$cmd" --arg cwd "$cwd" --arg s "s$session" --arg e "$event" \
      '{session_id:$s, cwd:$cwd, hook_event_name:$e, tool_name:"Bash", tool_input:{command:$c}}' |
    (cd "$cwd" && "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK"))
  rc=$?
}
same_session_hook() { session=$((session - 1)); run_hook "$@"; }

pass() { echo "✓ $1"; ok=$((ok + 1)); }
ng() { echo "✗ $1"; printf '  rc=%s out=%s\n' "${rc:-}" "$out"; fail=$((fail + 1)); }
context() { jq -r '.hookSpecificOutput.additionalContext // empty' <<< "$out" 2>/dev/null; }
expect_has() { # expect_has <説明> <含むべき文字列>
  if [ "${rc:-0}" -eq 0 ] && grep -qF -- "$2" <<< "$(context)"; then pass "$1"; else ng "$1"; fi
}
expect_lacks() {
  if [ "${rc:-0}" -eq 0 ] && ! grep -qF -- "$2" <<< "$out"; then pass "$1"; else ng "$1"; fi
}
expect_silent() {
  if [ "${rc:-0}" -eq 0 ] && [ -z "$out" ]; then pass "$1"; else ng "$1"; fi
}
expect_event() { # expect_event <説明> <event の名前>
  if [ "$(jq -r '.hookSpecificOutput.hookEventName' <<< "$out" 2>/dev/null)" = "$2" ]; then pass "$1"; else ng "$1"; fi
}

# 1. Bash で足した関数を commit すると、短い既存の名前の候補が出る (足した定義そのものは出さない)
R=$(new_repo)
commit_file "$R" src/a/new.go $'package a\n\nfunc collectWidgetsFast() int { return 0 }\n'
run_hook "$R" "git commit -m add -- src/a/new.go"
expect_has "足した関数に名前の近い既存の定義を出す" "src/a/c.go:3-3 collectWidgets"
expect_lacks "足した定義そのものは候補に出さない" "src/a/new.go"
expect_event "event の名前は入力のもの" PostToolUse

# 2. 同じ commit を同じ session で見直しても繰り返さない
same_session_hook "$R" "git commit -m add -- src/a/new.go"
expect_silent "同じ commit を同じ session で見直しても繰り返さない"

# 3. 既存のファイルに 1 つ足した commit は、親の木のファイルと比べる (既存の関数を足したものと読まない)
R=$(new_repo)
commit_file "$R" src/b/s.go $'package b\n\nfunc loadSettingsFile(p string) error { return nil }\nfunc parseConfigFile(p string) error { return nil }\nfunc loadSettings() {}\n'
run_hook "$R" "git commit -m add -- src/b/s.go"
expect_has "既存のファイルに足した関数に、同じファイルの既存の定義を出す" "loadSettingsFile"

# 4. 読むのは commit した中身で、作業ツリーではない (stage した後に作業ツリーを変えて、stage した方を commit する)
R=$(new_repo)
( cd "$R" && printf 'package b\n\nfunc parseConfig() error { return nil }\n' > src/b/p.go && git add src/b/p.go &&
  printf 'package b\n\nfunc parseConfigFast() error { return nil }\n' > src/b/p.go && git commit -q -m staged ) >/dev/null
run_hook "$R" "git commit -m staged"
expect_has "commit した中身 (stage した方) の関数を引く" "parseConfigFile"
expect_lacks "作業ツリーにだけある関数は引かない" "parseConfigFast"

# 5. 移動しただけのファイルは、移動元と比べる (移した関数を足したものと読まない)。日本語のパス (git が引用符で囲んで出す) でも同じ
R=$(new_repo)
( cd "$R" && mkdir -p src/c && git mv src/b/s.go src/c/s.go && git commit -q -m mv ) >/dev/null
run_hook "$R" "git commit -m mv"
expect_silent "移動しただけのファイルの関数は新しくない"
R=$(new_repo)
commit_file "$R" "src/b/設定.go" $'package b\n\nfunc loadSettings() {}\n'
# 移動先だけが ASCII だと、-z が無いときは移動元だけが引用符で囲まれて引けず、移した関数を新しいものと読む
( cd "$R" && mkdir -p src/c && git mv "src/b/設定.go" "src/c/settings.go" && git commit -q -m mv ) >/dev/null
run_hook "$R" "git commit -m mv"
expect_silent "日本語のパスから移動しただけのファイルの関数も新しくない"

# 6. git -C で別の dir から commit しても、その repo を見る。heredoc で message を渡す形でも動く
R2=$(new_repo)
commit_file "$R2" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
run_hook "$TMP_ROOT" "git -C $R2 commit -F - <<'M' -- src/b/p.go
p
M"
expect_has "git -C の行き先の repo の commit を見る" "parseConfigFile"

# 7. cwd の repo に新しい commit があっても、commit の行き先が別の repo なら cwd は見ない (B は古い commit だけを持つ)
A=$(new_repo); B=$(GIT_COMMITTER_DATE="2020-01-01T00:00:00" new_repo)
commit_file "$A" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
run_hook "$A" "git -C $B commit -m x"
expect_silent "cwd の repo の新しい commit を、別の repo への commit で出さない"

# 8. heredoc の本文に書いた commit が指す repo も、reflog が古ければ何もしない。本物の commit の行は、コメントに `<<EOF` があっても見る
R3=$(GIT_COMMITTER_DATE="2020-01-01T00:00:00" new_repo)
run_hook "$TMP_ROOT" "cat <<'M'
git -C $R3 commit -m p
M"
expect_silent "heredoc の本文の git commit が指す repo の古い commit は出さない"
R3b=$(new_repo)
commit_file "$R3b" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
run_hook "$R3b" "# message は <<EOF で渡す
git commit -F - <<EOF
p
EOF"
expect_has "コメントに <<EOF があっても、本物の commit の行を見る" "parseConfigFile"
# heredoc の本文に git commit の行を並べても、同じ行き先は 1 回だけ問い合わせ、時間の上限の内に本物の commit を見る
R3c=$(new_repo)
commit_file "$R3c" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
many=$(for _ in $(seq 1 1100); do echo "git commit -m x"; done)
run_hook "$R3c" ": <<'M'
$many
M
git commit -m real"
expect_has "heredoc の本文に git commit の行が並んでも、本物の commit を見る" "parseConfigFile"

# 9. reflog の先頭が古い commit は見ない (commit の時刻が新しくても、reflog の時刻で決める)
R4=$(new_repo)
( cd "$R4" && printf 'package b\n\nfunc parseConfig() error { return nil }\n' > src/b/p.go && git add src/b/p.go &&
  c=$(git commit-tree "$(git write-tree)" -p HEAD -m fresh) &&
  GIT_COMMITTER_DATE="2020-01-01T00:00:00" git update-ref -m "commit: fresh" HEAD "$c" ) >/dev/null
run_hook "$R4" "git commit -m fresh"
expect_silent "reflog の先頭が古い commit は、commit の時刻が新しくても見ない"

# 10. 未来の時刻の commit も今の commit と読まない
R5=$(new_repo)
( cd "$R5" && printf 'package b\n\nfunc parseConfig() error { return nil }\n' > src/b/p.go && git add src/b/p.go &&
  GIT_COMMITTER_DATE="2099-01-01T00:00:00" git commit -q -m future ) >/dev/null
run_hook "$R5" "git commit -m future"
expect_silent "未来の時刻の commit は見ない"

# 11. commit でないコマンドでは何もしない
R6=$(new_repo)
commit_file "$R6" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
run_hook "$R6" "git status"
expect_silent "git commit でないコマンドでは出さない"
run_hook "$R6" "echo 'git commit は後で'"
expect_silent "引用の中の git commit では出さない"

# 12. Write / Edit の時点で引いた名前は、commit で繰り返さない (印を共有する)
R7=$(new_repo)
session=$((session + 1))
edit_out=$(jq -n --arg s "s$session" --arg p "$R7/src/b/p.go" \
    '{session_id:$s, tool_name:"Write", tool_input:{file_path:$p, content:"package b\n\nfunc parseConfig() error { return nil }\n"}}' |
  (cd "$R7" && "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$EDIT_HOOK"))
if grep -qF parseConfigFile <<< "$edit_out"; then pass "Write の時点で候補を出す (前提)"; else ng "Write の時点で候補を出す (前提)"; fi
commit_file "$R7" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
same_session_hook "$R7" "git commit -m p"
expect_silent "Write の時点で引いた名前は commit で繰り返さない"

# 13. 関数を足していない commit では出さない
R8=$(new_repo)
commit_file "$R8" README.md $'x\n'
run_hook "$R8" "git commit -m readme"
expect_silent "関数を足していない commit では出さない"

# 14. PostToolUseFailure (commit の後のコマンドが落ちた) でも出す
R9=$(new_repo)
commit_file "$R9" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
run_hook "$R9" "git commit -m p && false" PostToolUseFailure
expect_has "PostToolUseFailure でも出す" "parseConfigFile"
expect_event "PostToolUseFailure の event の名前を返す" PostToolUseFailure

# 15. 計測のログに入口 commit の行を書く
if grep -q $'\tparseConfig\t1\tname\tcommit$' "$XDG_CACHE_HOME/claude-dupscan-inject.log"; then pass "計測のログに入口 commit の行を書く"; else ng "計測のログに入口 commit の行を書く"; fi

# 16. fail-open: lib が壊れていても、HOME が無くても rc=0
BROKEN="$TMP_ROOT/broken"; mkdir -p "$BROKEN/lib"
cp "$HOOK" "$BROKEN/"; cp "$ROOT_DIR"/_claude/hooks/lib/*.sh "$BROKEN/lib/"
printf 'this is not python(\n' > "$BROKEN/lib/dupscan_inject.py"
R10=$(new_repo)
commit_file "$R10" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
out=$(jq -n --arg cwd "$R10" '{session_id:"broken", cwd:$cwd, hook_event_name:"PostToolUse", tool_input:{command:"git commit -m p"}}' |
  (cd "$R10" && "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$BROKEN/inject-dupscan-candidates-on-commit.sh" 2>/dev/null)); rc=$?
expect_silent "lib が壊れていても rc=0 で何も出さない"
out=$(jq -n --arg cwd "$R10" '{session_id:"nohome", cwd:$cwd, hook_event_name:"PostToolUse", tool_input:{command:"git -C ~/x commit -m p"}}' |
  (cd "$R10" && env -u HOME "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null)); rc=$?
if [ "$rc" -eq 0 ]; then pass "HOME が無くても rc=0"; else ng "HOME が無くても rc=0"; fi

# 17. hook が __pycache__ を作らない (本体の checkout に untracked を残さない)
if [ ! -d "$ROOT_DIR/_claude/hooks/lib/__pycache__" ]; then pass "lib に __pycache__ を作らない"; else ng "lib に __pycache__ を作らない"; fi

# 18. settings.json の PostToolUse と PostToolUseFailure の Bash に配線してある (配線が無いと、テストが緑のまま何も動かない)
for ev in PostToolUse PostToolUseFailure; do
  n=$(jq --arg ev "$ev" '[.hooks[$ev][] | select(.matcher == "Bash") | .hooks[] | select(.command == "~/dotfiles/_claude/hooks/inject-dupscan-candidates-on-commit.sh")] | length' "$ROOT_DIR/_claude/settings.json")
  if [ "$n" = 1 ]; then pass "settings.json の $ev (Bash) に配線してある"; else rc=0; out="n=$n"; ng "settings.json の $ev (Bash) に配線してある"; fi
done

# 19. 長さはバイトで数える: 字数は上限 (20,000) の内でもバイトで超えるコマンドは、検出器の走査に入らずに抜ける (入ると 10 秒の timeout を越える)
R11=$(new_repo)
commit_file "$R11" src/b/p.go $'package b\n\nfunc parseConfig() error { return nil }\n'
long="git commit -m \"$(python3 -c "print(chr(0x1F60A) * 6000, end='')")\""
run_hook "$R11" "$long"
expect_silent "字数は上限の内でもバイトで超えるコマンドは見ない"

echo "---"
echo "ok=$ok fail=$fail"
[ "$fail" -eq 0 ]
