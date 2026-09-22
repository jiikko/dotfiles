#!/bin/sh
# Terminal.app の見た目プロファイル (色 + フォント) を repo 管理の .terminal から復元し、
# 既定プロファイルにする。
# 「新規ウィンドウのたびにプリセットを手で選ぶ」を根絶する (既定化すれば選択操作自体が不要になる)。
#
# 使い方: scripts/terminal_profile_restore.sh [.terminal ファイル]  (省略時 mac/ClaudeWarm.terminal)
#
# 仕組み: プロファイルの実体は defaults の com.apple.Terminal に入っている。
#   - "Window Settings"                  = プロファイル名 → 設定 dict の辞書 (登録)
#   - "Default/Startup Window Settings"  = 既定プロファイル名 (選択)
# 🚨 Terminal 稼働中は prefs をアプリがメモリ上に持ち終了時に書き戻すため、defaults へ直接
#   書いても quit 時に巻き戻る。稼働中は AppleScript (Terminal 自身の状態を変える = quit 時に
#   そのまま永続化) で settings set を構築する。open による import 経路は採らない: 余計な窓が
#   1 枚開く上、blob が Terminal-native な NSKeyedArchiver 形式でないと「ファイルが壊れています」
#   で拒否される (旧 NSArchiver 形式の生成ファイルで実測。現ファイルは Terminal 自身の序列化から
#   再生成済みなので import 自体は可能になったが、上記の理由で AppleScript 構築に一本化)。
#   osascript は Terminal の公式スクリプティング API のみ (UI 要素操作はしない)。
# 色とフォントの単一ソースは .terminal ファイル: 稼働中経路は lib/terminal_profile_appearance.swift
#   が blob をデコードして AppleScript に渡す (ここに RGB やフォント名をハードコードすると repo と
#   drift する)。フォントの実体は Brewfile の `cask "font-hack-nerd-font"` で入る。
set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
FILE="${1:-$SCRIPT_DIR/../mac/ClaudeWarm.terminal}"
[ -f "$FILE" ] || { echo "✗ プロファイルファイルが無い: $FILE" >&2; exit 1; }

# プロファイル名は plist の name キーが真 (ファイル名とは別物。ClaudeWarm.terminal の中身は "Claude Warm")
NAME=$(plutil -extract name raw -o - "$FILE")
[ -n "$NAME" ] || { echo "✗ $FILE に name キーが無い (プロファイル書き出しファイルではない?)" >&2; exit 1; }

FONT_MISSING_HINT='  未導入なら: brew install --cask font-hack-nerd-font (Brewfile 記載)'

if pgrep -xq Terminal; then
  command -v swift >/dev/null 2>&1 || {
    echo "✗ swift が無い (色/フォントのデコードに必要)。Terminal を終了してから再実行すれば defaults 経路で設定できる。" >&2
    exit 1
  }
  # 🚨 -suppress-warnings は付けない: 抑止していた deprecation warning の出元 (旧 streamtyped
  # blob 用の NSUnarchiver フォールバック) を削除したので、今は 0 件 (実測 2026-08-21)。
  # 付け直すと将来の実 warning も一緒に隠れる。
  props=$(swift "$SCRIPT_DIR/lib/terminal_profile_appearance.swift" "$FILE")
  # 🚨 プロファイル名とフォント名を AppleScript のソース文字列へ埋めないこと。どちらも .terminal
  # ファイル (= 第 1 引数で任意に差し替えられる外部入力) 由来なので、`"` を含む値で文字列を脱出でき
  # **`do shell script` に到達する**。実在形式の細工ファイルで marker 生成に成功した
  # (実測 2026-08-21。「✗ 構築に失敗」表示より前に payload が走っていた)。
  # 文字列は `on run argv` の argv で**データとして**渡す (色の数値とフォントサイズだけは case で
  # 語彙を固定したプロパティ名と数値なので、そのまま埋めても外部入力にならない)。
  as_lines="on run argv
  set profileName to item 1 of argv
  set fontName to item 2 of argv
  tell application \"Terminal\"
  if not (exists settings set profileName) then
    make new settings set with properties {name:profileName}
  end if"
  font_name=''
  font_resolved=''
  font_size=''
  font_available=''
  while IFS=' ' read -r key a b c d; do
    case "$key" in
      BackgroundColor) prop="background color" ;;
      TextColor)       prop="normal text color" ;;
      TextBoldColor)   prop="bold text color" ;;
      CursorColor)     prop="cursor color" ;;
      # 🚨 Font 行は 1 本目だけ採る。2 本目を後勝ちで拾うと、デコーダの出力へ行を注入できた場合に
      # 在庫判定を偽造される。**デコーダ側の語彙検査で注入自体が塞がっているので、この段は
      # 現状どの入力からも到達しない = 自動テストが無い** (語彙検査を外したときの保険として残す)。
      Font)            [ -n "$font_name" ] && continue
                       font_name="$a"; font_resolved="$b"; font_size="$c"; font_available="$d"; continue ;;
      *) continue ;;
    esac
    # r/g/b は swift 側が数値で出す。念のため数字とカンマ以外を弾く (壊れた出力で AppleScript を
    # 組み立てないため。ここが数値でなければその行は捨てる = 色が 1 つ落ちるだけで済む)。
    # 🚨 上の Font 行と同じく、デコーダ側の検査で到達しなくなったので**自動テストは無い**。
    case "$a$b$c" in *[!0-9.]*) continue ;; esac
    as_lines="$as_lines
  set $prop of settings set profileName to {$a, $b, $c}"
  done <<EOF
$props
EOF
  # 🚨 未導入のフォントは設定しない。Terminal はエラーを出さずに SFMonoTerminal-Regular へ
  # 黙って差し替え、**その代替名が profile に保存される** (実測 2026-09-23: 使い捨ての
  # settings set に存在しない名前を設定したら AppleScript は成功し、読み戻しが差し替え後の
  # 名前だった)。設定すると「repo の指定」が代替名で上書きされるので、警告だけ出して触らない。
  if [ -n "$font_name" ] && [ "$font_available" = "1" ]; then
    case "$font_size" in ''|*[!0-9.]*) font_size='' ;; esac
    as_lines="$as_lines
  set font name of settings set profileName to fontName"
    if [ -n "$font_size" ]; then
      as_lines="$as_lines
  set font size of settings set profileName to $font_size"
    fi
  else
    if [ -n "$font_name" ]; then
      echo "WARN: フォント $font_name が見つからないので設定しない (既存のフォントのまま)" >&2
      echo "$FONT_MISSING_HINT" >&2
    fi
    font_name=''
  fi
  as_lines="$as_lines
  set default settings to settings set profileName
  set startup settings to settings set profileName
  return (font name of settings set profileName) & \" \" & (font size of settings set profileName as text)
end tell
end run"
  applied=$(osascript -e "$as_lines" "$NAME" "$font_name") || {
    echo "✗ AppleScript での構築に失敗 (オートメーション許可が未付与の可能性)。" >&2
    echo "  Terminal を終了した状態で再実行すれば defaults 経路で設定できる。" >&2
    exit 1
  }
  # 🚨 「設定した」で閉じず読み戻しで確かめる (上記のとおり Terminal は黙って差し替える)。
  # 比較先は**解決後の名前** (font_resolved)。Terminal は family 名を PostScript 名へ解決して
  # 保存する (Menlo → Menlo-Regular。実測 2026-09-23) ので、アーカイブの名前と比べると
  # フォントが入っているのに「未導入」と誤報する。
  if [ -n "$font_name" ] && [ "${applied% *}" != "$font_resolved" ]; then
    echo "WARN: フォントが $font_resolved にならなかった (実際: $applied)" >&2
    echo "$FONT_MISSING_HINT" >&2
  fi
else
  # 非稼働: defaults に直接書ける (repo のファイルで登録を上書き = repo が真の restore 経路)。
  # Font は .terminal の blob ごと入るので、ここでは在庫の有無だけ見て警告する。
  defaults write com.apple.Terminal "Window Settings" -dict-add "$NAME" "$(cat "$FILE")"
  defaults write com.apple.Terminal "Default Window Settings" -string "$NAME"
  defaults write com.apple.Terminal "Startup Window Settings" -string "$NAME"
  if command -v swift >/dev/null 2>&1; then
    # 🚨 パイプで繋ぐと rc が sed のものになり、**swift の失敗が無音で消える** (/bin/sh に
    # pipefail は無い)。デコードの成否を先に確かめてから抽出する。
    if props=$(swift "$SCRIPT_DIR/lib/terminal_profile_appearance.swift" "$FILE"); then
      font_line=$(printf '%s\n' "$props" | sed -n 's/^Font //p' | head -1)
      case "$font_line" in
        *' 0') echo "WARN: フォント ${font_line%% *} が未導入 (代替フォントで表示される)" >&2
               echo "$FONT_MISSING_HINT" >&2 ;;
      esac
    else
      echo "WARN: プロファイルのデコードに失敗したのでフォントの在庫を確認できなかった" >&2
    fi
  fi
fi

echo "✓ Terminal 既定プロファイル = $NAME (新規ウィンドウ/起動時に自動適用)"
