#!/usr/bin/env bash
# 次に採番する issue の番号を出す (999 までは 3 桁のゼロ埋め、1000 からは桁が増える)。
#
#   scripts/issue_next_number.sh [<数える ref>]     (既定の ref は origin の既定のブランチ。無ければ origin/master)
#
# 作業ツリーの issues/ と <ref> の issues/ の両方を数える (push 済みの他のセッションの採番は作業ツリーに見えない)。
# fetch はしない: 打つ前に `git fetch origin` しておく。番号を取ったら即 commit & push する (issue 規約)。
# 他の repo でも、その repo の中から ~/dotfiles/scripts/issue_next_number.sh で呼べる。
# repo 直下の issues/ だけを数える。repo が bin/next-issue-number を持つなら、それを案内して何も出さずに止まる (rc=2)。
# 入れ子の issues/ と通しで採番する repo (obaket) で直下だけを数えると、入れ子の側の番号を取り直すため。
set -euo pipefail

# lib の場所は cd の前に解決する (cd の後だと、サブディレクトリから相対パスで起動したときに $0 が解決できない)
# shellcheck source=scripts/lib/issue_numbers.sh
. "$(dirname "$(readlink -f "$0")")/lib/issue_numbers.sh"
# shellcheck source=scripts/lib/repo_issue_tools.sh
. "$(dirname "$(readlink -f "$0")")/lib/repo_issue_tools.sh"
# 🚨 hook などから継承した GIT_DIR / GIT_WORK_TREE は cwd より優先され、別の repo の番号を数えて、その repo の道具の判定もすり抜ける
# shellcheck source=bin/lib/git_location_env.sh
. "$(dirname "$(readlink -f "$0")")/../bin/lib/git_location_env.sh"
git_location_env_unset || { echo "issue_next_number: 継承した git の場所の環境変数 (GIT_DIR など) を外せない" >&2; exit 2; }
root="$(git rev-parse --show-toplevel)"
if own="$(repo_issue_tool_owned "$root" next-issue-number)"; then
  echo "issue_next_number: この repo は自分の採番の道具を持つ。$own を使う" >&2; exit 2
fi
cd "$root" || exit 1
ref="${1:-$(issue_default_ref)}"
git rev-parse --verify -q "$ref^{commit}" >/dev/null || { echo "issue_next_number: $ref が無い (git fetch してから回す)" >&2; exit 1; }
[ -d issues ] || { echo "issue_next_number: $root に issues/ が無い" >&2; exit 1; }

max="$(issue_max_number "$ref")"
printf '%03d\n' "$((max + 1))"
