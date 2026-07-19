#!/usr/bin/env bash
# theme/colors.yml (色カタログの単一ソース) と各消費者の一致検査。
# 固定する不変条件:
#   - scripts/lib/theme_colors.sh は yml からの生成結果と一致する (手編集・再生成漏れの検出)
#   - _tmux.conf / nvim palette.lua の該当定数は yml と同じ値を持つ
#     (設定言語が別で定数を共有できないため、一致は本テストが機械的に保証する)
# 色を変える手順: theme/colors.yml を編集 → scripts/gen_theme_colors.sh を実行 →
# 本テストが指す _tmux.conf / palette.lua の該当行を追従 → make test
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/scripts/lib/theme_colors.sh"
TMUX_CONF="$ROOT_DIR/_tmux.conf"
PALETTE="$ROOT_DIR/nvim/lua/dotfiles/palette.lua"

fail=0
ng() { printf '✗ %s\n' "$1"; fail=1; }
ok() { printf '✓ %s\n' "$1"; }

echo "## 生成物の drift 検査"
if diff -u "$LIB" <("$ROOT_DIR/scripts/gen_theme_colors.sh" --stdout); then
  ok "scripts/lib/theme_colors.sh は colors.yml と一致 (再生成漏れなし)"
else
  ng "scripts/lib/theme_colors.sh が古い。scripts/gen_theme_colors.sh を実行して commit すること"
fi

GO_GEN="$ROOT_DIR/src/git-popup/theme_gen.go"
if diff -u "$GO_GEN" <("$ROOT_DIR/scripts/gen_theme_colors.sh" --go-stdout); then
  ok "src/git-popup/theme_gen.go は colors.yml と一致 (再生成漏れなし)"
else
  ng "src/git-popup/theme_gen.go が古い。scripts/gen_theme_colors.sh を実行して commit すること"
fi

# shellcheck source=/dev/null
source "$LIB"

echo ""
echo "## _tmux.conf との一致"
assert_tmux() { # <説明> <期待する固定文字列>
  if grep -qF "$2" "$TMUX_CONF"; then ok "$1"; else ng "$1 — _tmux.conf に「$2」が見つからない"; fi
}
assert_tmux "現在地 @cur-accent = current_accent" "@cur-accent 'colour${THEME_CURRENT_ACCENT}'"
assert_tmux "zoom 警告 @zoom-accent = zoom_red" "@zoom-accent 'colour${THEME_ZOOM_RED}'"
assert_tmux "消灯 @fade-cold-fg = cold_gray" "@fade-cold-fg 'colour${THEME_COLD_GRAY}'"
assert_tmux "バー地 status-style = base_bar_bg" "status-style 'bg=colour${THEME_BASE_BAR_BG}"
# window-style は行を特定して検査する (bg=colourN の部分一致だと別行の同色で偽陽性になる)
if grep -qE "^set -g window-style +'.*bg=colour${THEME_BASE_PANE_BG}'" "$TMUX_CONF"; then
  ok "pane 地 window-style = base_pane_bg"
else
  ng "pane 地 window-style = base_pane_bg — _tmux.conf の window-style 行に bg=colour${THEME_BASE_PANE_BG} が見つからない"
fi
assert_tmux "カーソル = active_green" "cursor-colour colour${THEME_ACTIVE_GREEN}"

echo ""
echo "## nvim palette.lua との一致"
assert_lua() { # <説明> <キー> <hex> <cterm> (キーの後の整列スペースを許容)
  if grep -qE "$2 += \{ hex = \"$3\", cterm = $4 \}" "$PALETTE"; then
    ok "$1"
  else
    ng "$1 — palette.lua に $2 = { hex = \"$3\", cterm = $4 } が見つからない"
  fi
}
assert_lua "現在地 current_accent" current_accent "$THEME_CURRENT_ACCENT_HEX" "$THEME_CURRENT_ACCENT"
assert_lua "選択テキスト kraft" kraft "$THEME_KRAFT_HEX" "$THEME_KRAFT"
assert_lua "マーカー bright_orange" bright_orange "$THEME_MARKER_ORANGE_HEX" "$THEME_MARKER_ORANGE"
assert_lua "数量 bright_yellow" bright_yellow "$THEME_QUANTITY_YELLOW_HEX" "$THEME_QUANTITY_YELLOW"
assert_lua "pane 地 dark0_hard" dark0_hard "$THEME_BASE_PANE_BG_HEX" "$THEME_BASE_PANE_BG"
assert_lua "バー地 dark0" dark0 "$THEME_BASE_BAR_BG_HEX" "$THEME_BASE_BAR_BG"
assert_lua "危険 diag.error_bg" error_bg "$THEME_ERROR_RED_HEX" "$THEME_ERROR_RED"

echo ""
echo "## 消費者の配線"
# 生成された Go マップを git-popup TUI が実際に参照しているか (定義だけで未使用だと形骸化する)。
# 旧 shell popup 退役後の theme 消費者は git-popup。
if grep -rqE 'themeCterm\[' "$ROOT_DIR/src/git-popup/"; then
  ok "git-popup は生成された themeCterm を参照している"
else
  ng "git-popup が themeCterm を参照していない (theme 配線が切れている)"
fi

echo ""
if [[ "$fail" != 0 ]]; then
  echo "[test-theme-colors] FAILED (色を変えたら theme/colors.yml → 生成 → 各定数の順で揃える)"
  exit 1
fi
echo "[test-theme-colors] all assertions passed"
