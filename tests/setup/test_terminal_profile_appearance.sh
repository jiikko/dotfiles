#!/usr/bin/env bash
# scripts/lib/terminal_profile_appearance.swift (.terminal の色 + フォントのデコード) のテスト。
#
# なぜ (色): 旧 NSArchiver (streamtyped) 形式のフォールバック (NSUnarchiver) は ObjC 例外を投げるため
# Swift では捕捉できず、壊れた旧形式 blob に対して診断メッセージではなく SIGABRT が出ていた
# (実測 2026-08-21: exit 134 / NSArchiverArchiveInconsistency)。呼び出し側
# (terminal_profile_restore.sh) は非 0 終了しか見ないので、abort だと「何が壊れているか」が
# 一切伝わらない。フォールバックを削除して「壊れた blob → 診断 + exit 1」に揃えたことを固定する。
#
# なぜ (フォント): 敵対レビュー 2026-09-23 が実証した 3 つを固定する。
#   1. **未インストールのフォントは NSFont として unarchive した時点で代替に差し替わる**
#      (`.AppleSystemUIFont` になる)。その名前を restore が渡すと、フォント未導入のマシンで
#      プロファイルが UI フォントに書き換わる → アーカイブの NSName を verbatim で読む。
#   2. **フォント名に改行を入れると出力プロトコル (空白区切り 1 行 1 プロパティ) へ任意行を注入でき**、
#      色の上書きと「利用可否 1」の偽造が通っていた → PostScript 名の語彙に限る。
#   3. **サイズが inf / Int64 超過だと `Int()` 変換が SIGTRAP** で落ち、084 が潰した
#      「abort させない」不変条件が Font 経由で戻っていた → 範囲検査。
#   4. 同型が**色側にも在った** (NSRGB が NaN。inf / 負値は NSColor がクランプするので NaN だけ)
#      → 成分の有限性を検査。**フォント側だけ直して横展開を grep しなかった**形だったので、
#      色とフォントの両方を同じ表で回す。
#   5. `.AppleSystemUIFont` は新しい語彙検査を通ってしまう。これは**修正前のバグが profile へ
#      書き戻していた当の文字列**なので、先頭 `.` の system font を弾く。
#
# 🚨 検査は 2 段構え:
#   - 静的 (どの環境でも走る。swift が無い環境がありうる)
#   - 実行 (macOS + swift + python3 のみ)
# 実行系を skip した事実は出力に出す (「検査できなかった」を緑に見せない)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SWIFT_SRC="$ROOT_DIR/scripts/lib/terminal_profile_appearance.swift"
PROFILE="$ROOT_DIR/mac/ClaudeWarm.terminal"
MAKE_FIXTURES="$ROOT_DIR/tests/setup/lib/make_terminal_fixtures.py"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
fails=0

ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

[ -f "$SWIFT_SRC" ] || { ng "デコーダが無い: $SWIFT_SRC"; exit 1; }

# --- 0. canary: 静的検査の抽出が生きていることを先に較正する --------------------------------
# 🚨 `if grep …; then ng; else ok` の形は、**grep が動かなくても ✓ を出す** (条件式なので
# rc≠0 が致命にならない)。「マッチしなかった」と「検査できなかった」が同じ緑に畳まれるので、
# 必ず当たるはずの語で先に較正する (敵対レビュー 2026-09-23 が PATH から grep を外して実証)。
code_only="$(sed 's|//.*||' "$SWIFT_SRC")"
if grep -q 'NSKeyedUnarchiver' <<< "$code_only"; then
  ok "canary: 静的検査の抽出が生きている (必ず在る語に当たる)"
else
  ng "canary が当たらない: 静的検査は何も見ていない (grep / sed / ファイル構成が壊れた)"
  exit 1
fi

# --- 1. 静的: 捕捉不能な旧 API を使わない ----------------------------------------------------
# NSUnarchiver の例外は Swift の try? を素通りして SIGABRT になるため、復活させてはいけない。
# 旧形式を読む必要が出たら ObjC 側で例外を捕まえるラッパを噛ませる (ヘッダのコメント参照)。
# コメントは落としてから見る (ヘッダが「なぜ持たないか」の説明で NSUnarchiver に言及するため)。
if grep -q 'NSUnarchiver' <<< "$code_only"; then
  ng "NSUnarchiver が復活している (ObjC 例外は Swift で捕捉できず SIGABRT になる)"
  grep -n 'NSUnarchiver' <<< "$code_only" | head -3
else
  ok "捕捉不能な NSUnarchiver フォールバックを持たない"
fi

# --- 1b. 静的: フォントを NSFont として unarchive しない -------------------------------------
# NSFont 経由で読むと未導入フォントが代替名に差し替わり、その名前が profile へ書き戻される。
# 🚨 綴りは 1 種類に絞らない (`ofClass: NSFont.self` / `of: NSFont.self` のどちらでも同じ事故)。
# 在庫判定の `NSFont(name:size:)` は別物なので、`NSFont.self` だけを見る。
if grep -q 'NSFont\.self' <<< "$code_only"; then
  ng "Font を NSFont として unarchive している (未導入フォントが代替名に差し替わる)"
  grep -n 'NSFont\.self' <<< "$code_only" | head -3
else
  ok "Font を NSFont 経由で unarchive していない (アーカイブの名前を verbatim で読む)"
fi

# --- 実行系の前提を確認 ---------------------------------------------------------------------
skip_runtime() {
  printf 'SKIP: %s なので実行系の検査は落とした (静的検査のみ実施)\n' "$1"
  [ "$fails" -eq 0 ] || { printf '\nFAIL: %d 件失敗\n' "$fails"; exit 1; }
  printf '\nAll terminal-profile-appearance tests passed successfully! (静的検査のみ)\n'
  exit 0
}
command -v swift   >/dev/null 2>&1 || skip_runtime "swift が無い環境"
command -v python3 >/dev/null 2>&1 || skip_runtime "python3 が無い環境 (fixture を作れない)"
[ -f "$PROFILE" ] || { ng "プロファイルが無い: $PROFILE"; exit 1; }

MISSING_FONT_NAME="$(python3 "$MAKE_FIXTURES" "$PROFILE" "$WORK")"

# 🚨 本番 (terminal_profile_restore.sh) と同じ呼び方にする。-suppress-warnings を付けて測ると
# 「本番では warning が混ざって壊れる」形を観測できない。
run_decoder() { swift "$SWIFT_SRC" "$1" >"$WORK/out" 2>"$WORK/err"; }

# --- 2. 正常系: 色 4 キー (キー名まで) + Font 1 行 ------------------------------------------
if run_decoder "$PROFILE"; then
  # 🚨 キー名も見る。restore 側は `case "$key" in BackgroundColor) … *) continue` で知らないキーを
  # 全部捨てるので、キー名が変わる退行は「色が 1 つも設定されない」完全な機能喪失になる。
  missing=''
  for key in BackgroundColor TextColor TextBoldColor CursorColor; do
    grep -qE "^$key [0-9]+ [0-9]+ [0-9]+$" "$WORK/out" || missing="$missing $key"
  done
  if [ -z "$missing" ]; then
    ok "色 4 キーが「キー名 R G B」で出る"
  else
    ng "色の行が無い / 形が想定と違う:$missing"; cat "$WORK/out"
  fi
  # Font <アーカイブの名前> <解決後の名前|-> <サイズ> <利用可否 0|1>
  if awk '/^Font /{ if (NF == 5 && $2 ~ /^[A-Za-z0-9._+-]+$/ && $3 ~ /^([A-Za-z0-9._+-]+|-)$/ \
                       && $4 ~ /^[0-9.]+$/ && $5 ~ /^[01]$/) { found = 1 } }
          END { exit found ? 0 : 1 }' "$WORK/out"; then
    ok "Font 行が「名前 解決後の名前 サイズ 利用可否」で出る ($(grep '^Font ' "$WORK/out"))"
  else
    ng "Font 行が無い / 形が想定と違う"; cat "$WORK/out"
  fi
else
  ng "正常な .terminal のデコードが失敗した (exit $?)"; cat "$WORK/err"
fi

# --- 3. 壊れた blob / 不正な値: abort ではなく診断 + exit 1 ----------------------------------
# 🚨 ここが 084 の本体。exit >= 128 はシグナル死 (SIGABRT=134 / SIGTRAP=133) で、呼び出し側には
# 「何が壊れているか」が伝わらない。huge-font は Font 経由で同じ失敗モードが戻った形。
for case_name in broken-keyed broken-streamtyped broken-font inject-font huge-font system-font color-nan; do
  set +e
  run_decoder "$WORK/$case_name.terminal"
  rc=$?
  set -e
  if [ "$rc" -ge 128 ]; then
    ng "$case_name: シグナル $((rc - 128)) で落ちた (診断が出ない)"
    head -3 "$WORK/err"
  elif [ "$rc" -ne 1 ]; then
    ng "$case_name: 終了コードが 1 でない (rc=$rc)"; head -3 "$WORK/out" "$WORK/err"
  elif ! grep -qE '^✗ ' "$WORK/err"; then
    ng "$case_name: 診断メッセージが出ていない"; head -3 "$WORK/err"
  else
    ok "$case_name: 診断 + exit 1 ($(grep -m1 -E '^✗ ' "$WORK/err"))"
  fi
done

# --- 3b. 注入: 細工した行が出力に 1 つも現れない ---------------------------------------------
# 上の rc 検査だけだと「exit 1 の前に注入行を print 済み」を見逃す (呼び出し側は set -eu で
# 止まるが、出力を別経路で読む実装に変わった瞬間に効く)。
set +e
run_decoder "$WORK/inject-font.terminal"
set -e
if grep -q '65535 0 0' "$WORK/out"; then
  ng "注入された色の行が stdout に出ている"; cat "$WORK/out"
else
  ok "注入された行は出力に現れない"
fi

# --- 4. 未導入フォント: 名前は verbatim / 解決後は - / 利用可否は 0 --------------------------
# 🚨 これが「NSFont として unarchive しない」ことの挙動側の固定。NSFont 経由に戻すと
# 名前が代替 (.AppleSystemUIFont 等) に化け、利用可否も 1 になる。
if run_decoder "$WORK/missing-font.terminal"; then
  got="$(grep '^Font ' "$WORK/out" || true)"
  if [ "$(awk '{print $2, $3, $5}' <<< "$got")" = "$MISSING_FONT_NAME - 0" ]; then
    ok "未導入フォントは名前が verbatim のまま利用可否 0 ($got)"
  else
    ng "未導入フォントの出力が想定と違う (期待: $MISSING_FONT_NAME - … 0): $got"
  fi
else
  ng "未導入フォントの .terminal でデコードが失敗した"; cat "$WORK/err"
fi

# --- 5. Font キーが無い .terminal: 色だけ出して exit 0 ---------------------------------------
if run_decoder "$WORK/no-font.terminal"; then
  if grep -q '^Font ' "$WORK/out"; then
    ng "Font キーが無いのに Font 行が出た"; cat "$WORK/out"
  else
    ok "Font キーが無い .terminal は色だけ出して成功する (旧い書き出しとの互換)"
  fi
else
  ng "Font キーが無い .terminal でデコードが失敗した (フォントは任意のはず)"; cat "$WORK/err"
fi

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: terminal-profile-appearance のテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll terminal-profile-appearance tests passed successfully!\n'
