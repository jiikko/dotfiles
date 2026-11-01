#!/usr/bin/env bash
# scripts/go_module_graph.sh (go.mod が tidy の形で、module グラフ全体が解決できるか) のテスト。
#
# fixture は実際に落ちた形を写す: a → b (replace あり) → c (b の go.mod だけが replace を持つ)。a が import するのは
# c に届かない b の package だけなので、go mod tidy -diff と go build は通り、go list -m all だけが落ちる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/go_module_graph.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
ok() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

if ! command -v go >/dev/null 2>&1; then
  echo "skipped: go が無い (scripts/go_module_graph.sh は go を呼ぶ)"
  exit 77
fi
# fixture は replace だけで閉じる (proxy へ出ない)。GOWORK / GOFLAGS は落とさない: script が自分で固定することを下の 5・6 で見る
export GOPROXY=off GOTOOLCHAIN=local

# fixture を作る。$1 = a の go.mod に足す行
mk() {
  local d="$TMP_DIR/$1"
  rm -rf "$d"; mkdir -p "$d/a" "$d/b/lan" "$d/c" "$d/d"
  printf 'module c\n\ngo 1.24\n' > "$d/c/go.mod"
  printf 'package c\n\nconst X = 1\n' > "$d/c/c.go"
  printf 'module d\n\ngo 1.24\n' > "$d/d/go.mod"
  printf 'package d\n\nconst W = 4\n' > "$d/d/d.go"
  printf 'module b\n\ngo 1.24\n\nrequire c v0.0.0\n\nreplace c => ../c\n' > "$d/b/go.mod"
  printf 'package b\n\nconst Y = 2\n' > "$d/b/b.go"
  printf 'package lan\n\nimport "c"\n\nconst Z = c.X\n' > "$d/b/lan/lan.go"
  printf 'module a\n\ngo 1.24\n\nrequire b v0.0.0\n\nreplace b => ../b\n%s' "$2" > "$d/a/go.mod"
  printf 'package main\n\nimport "b"\n\nfunc main() { _ = b.Y }\n' > "$d/a/main.go"
  printf '%s/a' "$d"
}

run() {  # $1 = dir。rc / out を残す
  rc=0
  out="$(cd "$1" && "$SCRIPT" 2>&1)" || rc=$?
}

# 1. 依存先が require する module の replace が無い → go list -m all で落ち、足す replace と paths を案内する
dir="$(mk missing '')"
# 前提: この fixture は go build が通る (落ちるのが build なら、この検査が要る理由の再現になっていない)
# 前提の確かめ (素の go コマンド) は親の GOFLAGS / GOWORK に左右させない。script の呼び出しは親の環境のまま (script 側の固定を見る)
if (cd "$dir" && GOWORK=off GOFLAGS='' go build ./... >/dev/null 2>&1); then ok "前提: replace の抜けがあっても go build は通る"; else bad "前提が崩れた: replace の抜けで go build が落ちる"; fi
run "$dir"
if [ "$rc" -eq 1 ] && grep -q 'go list -m all が rc=1' <<<"$out" && grep -q '「c => ../c」' <<<"$out" && grep -q '「src/c/\*\*」' <<<"$out"; then
  ok "replace の抜けを go list -m all で止め、足す replace と workflow の paths を案内する"
else
  bad "replace の抜けを止めない / 案内が違う (rc=${rc}):"; printf '%s\n' "$out"
fi

# 2. replace を足せば通る
run "$(mk fixed $'\nreplace c => ../c\n')"
if [ "$rc" -eq 0 ]; then ok "replace が揃っていれば通る"; else bad "replace が揃っているのに落ちた (rc=${rc}):"; printf '%s\n' "$out"; fi

# 3. tidy の形でない (使わない module を require している) → go mod tidy -diff で落ちる
run "$(mk untidy $'\nreplace c => ../c\n\nrequire d v0.0.0\n\nreplace d => ../d\n')"
if [ "$rc" -eq 1 ] && grep -q 'go mod tidy -diff が rc=' <<<"$out"; then ok "tidy の形でない go.mod を止める"; else bad "tidy の形でない go.mod を通した (rc=${rc}):"; printf '%s\n' "$out"; fi

# 4. go.mod の無い場所で呼ばれたら 2 (呼び出し方の誤り。0 で通さない)
mkdir -p "$TMP_DIR/nomod"
run "$TMP_DIR/nomod"
if [ "$rc" -eq 2 ]; then ok "go.mod の無い場所では rc=2"; else bad "go.mod の無い場所で rc=${rc}"; fi

# 5. 上に go.work があっても素通りしない (workspace の module が replace の抜けを埋める。2026-10-16 に再現)
dir="$(mk work '')"
printf 'go 1.24\n\nuse (\n\t./a\n\t./b\n\t./c\n)\n' > "$(dirname "$dir")/go.work"
if (cd "$dir" && GOWORK='' GOFLAGS='' go list -m all >/dev/null 2>&1); then ok "前提: go.work があると素の go list -m all は通る"; else bad "前提が崩れた: go.work があっても go list -m all が落ちる"; fi
rc=0; out="$(cd "$dir" && GOWORK='' "$SCRIPT" 2>&1)" || rc=$?
if [ "$rc" -eq 1 ] && grep -q '「c => ../c」' <<<"$out"; then ok "go.work があっても replace の抜けを止める"; else bad "go.work で素通りした (rc=${rc}):"; printf '%s\n' "$out"; fi

# 6. 手元の GOFLAGS=-mod=vendor で筋違いの red にしない (script が -mod=readonly に固定する)
rc=0; out="$(cd "$(mk vendor $'\nreplace c => ../c\n')" && GOFLAGS=-mod=vendor "$SCRIPT" 2>&1)" || rc=$?
if [ "$rc" -eq 0 ]; then ok "GOFLAGS=-mod=vendor の手元でも揃った go.mod は通る"; else bad "GOFLAGS=-mod=vendor で落ちた (rc=${rc}):"; printf '%s\n' "$out"; fi

# 7〜15. 外から import できる module (名前の最初の要素にドット) が、ディレクトリへの replace で解決している依存を、取り込む側
# (replace を見ない) が取得できるか。chromecookie → testwait (ドット無し + v0.0.0) で取り込む 3 repo の go mod tidy だけが落ちた。
# fixture は git の repo (prefix example.com/repo): lib → dep (replace ../dep)。版は dep を commit した時点の pseudo-version。
# hook から起動されたときに継承した GIT_DIR などで fixture の git が本物の repo を指さないようにする
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
gitc() {  # $1 = repo, $2 = committer の時刻 (UTC), 残り = commit message
  # author の時刻は別の値にする (go の pseudo-version は committer の時刻。同じ値だと author を読む退行が見えない)
  local r="$1" t="$2"; shift 2
  git -C "$r" add -A >/dev/null
  GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com \
    GIT_AUTHOR_DATE="2001-02-03 04:05:06 +0900" GIT_COMMITTER_DATE="$t +0000" git -C "$r" commit -q -m "$*"
}
pseudo_of() {  # $1 = repo, $2 = rev → v0.0.0-<committer の UTC 時刻>-<hash 12>。検査と別の式 (epoch → date -u) で作る
  local epoch; epoch="$(git -C "$1" log -1 --format=%ct "$2")"
  printf 'v0.0.0-%s-%s' "$(date -u -r "$epoch" +%Y%m%d%H%M%S)" "$(git -C "$1" rev-parse --short=12 "$2")"
}
mkrepo() {  # $1 = 名前, $2 = dep の module 名。dep を commit した repo を作って root を出す
  local d="$TMP_DIR/$1"
  rm -rf "$d"; mkdir -p "$d/lib" "$d/dep" "$d/leaf"
  git -C "$d" init -q -b main
  printf 'module %s\n\ngo 1.24\n' "$2" > "$d/dep/go.mod"
  printf 'package dep\n\nconst D = 1\n' > "$d/dep/dep.go"
  printf 'module leaf\n\ngo 1.24\n' > "$d/leaf/go.mod"
  printf 'package leaf\n\nconst L = 1\n' > "$d/leaf/leaf.go"
  gitc "$d" '2026-01-02 03:04:05' dep
  printf '%s' "$d"
}
pinlib() {  # $1 = repo, $2 = dep の名前, $3 = 版, $4 = lib の go.mod に足す行。lib を書いて commit する
  printf 'module example.com/repo/lib\n\ngo 1.24\n\nrequire %s %s\n\nreplace %s => ../dep\n%s' "$2" "$3" "$2" "$4" > "$1/lib/go.mod"
  printf 'package lib\n\nimport "%s"\n\nconst X = dep.D\n' "$2" > "$1/lib/lib.go"
  gitc "$1" '2026-01-03 00:00:00' lib
}
expect_bad() {  # $1 = 説明, $2 = 出力に含まれるべき理由
  if [ "$rc" -eq 1 ] && grep -qF "$2" <<<"$out"; then ok "$1"; else bad "$1 (rc=${rc}):"; printf '%s\n' "$out"; fi
}

# 7. 名前にドットが無い (testwait の形)
r="$(mkrepo dotless dep)"; pinlib "$r" dep "$(pseudo_of "$r" HEAD)" ''
run "$r/lib"; expect_bad "ドット無しの依存を止める" "名前が取り込む側から取得できる example.com/repo/dep でない"
grep -q 'go get example.com/repo/dep@' <<<"$out" || bad "直し方に取得できる名前を出していない"

# 8. 版が v0.0.0 (他の内部 module と同じ書き方)
r="$(mkrepo v000 example.com/repo/dep)"; pinlib "$r" example.com/repo/dep v0.0.0 ''
run "$r/lib"; expect_bad "v0.0.0 の依存を止める" "example.com/repo/dep v0.0.0 を取り込む側が取得できない (版が pseudo-version"

# 9. ゼロ埋めの pseudo-version (replace だけがあるときに go が書く既定の版)
r="$(mkrepo zero example.com/repo/dep)"; pinlib "$r" example.com/repo/dep v0.0.0-00010101000000-000000000000 ''
run "$r/lib"; expect_bad "ゼロ埋めの版を止める" "版の commit 000000000000 がこの repo に無い"

# 10. 取得できる形 (名前・版・commit・時刻・その時点の module 名が揃う) は通る
r="$(mkrepo good example.com/repo/dep)"; pinlib "$r" example.com/repo/dep "$(pseudo_of "$r" HEAD)" ''
run "$r/lib"
if [ "$rc" -eq 0 ]; then ok "取得できる形の依存は通す"; else bad "取得できる形で落ちた (rc=${rc}):"; printf '%s\n' "$out"; fi
good="$r"

# 11. 固定した commit の時点では dep がドット無しの名前 (作業ツリーだけ直っている)
r="$(mkrepo stale dep)"; old="$(pseudo_of "$r" HEAD)"
printf 'module example.com/repo/dep\n\ngo 1.24\n' > "$r/dep/go.mod"; gitc "$r" '2026-01-02 05:00:00' rename
pinlib "$r" example.com/repo/dep "$old" ''
run "$r/lib"; expect_bad "固定した commit と作業ツリーの食い違いを止める" "の時点の dep/go.mod の module 名が example.com/repo/dep でない"

# 12. 版の時刻が commit と一致しない
r="$(mkrepo badtime example.com/repo/dep)"; v="$(pseudo_of "$r" HEAD)"
pinlib "$r" example.com/repo/dep "v0.0.0-20990101000000-${v##*-}" ''
run "$r/lib"; expect_bad "時刻の合わない版を止める" "版の時刻 20990101000000 が commit"

# 13. HEAD の祖先でない commit (別の branch にだけある = push されない)
r="$(mkrepo side example.com/repo/dep)"
git -C "$r" checkout -q -b side; printf '// side\n' >> "$r/dep/dep.go"; gitc "$r" '2026-01-02 06:00:00' side
v="$(pseudo_of "$r" HEAD)"; git -C "$r" checkout -q main
pinlib "$r" example.com/repo/dep "$v" ''
run "$r/lib"; expect_bad "HEAD の祖先でない commit を止める" "が HEAD の祖先でない"

# 14. 間接の依存も見る: dep が require するドット無しの leaf を、lib の replace で解決している
r="$(mkrepo indirect example.com/repo/dep)"
printf 'module example.com/repo/dep\n\ngo 1.24\n\nrequire leaf v0.0.0\n' > "$r/dep/go.mod"; gitc "$r" '2026-01-02 07:00:00' leaf
pinlib "$r" example.com/repo/dep "$(pseudo_of "$r" HEAD)" $'\nreplace leaf => ../leaf\n'
run "$r/lib"; expect_bad "間接の依存のドット無しも止める" "leaf v0.0.0 を取り込む側が取得できない"

# 14b. 固定した commit の時点の dep の go.mod がドット無しの leaf を require (作業ツリーでは外した)。取り込む側はその commit を読む
r="$(mkrepo pinreq example.com/repo/dep)"
printf 'module example.com/repo/dep\n\ngo 1.24\n\nrequire leaf v0.0.0\n' > "$r/dep/go.mod"; gitc "$r" '2026-01-02 08:00:00' leaf
v="$(pseudo_of "$r" HEAD)"
printf 'module example.com/repo/dep\n\ngo 1.24\n' > "$r/dep/go.mod"; gitc "$r" '2026-01-02 09:00:00' unleaf
pinlib "$r" example.com/repo/dep "$v" ''
run "$r/lib"; expect_bad "固定した commit の go.mod の require も確かめる" "leaf v0.0.0 を取り込む側が取得できない (名前にドットが無い"

# 15. 浅い clone (CI の checkout の形) でも、履歴を取り直して 10 と同じく通る
shallow="$TMP_DIR/shallow"
git clone -q --depth 1 "file://$good" "$shallow"
[ "$(git -C "$shallow" rev-parse --is-shallow-repository)" = true ] || bad "前提: fixture の clone が浅くない"
run "$shallow/lib"
if [ "$rc" -eq 0 ] && [ "$(git -C "$shallow" rev-parse --is-shallow-repository)" = false ]; then
  ok "浅い clone でも履歴を取り直して確かめる"
else
  bad "浅い clone で確かめられない (rc=${rc}):"; printf '%s\n' "$out"
fi

# 16. 外から import できない module (名前にドットが無い) には当てない: 7 と同じ依存の形で、lib の名前だけドット無しにする
r="$(mkrepo internal dep)"; pinlib "$r" dep v0.0.0 ''
sed -i '' 's#^module example.com/repo/lib$#module lib#' "$r/lib/go.mod"
grep -q '^module lib$' "$r/lib/go.mod" || bad "前提: fixture の module 名を書き換えられていない"
run "$r/lib"
if [ "$rc" -eq 0 ]; then ok "外から import できない module には当てない"; else bad "内部の module まで止めた (rc=${rc}):"; printf '%s\n' "$out"; fi

# src/*/Makefile の lint が全部これを呼ぶことは scripts/check_go_project_lanes.sh (make test-lint) が止める

[ "$fail" -eq 0 ] || exit 1
echo "[test-go-module-graph] all ok"
