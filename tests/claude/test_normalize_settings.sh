#!/usr/bin/env bash
# _claude/hooks/normalize-settings.sh の unit テスト。偽の CLAUDE_CONFIG_DIR を組み、
# 実 ~/.claude には触れない。
#
# なぜ: このスクリプトは「追跡ファイルを書き換える」道具なので、壊れ方が 2 方向ある。
#   (a) 素通り = 正規化されないのに黙って rc=0 (churn が残り続け、誰も気づかない)
#   (b) 過剰 = 値を失う / symlink を実ファイルに置き換える / 退避できないのに削除する /
#       SessionStart の stdout を汚してセッションのコンテキストに毎回ノイズを注入する
# 規範: ~/.claude/rules/adversarial-review-own-safeguards.md
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/normalize-settings.sh"
fails=0

WORK="$(mktemp -d "${TMPDIR:-/tmp}/normalize-settings.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

ng() { echo "NG: $*"; fails=$((fails + 1)); }

# fixture <ケース名>: ケースごとに新しい偽 CLAUDE_CONFIG_DIR を作る。
# ケース間で状態を共有しない (前のケースの残骸が次に効くと A-B が同じ結果になり誤診する)。
fixture() {
  CFG="$WORK/$1"
  mkdir -p "$CFG"
  SETTINGS="$CFG/settings.json"
  LOCAL="$CFG/settings.local.json"
}

# run: 偽環境で hook を実行し、stdout を OUT、stderr を ERR、rc を RC に入れる。
# 🚨 stdout と stderr を混ぜない (SessionStart は stdout だけをコンテキストへ注入するので、
#    「無言か」の判定は stdout 単独で見る必要がある)。
run() {
  set +e
  OUT=$(CLAUDE_CONFIG_DIR="$CFG" "$HOOK" 2>"$CFG/stderr")
  RC=$?
  set -e
  ERR=$(cat "$CFG/stderr")
}

# 順序がばらばらで揮発キーを含む入力。hooks 配列は「実行順」なので保たれねばならない。
scrambled() {
  cat <<'JSON'
{
  "statusLine": { "type": "command", "command": "s.sh" },
  "model": "opus[1m]",
  "language": "日本語",
  "hooks": {
    "SessionStart": [
      { "hooks": [
          { "type": "command", "command": "first.sh", "timeout": 10 },
          { "type": "command", "command": "second.sh", "timeout": 10 }
      ] }
    ]
  },
  "env": { "B": "2", "A": "1" },
  "modelSettings": { "claude-opus-5": { "effortLevel": "low" } },
  "alwaysThinkingEnabled": true
}
JSON
}

# --- 1. キーがソートされる ------------------------------------------------
fixture case1; scrambled > "$SETTINGS"; run
[ "$RC" -eq 0 ] || ng "1: rc=$RC (expected 0)"
got_keys=$(jq -r 'keys_unsorted | join(",")' "$SETTINGS")
[ "$got_keys" = "alwaysThinkingEnabled,env,hooks,language,statusLine" ] \
  || ng "1: top-level キーがソートされていない: $got_keys"
nested=$(jq -r '.env | keys_unsorted | join(",")' "$SETTINGS")
[ "$nested" = "A,B" ] || ng "1: ネストしたオブジェクトがソートされていない: $nested"

# --- 2. 配列の順序は保たれる (hooks の実行順は意味を持つ) -----------------
order=$(jq -r '.hooks.SessionStart[0].hooks | map(.command) | join(",")' "$SETTINGS")
[ "$order" = "first.sh,second.sh" ] || ng "2: 配列の順序が変わった: $order"

# --- 3. 揮発キーが local へ退避され、settings.json から消える -------------
[ "$(jq -r 'has("model")' "$SETTINGS")" = "false" ] || ng "3: model が settings.json に残っている"
[ "$(jq -r 'has("modelSettings")' "$SETTINGS")" = "false" ] || ng "3: modelSettings が残っている"
[ "$(jq -r '.model' "$LOCAL")" = "opus[1m]" ] || ng "3: model が local へ退避されていない"
[ "$(jq -r '.modelSettings["claude-opus-5"].effortLevel' "$LOCAL")" = "low" ] \
  || ng "3: modelSettings が local へ退避されていない"

# --- 4. 日本語が \uXXXX へエスケープされない ------------------------------
grep -q '"language": "日本語"' "$SETTINGS" || ng "4: UTF-8 がエスケープされた: $(grep language "$SETTINGS")"

# --- 5. 成功時 stdout は空 (SessionStart のコンテキストを汚さない) --------
[ -z "$OUT" ] || ng "5: stdout に出力がある (SessionStart へ注入されてしまう): $OUT"
grep -q 'moved' <<<"$ERR" || ng "5: 退避の通知が stderr に出ていない"

# --- 6. 冪等 (2 回目で内容が変わらない) -----------------------------------
before=$(cat "$SETTINGS"); run
[ "$before" = "$(cat "$SETTINGS")" ] || ng "6: 2 回目の実行で内容が変わった"
[ -z "$OUT" ] || ng "6: 2 回目の stdout が空でない: $OUT"

# --- 7. 変化が無ければ書かない (mtime を動かさない) -----------------------
touch -t 202001010000 "$SETTINGS"
before_mtime=$(stat -f %m "$SETTINGS"); run
[ "$(stat -f %m "$SETTINGS")" = "$before_mtime" ] || ng "7: 変化が無いのに書き戻して mtime が動いた"

# --- 8. symlink が保たれる (mv で実ファイルに置き換えない) ----------------
fixture case8
mkdir -p "$CFG/dotfiles"
scrambled > "$CFG/dotfiles/settings.json"
ln -s "$CFG/dotfiles/settings.json" "$SETTINGS"
run
[ -L "$SETTINGS" ] || ng "8: symlink が実ファイルに置き換わった"
[ "$(jq -r 'has("model")' "$CFG/dotfiles/settings.json")" = "false" ] \
  || ng "8: symlink 越しに実体が更新されていない"

# --- 9. local が壊れていたら揮発キーを消さない (値を失わない) -------------
fixture case9; scrambled > "$SETTINGS"; printf 'not json' > "$LOCAL"; run
[ "$RC" -eq 0 ] || ng "9: rc=$RC (skip なので 0)"
[ "$(jq -r '.model' "$SETTINGS")" = "opus[1m]" ] \
  || ng "9: 退避できないのに settings.json から model を消した (値の消失)"
grep -q 'settings.local.json is invalid' <<<"$ERR" || ng "9: 判定不能が stderr に出ていない"
[ "$(jq -r 'keys_unsorted | join(",")' "$SETTINGS" | cut -d, -f1)" = "alwaysThinkingEnabled" ] \
  || ng "9: local が壊れていてもソートは行うべき"

# --- 10. マージは settings.json 側を優先する ------------------------------
fixture case10; scrambled > "$SETTINGS"
printf '{"model":"old","voice":"keep"}\n' > "$LOCAL"; run
[ "$(jq -r '.model' "$LOCAL")" = "opus[1m]" ] || ng "10: 古い local の値が settings.json を上書きした"
[ "$(jq -r '.voice' "$LOCAL")" = "keep" ] || ng "10: local の既存キーが失われた"

# --- 11. settings.json が不正 JSON なら触らずに skip ----------------------
fixture case11; printf 'not json' > "$SETTINGS"; run
[ "$RC" -eq 0 ] || ng "11: rc=$RC (skip なので 0)"
[ "$(cat "$SETTINGS")" = "not json" ] || ng "11: 不正 JSON を書き換えた"
grep -q 'invalid JSON' <<<"$ERR" || ng "11: skip の理由が stderr に出ていない"

# --- 12. settings.json が無ければ何もしない -------------------------------
fixture case12; run
[ "$RC" -eq 0 ] || ng "12: rc=$RC"
[ -e "$SETTINGS" ] && ng "12: 存在しない settings.json を作った"

# --- 13. jq が無い環境では skip (壊さない) --------------------------------
# macOS は /usr/bin/jq を同梱するので「PATH を素に戻す」では jq を消せない。
# 必要な外部コマンドだけを symlink した PATH を組み、jq だけを不在にする。
fixture case13; scrambled > "$SETTINGS"; orig=$(cat "$SETTINGS")
mkdir -p "$CFG/bin"
for c in mktemp cmp cat; do ln -s "$(command -v "$c")" "$CFG/bin/$c"; done
command -v jq >/dev/null 2>&1 || ng "13: 前提が崩れている (実環境に jq が無い)"
PATH="$CFG/bin" command -v jq >/dev/null 2>&1 && ng "13: 偽 PATH から jq を消せていない (このケースは何も検査していない)"
set +e
OUT=$(CLAUDE_CONFIG_DIR="$CFG" PATH="$CFG/bin" "$HOOK" 2>"$CFG/stderr"); RC=$?
set -e
[ "$RC" -eq 0 ] || ng "13: rc=$RC (jq 不在は skip なので 0)"
[ "$(cat "$SETTINGS")" = "$orig" ] || ng "13: jq が無いのに settings.json を書き換えた"
grep -q 'jq not found' "$CFG/stderr" || ng "13: jq 不在が stderr に出ていない"

# --- 14. rc=2 を漏らさない (ConfigChange では exit 2 が設定変更の block になる) --
# 事前検証 (jq empty) は通し、退避キーの抽出で jq が rc=2 を返す状況を shim で作る。
fixture case14; scrambled > "$SETTINGS"
real_jq=$(command -v jq)
mkdir -p "$CFG/shim"
cat > "$CFG/shim/jq" <<SHIM
#!/bin/sh
case "\$*" in *with_entries*) exit 2 ;; esac
exec "$real_jq" "\$@"
SHIM
chmod +x "$CFG/shim/jq"
case "$real_jq" in "$CFG/shim"/*) ng "14: shim が自分自身を実体として解決した" ;; esac
set +e
OUT=$(CLAUDE_CONFIG_DIR="$CFG" PATH="$CFG/shim:$PATH" "$HOOK" 2>"$CFG/stderr"); RC=$?
set -e
[ "$RC" -eq 1 ] || ng "14: rc=$RC (jq の rc=2 は 1 に丸めるべき)"

if [ "$fails" -eq 0 ]; then
  echo "OK: normalize-settings.sh (14 ケース)"
else
  echo "✗ normalize-settings.sh: $fails 件の失敗"
  exit 1
fi
