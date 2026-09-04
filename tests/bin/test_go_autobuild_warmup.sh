#!/usr/bin/env bash
# bin/lib/go_autobuild.zsh の温め (_go_autobuild_warm_signature) が前提にしている
# 「この機構を使う全ツールは未知フラグ --__autobuild_warmup__ で即終了する」を固定する。
#
# なぜ: 温めは install 直後に新バイナリを実際に exec する (署名検証キャッシュは exec でしか
# 温まらない)。未知フラグで終了しないツール (引数を無視して TUI やサーバとして待つ形) を
# 足すと、裏ビルドがそこで止まる。温めには timeout が無いので、この前提はここで機械的に守る。
# 対象一覧は bin/*.zsh ラッパーの go_autobuild_exec 呼び出しから取る (手で列挙すると新ツールが漏れる)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

if ! command -v go >/dev/null 2>&1; then
  echo "SKIP: go が無い環境"
  exit 77
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/autobuild-warmup.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# 秒数上限つきで 1 回起こす。戻り値: 0 = 上限内に終了 / 1 = 上限内に終わらず kill した。
# (`timeout` は coreutils 依存なので使わない。bash の & + kill で組む)
runs_within() { # $1=秒 $2...=コマンド
  local limit="$1"; shift
  "$@" >/dev/null 2>&1 </dev/null &
  local pid=$! waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( waited >= limit * 10 )); then
      kill -9 "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      return 1
    fi
    sleep 0.1; waited=$((waited + 1))
  done
  wait "$pid" 2>/dev/null || true
  return 0
}

# 自己検査: 判定器が「終わらないもの」を本当に検出するか (これが無いと全 ok が vacuous)
if runs_within 1 sleep 30; then
  fail "判定器の自己検査: sleep 30 を上限内終了と誤判定した"
else
  ok "判定器の自己検査: 終わらないコマンドを検出する"
fi

# 対象 = go_autobuild_exec を呼ぶラッパー。行の形: go_autobuild_exec [--async] [--pkg P] <src_dir> <name> -- "$@"
found=0
while IFS= read -r wrapper; do
  line="$(grep -E '^\s*go_autobuild_exec ' "$wrapper" | tail -1)" || continue
  [ -n "$line" ] || continue
  pkg=""; src=""; name=""
  set -- $line; shift # go_autobuild_exec
  while [ $# -gt 0 ]; do
    case "$1" in
      --async) shift ;;
      --pkg) pkg="$2"; shift 2 ;;
      --) break ;;
      *) if [ -z "$src" ]; then src="$1"; else name="$1"; fi; shift ;;
    esac
  done
  # ラッパー内の "${0:A:h}" は bin/ の絶対パス
  src="${src//\"\$\{0:A:h\}\"/$ROOT_DIR/bin}"; src="${src//\$\{0:A:h\}/$ROOT_DIR/bin}"
  src="${src%\"}"; src="${src#\"}"
  [ -d "$src" ] || { fail "$name: src dir が解決できない ($src)"; continue; }
  found=$((found + 1))
  out="$WORK/$name"
  if ! (cd "$src" && go build -o "$out" "./${pkg:-.}" >"$WORK/$name.build.log" 2>&1); then
    fail "$name: go build に失敗 ($(tail -1 "$WORK/$name.build.log"))"
    continue
  fi
  if runs_within 5 "$out" --__autobuild_warmup__; then
    ok "$name: --__autobuild_warmup__ で 5 秒以内に終了する"
  else
    fail "$name: --__autobuild_warmup__ で終了しない (温めが裏ビルドを止める)"
  fi
done < <(grep -lE '^\s*go_autobuild_exec ' "$ROOT_DIR"/bin/* 2>/dev/null)

# 対象 0 件は「全部通った」ではなく「1 本も見ていない」
if (( found == 0 )); then
  fail "go_autobuild_exec を呼ぶラッパーが 1 本も見つからない (grep の形が変わった?)"
fi

printf '\n'
if (( fails == 0 )); then
  echo "OK: go_autobuild warmup ($found 本が未知フラグで即終了)"
else
  echo "FAIL: go_autobuild warmup ($fails 件)" >&2
  exit 1
fi
