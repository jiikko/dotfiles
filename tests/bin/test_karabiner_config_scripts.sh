#!/usr/bin/env bash
# bin/restore_karabiner_config.sh / bin/backup_karabiner_config.sh が失敗を成功に見せないことを固定する (issue 610)。
#
# 守るもの: レイアウトのパッチ (jq) が失敗したら非 0 で止まり、.tmp と「restore 済み」の記録を残さない /
# 成功したときだけ記録する / コピーが失敗したら止まる。
# 🚨 両スクリプトは ~ 配下を直接書くので、HOME と XDG_STATE_HOME を使い捨ての場所へ差し替えてから走らせる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RESTORE="$ROOT_DIR/bin/restore_karabiner_config.sh"
BACKUP="$ROOT_DIR/bin/backup_karabiner_config.sh"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq が無い環境"; exit 77; }
REAL_JQ="$(command -v jq)"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-karabiner.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# $1=名前 $2=ioreg が返す言語 (Japanese / ISO / 空) $3=jq の振る舞い (real / fail)。残りは無し
# 結果: RC / H (偽の HOME) / SHA (記録ファイル) / TARGET
setup_home() {
  H="$WORK/$1/home"
  mkdir -p "$H/dotfiles/mac" "$H/.config/karabiner" "$WORK/$1/bin"
  printf '{"profiles":[{"virtual_hid_keyboard":{"keyboard_type":"ansi"}}]}\n' > "$H/dotfiles/mac/karabiner.json"
  printf '#!/bin/sh\n[ -n "%s" ] && echo "\\"KeyboardLanguage\\" = \\"%s\\""\nexit 0\n' "$2" "$2" > "$WORK/$1/bin/ioreg"
  printf '#!/bin/sh\nexit 0\n' > "$WORK/$1/bin/hidutil"
  if [[ "$3" == fail ]]; then
    printf '#!/bin/sh\necho "jq: error (fake)" >&2\nexit 5\n' > "$WORK/$1/bin/jq"
  else
    ln -s "$REAL_JQ" "$WORK/$1/bin/jq"
  fi
  chmod +x "$WORK/$1/bin/"*
  SHA="$H/.local/state/dotfiles/karabiner-json.sha256"
  TARGET="$H/.config/karabiner/karabiner.json"
}

run_in() {  # $1=名前 $2=スクリプト
  RC=0
  HOME="$H" XDG_STATE_HOME="$H/.local/state" PATH="$WORK/$1/bin:$PATH" \
    sh "$2" > "$WORK/$1/out" 2> "$WORK/$1/err" || RC=$?
}

# 1. JIS で jq が失敗 → 非 0、成功の文言を出さない、.tmp も記録も残さない
setup_home jq_fail Japanese fail
run_in jq_fail "$RESTORE"
if [[ $RC -ne 0 ]] && ! grep -q '自動設定' "$WORK/jq_fail/out" && [[ ! -e "$TARGET.tmp" ]] && [[ ! -e "$SHA" ]]; then
  ok "jq の失敗で止まり、.tmp と記録を残さない"
else
  fail "jq の失敗: rc=$RC tmp=$([[ -e "$TARGET.tmp" ]] && echo 有 || echo 無) sha=$([[ -e "$SHA" ]] && echo 有 || echo 無) out=$(cat "$WORK/jq_fail/out")"
fi

# 2. JIS で jq が成功 → rc=0、jis に書き換わり、記録が repo 側の sha と一致
setup_home jis_ok Japanese real
run_in jis_ok "$RESTORE"
want="$(shasum -a 256 "$H/dotfiles/mac/karabiner.json" | awk '{print $1}')"
if [[ $RC -eq 0 ]] && [[ "$(jq -r '.profiles[0].virtual_hid_keyboard.keyboard_type' "$TARGET")" == jis ]] \
  && [[ "$(cat "$SHA" 2>/dev/null)" == "$want" ]]; then
  ok "成功したら jis にして記録する"
else
  fail "jis の成功: rc=$RC $(cat "$WORK/jis_ok/err")"
fi

# 3. ANSI → パッチ無しで rc=0、記録する
setup_home ansi_ok "" real
run_in ansi_ok "$RESTORE"
if [[ $RC -eq 0 ]] && [[ -s "$SHA" ]]; then
  ok "ansi はパッチ無しで記録する"
else
  fail "ansi: rc=$RC $(cat "$WORK/ansi_ok/err")"
fi

# 4. repo 側の設定が無い (cp の失敗) → 非 0、記録しない
setup_home cp_fail Japanese real
rm "$H/dotfiles/mac/karabiner.json"
run_in cp_fail "$RESTORE"
if [[ $RC -ne 0 ]] && [[ ! -e "$SHA" ]]; then
  ok "restore のコピーの失敗で止まる"
else
  fail "restore のコピーの失敗: rc=$RC sha=$([[ -e "$SHA" ]] && echo 有 || echo 無)"
fi

# 5. backup: 適用中の設定が無い (cp の失敗) → 非 0、記録しない
setup_home backup_fail "" real
run_in backup_fail "$BACKUP"
if [[ $RC -ne 0 ]] && [[ ! -e "$SHA" ]] && ! grep -q 'コピーしました' "$WORK/backup_fail/out"; then
  ok "backup のコピーの失敗で止まる"
else
  fail "backup のコピーの失敗: rc=$RC sha=$([[ -e "$SHA" ]] && echo 有 || echo 無)"
fi

# 6. 記録先が書けない (state の dotfiles がファイル) → restore も backup も非 0
for script in restore backup; do
  setup_home "rec_fail_$script" "" real
  mkdir -p "$H/.local/state" && : > "$H/.local/state/dotfiles"
  [[ $script == backup ]] && cp "$H/dotfiles/mac/karabiner.json" "$TARGET"
  if [[ $script == restore ]]; then run_in "rec_fail_$script" "$RESTORE"; else run_in "rec_fail_$script" "$BACKUP"; fi
  if [[ $RC -ne 0 ]] && grep -q '記録を' "$WORK/rec_fail_$script/err"; then
    ok "$script: 記録の失敗で止まる"
  else
    fail "$script: 記録の失敗: rc=$RC $(cat "$WORK/rec_fail_$script/err")"
  fi
done

# 7. backup の成功 → 記録が repo 側の sha と一致
setup_home backup_ok "" real
printf '{"x":1}\n' > "$TARGET"
run_in backup_ok "$BACKUP"
if [[ $RC -eq 0 ]] && [[ "$(cat "$SHA" 2>/dev/null)" == "$(shasum -a 256 "$H/dotfiles/mac/karabiner.json" | awk '{print $1}')" ]]; then
  ok "backup の成功で記録する"
else
  fail "backup の成功: rc=$RC $(cat "$WORK/backup_ok/err")"
fi

if (( fails )); then
  echo "FAIL: $fails 件" >&2
  exit 1
fi
echo "OK karabiner config scripts (8 件)"
