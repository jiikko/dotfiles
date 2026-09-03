#!/usr/bin/env bash
# check_cd_rc_in_tests.sh — `cd` の rc を見ていない行を落とす (tests/ scripts/ bin/)。
#
# ⚠️ 名前は tests_ だが **scripts/ と bin/ も見る**。`scripts/discover_shell_scripts.sh` の
# `cd "$(dirname "$0")/.."` が失敗すると**テストを 0 件発見したまま緑**になる形が実在した
# (敵対的レビュー 2026-09-03 の指摘。同時に守った)。汚染より「静かに 0 件」の方が重い。
#
# なぜ (issue 204。2026-09-03 に実際に踏んだ):
#   テストは「一時ディレクトリを作って入り、そこに fixture を書く」形をとる。この 2 行 1 組の
#   どちらかが失敗しても rc を見ていないと、**CWD (= repo root) に fixture が書かれる**。
#     TEST_DIR="$TEST_TMP/inj2"   # TEST_TMP が空だと "/inj2"
#     mkdir -p "$TEST_DIR"        # 権限が無く失敗 (rc を見ていない)
#     cd "$TEST_DIR"              # 失敗 (rc を見ていない)
#     echo "dummy video" > "./$EVIL"   # ← repo root に書かれる
#   実際に repo root へ fixture 3 件が残った。名前に `$(touch pwned_*)` を含むものだったが
#   **注入は実行されていない** (`ls pwned_*` が 0 件。glob 展開では `$(...)` は評価されない)。
#
#   起点は「zsh のテストを bash で直接実行した」こと: `source "${0:A:h}/test_helper.sh"` の
#   `${0:A:h}` は zsh 拡張で、bash では空に潰れて `/test_helper.sh` になる。helper が 1 行も
#   走らないので `TEST_TMP` が空のまま先へ進む。**呼び方の誤りは静かに repo を汚す形だった**。
#
# この検査が守るもの: 呼び方を間違えたときに「失敗する」ようにする (汚してから緑を返さない)。
#   ⚠️ 「zsh のテストを bash で呼ぶな」を検査する形にはしていない。呼び方は人と runner の側の
#   問題で、テスト自身は**どう呼ばれても repo を汚さない**方が強い。
#
# 意図的な例外は行内に `cd-rc: allow` を書く (理由も添えること)。逃げ道が無いと
# 「検査に食われるから書き方を変える」運用になる (check_pipefail_grep_q.sh と同族)。
#
# 何を「守っている」と見なすか (敵対的レビュー 2026-09-03 で判定を作り替えた):
#   ⚠️ 最初は「`||` / `&&` / `;` を含む行は rc を見ている」としていたが、**`cd "$X" || true` /
#   `cd "$X" ; echo hi` / `cd "$X" || echo warn` が全部素通り**した (実測)。これらは最も
#   起こりやすい退行そのもので、案 C の目的「再発を止める」を達していなかった。
#   そこで **停止する動作 (exit / return / die / fail) を右辺に持つ形だけを allowlist する**。
#     許す: cd "$X" || exit 1 / … || exit / … || return 1 / … || die "msg" / … || fail …
#     落とす: cd "$X" / … || true / … ; echo hi / … || echo warn / … && cmd
#   `&&` を落とすのは「cd が失敗したら後続行が無防備」だから (`cd X && a && b` の b の後も続く)。
#   行末が `\` の継続行も落とす (どこで rc を見ているか静的に追えない)。
#
# ⚠️ **行ベースの検査**なので、heredoc の本文やサブシェル `( ... )` の中の行頭 `cd` も対象になる
#   (最初のヘッダは「検査しない」と書いていたが実装は検出する。実装が正)。
#   heredoc の中で意図的に守らない形が要るなら `cd-rc: allow` を書く。
# 検査しないもの:
#   - `$(cd ... && pwd)` のようなコマンド置換 (行頭が `cd` でない)
#   - `foo && cd "$X"` のように `cd` が行頭でない形 (前段の rc に従属している)
#   - `builtin cd` / `command cd` / `pushd` (現在 tests/ に 0 件。使い始めたら足す)
#
# ⚠️ **この検査が構造的に見落とすもの** (予算を置くなら見落としを先に書く):
#   - `cd "$X" && cmd` で**連鎖が同じ行で閉じている**場合、その**次の行**は cd の成功に
#     従属しない。行ベースの検査では追えない。現在 tests/ にある `&&` の形は 3 件とも
#     サブシェル `( ... )` の中で連鎖が最後まで続いており安全 (2026-09-03 に目視)。
#     `cd X && a` の直後に無防備な行を書く形が増えたら、この検査は止められない
#   - 変数経由 (`c=cd; $c "$X"`) / `eval` の中
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)" || exit 1

cd "$ROOT_DIR" || exit 1

# 行頭 (インデントのみ許す) が `cd` で、rc を見る形 (|| / && / ;) を伴わない行。
# ⚠️ system grep を使う。ugrep 経由の grep は `$` を行末アンカーと解釈して 0 件を返す
# (issue 204 のレビューで実測)。ここは bash から /usr/bin/grep を直接呼ぶので影響しないが、
# 手で確かめるときも `/usr/bin/grep` を使うこと。
# 行頭 (インデントのみ許す) が `cd` の行を全部拾い、**停止する形だけを許す**。
candidate='^[[:space:]]*cd([[:space:]]|$)'
# 右辺が停止動作なら守られている (exit / return / die / fail。引数の有無は問わない)
# 守られている形:
#   1. `|| exit` / `|| return` / `|| die` / `|| fail` — 停止する
#   2. `&&` — 右辺が cd の成功に従属する (cd が失敗したら 1 つも走らない)
#   3. `|| { …; exit 1; }` — ブロックの中で停止する (既存の check_*.sh がこの形)
#      ⚠️ ブロックの中に exit / return が**無い**形 (`|| { echo warn; }`) は許さない
guarded='(\|\|[[:space:]]*(exit|return|die|fail)([[:space:]]|$)|\|\|[[:space:]]*\{[^}]*(exit|return)|&&)'

hits=0
files=0
while IFS= read -r f; do
  files=$((files + 1))
  # per-file の grep。rc は 0 (一致) / 1 (不一致) 以外を「判定不能」として落とす
  # (読めないファイルを緑に畳まない。chmod 000 で実測: 以前は ✓ を返していた)
  out=$(/usr/bin/grep -HnE "$candidate" "$f")
  rc=$?
  if [ "$rc" -gt 1 ]; then
    echo "✗ 検査できなかった: $f (grep rc=$rc)" >&2
    exit 1
  fi
  [ -n "$out" ] || continue
  while IFS= read -r line; do
    case "$line" in
      *'cd-rc: allow'*) continue ;;
    esac
    # 停止する形なら守られている
    if printf '%s' "$line" | /usr/bin/grep -qE "$guarded"; then
      continue
    fi
    printf '  %s\n' "$line"
    hits=$((hits + 1))
  done <<< "$out"
done < <(/usr/bin/find tests scripts bin -type f \( -name '*.sh' -o -name '*.zsh' \) 2>/dev/null | sort)

if [ "$files" -eq 0 ]; then
  echo "✗ 検査対象の *.sh / *.zsh が 1 件も無い (検査が空振りしている)" >&2
  exit 1
fi

if [ "$hits" -gt 0 ]; then
  {
    echo ""
    echo "✗ cd の rc を見ていない行が ${hits} 件 (issue 204)"
    echo "  失敗しても CWD (= repo root) のまま先へ進み、fixture が repo に書かれる。"
    echo "  直し方: cd \"\$X\" || exit 1"
    echo "  意図的な例外は行内に 'cd-rc: allow' を書く (理由も添える)"
  } >&2
  exit 1
fi

echo "✓ cd の rc 未確認: ${files} ファイルに該当なし"
