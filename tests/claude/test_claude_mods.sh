#!/usr/bin/env bash
#
# _claude/mods/*/ (Claude Code の mods = 関数 hook の plugin) を自動で見つけ、各 mod に
# `claude plugin validate` と `claude plugin test` を回す (issue 619)。あわせて settings の env が
# この置き場所を読ませているかを見る (配線がずれると、検査は緑のまま本番は 1 本も読まない)。
#
# 🚨 CI では claude の要る検査を skip する (exit 77)。CI の runner に claude が無く、入れると毎回その時点の最新版になる。
#   mods の API は early access でリリースごとに変わるので、CI の版は手元で実際に動いている engine と一致しない。
#   手元の `make test` (コミット前のゲート) を正本にする。手元で claude が見つからないときは skip にせず失敗にする
#   (CI 以外で黙って検査が消える形にしない)。CI で回すと決めたら、版を固定して入れる手当てと一緒にここを直す。
#   settings の配線の検査は claude が要らないので、CI でも skip の前に走る。
#
# 判定は rc だけでなく、実行されたテストの件数も見る (0 件は失敗。verify-execution-not-just-exit-code.md)。
# pass の件数とは突き合わせない: kit に skip / todo が無く (`test.skip` は undefined で rc=1。2.1.287)、rc の検査と同値になる。
# `claude plugin test` の出力の形 (2.1.287 で実測): 末尾に ` N pass` / ` N fail` / `Ran N tests across M file(s).`。
# 失敗するテストがあれば rc=1、*.test.ts が無い mod は rc=1 + stderr に `no *.test.ts or *.test.tsx under <dir>`。
#
# 親の環境の CLAUDE_CODE_PLUGIN_DIRS は外さない: `claude plugin test` はそれを読まない (2.1.287 で実測。env 側に同名の
# mod を置いても結果は変わらず、壊した側はそのまま赤)。読むようになったら `env -u CLAUDE_CODE_PLUGIN_DIRS` で呼ぶ。

set -euo pipefail
unset CDPATH

repo="$(cd "$(dirname "$0")/../.." && pwd)"
mods_root="$repo/_claude/mods"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# --- settings の env が _claude/mods を読ませているか ------------------------------------
# 値は `~/dotfiles/_claude/mods` に固定する (読まれるのは ~/dotfiles の実体。docs/claude-mods.md)
# shellcheck disable=SC2088 # settings に書く文字列そのものと比べる (展開しない)
want_dirs='~/dotfiles/_claude/mods'
got_dirs="$(jq -r '.env.CLAUDE_CODE_PLUGIN_DIRS // empty' "$repo/_claude/settings.json")"
if [ "$got_dirs" != "$want_dirs" ]; then
  echo "✗ _claude/settings.json の env.CLAUDE_CODE_PLUGIN_DIRS が '$got_dirs' (期待 '$want_dirs')" >&2
  exit 1
fi
echo "✓ settings の env.CLAUDE_CODE_PLUGIN_DIRS = $want_dirs"

# shellcheck source=bin/lib/claude_bin.sh
source "$repo/bin/lib/claude_bin.sh"
# PATH の名前ではなく実際に動く実体で選ぶ (版管理の shim を掴んで rc=127 で落ちていた。issue 639)
resolve_rc=0
claude_bin="$(resolve_claude)" || resolve_rc=$?
if [ "$resolve_rc" -eq 1 ] && [ -n "${CI:-}" ]; then
  echo "- SKIP: CI に claude が無いので mods の validate / test を検査していない (手元の make test が正本)"
  exit 77
elif [ "$resolve_rc" -eq 1 ]; then
  echo "✗ claude が見つからない (CI 以外では mods の検査を skip しない)" >&2
  exit 1
elif [ "$resolve_rc" -ne 0 ]; then
  # 候補はあるが動かない (rc=2) は CI でも skip にしない。壊れた claude を緑の skip に隠さない
  echo "✗ PATH の claude がどれも動かない (resolve_claude rc=${resolve_rc}。上に試した候補)" >&2
  exit 1
fi
echo "✓ claude: $claude_bin"

# ran_count <claude plugin test の stdout>: `Ran N tests` の N。取れなければ空
ran_count() {
  sed -n 's/^Ran \([0-9][0-9]*\) tests\{0,1\} across .*/\1/p' <<<"$1" | tail -1
}

# --- canary: 件数の読み取りが今の出力の形で効くか ------------------------------------
# 本走査と同じ ran_count を通す。読み取りが空振りすると「0 件」と「読めない」を区別できない
if [ "$(ran_count $'(pass) x\n\n 2 pass\n 0 fail\nRan 2 tests across 1 file. [0.18s]')" != 2 ] ||
  [ "$(ran_count $' 1 pass\nRan 1 test across 1 file. [0.1s]')" != 1 ] ||
  [ -n "$(ran_count 'no tests')" ]; then
  echo "✗ canary: ran_count が既知の出力から件数を読めない" >&2
  exit 1
fi

fail=0
n=0
for mod in "$mods_root"/*/; do
  mod="${mod%/}"
  name="$(basename "$mod")"
  # manifest の無いディレクトリを engine は黙って読まない (debug log に `no manifest in <dir>` が出るだけ)。
  # mod を消した後も、engine が書いた ignore 済みの生成物 (tsconfig.json / types/) は git が消さないので、ディレクトリだけ残る
  if [ ! -f "$mod/.claude-plugin/plugin.json" ]; then
    echo "✗ $name: .claude-plugin/plugin.json が無い (engine は mod として読まない。消した mod の残骸で生成物だけなら rm -rf してよい)" >&2
    fail=1
    continue
  fi
  n=$((n + 1))

  if "$claude_bin" plugin validate "$mod" >"$work/validate.$name.out" 2>&1; then
    echo "✓ $name: claude plugin validate"
  else
    echo "✗ $name: claude plugin validate が失敗" >&2
    cat "$work/validate.$name.out" >&2
    fail=1
  fi

  trc=0
  "$claude_bin" plugin test "$mod" >"$work/test.$name.out" 2>"$work/test.$name.err" || trc=$?
  ran="$(ran_count "$(cat "$work/test.$name.out")")"
  if [ "$trc" -eq 0 ] && [ -n "$ran" ] && [ "$ran" -gt 0 ]; then
    echo "✓ $name: claude plugin test ($ran 件)"
  else
    echo "✗ $name: claude plugin test が失敗 (rc=$trc, 実行件数=${ran:-読めない})" >&2
    cat "$work/test.$name.out" "$work/test.$name.err" >&2
    fail=1
  fi
done

if [ "$n" -eq 0 ]; then
  echo "✗ $mods_root に mod が 1 つも無い (走査の空振りを合格にしない)" >&2
  exit 1
fi
echo "mods: $n 件を検査"
exit "$fail"
