#!/usr/bin/env bash
# _claude/hooks/suggest-dupscan-for-symbol-grep.sh (PreToolUse(Bash / Grep): dupscan を組み込んだ repo で Swift のシンボルを grep したら
# dupscan を案内する) の unit テスト。
#
# なぜ: 案内が出なくなる退行は無音 (grep はそのまま走る)。逆に出すぎる退行は毎回の Bash に 1 行ずつ文脈を足す。どちらもテストでしか見えない。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/suggest-dupscan-for-symbol-grep.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 案内が消える)
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "✗ python3 が無い。hook は python3 前提なので検査できない" >&2; exit 1; }

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
export TMPDIR="$TMP_ROOT"   # 出しすぎ防止の状態ファイルをテストの中に閉じる
# Pair the hook with its own no-build companion, independent of the user's installed binary.
mkdir -p "$TMP_ROOT/tool/_claude/hooks/lib" "$TMP_ROOT/tool/bin"
cp "$HOOK" "$TMP_ROOT/tool/_claude/hooks/"
cp "$ROOT_DIR/_claude/hooks/lib/hook_io.sh" "$TMP_ROOT/tool/_claude/hooks/lib/"
HOOK="$TMP_ROOT/tool/_claude/hooks/suggest-dupscan-for-symbol-grep.sh"
export CAPABILITY_LOG="$TMP_ROOT/capability.log"
cat > "$TMP_ROOT/tool/bin/dupscan" <<'CLI'
#!/bin/sh
[ "$#" -eq 4 ] && [ "$1" = -no-build ] && [ "$2" = capabilities ] && [ "$3" = -format ] && [ "$4" = json ] || exit 99
printf '%s\n' "$*" >> "$CAPABILITY_LOG"
[ "${CAPABILITY_UNREADY:-0}" != 1 ] || exit 2
case "${CAPABILITY_MODE:-ready}" in
 malformed) printf '{invalid'; exit 0 ;;
 unsupported) printf '%s\n' '{"tool":"dupscan","schema_version":1,"commands":[]}'; exit 0 ;;
esac
printf '%s\n' '{"tool":"dupscan","schema_version":1,"commands":[{"name":"index","languages":["go","swift"]},{"name":"callers","languages":["go","swift"]}]}'
CLI
chmod +x "$TMP_ROOT/tool/bin/dupscan"


new_repo() { # $1=1 なら .swift を 1 つ追跡する → $REPO
  REPO=$(mktemp -d "$TMP_ROOT/repo.XXXXXX")
  ( cd "$REPO" && git init -q . && printf 'x\n' > README.md &&
    if [ "$1" = 1 ]; then printf 'struct A {}\n' > A.swift; fi && git add -A ) >/dev/null 2>&1
  return 0
}
session=0
next_session() { session=$((session + 1)); }

run_bash() { # $1=command → stdout
  jq -n --arg c "$1" --arg d "$REPO" --arg s "s$session" '{tool_name: "Bash", session_id: $s, cwd: $d, tool_input: {command: $c}}' |
    "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}
run_grep() { # $1=pattern $2=glob → stdout
  jq -n --arg p "$1" --arg g "$2" --arg d "$REPO" --arg s "s$session" '{tool_name: "Grep", session_id: $s, cwd: $d, tool_input: {pattern: $p, glob: $g}}' |
    "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true
}

expect_nudge() { # $1=説明 $2=stdout $3=案内に入るべき名前
  if ! printf '%s' "$2" | jq -e --arg n "dupscan def $3" '.hookSpecificOutput.additionalContext | contains($n)' >/dev/null 2>&1; then
    echo "✗ 案内が出ない / 名前が違う / JSON が壊れている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  # 🚨 止めない: permissionDecision を返したら grep が止まる
  if printf '%s' "$2" | jq -e '.hookSpecificOutput.permissionDecision' >/dev/null 2>&1; then
    echo "✗ permissionDecision を返している (案内だけのはず): $1"; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=stdout
  if [ -n "$2" ]; then echo "✗ 案内が出るべきでない: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return; fi
  echo "✓ $1"; ok=$((ok+1))
}

new_repo 1
# 名指しで探す対象のファイルと dir (Swift 以外のファイルか、拡張子つきの dir かは在るもので判定する)
printf 'x\n' > "$REPO/build.log"; printf 'x\n' > "$REPO/.gitignore"; mkdir -p "$REPO/MyPkg.swiftpm" "$REPO/Sources"; printf 'x\n' > "$REPO/Sources/A.swift"
next_session; expect_nudge "宣言の語つきの grep" "$(run_bash 'grep -rn "func executeMutation" shared/Sources')" executeMutation
next_session; expect_nudge "PascalCase の rg" "$(run_bash "rg -n 'StorageOperationLockRegistry' shared")" StorageOperationLockRegistry
next_session; expect_nudge "呼び出しの形 (末尾の括弧) で Swift を対象" "$(run_bash 'grep -rn "retrying(" --include=*.swift .')" retrying
next_session; expect_nudge "\\b つきの camelCase" "$(run_bash 'grep -rnw "\bdiagnosticCode\b" .')" diagnosticCode
next_session; expect_nudge "git grep の Type.member" "$(run_bash 'git grep -n "DropboxClient.upload"')" DropboxClient.upload
next_session; expect_nudge "cd の後ろの segment (後ろにパイプが続く)" "$(run_bash 'cd shared && grep -rn "struct HTTPRequestHead" Sources | head -5')" HTTPRequestHead
next_session; expect_nudge "-e で渡したパターン" "$(run_bash 'grep -rn -e "makeUploadSessionBodyRequest" Sources')" makeUploadSessionBodyRequest
next_session; expect_nudge "Grep ツールの camelCase" "$(run_grep 'func statsEvent' '')" statsEvent
next_session; expect_nudge "Grep ツールの小文字の語でも glob が swift" "$(run_grep 'retrying' '*.swift')" retrying

next_session; expect_silent "小文字の語で Swift を対象にしていない" "$(run_bash 'grep -n refresh config.yml')"
next_session; expect_silent "全部が大文字の語" "$(run_bash 'grep -rn "TODO" Sources')"
next_session; expect_silent "正規表現" "$(run_bash 'grep -rn "foo.*Bar" Sources')"
next_session; expect_silent "grep を含まないコマンド" "$(run_bash 'swift build')"
next_session; expect_silent "grep が語の一部 (grepper)" "$(run_bash 'grepper StorageOperationLockRegistry')"
next_session; expect_silent "heredoc の本文の中の grep" "$(run_bash "$(printf 'git commit -F - <<%sM%s\nfix: grep -rn StorageOperationLockRegistry を直す\nM\n' "'" "'")")"
next_session; expect_silent "Grep ツールの記号だけのパターン" "$(run_grep 'error:' '')"
next_session; expect_silent "パイプの後ろで stdin を絞る grep" "$(run_bash 'make test 2>&1 | grep FooBarFailure')"
next_session; expect_silent "alternation のパターン" "$(run_bash 'grep -rn "FooBar|BazQux" Sources')"
next_session; expect_nudge "値を取るオプションの値をパターンと読まない (--exclude-dir)" "$(run_bash 'grep -rn --exclude-dir build StorageFoo --include=*.swift .')" StorageFoo
next_session; expect_nudge "値を取るオプションの値をパターンと読まない (rg --sort)" "$(run_bash "rg -n --sort path fooBarQux -g '*.swift'")" fooBarQux
next_session; expect_nudge "grep の -r は値を取らない" "$(run_bash 'grep -r StorageOperationLockRegistry .')" StorageOperationLockRegistry
next_session; expect_silent ".swiftlint.yml は Swift の対象ではない" "$(run_bash 'grep -n disabled_rules .swiftlint.yml')"
next_session; expect_silent "対象が Swift 以外のファイルだけ (ログ)" "$(run_bash 'grep -c Error build.log')"
next_session; expect_silent "対象が Swift 以外のファイルだけ (dot file)" "$(run_bash 'grep -n "Package.resolved" .gitignore')"
next_session; expect_nudge "Swift のファイルを名指し" "$(run_bash 'grep -n "fooBarQux" Sources/A.swift')" fooBarQux
next_session; expect_silent "大文字小文字を無視する検索 (-rni)" "$(run_bash 'grep -rni "StorageLock" --include=*.swift .')"
next_session; expect_nudge "値をくっつけた -tswift を -i と読まない" "$(run_bash 'rg -n FooBar -tswift')" FooBar
next_session; expect_nudge "--type=swift を Swift の対象と読む" "$(run_bash 'rg -n refresh --type=swift')" refresh
next_session; expect_nudge "親の dir (..) をファイルと読まない" "$(run_bash 'grep -rn FooBar ..')" FooBar
next_session; expect_nudge "拡張子つきの dir をファイルと読まない" "$(run_bash 'grep -rn FooBar MyPkg.swiftpm')" FooBar
next_session; expect_nudge "束ねた -nA の値をパターンと読まない" "$(run_bash 'grep -rnA 3 FooBar Sources')" FooBar
next_session; expect_silent "Grep ツールの -i" "$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name: "Grep", session_id: $s, cwd: $d, tool_input: {pattern: "StorageLocation", "-i": true}}' | "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)"
next_session; expect_silent "Grep ツールの Swift でない glob" "$(run_grep 'StorageLocation' '*.md')"
next_session; expect_silent "Grep ツールの Swift でないファイルの path" "$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name: "Grep", session_id: $s, cwd: $d, tool_input: {pattern: "StorageLocation", path: "build.log"}}' | "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>/dev/null || true)"

# 出しすぎない: 同じ session の 2 回目は出さず、別の session なら出す
next_session
expect_nudge "1 回目は出る" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
expect_silent "同じ session の 2 回目は出ない" "$(run_bash 'grep -rn "func fooBarBaz" .')"
next_session; expect_nudge "別の session なら出る" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
# 状態ファイルが壊れていても (数字でない・桁が多すぎる) 出す側に倒れ、エラーで無出力にならない
next_session; printf 'abc\n' > "$TMP_ROOT/claude-dupscan-nudge-s$session"
expect_nudge "状態ファイルが数字でない" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz
next_session; printf '12345678901234567890\n' > "$TMP_ROOT/claude-dupscan-nudge-s$session"
expect_nudge "状態ファイルの桁が多すぎる" "$(run_bash 'grep -rn "func fooBarBaz" .')" fooBarBaz

# 書けない TMPDIR: 状態を残せなくても案内は出し、stderr に漏らさない
next_session
err=$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name: "Bash", session_id: $s, cwd: $d, tool_input: {command: "grep -rn \"func fooBarBaz\" ."}}' |
  TMPDIR=/nonexistent/dir "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" 2>&1 >/dev/null || true)
if [ -z "$err" ]; then echo "✓ 書けない TMPDIR でも stderr に出さない"; ok=$((ok+1)); else echo "✗ 書けない TMPDIR で stderr に出た: $err"; fail=$((fail+1)); fi

# .swift が多い repo (ls-files の出力がパイプの buffer を超える) でも出す。head の後に git が SIGPIPE で落ちても黙らない
REPO=$(mktemp -d "$TMP_ROOT/big.XXXXXX")
( cd "$REPO" && git init -q . && mkdir -p Sources/VeryLongDirectoryNameToGrowTheListing &&
  for n in $(seq 1 2500); do : > "Sources/VeryLongDirectoryNameToGrowTheListing/GeneratedSourceFile$n.swift"; done && git add -A ) >/dev/null 2>&1
next_session; expect_nudge ".swift が多い repo" "$(run_bash 'grep -rn "func executeMutation" Sources')" executeMutation

# .swift を追跡していない repo・git の外では出さない
new_repo 0
next_session; expect_silent ".swift を追跡していない repo" "$(run_bash 'grep -rn "func executeMutation" Sources')"
REPO="$TMP_ROOT"
next_session; expect_silent "git の外" "$(run_bash 'grep -rn "func executeMutation" Sources')"

# Goの明示指定は、Goで対応する案内だけを出す。
expect_go_nudge() {
  if ! printf '%s' "$2" | jq -e --arg n "dupscan index -name $3" '
    .hookSpecificOutput as $h | ($h.additionalContext | contains($n)) and
    ($h.additionalContext | contains("dupscan callers")) and
    ($h.additionalContext | contains("dupscan def") or contains("dupscan refs") or contains("dupscan context") | not) and
    ($h.permissionDecision == null)' >/dev/null 2>&1; then
    echo "✗ Goの案内が無い / 非対応コマンドがある / 拒否している: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
new_repo 0
printf 'package sample\n' > "$REPO/A.go"
git -C "$REPO" add -- A.go
printf 'x\n' > "$REPO/build.log"
for command in \
  'rg -n BuildDefinitions -t go .' \
  'rg -n BuildDefinitions -tgo .' \
  'rg --type=go -n BuildDefinitions .' \
  'rg --type go -n BuildDefinitions .' \
  'grep -rn BuildDefinitions --include=*.go .' \
  'grep -rn --include *.go BuildDefinitions .' \
  'rg -n BuildDefinitions -g "*.go" .' \
  'rg -n -g*.go BuildDefinitions .' \
  'git grep -n BuildDefinitions -- "*.go"' \
  'grep -n BuildDefinitions A.go'; do
  next_session; expect_go_nudge "Go指定: $command" "$(run_bash "$command")" BuildDefinitions
done
next_session; expect_go_nudge "Goの小文字名" "$(run_bash 'rg -t go refresh .')" refresh
next_session; expect_go_nudge "Goのtype宣言" "$(run_bash 'rg -t go "type Worker" .')" Worker
next_session; expect_go_nudge "GoのType.memberは末尾名を使う" "$(run_bash 'rg -t go Worker.refresh .')" refresh
next_session; expect_go_nudge "GrepのGo glob" "$(run_grep 'BuildDefinitions' '*.go')" BuildDefinitions
next_session; expect_go_nudge "GrepのGo type" "$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name:"Grep",session_id:$s,cwd:$d,tool_input:{pattern:"BuildDefinitions",type:"go"}}' | "$HOOK")" BuildDefinitions
next_session; expect_go_nudge "GrepのGo path" "$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name:"Grep",session_id:$s,cwd:$d,tool_input:{pattern:"BuildDefinitions",path:"A.go"}}' | "$HOOK")" BuildDefinitions
for command in \
  'rg -i -t go BuildDefinitions .' \
  'rg -t go "foo.*Bar" .' \
  'rg -t go TODO .' \
  'rg -t python BuildDefinitions .' \
  'rg --type-not go BuildDefinitions .' \
  'rg --exclude=*.go BuildDefinitions .' \
  'rg BuildDefinitions .' \
  'rg -t go BuildDefinitions build.log'; do
  next_session; expect_silent "Goの対象外: $command" "$(run_bash "$command")"
done
# 混在repoでもGoを誤ってSwift向けに案内せず、Go通知がSwift通知の間隔を消費しない。
printf 'struct A {}\n' > "$REPO/A.swift"
git -C "$REPO" add -- A.swift
next_session; expect_go_nudge "混在repoの -t=go" "$(run_bash 'rg -t=go FooBar .')" FooBar
next_session; expect_go_nudge "混在repoの -nt=go" "$(run_bash 'rg -nt=go FooBar .')" FooBar
next_session; expect_silent "混在repoの -nit=go はcase-insensitive" "$(run_bash 'rg -nit=go FooBar .')"
next_session; expect_go_nudge "混在repoで束ねた -nt go" "$(run_bash 'rg -nt go FooBar .')" FooBar
next_session; expect_go_nudge "混在repoで束ねた -ntgo" "$(run_bash 'rg -ntgo FooBar .')" FooBar
next_session; expect_silent "混在repoの -nit go はcase-insensitive" "$(run_bash 'rg -nit go FooBar .')"
next_session; expect_silent "混在repoの -nitgo はcase-insensitive" "$(run_bash 'rg -nitgo FooBar .')"
next_session; expect_go_nudge "混在repoでGo指定" "$(run_bash 'rg -t go BuildDefinitions .')" BuildDefinitions
expect_silent "Go通知の2回目は抑制" "$(run_bash 'rg -t go BuildDefinitions .')"
expect_nudge "同sessionでもSwift通知は独立" "$(run_bash 'rg -t swift FooBar .')" FooBar
next_session; expect_nudge "-g=!*.go はGoを除外したSwift検索" "$(run_bash 'rg -g=!*.go FooBar .')" FooBar
next_session; expect_silent "GoとSwiftの矛盾したselector" "$(run_bash 'rg -t go -t swift FooBar .')"
next_session; expect_go_nudge "負のSwift globはGo指定を覆さない" "$(run_bash 'rg -t go -g "!*.swift" FooBar .')" FooBar
# 言語のstamp namespaceをsession IDの末尾で表現しない。
expect_go_nudge "Go stampの衝突確認用" "$(jq -n --arg d "$REPO" '{tool_name:"Bash",session_id:"collision",cwd:$d,tool_input:{command:"rg -t go FooBar ."}}' | "$HOOK")" FooBar
expect_nudge "末尾-goの別sessionのSwiftを抑制しない" "$(jq -n --arg d "$REPO" '{tool_name:"Bash",session_id:"collision-go",cwd:$d,tool_input:{command:"rg -t swift FooBar ."}}' | "$HOOK")" FooBar

# 941: 言語の手掛かりが無い検索は、Swift と Go を両方追跡する repo では Swift 専用の def / refs を勧めず、両方で使える index / callers を案内する。
expect_neutral_nudge() { # $1=説明 $2=stdout $3=名前
  if ! printf '%s' "$2" | jq -e --arg n "dupscan index -name $3" '
    .hookSpecificOutput as $h | ($h.additionalContext | contains($n)) and
    ($h.additionalContext | contains("dupscan callers")) and ($h.additionalContext | contains("Swift のファイルだけ")) and
    ($h.additionalContext | contains("dupscan def") or contains("dupscan refs") | not) and ($h.permissionDecision == null)' >/dev/null 2>&1; then
    echo "✗ 中立の案内が出ない / Swift 専用のコマンドを勧めている: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); return
  fi
  echo "✓ $1"; ok=$((ok+1))
}
expect_extra() { # $1=説明 $2=stdout $3=案内に入るべき追加の名前
  if printf '%s' "$2" | jq -e --arg n "他の名前も同様に引ける: $3" '.hookSpecificOutput.additionalContext | contains($n)' >/dev/null 2>&1; then
    echo "✓ $1"; ok=$((ok+1)); else echo "✗ 追加の名前が案内に無い: $1"; printf '%s\n' "$2" | head -3; fail=$((fail+1)); fi
}
MIXED_REPO="$REPO"
next_session; expect_neutral_nudge "混在 repo の言語不明 (PascalCase)" "$(run_bash 'rg -n FooBar .')" FooBar
next_session; expect_neutral_nudge "混在 repo の func.*Name" "$(run_bash 'rg "func.*FooBar" .')" FooBar
next_session; expect_neutral_nudge "混在 repo の Grep ツール (言語の手掛かりなし)" "$(run_grep 'FooBar' '')" FooBar
next_session; out=$(CAPABILITY_MODE=unsupported run_bash 'rg -n FooBar .')
if printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | contains("dupscan def FooBar")' >/dev/null 2>&1; then
  echo '✓ 混在 repo で Go 対応を確かめられないときは従来どおり Swift 向け (確認の文だけに落として情報を減らさない)'; ok=$((ok+1)); else echo '✗ 混在 repo で Go 対応が未確認のとき従来の Swift 向けの案内でない'; printf '%s\n' "$out" | head -3; fail=$((fail+1)); fi
# 引用符を取り違える入力 (コメントの ' や $'..') でも、後ろの grep を落とさない (旧の分割に倒す)
next_session; expect_neutral_nudge "コメントの ' で引用が閉じなくても後ろの grep を見る" "$(run_bash "$(printf 'echo "a" # it'"'"'s\ngrep -rn FooBar .')")" FooBar
next_session; expect_neutral_nudge "\$'..' の後ろの grep を見る" "$(run_bash "grep \$'it\\'s' . ; grep -rn FooBar .")" FooBar
# bash -c の中の grep (引用符を読む分割でも見失わない)
next_session; expect_neutral_nudge "bash -c '…; grep …' の中身" "$(run_bash "bash -c 'cd . ; grep -rn FooBar . | head -5; ls'")" FooBar
next_session; expect_silent "bash -c の中のパイプの後ろの grep は見ない" "$(run_bash "bash -c 'make test | grep FooBar'")"
# 値のくっついた短いオプション (-tswift) をパターンの前に置いても、次の語をパターンとして読む
next_session; expect_nudge "rg -tswift をパターンの前に置く" "$(run_bash 'rg -tswift FooBar .')" FooBar
# Go のメソッド宣言 (receiver 付き) は言語の手掛かりが無くても Go と読む。Swift に無い形
next_session; expect_go_nudge "receiver 付きの宣言 (エスケープあり)" "$(run_bash 'rg -n "func \(w \*Workspace\) ReadCookies" .')" ReadCookies
next_session; expect_go_nudge "receiver 付きの宣言 (grep のエスケープなし)" "$(run_bash 'grep -rn "func (w \*Workspace) ReadCookies" .')" ReadCookies
next_session; expect_go_nudge "receiver 付きの宣言 (Grep ツール)" "$(run_grep 'func \(w \*Workspace\) ReadCookies' '')" ReadCookies
next_session; expect_silent "receiver 付きでも Swift を明示したら Go と読まない" "$(run_bash 'rg -t swift "func \(w \*Workspace\) ReadCookies" .')"
next_session; expect_silent "receiver 付きでも -i なら案内しない" "$(run_bash 'grep -rni "func (w \*Workspace) ReadCookies" .')"
# 識別子だけの alternation (3 つまで)
next_session; out=$(run_bash 'rg -t go "ListProfiles|fallbackProfiles" .')
expect_go_nudge "rg の alternation (Go 指定)" "$out" ListProfiles
expect_extra "alternation の残りの名前を案内に添える" "$out" fallbackProfiles
next_session; expect_go_nudge "grep の BRE の \\| (Go 指定)" "$(run_bash 'grep -rn "ListProfiles\|fallbackProfiles" --include=*.go .')" ListProfiles
next_session; expect_go_nudge "grep -E の |" "$(run_bash 'grep -rnE "ListProfiles|fallbackProfiles" --include=*.go .')" ListProfiles
next_session; expect_go_nudge "alternation の後ろにパイプが続く" "$(run_bash 'rg -t go "ListProfiles|fallbackProfiles" . | head -5')" ListProfiles
next_session; expect_go_nudge "Grep ツールの alternation (type=go)" "$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name:"Grep",session_id:$s,cwd:$d,tool_input:{pattern:"ListProfiles|fallbackProfiles",type:"go"}}' | "$HOOK")" ListProfiles
next_session; expect_silent "rg の \\| は文字の | なので alternation と読まない" "$(run_bash 'rg -t go "ListProfiles\|fallbackProfiles" .')"
next_session; expect_silent "grep の BRE の | は文字の |" "$(run_bash 'grep -rn "ListProfiles|fallbackProfiles" --include=*.go .')"
next_session; expect_silent "alternation が 4 つ" "$(run_bash 'rg -t go "Aaa1|Bbb2|Ccc3|Ddd4" .')"
next_session; expect_silent "alternation に識別子でない部分" "$(run_bash 'rg -t go "ListProfiles|foo.*bar" .')"
# 引用符の中の | でコマンドを割らない (割ると alternation が壊れ、パイプの後ろ扱いにもなる)
next_session; expect_silent "パイプの後ろの grep は alternation でも見ない" "$(run_bash 'make test 2>&1 | rg -t go "ListProfiles|fallbackProfiles"')"
# Go だけの repo: 言語の手掛かりが無い検索は従来どおり黙る (939)。receiver 付きだけは Go と読める
new_repo 0; printf 'package sample\n' > "$REPO/A.go"; git -C "$REPO" add -- A.go
next_session; expect_silent "Go だけの repo の言語不明は黙る (939 の決定)" "$(run_bash 'rg -n FooBar .')"
next_session; expect_go_nudge "Go だけの repo でも receiver 付きは案内する" "$(run_bash 'rg -n "func \(w \*Workspace\) ReadCookies" .')" ReadCookies
# 追跡ファイルの優位 (10 倍以上) で言語不明の検索の扱いを決める: 1 つでも追跡していれば混在、とはしない (obaket は Swift 1670 本・Go 1 本)
new_repo 0; for n in $(seq 1 20); do printf 'struct S%d {}\n' "$n" > "$REPO/S$n.swift"; done; printf 'package a\n' > "$REPO/A.go"; git -C "$REPO" add -A >/dev/null 2>&1
next_session; expect_nudge "Swift 優位 (20 対 1) の言語不明は従来どおり Swift 向け" "$(run_bash 'rg -n FooBar .')" FooBar
new_repo 0; for n in $(seq 1 20); do printf 'package a\n' > "$REPO/G$n.go"; done; printf 'struct A {}\n' > "$REPO/A.swift"; git -C "$REPO" add -A >/dev/null 2>&1
next_session; expect_silent "Go 優位 (1 対 20) の言語不明は黙る (Go だけの repo と同じ)" "$(run_bash 'rg -n FooBar .')"
next_session; expect_go_nudge "Go 優位でも Go を指定した検索は案内する" "$(run_bash 'rg -t go FooBar .')" FooBar
next_session; expect_nudge "Go 優位でも Swift を指定した検索は案内する" "$(run_bash 'rg -t swift FooBar .')" FooBar
# Swift だけの repo は今までどおり Swift 向け (alternation も)
new_repo 1
next_session; out=$(run_bash 'grep -rn "FooBar\|BazQux" --include=*.swift Sources')
expect_nudge "Swift だけの repo の alternation" "$out" FooBar
expect_extra "Swift の alternation の残りの名前" "$out" BazQux
next_session; expect_nudge "Swift だけの repo の言語不明は従来どおり Swift 向け" "$(run_bash 'rg -n FooBar .')" FooBar
REPO="$MIXED_REPO"

# hook自身はdupscan/goを起動しない。案内文字列を出すだけ。
SPY_BIN="$TMP_ROOT/spy-bin"; mkdir "$SPY_BIN"
cat > "$SPY_BIN/dupscan" <<'SPY'
#!/bin/sh
: > "$DUPSCAN_NUDGE_SPY"
exit 99
SPY
cp "$SPY_BIN/dupscan" "$SPY_BIN/go"; chmod +x "$SPY_BIN/dupscan" "$SPY_BIN/go"
next_session
out=$(jq -n --arg d "$REPO" --arg s "s$session" '{tool_name:"Bash",session_id:$s,cwd:$d,tool_input:{command:"rg -t go FooBar ."}}' |
  PATH="$SPY_BIN:$PATH" DUPSCAN_NUDGE_SPY="$TMP_ROOT/executed" "$HOOK")
expect_go_nudge "PATH上の旧dupscanやgoを起動せず案内する" "$out" FooBar
if [ -e "$TMP_ROOT/executed" ]; then echo "✗ hookがPATH上のdupscan/goを起動した"; fail=$((fail+1)); else echo "✓ hookでPATH上のdupscan/go起動なし"; ok=$((ok+1)); fi

next_session
out=$(CAPABILITY_UNREADY=1 run_bash 'rg -t go FooBar .')
if printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | contains("dupscan capabilities") and (contains("dupscan index -name") | not)' >/dev/null; then
  echo '✓ no-build未準備では具体的な未検証commandを案内しない'; ok=$((ok+1))
else echo '✗ 未準備のcapabilityを誤案内'; fail=$((fail+1)); fi
if [ -s "$CAPABILITY_LOG" ] && [ -z "$(grep -vFx -- '-no-build capabilities -format json' "$CAPABILITY_LOG" || true)" ]; then
  echo '✓ capabilityはno-buildの固定argvだけで取得する'; ok=$((ok+1))
else echo '✗ capabilityが危険なargvで呼ばれた'; fail=$((fail+1)); fi

for mode in malformed unsupported; do
  next_session
  out=$(CAPABILITY_MODE="$mode" run_bash 'rg -t go FooBar .')
  if printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | contains("dupscan capabilities") and (contains("dupscan index -name") | not)' >/dev/null; then
    echo "✓ $mode capabilityは具体的なcommandへ誘導しない"; ok=$((ok+1))
  else echo "✗ $mode capabilityを誤案内"; fail=$((fail+1)); fi
done
printf -v long_symbol '%*s' 1400 ''
long_symbol="Foo${long_symbol// /a}"
next_session
out=$(run_bash "rg -t go $long_symbol .")
context_bytes=$(printf '%s' "$out" | jq -j '.hookSpecificOutput.additionalContext' | wc -c)
if [ "$context_bytes" -le 1024 ] && printf '%s' "$out" | jq -e '.hookSpecificOutput.additionalContext | contains("長い名前は手動で渡す")' >/dev/null; then
  echo '✓ 長いsymbolでも通知は1024bytes以内'; ok=$((ok+1))
else echo '✗ 長いsymbolで通知予算を超えた'; fail=$((fail+1)); fi

echo "[suggest-dupscan-for-symbol-grep] $ok passed / $fail failed"
[ "$fail" -eq 0 ]
