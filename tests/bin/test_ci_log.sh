#!/usr/bin/env bash
# bin/ci-log の既定経路 (引数なし) が gh の失敗を「CI は緑」に化けさせないことを、偽の gh で固定する (issue 610)。
#
# 守るもの: `gh run list` が失敗したら非 0 で止まり「失敗した run はありません」と言わない (2 か所の gh のどちらでも) /
# 失敗した run があればその失敗ログを取りに行く / bash 3.2 (/bin/bash) でも動く (mapfile を使わない) /
# -i は commit を完全な SHA に展開して --commit に渡し、一覧の先頭ではなく headSha が一致する run の ID だけを返す (issue 634)。
# 偽の gh は --jq を解釈しないので、絞り込み後の行を環境変数でそのまま返す。
set -euo pipefail
unset CDPATH
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUT="$ROOT_DIR/bin/ci-log"
fails=0
passes=0
ok()   { printf '✓ %s\n' "$1"; passes=$((passes + 1)); }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-ci-log.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# ci-log は HEAD の sha を引くので、使い捨ての repo の中で走らせる
REPO="$WORK/repo"
git init -q "$REPO"
git -C "$REPO" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m init

mkdir -p "$WORK/fakebin"
cat > "$WORK/fakebin/gh" <<'SH'
#!/bin/sh
echo "gh $*" >> "$GH_LOG"
case "$1 $2" in
  "run list")
    case "$*" in
      *"--workflow"*) [ -n "${GH_FAIL_ID:-}" ] && { echo "HTTP 401: Bad credentials" >&2; exit 1; }
                      [ -n "${GH_ID_ROWS:-}" ] && printf '%b\n' "$GH_ID_ROWS"; exit 0 ;;
      *"--limit 40"*) [ -n "${GH_FAIL_FIRST:-}" ] && { echo "HTTP 401: Bad credentials" >&2; exit 1; }
                      [ -n "${GH_FAILED_ROWS:-}" ] && printf '%b\n' "$GH_FAILED_ROWS"; exit 0 ;;
      *"--limit 60"*) [ -n "${GH_FAIL_SECOND:-}" ] && { echo "HTTP 401: Bad credentials" >&2; exit 1; }
                      [ -n "${GH_RECENT_ROWS:-}" ] && printf '%b\n' "$GH_RECENT_ROWS"; exit 0 ;;
      *) echo "(run 一覧)"; exit 0 ;;
    esac ;;
  "run view") echo "失敗ログ of $3"; exit 0 ;;
  "api repos/{owner}/{repo}/commits/"*)   # -i が push 済みかを聞く。GH_API=missing / fail で GitHub に無い / 失敗を演じる
    case "${GH_API:-found}" in
      missing) echo '{"message":"No commit found"}'; echo "gh: No commit found for SHA: ${2##*/} (HTTP 422)" >&2; exit 1 ;;
      fail)    echo "error connecting to api.github.com" >&2; exit 1 ;;
      *)       echo "${2##*/}"; exit 0 ;;
    esac ;;
esac
exit 0
SH
chmod +x "$WORK/fakebin/gh"

for sh in bash /bin/bash; do
  # $1=名前、残り=ci-log の引数。環境は呼び出し側が渡す。RC / OUT / ERR / LOG
  run_case() {
    local name
    name="$1.$(basename "$sh")"; shift
    LOG="$WORK/$name.gh"; : > "$LOG"
    OUT="$WORK/$name.out"; ERR="$WORK/$name.err"
    RC=0
    (cd "$REPO" && GH_LOG="$LOG" PATH="$WORK/fakebin:$PATH" "$sh" "$SUT" "$@" > "$OUT" 2> "$ERR") || RC=$?
  }

  GH_FAIL_FIRST=1 run_case first_fail
  if [[ $RC -ne 0 ]] && ! grep -q 'ありません' "$ERR" && grep -q 'gh run list に失敗' "$ERR"; then
    ok "[$sh] 1 本目の gh の失敗で止まる"
  else
    fail "[$sh] 1 本目の gh の失敗: rc=$RC $(cat "$ERR")"
  fi

  GH_FAIL_SECOND=1 run_case second_fail
  if [[ $RC -ne 0 ]] && grep -q 'gh run list に失敗' "$ERR"; then
    ok "[$sh] 2 本目 (HEAD より前の赤の走査) の gh の失敗で止まる"
  else
    fail "[$sh] 2 本目の gh の失敗: rc=$RC $(cat "$ERR")"
  fi

  GH_FAILED_ROWS='111\tLint\n222\tTests' run_case has_failed
  if [[ $RC -eq 0 ]] && grep -qx '失敗ログ of 111' "$OUT" && grep -qx '失敗ログ of 222' "$OUT"; then
    ok "[$sh] 失敗した run を全部取りに行く"
  else
    fail "[$sh] 失敗した run: rc=$RC out=$(tr '\n' ' ' < "$OUT") err=$(cat "$ERR")"
  fi

  # HEAD より前の赤: 空白を含む workflow 名の最新 run が failure → 🚨 で名指しする。同じ workflow の古い success は見ない
  head_sha="$(git -C "$REPO" rev-parse HEAD)"
  GH_RECENT_ROWS="src glogx lint\t$head_sha\tfailure\t333\nsrc glogx lint\t$head_sha\tsuccess\t300" run_case stale_red
  if [[ $RC -eq 0 ]] && grep -q '🚨 HEAD より前に' "$ERR" && grep -q 'src glogx lint.*(ci-log 333)' "$ERR"; then
    ok "[$sh] HEAD より前の赤を名指しする"
  else
    fail "[$sh] HEAD より前の赤: rc=$RC $(cat "$ERR")"
  fi

  run_case all_green
  if [[ $RC -eq 0 ]] && grep -q '失敗した run はありません' "$ERR"; then
    ok "[$sh] 失敗が無ければそう言う"
  else
    fail "[$sh] 失敗なし: rc=$RC $(cat "$ERR")"
  fi
  if grep -q -- "--commit $head_sha --limit 40" "$LOG"; then
    ok "[$sh] HEAD の失敗 run を --commit (完全な SHA) で絞って取る"
  else
    fail "[$sh] 既定の経路が --commit で絞っていない: $(cat "$LOG")"
  fi

  # -i (issue 634): 一覧の先頭が別の commit の古い run でも、headSha が一致する run の ID だけを返す
  other_sha=0123456789abcdef0123456789abcdef01234567
  GH_ID_ROWS="999\t$other_sha\n555\t$head_sha" run_case id_stale_first -i Tests
  if [[ $RC -eq 0 && "$(cat "$OUT")" == 555 ]]; then
    ok "[$sh] -i は先頭の別の commit の run を飛ばして一致する run の ID を返す"
  else
    fail "[$sh] -i の照合: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  GH_ID_ROWS="555\t$head_sha" run_case id_short_sha -i Tests "$(git -C "$REPO" rev-parse --short HEAD)"
  if [[ $RC -eq 0 ]] && grep -q -- "--commit $head_sha --workflow Tests" "$LOG"; then
    ok "[$sh] -i は短縮 SHA を完全な SHA に展開して --commit に渡す"
  else
    fail "[$sh] -i の SHA の展開: rc=$RC $(cat "$LOG")"
  fi
  GH_ID_ROWS="999\t$other_sha" run_case id_none -i Tests
  if [[ $RC -eq 4 && ! -s "$OUT" ]] && grep -q 'run がまだありません' "$ERR"; then
    ok "[$sh] -i は push 済みの commit で一致する run が無ければ rc=4 で ID を出さない"
  else
    fail "[$sh] -i の該当なし: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  GH_ID_ROWS="999\t$other_sha" GH_API=missing run_case id_unpushed -i Tests
  if [[ $RC -eq 5 && ! -s "$OUT" ]] && grep -q 'push していない' "$ERR"; then
    ok "[$sh] -i は GitHub に無い commit (HTTP 422) を rc=5 で分ける (待っても来ない)"
  else
    fail "[$sh] -i の push していない commit: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  GH_ID_ROWS="999\t$other_sha" GH_API=fail run_case id_api_fail -i Tests
  if [[ $RC -eq 1 && ! -s "$OUT" ]] && grep -q 'gh run list に失敗' "$ERR" && ! grep -q 'push していない' "$ERR"; then
    ok "[$sh] -i は push 済みかを聞けなければ rc=1 (push していないとは言わない)"
  else
    fail "[$sh] -i の API の失敗: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  GH_ID_ROWS="555\t$head_sha\n444\t$head_sha" run_case id_multi -i Tests
  if [[ $RC -eq 0 && "$(cat "$OUT")" == 555 ]] && grep -q '2 本' "$ERR"; then
    ok "[$sh] -i は同じ commit に複数あれば先頭 (作成が新しいもの) を返し、そう言う"
  else
    fail "[$sh] -i の複数: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  GH_FAIL_ID=1 run_case id_gh_fail -i Tests
  if [[ $RC -eq 1 && ! -s "$OUT" ]] && grep -q 'gh run list に失敗' "$ERR"; then
    ok "[$sh] -i は gh の失敗で rc=1 (該当なしと区別する)"
  else
    fail "[$sh] -i の gh の失敗: rc=$RC out=$(cat "$OUT") err=$(cat "$ERR")"
  fi
  run_case id_usage -i
  rc_usage=$RC
  run_case id_bad_commit -i Tests no-such-ref
  if [[ $rc_usage -eq 2 && $RC -eq 2 ]] && grep -q 'commit を解決できません' "$ERR"; then
    ok "[$sh] -i は workflow の名前が無いか commit を解決できなければ rc=2"
  else
    fail "[$sh] -i の引数の誤り: usage rc=$rc_usage / bad commit rc=$RC $(cat "$ERR")"
  fi
done

if (( fails )); then
  echo "FAIL: $fails 件" >&2
  exit 1
fi
echo "OK ci-log ($passes 件)"
