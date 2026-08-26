#!/usr/bin/env bash
#
# codex-drive の既定モデル / effort が、4 つの写しの間で食い違っていないことを検査する。
#
# なぜ: この値は「1 箇所」に寄せられない。SKILL.md の codex 起動例は driver
# (bin/codex-fanout) を経由しない直の `codex exec` (fallback 経路) で、実行時に driver の
# 既定を読む術がない。env 参照に書き換えると、変数が未設定のとき ~/.codex/config.toml の
# 既定を拾う (SKILL.md「モデルはスキル側で明示する」が警告している事故) ため、リテラルの
# まま持つのが正しい。したがって「単一の出典」ではなく「写し同士の一致」を検査で強制する。
#
# 実例 (2026-08-25): commit 8736d37 が effort を low から max へ変えた際、SKILL.md と
# bin/codex-fanout は更新されたが tests/codex_fanout.bats が取り残されて赤のままだった。
#
# 検査する写し:
#   1. bin/codex-fanout の実既定 (CODEX_FANOUT_MERGER_MODEL / _EFFORT の :- 既定) ← 基準
#   2. bin/codex-fanout 冒頭 usage コメントが述べる既定値
#   3. _claude/skills/codex-drive/SKILL.md の全 codex 起動例 (-m / model_reasoning_effort)
#
# 値そのものの pin (「max であること」) は tests/codex_fanout.bats の
# 「merger の既定モデル/effort と env 上書きが効く」が持つ。こちらは相対的な一致だけを見る
# ので、両方が揃って初めて「どこか 1 箇所を変えたら赤」になる。
#
# codex-drive 以外の skill (codex-lead / codex-review) は意図的に別の effort を使うため対象外。
#
# 例外 (sol-mid): codex-drive は「裁定・ゲート役」の 3 フェーズ ([S] の第 3 の codex /
# [D3] の敵対観点 / [7] の敵対照合) だけ上位モデルを許可している (ユーザー決定 2026-08-26)。
# その値をこの検査にハードコードすると、許可を変えたときに 2 箇所を直すことになるので、
# SKILL.md の宣言行から抽出して「既定 または 宣言された例外」だけを許す形にする。
# 宣言行が消えたら例外も消える (= 無断で sol を使い始めると落ちる)。

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
DRIVER="$ROOT_DIR/bin/codex-fanout"
SKILL_MD="$ROOT_DIR/_claude/skills/codex-drive/SKILL.md"

fail=0

for f in "$DRIVER" "$SKILL_MD"; do
  [ -f "$f" ] || { echo "FAIL: 検査対象が存在しない: $f" >&2; exit 1; }
done

# 写し 1: driver の実既定 (基準値)
def_model=$(sed -n 's/.*CODEX_FANOUT_MERGER_MODEL:-\([^}]*\)}.*/\1/p' "$DRIVER" | head -1)
def_effort=$(sed -n 's/.*CODEX_FANOUT_MERGER_EFFORT:-\([^}]*\)}.*/\1/p' "$DRIVER" | head -1)

# 抽出できないまま緑を返さない (書式が変わったら検査自体が無効になるため)
[ -n "$def_model" ] || { echo "FAIL: $DRIVER から merger の既定モデルを抽出できない (:- 既定の書式が変わった?)" >&2; exit 1; }
[ -n "$def_effort" ] || { echo "FAIL: $DRIVER から merger の既定 effort を抽出できない (:- 既定の書式が変わった?)" >&2; exit 1; }

# 写し 2: usage コメントが述べる既定値
doc_model=$(sed -n 's/.*CODEX_FANOUT_MERGER_MODEL .*既定 \([A-Za-z0-9.-]*\).*/\1/p' "$DRIVER" | head -1)
doc_effort=$(sed -n 's/.*CODEX_FANOUT_MERGER_EFFORT .*既定 \([a-z]*\).*/\1/p' "$DRIVER" | head -1)

[ -n "$doc_model" ] || { echo "FAIL: $DRIVER の usage コメントから既定モデルを抽出できない" >&2; exit 1; }
[ -n "$doc_effort" ] || { echo "FAIL: $DRIVER の usage コメントから既定 effort を抽出できない" >&2; exit 1; }

if [ "$doc_model" != "$def_model" ]; then
  echo "FAIL: bin/codex-fanout の usage コメントの既定モデル ($doc_model) が実既定 ($def_model) と食い違う" >&2
  fail=1
fi
if [ "$doc_effort" != "$def_effort" ]; then
  echo "FAIL: bin/codex-fanout の usage コメントの既定 effort ($doc_effort) が実既定 ($def_effort) と食い違う" >&2
  fail=1
fi

# 例外の宣言を SKILL.md から抽出する (無ければ例外なし = 既定のみ許可)
exc_model=$(grep -o '例外は下表の「裁定・ゲート役」3 フェーズだけで、そこは `[^`]*`' "$SKILL_MD" \
  | sed 's/.*`\(.*\)`/\1/' | head -1)
exc_effort=$(grep -o '3 フェーズだけで、そこは `[^`]*` + effort `[a-z]*`' "$SKILL_MD" \
  | sed 's/.*effort `\([a-z]*\)`.*/\1/' | head -1)

if [ -n "$exc_model" ] || [ -n "$exc_effort" ]; then
  # 片方だけ拾えたら宣言行の書式が壊れている (無言で例外を広げない)
  [ -n "$exc_model" ] && [ -n "$exc_effort" ] \
    || { echo "FAIL: SKILL.md の sol-mid 例外の宣言行からモデルと effort の両方を抽出できない" >&2; exit 1; }
  echo "note: 例外 (sol-mid) を許可: model=$exc_model / effort=$exc_effort"
fi

# 写し 3: SKILL.md の全起動例
skill_efforts=$(grep -o 'model_reasoning_effort="[a-z]*"' "$SKILL_MD" | sed 's/.*="\(.*\)"/\1/' | sort -u)
skill_models=$(grep -o -- '-m gpt-[A-Za-z0-9.-]*' "$SKILL_MD" | sed 's/^-m //' | sort -u)

[ -n "$skill_efforts" ] || { echo "FAIL: $SKILL_MD から effort 指定を 1 件も抽出できない (起動例の書式が変わった?)" >&2; exit 1; }
[ -n "$skill_models" ] || { echo "FAIL: $SKILL_MD からモデル指定を 1 件も抽出できない (起動例の書式が変わった?)" >&2; exit 1; }

while IFS= read -r e; do
  if [ "$e" != "$def_effort" ] && { [ -z "$exc_effort" ] || [ "$e" != "$exc_effort" ]; }; then
    echo "FAIL: SKILL.md の起動例に effort=$e があるが、許可されるのは $def_effort${exc_effort:+ か $exc_effort} だけ" >&2
    fail=1
  fi
done <<< "$skill_efforts"

while IFS= read -r m; do
  if [ "$m" != "$def_model" ] && { [ -z "$exc_model" ] || [ "$m" != "$exc_model" ]; }; then
    echo "FAIL: SKILL.md の起動例にモデル $m があるが、許可されるのは $def_model${exc_model:+ か $exc_model} だけ" >&2
    fail=1
  fi
done <<< "$skill_models"

if [ "$fail" -ne 0 ]; then
  echo "" >&2
  echo "codex-drive の既定を変えるときは 4 箇所すべてを同時に更新すること:" >&2
  echo "  1. bin/codex-fanout の :- 既定 / 2. 同ファイル冒頭の usage コメント" >&2
  echo "  3. _claude/skills/codex-drive/SKILL.md の起動例と effort 表" >&2
  echo "  4. tests/codex_fanout.bats の絶対 pin" >&2
  exit 1
fi

echo "ok: codex-drive の既定 (model=$def_model / effort=$def_effort) が driver・usage・SKILL.md で一致"
