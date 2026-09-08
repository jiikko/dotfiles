#!/usr/bin/env bash
# Makefile のテスト runner が、各テストを使い捨ての TMPDIR で走らせることを固定する (issue 325)。
#
# なぜ: テストとその子プロセス (テストが起動する repo のスクリプト) が
# `${TMPDIR:-/tmp}` を組み立てて `mktemp` すると、隔離が無ければ実機の
# `/var/folders/.../T` へ落ちる。macOS のそこは**起動時にしか一掃されない**
# (`/etc/periodic/` が無い。実測 2026-09-06: uptime 64 日 / 7 日超のエントリ 2,766 個)。
# `scripts/tmux_schedule_keys.sh` の `SK_TMP_PREFIX="${TMPDIR:-/tmp}/schedkeys"` だけで
# 14,147 個 (TMPDIR 全 18,297 エントリの 77%) が溜まっていた。
#
# 🚨 **脅威モデル**: 止めるのは「runner の隔離をうっかり消す / 新しい runner を隔離なしで
# 足す」形だけ。意図的な迂回は review の責務。
# 🚨 **検出しないと決めた形** (実装後に射程を突き合わせた結果):
#   - テスト側が自分で `mktemp -d` (引数なし) する形。**macOS の mktemp は TMPDIR 環境変数を
#     見ない** (実測 2026-09-08: `-t prefix` でも見ず、`_CS_DARWIN_USER_TEMP_DIR` を使う。
#     TMPDIR に従うのは `mktemp "$TMPDIR/x.XXXXXX"` のように**パスを自分で組む形だけ**)。
#     この形の後始末はテスト自身の trap の責任で、issue 305 が担当する
#   - runner が渡した TMPDIR を、テストが上書きする形 (二重に隔離されるだけで無害)
#   - Makefile 以外の経路 (CI が直接テストを叩く等) — 現状そんな経路は無い
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

while IFS= read -r entry; do
  name="${entry%%:*}"; label="${entry#*:}"
  body=$(extract_define "$name")
  if [ -z "$body" ]; then
    bad "$label ($name) を Makefile から抽出できない (define の名前が変わった? 抽出が壊れると違反 0 件で緑になる)"
    continue
  fi
  # 使い捨て dir を作って TMPDIR として渡しているか
  if grep -q 'TMPDIR="\$\$td"' <<< "$body"; then
    ok "$label: テストへ使い捨ての TMPDIR を渡している"
  else
    bad "$label ($name) がテストへ TMPDIR を渡していない (issue 325: 実機の TMPDIR へ撒く)"
  fi
  # 作った dir を消しているか (作りっぱなしだと今度は空 dir が溜まる)
  if grep -q 'rm -rf "\$\$td"' <<< "$body"; then
    ok "$label: 使い終わった TMPDIR を消している"
  else
    bad "$label ($name) が作った TMPDIR を消していない"
  fi
done <<< "$runners"

# 実機の TMPDIR をそのまま使わせる形が復活していないか (隔離を消した状態の直接の姿)
if grep -qE '^\s*.out=\$\$\(mktemp\); "\$\$0"' <<< "$(extract_define run_tests_parallel)"; then
  bad '並列 runner が隔離なしで "$0" を起動する旧形に戻っている'
fi

# isolate_env.sh 側 (tmux テストの共通隔離) にも TMPDIR が入っていること
ISO="$ROOT_DIR/tests/tmux/lib/isolate_env.sh"
if grep -q '^export TMPDIR=' "$ISO"; then
  ok "isolate_env.sh: TMPDIR を隔離している"
else
  bad "isolate_env.sh が TMPDIR を隔離していない (test_smooth_scroll.sh の自前 export を集約した先)"
fi

if [ "$fails" -gt 0 ]; then
  printf '\n✗ runner の TMPDIR 隔離: %d 件の違反\n' "$fails" >&2
  exit 1
fi
printf '✓ runner の TMPDIR 隔離: 検査 5 件すべて通過\n'
