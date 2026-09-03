#!/usr/bin/env bash
# check_skip_exit_code.sh — 「丸ごと skip したのに exit 0」を落とす。
#
# なぜ (issue 139 が正本。発生源は issue 133):
#   runner (Makefile の run_tests) は **exit 77 を [skip]、0 を [ok]** として数える。
#   skip を告げてから `exit 0` すると、**assert が 1 本も走っていないのに合格と同じ表示**になり、
#   検査が消えたことが緑に埋もれる。実害: 2026-08-29 に CI を macOS へ移した時点で
#   timeout(1) が無くなり、test_deny_bare_tmux_kill.sh の 60 件の assert が丸ごと消えたのに
#   [ok] と集計されていた (issue 139)。規約は tests/CLAUDE.md の「0 件・skip・沈黙の扱い」。
#
# 検出する形: skip を告げる出力の**直後 3 行以内**にある `exit 0`。
#   print -u2 "[foo] skipped: ..."
#   exit 0        # ← これ。正しくは exit 77
#
# 検査しないもの:
#   - **部分 skip** (ファイルの一部の assert だけを飛ばす形) は tests/CLAUDE.md が 0 で抜けてよいと
#     明記している。正当な例が実在するので、行内に `partial-skip: allow <理由>` を書いて通す
#     (pipefail-grep-q: allow / trigger-log-writer: allow と同じイディオム)
#   - このスクリプト自身と、説明文に同じ字面が出るドキュメント
#
# ⚠️ 0 件でも「検査対象が 1 つも見つからない」なら失敗にする (パスの書き方が壊れたら赤にする)。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

# skip を告げていると見なす語 (実際に使われている表記から採った)
SKIP_RE='skipped:|skipping|handle_result[^#]*"skip"'

scanned=0
violations=0

while IFS= read -r f; do
  case "$f" in
    scripts/check_skip_exit_code.sh) continue ;;
  esac
  scanned=$((scanned + 1))
  # skip を告げる行を見つけ、そこから 3 行以内の `exit 0` を探す
  while IFS=: read -r lineno _; do
    [ -n "$lineno" ] || continue
    # 窓は skip 行の**前後**を見る。allow マーカーは理由を書く都合で skip 行の手前に
    # 置かれることがある (実測: test_reap_orphan_servers.sh)
    from=$((lineno > 3 ? lineno - 3 : 1))
    window=$(sed -n "${from},$((lineno + 3))p" "$f")
    # ⚠️ パイプで grep -q に渡さない。pipefail 下では一致していても非 0 になりうる
    # (issue 096 / scripts/check_pipefail_grep_q.sh が落とす形)。herestring で渡す
    grep -qE '^[[:space:]]*exit 0([[:space:]]|$)' <<< "$window" || continue
    grep -q 'partial-skip: allow' <<< "$window" && continue
    printf '✗ %s:%s 丸ごと skip なのに exit 0 (runner が [ok] と数える。exit 77 にする)\n' "$f" "$lineno"
    violations=$((violations + 1))
  done < <(grep -nE "$SKIP_RE" "$f" 2>/dev/null || true)
done < <(find tests -name '*.sh' -type f | sort)

if [ "$scanned" -eq 0 ]; then
  echo "✗ 検査対象のテストが 1 件も見つからない (find のパスが壊れている)" >&2
  exit 1
fi

if [ "$violations" -gt 0 ]; then
  echo "✗ 丸ごと skip なのに exit 0: $violations 件 (tests/CLAUDE.md / issue 139)" >&2
  echo "  部分 skip なら行内に 'partial-skip: allow <理由>' を書く" >&2
  exit 1
fi

echo "OK: skip の終了コードを検査した ($scanned ファイル)"
