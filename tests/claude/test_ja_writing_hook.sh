#!/usr/bin/env bash
# _claude/hooks/ja-writing-hook.sh (ja-writing hook を settings の 5 つのイベントから起こす包み) の unit テスト。
#
# なぜ: 配線が外れても、包みがビルドの無いときに黙っても、どちらも無音 (何も知らせずに原稿の照合が働かなくなる)。
# 逆に包みが rc≠0 や stderr を出すと、ホストの作業を止める。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/ja-writing-hook.sh"
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い" >&2; exit 1; }
ok=0; fail=0
pass() { echo "✓ $1"; ok=$((ok+1)); }
ng() { echo "✗ $1"; fail=$((fail+1)); }

# 配線: 本家の --print-config (claude-code) と同じイベント・matcher・上限で、settings.json から呼ばれている
want=$(cd "$ROOT_DIR/src/ja-writing" && go run . hook --print-config claude-code |
  jq -c '[.hooks | to_entries[] | {event: .key, matcher: .value[0].matcher, timeout: .value[0].hooks[0].timeout}] | sort_by(.event)')
got=$(jq -c '[.hooks | to_entries[] | .key as $e | .value[] | select(any(.hooks[]; .command == "~/dotfiles/_claude/hooks/ja-writing-hook.sh"))
  | {event: $e, matcher: .matcher, timeout: .hooks[0].timeout}] | sort_by(.event)' "$ROOT_DIR/_claude/settings.json")
if [ -n "$want" ] && [ "$want" = "$got" ]; then pass "settings.json の配線が --print-config と同じ"; else ng "配線: settings $got / --print-config $want"; fi

TMP_ROOT=$(cd "$(mktemp -d)" && pwd -P) || exit 1
trap 'rm -rf "$TMP_ROOT"' EXIT
# 包みはビルドしないので、先に 1 回ビルドしておく (本番も同じ: bin/ja-writing を一度打てば入る)
"$ROOT_DIR/bin/ja-writing" hook --version >/dev/null

# 登録の流れ: 原稿を名指した UserPromptSubmit で登録の文が出る (stdin が hook に届き、出力が返る)
proj="$TMP_ROOT/proj"
mkdir -p "$proj"
printf '今日は良い天気です。\n' > "$proj/src.md"
input=$(jq -n --arg c "$proj" '{session_id: "s1", hook_event_name: "UserPromptSubmit", cwd: $c, prompt: "/yomiyasu src.md"}')
rc=0
printf '%s' "$input" | "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err" || rc=$?
if [ "$rc" -eq 0 ] && [ ! -s "$TMP_ROOT/err" ] && grep -qF 'yomiyasu の管理: 原稿 src.md を登録しました' "$TMP_ROOT/out" && [ -d "$proj/.yomiyasu/armed" ]; then
  pass "原稿を名指すと登録して知らせる"
else
  ng "登録: rc=$rc out=$(cat "$TMP_ROOT/out") err=$(cat "$TMP_ROOT/err")"
fi

# ビルドが無いときは何もせず rc 0 (偽の repo。src/ja-writing は go.mod の無い空のフォルダ)
fake="$TMP_ROOT/fake"
mkdir -p "$fake/_claude/hooks" "$fake/bin/lib" "$fake/src/ja-writing"
cp "$HOOK" "$fake/_claude/hooks/"
ln -s "$ROOT_DIR/bin/lib/go_autobuild.zsh" "$fake/bin/lib/go_autobuild.zsh"
printf '今日は良い天気です。\n' > "$proj/src2.md"
input=$(jq -n --arg c "$proj" '{session_id: "s1", hook_event_name: "UserPromptSubmit", cwd: $c, prompt: "/yomiyasu src2.md"}')
rc=0
printf '%s' "$input" | "$fake/_claude/hooks/ja-writing-hook.sh" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err" || rc=$?
if [ "$rc" -eq 0 ] && [ ! -s "$TMP_ROOT/out" ] && [ ! -s "$TMP_ROOT/err" ]; then
  pass "ビルドが無いときは何もせず rc 0"
else
  ng "ビルドなし: rc=$rc out=$(cat "$TMP_ROOT/out") err=$(cat "$TMP_ROOT/err")"
fi

echo "ja-writing-hook: ok=$ok fail=$fail"
[ "$fail" -eq 0 ] || exit 1
