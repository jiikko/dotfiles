#!/usr/bin/env bash
# scripts/lib/terminal_profile_colors.swift (.terminal の color blob デコード) のテスト。
#
# なぜ: 旧 NSArchiver (streamtyped) 形式のフォールバック (NSUnarchiver) は ObjC 例外を投げるため
# Swift では捕捉できず、壊れた旧形式 blob に対して診断メッセージではなく SIGABRT が出ていた
# (実測 2026-08-21: exit 134 / NSArchiverArchiveInconsistency)。呼び出し側
# (terminal_profile_restore.sh) は非 0 終了しか見ないので、abort だと「何が壊れているか」が
# 一切伝わらない。フォールバックを削除して「壊れた blob → 診断 + exit 1」に揃えたことを固定する。
#
# ⚠️ 検査は 2 段構え:
#   - 静的 (どの環境でも走る。CI = ubuntu に swift は無い): NSUnarchiver を使っていないこと
#   - 実行 (macOS + swift + python3 のみ): 壊れた blob で abort せず診断 + exit 1
# 実行系を skip した事実は出力に出す (「検査できなかった」を緑に見せない)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SWIFT_SRC="$ROOT_DIR/scripts/lib/terminal_profile_colors.swift"
PROFILE="$ROOT_DIR/mac/ClaudeWarm.terminal"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0

ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

[ -f "$SWIFT_SRC" ] || { ng "デコーダが無い: $SWIFT_SRC"; exit 1; }

# --- 1. 静的: 捕捉不能な旧 API を使わない (どの環境でも走る) --------------------------------
# NSUnarchiver の例外は Swift の try? を素通りして SIGABRT になるため、復活させてはいけない。
# 旧形式を読む必要が出たら ObjC 側で例外を捕まえるラッパを噛ませる (ヘッダのコメント参照)。
# コメントは落としてから見る (ヘッダが「なぜ持たないか」の説明で NSUnarchiver に言及するため)。
code_only="$(sed 's|//.*||' "$SWIFT_SRC")"
if grep -q 'NSUnarchiver' <<< "$code_only"; then
  ng "NSUnarchiver が復活している (ObjC 例外は Swift で捕捉できず SIGABRT になる)"
  printf '%s\n' "$code_only" | grep -n 'NSUnarchiver' | head -3
else
  ok "捕捉不能な NSUnarchiver フォールバックを持たない"
fi

# --- 実行系の前提を確認 ---------------------------------------------------------------------
if ! command -v swift >/dev/null 2>&1; then
  printf 'SKIP: swift が無い環境なので実行系の検査は落とした (静的検査のみ実施)\n'
  [ "$fails" -eq 0 ] || { printf '\nFAIL: %d 件失敗\n' "$fails"; exit 1; }
  printf '\nAll terminal-profile-colors tests passed successfully! (静的検査のみ)\n'
  exit 0
fi
if ! command -v python3 >/dev/null 2>&1; then
  printf 'SKIP: python3 が無い環境なので壊れた blob の fixture を作れない (静的検査のみ実施)\n'
  [ "$fails" -eq 0 ] || { printf '\nFAIL: %d 件失敗\n' "$fails"; exit 1; }
  printf '\nAll terminal-profile-colors tests passed successfully! (静的検査のみ)\n'
  exit 0
fi
[ -f "$PROFILE" ] || { ng "プロファイルが無い: $PROFILE"; exit 1; }

# 壊れた blob を持つ .terminal を 2 種作る (現行形式の破損 / 旧 streamtyped 形式の破損)
python3 - "$PROFILE" "$WORK" <<'PYEOF'
import plistlib, sys
src, work = sys.argv[1], sys.argv[2]
d = plistlib.load(open(src, 'rb'))
broken_keyed = dict(d)
broken_keyed['BackgroundColor'] = b'bplist00' + b'\x00' * 20
plistlib.dump(broken_keyed, open(work + '/broken-keyed.terminal', 'wb'))
# 旧 NSArchiver 形式のヘッダを持つ切り詰め blob (NSUnarchiver が例外を投げる形)
broken_st = dict(d)
broken_st['BackgroundColor'] = b'\x04\x0bstreamtyped\x81\xe8\x03\x84\x01' + b'\x00' * 8
plistlib.dump(broken_st, open(work + '/broken-streamtyped.terminal', 'wb'))
PYEOF

# ⚠️ 本番 (terminal_profile_restore.sh) と同じ呼び方にする。-suppress-warnings を付けて測ると
# 「本番では warning が混ざって壊れる」形を観測できない。
run_decoder() { swift "$SWIFT_SRC" "$1" >"$WORK/out" 2>"$WORK/err"; }

# --- 2. 正常系: 4 キーを「キー R G B」で出して exit 0 --------------------------------------
if run_decoder "$PROFILE"; then
  lines=$(wc -l < "$WORK/out" | tr -d ' ')
  if [ "$lines" = "4" ] && awk 'NF != 4 || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ { exit 1 }' "$WORK/out"; then
    ok "正常な .terminal から 4 キーの RGB が出る"
  else
    ng "出力の形が想定と違う ($lines 行)"; cat "$WORK/out"
  fi
else
  ng "正常な .terminal のデコードが失敗した (exit $?)"; cat "$WORK/err"
fi

# --- 3. 壊れた blob: abort ではなく診断 + exit 1 --------------------------------------------
# ⚠️ ここが 084 の本体。exit >= 128 はシグナル死 (SIGABRT=134) で、呼び出し側には
# 「何が壊れているか」が伝わらない。
for case_name in broken-keyed broken-streamtyped; do
  set +e
  run_decoder "$WORK/$case_name.terminal"
  rc=$?
  set -e
  if [ "$rc" -ge 128 ]; then
    ng "$case_name: シグナル $((rc - 128)) で落ちた (診断が出ない。NSUnarchiver 系の abort)"
    head -3 "$WORK/err"
  elif [ "$rc" -ne 1 ]; then
    ng "$case_name: 終了コードが 1 でない (rc=$rc)"
  elif ! grep -q 'をデコードできない' "$WORK/err"; then
    ng "$case_name: 診断メッセージが出ていない"; head -3 "$WORK/err"
  else
    ok "$case_name: 診断 + exit 1 (abort しない)"
  fi
done

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: terminal-profile-colors のテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll terminal-profile-colors tests passed successfully!\n'
