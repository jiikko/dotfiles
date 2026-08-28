#!/usr/bin/env bash
# CI (tests.yml) のグループ分割が「Makefile を出典にする」契約を守れているか検査する。
#
# 検査は 3 本:
#   1. sed 契約: workflow は make を使えない (依存解決の時点で make があるとは限らない) ため Makefile の生の行を
#      sed で読む。つまり実際の契約は「1 行の `VAR := ...` で書くこと」で、make の意味論
#      (`+=` / 行継続 / `$(OTHER)`) は通らない。make が見る値と sed が読む値を突き合わせ、
#      食い違ったら落とす。これが無いと「make は正しいのに CI だけパッケージが減る」状態が
#      無言で成立する (実測: `+=` 追記・行継続・変数参照のいずれでも 導入対象が減って rc=0。
#      しかも欠けたのが bats だと test-bats が skip して緑になるので観測できない)
#   2. heavy に .bats を置かない: heavy ジョブが走らせるのは test_*.sh だけで (test-discovered-heavy)、
#      .bats は prune の無い test-bats = rest ジョブで走る。heavy に置くと「heavy の依存として
#      検査されるのに実行は rest」という前提のずれになるので、置かせない
#   3. heavy のテストが「heavy に入れていないコマンド」(= rest にだけあるもの) を呼んでいないか
#
# ⚠️ 検査できなかったときに緑を返さないこと。依存コマンドの不在・読めないファイル・grep の
# エラー (exit 2) ・対象 0 件はすべて失敗として扱う。実測で、grep が exit 2 を返す入力
# (ERE メタ文字を含むコマンド名) で「依存なし」と誤判定していた。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

dirs="${CI_HEAVY_TEST_DIRS:-}"
only_rest="${CI_COMMANDS_ONLY_REST:-}"
pkgs_heavy="${CI_COMMANDS_HEAVY:-}"
pkgs_rest="${CI_COMMANDS_REST:-}"

fail() { printf '✗ %s\n' "$1"; exit 1; }

# ---- 依存コマンドの健全性 -------------------------------------------------------------
# ⚠️ 固定パターンだけで試さないこと。パターン依存の失敗 (メタ文字で exit 2) は
# 下の grep_rc で個別に見る。
for c in grep head find sed make sort; do
  command -v "$c" >/dev/null 2>&1 || fail "$c が無い。検査できないので緑にしない"
done
grep -q probe <<< 'probe' || fail "grep が正常に動作しない"
grep -q probe <<< "$(grep -vE '^#' <<< 'probe')" || fail "grep -vE が正常に動作しない"
[ "$(printf 'first\nsecond\n' | head -1)" = "first" ] || fail "head が正常に動作しない"

[ -n "$dirs" ] || fail "CI_HEAVY_TEST_DIRS が空 (Makefile から渡されていない)"
[ -n "$only_rest" ] || fail "CI_COMMANDS_ONLY_REST が空 (heavy と rest の差が無い、または受け渡しが壊れている)"
[ -n "$pkgs_heavy" ] || fail "CI_COMMANDS_HEAVY が空"
[ -n "$pkgs_rest" ] || fail "CI_COMMANDS_REST が空"

# ---- 1. sed 契約: make の値と workflow が読む値が一致すること --------------------------
# workflow (.github/workflows/tests.yml) と同じ式を使う。ここを変えたら workflow も直す。
sed_read() { sed -n "s/^$1[[:space:]]*:=[[:space:]]*//p" Makefile; }
norm() { tr -s ' \t' ' ' | sed 's/^ //; s/ $//'; }

for grp in HEAVY REST; do
  case "$grp" in
    HEAVY) want="$pkgs_heavy" ;;
    REST) want="$pkgs_rest" ;;
  esac
  got="$(sed_read "CI_COMMANDS_$grp")"
  [ -n "$got" ] || fail "workflow の sed 式が CI_COMMANDS_$grp を読めない (1 行の := で書くこと)"
  # 複数行が返る = 同名の定義が 2 つある (workflow は両方を連結して受け取り、意図とずれる)
  if [ "$(printf '%s\n' "$got" | grep -c .)" -ne 1 ]; then
    fail "CI_COMMANDS_$grp の := 定義が複数行ある (workflow は最初の 1 行だけを見ない)"
  fi
  a="$(printf '%s' "$want" | norm)"
  b="$(printf '%s' "$got" | norm)"
  if [ "$a" != "$b" ]; then
    printf '✗ CI_COMMANDS_%s: make の値と workflow が読む値が食い違う\n' "$grp"
    printf '    make (意図):        [%s]\n' "$a"
    printf '    workflow (sed):     [%s]\n' "$b"
    printf '  workflow は make を使えない (依存解決の時点で make があるとは限らない) ため生の行を sed で読む。\n'
    printf '  make の意味論 (+= / 行継続 / 他変数の参照) は通らない。1 行の := で書くこと。\n'
    exit 1
  fi
done

# ---- 2/3. heavy のテストを走査 --------------------------------------------------------
# quotemeta: コマンド名を ERE へ埋める前にメタ文字を無効化する (g++ 等で grep が exit 2 に
# なり、それを「依存なし」と誤判定していた)
quotemeta() { printf '%s' "$1" | sed 's/[][^$.*+?(){}|\\]/\\&/g'; }

total=0
bad=0
for d in $dirs; do
  [ -d "$d" ] || fail "heavy ディレクトリが無い: $d (CI_HEAVY_TEST_DIRS の追従漏れ)"

  # .bats は heavy では走らない (test-discovered-heavy は test_*.sh のみ) ので置かせない
  bats_here="$(find "$d" \( -type f -o -type l \) -name '*.bats' 2>/dev/null)"
  if [ -n "$bats_here" ]; then
    printf '✗ heavy に .bats がある (heavy ジョブは test_*.sh しか走らせない = 実行は rest 側):\n'
    printf '%s\n' "$bats_here" | sed 's/^/    /'
    bad=1
  fi

  # 走査対象。⚠️ 拡張子で絞りすぎないこと (.bash / 拡張子なしヘルパーが漏れる)。
  # symlink も含める (-type f だけだと除外される)。
  files=()
  while IFS= read -r f; do [ -n "$f" ] && files+=("$f"); done < <(
    find "$d" \( -type f -o -type l \) ! -name '*.md' ! -name '*.txt' ! -name '*.json' 2>/dev/null
  )
  # ⚠️ 0 件判定はディレクトリごとに行う。全 dir の合計で見ると、片方の被覆が丸ごと消えても
  # もう片方が 1 件あれば緑になる (実測)。
  if [ "${#files[@]}" -lt 1 ]; then
    fail "heavy ディレクトリにファイルが 1 件も無い: $d (パス変更 / find の失敗)"
  fi
  total=$((total + ${#files[@]}))

  for f in "${files[@]}"; do
    [ -r "$f" ] || fail "読めないファイルがある: $f (検査できないので緑にしない)"
    for cmd in $only_rest; do
      q="$(quotemeta "$cmd")"
      # shebang での使用 (bats 等)。行頭アンカーを付ける (1 行目の散文で誤検出しない)
      if grep -qE "^#!.*(^|[^A-Za-z0-9_.-])${q}([^A-Za-z0-9_.-]|\$)" <<< "$(head -1 "$f")"; then
        printf '✗ heavy のテストが %s に依存 (shebang): %s\n' "$cmd" "$f"
        bad=1
        continue
      fi
      # 実コードでの使用。コメント (行頭・行内) を落としてから見る。
      # 境界は否定文字クラスで取る ($( ・バッククォート・引用符・絶対パス・&&・; の直前後も拾う)。
      # 代入右辺 (=tmux / :-tmux) も「使う意図」として拾う。
      # grep -a: NUL を含むファイルを binary 扱いで飛ばさない (飛ばすと無検査で緑になる)
      # ⚠️ heredoc 内の散文は静的には除外できない (実測で偽陽性になる)。逃げ道として
      # 行内に `ci-group-deps: allow` を書いた行は無視する。逃げ道が無いと、将来
      # 「検査器に食われるから文章を書き換える」運用になる (no-comment-line-starting-with-shellcheck.md
      # と同族の罠)。
      body="$(grep -va '^[[:space:]]*#' "$f" 2>/dev/null \
        | grep -va 'ci-group-deps: allow' \
        | sed 's/[[:space:]]#[[:space:]].*$//')"
      grep -qaE "(^|[^A-Za-z0-9_.-])/?${q}([^A-Za-z0-9_.=-]|\$)|[=:][-]?${q}([^A-Za-z0-9_.-]|\$)" <<< "$body"
      rc=$?
      case "$rc" in
        0)
          printf '✗ heavy のテストが %s に依存: %s\n' "$cmd" "$f"
          bad=1
          ;;
        1) : ;; # 一致なし = 依存していない
        *) fail "grep が失敗した (rc=$rc, cmd=$cmd, file=$f)。検査できないので緑にしない" ;;
      esac
    done
  done
done

if [ "$bad" -ne 0 ]; then
  printf '\nheavy グループ (CI_COMMANDS_HEAVY = %s) にこれらは入っていないため、CI では\n' "$pkgs_heavy"
  printf 'command not found で落ちる。Makefile の CI_COMMANDS_HEAVY に足すか、そのテストを\n'
  printf 'rest 側 (heavy 以外のディレクトリ) へ置くこと。\n'
  printf '散文 (heredoc 等) の誤検出なら、その行に ci-group-deps: allow と書けば除外される。\n'
  exit 1
fi

printf '✓ CI グループ: sed 契約一致 / heavy の %d ファイルは rest 専用コマンド (%s) 非依存\n' \
  "$total" "$only_rest"
