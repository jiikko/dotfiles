#!/usr/bin/env bash
# bin/lib/go_autobuild.zsh の unit テスト。
#
# 実 go は使わず、PATH 先頭に置いた偽 go でビルドを注入する (成功/失敗を切り替えられる)。
# ここで pin したいのは「ビルドの成否」ではなく機構の不変条件:
#   - stale 検知が再帰する (サブパッケージ・go.mod の変更を拾い、*_test.go では発火しない)
#   - ビルド失敗で既存バイナリが壊れない (一時ファイル → rename の atomicity)
#   - 失敗後はソースが変わるまで再挑戦しない (毎起動ごとにビルドを撒く CPU リークの防止)
#   - lock の持ち主が死んでいたら奪う (kill された builder の lock で旧版に永久固定されない)
#   - --async は旧版で即 exec し、ビルド完了は次回起動から反映される
#
# mtime は「何分前」で明示的に置く (mtime_at)。未来 mtime を打つと以降その file が永久に
# 新しくなり「毎回 stale」になってテストが意味を失うため、常に過去側で差を作る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/bin/lib/go_autobuild.zsh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

if [[ ! -f "$LIB" ]]; then
  printf '✗ ライブラリが存在しない: %s\n' "$LIB"
  exit 1
fi
printf '✓ %s exists\n' "${LIB#"$ROOT_DIR"/}"

fail() { printf '✗ %s\n' "$1"; exit 1; }
ok() { printf '✓ %s\n' "$1"; }

# 偽 go。`go build -o <out> .` で out に実行可能な shim を書き、呼ばれた回数を記録する。
mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/go" <<'EOS'
#!/bin/sh
if [ "$1" = "env" ]; then printf '%s\n' "${FAKE_GO_VERSION:-go1.99.0}"; exit 0; fi
echo "build" >> "$FAKE_GO_CALLS"
[ -n "${FAKE_GO_PIDFILE:-}" ] && echo $$ > "$FAKE_GO_PIDFILE"
[ -n "${FAKE_GO_SLEEP:-}" ] && sleep "$FAKE_GO_SLEEP"
if [ -n "${FAKE_GO_FAIL:-}" ]; then echo "fake build error" >&2; exit 1; fi
# 実 go と同じ -C の契約を守る: 最初のフラグでなければ使用法エラー (rc=2)、あれば chdir する。
# ⚠️ これが無いと -C の意味論がテストの盲点になる: 引数順を崩す変更 (-trimpath 等を先頭に足す)
# を入れても全テストが緑のまま通り、実環境だけラッパーが起動不能になる。
shift  # "build"
if [ "$1" = "-C" ]; then
  cd "$2" || exit 1
  shift 2
fi
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -C) echo "invalid value \"$2\" for flag -C: -C flag must be first flag on command line" >&2; exit 2 ;;
    -o) out="$2"; shift ;;
  esac
  shift
done
[ -n "$out" ] || exit 1
printf '#!/bin/sh\necho %s\n' "$FAKE_GO_MARK" > "$out"
chmod +x "$out"
exit 0
EOS
chmod +x "$TMP_DIR/bin/go"

mtime_at() {  # $1=何分前, 残り=対象ファイル
  local m="$1"; shift
  local ts
  ts="$(date -v-"${m}"M '+%Y%m%d%H%M.%S' 2>/dev/null || date -d "-${m} minutes" '+%Y%m%d%H%M.%S')"
  touch -t "$ts" "$@"
}

new_project() {  # $1=プロジェクト名 → $TMP_DIR/$1 に src と wrapper を作る (bin/glogx と同じ形)
  local name="$1" root="$TMP_DIR/$1"
  mkdir -p "$root/bin/lib" "$root/src/tool/sub"
  cp "$LIB" "$root/bin/lib/go_autobuild.zsh"
  printf 'module tool\n\ngo 1.99\n' > "$root/src/tool/go.mod"
  : > "$root/src/tool/go.sum"
  printf 'package main\n' > "$root/src/tool/main.go"
  printf 'package sub\n' > "$root/src/tool/sub/sub.go"
  cat > "$root/bin/tool" <<'EOS'
#!/usr/bin/env zsh
set -u
source "${0:A:h}/lib/go_autobuild.zsh"
go_autobuild_exec ${AUTOBUILD_ARGS:-} "${0:A:h}/../src/tool" tool -- "$@"
EOS
  chmod +x "$root/bin/tool"
  printf '%s' "$root"
}

# 全ソースを 60 分前・バイナリを 30 分前に置く = stale でない基準状態。
# 以後 bump (10 分前) した file だけが「新しいソース」になる。
freeze() {
  local root="$1"
  mtime_at 60 "$root/src/tool/main.go" "$root/src/tool/sub/sub.go" \
    "$root/src/tool/go.mod" "$root/src/tool/go.sum"
  [[ -e "$root/src/tool/sub/sub_test.go" ]] && mtime_at 60 "$root/src/tool/sub/sub_test.go"
  mtime_at 30 "$root/src/tool/tool"
}
bump() { mtime_at 10 "$1"; }

run_tool() {  # $1=root, 残り=tool 引数
  local root="$1"; shift
  PATH="$TMP_DIR/bin:$PATH" \
    FAKE_GO_CALLS="$root/calls" \
    FAKE_GO_MARK="${FAKE_GO_MARK:-v1}" \
    FAKE_GO_PIDFILE="${FAKE_GO_PIDFILE:-}" \
    "$root/bin/tool" "$@" 2>>"$root/stderr"
}

calls() { local f="$1/calls"; if [[ -f "$f" ]]; then wc -l < "$f" | tr -d ' '; else printf '0'; fi; }
# 偽 go が書くバイナリは `echo <mark>` の shim。最終行の最後の語が mark。
binary_mark() { tail -1 "$1/src/tool/tool" 2>/dev/null | awk '{print $NF}'; }
binary_is() { [[ "$(binary_mark "$1")" == "$2" ]]; }

wait_for() {  # $1=説明, 残り=条件コマンド。10 秒まで待つ
  local msg="$1"; shift
  local i
  for ((i = 0; i < 100; i++)); do
    "$@" && return 0
    sleep 0.1
  done
  fail "$msg (10 秒待っても成立しない)"
}

printf '\n## 初回ビルド (バイナリ不在なら同期)\n'
ROOT="$(new_project first)"
out="$(FAKE_GO_MARK=v1 run_tool "$ROOT")"
[[ "$out" == "v1" ]] || fail "初回ビルド後に新バイナリが exec されない (got: $out)"
[[ "$(calls "$ROOT")" == "1" ]] || fail "初回ビルドが 1 回で済んでいない"
ok "バイナリ不在なら同期ビルドして exec する"

out="$(FAKE_GO_MARK=v2 run_tool "$ROOT")"
[[ "$out" == "v1" ]] || fail "ソース未変更なのに再ビルドされた (got: $out)"
[[ "$(calls "$ROOT")" == "1" ]] || fail "ソース未変更なのにビルドが走った"
ok "ソース未変更なら再ビルドしない"

printf '\n## stale 検知の範囲\n'
freeze "$ROOT"
bump "$ROOT/src/tool/sub/sub.go"
out="$(FAKE_GO_MARK=v2 run_tool "$ROOT")"
[[ "$out" == "v2" ]] || fail "サブパッケージの変更で再ビルドされない (got: $out)"
ok "サブパッケージ (sub/*.go) の変更で再ビルドする"

freeze "$ROOT"
bump "$ROOT/src/tool/go.mod"
out="$(FAKE_GO_MARK=v3 run_tool "$ROOT")"
[[ "$out" == "v3" ]] || fail "go.mod の変更で再ビルドされない (got: $out)"
ok "go.mod の変更で再ビルドする"

printf 'package sub\n' > "$ROOT/src/tool/sub/sub_test.go"
freeze "$ROOT"
bump "$ROOT/src/tool/sub/sub_test.go"
before="$(calls "$ROOT")"
out="$(FAKE_GO_MARK=v4 run_tool "$ROOT")"
[[ "$(calls "$ROOT")" == "$before" ]] || fail "*_test.go の変更で再ビルドが走った"
[[ "$out" == "v3" ]] || fail "*_test.go 変更後に別バイナリが動いた (got: $out)"
ok "*_test.go の変更では再ビルドしない (再帰 glob でも除外される)"

printf '\n## ビルド失敗時 (同期)\n'
ROOT="$(new_project syncfail)"
FAKE_GO_MARK=v1 run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
if FAKE_GO_FAIL=1 run_tool "$ROOT" >/dev/null; then
  fail "同期ビルド失敗が成功として扱われた"
fi
ok "同期ビルド失敗は非 0 で終わる (loud)"
binary_is "$ROOT" v1 || fail "同期ビルド失敗で既存バイナリが壊れた"
ok "同期ビルド失敗でも既存バイナリは無傷"
out="$(FAKE_GO_MARK=v9 run_tool "$ROOT")"
[[ "$out" == "v9" ]] || fail "失敗後にビルドし直しても復帰しない (got: $out)"
ok "同期経路はソース未変更でも失敗後に再挑戦する"

printf '\n## --async: 旧版で即 exec し、次回から新版\n'
ROOT="$(new_project async)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
out="$(AUTOBUILD_ARGS=--async FAKE_GO_MARK=new run_tool "$ROOT")"
[[ "$out" == "old" ]] || fail "--async で初回から新版が動いた (旧版で即起動していない: $out)"
ok "stale でも旧版で即 exec する (ビルド待ちを見せない)"

wait_for "バックグラウンドビルドが完了しない" \
  binary_is "$ROOT" new
out="$(AUTOBUILD_ARGS=--async FAKE_GO_MARK=newer run_tool "$ROOT")"
[[ "$out" == "new" ]] || fail "バックグラウンドビルドが次回起動に反映されない (got: $out)"
ok "バックグラウンドビルドの結果が次回起動で反映される"

printf '\n## --async: 裏でビルド中であることを env で下流へ伝える\n'
# 旧バイナリで exec するため、起動したツールからは新版の完成もビルド失敗も観測できない。
# GO_AUTOBUILD_PENDING はその「裏で走っている」ことを伝える唯一の手がかりで、glogx が
# これを見て決着をトースト通知する (src/glogx/autobuild.go)。
ROOT="$(new_project asyncenv)"
# exec される側 (= 既存バイナリ) が env を読めるよう、実行時展開の probe を焼き込む
# (偽 go は mark をそのまま `echo <mark>` として書くので、単引用符で展開を遅らせる)
PROBE='pending=${GO_AUTOBUILD_PENDING-}'
FAKE_GO_MARK="$PROBE" run_tool "$ROOT" >/dev/null
freeze "$ROOT"
out="$(AUTOBUILD_ARGS=--async run_tool "$ROOT")"
[[ "$out" == "pending=" ]] || fail "再ビルド不要な起動で GO_AUTOBUILD_PENDING が立っている (got: $out)"
ok "再ビルド不要な起動では GO_AUTOBUILD_PENDING を立てない"

bump "$ROOT/src/tool/main.go"
out="$(AUTOBUILD_ARGS=--async FAKE_GO_MARK="$PROBE" run_tool "$ROOT")"
[[ "$out" == "pending=1" ]] || fail "async 再ビルド時に GO_AUTOBUILD_PENDING が立たない (got: $out)"
ok "async 再ビルドを spawn した起動では GO_AUTOBUILD_PENDING=1 を渡す"

printf '\n## 古い失敗記録は無視して再挑戦する (一時的な失敗で永久に止まらない)\n'
# ⚠️ backoff の条件は「ソースが失敗記録より新しいか」だが、失敗記録は pull の後に書かれるので
# その条件は二度と成立しない = 一度の一時的な失敗 (toolchain 取得の失敗等) で再ビルドが永久に
# 止まる。実証 (2026-07-31): pull 相当の状態で 1 回失敗させると、要因を解消した後の 3 回の起動で
# go build が 0 回しか呼ばれず古いバイナリに固定された。TTL で救済する。
ROOT="$(new_project failttl)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_FAIL=1 run_tool "$ROOT" >/dev/null
wait_for "失敗記録が作られない" test -f "$ROOT/src/tool/.autobuild.failed"
before="$(calls "$ROOT")"
# TTL 内 (失敗記録は今) はソース未変更でも再挑戦しない = 従来の backoff
AUTOBUILD_ARGS=--async FAKE_GO_MARK=fixed run_tool "$ROOT" >/dev/null
sleep 0.5
[[ "$(calls "$ROOT")" == "$before" ]] || fail "TTL 内なのに再挑戦した (落ちるビルドを撒く)"
ok "TTL 内はソース未変更で再挑戦しない"
# 失敗記録を TTL より古くすると、ソース未変更でも再挑戦する
mtime_at 30 "$ROOT/src/tool/.autobuild.failed"
AUTOBUILD_ARGS=--async GO_AUTOBUILD_FAILED_TTL=60 FAKE_GO_MARK=fixed run_tool "$ROOT" >/dev/null
wait_for "TTL 超過後も再挑戦しない (一時的な失敗から復帰できない)" \
  binary_is "$ROOT" fixed
ok "TTL を超えた失敗記録は無視して再挑戦する"

printf '\n## backoff 中の失敗の残り方 (ツール側が stale を判定できる形)\n'
# 失敗が残っていることは env で渡さない: ツール側は「.autobuild.failed が自バイナリより新しいか」で
# 判定する (glogx の autobuildStaleBinary)。ここで固定するのは shim 側の 2 つの前提 —
# 記録が残り続けること・再挑戦していないのに「ビルド中」を名乗らないこと。
ROOT="$(new_project failedstamp)"
PROBE2='pending=${GO_AUTOBUILD_PENDING-}'
FAKE_GO_MARK="$PROBE2" run_tool "$ROOT" >/dev/null   # 初回の同期ビルドで probe を焼き込む
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_FAIL=1 run_tool "$ROOT" >/dev/null
wait_for "失敗記録が作られない" test -f "$ROOT/src/tool/.autobuild.failed"
out="$(AUTOBUILD_ARGS=--async run_tool "$ROOT")"
[[ "$out" == "pending=" ]] || fail "再挑戦していないのに PENDING が立った (got: $out)"
ok "再挑戦しない間は「ビルド中」を名乗らない"
[[ "$ROOT/src/tool/.autobuild.failed" -nt "$ROOT/src/tool/tool" ]] \
  || fail "失敗記録が自バイナリより新しくない (ツール側が stale を判定できない)"
ok "失敗記録が残り、自バイナリより新しい (stale の判定材料になる)"
grep -q "build failed (exit " "$ROOT/src/tool/.autobuild.log" \
  || fail "ビルド失敗の exit code がログに残らない (シグナル死の原因を追えない)"
ok "ビルド失敗は exit code つきでログに残る"

printf '\n## --async のビルド失敗 (fail-open + backoff)\n'
ROOT="$(new_project asyncfail)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
out="$(AUTOBUILD_ARGS=--async FAKE_GO_FAIL=1 run_tool "$ROOT")"
[[ "$out" == "old" ]] || fail "async ビルド失敗で起動が壊れた (fail-open していない: $out)"
ok "async ビルド失敗でも旧版で動き続ける (fail-open)"

wait_for "失敗記録 (.autobuild.failed) が作られない" \
  test -f "$ROOT/src/tool/.autobuild.failed"
binary_is "$ROOT" old || fail "async ビルド失敗で既存バイナリが壊れた"
ok "ビルド失敗で既存バイナリは壊れない (一時ファイル → rename)"

before="$(calls "$ROOT")"
for _ in 1 2 3; do AUTOBUILD_ARGS=--async FAKE_GO_FAIL=1 run_tool "$ROOT" >/dev/null; done
sleep 0.5
[[ "$(calls "$ROOT")" == "$before" ]] || fail "失敗後もソース未変更で再ビルドを撒いている (起動ごとの CPU リーク)"
ok "失敗後はソースが変わるまで再挑戦しない (backoff)"

mtime_at 30 "$ROOT/src/tool/.autobuild.failed"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_MARK=fixed run_tool "$ROOT" >/dev/null
wait_for "ソース更新後も backoff が解除されない (再挑戦しない)" \
  binary_is "$ROOT" fixed
[[ -f "$ROOT/src/tool/.autobuild.failed" ]] && fail "成功後も失敗記録が残っている"
ok "ソースが更新されれば再挑戦し、成功で失敗記録が消える"

printf '\n## lock\n'
ROOT="$(new_project lock)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
# 生きている builder の lock は尊重する (自分の pid を持ち主に据える = 確実に生存)
mkdir -p "$ROOT/src/tool/.autobuild.lock"
printf '%s\n' "$$" > "$ROOT/src/tool/.autobuild.lock/pid"
before="$(calls "$ROOT")"
AUTOBUILD_ARGS=--async run_tool "$ROOT" >/dev/null
sleep 0.5
[[ "$(calls "$ROOT")" == "$before" ]] || fail "生きている builder がいるのに二重ビルドした"
ok "生存中の builder の lock を尊重する (多重ビルドしない)"

# 死んだ持ち主の lock は奪う。これが効かないと旧版に永久固定される
rm -rf "$ROOT/src/tool/.autobuild.lock"
mkdir -p "$ROOT/src/tool/.autobuild.lock"
dead_pid="$(bash -c 'echo $$')"  # 即終了した子の pid = 生存していない
printf '%s\n' "$dead_pid" > "$ROOT/src/tool/.autobuild.lock/pid"
AUTOBUILD_ARGS=--async FAKE_GO_MARK=taken run_tool "$ROOT" >/dev/null
wait_for "死んだ builder の lock を奪えず旧版に固定された" \
  binary_is "$ROOT" taken
ok "死んだ builder の lock を奪って再ビルドする"

# pid が生きて見えても timeout 超過の lock は奪う (pid 再利用での永久固定の救済)
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
rm -rf "$ROOT/src/tool/.autobuild.lock"
mkdir -p "$ROOT/src/tool/.autobuild.lock"
printf '%s\n' "$$" > "$ROOT/src/tool/.autobuild.lock/pid"
mtime_at 120 "$ROOT/src/tool/.autobuild.lock"
AUTOBUILD_ARGS=--async FAKE_GO_MARK=stolen GO_AUTOBUILD_LOCK_TIMEOUT=60 run_tool "$ROOT" >/dev/null
wait_for "timeout 超過の lock を奪えない (pid 再利用時に永久固定される)" \
  binary_is "$ROOT" stolen
ok "timeout を超えた lock は持ち主が生きて見えても奪う"

# age が取れない環境 (zstat 不在) で「不明なら奪う」にすると生存 builder の lock を毎回
# 奪って多重ビルドになる。age 判定を潰して pid 判定に落ちることを pin する。
if zsh -c 'source "'"$LIB"'"
  _go_autobuild_age() { REPLY=; return 1 }
  d=$(mktemp -d); mkdir "$d/.autobuild.lock"; print -r -- $$ > "$d/.autobuild.lock/pid"
  _go_autobuild_take_lock "$d/.autobuild.lock" 99999; rc=$?; rm -rf $d; exit $rc' 2>/dev/null; then
  fail "age 不明 (zstat 不在) のとき生存 builder の lock を奪った (多重ビルドになる)"
fi
ok "age が取れない環境でも生存 builder の lock は pid 判定で尊重する"

printf '\n## ビルド中に lock を奪われたら install しない\n'
# 奪った側は自分より新しいソースでビルドしている。古い成果物を後から被せると
# 「バイナリ mtime = 今」で stale 判定を欺き、黙って古い版に固定される。
ROOT="$(new_project stolen_mid)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_SLEEP=2 FAKE_GO_MARK=slow run_tool "$ROOT" >/dev/null
wait_for "builder が lock を取らない" test -f "$ROOT/src/tool/.autobuild.lock/pid"
printf '%s\n' "$$" > "$ROOT/src/tool/.autobuild.lock/pid"  # 別の builder に奪われた状態にする
sleep 3
binary_is "$ROOT" old || fail "lock を奪われた builder が古い成果物を install した (got: $(binary_mark "$ROOT"))"
[[ -f "$ROOT/src/tool/.autobuild.failed" ]] && fail "install 中止を失敗として記録した (次回の再挑戦が止まる)"
ok "lock を奪われた builder は install を中止し、失敗記録も残さない"
# 奪った側 (このテスト自身) が持ち主として残るので自分で片付ける。builder は他人の lock を
# 消さない (「lock を解放してよいのは持ち主だけ」) ため、放っておくと末尾の残骸チェックに出る。
rm -rf "$ROOT/src/tool/.autobuild.lock"

printf '\n## GO_AUTOBUILD_SYNC は --async を打ち消す\n'
ROOT="$(new_project syncenv)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
out="$(AUTOBUILD_ARGS=--async GO_AUTOBUILD_SYNC=1 FAKE_GO_MARK=now run_tool "$ROOT")"
[[ "$out" == "now" ]] || fail "GO_AUTOBUILD_SYNC=1 でも旧版が動いた (got: $out)"
ok "GO_AUTOBUILD_SYNC=1 なら同じ起動で新版が動く"

printf '\n## toolchain 警告は手元 go が go.mod より古いときだけ\n'
# 前置き一致で判定すると go1.25.4 vs go.mod 1.25.0 (手元が新しい) でも警告が出る。
# 実際に出ていた誤発火の回帰ガード。
ROOT="$(new_project gover)"
FAKE_GO_VERSION=go1.99.4 run_tool "$ROOT" >/dev/null
grep -q 'toolchain' "$ROOT/stderr" && fail "手元 go が go.mod より新しいのに toolchain 警告が出た"
ok "手元 go が要求より新しければ警告しない (パッチ版の誤発火防止)"

ROOT="$(new_project gover_old)"
FAKE_GO_VERSION=go1.98.0 run_tool "$ROOT" >/dev/null
grep -q 'toolchain' "$ROOT/stderr" || fail "手元 go が go.mod より古いのに toolchain 警告が出ない"
ok "手元 go が要求より古ければ警告する (popup 内のフリーズ見えを説明する)"

printf '\n## zsh/system を持たない zsh でも動く\n'
# ${sysparams[pid]:-$$} と書くと、zsh は未定義の連想配列への添字参照を set -u で fatal に
# するためフォールバックが効かず、ラッパーが起動しなくなる。zmodload 行を落とした複製で
# その経路を決定的に踏む。
NOSYS_LIB="$TMP_DIR/nosys_lib.zsh"
# zmodload を失敗させて else 側 ($$ への縮退) を強制する
sed 's|^if zmodload zsh/system 2>/dev/null; then|if false; then|' "$LIB" > "$NOSYS_LIB"
grep -q 'if false; then' "$NOSYS_LIB" || fail "zsh/system 取得の分岐を潰せていない (テストが経路を踏めていない)"
ROOT="$(new_project nosys)"
cp "$NOSYS_LIB" "$ROOT/bin/lib/go_autobuild.zsh"
out="$(FAKE_GO_MARK=v1 run_tool "$ROOT")"
[[ "$out" == "v1" ]] || fail "zsh/system 不在で初回ビルドが動かない (got: $out / stderr: $(tail -1 "$ROOT/stderr"))"
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
out="$(AUTOBUILD_ARGS=--async FAKE_GO_MARK=v2 run_tool "$ROOT")"
[[ "$out" == "v1" ]] || fail "zsh/system 不在で --async が旧版起動にならない (got: $out)"
wait_for "zsh/system 不在でバックグラウンドビルドが完了しない" binary_is "$ROOT" v2
ok 'zsh/system が無い zsh でも同期・async ともに動く ($$ への縮退)' 

printf '\n## ビルド中に着地したソース更新を飲まない\n'
# ⚠️ 成果物の mtime を install した時刻にすると、「ビルド中に入った編集」が永久に取り込まれない:
# 編集の mtime < install の mtime になるので stale 判定が二度と成立しない。glogx を触りながら
# popup で起動する = この repo の開発ループそのもので踏む。
ROOT="$(new_project midedit)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_SLEEP=2 FAKE_GO_MARK=mid run_tool "$ROOT" >/dev/null
wait_for "builder が動き出さない" test -f "$ROOT/src/tool/.autobuild.lock/pid"
sleep 0.3
touch "$ROOT/src/tool/main.go"   # ← ビルド実行中に着地した編集
wait_for "ビルドが完了しない" binary_is "$ROOT" mid
AUTOBUILD_ARGS=--async FAKE_GO_MARK=after run_tool "$ROOT" >/dev/null
wait_for "ビルド中の編集が拾われない (以後まったく再ビルドされない)" \
  binary_is "$ROOT" after
ok "ビルド中に着地したソース更新を次の起動で拾う"

printf '\n## 走行中の builder は、あとから入った新しいバイナリを踏まない\n'
# ⚠️ 同期ビルド (GO_AUTOBUILD_SYNC=1 =「今すぐ新版が欲しい」) は lock を取らないので、走行中の
# async builder は「lock を奪われた」判定に引っかからない。時刻で見ないと、あとから入った
# 新しいバイナリを古い成果物で踏み、ユーザーの復旧操作を黙って巻き戻す。
ROOT="$(new_project revert)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_SLEEP=3 FAKE_GO_MARK=slow run_tool "$ROOT" >/dev/null
wait_for "builder が動き出さない" test -f "$ROOT/src/tool/.autobuild.lock/pid"
sleep 0.5
GO_AUTOBUILD_SYNC=1 FAKE_GO_MARK=now run_tool "$ROOT" >/dev/null
binary_is "$ROOT" now || fail "前提が崩れた: 同期ビルドが入っていない (got: $(binary_mark "$ROOT"))"
sleep 4
binary_is "$ROOT" now \
  || fail "走行中の builder が新しいバイナリを古い成果物で踏んだ (got: $(binary_mark "$ROOT"))"
ok "走行中の builder は、あとから入った新しいバイナリを踏まない"

printf '\n## lock を解放してよいのは持ち主だけ\n'
# ⚠️ EXIT trap が所有権を見ずに rm -rf すると、timeout 超過で lock を奪われた builder が
# 「奪った側の lock」まで消す。以後その src は無施錠になり多重ビルドが漏れる。
ROOT="$(new_project lockown)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
AUTOBUILD_ARGS=--async FAKE_GO_SLEEP=2 FAKE_GO_MARK=slow run_tool "$ROOT" >/dev/null
wait_for "builder が lock を取らない" test -f "$ROOT/src/tool/.autobuild.lock/pid"
printf '%s\n' "$$" > "$ROOT/src/tool/.autobuild.lock/pid"  # 別の builder に奪われた状態にする
sleep 3
[[ -f "$ROOT/src/tool/.autobuild.lock/pid" ]] \
  || fail "lock を奪われた builder が、奪った側の lock まで消した (以後この src は無施錠)"
ok "lock を奪われた builder は、奪った側の lock を消さない"
rm -rf "$ROOT/src/tool/.autobuild.lock"  # 奪った状態を残さない (末尾の残骸チェック用)

printf '\n## 走行中の builder の作業ファイルを、あとから来た builder が消さない\n'
# ⚠️ 同期ビルド (GO_AUTOBUILD_SYNC=1 / バイナリ不在の初回) は lock を取らないので、spawn が
# lock 取得直後に走らせる .autobuild.new.* の掃除と直列化されない。掃除が走行中の目印を消すと:
#   - install ガード [[ "$bin" -nt "$started" ]] は zsh の -nt が「参照先不在 = FALSE」に倒れて素通り
#   - touch -r "$started" も黙って失敗し、成果物の mtime が install 時刻のまま残る
# = 2 つの修正が同時に、ログにも痕跡を残さず OFF になる。glogx の stale トーストは復旧手順として
# GO_AUTOBUILD_SYNC=1 を案内するので「案内どおり同期ビルド中に popup を開く」だけで踏む。
ROOT="$(new_project sweep)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
( GO_AUTOBUILD_SYNC=1 FAKE_GO_SLEEP=4 FAKE_GO_MARK=sync run_tool "$ROOT" >/dev/null 2>&1 ) &
SYNC_PID=$!
# ⚠️ wait_for に渡すのは関数にする。`test -n "$(...)"` だと置換が呼び出し時に 1 度だけ展開され、
# 同じ結果を 10 秒ポーリングし続けて「観測しているつもり」になる。
has_workfile() { [[ -n "$(find "$1/src/tool" -name '.autobuild.new.*' -print -quit)" ]]; }
wait_for "同期ビルドが始まらない" has_workfile "$ROOT"
marker="$(find "$ROOT/src/tool" -name '.autobuild.new.*' -print -quit)"
AUTOBUILD_ARGS=--async FAKE_GO_MARK=other run_tool "$ROOT" >/dev/null   # ← spawn の掃除が走る
sleep 0.6
[[ -e "$marker" ]] \
  || fail "走行中の builder の作業ファイルを、あとから来た builder が消した: ${marker##*/}"
ok "走行中の builder の作業ファイルを、あとから来た builder が消さない"
wait "$SYNC_PID" 2>/dev/null || true

printf '\n## 相対パスで呼ばれても一時ファイルの置き場所がずれない\n'
# ⚠️ -C はビルド前に chdir するので、src_dir が相対だと -o の出力先が「移動後の cwd 基準」に
# なり、意図しない場所へ書く。_go_autobuild_build の ${1:a} 正規化がそれを潰している。
# 現在の呼び出しは全て ${0:A:h} 由来で絶対だが、-C を使う限りこれは前提であって偶然ではない。
ROOT="$(new_project relpath)"
FAKE_GO_MARK=v1 run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
( cd "$ROOT/bin" && PATH="$TMP_DIR/bin:$PATH" FAKE_GO_CALLS="$ROOT/calls" FAKE_GO_MARK=rel \
    zsh -c 'source ./lib/go_autobuild.zsh; _go_autobuild_build ../src/tool tool 1' >/dev/null 2>&1 )
binary_is "$ROOT" rel || fail "相対 src_dir で呼ぶと成果物が入らない (got: $(binary_mark "$ROOT"))"
stray="$(find "$ROOT/bin" -name '.autobuild.new.*' | head -3)"
[[ -z "$stray" ]] || fail "相対 src_dir で一時ファイルを cwd 側へ書いた: $stray"
head -1 "$ROOT/bin/tool" | grep -q zsh || fail "相対 src_dir で cwd 側の別ファイル (ラッパー) を成果物で上書きした"
ok "相対 src_dir でも成果物と一時ファイルが src 側に置かれる"

printf '\n## spawn したビルドは popup を閉じたときの HUP で死なない\n'
# ⚠️ 実害 (2026-08-01): tmux popup を閉じると process group へ HUP が飛び、裏で走っている
# go build が exit 129 (=128+SIGHUP) で死ぬ。失敗記録が残るので以後は TTL が切れるまで旧版に
# 固定され、「古い版で動いています」が出続けてビルドされない状態になった。
#
# _go_autobuild_spawn は `trap '' HUP TERM INT` を張っているが、_go_autobuild_build が内側で
# サブシェルを掘るとそこで ignore が失われる (zsh は fork したサブシェルで trap を既定へ戻す)。
# ここで pin するのは「ignore が実際にビルドプロセスまで届いていること」。
ROOT="$(new_project hup)"
FAKE_GO_MARK=old run_tool "$ROOT" >/dev/null
freeze "$ROOT"
bump "$ROOT/src/tool/main.go"
PIDFILE="$ROOT/go.pid"
AUTOBUILD_ARGS=--async FAKE_GO_SLEEP=3 FAKE_GO_MARK=survived FAKE_GO_PIDFILE="$PIDFILE" \
  run_tool "$ROOT" >/dev/null
wait_for "ビルドプロセスが起動しない" test -s "$PIDFILE"
kill -HUP "$(cat "$PIDFILE")" 2>/dev/null || fail "ビルドプロセスへ HUP を撃てない (既に死んでいる?)"
wait_for "HUP でビルドが死んだ (popup を閉じるたびに再ビルドが失敗する)" \
  binary_is "$ROOT" survived
ok "走行中のビルドは HUP を無視して完走する"
[[ -f "$ROOT/src/tool/.autobuild.failed" ]] && fail "HUP 死の失敗記録が残っている (TTL まで旧版に固定される)"
ok "HUP で失敗記録を残さない"

printf '\n## 作業ファイル / lock を残さない\n'
# ⚠️ 「いずれ消える」で判定する。builder は非同期なので、バイナリが入った瞬間にはまだ lock の
# 解放 (spawn subshell の EXIT trap) が済んでいないことがある。即時判定にすると spawn を使う
# テストの直後で偽陽性になる: 手元では 0ms で解放されるが CI の遅い runner で落ちた (2026-08-01)。
# 「残さない」の意味は「builder が終われば消える」であって「バイナリと同時に消える」ではない。
leftovers() { find "$TMP_DIR" \( -name '.autobuild.new.*' -o -name 'nohup.out' -o -name '.autobuild.lock' \) | head -5; }
for ((i = 0; i < 100; i++)); do
  leftover="$(leftovers)"
  [[ -z "$leftover" ]] && break
  sleep 0.1
done
[[ -z "$leftover" ]] || fail "10 秒経っても作業ファイル / lock が残っている: $leftover"
ok "rename 前の一時ファイル・nohup.out・lock を残さない (builder 終了後に解放される)"

printf '\nAll go_autobuild tests passed successfully!\n'
