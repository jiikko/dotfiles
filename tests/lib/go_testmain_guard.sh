# shellcheck shell=bash
# go_testmain_guard.sh — Go のテストのパッケージが TestMain の前処理 (guard) を呼んでいるかを判定する。
# tests/scripts/test_git_tests_unset_location_env.sh と test_widthenv_guarded.sh が共有する。
#
# 認める形は 2 つ:
#   1. パッケージの _test.go が guard を直接呼ぶ
#   2. パッケージの _test.go が `<module>/internal/testenv` を import して testenv.Main を呼び、その testenv の
#      func Main が m.Run() より前に guard を呼ぶ (glogx は前処理を internal/testenv に寄せた)
# 検出しない形 (どちらの形も字面で見る): コメント・文字列・`if false` の中の呼び出し・TestMain 以外の関数の中の呼び出し・
# `xtestenv.Main(` のような左に続く名前・2 の Main の中で guard が m.Run の後に走る形 (defer・使われない closure・条件分岐・
# m.Run を別の関数の中で呼ぶ)。最後の形は、UnsetGitLocationEnv については src/glogx/internal/testenv/main_test.go が実行時に
# 確かめる (widthenv.ExitIfUnsupported はプロセスを終えるので実行時に確かめられない)。脅威モデルはうっかりの漏れで、
# testenv の Main を意図して作り替える形は止めない
# 誤検出で赤になる形: testenv.Main を別名で import した呼び出し・Main が guard を別の関数経由で呼ぶ形 (Main の本文に guard の字面を要求する)

# go_testmain_guard_present <パッケージの dir> <guard の ERE>
# rc: 0 = 呼んでいる / 1 = 呼んでいない / 2 = ファイルを読めない (判定不能)
# module の解決は repo root からの絶対パスで行う (cwd が repo root でないと go.mod を引けず、判定不能が「呼んでいない」に化ける)
GO_TESTMAIN_GUARD_ROOT="$(CDPATH='' cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)" || return 1

go_testmain_guard_present() {
  local dir="$1" guard="$2" rc f imp mod moddir tdir src gm
  local tests=()
  shopt -s nullglob
  tests=("$dir"/*_test.go)
  shopt -u nullglob
  [ "${#tests[@]}" -gt 0 ] || return 1

  grep -lqE "$guard" "${tests[@]}"; rc=$?
  [ "$rc" -le 1 ] || return 2
  [ "$rc" -eq 0 ] && return 0

  for f in "${tests[@]}"; do
    grep -qE 'testenv\.Main\(' "$f"; rc=$?
    [ "$rc" -le 1 ] || return 2
    [ "$rc" -eq 0 ] || continue
    imp="$(grep -oE '"[^"]+/internal/testenv"' "$f" | head -1)"
    [ -n "$imp" ] || continue
    mod="${imp#\"}"; mod="${mod%/internal/testenv\"}"
    # module path から src/<dir> を引く (module の名前と dir の名前は一致しないことがある)
    moddir=""
    for gm in "$GO_TESTMAIN_GUARD_ROOT"/src/*/go.mod; do
      if grep -qxE "module $mod" "$gm"; then moddir="${gm%/go.mod}"; break; fi
    done
    [ -n "$moddir" ] || continue
    tdir="$moddir/internal/testenv"
    for src in "$tdir"/*.go; do
      case "$src" in *_test.go) continue ;; esac
      [ -r "$src" ] || return 2
      # build 制約の付いたファイル (`//go:build ignore` の古い Main など) は数えない
      grep -q '^//go:build' "$src"; rc=$?
      [ "$rc" -le 1 ] || return 2
      [ "$rc" -eq 1 ] || continue
      # func Main(m *testing.M ...) の本文で、最初の m.Run() より前に guard があるか
      # guard は ENVIRON で渡す (awk -v はバックスラッシュを解釈して ERE の \( を壊す)
      if GUARD_ERE="$guard" awk '
        /^func Main\(m \*testing\.M/ { inmain = 1; next }
        inmain && /^}/ { exit }
        inmain && /m\.Run\(\)/ { exit }
        inmain && $0 ~ ENVIRON["GUARD_ERE"] { found = 1; exit }
        END { exit found ? 0 : 1 }
      ' "$src"; then
        return 0
      fi
    done
  done
  return 1
}
