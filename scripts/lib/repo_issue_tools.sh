# repo_issue_tools.sh — dotfiles の issue の道具 (issue_done.sh・issue_next_number.sh・issue_claim.sh) を他の repo から呼んだときに、
# その repo の決まりへ渡すための判定。source して使う (set -e の下で呼んでよい)。
#
# dotfiles の道具は「repo 直下の issues/ を 1 つの番号の列として扱う」repo の道具。入れ子の issues/ の扱いは repo ごとに違い
# (obaket は issues/ と macOS/issues/ で通し番号、DualNoteApp は issues/ と macOS/issues/ が通しで iOS/issues/ だけ 001 からの別の列、umbrella の project-templates/issues/ は
# submodule の別の repo)、外から決められない。だから入れ子の dir は操作せず、repo が自分の道具を持つならそちらへ案内して止まる。
# 入れ子の dir を一律に数える形にしない: DualNoteApp では iOS/ の列の番号を最大値に混ぜ、umbrella では submodule の issue を動かす。

# repo_issue_tool_owned <repo> <tool>: <repo>/bin/<tool> (実行できるファイル) があれば 0 を返し、パスを stdout に出す。
# 🚨 dotfiles の bin/issue-claim は issue_claim.sh の部品なので、claim の道具の判定には使わない (dotfiles の中で自分を案内して止まる)
repo_issue_tool_owned() {
  [ -f "$1/bin/$2" ] && [ -x "$1/bin/$2" ] || return 1
  printf '%s\n' "$1/bin/$2"
}

# repo_issue_nested_matches <repo> <NNN>: 入れ子の 1 段 (<repo>/*/issues・<repo>/*/issue) にある <NNN>-*.md を repo からの相対パスで出す
# (done/ も含む)。別の repo (submodule) の dir は数えない。何も出なければ rc=1。issue が見つからなかったときの案内にだけ使い、操作には使わない
repo_issue_nested_matches() {
  local repo="$1" num="$2" dir top found=1 m
  for dir in "$repo"/*/issues "$repo"/*/issue; do
    [ -d "$dir" ] || continue
    top="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)" || continue
    [ "$(cd "$top" && pwd -P)" = "$(cd "$repo" && pwd -P)" ] || continue
    while IFS= read -r m; do
      [ -n "$m" ] || continue
      printf '%s\n' "${m#"$repo"/}"
      found=0
    done < <(find "$dir" -type f -name "$num-*.md" -print)
  done
  return "$found"
}
