#!/usr/bin/env bash
# テストの runner が、bin にしか無い道具を素の名前で呼ぶテストを手元でも rc=127 で落とすことを固定する。
# CI の PATH には dotfiles の bin/ が無いので、手元だけで通る素の呼び出しは CI でしか見つからない。
# scripts/test_bare_bin_guard.sh の判定と、直列・並列の runner への配線の両方を見る。
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
GUARD="$ROOT_DIR/scripts/test_bare_bin_guard.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/bare-bin-guard.XXXXXX") || { echo "✗ mktemp -d 失敗"; exit 1; }
trap 'rm -rf "$work"' EXIT
work=$(cd "$work" && pwd -P) || exit 1

fails=0
ok() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

mkx() { mkdir -p "${1%/*}" && printf '#!/bin/sh\necho real-%s\n' "${1##*/}" > "$1" && chmod +x "$1"; }

# --- 判定 (guard 単体) ---
r="$work/root"
mkx "$r/bin/onlyhere"
mkx "$r/bin/dot.sh"
# bin の実体への symlink だけが在る dir は「他の場所」に数えない (CI には無い)。root と別の checkout の bin を指すものも同じ
mkdir -p "$work/symfarm" && ln -s "$r/bin/onlyhere" "$work/symfarm/onlyhere"
mkx "$r/bin/in-clone"
mkx "$work/clone/bin/in-clone"
ln -s "$work/clone/bin/in-clone" "$work/symfarm/in-clone"
# <root>/bin/x が外の実体を指す symlink なら、外の実体は他の場所に在るので stub にしない
mkx "$work/other/realtool"
ln -s "$work/other/realtool" "$r/bin/realtool"
mkx "$r/bin/shadowed"
mkx "$r/bin/in-home-dotfiles"
printf 'x\n' > "$r/bin/notexec"
mkdir -p "$r/bin/lib"
mkx "$r/bin/bad name"
mkx "$work/other/shadowed"
mkx "$work/home/dotfiles/bin/in-home-dotfiles"
ln -s "$r/bin" "$work/binlink"
# HOME の外にある別の clone の bin (隣に _claude/settings.json を持つ) も dotfiles の bin に数える
mkx "$work/clone/bin/onlyhere"
mkdir -p "$work/clone/_claude" && : > "$work/clone/_claude/settings.json"

out=$(HOME="$work/home" PATH="$work/binlink:$work/home/dotfiles/bin:$work/clone/bin:$work/symfarm:$work/other:/usr/bin:/bin" "$GUARD" "$r" "$work/stubs" 2>"$work/guard.err"); rc=$?
[ "$rc" = 0 ] && ok "guard: rc=0" || bad "guard: rc=$rc ($(cat "$work/guard.err"))"
[ "${out%%:*}" = "$work/stubs" ] && ok "guard: stub の dir を PATH の先頭に置く" || bad "guard: 出した PATH の先頭が違う: $out"
[ "${out#*:}" = "$work/binlink:$work/home/dotfiles/bin:$work/clone/bin:$work/symfarm:$work/other:/usr/bin:/bin" ] \
  && ok "guard: 元の PATH は並びを変えずに後ろへ残す" || bad "guard: 元の PATH が変わった: $out"
got=$(cd "$work/stubs" && ls | sort | paste -sd' ' -)
[ "$got" = "dot.sh in-clone in-home-dotfiles onlyhere" ] \
  && ok "guard: bin にしか無い名前だけを stub にする (他の dir に在る名前・実行できないもの・dir・空白を含む名前は作らない。~/dotfiles/bin・symlink 越しの bin・別の clone の bin・dotfiles の bin の実体への symlink は『他の dir』に数えない。外の実体を指す bin の symlink は stub にしない。. を含む名前も作る)" \
  || bad "guard: stub の一覧が違う: [$got]"
grep -q 'stub にした' "$work/guard.err" && ok "guard: 作った本数を stderr に出す" || bad "guard: 本数の行が無い: $(cat "$work/guard.err")"
msg=$("$work/stubs/onlyhere" 2>&1); src=$?
[ -f "$work/stubs/.bare-bin-stubs" ] && ok "guard: stub の dir に目印を置く" || bad "guard: 目印が無い"
p2=$(HOME="$work/home" PATH="$out" "$GUARD" "$r" "$work/stubs-inner" 2>/dev/null) && [ -x "$work/stubs-inner/onlyhere" ] \
  && ok "guard: 外側の stub の dir を『他の場所』に数えない (runner の入れ子でも stub を作る)" || bad "guard: 入れ子で stub を作らなかった: $p2"
[ "$src" = 127 ] && ok "stub: rc=127 (CI で見つからないときと同じ)" || bad "stub: rc=$src"
case "$msg" in *"'onlyhere' を PATH から探して呼んだ"*) ok "stub: 呼んだ名前と理由を出す" ;; *) bad "stub: 案内が違う: $msg" ;; esac

mkdir -p "$work/empty/bin"
HOME="$work/home" PATH="/usr/bin:/bin" "$GUARD" "$work/empty" "$work/stubs2" >/dev/null 2>&1 \
  && bad "guard: 列挙が空なのに rc=0" || ok "guard: stub が 0 本なら失敗にする (列挙の空振りを緑にしない)"
HOME="$work/home" PATH="/usr/bin:/bin" "$GUARD" "$work/nowhere" "$work/stubs3" >/dev/null 2>&1 \
  && bad "guard: bin の無い root で rc=0" || ok "guard: bin の無い root は失敗にする"

# --- runner への配線 ---
# 開発機と同じく repo の bin を PATH に置いた状態で make を起こす。配線が外れていれば ci-log が実体に解決して通ってしまう。
# 外側の runner が export した TEST_PATH は外す (残すと、内側の配線が外れても外側の stub で同じ結果になる)。
run_make() {
  env -u CI -u MAKEFLAGS -u MAKELEVEL -u MFLAGS -u TEST_PATH -u TEST_FAIL_LEDGER \
    HOME="$work/mhome" PATH="$ROOT_DIR/bin:$PATH" make -C "$ROOT_DIR" --no-print-directory "$@"
}
mkdir -p "$work/suite"
cat > "$work/suite/test_bare.sh" <<'EOF'
#!/bin/sh
msg=$(ci-log -h 2>&1); rc=$?
[ "$rc" = 127 ] || { echo "ci-log が rc=$rc で返った (stub に当たっていない)"; exit 1; }
case "$msg" in *"'ci-log' を PATH から探して呼んだ"*) ;; *) echo "案内が無い: $msg"; exit 1 ;; esac
t=$(command -v tmux) || exit 0
case "$t" in */bare-bin-stubs/*) echo "tmux が stub に当たった: $t"; exit 1 ;; esac
exit 0
EOF
chmod +x "$work/suite/test_bare.sh"

run_make test-dir DIR="$work/suite" > "$work/serial.out" 2>&1; rc=$?
[ "$rc" = 0 ] && grep -q '\[bare-bin\]' "$work/serial.out" \
  && ok "直列 runner: 素の ci-log が案内つきの rc=127 になり、tmux の shim は残る" \
  || bad "直列 runner: rc=$rc $(tail -5 "$work/serial.out")"
run_make test-discovered-heavy CI_HEAVY_TEST_DIRS="$work/suite" > "$work/par.out" 2>&1; rc=$?
[ "$rc" = 0 ] && grep -q '\[bare-bin\]' "$work/par.out" && grep -q "\[ok\] $work/suite/test_bare.sh" "$work/par.out" \
  && ok "並列 runner: 同上" \
  || bad "並列 runner: rc=$rc $(tail -5 "$work/par.out")"

# test-bats は bats の場所を tests/ に決め打ちしていて fixture を流せないので、配線を字面で固定する
# レシピは target の行の次から、tab で始まらない最初の行の手前まで
recipe=$(awk '/^test-bats:/{f=1; next} f && !/^\t/{exit} f' "$ROOT_DIR/Makefile")
case "$recipe" in *'$(TEST_CHILD_ENV) bats "$$t"'*) b1=1 ;; *) b1=0 ;; esac
case "$recipe" in *'$(call bare_bin_guard,'*) b2=1 ;; *) b2=0 ;; esac
[ "$b1$b2" = 11 ] && ok "test-bats: guard を呼び、TEST_CHILD_ENV (PATH) で bats を起こす (字面の固定)" \
  || bad "test-bats: 配線が外れた (TEST_CHILD_ENV=$b1 guard=$b2)"

[ "$fails" -eq 0 ] || { echo "✗ $fails 件失敗"; exit 1; }
echo "✓ すべて通過"
