#!/bin/sh
# settings.json を「共有設定だけ」の状態に正規化する。
#
# 背景: /model や /effort などマシン固有・頻繁に変わる設定を Claude Code は
# 追跡対象の settings.json に直接書き込む。放置すると git pull のたびに
# コンフリクトする。そこで揮発キーを非追跡の settings.local.json へ退避し、
# settings.json は hooks/statusLine 等の共有設定だけに保つ。
#
# 実行タイミングは `make pull`（pull 直前）。settings.local.json の値は
# settings.json より優先されるため、退避後も /model の選択は効き続ける。
#
# settings.json は dotfiles への symlink なので、書き戻しは symlink を壊さない
# よう `cat > ` で行う（mv だと symlink が実ファイルに置き換わる）。
set -eu

CLAUDE_DIR="${HOME}/.claude"
SETTINGS="${CLAUDE_DIR}/settings.json"
LOCAL="${CLAUDE_DIR}/settings.local.json"

# 退避対象の揮発キー。共有したくない・CLI が勝手に書き換えるものだけ。
VOLATILE='["model","effortLevel","advisorModel","voice"]'

[ -f "$SETTINGS" ] || exit 0
command -v jq >/dev/null 2>&1 || { echo "normalize-settings: jq not found; skip" >&2; exit 0; }
jq empty "$SETTINGS" 2>/dev/null || { echo "normalize-settings: settings.json is invalid JSON; skip" >&2; exit 0; }

# settings.json に含まれる揮発キーだけを取り出す
extracted=$(jq -c --argjson keys "$VOLATILE" \
  'with_entries(select(.key as $k | $keys | index($k)))' "$SETTINGS")
[ "$extracted" = "{}" ] && exit 0  # 揮発キーが無ければ何もしない

# settings.local.json が無ければ空で作る / 壊れていたら中断
[ -f "$LOCAL" ] || printf '{}\n' > "$LOCAL"
jq empty "$LOCAL" 2>/dev/null || { echo "normalize-settings: settings.local.json is invalid JSON; skip" >&2; exit 0; }

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

# 1. local へ退避（settings.json 側が最新なので extracted を優先: local * extracted）
printf '%s' "$extracted" > "$tmp_dir/extracted.json"
jq -s '.[0] * .[1]' "$LOCAL" "$tmp_dir/extracted.json" > "$tmp_dir/local.json"
jq empty "$tmp_dir/local.json" 2>/dev/null || { echo "normalize-settings: merge produced invalid JSON; abort" >&2; exit 1; }
mv "$tmp_dir/local.json" "$LOCAL"

# 2. settings.json から揮発キーを削除（local へ退避済みなので値は失われない）
jq --argjson keys "$VOLATILE" 'delpaths([$keys[] | [.]])' "$SETTINGS" > "$tmp_dir/settings.json"
jq empty "$tmp_dir/settings.json" 2>/dev/null || { echo "normalize-settings: strip produced invalid JSON; abort" >&2; exit 1; }
cat "$tmp_dir/settings.json" > "$SETTINGS"  # symlink を壊さないため cat（mv 不可）

echo "normalize-settings: moved $(printf '%s' "$extracted" | jq -r 'keys | join(", ")') -> settings.local.json"
