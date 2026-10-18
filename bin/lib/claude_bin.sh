# claude_bin.sh — 実際に動く claude の実体を PATH から選ぶ
# shellcheck shell=bash  # shebang を持たない source 専用ファイル
#
# 提供する関数:
#   resolve_claude
#     出力: PATH にある claude の候補のうち、`--version` が成功する最初のものの絶対パスを stdout に 1 行
#     rc:   0 = 選べた / 1 = PATH に claude が無い / 2 = 候補はあるがどれも動かない・応答しない (試した候補を stderr に出す)
#     呼び出し側は 1 と 2 を区別できる (CI で「無いから skip」はよいが、「壊れた claude しか無い」は見えるようにする)
#
# なぜ `command -v claude` で済ませないか:
#   版管理 (nodenv 等) の shim は、どれかの版に claude が入っているだけで PATH の先頭に現れる。
#   今の版に無ければ shim は実行時に rc=127 で終わるが、`command -v` も `-x` もそれを「在る」と判定する。
#   shim を `nodenv which claude` で辿る方法 (src/pro-con/dispatcher/claude.go) も、今の版に無ければ失敗して
#   後ろの本物に落ちないので使えない。実際に起動して確かめるのが唯一の判定になる。
#
# --version には時間の上限を付ける (既定 10 秒、環境変数 CLAUDE_BIN_VERSION_TIMEOUT で変えられる)。終わらない候補が
# 1 つあるだけで make test や skill-eval が止まったままになるため。上限を超えた候補は子孫ごと止めて「応答なし」として次へ進む。
# 上限付きの実行は runtimeout (bin/lib/runtimeout.sh)。解決できない (go が無い・ビルドが落ちた) なら rc=2 で止める
# (上限なしで起動すると、終わらない候補 1 つで呼び出し元が止まったままになる)。

# shellcheck source=bin/lib/runtimeout.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/runtimeout.sh"
# shellcheck source=bin/lib/uint.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/uint.sh"
_CLAUDE_BIN_ROOT="$(CDPATH='' cd "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"

# _claude_bin_version_ok <候補>: 0 = 上限内に --version が成功 / 1 = 失敗 / 124 = 上限を超えたので止めた
_claude_bin_version_ok() {
  local limit="${CLAUDE_BIN_VERSION_TIMEOUT:-10}" rc=0
  # 0 (と先頭が 0 のもの) も既定値に寄せる (runtimeout の 0 は「上限なし」で、終わらない候補 1 つで呼び出し元が止まったままになる)
  # 数字でない値と 7 桁を超える値も既定値に寄せる (罠の中身は bin/lib/uint.sh。runtimeout は 9223372036 秒を超える値を
  # 誤用 rc=125 で拒み、全候補が「失敗」に見える。7 桁 ≈ 115 日はそれより十分小さい)
  uint_ok "$limit" 7 || limit=10
  case "$limit" in 0*) limit=10 ;; esac
  # stdin を閉じる: resolve_claude のループの入力 (候補の一覧) を候補が読んで、後ろの候補が消えるのを防ぐ
  "$RUNTIMEOUT" -k 1 "$limit" "$1" --version </dev/null >/dev/null 2>&1 || rc=$?
  case "$rc" in
    0) return 0 ;;
    124) return 124 ;;
    *) return 1 ;;
  esac
}

resolve_claude() {
  local c seen=":" tried=() vrc
  while IFS= read -r c; do
    [ -n "$c" ] || continue
    case "$seen" in *":$c:"*) continue ;; esac  # PATH の重複で同じ候補を 2 回起動しない
    seen="$seen$c:"
    # 毎回通す (同じ checkout のバイナリが入っていれば速く返る)。RUNTIMEOUT の値をここで信用すると、別のバイナリを指す値で照合を迂回する
    runtimeout_resolve "$_CLAUDE_BIN_ROOT" || {
      printf 'resolve_claude: runtimeout を用意できないので、候補の --version に上限を課せない\n' >&2
      return 2
    }
    vrc=0
    _claude_bin_version_ok "$c" || vrc=$?
    if [ "$vrc" -eq 0 ]; then
      printf '%s\n' "$c"
      return 0
    fi
    if [ "$vrc" -eq 124 ]; then
      tried+=("$c (応答なし)")
    else
      tried+=("$c")
    fi
  done < <(type -aP claude 2>/dev/null || true)
  if [ "${#tried[@]}" -eq 0 ]; then
    return 1
  fi
  printf 'resolve_claude: PATH の claude はどれも動かない (--version が失敗か応答なし): %s\n' "${tried[*]}" >&2
  return 2
}
