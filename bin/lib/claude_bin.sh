# claude_bin.sh — 実際に動く claude の実体を PATH から選ぶ
# shellcheck shell=bash  # shebang を持たない source 専用ファイル
#
# 提供する関数:
#   resolve_claude
#     出力: PATH にある claude の候補のうち、`--version` が成功する最初のものの絶対パスを stdout に 1 行
#     rc:   0 = 選べた / 1 = PATH に claude が無い / 2 = 候補はあるがどれも動かない (試した候補を stderr に出す)
#     呼び出し側は 1 と 2 を区別できる (CI で「無いから skip」はよいが、「壊れた claude しか無い」は見えるようにする)
#
# なぜ `command -v claude` で済ませないか (issue 639):
#   版管理 (nodenv 等) の shim は、どれかの版に claude が入っているだけで PATH の先頭に現れる。
#   今の版に無ければ shim は実行時に rc=127 で終わるが、`command -v` も `-x` もそれを「在る」と判定する。
#   shim を `nodenv which claude` で辿る方法 (src/pro-con/dispatcher/claude.go) も、今の版に無ければ失敗して
#   後ろの本物に落ちないので使えない。実際に起動して確かめるのが唯一の判定になる。
#
# --version に時間の上限は付けていない (macOS には標準の timeout が無い)。stdin は閉じて起動するので、
# 対話の入力待ちで止まることはない。ハングする claude が実際に現れたら上限を足す。

resolve_claude() {
  local c seen=":" tried=()
  while IFS= read -r c; do
    [ -n "$c" ] || continue
    case "$seen" in *":$c:"*) continue ;; esac  # PATH の重複で同じ候補を 2 回起動しない
    seen="$seen$c:"
    # stdin を閉じる: このループの入力 (候補の一覧) を候補が読んで、後ろの候補が消えるのを防ぐ
    if "$c" --version </dev/null >/dev/null 2>&1; then
      printf '%s\n' "$c"
      return 0
    fi
    tried+=("$c")
  done < <(type -aP claude 2>/dev/null || true)
  if [ "${#tried[@]}" -eq 0 ]; then
    return 1
  fi
  printf 'resolve_claude: PATH の claude はどれも動かない (--version が失敗): %s\n' "${tried[*]}" >&2
  return 2
}
