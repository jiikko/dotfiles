#!/usr/bin/env bash
#
# PreToolUse(Bash) フック: 一時領域の浅い場所 (Claude Code の scratchpad の根・project・セッション dir、$TMPDIR の直下) を
# 直接消す・移すコマンドを deny し、bin/tmpclean (skill cleanup-tmp-dir) を案内する。
#
# なぜ: 生きているセッションの scratchpad を消すとそのセッションが壊れる。見分けは bin/tmpclean が層を重ねて行う
# (src/tmpclean/README.md)。Claude が rm を直接組むと、その守りを全部迂回する (2026-10-21、手順書に埋めた rm で
# 敵対レビュー 3 周とも生きたセッションを消せる穴が出た)。
#
# 判定 (字面で見る粗い判定):
#   - 消す・移す語 (rm / rmdir / unlink / mv / trash / find -delete / xargs rm) がコマンドのどこかにあり、かつ
#   - 次のパスがコマンドのどこかにある: /tmp/claude-<数字> か /private/tmp/claude-<数字> の配下で深さ 2 まで、
#     /var/folders/*/*/T か /private/var/folders/*/*/T の配下で深さ 1 まで、$TMPDIR / ${TMPDIR} の配下で深さ 1 まで。
#     引用符は外してから見る。`..` と `.` は字面で畳む。glob (* ? [) を含む要素がその深さまでにあれば該当とみなす
#   自分の scratchpad の中のファイル (深さ 3 以上) を消す普段の作業は止めない。
#
# 検出しないもの: 変数経由 (P=/private/tmp/claude-501; rm -rf "$P"/x)・スクリプト経由・他の言語 (python の shutil 等)・
#   eval / base64 で組んだコマンド。語とパスが別々のコマンドにあっても 1 つの Bash 呼び出しの中なら deny する (偽陽性は
#   呼び出しを分ければ通る)。
#
# 入力: PreToolUse の hook JSON (stdin)。出力: 違反時のみ permissionDecision=deny の JSON。それ以外は無出力 exit 0。
set -uo pipefail

# shellcheck source=_claude/hooks/lib/hook_io.sh
. "$(dirname "$0")/lib/hook_io.sh" 2>/dev/null || exit 0
hook_read_bash_command || exit 0
cmd=$HOOK_CMD

# 対象のパスの手がかりが無ければ即通す (fast path。判定は変えない)
case "$cmd" in
  *claude-* | *folders/* | *TMPDIR*) ;;
  *) exit 0 ;;
esac

REASON="一時領域の浅い場所 (Claude Code の scratchpad の根・project・セッション dir、\$TMPDIR の直下) を直接消す・移すのは止めています。生きているセッションを消すとそのセッションが壊れます。bin/tmpclean (skill cleanup-tmp-dir) で数える → plan → 承認 → apply の順に片付けてください。自分の scratchpad の中のファイル (セッション dir より深い所) は消せます。語とパスが別のコマンドにあるだけなら、Bash の呼び出しを分けてください"

# 消す・移す語。前に英数字や / . - が付くもの (grm・./rm-x・--rm など) は語として数えない
verb_re='(^|[^[:alnum:]_./-])(rm|rmdir|unlink|mv|trash)([[:space:]]|$)|[[:space:]]-delete([[:space:]]|$)|xargs([[:space:]]+-[^[:space:]]+)*[[:space:]]+(rm|mv)([[:space:]]|$)'
grep -Eq "$verb_re" <<<"$cmd" || exit 0

# 大きすぎる入力は検査を諦めて deny に倒す (timeout に殺されて無出力 = 素通りにしない)
if [ "${#cmd}" -gt 131072 ]; then
  hook_deny "入力が大きすぎて静的検査を完了できない (${#cmd} byte)。一時領域を消す語を含むため保守的に拒否した。呼び出しを分けること"
fi

flat=${cmd//[\"\']/}
seg='[^[:space:];&|<>()`]*'
tokens=$(grep -oE "(/private)?/tmp/claude-[0-9]+(/$seg)?|(/private)?/var/folders/[^/[:space:]]+/[^/[:space:]]+/T(/$seg)?|\\\$\\{?TMPDIR\\}?(/$seg)?" <<<"$flat" || true)
[ -n "$tokens" ] || exit 0

# shallow <残りのパス> <深さの上限>: 根からの深さが上限以下か、上限までの要素に glob があれば 0 (該当)
shallow() {
  local rest=$1 limit=$2 depth=0 comp
  local IFS=/
  set -f
  for comp in $rest; do
    case $comp in
      '' | .) continue ;;
      ..) depth=$((depth > 0 ? depth - 1 : 0)); continue ;;
    esac
    depth=$((depth + 1))
    case $comp in
      *[*?[]*) [ "$depth" -le "$limit" ] && { set +f; return 0; } ;;
    esac
  done
  set +f
  [ "$depth" -le "$limit" ]
}

while IFS= read -r t; do
  case $t in
    */tmp/claude-[0-9]*)
      rest=${t#*/tmp/claude-}
      rest=${rest#"${rest%%/*}"}
      shallow "$rest" 2 && hook_deny "$REASON"
      ;;
    */var/folders/*)
      rest=${t#*/var/folders/*/*/T}
      shallow "$rest" 1 && hook_deny "$REASON"
      ;;
    *TMPDIR*)
      rest=${t#*TMPDIR}
      rest=${rest#\}}
      shallow "$rest" 1 && hook_deny "$REASON"
      ;;
  esac
done <<<"$tokens"
exit 0
