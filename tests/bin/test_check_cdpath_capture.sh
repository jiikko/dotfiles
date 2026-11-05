#!/usr/bin/env bash
# bin/check-cdpath-capture の「repo から対象を探す」部分の契約を固定する (判定そのものは tests/scripts/test_check_cdpath_before_cd_capture.sh)。
#
# 守るもの: git が知っているファイル (追跡 + ignore されていない未追跡) の shell script を全部見る (拡張子・shebang) /
# ignore されたもの・消したもの・symlink・shell でないものは見ない / --exclude で外せて、当たらない --exclude は落とす /
# 対象が --min 未満なら緑にしない / repo を引数でも cwd でも受ける / 継承した GIT_DIR に引きずられない。
# fixture は検査に渡す shell の文字列なので `$(cd` を展開させない。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUT="$ROOT_DIR/bin/check-cdpath-capture"
fails=0
ok()  { printf '  ✓ %s\n' "$1"; }
bad() { printf '  ✗ %s\n' "$1"; fails=$((fails + 1)); }

WORK="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/test-check-cdpath-capture.XXXXXX")" && pwd -P)" || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$WORK"' EXIT

BAD=$'ROOT="$(cd sub && pwd)"'
REPO="$WORK/repo"
git init -q "$REPO"
# 手元の global な ignore (vendor/ を外す人がいる) で fixture の追跡が変わらないようにする
git -C "$REPO" config core.excludesFile /dev/null
mkdir -p "$REPO/bin" "$REPO/vendor/x" "$REPO/build"
printf '#!/bin/bash\n%s\n' "$BAD" > "$REPO/bin/tool"                 # 拡張子なし + bash の shebang
printf '#!/usr/bin/env zsh\n%s\n' "$BAD" > "$REPO/bin/ztool"          # zsh の shebang
printf '%s\n' "$BAD" > "$REPO/bin/lib.bash"                           # shebang の無い lib (.bash)
printf '%s\n' "$BAD" > "$REPO/bin/t.bats"
printf '%s\n' "$BAD" > "$REPO/vendor/x/plugin.sh"                     # 他人のコード (--exclude で外す)
printf '#!/usr/bin/env python3\n%s\n' "$BAD" > "$REPO/bin/py"         # shell でない
printf '%s\n' "$BAD" > "$REPO/README.md"                              # shell でない
printf '%s\n' "$BAD" > "$REPO/build/gen.sh"                           # ignore される
printf 'build/\n' > "$REPO/.gitignore"
printf '%s\n' "$BAD" > "$REPO/bin/gone.sh"                            # 追跡してから work tree で消す
ln -s tool "$REPO/bin/tool-link"                                      # symlink
git -C "$REPO" add -A
git -C "$REPO" -c user.name=t -c user.email=t@example.com commit -q -m init
rm "$REPO/bin/gone.sh"
printf '%s\n' "$BAD" > "$REPO/bin/new.sh"                             # 未追跡 (commit 前の新しい script も見る)

RC=0; OUT="$("$SUT" "$REPO" 2>&1)" || RC=$?
printf 'Test 1: git が知っている shell script を全部見る\n'
for n in bin/tool: bin/ztool: bin/lib.bash: bin/t.bats: vendor/x/plugin.sh: bin/new.sh:; do
  if [ "$RC" -eq 1 ] && grep -qF "$n" <<< "$OUT"; then ok "${n%:} を診断する"; else bad "${n%:} を診断しない (rc=$RC): $OUT"; fi
done
printf 'Test 2: 対象でないものは見ない\n'
for n in bin/py: README.md: build/gen.sh: bin/gone.sh bin/tool-link:; do
  if ! grep -qF "$n" <<< "$OUT"; then ok "${n%:} は見ない"; else bad "${n%:} を見た: $OUT"; fi
done

printf 'Test 3: --exclude で外せる / 当たらない --exclude は落とす\n'
RC=0; OUT="$("$SUT" --exclude 'vendor/*' "$REPO" 2>&1)" || RC=$?
if [ "$RC" -eq 1 ] && ! grep -qF vendor/ <<< "$OUT" && grep -qF bin/tool: <<< "$OUT"; then ok "vendor/* を外し、残りは診断する"; else bad "--exclude が効かない (rc=$RC): $OUT"; fi
RC=0; OUT="$("$SUT" --exclude 'vendr/*' "$REPO" 2>&1)" || RC=$?
if [ "$RC" -eq 1 ] && grep -qF 'どのファイルにも当たらない' <<< "$OUT"; then ok "当たらない --exclude は rc=1"; else bad "当たらない --exclude を通した (rc=$RC): $OUT"; fi

printf 'Test 4: 直した repo は通す・cwd の repo を見る\n'
for f in bin/tool bin/ztool bin/new.sh; do printf '#!/bin/bash\nunset CDPATH\n%s\n' "$BAD" > "$REPO/$f"; done
printf '%s\n' 'ROOT="$(CDPATH= cd sub && pwd)"' > "$REPO/bin/lib.bash"
printf '%s\n' 'ROOT="$(CDPATH= cd sub && pwd)"' > "$REPO/bin/t.bats"
RC=0; OUT="$(cd "$REPO/bin" && "$SUT" --exclude 'vendor/*' 2>&1)" || RC=$?
if [ "$RC" -eq 0 ] && grep -qF '5 ファイルに該当なし' <<< "$OUT"; then ok "サブディレクトリから打っても repo 全体の 5 本を見て通す"; else bad "直した repo を通さない (rc=$RC): $OUT"; fi

printf 'Test 5: 対象が --min 未満なら緑にしない\n'
EMPTY="$WORK/empty"; git init -q "$EMPTY"; printf 'x\n' > "$EMPTY/README.md"
RC=0; OUT="$("$SUT" "$EMPTY" 2>&1)" || RC=$?
if [ "$RC" -eq 1 ] && grep -qF '下限 1' <<< "$OUT"; then ok "0 件は rc=1"; else bad "0 件を緑にした (rc=$RC): $OUT"; fi
RC=0; OUT="$("$SUT" --min 6 --exclude 'vendor/*' "$REPO" 2>&1)" || RC=$?
if [ "$RC" -eq 1 ] && grep -qF '下限 6' <<< "$OUT"; then ok "下限を上げれば 5 件で rc=1"; else bad "--min が効かない (rc=$RC): $OUT"; fi
RC=0; OUT="$("$SUT" --min x "$REPO" 2>&1)" || RC=$?
if [ "$RC" -eq 2 ]; then ok "整数でない --min は rc=2"; else bad "整数でない --min を受けた (rc=$RC): $OUT"; fi

printf 'Test 6: 継承した GIT_DIR が別の repo を指しても、cwd の repo を見る\n'
RC=0; OUT="$(cd "$EMPTY" && GIT_DIR="$REPO/.git" GIT_WORK_TREE="$REPO" "$SUT" 2>&1)" || RC=$?
if [ "$RC" -eq 1 ] && grep -qF '下限 1' <<< "$OUT"; then ok "GIT_DIR を外して cwd (空の repo) を見る"; else bad "継承した GIT_DIR の repo を見た (rc=$RC): $OUT"; fi

printf 'Test 7: 名前が代入 (a=b.sh) や - のファイルがあっても、そのファイルと残りを全部見る\n'
ODD="$WORK/odd"; git init -q "$ODD"; git -C "$ODD" config core.excludesFile /dev/null
for n in 'a=b.sh' z.sh zz.sh -; do printf '#!/bin/bash\n%s\n' "$BAD" > "$ODD/$n"; done
RC=0; OUT="$("$SUT" "$ODD" 2>&1)" || RC=$?
for n in 'a=b.sh:' z.sh: zz.sh: '-:'; do
  if [ "$RC" -eq 1 ] && grep -qF -- "$n" <<< "$OUT"; then ok "${n%:} を診断する"; else bad "${n%:} を診断しない (rc=$RC): $OUT"; fi
done
printf 'Test 8: 版つきの shell・bats・ash の shebang も拾う\n'
SB="$WORK/sb"; git init -q "$SB"
printf '#!/bin/bash5\n%s\n' "$BAD" > "$SB/v1"; printf '#!/usr/local/bin/bash-5.2\n%s\n' "$BAD" > "$SB/v2"
printf '#!/usr/bin/env bats\n%s\n' "$BAD" > "$SB/v3"; printf '#!/usr/bin/env ash\n%s\n' "$BAD" > "$SB/v4"
RC=0; OUT="$("$SUT" "$SB" 2>&1)" || RC=$?
for n in v1: v2: v3: v4:; do
  if [ "$RC" -eq 1 ] && grep -qF "$n" <<< "$OUT"; then ok "${n%:} を診断する"; else bad "${n%:} を診断しない (rc=$RC): $OUT"; fi
done

[ "$fails" -eq 0 ] || { printf '✗ %d 件失敗\n' "$fails"; exit 1; }
printf '=== check-cdpath-capture: すべて成功 ===\n'
