#!/usr/bin/env bash
# 番号が 1000 を超えた後の採番と重複の検査。
#
#   ① scripts/issue_next_number.sh は 999 と 1000 を数値で比べる (文字列の sort は 999 を 1000 より後に置く)。
#      作業ツリーに無く ref にだけある 4 桁の番号も数える。ゼロ埋めの 3 桁は 999 まで
#   ② tests/issues/test_issue_numbers_unique.sh は 4 桁の番号の重複を落とす (3 桁で切り出すと別の値になって見逃す)
set -euo pipefail
unset CDPATH
# 利用者の git の設定を読まない (`core.quotePath = false` を設定していると、ls-tree が非 ASCII の名前に引用符を付ける既定の形を再現できない)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NEXT="$ROOT_DIR/scripts/issue_next_number.sh"
UNIQUE="$ROOT_DIR/tests/issues/test_issue_numbers_unique.sh"
[ -x "$NEXT" ] || { printf '✗ scripts/issue_next_number.sh が無い / 実行できない\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() { printf '✗ %s\n' "$1" >&2; exit 1; }
g() { git -C "$1" -c user.email=t@t -c user.name=t "${@:2}"; }

# repo を作る。$2 以降の issue を作業ツリーに置いて commit し、origin/master をそこに向ける
new_repo() {
  local d="$1"; shift
  mkdir -p "$d/issues"
  for f in "$@"; do mkdir -p "$d/issues/$(dirname "$f")"; : > "$d/issues/$f"; done
  g "$d" init -q
  g "$d" add -A
  g "$d" commit -qm base
  g "$d" update-ref refs/remotes/origin/master HEAD
}

next_in() { (cd "$1" && "$NEXT" origin/master); }

# ① 3 桁だけ: ゼロ埋めを保つ
d="$work/three"; new_repo "$d" 007-a.md done/010-b.md
[ "$(next_in "$d")" = 011 ] || fail "3 桁だけのとき 011 にならない: $(next_in "$d")"

# ① 999 と 1000 の跨ぎ (文字列の sort だと 999 が最大に見える)
d="$work/cross"; new_repo "$d" done/998-a.md 999-b.md epic/900/next/1000-c.md
[ "$(next_in "$d")" = 1001 ] || fail "999 と 1000 があるとき 1001 にならない: $(next_in "$d")"

# ① ref にだけある 4 桁 (他のセッションが先に push した番号) を跨ぐ
d="$work/ref"; new_repo "$d" 999-b.md
: > "$d/issues/1003-x.md"; g "$d" add -A; g "$d" commit -qm other
g "$d" update-ref refs/remotes/origin/master HEAD; g "$d" reset -q --hard HEAD~1
[ ! -e "$d/issues/1003-x.md" ] || fail "前提: 作業ツリーに 1003 が残っている"
[ "$(next_in "$d")" = 1004 ] || fail "ref にだけある 1003 を跨がない: $(next_in "$d")"

# ① 番号でないファイル (new- の仮の名前・README・2 桁・日付で始まる記録) は数えない
d="$work/noise"; new_repo "$d" 120-a.md new-feat-x.md README.md 99-short.md logs/2026-02-20-150715-incident.md
[ "$(next_in "$d")" = 121 ] || fail "番号でないファイルを数えた: $(next_in "$d")"

# ① ref にだけある非 ASCII の名前 (git ls-tree が引用符を付ける) も数える
d="$work/quoted"; new_repo "$d" 130-a.md
: > "$d/issues/140-bug-日本語.md"; g "$d" add -A; g "$d" commit -qm other
g "$d" update-ref refs/remotes/origin/master HEAD; g "$d" reset -q --hard HEAD~1
[ "$(next_in "$d")" = 141 ] || fail "ref にだけある非 ASCII の名前 (140) を数えない: $(next_in "$d")"

# ① ref を省くと origin の既定のブランチ (origin/HEAD) を数える。サブディレクトリから相対パスで起動しても動く
d="$work/defref"; new_repo "$d" 150-a.md
g "$d" update-ref -d refs/remotes/origin/master
: > "$d/issues/160-x.md"; g "$d" add -A; g "$d" commit -qm other
g "$d" update-ref refs/remotes/origin/main HEAD; g "$d" reset -q --hard HEAD~1
g "$d" symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/main
mkdir -p "$d/sub/dir" "$d/bin"; cp -R "$ROOT_DIR/scripts" "$d/scripts"; cp -RL "$ROOT_DIR/bin/lib" "$d/bin/lib" # 採番の道具は bin/lib/git_location_env.sh (devtool/lib への symlink) も読む
got="$(cd "$d/sub/dir" && ../../scripts/issue_next_number.sh)" || fail "サブディレクトリから相対パスで起動して落ちた"
[ "$got" = 161 ] || fail "ref を省いたとき origin/HEAD (origin/main) の 160 を数えない: $got"

# ① repo が自分の採番の道具 (bin/next-issue-number) を持つなら、番号を出さずにそれを案内する (入れ子の issues/ と通しで採番する repo で
#    直下だけを数えると、入れ子の側の番号を取り直す)
d="$work/owntool"; new_repo "$d" 170-a.md
mkdir -p "$d/bin" && printf '#!/bin/sh\n' > "$d/bin/next-issue-number" && chmod +x "$d/bin/next-issue-number"
rc=0; next_in "$d" > "$work/owntool.out" 2> "$work/owntool.err" || rc=$?
[ "$rc" -eq 2 ] || fail "自分の採番の道具を持つ repo で rc=$rc (2 を期待)"
[ ! -s "$work/owntool.out" ] || fail "自分の採番の道具を持つ repo で番号を出した: $(cat "$work/owntool.out")"
grep -qF "/bin/next-issue-number を使う" "$work/owntool.err" || { cat "$work/owntool.err" >&2; fail "repo の採番の道具を案内していない"; }

# ① 継承した GIT_DIR / GIT_WORK_TREE は外し、cwd の repo の道具を案内する (別の repo の番号を出さない)
rc=0; (cd "$d" && GIT_DIR="$work/three/.git" GIT_WORK_TREE="$work/three" "$NEXT" origin/master) > "$work/gitdir.out" 2>&1 || rc=$?
[ "$rc" -eq 2 ] || { cat "$work/gitdir.out" >&2; fail "継承した GIT_DIR に従って別の repo を数えた (rc=$rc)"; }

# ② 4 桁の重複を落とす / 4 桁でも別の番号なら通る
d="$work/dup4"; mkdir -p "$d/issues/done"; : > "$d/issues/1000-a.md"; : > "$d/issues/done/1000-b.md"; : > "$d/issues/999-c.md"
if bash "$UNIQUE" "$d/issues" > "$work/dup4.log" 2>&1; then
  fail "1000-a と done/1000-b の重複を落とさない"
fi
grep -qF '番号 1000:' "$work/dup4.log" || { cat "$work/dup4.log" >&2; fail "重複の番号に 1000 を出していない"; }
d="$work/uniq4"; mkdir -p "$d/issues"; : > "$d/issues/1000-a.md"; : > "$d/issues/1001-b.md"; : > "$d/issues/100-c.md"
bash "$UNIQUE" "$d/issues" > "$work/uniq4.log" 2>&1 || { cat "$work/uniq4.log" >&2; fail "1000・1001・100 を重複と読んだ"; }

printf '✓ issue 番号の 4 桁: 採番 (3 桁のゼロ埋め・999 と 1000 の跨ぎ・ref にだけある番号・番号でないファイルと日付の記録・ref にだけある非 ASCII の名前・既定の ref と相対パスでの起動・repo の採番の道具への案内) / 重複の検査 (4 桁の重複・別の番号)\n'
