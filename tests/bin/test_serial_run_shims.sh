#!/usr/bin/env bash
# PATH shim (bin/swift・bin/xcodebuild。共通部分は bin/lib/serial_run_shim.sh) が、対象の呼び出しを serial-run --if-heavy に渡し、
# 列の中・CI・無効化では実体をそのまま起こすことを固定する。実体と serial-run は偽物 (呼ばれた引数を記録する) にする。
# 本物の serial-run の判定 (--if-heavy・isHeavy) は src/serialrun の Go のテストが見る
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1

td=$(mktemp -d) || exit 1
trap 'rm -rf "$td"' EXIT
log=$td/log
mkdir -p "$td/real" "$td/lnk" "$td/lnk2"
# 偽の実体: 引数と列の中の印を記録し、stdin を記録に足して FAKE_RC で終わる
cat > "$td/real/fake" <<'EOF'
#!/bin/sh
printf 'real[%s] held=%s failopen=%s\n' "$*" "${SERIAL_RUN_HELD-}" "${SERIAL_RUN_FAIL_OPEN-}" >> "$LOG"
case " $* " in *" stdin "*) cat >> "$LOG" ;; esac
exit "${FAKE_RC:-0}"
EOF
# 偽の serial-run: 引数を記録してから -- の後ろを起こす (本物と同じく子の rc を返す。本物と違い SERIAL_RUN_FAIL_OPEN を外さない)。
# shebang は本物 (bin/serial-run) と同じく zsh にする (/bin/sh だと、shim が -f を付け忘れても .zshenv を読まず、その検査が素通りする)
cat > "$td/sr" <<'EOF'
#!/usr/bin/env zsh
printf 'sr[%s] failopen=%s\n' "$*" "${SERIAL_RUN_FAIL_OPEN-}" >> "$LOG"
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
EOF
chmod +x "$td/real/fake" "$td/sr"
# 別の checkout (隣に _claude/settings.json がある bin) を 2 つ作り、shim と、shim の隣の serial-run (偽物。印 sibling を記録する) を置く
for co in other other2; do
  mkdir -p "$td/$co/bin/lib" "$td/$co/_claude"
  : > "$td/$co/_claude/settings.json"
  cp "$ROOT_DIR/bin/lib/serial_run_shim.sh" "$td/$co/bin/lib/"
  sed 's/sr\[%s\]/sibling-sr[%s]/' "$td/sr" > "$td/$co/bin/serial-run"
  chmod +x "$td/$co/bin/serial-run"
done
for tool in swift xcodebuild; do
  ln -s fake "$td/real/$tool"
  cp "$ROOT_DIR/bin/$tool" "$td/other/bin/$tool"
  cp "$ROOT_DIR/bin/$tool" "$td/other2/bin/$tool"
  # 別の checkout の shim を指す外の symlink (lnk は other、lnk2 は other2)
  ln -s "$td/other/bin/$tool" "$td/lnk/$tool"
  ln -s "$td/other2/bin/$tool" "$td/lnk2/$tool"
done

PASS=0
FAIL=0
ok() { echo "✓ $1"; PASS=$((PASS + 1)); }
ng() { echo "✗ $1"; FAIL=$((FAIL + 1)); }

# run <コマンド名> <PATH> <env の足し…> -- <引数…>: 素の名前で起こし (PATH の探索を通す)、rc と記録を返す。
# SERIAL_RUN_BIN は偽物に向ける (空を渡すと shim の既定 = shim と同じ checkout の serial-run)
run() {
  local tool="$1" p="$2"
  shift 2
  local -a extra=()
  while [ "$1" != -- ]; do extra+=("$1"); shift; done
  shift
  : > "$log"
  out=$(env -u SERIAL_RUN_HELD -u CI -u SERIAL_RUN_SHIM_DISABLE -u SERIAL_RUN_FAIL_OPEN -u FAKE_RC \
    LOG="$log" SERIAL_RUN_BIN="$td/sr" PATH="$p" ${extra[@]+"${extra[@]}"} \
    "$RUNTIMEOUT" 10 "$tool" "$@" 2>&1 </dev/null)
  rc=$?
  got=$(cat "$log")
}
expect_log() {  # $1=説明 $2=期待する記録
  if [ "$got" = "$2" ]; then ok "$1"; else ng "$1: got [$got] want [$2] (rc=$rc out=$out)"; fi
}

P="$ROOT_DIR/bin:$td/real:/usr/bin:/bin"

# --- swift: build / test だけを serial-run へ渡す
run swift "$P" -- --version
expect_log "swift: build / test 以外は serial-run を起こさない" "real[--version] held= failopen="
run swift "$P" --
expect_log "swift: 引数の無い swift (REPL) もそのまま渡す" "real[] held= failopen="
run swift "$P" -- build -c release
expect_log "swift: build は serial-run --if-heavy を通る (待ちの上限は serial-run の既定)" \
  "sr[--if-heavy -- $td/real/swift build -c release] failopen=1
real[build -c release] held= failopen=1"

# --- xcodebuild: 前段の振り分けを置かず、全部を serial-run --if-heavy へ渡す (情報表示の判定は Go の xcodeInfoFlags)
run xcodebuild "$P" -- -scheme X test
expect_log "xcodebuild: test は serial-run --if-heavy を通る" \
  "sr[--if-heavy -- $td/real/xcodebuild -scheme X test] failopen=1
real[-scheme X test] held= failopen=1"
run xcodebuild "$P" --
expect_log "xcodebuild: 引数なし (既定の scheme のビルド) も serial-run を通る" \
  "sr[--if-heavy -- $td/real/xcodebuild] failopen=1
real[] held= failopen=1"

# --- 共通の部分 (bin/lib/serial_run_shim.sh) は両方で見る
for tool in swift xcodebuild; do
  run "$tool" "$P" FAKE_RC=5 -- test
  [ "$rc" = 5 ] && ok "$tool: serial-run を通しても実体の rc を返す" || ng "$tool: rc=$rc (5 のはず)"
  run "$tool" "$P" SERIAL_RUN_HELD=x -- test
  expect_log "$tool: 列の中 (SERIAL_RUN_HELD) でも serial-run に照合を任せる (値をそのまま渡す)" "sr[--if-heavy -- $td/real/$tool test] failopen=1
real[test] held=x failopen=1"
  run "$tool" "$P" CI=true -- test
  expect_log "$tool: CI では列を使わない" "real[test] held= failopen="
  run "$tool" "$P" SERIAL_RUN_SHIM_DISABLE=1 -- test
  expect_log "$tool: SERIAL_RUN_SHIM_DISABLE で外せる" "real[test] held= failopen="
  # serial-run (zsh) を起こすときに ~/.zshenv を読まない (そこで xcodebuild / swift を呼ぶ設定があると shim と再帰する)
  mkdir -p "$td/zdot"
  printf 'print zshenv-read >> "$LOG"\n' > "$td/zdot/.zshenv"
  run "$tool" "$P" ZDOTDIR="$td/zdot" -- test
  expect_log "$tool: serial-run を起こすときに .zshenv を読まない" "sr[--if-heavy -- $td/real/$tool test] failopen=1
real[test] held= failopen=1"
  run "$tool" "$P" SERIAL_RUN_BIN="$td/none" -- test
  expect_log "$tool: serial-run が無ければ実体を起こす" "real[test] held= failopen="

  : > "$log"
  out=$(printf 'from-stdin\n' | env -u SERIAL_RUN_HELD -u CI LOG="$log" PATH="$P" SERIAL_RUN_BIN="$td/sr" "$RUNTIMEOUT" 10 "$tool" test stdin 2>&1)
  rc=$?
  got=$(sed '/^sr\[/d' "$log")
  [ "$rc" = 0 ] || ng "$tool: stdin の run の rc=$rc"
  expect_log "$tool: serial-run を通しても stdin を実体へ渡す" "real[test stdin] held= failopen=1
from-stdin"

  run "$tool" "$td/other/bin:$ROOT_DIR/bin:$td/real:/usr/bin:/bin" CI=true -- -version
  expect_log "$tool: 別の checkout の shim を実体と読まない (互いに呼び合わない)" "real[-version] held= failopen="
  run "$tool" "$td/lnk:$td/real:/usr/bin:/bin" CI=true -- -version
  expect_log "$tool: 別の checkout の shim を指す symlink も実体と読まない" "real[-version] held= failopen="
  # PATH に /usr/bin (readlink) が無くても、別の checkout を指す symlink どうしで呼び合わない
  run "$tool" "$td/lnk:$td/lnk2" CI=true -- -version
  if [ "$rc" = 127 ] && [ -z "$got" ]; then
    ok "$tool: PATH に readlink が無くても、別の checkout の shim の symlink どうしで呼び合わない"
  else
    ng "$tool: readlink の無い PATH: rc=$rc out=$out log=$got"
  fi
  run "$tool" "$td/other/bin:$td/real" SERIAL_RUN_BIN= -- test
  expect_log "$tool: 既定では shim と同じ checkout の serial-run を呼ぶ" "sibling-sr[--if-heavy -- $td/real/$tool test] failopen=1
real[test] held= failopen=1"

  # / の無い名前で起こしても lib を読める (bash swift)
  : > "$log"
  out=$(cd "$ROOT_DIR/bin" && env -u SERIAL_RUN_HELD LOG="$log" CI=true PATH="$td/real:/usr/bin:/bin" "$RUNTIMEOUT" 10 /bin/bash "$tool" -version 2>&1)
  rc=$?
  got=$(cat "$log")
  expect_log "$tool: / の無い名前 (bash $tool) で起こしても実体へ届く" "real[-version] held= failopen="

  run "$tool" "$ROOT_DIR/bin" -- -version
  if [ "$rc" = 127 ] && [ -z "$got" ] && grep -q "実体の $tool が無い" <<< "$out"; then
    ok "$tool: 実体が無ければ 127 で止まる (自分を呼び続けない)"
  else
    ng "$tool: 実体が無いとき: rc=$rc out=$out log=$got"
  fi
done

if [ "$FAIL" -gt 0 ]; then
  echo "✗ serial-run の PATH shim: $FAIL 件失敗 ($PASS 件通過)"
  exit 1
fi
echo "✓ serial-run の PATH shim: すべて通過 ($PASS 件)"
