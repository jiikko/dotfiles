#!/bin/sh
# discover_shell_scripts.sh — lint 対象の shell script を機械的に発見して 1 行ずつ出力する。
# Makefile が SHELLCHECK_FILES の導出 (発見集合 − ZSH_SYNTAX_FILES の補集合) に使う。
# 発見規約: LINT_DIRS 配下の *.sh / *.zsh、または拡張子なしで shell shebang を持つファイル。
#
# 旧 tests/test_lint_coverage.sh の discover() を移設。
#
# 🚨 **ヘッダが実装より強いことを主張していた** (issue 313 ③、2026-09-09 に訂正)。
# 以前ここには「『未登録』は構造的に発生せず、zsh 例外の登録漏れは SC1071 で loud に落ちる」と
# 書いてあったが、**どちらも実ファイルでは成立していなかった**:
#   - `LINT_DIRS` の**外**にある shell script は発見されない = 未登録が普通に起きる。実測で
#     `mac/` に 2 本、`src/glogx/tools/` に 1 本あった (3 本とも lint 0 件だったので LINT_DIRS へ
#     足した。再発は tests/scripts/test_lint_dirs_cover_all_scripts.sh が検出する)
#   - **SC1071 が出るのは zsh の shebang を持つファイルだけ**。zshlib の `*.zsh` は source される
#     ライブラリで shebang が無く (多くは `# shellcheck shell=bash` を付けている)、
#     `ZSH_SYNTAX_FILES` から漏れても SC1071 にはならず、bash として検査されて緑になる
#     (= `zsh -n` が漏れたことは loud にならない)。これは既知の穴として残す
# 例外リストの削除残りが `zsh -n` の does-not-exist で落ちるのは実装どおり。
# 注意: この file 内のコメント行を `# shellcheck` で始めない (directive と誤認され SC1072 になる)。
set -eu
unset CDPATH
cd "$(dirname "$0")/.." || exit 1

# lint 対象を持つディレクトリ。vendor/ は対象外。tests/ もここでは対象外だが、
# 無 lint ではない: 約半数が zsh スクリプトで ZSH_SYNTAX_FILES 方式だと 40 本超の
# 手動例外リストになるため、shebang 機械分類で lint する scripts/lint_test_scripts.sh
# (make test-lint-tests) が別途カバーする。
# _claude は hooks だけでなく直下も含める (statusline-command.sh が漏れていた。shellcheck の
# dialect 判定に載らないため #!/bin/sh のまま bash 専用置換を書いても静かに通り、Linux (dash)
# 実行で初めて Bad substitution になった。2026-07-25)
# mac / src/glogx/tools は issue 313 ③ で追加 (3 本とも shellcheck 0 件だったので、
# 発見漏れを直すコストがゼロだった)。
LINT_DIRS="setup.sh bin scripts zshlib _claude mac src/glogx/tools"

# find の失敗 (ディレクトリ不在・権限エラー) をパイプに隠さない: 失敗時は番兵を stdout に出して
# 非 0 で終わる。Make の $(shell) は exit code を捨てるが、番兵が実在しないファイル名として
# SHELLCHECK_FILES に混ざり shellcheck が does-not-exist で loud に落ちる (静かな部分 lint を防ぐ)。
# shellcheck disable=SC2086 # LINT_DIRS は意図的に単語分割する (find の複数起点)
all_files=$(find $LINT_DIRS -type f) || { printf '__DISCOVERY_FAILED__\n'; exit 1; }

printf '%s\n' "$all_files" | while IFS= read -r f; do
  case "$f" in
    *.sh|*.zsh) printf '%s\n' "$f" ;;
    *) head -1 "$f" 2>/dev/null | grep -qE '^#!.*[/ ](sh|bash|zsh|dash|ksh)( |$)' && printf '%s\n' "$f" ;;
  esac
done | sort -u
