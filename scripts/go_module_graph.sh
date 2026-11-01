#!/usr/bin/env bash
# go_module_graph.sh — 今いるディレクトリの Go module について、go.mod / go.sum が `go mod tidy` の形で、module グラフ全体が
# 解決できる (`go list -m all` が通る) ことを確かめる。
#
# src/*/Makefile の lint が golangci-lint の前に呼ぶ。手元の `make test` (test-src → test-go-lint)・`make -C src/<proj> lint`・
# CI の Lint step (`_go-project.yml` の `make -C <dir> lint`) が同じ入口を通る。呼び忘れと workflow の paths の書き忘れは
# scripts/check_go_project_lanes.sh (make test-lint) が止める。
#
# なぜ要るか: replace は依存元へ伝わらない。依存先の module が別の module を新しく require すると、依存元の go.mod に
# その replace が無い限り `go list -m all` は `malformed module path "<名前>": missing dot in first path element` で落ちるが、
# go build / go test / golangci-lint は落ちない (依存元が import する package がその module に届かなければ)。実際に 2 回、
# CI だけが落ちて手元の make test は緑のまま push された。
set -uo pipefail
unset CDPATH
# 手元の環境で結果を変えない (CI と同じ条件で見る): go.work があると workspace の module が replace の抜けを埋めて素通りする。
# GOFLAGS=-mod=mod だと go list が go.sum を書き換え、-mod=vendor だと筋違いの red になる (どちらも 2026-10-16 に再現)
export GOWORK=off GOFLAGS=-mod=readonly

if [ ! -f go.mod ]; then
  echo "✗ go_module_graph: go.mod が無い ($(pwd))。src/<proj> の Makefile から呼ぶ" >&2
  exit 2
fi

out="$(go mod tidy -diff 2>&1)"
rc=$?
if [ "$rc" -ne 0 ]; then
  printf '%s\n' "$out" >&2
  echo "✗ go_module_graph: go mod tidy -diff が rc=${rc} ($(pwd))。差分なら go mod tidy をかける" >&2
  exit 1
fi

out="$(go list -m all 2>&1 >/dev/null)"
rc=$?
if [ "$rc" -ne 0 ]; then
  printf '%s\n' "$out" >&2
  echo "✗ go_module_graph: go list -m all が rc=${rc} ($(pwd))" >&2
  missing="$(printf '%s\n' "$out" | sed -n 's/.*malformed module path "\([^"]*\)".*/\1/p' | head -n 1)"
  if [ -n "$missing" ]; then
    # replace を足すと scripts/check_go_project_lanes.sh (make test-lint) が workflow の paths も要求するので、両方を案内する
    echo "  直し方: go.mod の replace に「${missing} => ../${missing}」を足し、.github/workflows/src_$(basename "$(pwd)").yml の" \
      "push.paths と pull_request.paths に「src/${missing}/**」を足す (依存先が require する module。replace は依存元へ伝わらない)" >&2
  fi
  exit 1
fi

# 外から import できる module (名前の最初の要素にドットがある。github.com/jiikko/dotfiles/src/chromecookie 等) は、
# 取り込む側の go mod tidy / go list -m all / go build が解決できる require だけを持つ。取り込む側はこの go.mod の
# replace を見ず、require の版をそのまま取りに行き、その版の commit の go.mod を読む。dotfiles の中では replace が埋めるので、
# 上の tidy -diff / go list -m all も build / test も緑のまま通る (chromecookie のテストが testwait を使ったとき、
# 取り込む 3 repo の go mod tidy だけが「malformed module path "testwait"」で落ちた)。
# そこで、ディレクトリへの replace で解決している module ごとに、取り込む側が取りに行くものをこの repo の git で確かめる:
#   1. 名前が <この repo の module の prefix>/<replace 先の dir> であること (ドット無し・取得できない名前を止める)
#   2. 版が v0.0.0-<UTC の時刻>-<hash 12 桁> の pseudo-version であること (この repo は tag を打たない)
#   3. その commit が実在し、HEAD の祖先であること (ゼロ埋めの版・push されない commit を止める)
#   4. commit の時刻 (committer。go の pseudo-version と同じ) が版の時刻と一致すること
#   5. その commit の時点の go.mod の module 名が require の名前と一致し、その go.mod の require も取得できること
#      (ドット無しは落とし、この repo の module は 2〜5 を辿る)。取り込む側は作業ツリーではなくその commit を読む
# 検出しないもの: 固定した commit にしか無い / 作業ツリーにしか無い API の差 (取り込む側の compile で落ちる)。
# 版の置換 (replace X => Y v1.2.3) は対象外 (この repo では使っていない)。
# repo の root に置いた module・名前が /vN で終わる module は、名前の照合 (prefix/<dir>) が合わずに落ちる (この repo には無い。
# 足すときはここの照合を広げる)。PR の merge commit (refs/pull/N/merge) の浅い checkout での --unshallow は試していない。
# CI の checkout は浅い (lint の job は履歴を取らない) ので、commit が無い / 祖先と言えないときだけ履歴を取り直す。
main_path="$(go list -m -f '{{.Path}}')" || { echo "✗ go_module_graph: main module の名前を取れない ($(pwd))" >&2; exit 1; }
[ -n "$main_path" ] || { echo "✗ go_module_graph: main module の名前が空 ($(pwd))" >&2; exit 1; }
case "${main_path%%/*}" in
  *.*)
    # 継承した GIT_DIR などで、cwd とは別の repo の履歴を読まない
    # shellcheck source=bin/lib/git_location_env.sh
    . "$(dirname "$(readlink -f "$0")")/../bin/lib/git_location_env.sh" ||
      { echo "✗ go_module_graph: bin/lib/git_location_env.sh を読めない" >&2; exit 1; }
    git_location_env_unset || { echo "✗ go_module_graph: 継承した git の場所の環境変数を外せない" >&2; exit 1; }
    rel_main="$(git rev-parse --show-prefix)" || { echo "✗ go_module_graph: git の中で呼ばれていない ($(pwd))" >&2; exit 1; }
    rel_main="${rel_main%/}"
    case "$main_path" in
      */"$rel_main") repo_prefix="${main_path%/"$rel_main"}" ;;
      *) echo "✗ go_module_graph: module 名 ${main_path} が repo の中の場所 ${rel_main:-(root)} で終わらない" >&2; exit 1 ;;
    esac
    tmpd="$(mktemp -d)"
    trap 'rm -rf "$tmpd"' EXIT
    replaced="$(go list -m -f '{{if .Replace}}{{if not .Replace.Version}}{{.Path}} {{.Version}} {{.Replace.Path}}{{end}}{{end}}' all 2>"$tmpd/err")"
    rc=$?
    if [ "$rc" -ne 0 ]; then
      cat "$tmpd/err" >&2
      echo "✗ go_module_graph: replace の一覧を取れない (go list -m rc=${rc}, $(pwd))" >&2
      exit 1
    fi

    unshallowed=0
    have_history() {  # $1 = hash。commit があり HEAD の祖先か (浅い clone なら 1 回だけ履歴を取り直してから答える)
      if git cat-file -e "$1^{commit}" 2>/dev/null && git merge-base --is-ancestor "$1" HEAD 2>/dev/null; then return 0; fi
      if [ "$unshallowed" -eq 0 ] && [ "$(git rev-parse --is-shallow-repository)" = true ]; then
        unshallowed=1
        git fetch --quiet --unshallow origin >&2 || echo "  (履歴を取り直せなかった: git fetch --unshallow)" >&2
        have_history "$1"; return
      fi
      return 1
    }
    # modfile_at: $1 = hash, $2 = repo の中の dir。その commit の go.mod を整形して「M <名前>」「R <名前> <版>」の行で出す
    modfile_at() {
      rm -rf "$tmpd/m"; mkdir -p "$tmpd/m"
      git show "$1:$2/go.mod" > "$tmpd/m/go.mod" 2>/dev/null || return 1
      (cd "$tmpd/m" && GOFLAGS='' go mod edit -fmt) || return 1
      awk '/^module /{print "M", $2} /^require \($/{b=1; next} b && /^\)/{b=0; next} b{print "R", $1, $2} /^require [^(]/{print "R", $2, $3}' "$tmpd/m/go.mod"
    }

    bad=0
    report() {  # $1 = 名前, $2 = 版, $3 = 理由, $4 = どこが require しているか
      echo "✗ go_module_graph: ${main_path} は外から import できるが、${1} ${2} を取り込む側が取得できない (${3}; ${4})。" \
        "取り込む側は replace を見ないので go mod tidy が落ちる" >&2
      case "$1" in
        "$repo_prefix"/*) echo "  直し方: ここでは replace を残したまま GOPROXY=direct go get ${1}@<その名前の go.mod を含む push 済みの commit>" \
          "で取得できる pseudo-version を require する" >&2 ;;
      esac
      bad=$((bad + 1))
    }
    # 辿る列: 「名前 版 どこが require しているか」。最初は replace で解決している module (1 を先に確かめる)
    queue=()
    while read -r path ver rdir; do
      [ -n "$path" ] || continue
      case "$rdir" in /*) adir="$rdir" ;; *) adir="$(pwd)/$rdir" ;; esac
      rel_dep="$( (cd "$adir" 2>/dev/null && git rev-parse --show-prefix) )"
      rel_dep="${rel_dep%/}"
      if [ -z "$rel_dep" ] || [ "$path" != "${repo_prefix}/${rel_dep}" ]; then
        report "$path" "$ver" "名前が取り込む側から取得できる ${repo_prefix}/${rel_dep:-<replace 先>} でない" "replace => ${rdir}"
        echo "  直し方: ${rel_dep:-<replace 先>} の module 名を ${repo_prefix}/${rel_dep:-<dir>} にし、ここでは replace を残したまま" \
          "GOPROXY=direct go get ${repo_prefix}/${rel_dep:-<dir>}@<その名前の go.mod を含む push 済みの commit> で pseudo-version を require する" >&2
        continue
      fi
      queue+=("$path $ver ${main_path}")
    done <<< "$replaced"

    seen=" "
    i=0
    while [ "$i" -lt "${#queue[@]}" ]; do
      read -r path ver from <<< "${queue[$i]}"
      i=$((i + 1))
      case "$seen" in *" $path@$ver "*) continue ;; esac
      seen="$seen$path@$ver "
      rel_dep="${path#"$repo_prefix"/}"
      where="${from} の go.mod が require"
      if ! [[ "$ver" =~ ^v0\.0\.0-([0-9]{14})-([0-9a-f]{12})$ ]]; then
        report "$path" "$ver" "版が pseudo-version (v0.0.0-<時刻>-<hash>) でない" "$where"; continue
      fi
      ts="${BASH_REMATCH[1]}" hash="${BASH_REMATCH[2]}"
      if ! git cat-file -e "$hash^{commit}" 2>/dev/null && ! have_history "$hash"; then
        report "$path" "$ver" "版の commit ${hash} がこの repo に無い" "$where"; continue
      fi
      if ! have_history "$hash"; then
        report "$path" "$ver" "版の commit ${hash} が HEAD の祖先でない (push されない / 取り込む側が取れない)" "$where"; continue
      fi
      if [ "$(TZ=UTC git show -s --format=%cd --date=format-local:%Y%m%d%H%M%S "$hash")" != "$ts" ]; then
        report "$path" "$ver" "版の時刻 ${ts} が commit ${hash} の時刻と一致しない" "$where"; continue
      fi
      if ! lines="$(modfile_at "$hash" "$rel_dep")"; then
        report "$path" "$ver" "commit ${hash} に ${rel_dep}/go.mod が無い / 読めない" "$where"; continue
      fi
      got="$(sed -n 's/^M //p' <<< "$lines")"
      if [ "$got" != "$path" ]; then
        report "$path" "$ver" "commit ${hash} の時点の ${rel_dep}/go.mod の module 名が ${path} でない (${got:-なし})" "$where"; continue
      fi
      while read -r tag rpath rver; do
        [ "$tag" = R ] || continue
        case "${rpath%%/*}" in
          *.*) ;;
          *) report "$rpath" "$rver" "名前にドットが無い" "${path}@${hash} の go.mod が require"; continue ;;
        esac
        case "$rpath" in "$repo_prefix"/*) queue+=("$rpath $rver ${path}@${hash}") ;; esac
      done <<< "$lines"
    done
    [ "$bad" -eq 0 ] || exit 1
    ;;
esac
