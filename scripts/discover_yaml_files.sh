#!/bin/sh
# discover_yaml_files.sh — yamllint の対象を機械的に発見して 1 行ずつ出力する。
# Makefile が YAML_FILES の導出に使う (`make test-yaml`)。
#
# なぜ導出にするか (issue 313 ①): 手書き列挙だと**後から足した yml が黙って検査の外に出る**。
# 実測 2026-09-09: 実在 25 本に対して列挙は 13 本で、**12 本が yamllint の対象外**だった
# (`.github/workflows/doctor.yml` / `src_doctor.yml` / `src_termsafe.yml` / `_go-project.yml` /
# `.github/actions/ensure-toolchain/action.yml` / `src/*/.golangci.yml` 6 本 /
# `zshlib/tmux-window-name.yaml`)。列挙の向きを反転させ、**外すものだけを理由つきで書く**。
#
# 🚨 対象 0 件は失敗にする (verify-execution-not-just-exit-code.md)。抽出が壊れると
# 「違反 0 件」で緑になるのが、この手の検査で最も危ない壊れ方。
# 注意: この file 内のコメント行を `# shellcheck` で始めない (directive と誤認され SC1072 になる)。
set -eu
unset CDPATH
cd "$(dirname "$0")/.." || exit 1

# 除外するもの。**理由を書けないものは除外しない**。
#   .git       : メタデータ
#   vendor     : 取り込んだ第三者のコード (こちらの規約を当てる対象ではない)
#   node_modules / tmp : 生成物・作業領域 (tmp は ~/.gitignore_global で ignore)
found=$(find . \( -name '*.yml' -o -name '*.yaml' \) -type f \
  -not -path './.git/*' \
  -not -path '*/vendor/*' \
  -not -path '*/node_modules/*' \
  -not -path './tmp/*' \
  | sed 's|^\./||' | sort) || { printf '__DISCOVERY_FAILED__\n'; exit 1; }

if [ -z "$found" ]; then
  printf '__DISCOVERY_FAILED__\n'
  echo "discover_yaml_files.sh: yml/yaml が 1 件も見つからない (抽出が壊れている)" >&2
  exit 1
fi

printf '%s\n' "$found"
