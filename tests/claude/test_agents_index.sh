#!/usr/bin/env bash
#
# _claude/agents/README.md の索引と _claude/agents/*.md の実体を突き合わせる。
#
# なぜ: 索引は手動メンテなので、agent の追加・削除と乖離する。**乖離した索引は無いより悪い**
# (載っていない agent は「存在しない」と読まれ、消えた agent は探させる)。
# 乖離を「読んだ人が気づく」から「テストが落ちる」へ格上げする
# (tests/claude/test_skill_trigger_table.sh と同じ発想。issue 001 の項目 21)。
#
# 検出する乖離 (両方向):
#   1. 索引に在るのに実体が無い (削除残り)
#   2. 実体が在るのに索引に無い (登録漏れ)
#
# 意図的に索引だけへ載せる名前は HARNESS_AGENTS に足すこと
# (Claude Code 標準の組み込みエージェントは repo にファイルを持たない)。

set -euo pipefail
unset CDPATH  # CDPATH が export されていると `cd foo` が解決先を stdout に出す

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
INDEX="$ROOT_DIR/_claude/agents/README.md"
AGENTS_DIR="$ROOT_DIR/_claude/agents"

# repo にファイルを持たない組み込みエージェント (索引には載せる)
HARNESS_AGENTS="statusline-setup"

fail=0

[ -f "$INDEX" ] || { echo "✗ 索引が無い: $INDEX"; exit 1; }

# 索引の表から agent 名を拾う (行頭の `| \`name\`` 形式)
listed=$(grep -oE '^\| `[a-z0-9-]+`' "$INDEX" | tr -d '|` ' | sort -u)
# 実体 (README 自身は除く)
actual=$(find "$AGENTS_DIR" -maxdepth 1 -name '*.md' ! -name 'README.md' -exec basename {} .md \; | sort)

# 🚨 抽出が空なら「乖離なし」ではなく「測れていない」。0 件で緑にしない
listed_n=$(printf '%s\n' "$listed" | grep -c . || true)
actual_n=$(printf '%s\n' "$actual" | grep -c . || true)
if [ "$listed_n" -lt 5 ] || [ "$actual_n" -lt 5 ]; then
  echo "✗ 抽出が期待どおり動いていない (索引 ${listed_n} 件 / 実体 ${actual_n} 件)。書式が変わった疑い"
  exit 1
fi

for a in $actual; do
  # 🚨 `printf … | grep -qx` のパイプに戻さないこと。grep -q は一致した瞬間に exit するので
  # 上流の printf が SIGPIPE で落ち、pipefail 下では**一致しているのに非 0** になる (issue 096)
  grep -qx "$a" <<<"$listed" || { echo "✗ 索引に無い agent: $a (_claude/agents/README.md に足すこと)"; fail=1; }
done

for a in $listed; do
  case " $HARNESS_AGENTS " in *" $a "*) continue ;; esac
  [ -f "$AGENTS_DIR/$a.md" ] || { echo "✗ 実体が無いのに索引に在る: $a (削除残り。README から外すか HARNESS_AGENTS へ)"; fail=1; }
done

# 件数の記述も揃える (見出しの「N 件」)
head_n=$(grep -oE '^# エージェント索引 \(([0-9]+) 件\)' "$INDEX" | grep -oE '[0-9]+' || true)
if [ -n "${head_n:-}" ] && [ "$head_n" != "$actual_n" ]; then
  echo "✗ 見出しの件数 ${head_n} が実体 ${actual_n} と違う"
  fail=1
fi

[ "$fail" = 0 ] || { echo "[test-agents-index] FAILED"; exit 1; }
echo "[test-agents-index] OK: agent 索引と実体が一致 (${actual_n} 件 + 組み込み $(printf '%s' "$HARNESS_AGENTS" | wc -w | tr -d ' ') 件)"
