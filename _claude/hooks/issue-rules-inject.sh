#!/usr/bin/env bash
#
# SessionStart フック: issue 運用の共通規約 (_claude/issue-rules.md) を、issues/ を持つ repo の
# セッションにだけ注入する。
#
# なぜ: 規約を ~/.claude/CLAUDE.md や rules/ に置くと issues/ を持たない repo (仕事の repo) でも
# 毎セッション全文読まれる。逆に各 repo の issues/README.md へコピーすると更新が伝播せず乖離する
# (2026-09-19 に my-products の 6 アプリのコピーが正本より古いまま放置されていた。issue 401)。
# 「issues/ があるか」の判定は他の issue 系 hook と同じ issue_hook_resolve_dir に寄せる
# (判定を別に書くと、どの repo で効くかが hook ごとにずれる)。
#
# 🚨 issues/ がある repo で規約ファイルを読めないときは黙らない。規約が届かないことが
# このフックの唯一の壊れ方で、沈黙すると「規約なしで issue を書く」セッションが気づかれずに続く。

set -u

here="$(dirname "$0")"
lib="$here/lib/issue-hooks.sh"
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_resolve_dir >/dev/null 2>&1; then
  printf '%s を読めないため issue 運用規約を注入できなかった (hook の配線を確認する)\n' "$lib"
  exit 0
fi
issue_hook_resolve_dir || exit 0

rules="${ISSUE_RULES_FILE:-$here/../issue-rules.md}"
if ! body=$(cat "$rules" 2>/dev/null) || [ -z "$body" ]; then
  issue_hook_emit "🚨 issue 運用規約 ($rules) を読めなかった。この repo には issues/ があるので、issue を書く前にユーザーへ伝えること:" "$rules"
  exit 0
fi
issue_hook_emit "この repo には issues/ がある。以下の issue 運用規約に従うこと (repo 固有の事項は各 issues/README.md が補う):" "$body"
