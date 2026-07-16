#!/bin/sh
# Terminal.app の見た目プロファイルを repo 管理の .terminal から復元し、既定プロファイルにする。
# 「新規ウィンドウのたびにプリセットを手で選ぶ」を根絶する (既定化すれば選択操作自体が不要になる)。
#
# 使い方: scripts/terminal_profile_restore.sh [.terminal ファイル]  (省略時 mac/ClaudeWarm.terminal)
#
# 仕組み: プロファイルの実体は defaults の com.apple.Terminal に入っている。
#   - "Window Settings"                  = プロファイル名 → 設定 dict の辞書 (登録)
#   - "Default/Startup Window Settings"  = 既定プロファイル名 (選択)
# ⚠️ Terminal 稼働中は prefs をアプリがメモリ上に持ち終了時に書き戻すため、defaults へ直接
#   書いても quit 時に巻き戻る。稼働中は AppleScript (Terminal 自身の状態を変える = quit 時に
#   そのまま永続化) で settings set を構築する。open による import 経路は採らない: 余計な窓が
#   1 枚開く上、blob が Terminal-native な NSKeyedArchiver 形式でないと「ファイルが壊れています」
#   で拒否される (旧 NSArchiver 形式の生成ファイルで実測。現ファイルは Terminal 自身の序列化から
#   再生成済みなので import 自体は可能になったが、上記の理由で AppleScript 構築に一本化)。
#   osascript は Terminal の公式スクリプティング API のみ (UI 要素操作はしない)。
# 色の単一ソースは .terminal ファイル: 稼働中経路は lib/terminal_profile_colors.swift が
#   blob をデコードして AppleScript に渡す (ここに RGB をハードコードすると repo と drift する)。
set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
FILE="${1:-$SCRIPT_DIR/../mac/ClaudeWarm.terminal}"
[ -f "$FILE" ] || { echo "✗ プロファイルファイルが無い: $FILE" >&2; exit 1; }

# プロファイル名は plist の name キーが真 (ファイル名とは別物。ClaudeWarm.terminal の中身は "Claude Warm")
NAME=$(plutil -extract name raw -o - "$FILE")
[ -n "$NAME" ] || { echo "✗ $FILE に name キーが無い (プロファイル書き出しファイルではない?)" >&2; exit 1; }

if pgrep -xq Terminal; then
  command -v swift >/dev/null 2>&1 || {
    echo "✗ swift が無い (色デコードに必要)。Terminal を終了してから再実行すれば defaults 経路で設定できる。" >&2
    exit 1
  }
  colors=$(swift "$SCRIPT_DIR/lib/terminal_profile_colors.swift" "$FILE")
  as_lines="tell application \"Terminal\"
  if not (exists settings set \"$NAME\") then
    make new settings set with properties {name:\"$NAME\"}
  end if"
  while IFS=' ' read -r key r g b; do
    case "$key" in
      BackgroundColor) prop="background color" ;;
      TextColor)       prop="normal text color" ;;
      TextBoldColor)   prop="bold text color" ;;
      CursorColor)     prop="cursor color" ;;
      *) continue ;;
    esac
    as_lines="$as_lines
  set $prop of settings set \"$NAME\" to {$r, $g, $b}"
  done <<EOF
$colors
EOF
  as_lines="$as_lines
  set default settings to settings set \"$NAME\"
  set startup settings to settings set \"$NAME\"
end tell"
  osascript -e "$as_lines" >/dev/null || {
    echo "✗ AppleScript での構築に失敗 (オートメーション許可が未付与の可能性)。" >&2
    echo "  Terminal を終了した状態で再実行すれば defaults 経路で設定できる。" >&2
    exit 1
  }
else
  # 非稼働: defaults に直接書ける (repo のファイルで登録を上書き = repo が真の restore 経路)
  defaults write com.apple.Terminal "Window Settings" -dict-add "$NAME" "$(cat "$FILE")"
  defaults write com.apple.Terminal "Default Window Settings" -string "$NAME"
  defaults write com.apple.Terminal "Startup Window Settings" -string "$NAME"
fi

echo "✓ Terminal 既定プロファイル = $NAME (新規ウィンドウ/起動時に自動適用)"
