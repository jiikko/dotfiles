#!/usr/bin/env bash
#
# SessionStart フック: issue 運用の共通規約を、issues/ を持つ repo のセッションにだけ注入する。
#
#   issue-rules-inject.sh                       # _claude/issue-rules.md (規約の本体)
#   issue-rules-inject.sh issue-rules.d/<x>.md  # issue 運用でしか発動しない rule (issue 414)
#
# なぜ: 規約を ~/.claude/CLAUDE.md や rules/ に置くと issues/ を持たない repo (仕事の repo) でも
# 毎セッション全文読まれる。逆に各 repo の issues/README.md へコピーすると更新が伝播せず乖離する
# (2026-09-19 に複数 repo のコピーが正本より古いまま放置されていた。issue 401)。
# 「issues/ があるか」の判定は他の issue 系 hook と同じ issue_hook_resolve_dir に寄せる
# (判定を別に書くと、どの repo で効くかが hook ごとにずれる)。
#
# 🚨 1 回の呼び出しで 1 ファイルだけを注入する。Claude Code は hook 1 本の additionalContext を
# 約 10k 字で打ち切り、末尾を黙って落とす (実測 2026-09-27 / Claude Code 2.1 系: 9,525 字は全部届き、
# 11,925 字では末尾が届かなかった。上限は hook の呼び出しごとで、9.5k 字の hook 2 本は両方届いた)。
# だから issue-rules.d/ の各ファイルは _claude/settings.json に 1 本ずつ並べて呼ぶ
# (対応は tests/claude/test_issue_rules_inject.sh が検査する)。上限が変わったら MAX を測り直す。
#
# 🚨 issues/ がある repo で規約ファイルを読めないとき・上限を超えるときは黙らない。規約が届かないことが
# このフックの壊れ方で、沈黙すると「規約なしで issue を書く」セッションが気づかれずに続く。

set -u

here="$(dirname "$0")"
lib="$here/lib/issue-hooks.sh"
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_resolve_dir >/dev/null 2>&1; then
  printf '%s を読めないため issue 運用規約を注入できなかった (hook の配線を確認する)\n' "$lib"
  exit 0
fi
issue_hook_resolve_dir || exit 0

rules="${ISSUE_RULES_FILE:-$here/../${1:-issue-rules.md}}"
if ! body=$(cat "$rules" 2>/dev/null) || [ -z "$body" ]; then
  issue_hook_emit "🚨 issue 運用規約 ($rules) を読めなかった。この repo には issues/ があるので、issue を書く前にユーザーへ伝えること:" "$rules"
  exit 0
fi

head="この repo には issues/ がある。以下の issue 運用規約に従うこと (repo 固有の事項は各 issues/README.md が補う):"
max="${ISSUE_RULES_INJECT_MAX:-9500}"
# wc -m は locale で数え方が変わる (C では byte を数え、日本語は 3 倍になる)。字数で比べるため UTF-8 に固定する
n=$(printf '%s\n%s' "$head" "$body" | LC_ALL=en_US.UTF-8 wc -m | tr -d ' ')
if [ "$n" -gt "$max" ]; then
  # 警告は先頭に置く (打ち切られるのは末尾なので、先頭は必ず届く)
  head="🚨 この規約は ${n} 字で、hook の注入の上限 (約 10k 字) を超えるため末尾が落ちる。issue を触る前に $rules を Read し、ユーザーへも伝えること。$head"
fi
issue_hook_emit "$head" "$body"
