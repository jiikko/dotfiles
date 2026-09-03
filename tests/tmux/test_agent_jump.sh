#!/usr/bin/env bash
# scripts/tmux_agent_jump.sh の unit テスト (PATH stub 方式。実 tmux / fzf には触れない)。
# 固定する不変条件:
#   - 並び順: 🔔 input → ⚙ working → ✓ idle → 🔕 seen (注意が必要な順に fzf へ渡る)
#   - popup 専用セッション (scratch 等、TT_POPUP_SESSION_RE) は候補から除外する
#     (switch-client で full attach 事故になるため)
#   - 選択後は pane_id で switch-client + select-window + select-pane まで行う
#   - エージェント 0 件なら fzf を呼ばず exit 0 / fzf キャンセルも exit 0 (popup -E 前提)
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_agent_jump.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"
FZF_INPUT="$TMP_DIR/fzf_input.txt"
export CALLS FZF_INPUT

mkdir -p "$TMP_DIR/bin"
# stub tmux: list-panes は STUB_ROWS (実 format 展開後の行) を返す
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  list-panes) printf '%b' "${STUB_ROWS:-}" ;;
esac
exit 0
EOS
# stub fzf: 渡された候補と引数を記録し、STUB_FZF_PICK 行目を選択として返す (0 = キャンセル)
cat > "$TMP_DIR/bin/fzf" <<'EOS'
#!/bin/sh
echo "fzf env-default-opts=[${FZF_DEFAULT_OPTS-UNSET}] $*" >> "$CALLS"
cat > "$FZF_INPUT"
[ "${STUB_FZF_PICK:-1}" = 0 ] && exit 130
sed -n "${STUB_FZF_PICK:-1}p" "$FZF_INPUT"
EOS
chmod +x "$TMP_DIR/bin/tmux" "$TMP_DIR/bin/fzf"
export PATH="$TMP_DIR/bin:$PATH"

fail=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fail=1; }

T=$(printf '\t')
# 実データ形: state \t pane_id \t session:index.pane \t since(epoch。空も可) \t pane_title
now=$(date +%s)
ROWS="✓ idle${T}%1${T}dev:2.1${T}$((now - 90))${T}claude A\n"
ROWS+="🔔 input${T}%2${T}web:1.1${T}$((now - 30))${T}claude B\n"
ROWS+="🔕 seen${T}%3${T}scratch:5.1${T}${T}claude C\n"     # popup 専用セッション → 除外対象
ROWS+="⚙ working${T}%4${T}api:3.2${T}${T}claude D\n"       # since 未設定 (旧 hook の状態) も壊れない

# --- 0 件: fzf を呼ばず exit 0 ---------------------------------------------------
: > "$CALLS"; : > "$FZF_INPUT"
STUB_ROWS='' "$SCRIPT" >/dev/null 2>&1 || ng "0 件で非 0"
[ -s "$FZF_INPUT" ] && ng "0 件なのに fzf が呼ばれた" || ok "0 件: fzf を呼ばず exit 0"

# --- 並び順 + 除外 ---------------------------------------------------------------
: > "$CALLS"; : > "$FZF_INPUT"
STUB_ROWS="$ROWS" STUB_FZF_PICK=1 "$SCRIPT" >/dev/null 2>&1 || ng "選択実行が失敗"
grep -q 'scratch' "$FZF_INPUT" && ng "popup 専用セッション scratch が候補に居る" \
  || ok "popup 専用セッションを除外"
order=$(grep -o '%[0-9]' "$FZF_INPUT" | tr -d '%\n')
if [ "$order" = "241" ]; then
  ok "並び順: input(%2) → working(%4) → idle(%1)"
else
  ng "並び順が不正: got=$order want=241"
fi

# --- 選択 → pane 単位でジャンプ ---------------------------------------------------
# 1 行目 = 最優先の %2 を選んだことになっている
grep -q 'switch-client -t %2' "$CALLS" || ng "switch-client が pane_id で呼ばれない"
grep -q 'select-window -t %2' "$CALLS" || ng "select-window が呼ばれない"
grep -q 'select-pane -t %2'   "$CALLS" || ng "select-pane が呼ばれない"
ok "選択 → switch-client + select-window + select-pane (上の ✗ が無ければ)"

# --- fzf の操作モード: 絞り込み無効 + j/k 移動 + プレビュー末尾スクロール ------------
# (インクリメンタルサーチ不要・j/k カーソル移動のユーザー要望 2026-08-08。
#  末尾表示は follow を使う。+999999 の clamp は「最終行が先頭」で以降が空白になり
#  何も表示されないように見える実測バグがあった)
# 🚨 この節は「意図した flag/bind を fzf に渡したか」の grep までしか守れない (stub 方式の
# 構造的限界)。bind 名の実在・follow の実挙動は fzf 0.54.0 + tmux 3.7b の実表示で確認済み
# (2026-08-08 敵対レビュー)。fzf を major 更新したら bind 名と follow を実機で再確認すること
grep -q 'fzf .*--disabled' "$CALLS" || ng "fzf: --disabled (絞り込み無効) が無い"
grep -q 'j:down,k:up' "$CALLS" || ng "fzf: j/k のカーソル移動 bind が無い"
grep -q 'preview-half-page' "$CALLS" || ng "fzf: J/K のプレビュースクロール bind が無い"
grep -Eq 'preview-window=down,[0-9]+,follow' "$CALLS" || ng "fzf: 絶対行数 + follow のプレビュー指定が無い"
grep -q -- '--header=.*スクロール' "$CALLS" || ng "fzf: 操作ヒントのヘッダーが無い"
grep -q '+999999' "$CALLS" && ng "fzf: +999999 に退行 (最終行が先頭に来て空白に見える)"
# zshrc の FZF_DEFAULT_OPTS ('--height 80% --border') が継承されると上下 2 割の余白と
# 二重枠になる (実測 2026-08-08)。スクリプトが必ず空で上書きしていること
grep -q 'env-default-opts=\[\]' "$CALLS" || ng "fzf: FZF_DEFAULT_OPTS を空にしていない (--height 80% が継承される)"
ok "fzf: --disabled + j/k + プレビュースクロール (上の ✗ が無ければ)"

# --- fzf キャンセル: 何も切り替えない --------------------------------------------
: > "$CALLS"; : > "$FZF_INPUT"
STUB_ROWS="$ROWS" STUB_FZF_PICK=0 "$SCRIPT" >/dev/null 2>&1 || ng "キャンセルで非 0"
grep -q 'switch-client' "$CALLS" && ng "キャンセルなのに switch している" \
  || ok "キャンセル: 切り替えなし"

if [ "$fail" -eq 0 ]; then
  echo "test_agent_jump: all ok"
else
  echo "test_agent_jump: FAILED"
  exit 1
fi
