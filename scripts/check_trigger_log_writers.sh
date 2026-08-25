#!/usr/bin/env bash
# check_trigger_log_writers.sh — 共有観測ログへ「直接 append する」書き手が増えるのを落とす。
#
# なぜ (issue 079):
#   ~/.cache/tt-restore-trigger.log は watchdog の死因分類 (verdict) の入力で、行の書式と
#   「誰が書けるか」が判定の前提になっている。書き手が散らばると 2 つ壊れる:
#     1. 行書式の正本が無くなる (タイムスタンプの書式・タブ区切りが書き手ごとに drift する)
#     2. **default socket ゲートを通らない書き手**が生まれる。-L の隔離テストサーバも同じ
#        conf を source して同じ hook を持つので、テストのイベントが本番の verdict を汚す
#        (scripts/CLAUDE.md「サーバ状態に触るスクリプトの不変条件」)
#   実際に _tmux.conf の 2 つの復元 hook がこの形で残っており、唯一ゲート無しで書いていた。
#
# 直し方: guards.sh を source して `tt_trigger_log "<本文>" [打刻]` を呼ぶ。
#   ゲート (`tt_on_default_server || exit 0`) も同じファイルにある。
#
# 意図的な例外は行内に `trigger-log-writer: allow` と理由を書く。逃げ道が無いと
# 「検査に食われるから書き方を変える」運用になる (check_pipefail_grep_q.sh と同じ方針)。
#
# 本ファイルの説明文には検査対象の字面が入るため、自分自身は対象から外す。
# メッセージにも `$TT_TRIGGER_LOG` や例外マーカーが字として入る (検査の説明そのもの)。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

for c in grep git; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done

SELF="scripts/check_trigger_log_writers.sh"
ALLOWED_SINGLE_WRITER="scripts/lib/tmux_resurrect_guards.sh"

# 追記リダイレクト (`>> …TT_TRIGGER_LOG` / `>> …tt-restore-trigger.log`) だけを見る。
# 散文・表・変数定義 (`TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-…}"`) は対象外。
PATTERN='>>[[:space:]]*"?(\$\{?TT_TRIGGER_LOG|[^"]*tt-restore-trigger\.log)'

# 散文 (issue / docs / README) は対象外。字面としての引用まで落とすと、検査の説明が書けなくなる
files="$(git ls-files -z | tr '\0' '\n' | grep -v '^vendor/' | grep -v '\.md$' || true)"
[ -n "$files" ] || { printf '✗ 検査対象のファイルを 1 つも列挙できなかった (git ls-files 失敗?)\n'; exit 1; }

violations=0
checked=0
while IFS= read -r f; do
  [ -f "$f" ] || continue
  case "$f" in "$SELF"|"$ALLOWED_SINGLE_WRITER") continue ;; esac
  checked=$(( checked + 1 ))
  hits="$(grep -nE "$PATTERN" "$f" 2>/dev/null | grep -v 'trigger-log-writer: allow' || true)"
  [ -n "$hits" ] || continue
  while IFS= read -r h; do
    printf '✗ %s:%s\n' "$f" "$h"
    violations=$(( violations + 1 ))
  done <<EOF
$hits
EOF
done <<EOF
$files
EOF

# 「そもそも検査対象が 0 件」を緑にしない (パターンや列挙が壊れたら赤にする)
[ "$checked" -gt 0 ] || { printf '✗ 1 ファイルも検査していない (列挙が壊れている)\n'; exit 1; }

if [ "$violations" -gt 0 ]; then
  printf '\n共有観測ログへ直接 append している箇所が %d 件ある。\n' "$violations"
  printf 'guards.sh を source して tt_trigger_log を使うこと (ゲートも同じファイルにある)。\n'
  printf '意図的な例外なら行内に `trigger-log-writer: allow` と理由を書く。\n'
  exit 1
fi
printf '✓ 観測ログの直接書き込みなし (%d ファイル検査。書き手は %s のみ)\n' "$checked" "$ALLOWED_SINGLE_WRITER"
