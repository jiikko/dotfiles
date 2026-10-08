# shellcheck shell=bash
# codex_events.sh — codex-events (src/codexevents。codex の JSONL の run の判定) のバイナリを解決する。
#
# codex_events_resolve <repo root>: CODEX_EVENTS にバイナリの絶対パスを入れて export する (起動時に 1 回だけ。理由は bin/lib/go_tool.sh)。
# 呼び出しは "$CODEX_EVENTS" inspect … / "$CODEX_EVENTS" is-object <file>。
codex_events_resolve() {  # $1=repo root
  # shellcheck source=bin/lib/go_tool.sh
  . "$1/bin/lib/go_tool.sh" || { echo "✗ codex_events_resolve: $1/bin/lib/go_tool.sh を読めない" >&2; return 1; }
  go_tool_resolve CODEX_EVENTS "$1" codexevents codex-events help
}
