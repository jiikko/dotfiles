#!/usr/bin/env bash
# 破壊的 confirm の Enter ガード (--default=false) を「全ての呼び出し箇所」で強制するゲート。
#
# なぜ必要か: scripts/CLAUDE.md は「gum confirm は --default=false に統一する (Enter 素通しでは
# 実行されない)」を規約として定めているが、これは長らくコメントだけで強制されていなかった。
# 2026-08-20 の監査の変異実験で、3 本の confirm から --default=false を全削除しても
# tests/tmux/test_confirm_scripts.sh が緑のままだと実証された (隣の `&&` 短絡の規約は
# 実装強制されていたので、同じ CLAUDE.md の 2 行が非対称だった)。
#
# 検出は「呼び出し単位」で行う。以下は red team が実際に素通りさせた形で、いずれも
# 行単位の部分一致では防げない (issue 069 と同じ「窓の外から騙す」クラス):
#   - 同じ行の行末コメントに --default=false という文字列だけ置く
#   - 1 行に 2 つ目の confirm を足す (_tmux.conf の bind は 1 行に全部書く様式)
#   - scripts/lib/ や bin/ や zshlib/ に置く (旧実装は scripts/*.sh と _tmux.conf だけ見ていた)
#   - "$GUM" confirm と書く / gum と confirm を行継続で分割する
#
# 🚨 検査できなかったときに緑を返さないこと。発見 0 件・awk の失敗はいずれも失敗扱いにする。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 検査対象は「実行されるコード」。登録不要の発見式にし、検査対象の外に confirm を置いて
# 逃げられないようにする。除外するのは実行されない場所だけ (テスト・文書・生成物・vendor)。
# 検査対象は「実行されるコード」。登録不要の発見式にし、検査対象の外に confirm を置いて
# 逃げられないようにする (旧実装は scripts/*.sh と _tmux.conf だけ見ており、scripts/lib/ や
# bin/ や zshlib/ に置くだけで素通りした)。除外するのは実行されない場所だけ
# (テスト自身・文書・生成物・vendor)。バイナリは awk が LANG 依存で落ちる (実測: .DS_Store と
# ファームウェア .upd で rc=2) ため、confirm を含むテキストファイルだけに絞る。
# 🚨 grep の rc は 3 分岐する: 0 = 一致、1 = 不一致、**2 以上 = 検査できなかった** (読めない
# ファイル・I/O エラー)。`&& targets+=(…)` だと 1 と 2 が同じ扱いになり、**読めなかった違反
# ファイルを黙って対象から外して緑を返す** (実測 2026-08-21: 違反ファイルを chmod 000 にすると
# 「✓ すべてが --default=false を持つ」rc=0)。除外されると下の awk の fail-closed も発動しない。
# 冒頭が「検査できなかったときに緑を返さないこと」と宣言している以上、rc>=2 は失敗にする。
targets=()
unreadable=()
while IFS= read -r f; do
  LC_ALL=C grep -Iq 'confirm' "$f" 2>/dev/null
  case "$?" in
    0) targets+=("$f") ;;
    1) : ;;                      # 一致なし = 検査済みで対象外
    *) unreadable+=("$f") ;;     # 検査できなかった = 緑にしない
  esac
done < <(
  find "$ROOT_DIR" \
    \( -name .git -o -name tmp -o -name tests -o -name docs -o -name issues \
       -o -name node_modules -o -name vendor -o -name src -o -name .venv \) -prune -o \
    -type f ! -name '*.md' ! -name '*.json' ! -name '*.lock' -print 2>/dev/null
)
if [ "${#unreadable[@]}" -gt 0 ]; then
  printf '✗ 検査できなかったファイルがある (%d 件)。緑にしない:\n' "${#unreadable[@]}"
  printf '  %s\n' "${unreadable[@]}"
  exit 1
fi
if [ "${#targets[@]}" -lt 1 ]; then
  printf '✗ confirm を含むファイルを 1 件も列挙できなかった (find / grep の失敗、または全廃)\n'
  printf '  検査できていない可能性があるので緑にしない\n'
  exit 1
fi

# awk で「呼び出しごとの引数区間」を切り出して検査する。
# - 行継続 (\ + 改行) を畳む: `gum \` + 改行 + `confirm ...` を 1 呼び出しとして見る
# - "$GUM" / ${GUM} / $GUM を gum に正規化する (テストスタブ化で導入されがちな書き方)
# - 引数区間は「次の連結演算子 / 次の confirm まで」。同一行の 2 つ目以降も個別に見る
# - 区間から行末コメント (`# …` / バッククォートコメント) を落としてから判定する
#   (コメントに文字列だけ置いて騙す形を閉じる)
report=$(awk '
function strip_comments(s) {
  gsub(/`#[^`]*`/, "", s)
  sub(/[ \t]#[ \t].*/, "", s)
  return s
}
{
  ln = FNR; line = $0
  # 行全体がコメントの行は説明文なのでコードとして数えない (規約を説明している散文が
  # 「--default=false の無い呼び出し」に見えてしまう偽陽性を避ける)。
  probe = line; sub(/^[ \t]+/, "", probe)
  if (probe ~ /^#/) next
  while (line ~ /\\$/) {
    sub(/\\$/, " ", line)
    if ((getline nxt) > 0) line = line nxt; else break
  }
  gsub(/"?\$\{?GUM\}?"?/, "gum", line)
  rest = line
  while (match(rest, /gum[ \t]+confirm/)) {
    after = substr(rest, RSTART + RLENGTH)
    args = after
    if (match(args, /(&&|\|\||;|gum[ \t]+confirm)/)) args = substr(args, 1, RSTART - 1)
    args = strip_comments(args)
    n++
    if (args !~ /--default=false/) {
      printf("BAD\t%s:%d\t%s\n", FILENAME, ln, substr(line, 1, 160))
    }
    rest = after
  }
}
END { printf("COUNT\t%d\n", n) }
' "${targets[@]}" 2>/dev/null)
awk_rc=$?

if [ "$awk_rc" -ne 0 ]; then
  printf '✗ 呼び出し箇所の走査に失敗した (awk rc=%s)。検査できていないので緑にしない\n' "$awk_rc"
  exit 1
fi

n=0
while IFS= read -r line; do
  case "$line" in
    COUNT*) n="${line#COUNT	}" ;;
  esac
done <<< "$report"
case "$n" in ''|*[!0-9]*) n=0 ;; esac

if [ "$n" -lt 1 ]; then
  printf '✗ gum confirm の呼び出しを 1 件も発見できなかった\n'
  printf '  confirm を意図的に全廃したならこのゲートも畳むこと (無マッチを緑にしない)\n'
  exit 1
fi

bad=0
while IFS= read -r line; do
  case "$line" in
    BAD*)
      printf '✗ --default=false の無い confirm: %s\n' "${line#BAD	}"
      bad=1
      ;;
  esac
done <<< "$report"

if [ "$bad" -ne 0 ]; then
  printf '\n破壊的な確認は Enter 素通しで実行されてはいけない (scripts/CLAUDE.md の規約)。\n'
  printf 'gum confirm には --default=false を付けること。\n'
  exit 1
fi

printf '✓ gum confirm %s 呼び出しすべてが --default=false を持つ\n' "$n"
printf '\nAll confirm-default-gate tests passed successfully!\n'
