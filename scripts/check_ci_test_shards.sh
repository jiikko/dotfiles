#!/usr/bin/env bash
# CI (.github/workflows/tests.yml) の TEST_SHARD の組 (rest の並列腕を複数の job に分ける。Makefile の test_shard_filter) を検査する:
# rest の並列腕の make-target に字で書かれ、1/n〜n/n を欠けずに 1 回ずつ持ち、test-runtime-rest がちょうど 1 つであるか。
# 組が欠けると、その組に振られたテストはどの job でも走らず、残りの job は緑のまま終わる。
#
# 使い方: scripts/check_ci_test_shards.sh [WORKFLOW]   (WORKFLOW を省くと .github/workflows/tests.yml。make test-ci-test-shards が呼ぶ)
# rc: 0 = 揃っている (TEST_SHARD が無いときも 0) / 1 = 揃っていない・読めない
#
# 読むのは make-target の行に字で書いた TEST_SHARD=k/n だけ。matrix の変数・env: で渡す形は読めないので落とす
# (env: は Makefile の TEST_SHARD := が上書きして効かず、両方の job が全部を走らせる)。
# 検出しない形: matrix の exclude: や job の if: で組の entry が走らない workflow・組の entry の依存の宣言
# (deps-group / needs-nvim / needs-go) が rest と違う workflow・組の無い rest の entry を組と並べた workflow (2 回走るだけ)。
# make-target の値を引用符や flow 形式で書いた workflow は落ちる側 (うるさい赤) になる。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }
fail() { printf '✗ %s\n' "$1"; exit 1; }

workflow="${1:-.github/workflows/tests.yml}"
[ -f "$workflow" ] && [ -r "$workflow" ] || fail "$workflow を読めない (検査できないので緑にしない)"
body="$(grep -v '^[[:space:]]*#' "$workflow" | sed 's/[[:space:]]#.*$//')"
mentions="$(grep -o 'TEST_SHARD' <<< "$body" | wc -l | tr -d ' ')"
shard_lines="$(grep -E 'TEST_SHARD=[0-9]+/[0-9]+' <<< "$body")"
[ "$mentions" -eq "$(grep -oE 'TEST_SHARD=[0-9]+/[0-9]+' <<< "$body" | wc -l | tr -d ' ')" ] \
  || fail "$workflow が TEST_SHARD を make-target の TEST_SHARD=k/n 以外の書き方 (matrix の変数・env など) で渡している。この検査は組の欠けを読めないので、make-target に字で書く"
# 組は rest の並列腕 (test-discovered-rest-parallel。test-runtime-rest はそれを含む) にだけ掛ける。
# 別の target に掛けると、番号が揃っていても rest の並列腕の一部がどの job でも走らない
if [ -n "$shard_lines" ]; then
  stray="$(grep -vE '^[[:space:]]*(-[[:space:]]+)?make-target:[[:space:]]+(test-runtime-rest|test-discovered-rest-parallel)[[:space:]]+TEST_SHARD=[0-9]+/[0-9]+[[:space:]]*$' <<< "$shard_lines")"
  [ -z "$stray" ] || fail "$workflow の TEST_SHARD が rest の並列腕以外に掛かっている: $(printf '%s' "$stray" | tr -s ' ')"
  # 直列腕・構文・bats は test-runtime-rest の組だけが走らせる。全部の組を test-discovered-rest-parallel にすると CI から消える
  [ "$(grep -cE 'make-target:[[:space:]]+test-runtime-rest[[:space:]]' <<< "$shard_lines")" -eq 1 ] \
    || fail "$workflow の TEST_SHARD の組のうち test-runtime-rest はちょうど 1 つにする (0 なら直列腕・構文・bats がどの job でも走らず、2 以上なら 2 回走る)"
fi
shards="$(grep -oE 'TEST_SHARD=[0-9]+/[0-9]+' <<< "$body" | sed 's/^TEST_SHARD=//')"
shard_note="TEST_SHARD なし"
if [ -n "$shards" ]; then
  ns="$(printf '%s\n' "$shards" | cut -d/ -f2 | sort -u)"
  [ "$(grep -c . <<< "$ns")" -eq 1 ] || fail "$workflow の TEST_SHARD の組の数がそろっていない: $(printf '%s' "$shards" | tr '\n' ' ')"
  want="$(seq 1 "$ns" | tr '\n' ' ')"
  got="$(printf '%s\n' "$shards" | cut -d/ -f1 | sort -n | tr '\n' ' ')"
  [ "$got" = "$want" ] || fail "$workflow の TEST_SHARD は 1/${ns}〜${ns}/${ns} を 1 回ずつ持つ (今: $(printf '%s' "$shards" | tr '\n' ' ')。欠けた組のテストはどの job でも走らない)"
  shard_note="TEST_SHARD は 1/${ns}〜${ns}/${ns}"
fi

printf '✓ CI の組: %s\n' "$shard_note"
