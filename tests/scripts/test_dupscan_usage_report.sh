#!/usr/bin/env bash
# scripts/dupscan_usage_report.py (Claude Code の transcript から dupscan の利用を数える) の canary。
# 既知の transcript で既知の数が出ること、除外 (-i・パイプの後ろ・dupscan 自身の開発セッション) が効くことを固定する。
# 数え方を変えると、後日の測り直しが以前の数字と比べられなくなるため。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPORT="$ROOT_DIR/scripts/dupscan_usage_report.py"
command -v python3 >/dev/null 2>&1 || { echo "skipped: python3 が無い"; exit 77; }
command -v jq >/dev/null 2>&1 || { echo "skipped: jq が無い"; exit 77; }

FAIL_COUNT=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT
PROJECTS="$TMP_ROOT/projects"; mkdir -p "$PROJECTS/p1" "$PROJECTS/dev"

TMP_ROOT="$TMP_ROOT" python3 -I - <<'PY'
import json, datetime, os
root = os.environ["TMP_ROOT"] + "/projects"
base = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=2)
n = [0]
def ts():
    n[0] += 1
    return (base + datetime.timedelta(seconds=n[0])).strftime("%Y-%m-%dT%H:%M:%S.000Z")
def use(i, name, **inp):
    return {"type": "assistant", "timestamp": ts(), "message": {"content": [{"type": "tool_use", "id": i, "name": name, "input": inp}]}}
def nudge(i):
    return {"type": "attachment", "timestamp": ts(), "attachment": {"type": "hook_additional_context", "hookEvent": "PreToolUse", "toolUseID": i, "content": ["dupscan で Go の候補・呼び出し元を引ける"]}}
s1 = [
    use("t1", "Bash", command='grep -rn "func ReadCookies" --include=*.go .'),  # 識別子らしい grep (Go)。案内が出る
    nudge("t1"),
    use("t2", "Bash", command="dupscan index -name ReadCookies"),                # 直後の探索
    use("t3", "Read", file_path="/x/cookie.go", offset=291, limit=59),           # index の直後の Read (範囲指定)
    use("t4", "Bash", command="rg -n -i foobar ."),                              # -i は数えない
    use("t5", "Bash", command="make test | grep FooBarFailure"),                 # パイプの後ろは数えない
    use("t6", "Bash", command='grep -rn "refresh" config.yml'),                  # 識別子らしくない
    use("t7", "Grep", pattern="StorageLocation", glob="*.swift"),                # Grep ツール (Swift)・案内なし
    use("t8", "Bash", command="dupscan gate"),                                   # gate は探索に数えない
    use("t9", "Bash", command='grep -rn "foo.*Bar" Sources'),                    # 正規表現は識別子らしくない
]
dev = [use("d%d" % k, "Edit", file_path="/r/src/dupscan/x.go") for k in range(3)] + [use("d9", "Bash", command='grep -rn "func Dev" --include=*.go .')]
for name, lines in (("p1/s1.jsonl", s1), ("dev/s2.jsonl", dev)):
    with open(root + "/" + name, "w") as f:
        for o in lines:
            f.write(json.dumps(o, ensure_ascii=False) + "\n")
PY

OUT=$(python3 "$REPORT" --projects-dir "$PROJECTS" --days 3 --json)

check() { # $1=説明 $2=jq の式 (true を期待)
  if printf '%s' "$OUT" | jq -e "$2" >/dev/null 2>&1; then echo "✓ $1"; else echo "✗ $1"; FAIL_COUNT=$((FAIL_COUNT+1)); fi
}
check "セッション 1 本だけ数える (dupscan 自身の開発セッションは除く)" '.buckets.all.sessions == 1 and .skipped_dupscan_dev_sessions == 1'
check "識別子らしい grep は Go 1 回・Swift 1 回 (-i・パイプの後ろ・識別子でないものは数えない)" '.buckets.all.search_symbol == {"go":1,"swift":1} and .buckets.all.search_other == 2'
check "dupscan の探索は index 1 回だけ (gate は数えない)" '.buckets.all.explore_by_command == {"index":1}'
check "案内が出た grep は Go 1 回で、直後の dupscan の探索に続いた" '.buckets.all.nudged == {"go":1} and .buckets.all.nudged_followed == {"go":1}'
check "index の直後の Read は範囲指定つきとして数える" '.buckets.all.index_then_read == 1 and .buckets.all.index_read_with_range == 1'
check "dupscan 自身の開発セッションの grep は数えない" '(.buckets.all.search_symbol | add) == 2'

OUT=$(python3 "$REPORT" --projects-dir "$PROJECTS" --days 3 --include-dupscan-dev --json)
check "--include-dupscan-dev なら開発セッションも数える" '.buckets.all.sessions == 2 and .skipped_dupscan_dev_sessions == 0'

OUT=$(python3 "$REPORT" --projects-dir "$PROJECTS" --days 3 --by-day --json)
check "--by-day は日ごとに分ける (1 つの日にまとまる)" '(.buckets | length) >= 1 and ([.buckets[] | .sessions] | add) >= 1'

OUT=$(python3 "$REPORT" --projects-dir "$PROJECTS" --days 3 --split "$(date -u -v+1d '+%Y-%m-%dT%H:%M:%S+00:00')" --json)
check "--split より前の呼び出しは before に入り、after は空" '.buckets.before.sessions == 1 and .buckets.after.sessions == 0'

if (( FAIL_COUNT > 0 )); then exit 1; fi
