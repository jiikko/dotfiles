#!/usr/bin/env bash
# scripts/terminal_profile_restore.sh の AppleScript 組み立ての回帰テスト。
#
# なぜ: プロファイル名 (`name`) は .terminal ファイル = **第 1 引数で任意に差し替えられる外部
# 入力**由来。これを AppleScript のソース文字列へ埋めると `"` で文字列を脱出して
# `do shell script` に到達する (監査 2026-08-21 が細工ファイルで marker 生成に成功)。
# 名前は `on run argv` の argv でデータとして渡すのが正で、その形が崩れていないかを固定する。
#
# 🚨 実行系のテスト (osascript) は macOS 限定なので、無い環境では構造検査だけに落とす
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
# 🚨 範囲指定 (/a/,/b/) は終端行も含むので、名前を argv で渡す osascript 行そのものが
# 混ざって偽の失敗になる。フラグで終端行を除く。
block="$(awk '/as_lines="on run argv/{f=1} /osascript -e "\$as_lines"/{f=0} f' "$SCRIPT")"
if [ -z "$block" ]; then
  ng "as_lines の組み立て区間が見つからない (argv 方式が壊れた / 書き換えられた)"
elif grep -q '\$NAME' <<< "$block"; then
  ng "AppleScript の組み立てに \$NAME を埋めている (文字列脱出でコード実行に到達する)"
  grep -n '\$NAME' <<< "$block" | head -3
else
  ok "AppleScript の組み立てに名前を埋めていない (argv 経由)"
fi

# --- 2. 構造: osascript に名前とフォント名を引数として渡している ---------------------------
if grep -q 'osascript -e "\$as_lines" "\$NAME" "\$font_name"' "$SCRIPT"; then
  ok "osascript へ名前とフォント名を argv で渡している"
else
  ng "osascript の呼び出しが argv 形式でない"
  grep -n 'osascript -e' "$SCRIPT" | head -3
fi

# --- 2b. 構造: フォント名も AppleScript のソースへ埋めていない -----------------------------
# フォント名も .terminal 由来の外部入力なので $NAME と同じ扱いが要る。AppleScript 側は argv で
# 受けた変数 (fontName) を参照し、シェル変数 ($font_name) を文字列へ展開してはいけない。
if ! grep -q 'set fontName to item 2 of argv' "$SCRIPT"; then
  ng "AppleScript がフォント名を argv (item 2) で受けていない"
elif ! grep -q 'set font name of settings set profileName to fontName' "$SCRIPT"; then
  ng "AppleScript が argv 由来の変数でフォントを設定していない"
  grep -n 'set font name' "$SCRIPT" | head -3
elif grep -q '\$font_name' <<< "$(grep -n 'set font name' "$SCRIPT")"; then
  ng "フォント名を AppleScript のソースへ埋めている (文字列脱出でコード実行に到達する)"
  grep -n 'set font name' "$SCRIPT" | head -3
else
  ok "フォント名も argv 経由で渡している (ソースへ埋めていない)"
fi

# --- 2c. 構造: 設定後に読み戻して確かめている ----------------------------------------------
# 🚨 Terminal は未導入フォントをエラー無しで代替へ差し替える (実測 2026-09-23)。
# 「AppleScript が成功した」は「その字面になった」を意味しないので、読み戻しの比較が要る。
if ! grep -q 'return (font name of settings set profileName)' "$SCRIPT"; then
  ng "AppleScript がフォント名を読み戻していない"
elif ! grep -q '\${applied% \*}" != "\$font_resolved' "$SCRIPT"; then
  # 🚨 `grep -q applied` では足りない。代入行 (`applied=$(osascript …)`) にも当たるので、
  # **比較だけ落とす**退行 (ログ用に変数は残す形) を素通りする (敵対レビュー 2026-09-23 が実証)。
  ng "読み戻しの比較が無い / 比較先が解決後の名前でない"
  grep -n 'applied' "$SCRIPT" | head -3
else
  ok "読み戻した名前を解決後の名前と比較している"
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
  elif grep -q '^len:' <<< "$got"; then
    ok "細工した名前は argv でデータとして扱われる ($got)"
  else
    ng "osascript が想定外の応答: $got"
  fi
else
  printf 'SKIP: osascript が無い環境なので実行系の検査は落とした (構造検査のみ実施)\n'
fi

# --- 4. 挙動: fake osascript で「実際に何を組み立てたか」を見る -----------------------------
# 🚨 ここまでは全部 grep の pin で、**シェル側の 2 段 (在庫ゲート / 読み戻し比較) はどちらを
# 外しても緑のまま**だった (敵対レビュー 2026-09-23 が変異で実証)。段が実際に効いているかは
# 組み立て結果を見ないと分からないので、osascript と pgrep を PATH 先頭の stub に差し替える。
# stub は実体を一切呼ばない (本物の Terminal 設定は 1 バイトも変えない)。
if ! command -v swift >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
  printf 'SKIP: swift / python3 が無い環境なので挙動の検査は落とした (静的検査のみ実施)\n'
else
  PROFILE="$ROOT_DIR/mac/ClaudeWarm.terminal"
  MAKE_FIXTURES="$ROOT_DIR/tests/setup/lib/make_terminal_fixtures.py"
  mkdir -p "$WORK/bin"
  printf '#!/bin/sh\nexit 0\n' > "$WORK/bin/pgrep"          # Terminal 稼働中として扱う
  cat > "$WORK/bin/osascript" <<'STUB'
#!/bin/sh
# 本物を呼ばない完全な fake。渡された引数を 1 行 1 個で落とし、読み戻し値を返す。
: > "$OSA_ARGS"
for a in "$@"; do printf '%s\n<<<ARG>>>\n' "$a" >> "$OSA_ARGS"; done
printf '%s\n' "$FAKE_READBACK"
STUB
  chmod +x "$WORK/bin/pgrep" "$WORK/bin/osascript"
  python3 "$MAKE_FIXTURES" "$PROFILE" "$WORK" > /dev/null

  # $1=説明用ラベル, $2=.terminal, $3=読み戻し値 → out/err/args と rc を作る
  run_restore() {
    # 🚨 隔離は PATH 順だけに頼らない。**本物の osascript へ解決したら実行前に落とす**
    # (解決しているのに走らせると、ユーザーの実プロファイルへ `make new settings set` が飛ぶ)。
    local resolved
    resolved="$(PATH="$WORK/bin:$PATH" command -v osascript || true)"
    if [ "$resolved" != "$WORK/bin/osascript" ]; then
      ng "stub へ解決していない ($1): $resolved — 実 Terminal を触る恐れがあるので実行しない"
      return 2
    fi
    OSA_ARGS="$WORK/args" FAKE_READBACK="$3" PATH="$WORK/bin:$PATH" \
      "$SCRIPT" "$2" > "$WORK/out" 2> "$WORK/err"
  }
  # osascript stub へ渡った n 番目の引数 (AppleScript ソースは 2 番目)
  arg_n() { awk -v n="$1" 'BEGIN{RS="<<<ARG>>>\n"} NR==n{printf "%s", $0}' "$WORK/args"; }

  # 「在庫あり」の経路は OS 同梱の Menlo-Regular に差し替えた fixture で作る。
  # 🚨 repo のプロファイル (HackNFP) を使わないこと。在庫判定はホストの実フォントを見るので、
  # フォント未導入の CI runner では在庫ゲートで落ち、4a / 4c が偽の赤になる (2026-09-23 に実際に赤)。
  PRESENT="$WORK/present-font.terminal"
  present_font_line="$(swift "$ROOT_DIR/scripts/lib/terminal_profile_appearance.swift" "$PRESENT" | grep '^Font ' || true)"
  if [ "$present_font_line" != "Font Menlo-Regular Menlo-Regular 13 1" ]; then
    ng "前提: present-font が「在庫あり」にならない ($present_font_line) — 4a / 4c が在庫経路を通らない"
  fi

  # --- 4a. 正常系: 色 4 本とフォントが組み立てられ、警告は出ない ---------------------------
  rm -f "$WORK/args"
  if run_restore "正常" "$PRESENT" "Menlo-Regular 13"; then
    src="$(arg_n 2)"
    colors="$(grep -c 'color of settings set profileName to {' <<< "$src" || true)"
    # 🚨 本数だけ見ると、**背景色と文字色を取り違える / RGB を入れ替える**退行が素通りする
    # (敵対レビュー 2026-09-23 が変異で実証)。デコーダの出力とプロパティ名の対応を 1 行ずつ見る。
    mapping_bad=''
    while read -r key r g b; do
      case "$key" in
        BackgroundColor) prop='background color' ;;
        TextColor)       prop='normal text color' ;;
        TextBoldColor)   prop='bold text color' ;;
        CursorColor)     prop='cursor color' ;;
        *) continue ;;
      esac
      grep -qF "set $prop of settings set profileName to {$r, $g, $b}" <<< "$src" \
        || mapping_bad="$mapping_bad $key"
    done <<< "$(swift "$ROOT_DIR/scripts/lib/terminal_profile_appearance.swift" "$PRESENT")"
    if [ "$colors" != "4" ]; then
      ng "正常系: 色の設定が 4 本組み立てられていない ($colors 本)"
    elif [ -n "$mapping_bad" ]; then
      ng "正常系: 色のキーとプロパティ/数値の対応が違う:$mapping_bad"
      grep 'color of settings set' <<< "$src"
    elif ! grep -q 'set font name of settings set profileName to fontName' <<< "$src"; then
      ng "正常系: フォント名の設定行が組み立てられていない"
    elif ! grep -q 'set font size of settings set profileName to 13' <<< "$src"; then
      ng "正常系: フォントサイズの設定行が組み立てられていない"
    elif [ "$(arg_n 4)" != "Menlo-Regular" ]; then
      ng "正常系: osascript へ渡ったフォント名が違う: $(arg_n 4)"
    elif grep -q 'WARN' "$WORK/err"; then
      ng "正常系なのに WARN が出た"; cat "$WORK/err"
    else
      ok "正常系: 色 4 本 (キー↔プロパティ↔数値の対応込み) + フォント名 (argv) + サイズ、警告なし"
    fi
  else
    ng "正常系で restore が失敗した (rc=$?)"; cat "$WORK/err"
  fi

  # --- 4b. 段 (a): 未導入フォントは「設定しない」---------------------------------------------
  # 🚨 これが今回の変更の主目的。Terminal は未導入フォントをエラー無しで代替へ差し替え、
  # **その代替名を保存する**ので、設定してしまうと repo の指定が代替名に化ける。
  rm -f "$WORK/args"
  if run_restore "未導入" "$WORK/missing-font.terminal" "SFMonoTerminal-Regular 13"; then
    src="$(arg_n 2)"
    if grep -q 'set font name' <<< "$src"; then
      ng "未導入フォントなのに font name の設定行を組み立てた (代替名で上書きされる)"
    elif [ -n "$(arg_n 4)" ]; then
      ng "未導入フォントなのに osascript へ名前を渡した: $(arg_n 4)"
    elif ! grep -q '見つからないので設定しない' "$WORK/err"; then
      ng "未導入フォントの WARN が出ていない"; cat "$WORK/err"
    elif [ "$(grep -c 'color of settings set profileName to {' <<< "$src" || true)" != "4" ]; then
      ng "未導入フォントで色の設定まで落ちた (色は独立しているはず)"
    else
      ok "段 (a): 未導入フォントは設定せず WARN のみ (色は設定する)"
    fi
  else
    ng "未導入フォントで restore が失敗した (色だけでも設定されるべき)"; cat "$WORK/err"
  fi

  # --- 4c. 段 (b): 読み戻しが違えば WARN --------------------------------------------------
  # 在庫判定を通ったフォントでも Terminal が差し替えることはありうる。設定後の読み戻しが
  # 唯一の「実際にそうなった」証拠。
  rm -f "$WORK/args"
  if run_restore "読み戻し不一致" "$PRESENT" "SFMonoTerminal-Regular 13"; then
    if grep -q 'にならなかった' "$WORK/err"; then
      ok "段 (b): 読み戻しが違えば WARN を出す"
    else
      ng "読み戻しが違うのに WARN が出ない (差し替えを見逃す)"; cat "$WORK/err"
    fi
  else
    ng "読み戻し不一致のケースで restore が失敗した"; cat "$WORK/err"
  fi

  # --- 4d. 注入: 細工した名前は osascript に到達しない --------------------------------------
  # デコーダが PostScript 名の語彙で弾くので、そもそも osascript が呼ばれない。
  rm -f "$WORK/args"
  set +e
  run_restore "注入" "$WORK/inject-font.terminal" "x 13"
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    ng "細工した .terminal で restore が成功してしまった"; cat "$WORK/out" "$WORK/err"
  elif [ -f "$WORK/args" ]; then
    ng "細工した .terminal で osascript まで到達した"; cat "$WORK/args"
  else
    ok "注入: 細工した名前は osascript に到達せず落ちる (rc=$rc)"
  fi
fi

if [ "$fails" -gt 0 ]; then
  printf '\nFAIL: terminal_profile_restore のテストが %d 件失敗\n' "$fails"
  exit 1
fi
printf '\nAll terminal-profile-restore tests passed successfully!\n'
