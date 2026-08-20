#!/usr/bin/env bash
# scripts/terminal_profile_restore.sh の AppleScript 組み立ての回帰テスト。
#
# なぜ: プロファイル名 (`name`) は .terminal ファイル = **第 1 引数で任意に差し替えられる外部
# 入力**由来。これを AppleScript のソース文字列へ埋めると `"` で文字列を脱出して
# `do shell script` に到達する (監査 2026-08-21 が細工ファイルで marker 生成に成功)。
# 名前は `on run argv` の argv でデータとして渡すのが正で、その形が崩れていないかを固定する。
#
# ⚠️ 実行系のテスト (osascript) は macOS 限定なので、無い環境では構造検査だけに落とす
# (「検査できなかった」を緑にしないため、落とした事実は出力に出す)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/terminal_profile_restore.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0

ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

[ -f "$SCRIPT" ] || { ng "スクリプトが無い: $SCRIPT"; exit 1; }

# --- 1. 構造: AppleScript の組み立てに $NAME を埋めていない -------------------------------
# as_lines の組み立て区間 (on run argv 〜 osascript 呼び出し) に $NAME が現れてはいけない。
# ⚠️ 範囲指定 (/a/,/b/) は終端行も含むので、名前を argv で渡す osascript 行そのものが
# 混ざって偽の失敗になる。フラグで終端行を除く。
block="$(awk '/as_lines="on run argv/{f=1} /osascript -e "\$as_lines"/{f=0} f' "$SCRIPT")"
if [ -z "$block" ]; then
  ng "as_lines の組み立て区間が見つからない (argv 方式が壊れた / 書き換えられた)"
elif printf '%s' "$block" | grep -q '\$NAME'; then
  ng "AppleScript の組み立てに \$NAME を埋めている (文字列脱出でコード実行に到達する)"
  printf '%s\n' "$block" | grep -n '\$NAME' | head -3
else
  ok "AppleScript の組み立てに名前を埋めていない (argv 経由)"
fi

# --- 2. 構造: osascript に名前を引数として渡している ---------------------------------------
if grep -q 'osascript -e "\$as_lines" "\$NAME"' "$SCRIPT"; then
  ok "osascript へ名前を argv で渡している"
else
  ng "osascript の呼び出しが argv 形式でない"
  grep -n 'osascript -e' "$SCRIPT" | head -3
fi

# --- 3. 意味論: argv 経由なら細工した名前でもコードが実行されない (macOS のみ) --------------
if command -v osascript >/dev/null 2>&1; then
  marker="$WORK/MARKER"
  evil='pwn"
  do shell script "touch '"$marker"'"
  set x to "'
  got="$(osascript -e 'on run argv
  set n to item 1 of argv
  return "len:" & (count of n)
end run' "$evil" 2>&1 || true)"
  if [ -e "$marker" ]; then
    ng "argv 経由でも payload が実行された (marker が作られた): $got"
  elif printf '%s' "$got" | grep -q '^len:'; then
    ok "細工した名前は argv でデータとして扱われる ($got)"
  else
    ng "osascript が想定外の応答: $got"
  fi
else
  printf 'SKIP: osascript が無い環境なので実行系の検査は落とした (構造検査のみ実施)\n'
fi

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: terminal_profile_restore のテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll terminal-profile-restore tests passed successfully!\n'
