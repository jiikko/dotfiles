#!/usr/bin/env bash
# human issue (`NNN-human-*.md`) が、機械から読める `期限:` を必ず持つことを検査する。
#
# なぜ: 期限は**人を待たせないための唯一の仕掛け**で、SessionStart hook
# (`_claude/hooks/human-tasks-due.sh`) と `issue-sync` skill の Step 0 が
# **行頭 `期限[:：]`** を grep して期限切れを報告する。書式が外れたファイルは
# 「期限が無い issue」と区別が付かず、**黙って報告から消える**。
#
# 🚨 実際に踏んだ (2026-09-15、issue 375): `- 期限: 2026-10-15` と箇条書きで書いたため、
# human issue 6 件のうち 5 件しか期限が報告されていなかった。沈黙なので、
# 人が「そういえばあの issue はどうなった」と思い出すまで誰も気づけない。
#
# 書式の正本は `issues/README.md` の「`期限:` — 人が読む期限を本文に書く」節:
#   **行頭 `期限:` + 半角コロン + `YYYY-MM-DD`**。
# ここは hook より**厳しい**向きにずれている (意図的):
#   - hook は全角コロン (`期限：`) も拾うが、README は半角に揃えろと言うのでここでは不正にする
#     (表記が割れると、次に検出側を書く人が両方を覚えていないと取りこぼす)
#   - 日付の形も見る (`2026-9-1` は hook の grep は通るが、人も機械も並べ替えられない)
#
# 変異検証のため、検査対象ディレクトリを第 1 引数で差し替えられる (既定 = repo の issues/)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

# 🚨 **本走査と canary は必ずこの関数を通す**。式をコピーして canary を別に書くと、
# canary は「コピーした側」しか検査しない (本走査の破損を検出しない)。
# 出力: 違反 1 件につき "<パス>\t<理由>" を 1 行。違反が無ければ何も出さない。
scan_violations() { # scan_violations <issues dir>
  local dir="$1" f line value
  # 🚨 prune は `-name done` で書く (`-path issues/done` は起点の綴りで一致しなくなり、
  # done 全件が open に化ける)
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    line=$(grep -m1 -E '^期限[:：]' "$f" 2>/dev/null || true)
    if [ -z "$line" ]; then
      printf '%s\t行頭の `期限:` が無い (箇条書き `- 期限:` や見出し内は hook から見えない)\n' "$f"
      continue
    fi
    case "$line" in
      期限：*) printf '%s\t全角コロン (README は半角に揃えろと言う): %s\n' "$f" "$line"; continue ;;
    esac
    value=${line#期限:}
    value=${value# }
    if ! grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}([^0-9-]|$)' <<< "$value"; then
      printf '%s\t日付が YYYY-MM-DD でない: %s\n' "$f" "$line"
      continue
    fi
  done <<< "$(find "$dir" -name 'done' -prune -o -type f -name '*-human-*.md' -print | sort)"
}

# --- canary: 既知の入力で既知の答えが出ることを、本走査の前に固定する ------------------------
#
# 抽出が壊れると「違反 0 件 = 緑」になる形 (`verify-execution-not-just-exit-code.md`) を塞ぐ。
canary_dir=$(mktemp -d /tmp/ttdl.XXXXXX)
trap 'rm -rf -- "$canary_dir"' EXIT INT TERM HUP
mkdir -p "$canary_dir/done"
printf '# ok\n\n起票日: 2026-01-01\n期限: 2026-12-31\n'      > "$canary_dir/901-human-ok.md"
printf '# bullet\n\n- 期限: 2026-12-31\n'                     > "$canary_dir/902-human-bullet.md"
printf '# zenkaku\n\n期限： 2026-12-31\n'                     > "$canary_dir/903-human-zenkaku.md"
printf '# baddate\n\n期限: 2026-9-1\n'                        > "$canary_dir/904-human-baddate.md"
printf '# not human\n\nなにもない\n'                          > "$canary_dir/905-bug-not-human.md"
printf '# done は見ない\n\n- 期限: 2026-12-31\n'              > "$canary_dir/done/906-human-done.md"

canary_out=$(scan_violations "$canary_dir")
canary_n=$(printf '%s' "$canary_out" | grep -c . || true)
if [ "$canary_n" != 3 ]; then
  printf '✗ canary が壊れている: 違反 3 件を期待したが %s 件だった\n%s\n' "$canary_n" "$canary_out" >&2
  exit 1
fi
# 🚨 **理由まで見る**。ファイル名だけを見ると、枝が 1 つ死んでも別の枝が同じファイルを
# 拾って件数が合い、緑のまま通る (実測 2026-09-16: 全角コロンの枝を外す変異は、
# 日付検査がそのファイルを拾うため件数では検出できなかった)。理由はそのまま
# 「何を直せばいいか」なので、枝ごとに固定する価値がある
while IFS='|' read -r want reason; do
  grep -q "$want" <<< "$canary_out" || {
    printf '✗ canary: %s を違反として拾えていない\n%s\n' "$want" "$canary_out" >&2; exit 1; }
  grep -q "$reason" <<< "$(grep "$want" <<< "$canary_out")" || {
    printf '✗ canary: %s の理由が「%s」でない (枝が死んでいる可能性)\n%s\n' \
      "$want" "$reason" "$canary_out" >&2; exit 1; }
done <<'CANARY'
902-human-bullet|行頭の
903-human-zenkaku|全角コロン
904-human-baddate|日付が
CANARY
for never in 901-human-ok 905-bug-not-human 906-human-done; do
  if grep -q "$never" <<< "$canary_out"; then
    printf '✗ canary: %s を誤って違反にした (偽陽性)\n%s\n' "$never" "$canary_out" >&2; exit 1
  fi
done

# --- 本走査 -----------------------------------------------------------------------------------
issues_dir="${1:-issues}"
while [ "${issues_dir%/}" != "$issues_dir" ]; do issues_dir="${issues_dir%/}"; done
if [ ! -d "$issues_dir" ]; then
  printf '✗ 検査対象ディレクトリが無い: %s\n' "$issues_dir" >&2
  exit 1
fi

checked=$(find "$issues_dir" -name 'done' -prune -o -type f -name '*-human-*.md' -print | grep -c . || true)
out=$(scan_violations "$issues_dir")
bad=$(printf '%s' "$out" | grep -c . || true)
if [ "$bad" != 0 ]; then
  printf '%s\n' "$out" | sed 's/^/✗ /' >&2
  printf '✗ human issue の期限: %s 件を検査、%s 件が機械から読めない (%s)\n' "$checked" "$bad" "$issues_dir" >&2
  printf '  書式は issues/README.md の「期限:」節 — 行頭 `期限:` + 半角コロン + YYYY-MM-DD\n' >&2
  exit 1
fi
# 🚨 0 件は失敗にしない: human issue が全部 done になった状態は正当。抽出が壊れていないことは
# 上の canary が保証する (「対象 0 件 = 緑」を canary の側で塞いでいる)
printf '✓ human issue の期限: %s 件すべてが行頭 `期限: YYYY-MM-DD` (canary 3 件で抽出を確認)\n' "$checked"
