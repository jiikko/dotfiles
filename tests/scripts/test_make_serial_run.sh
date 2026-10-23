#!/usr/bin/env bash
# make/serial-run.mk の前置き ($(serial_run)) が、包むときと包まないときを正しく分けることを pin する。
# 本物の Makefile の test を -n で試さない: `+` の行は dry-run でも走るので、本当に順番待ちに入る。
# ここは断片だけを include した Makefile で前置きの値を見る。Makefile の test が前置きを使っていることは字面で見る。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

td=$(mktemp -d) || exit 1
trap 'rm -rf "$td"' EXIT
# `+` を付けて -n / -q / -t の下でも echo を走らせる (前置きの値を見るため)
# show は .PHONY にする (-t は target のファイルを作るので、2 回目から「up to date」で何も走らなくなる)。make は一時 dir で走らせる
printf 'include %s/make/serial-run.mk\n.PHONY: show\nshow:\n\t@+echo "[$(serial_run)]"\n' "$ROOT_DIR" > "$td/Makefile"
# 既定の置き場 ($HOME/dotfiles/bin/serial-run) を一時の HOME に作る (CI の runner の HOME には dotfiles が無い)
mkdir -p "$td/home/dotfiles/bin" && : > "$td/home/dotfiles/bin/serial-run"

FAIL=0
PASS=0
expect() {  # $1=期待 $2=説明 $3=make の引数 (空白区切り) 残り=env の足し引き
  local want="$1" desc="$2" margs="$3" got
  shift 3
  # shellcheck disable=SC2086 # make の引数は語に分けて渡す
  got=$(env -u SERIAL_RUN_HELD -u CI -u SERIAL_RUN_MAKE_DISABLE -u MAKEFLAGS -u MAKELEVEL HOME="$td/home" "$@" make --no-print-directory -s -C "$td" $margs -f "$td/Makefile" show 2>&1)
  got=${got##*$'\n'} # -n は実行する行そのものも印字するので、最後の行 (echo の出力) を比べる
  if [ "$got" = "$want" ]; then
    echo "✓ $desc"
    PASS=$((PASS + 1))
  else
    echo "✗ $desc: got [$got] want [$want]"
    FAIL=$((FAIL + 1))
  fi
}

expect "[/bin/echo --]" "CI でなければ包む" "" SERIAL_RUN_BIN=/bin/echo
expect "[/bin/echo --]" "SERIAL_RUN_HELD があっても包む (入れ子の判定は serial-run が札を照合して行う)" "" SERIAL_RUN_BIN=/bin/echo SERIAL_RUN_HELD=x
expect "[]" "CI では包まない" "" SERIAL_RUN_BIN=/bin/echo CI=true
expect "[]" "SERIAL_RUN_MAKE_DISABLE で外せる" "" SERIAL_RUN_BIN=/bin/echo SERIAL_RUN_MAKE_DISABLE=1
expect "[]" "serial-run が無ければ包まない" "" SERIAL_RUN_BIN=/nonexistent/serial-run
expect "[]" "make -n では包まない (dry-run が本物の列に並ばない)" "-n" SERIAL_RUN_BIN=/bin/echo
expect "[]" "make -q では包まない" "-q" SERIAL_RUN_BIN=/bin/echo
expect "[]" "make -t では包まない" "-t" SERIAL_RUN_BIN=/bin/echo
expect "[/bin/echo --]" "make -k は包む (実行はする)" "-k" SERIAL_RUN_BIN=/bin/echo
expect "[/bin/echo --]" "コマンドラインの変数の n を -n と読まない" "VAR=n" SERIAL_RUN_BIN=/bin/echo
expect "[$td/home/dotfiles/bin/serial-run --]" "既定は本体 checkout の serial-run (worktree の版ではない)" ""

# root の Makefile の入口 test が前置きを通っている (本体は test-all)
if grep -q '\$(serial_run) \$(MAKE) --no-print-directory test-all' <<< "$(awk '/^test:/{f=1;next} f&&/^\t/{print;exit}' "$ROOT_DIR/Makefile")"; then
  echo "✓ Makefile の test が serial-run の前置きを通る"
  PASS=$((PASS + 1))
else
  echo "✗ Makefile の test が serial-run の前置きを通っていない"
  FAIL=$((FAIL + 1))
fi

if [ "$FAIL" -gt 0 ]; then
  echo "✗ $FAIL 件失敗 ($PASS 件通過)"
  exit 1
fi
echo "✓ make/serial-run.mk: すべて通過 ($PASS 件)"
