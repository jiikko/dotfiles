#!/usr/bin/env zsh
# zshlib/_src_env.zsh (~/src の下で .src-env の KEY=パス を export する) のテスト。
# 一時の dir を SRC_ENV_ROOT にして、入る・下へ降りる・外へ出るたびに _src_env_apply を呼び、環境変数を見る。

set -euo pipefail
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

TMP_ROOT=$(mktemp -d)
cleanup() { rm -rf "$TMP_ROOT"; }
trap cleanup EXIT
TMP_ROOT="${TMP_ROOT:A}"

source "$ROOT_DIR/zshlib/_src_env.zsh"

fails=0
ok() { print "✓ $1"; }
ng() { print -u2 "✗ $1"; fails=$(( fails + 1 )); }
expect_var() {
  local label="$1" name="$2" want="$3"
  if [[ "$want" == "<unset>" ]]; then
    if (( ${+parameters[$name]} )); then ng "$label: $name=${(P)name} (unset のはず)"; else ok "$label"; fi
  elif [[ "${(P)name-<unset>}" == "$want" ]]; then
    ok "$label"
  else
    ng "$label: $name=${(P)name-<unset>} (期待 $want)"
  fi
}
go() { cd "$1"; _src_env_apply 2>> "$TMP_ROOT/warn.log"; }

src="$TMP_ROOT/src"
umb="$src/umbrella"
mkdir -p "$umb/apps/app1/deep" "$umb/lib" "$src/other" "$TMP_ROOT/outside"
cat > "$umb/.src-env" <<'EOF'
# umbrella の root
UMBRELLA_ROOT=$DIR
UMBRELLA_LIB="${DIR}/lib"
ABS_PATH=/opt/example
EOF
cat > "$umb/apps/app1/.src-env" <<'EOF'
APP_ROOT=$DIR
ABS_PATH=$DIR/override
EOF
cp "$umb/.src-env" "$TMP_ROOT/outside/.src-env"
export SRC_ENV_ROOT="$src"
unset UMBRELLA_ROOT APP_ROOT UMBRELLA_LIB 2>/dev/null || true
export ABS_PATH=original

go "$umb"
expect_var "root で \$DIR がそのディレクトリになる" UMBRELLA_ROOT "$umb"
expect_var "\${DIR}/sub と引用符" UMBRELLA_LIB "$umb/lib"
expect_var "絶対パス" ABS_PATH /opt/example

go "$umb/apps/app1/deep"
expect_var "submodule の下でも上の .src-env が効く" UMBRELLA_ROOT "$umb"
expect_var "下の .src-env も効く" APP_ROOT "$umb/apps/app1"
expect_var "同じ KEY は近い方が勝つ" ABS_PATH "$umb/apps/app1/override"

go "$umb/lib"
expect_var "下の .src-env の外へ出たら、その KEY は消える" APP_ROOT "<unset>"
expect_var "上の値に戻る" ABS_PATH /opt/example

go "$src/other"
expect_var "umbrella の外では消える (元が無い)" UMBRELLA_ROOT "<unset>"
expect_var "元の値に戻る" ABS_PATH original

go "$TMP_ROOT/outside"
expect_var "SRC_ENV_ROOT の外の .src-env は読まない" UMBRELLA_ROOT "<unset>"

# 受け付けない行
mkdir -p "$src/bad"
cat > "$src/bad/.src-env" <<'EOF'
PATH=$DIR/bin
GIT_SSH_COMMAND=/tmp/evil
DYLD_INSERT_LIBRARIES=/tmp/x.dylib
REL_PATH=relative/dir
CMD_SUB=$DIR/$(touch /tmp/should-not-run)
lowercase=/x
GOOD_DIR=$DIR
EOF
: > "$TMP_ROOT/warn.log"
path_before="$PATH"
go "$src/bad"
expect_var "PATH は受け付けない" PATH "$path_before"
expect_var "GIT_* は受け付けない" GIT_SSH_COMMAND "<unset>"
expect_var "DYLD_* は受け付けない" DYLD_INSERT_LIBRARIES "<unset>"
expect_var "相対パスは受け付けない" REL_PATH "<unset>"
expect_var "\$( を含む値は受け付けない" CMD_SUB "<unset>"
expect_var "受け付ける行は同じファイルでも効く" GOOD_DIR "$src/bad"
n="$(grep -c 'src-env:' "$TMP_ROOT/warn.log" || true)"
[[ "$n" == 6 ]] && ok "受け付けない 6 行を知らせる" || ng "知らせた行の数: $n (期待 6)"
: > "$TMP_ROOT/warn.log"
go "$src/bad"
n="$(grep -c 'src-env:' "$TMP_ROOT/warn.log" || true)"
[[ "$n" == 0 ]] && ok "同じ行は 2 回目から知らせない" || ng "2 回目に知らせた行の数: $n"

# umbrella_root と cdu
go "$umb/apps/app1/deep"
r="$(umbrella_root)" && [[ "$r" == "$umb" ]] && ok "umbrella_root は root を出す" || ng "umbrella_root: [$r]"
r="$(umbrella_root apps/app1)" && [[ "$r" == "$umb/apps/app1" ]] && ok "umbrella_root <パス> は root の下を出す" || ng "umbrella_root apps/app1: [$r]"
if r="$(umbrella_root no/such 2> "$TMP_ROOT/ur.err")"; then ng "無いパスで umbrella_root が成功した: [$r]"; else
  [[ -z "$r" ]] && grep -q 'が無い' "$TMP_ROOT/ur.err" && ok "無いパスは rc 1 で理由を出す" || ng "無いパスの扱い: [$r]"
fi
cdu
[[ "$PWD" == "$umb" ]] && ok "cdu は root へ移る" || ng "cdu の後の PWD: $PWD"
cdu apps/app1
[[ "$PWD" == "$umb/apps/app1" ]] && ok "cdu <パス> は root の下へ移る" || ng "cdu apps/app1 の後の PWD: $PWD"
go "$src/other"
if r="$(umbrella_root 2> "$TMP_ROOT/ur.err")"; then ng "umbrella の外で umbrella_root が成功した: [$r]"; else
  [[ -z "$r" ]] && grep -q 'UMBRELLA_ROOT が無い' "$TMP_ROOT/ur.err" && ok "umbrella の外では rc 1 で理由を出す (空を出さない)" || ng "外での扱い: [$r]"
fi
if cdu 2> "$TMP_ROOT/cdu.err"; then ng "UMBRELLA_ROOT が無いのに cdu が成功した"; else
  [[ "$PWD" == "$src/other" ]] && grep -q 'UMBRELLA_ROOT が無い' "$TMP_ROOT/cdu.err" && ok "UMBRELLA_ROOT が無ければ cdu は移らずに知らせる" || ng "cdu の失敗の扱い: PWD=$PWD"
fi
export UMBRELLA_ROOT="$TMP_ROOT/gone"
if umbrella_root > /dev/null 2> "$TMP_ROOT/ur.err"; then ng "指す先が無いのに umbrella_root が成功した"; else
  grep -q 'のディレクトリが無い' "$TMP_ROOT/ur.err" && ok "指す先が無ければ rc 1 で理由を出す" || ng "指す先が無いときの理由: $(cat "$TMP_ROOT/ur.err")"
fi
unset UMBRELLA_ROOT

if (( fails )); then
  print -u2 "src-env: $fails 件失敗"
  exit 1
fi
