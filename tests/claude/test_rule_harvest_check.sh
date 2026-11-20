#!/usr/bin/env bash
# _claude/hooks/rule-harvest-check.sh (Stop で、実装を変えた commit があれば skill rule-harvest を促す) の unit テスト。
#
# なぜ: この hook は止める条件を誤ると、黙る (rule に落とす機会が消える) か、rule を足す commit のたびに止め直す (ループ) かになる。
# 数える commit (実装を変えた) と数えない commit (md・issues・submodule の参照だけ・失敗した commit・commit でないコマンド)、
# 1 セッション 1 回の抑制、止めない条件、repo を特定できない sha の扱いを固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/rule-harvest-check.sh"
fails=0
if ! command -v python3 >/dev/null 2>&1; then echo "SKIP: python3 が無い環境"; exit 77; fi
if ! command -v jq >/dev/null 2>&1; then echo "SKIP: jq が無い環境"; exit 77; fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/rule-harvest.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
export XDG_CACHE_HOME="$WORK/cache"
unset CLAUDE_RULE_HARVEST
repo="$WORK/repo"; other="$WORK/other"
mkdir -p "$repo/src" "$repo/issues" "$repo/docs" "$other"
git -C "$repo" init -q . && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
echo base >"$repo/src/a.go"
git -C "$repo" add -A && git -C "$repo" commit -qm "init"

commit() { # $1=subject → 短い sha
  git -C "$repo" add -A && git -C "$repo" commit -qm "$1" && git -C "$repo" rev-parse --short=8 HEAD
}
echo impl >>"$repo/src/a.go"; impl=$(commit "feat: 実装を変える")
echo doc >"$repo/docs/x.md"; mdonly=$(commit "docs: md だけ")
echo i >"$repo/issues/101-feat-x.md"; issuesonly=$(commit "issue: issues だけ")
git -C "$repo" update-index --add --cacheinfo "160000,$(git -C "$repo" rev-parse HEAD),sub" && git -C "$repo" commit -qm "chore: submodule の参照だけ"
gitlink=$(git -C "$repo" rev-parse --short=8 HEAD)
git -C "$repo" update-index --force-remove sub && git -C "$repo" commit -qm "chore: 参照を外す"
echo x >"$repo/src/b.go"; ctl=$(commit "feat: x$(printf '\001')y の件名")
echo y >"$repo/docs/設計.md"; jadoc=$(commit "docs: 日本語名の md だけ")
mkdir -p "$repo/Sources/issues"; echo x >"$repo/Sources/issues/Foo.swift"; nested=$(commit "feat: issues という名前の dir の下のコード")

# transcript を作る。引数は「コマンド<TAB>結果<TAB>is_error」の行。cwd は $tcwd
tcwd="$repo"
transcript() { # $1=ファイル名 stdin=行
  local f="$WORK/$1.jsonl" i=0 cmd out err
  : >"$f"
  printf '{"type":"user","cwd":"%s","message":{"role":"user","content":"go"}}\n' "$tcwd" >>"$f"
  while IFS=$'\t' read -r cmd out err; do
    i=$((i+1))
    jq -cn --arg cwd "$tcwd" --arg cmd "$cmd" --arg id "t$i" \
      '{type:"assistant",cwd:$cwd,message:{role:"assistant",content:[{type:"tool_use",id:$id,name:"Bash",input:{command:$cmd}}]}}' >>"$f"
    jq -cn --arg cwd "$tcwd" --arg out "$out" --arg id "t$i" --argjson err "${err:-false}" \
      '{type:"user",cwd:$cwd,message:{role:"user",content:[{type:"tool_result",tool_use_id:$id,content:$out,is_error:$err}]}}' >>"$f"
  done
  printf '%s' "$f"
}
run() { # $1=session $2=transcript path $3=extra json
  printf '{"session_id":"%s","transcript_path":"%s","cwd":"%s"%s}' "$1" "$2" "$tcwd" "${3:-}" | "$HOOK" 2>/dev/null || echo "__ERROR__"
}
reason() { local out; out=$(run "$@"); [ -n "$out" ] || return 0; printf '%s' "$out" | jq -r 'if .decision=="block" then .reason else "__NOBLOCK__" end'; }
check() { local desc="$1" want="$2" got="$3"
  if [ -z "$want" ]; then [ -z "$got" ] && return 0; echo "NG: $desc — 無出力のはず:"; printf '%s\n' "$got"; fails=$((fails+1)); return; fi
  grep -Eq -- "$want" <<<"$got" && return 0
  echo "NG: $desc — /$want/ が無い:"; printf '%s\n' "${got:-(無出力)}"; fails=$((fails+1)); }
line() { printf '%s\t%s\t%s\n' "$1" "$2" "${3:-false}"; }

# 1. 実装を変えた commit → 止め、repo・sha・subject と skill を挙げる
t1=$(line "git commit -m x -- src/a.go" "[master $impl] feat: 実装を変える
 1 file changed" | transcript t1)
got=$(reason s1 "$t1")
check "実装を変えた commit で止める" "repo ${impl}[0-9a-f]* feat: 実装を変える" "$got"
check "skill rule-harvest へ案内する" "skill rule-harvest" "$got"
# 2. 同じ sha では 1 セッション 1 回 / 別のセッションでは止める
check "同じ sha は再び止めない" "" "$(reason s1 "$t1")"
marks_lines() { grep -c "^s1	" "$XDG_CACHE_HOME/claude-rule-harvest.marks" || true; }
before=$(marks_lines); reason s1 "$t1" >/dev/null
check "報告済みの sha の印を書き足さない" "^$before\$" "$(marks_lines)"
check "別のセッションでは止める (2 の対照)" "$impl" "$(reason s2 "$t1")"
# 3. stop_hook_active・CLAUDE_RULE_HARVEST=off では黙る
check "stop_hook_active で黙る" "" "$(reason s3 "$t1" ',"stop_hook_active":true')"
# 続きの turn では印だけを書く (この skill の続きで作った commit を、次の turn にもう一度報告しない)
check "stop_hook_active で印を書いた sha は、次の turn でも止めない" "" "$(reason s3 "$t1")"
check "CLAUDE_RULE_HARVEST=off で黙る" "" "$(CLAUDE_RULE_HARVEST=off reason s4 "$t1")"
check "3 の対照: 印の無いセッションは止める" "$impl" "$(reason s5 "$t1")"

# 4. md だけ・issues だけ・submodule の参照だけの commit では止めない
t4=$( { line "git commit -m d" "[master $mdonly] docs: md だけ"
        line "git commit -m i" "[master $issuesonly] issue: issues だけ"
        line "git commit -m s" "[master $gitlink] chore: submodule の参照だけ"; } | transcript t4)
check "md・issues・submodule の参照だけなら黙る" "" "$(reason s6 "$t4")"
# 4 の対照: 同じ transcript に実装の commit が 1 つ混ざれば、それだけを挙げる
t4b=$( { line "git commit -m d" "[master $mdonly] docs: md だけ"
         line "git commit -m x" "[master $impl] feat: 実装を変える"; } | transcript t4b)
got=$(reason s7 "$t4b")
check "混ざった実装の commit だけを挙げる" "$impl" "$got"
check "md だけの commit は挙げない" "" "$(grep -F "$mdonly" <<<"$got" || true)"

# 4c. issues という名前の dir の下でも、md でないコードは数える (外すのは md で、dir の名前では外さない)
t4c=$(line "git commit -m n" "[master $nested] feat: issues という名前の dir の下のコード" | transcript t4c)
check "Sources/issues/ の下のコードは数える" "$nested" "$(reason s13 "$t4c")"

# 4d. 件名に制御文字がある実装の commit も数える / 日本語名の md だけの commit は数えない (core.quotepath が true の git でも)
t4d=$(line "git commit -m c" "[master $ctl] feat: 制御文字" | transcript t4d)
check "件名に \\x01 がある実装の commit も数える" "$ctl" "$(reason s14 "$t4d")"
t4e=$(line "git commit -m j" "[master $jadoc] docs: 日本語名の md だけ" | transcript t4e)
check "日本語名の md だけの commit は数えない" "" "$(git -C "$repo" config core.quotepath true; reason s15 "$t4e")"

# 5. 失敗した commit・commit でないコマンドの出力は数えない
t5=$( { line "git commit -m x" "[master $impl] feat: 実装を変える" true
        line "git log -1 --format=x" "[master $impl] feat: 実装を変える"; } | transcript t5)
check "is_error の commit と commit でないコマンドは数えない" "" "$(reason s8 "$t5")"

# 6. cwd が別の dir でも、コマンドの git -C の先から repo を見つける
tcwd="$other"
t6=$(line "git -C $repo commit -m x" "[master $impl] feat: 実装を変える" | transcript t6)
check "git -C の先の repo で sha を引く" "repo $impl" "$(reason s9 "$t6")"
# 7. どの dir でも見つからない sha は、中身を見られないので止める (repo を特定できなかったと書く)
t7=$(line "git commit -m x" "[master deadbeef1] feat: どこかの repo" | transcript t7)
check "repo を特定できない sha でも止める" "deadbeef1 \(repo を特定できなかった" "$(reason s10 "$t7")"
tcwd="$repo"

# 8. transcript が無い → 黙る。assistant の行が無い transcript → 黙り、warn を残す
check "transcript が無ければ黙る" "" "$(reason s11 "$WORK/none.jsonl")"
printf '{"type":"user","message":{"role":"user","content":"x"}}\n' >"$WORK/empty.jsonl"
check "assistant の行が無ければ黙る" "" "$(reason s12 "$WORK/empty.jsonl")"
check "assistant の行が無いと warn を残す" "rule-harvest-check: transcript に assistant の行が無い" "$(cat "$XDG_CACHE_HOME/claude-hook-marks.log" 2>/dev/null || true)"

# 9. 入力が JSON でない → 黙る (Stop を止めない)
check "JSON でない入力で黙る" "" "$(printf 'not json' | "$HOOK" 2>/dev/null || echo __ERROR__)"

if [ "$fails" -gt 0 ]; then echo "FAIL: $fails 件"; exit 1; fi
echo "OK: rule-harvest-check"
