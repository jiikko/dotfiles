#!/usr/bin/env bash
# Makefile のテスト runner が、各テストを使い捨ての TMPDIR で走らせることを固定する (issue 325)。
#
# なぜ: テストとその子プロセスが **`${TMPDIR:-/tmp}` を組み立てて** `mktemp` する形
# (例: `scripts/tmux_schedule_keys.sh` の `SK_TMP_PREFIX="${TMPDIR:-/tmp}/schedkeys"`) は、
# 隔離が無ければ実機の `/var/folders/.../T` へ落ちる。macOS のそこは**起動時にしか一掃されない**
# (`/etc/periodic/` が無い。実測 2026-09-06: uptime 64 日 / 7 日超のエントリ 2,766 個 /
# `test_schedule_keys.sh` 由来だけで 14,147 個)。
#
# 🚨 **脅威モデル**: 止めるのは「runner の隔離をうっかり消す / 順序を崩す / 新しい runner を
# 隔離なしで足す」形だけ。意図的な迂回は review の責務。
# 🚨 **検出しないと決めた形** (実装後に射程を突き合わせた結果):
#   - テスト側が自分で `mktemp -d` (引数なし) する形。**macOS の mktemp は TMPDIR 環境変数を
#     見ない** (実測 2026-09-08: `-t prefix` でも見ず、`_CS_DARWIN_USER_TEMP_DIR` を使う。
#     TMPDIR に従うのは `mktemp "$TMPDIR/x.XXXXXX"` のように**パスを自分で組む形だけ**)。
#     この形の後始末はテスト自身の trap の責任で、issue 305 が担当する
#   - runner が渡した TMPDIR を、テストが上書きする形 (二重に隔離されるだけで無害)
#   - **Makefile 以外の経路**: `.github/workflows/src_glogx.yml` の no-real-commands job が
#     `tests/glogx/test_no_real_commands_in_tests.sh` を直接叩く。runner は使い捨てなので
#     実害は無いが、「そんな経路は無い」と書くのは嘘なので明記しておく
#     (恒久化するなら `make test-dir` 経由へ寄せる)
# shellcheck disable=SC2016  # Makefile のリテラル `$$td` / `$$0` を探すので単一引用符が正しい
set -uo pipefail
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
MAKEFILE="$ROOT_DIR/Makefile"
fails=0
bad() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }
ok() { printf '✓ %s\n' "$1"; }

# 対象の runner (define 名 → 人が読む名前)。空白で割れないよう改行区切りで持つ
runners=$(printf '%s\n' 'run_tests:直列 runner' 'run_tests_parallel:並列 runner')

# canary: 本走査と同じ抽出関数を通す。抽出が壊れて 0 件になったら「違反なし」で緑になるのを塞ぐ
extract_define() { # extract_define <define 名>
  awk -v name="$1" '
    $0 == "define " name { inside = 1; next }
    inside && $0 == "endef" { inside = 0 }
    inside { print }
  ' "$MAKEFILE"
}

# 行番号つきで最初のヒットを返す (見つからなければ空)。順序の比較に使う
line_of() { # line_of <本文> <固定文字列>
  grep -nF -- "$2" <<< "$1" | head -1 | cut -d: -f1
}

while IFS= read -r entry; do
  name="${entry%%:*}"; label="${entry#*:}"
  body=$(extract_define "$name")
  if [ -z "$body" ]; then
    bad "$label ($name) を Makefile から抽出できない (define の名前が変わった? 抽出が壊れると違反 0 件で緑になる)"
    continue
  fi

  # ① スイート単位の親 dir を作り、失敗したら止める (fail-open にしない)
  if grep -qF 'suite_td=$$(mktemp -d)' <<< "$body"; then
    ok "$label: スイート単位の使い捨て dir を作っている"
  else
    bad "$label ($name) が使い捨ての親 dir を作っていない (issue 325)"
  fi
  if grep -qF '隔離なしで走らせない' <<< "$body"; then
    ok "$label: mktemp -d の失敗で止まる (fail-open にしない)"
  else
    bad "$label ($name) が mktemp -d の失敗を素通りさせている (空の TMPDIR で走ると隔離が消える)"
  fi

  # ② 中断でも回収する trap
  if grep -qF "trap 'rm -rf \"\$\$suite_td\"' EXIT INT TERM HUP" <<< "$body"; then
    ok "$label: 中断 (INT/TERM/HUP) でも親 dir を回収する"
  else
    bad "$label ($name) に中断時の trap が無い (Ctrl-C / CI キャンセルでツリーが残り、変更前より悪化する)"
  fi

  # ③ テストへ渡している
  if grep -qF 'TMPDIR="$$td"' <<< "$body"; then
    ok "$label: テストへ使い捨ての TMPDIR を渡している"
  else
    bad "$label ($name) がテストへ TMPDIR を渡していない (実機の TMPDIR へ撒く)"
  fi

  # ④ 🚨 **順序**: 渡すのが先、消すのが後。入れ替えると全テストが存在しない dir を受け取る
  pass_at=$(line_of "$body" 'TMPDIR="$$td"')
  rm_at=$(line_of "$body" 'rm -rf "$$td"')
  if [ -z "$rm_at" ]; then
    bad "$label ($name) がテストごとの dir を消していない"
  elif [ -n "$pass_at" ] && [ "$pass_at" -lt "$rm_at" ]; then
    ok "$label: TMPDIR を渡してから消している (順序)"
  else
    bad "$label ($name) が dir を消してからテストを走らせている (存在しない TMPDIR を渡すことになる)"
  fi
done <<< "$runners"

# 実機の TMPDIR をそのまま使わせる形が復活していないか (隔離を消した状態の直接の姿)
if grep -qE '^\s*.out=\$\$\(mktemp\); "\$\$0"' <<< "$(extract_define run_tests_parallel)"; then
  bad '並列 runner が隔離なしで "$0" を起動する旧形に戻っている'
fi

# isolate_env.sh 側 (tmux テストの共通隔離)。export だけでなく **mkdir の対象**にも入っていること
# (mkdir から外すと、export は残るのに実体が無い TMPDIR を配ることになる)
ISO="$ROOT_DIR/tests/tmux/lib/isolate_env.sh"
if grep -q '^export TMPDIR=' "$ISO"; then
  ok "isolate_env.sh: TMPDIR を隔離している"
else
  bad "isolate_env.sh が TMPDIR を隔離していない (test_smooth_scroll.sh の自前 export を集約した先)"
fi
if grep -qE '^mkdir -p .*"\$TMPDIR"' "$ISO"; then
  ok "isolate_env.sh: TMPDIR の実体を作っている"
else
  bad "isolate_env.sh が \$TMPDIR を mkdir していない (export だけでは実体が無い)"
fi

if [ "$fails" -gt 0 ]; then
  printf '\n✗ runner の TMPDIR 隔離: %d 件の違反\n' "$fails" >&2
  exit 1
fi
printf '✓ runner の TMPDIR 隔離: 検査 12 件すべて通過\n'
