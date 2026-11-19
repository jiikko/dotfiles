#!/bin/bash
# scripts/check_devtool_boundary.sh: devtool/ の外を参照する書き方を、形ごとに落とすこと。
# 落とさない形 (散文のコメント・devtool の中を指す symlink・理由つきの印) で誤って落とさないこと。
# 1 ケースごとに使い捨ての devtool のディレクトリを作り、検査を当てる。
set -u
unset CDPATH

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_devtool_boundary.sh"
FAIL_COUNT=0
fail() { echo "✗ $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
pass() { echo "✓ $1"; }

T="$(mktemp -d)" || { echo "✗ mktemp -d に失敗"; exit 1; }
trap 'rm -rf "$T"' EXIT
T="$(cd -P "$T" && pwd -P)" || exit 1
n=0

# new_case: 新しい devtool のディレクトリ ($D) を作る。bin/ok は落とさない中身
new_case() {
  n=$((n + 1)); D="$T/c$n/devtool"
  mkdir -p "$D/bin" || exit 1
  printf '#!/bin/bash\n# 散文の .. は落とさない (コメント)\necho ok\n' > "$D/bin/ok"
}
# expect_fail <名前> <stdout に出るはずの文字列>
expect_fail() {
  "$CHECK" "$D" > "$T/out" 2>&1; rc=$?
  if [ "$rc" -eq 1 ] && grep -qF -- "$2" "$T/out"; then pass "$1"; else fail "$1 (rc=$rc): $(cat "$T/out")"; fi
}
expect_ok() {
  "$CHECK" "$D" > "$T/out" 2>&1; rc=$?
  if [ "$rc" -eq 0 ] && grep -qF '✓ devtool の境界' "$T/out"; then pass "$1"; else fail "$1 (rc=$rc): $(cat "$T/out")"; fi
}

new_case
expect_ok "外を参照しない devtool は通す (散文のコメントの .. を落とさない)"

new_case; printf '#!/bin/bash\n. "$(dirname "$0")/../lib/x.sh"\n' > "$D/bin/rel"
expect_fail "コードの行の ../ を落とす (devtool の中の移動も)" "bin/rel:2: パスの段としての .."

new_case; printf '#!/bin/bash\nroot="$(cd "$here/.." && pwd)"\n' > "$D/bin/up"
expect_fail "末尾の /.. を落とす" "bin/up:2: パスの段としての .."

new_case; printf '#!/bin/bash\ncd ..\n' > "$D/bin/bare"
expect_fail "単独の .. を落とす" "bin/bare:2: パスの段としての .."

new_case; printf '#!/bin/bash\n# shellcheck source=../../bin/lib/x.sh\n. "$LIB"\n' > "$D/bin/sc"
expect_fail "shellcheck source= の .. を落とす" "bin/sc:2: shellcheck source= が .. を含む"

new_case; printf '#!/bin/bash\ncat <../bin/skill-tmpdir\n' > "$D/bin/redir"
expect_fail "リダイレクトの直後の ../ を落とす" "bin/redir:2: パスの段としての .."

new_case; printf '#!/bin/bash\n. "${LIB:-../bin/lib/runtimeout.sh}"\n' > "$D/bin/param"
expect_fail "パラメータ展開の既定値の ../ を落とす" "bin/param:2: パスの段としての .."

new_case; printf '#!/bin/bash\necho "..." a..b x.../y\n' > "$D/bin/ellipsis"
expect_ok "... や a..b はパスの段として落とさない"

new_case; printf '#!/bin/bash\n. "$HOME"/dotfiles/bin/lib/runtimeout.sh\n' > "$D/bin/qhome"
expect_fail "引用符で区切った \"\$HOME\"/dotfiles を落とす" "bin/qhome:2: dotfiles の checkout を直接指す"

new_case; printf '#!/bin/bash\nx="$HOME/dotfiles/bin/runtimeout"\n' > "$D/bin/home"
expect_fail "\$HOME/dotfiles を落とす" "bin/home:2: dotfiles の checkout を直接指す"

new_case; printf '#!/bin/bash\nx=~/dotfiles/bin/skill-tmpdir\n' > "$D/bin/tilde"
expect_fail "チルダで書いた dotfiles (~/dotfiles) を落とす" "bin/tilde:2: dotfiles の checkout を直接指す"

new_case; printf '#!/bin/bash\nx="${HOME}/dotfiles"\n' > "$D/bin/brace"
expect_fail "\${HOME}/dotfiles を落とす" "bin/brace:2: dotfiles の checkout を直接指す"

new_case; printf '#!/bin/bash\nx=/Users/someone/dotfiles/bin\n' > "$D/bin/abs"
expect_fail "/Users/<名前>/dotfiles を落とす" "bin/abs:2: dotfiles の checkout を直接指す"

new_case; mkdir -p "$D/x"; printf 'module x\nreplace y => ../../src/y\n' > "$D/x/go.mod"
expect_fail "go.mod の外向きの replace を落とす" "x/go.mod:2: パスの段としての .."

new_case; ln -s "$ROOT_DIR/bin/skill-tmpdir" "$D/bin/out-link" || exit 1
expect_fail "devtool の外を指す symlink を落とす" "devtool の外を指す symlink: bin/out-link"

new_case; ln -s "$T/absent" "$D/bin/dangling" || exit 1
expect_fail "解けない symlink を外と同じに落とす" "devtool の外を指す symlink: bin/dangling"

new_case; ln -s absent "$D/bin/in-dangling" || exit 1
expect_fail "中を指していてもリンク先の無い symlink は落とす" "devtool の外を指す symlink: bin/in-dangling"

new_case; ln -s .. "$D/bin/root-link" || exit 1
expect_ok "devtool の root 自体を指す symlink は通す"

new_case; ln -s ok "$D/bin/in-link" || exit 1
expect_ok "devtool の中を指す symlink は通す"

new_case; printf '#!/bin/bash\ncd .. # devtool-boundary: allow テストの fixture の説明\n' > "$D/bin/allowed"
expect_ok "理由つきの印の行は通す"

new_case; printf '#!/bin/bash\ncd .. # devtool-boundary: allow\n' > "$D/bin/noreason"
expect_fail "理由の無い印は落とす" "理由の無い devtool-boundary: allow"

n=$((n + 1)); D="$T/c$n/devtool"; mkdir -p "$D" && printf '# 説明だけ\n' > "$D/README.md"
expect_fail ".md しか無いなら 0 件として落とす" "検査対象のファイルが 0 件"

"$CHECK" "$T/absent-devtool" > "$T/out" 2>&1; rc=$?
if [ "$rc" -eq 1 ] && grep -qF 'devtool のディレクトリが無い' "$T/out"; then pass "ディレクトリが無いなら落とす"; else fail "ディレクトリが無いときの扱い (rc=$rc): $(cat "$T/out")"; fi

# 本物の devtool/ も検査が通り、件数が 1 以上であること (本走査の canary)
"$CHECK" > "$T/out" 2>&1; rc=$?
if [ "$rc" -eq 0 ] && grep -qE '検査対象 [1-9][0-9]* 件' "$T/out"; then pass "本物の devtool/ を検査して通る"; else fail "本物の devtool/ (rc=$rc): $(cat "$T/out")"; fi

if [ "$FAIL_COUNT" -gt 0 ]; then exit 1; fi
