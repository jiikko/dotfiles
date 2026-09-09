#!/usr/bin/env bash
# 「手書き列挙」と「桁数の決め打ち」で射程が静かに縮む形を固定する (issue 313)。
#
# なぜ: どれも**壊れたときに黙る**。対象から漏れたファイルは検査されないだけで何も言わないし、
# 1000 番の issue は「関わった issue なし」になるだけ。実測 2026-09-09 の起点:
#   ① yamllint の対象が手書き 13 本で、実在 25 本のうち **12 本が検査の外**にいた
#   ② issue 番号の抽出 3 箇所が 3 桁決め打ちで、1000 番以降を抽出集合から落とす
#   ③ `discover_shell_scripts.sh` の `LINT_DIRS` の外に shell script が 3 本あった
#      (ヘッダは「未登録は構造的に発生しない」と主張していた)
#
# 🚨 **脅威モデル**: 止めるのは「列挙へ戻す / 桁を決め打ちへ戻す / 新しいディレクトリを
# 追加して発見の外に置く」形だけ。意図的な迂回は review の責務。
# 🚨 **検出しないと決めた形**:
#   - yamllint 以外の検査 (json / ruby) の手書き列挙。件数が少なく、増えたときに気づく
#   - `zshlib/*.zsh` が `ZSH_SYNTAX_FILES` から漏れる形。shebang が無いので SC1071 は出ず、
#     bash として検査されて緑になる (既知の穴。`discover_shell_scripts.sh` のヘッダに明記)
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT_DIR" || exit 1
fails=0
bad() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }
passes=0
ok() { printf '✓ %s\n' "$1"; passes=$((passes + 1)); }

# --- ① yamllint の対象は導出で、実在するものを取りこぼさない ---------------------------------
derived=$(scripts/discover_yaml_files.sh) || derived=""
if [ -z "$derived" ] || grep -q '__DISCOVERY_FAILED__' <<< "$derived"; then
  bad "discover_yaml_files.sh が対象を出せない (抽出が壊れると「違反 0 件」で緑になる)"
else
  # canary: 本走査と**同じスクリプト**を通す。実在するはずの 1 本が入っていなければ抽出が壊れている
  if grep -qx 'theme/colors.yml' <<< "$derived"; then
    ok "yamllint の対象を導出できている ($(wc -l <<< "$derived" | tr -d ' ') 本)"
  else
    bad "導出結果に theme/colors.yml が無い (抽出が壊れている)"
  fi
  # 実在する yml/yaml が全部入っていること (= 手書き列挙へ戻すと落ちる)
  actual=$(find . \( -name '*.yml' -o -name '*.yaml' \) -type f \
    -not -path './.git/*' -not -path '*/vendor/*' -not -path '*/node_modules/*' -not -path './tmp/*' \
    | sed 's|^\./||' | sort)
  missing=$(comm -23 <(printf '%s\n' "$actual") <(printf '%s\n' "$derived" | sort))
  if [ -z "$missing" ]; then
    ok "実在する yml/yaml が 1 本も漏れていない"
  else
    bad "yamllint の対象から漏れている: $(tr '\n' ' ' <<< "$missing")"
  fi
fi

# Makefile が導出を使っていること (列挙へ戻す変更を止める)
# shellcheck disable=SC2016  # Makefile のリテラル `$(shell …)` を探すので展開させない
if grep -q 'YAML_FILES := \$(shell scripts/discover_yaml_files.sh)' Makefile; then
  ok "Makefile が YAML_FILES を導出している"
else
  bad "Makefile の YAML_FILES が手書き列挙に戻っている"
fi

# --- ② issue 番号の抽出が桁数に依存しない -----------------------------------------------------
#
# 🚨 **実際に 4 桁のファイルを置いて確かめる** (パターンを grep するだけだと、
# 別の場所に残った決め打ちを見落とす)。
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/issues/next"
: > "$tmp/issues/1001-four-digit.md"
: > "$tmp/issues/999-three-digit.md"
for pat_file in _claude/hooks/issue-progress-check.sh tests/issues/test_issue_numbers_unique.sh; do
  # そのファイルが使っている find のパターンを実際に走らせる
  while IFS= read -r pat; do
    hits=$(find "$tmp/issues" -type f -name "$pat" | wc -l | tr -d ' ')
    if [ "$hits" -eq 2 ]; then
      ok "$pat_file: パターン '$pat' が 3 桁と 4 桁の両方を拾う"
    else
      bad "$pat_file: パターン '$pat' が $hits 件しか拾わない (1000 番以降を落とす)"
    fi
  done < <(grep -oE "'\[0-9\]\[0-9\]\[0-9\][^']*-\*\.md'" "$pat_file" | tr -d "'" | sort -u)
done

# grep 側 (commit subject から番号を拾う) も 4 桁を拾うこと。
#
# 🚨 **パターンを hook の実体から抜いて使う**。テスト側に書き写すと、それは「コピーした
# 正規表現」を検査しているだけで本走査の破損を検出しない (最初そう書き、hook を `{3}` へ
# 戻す変異が緑のまま通った)。
subj='fix(1001): 何か / docs(999): 何か'
hook_pat=$(grep -oE "grep -oE '\\\\\(\(\[0-9\]\{[0-9,]+\}[^']*\)\\\\\)'" \
  _claude/hooks/issue-progress-check.sh | head -1 | sed -E "s/^grep -oE '//; s/'$//")
if [ -z "$hook_pat" ]; then
  bad "hook から subject 抽出のパターンを取り出せない (実装が変わった? テストが本走査を通っていない)"
else
  got=$(grep -oE "$hook_pat" <<< "$subj" | grep -oE '[0-9]{3,}' | sort -u | tr '\n' ' ')
  if [ "$got" = "1001 999 " ]; then
    ok "commit subject の番号抽出が 3 桁と 4 桁の両方を拾う (hook の実パターンで検査)"
  else
    bad "commit subject の番号抽出が桁数に依存している (pat: $hook_pat / got: $got)"
  fi
fi

# --- ③ LINT_DIRS の外に shell script が残っていない -------------------------------------------
#
# 🚨 ヘッダが「未登録は構造的に発生しない」と主張していたが偽だった。主張どおりに動く形へ寄せ、
# 新しいディレクトリを足したときに黙って外へ出ないよう機械で見る。
discovered=$(scripts/discover_shell_scripts.sh | sort)
allsh=$(find . -type f \( -name '*.sh' -o -name '*.zsh' \) \
  -not -path './.git/*' -not -path '*/vendor/*' -not -path './tmp/*' -not -path './tests/*' \
  | sed 's|^\./||' | sort)
outside=$(comm -23 <(printf '%s\n' "$allsh") <(printf '%s\n' "$discovered"))
if [ -z "$outside" ]; then
  ok "LINT_DIRS の外に shell script が無い ($(wc -l <<< "$discovered" | tr -d ' ') 本を発見)"
else
  bad "LINT_DIRS の外に shell script がある (発見されないので lint されない): $(tr '\n' ' ' <<< "$outside")"
fi

if [ "$fails" -gt 0 ]; then
  printf '\n✗ 列挙の導出: %d 件の違反\n' "$fails" >&2
  exit 1
fi
printf '✓ 列挙の導出: 検査 %d 件すべて通過\n' "$passes"
