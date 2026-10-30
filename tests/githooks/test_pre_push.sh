#!/usr/bin/env bash
# githooks/pre-push を、使い捨ての repo から bare の remote へ実際に push して検査する。
# fixture は repo の HEAD のスナップショット (`git archive`) なので、本物の issues/ と
# tests/issues/ がそのまま入る (件数の下限を持つ検査もそのまま通る)。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK_DIR="$ROOT_DIR/githooks"

# 🚨 hook から起動された make test (上位の pre-push 等) が GIT_DIR を継承していると、下の
# `git -C` は -C の先ではなく継承した repo を触る (sandbox-real-destructive-test-apis.md)
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_OBJECT_DIRECTORY GIT_COMMON_DIR

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

# shellcheck source=../lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"

fails=0
pass() { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

work="$T/work"
remote="$T/remote.git"
mkdir -p "$work"
git -C "$ROOT_DIR" archive HEAD | tar -x -C "$work"
git init -q --bare "$remote"
git -C "$work" init -q -b master
git -C "$work" config user.name test
git -C "$work" config user.email test@example.invalid
git -C "$work" config commit.gpgsign false
# 作業ツリーの (未コミットかもしれない) hook を検査する
git -C "$work" config core.hooksPath "$HOOK_DIR"
git -C "$work" remote add origin "$remote"
# 8 番のケース用: issues/ の外のファイルへの相対リンク (リンク先を動かすとリンクが切れる)
first_issue=$(find "$work/issues" -maxdepth 1 -type f -name '[0-9][0-9][0-9]*-*.md' | sort | head -1)
printf '# fixture\n' >"$work/docs/pre-push-fixture.md"
printf '\n[fixture](../docs/pre-push-fixture.md)\n' >>"$first_issue"
git -C "$work" add -A
git -C "$work" commit -q --no-verify -m base

remote_head() { git -C "$remote" rev-parse -q --verify refs/heads/master || echo none; }

# push を打って rc と stderr を控える
push() { # $1=名前 残り=git push の追加引数
  local name="$1"; shift
  set +e
  git -C "$work" push origin "$@" >"$T/$name.out" 2>"$T/$name.err"
  echo $? >"$T/$name.rc"
  set -e
}

# 0. hook の一覧 (回す + 外す) が tests/issues/test_*.sh の実在と一致する
listed=$("$HOOK_DIR/pre-push" --list-checks | awk '$1 == "check" || $1 == "excluded" {print $2}' | sort)
actual=$(find "$ROOT_DIR/tests/issues" -maxdepth 1 -type f -name 'test_*.sh' | sed 's|.*/||; s|\.sh$||' | sort)
if [ -n "$actual" ] && [ "$listed" = "$actual" ]; then
  pass "hook の一覧が tests/issues/ の実在と一致する ($(printf '%s\n' "$actual" | wc -l | tr -d ' ') 本)"
else
  fail "hook の一覧と tests/issues/ が食い違う (新しい検査を CHECKS か EXCLUDED に足す):"
  diff <(printf '%s\n' "$listed") <(printf '%s\n' "$actual") >&2 || true
fi

# --- 既存の番号とまだ無い番号を実物から取る ---
existing=$(find "$work/issues" -maxdepth 1 -type f -name '[0-9][0-9][0-9]*-*.md' | sed 's|.*/||' | sort | head -1)
existing_num=${existing%%-*}
max=$(find "$work/issues" -type f -name '[0-9][0-9][0-9]*-*.md' | sed 's|.*/||; s|-.*||' | sort -n | tail -1)
free_num=$(printf '%03d' $((10#$max + 1)))
[ -n "$existing_num" ] && [ -n "$free_num" ] || { echo "✗ fixture から番号を取れない" >&2; exit 1; }

# 1. 新しい ref (issues/ を含む) の push: 検査を全部回して通る
push base HEAD:refs/heads/master
if [ "$(cat "$T/base.rc")" = 0 ] && [ "$(remote_head)" = "$(git -C "$work" rev-parse HEAD)" ]; then
  pass "新しい ref の push は検査を回して通る"
else
  fail "新しい ref の push が通らない: $(cat "$T/base.err")"
fi
n_ok=$(grep -c '^pre-push:   ✓ ' "$T/base.err" || true)
n_checks=$("$HOOK_DIR/pre-push" --list-checks | grep -c '^check ')
if grep -q '整合検査を .* 本回す' "$T/base.err" && [ "$n_ok" = "$n_checks" ] && [ "$n_checks" -gt 0 ]; then
  pass "回した検査が全部出力に出る ($n_ok / $n_checks)"
else
  fail "回した検査の出力が足りない ($n_ok / $n_checks): $(cat "$T/base.err")"
fi
base=$(git -C "$work" rev-parse HEAD)

# ケースごとに work と remote を base へ戻す (前のケースが外れて remote が進んでも、後ろのケースへ
# 連鎖させない。連鎖すると non-fast-forward で落ち、set -e が後ろのケースを黙って打ち切る)
reset_case() {
  git -C "$work" reset -q --hard "$base"
  git -C "$work" clean -fdq
  git -C "$remote" update-ref refs/heads/master "$base"
  git -C "$work" update-ref refs/remotes/origin/master "$base"
}

# 2. 番号の重複を足す push は止まり、remote は動かない
reset_case
before=$(remote_head)
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-dup.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m dup
push dup HEAD:refs/heads/master
if [ "$(cat "$T/dup.rc")" != 0 ] && [ "$(remote_head)" = "$before" ] && grep -q '✗ test_issue_numbers_unique' "$T/dup.err"; then
  pass "番号の重複は push を止める"
else
  fail "番号の重複で止まらない (rc=$(cat "$T/dup.rc")): $(cat "$T/dup.err")"
fi

# 3. 位置からの相対になっていないリンク (pending に置いて ../ が無い) は止まる
reset_case
mkdir -p "$work/issues/pending"
printf '# %s (chore): x\n\n起票日: 2026-09-28\n\n親: [a](%s)\n' "$free_num" "$existing" \
  >"$work/issues/pending/${free_num}-chore-link.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m link
push link HEAD:refs/heads/master
if [ "$(cat "$T/link.rc")" != 0 ] && [ "$(remote_head)" = "$before" ] && grep -q '✗ test_issue_links_valid' "$T/link.err"; then
  pass "リンク切れは push を止める"
else
  fail "リンク切れで止まらない (rc=$(cat "$T/link.rc")): $(cat "$T/link.err")"
fi

# 4. 検査するのは push する commit。作業ツリーの未追跡の重複は見ない
reset_case
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-untracked.md"
printf '\n- 追記\n' >>"$work/issues/$existing"
git -C "$work" commit -q --no-verify -m append -- "issues/$existing"
push clean HEAD:refs/heads/master
if [ "$(cat "$T/clean.rc")" = 0 ] && [ "$(remote_head)" = "$(git -C "$work" rev-parse HEAD)" ]; then
  pass "作業ツリーの未追跡ファイルは検査に入らない"
else
  fail "作業ツリーの未追跡ファイルで止まった: $(cat "$T/clean.err")"
fi

# 5. issues/ を触らない push は、remote が既に壊れていても止めない (検査を回さない)
reset_case
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-dup.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m dup
git -C "$work" push -q --no-verify origin HEAD:refs/heads/master
printf '\nx\n' >>"$work/README.md"
git -C "$work" commit -q --no-verify -m readme -- README.md
push other HEAD:refs/heads/master
if [ "$(cat "$T/other.rc")" = 0 ] && grep -q '削除・移動も無い push なので issue の整合検査は省略' "$T/other.err" \
  && ! grep -q '整合検査を .* 本回す' "$T/other.err"; then
  pass "issues/ を触らない push は検査を省いて通る"
else
  fail "issues/ を触らない push が止まった / 検査を回した: $(cat "$T/other.err")"
fi

# 6. 同じ状態で issues/ を触る push は、壊したのが今回でなくても止まる
printf '\n- 追記 2\n' >>"$work/issues/$existing"
git -C "$work" commit -q --no-verify -m append2 -- "issues/$existing"
before=$(remote_head)
push preexisting HEAD:refs/heads/master
if [ "$(cat "$T/preexisting.rc")" != 0 ] && [ "$(remote_head)" = "$before" ]; then
  pass "issues/ を触る push は、既存の壊れでも止まる"
else
  fail "既存の壊れを抱えたまま issues/ を触る push が通った: $(cat "$T/preexisting.err")"
fi

# 7. merge commit の中だけで重複を足す (側のブランチは issues/ を触らない) push も止まる
reset_case
git -C "$work" checkout -q -b side
printf '\ny\n' >>"$work/README.md"
git -C "$work" commit -q --no-verify -m side -- README.md
git -C "$work" checkout -q master
git -C "$work" merge -q --no-ff --no-commit side
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-dup.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m merge
before=$(remote_head)
push merge HEAD:refs/heads/master
if [ "$(cat "$T/merge.rc")" != 0 ] && [ "$(remote_head)" = "$before" ]; then
  pass "merge commit の中だけの重複も push を止める"
else
  fail "merge commit の中だけの重複が通った: $(cat "$T/merge.err")"
fi
git -C "$work" branch -q -D side

# 8. issue がリンクしている issues/ の外のファイルを動かす push も止まる
reset_case
git -C "$work" mv docs/pre-push-fixture.md docs/pre-push-fixture-moved.md
git -C "$work" commit -q --no-verify -m mvdoc
before=$(remote_head)
push mvdoc HEAD:refs/heads/master
if [ "$(cat "$T/mvdoc.rc")" != 0 ] && [ "$(remote_head)" = "$before" ] && grep -q '✗ test_issue_links_valid' "$T/mvdoc.err"; then
  pass "issue のリンク先を動かす push も止める"
else
  fail "issue のリンク先を動かす push が通った (rc=$(cat "$T/mvdoc.rc")): $(cat "$T/mvdoc.err")"
fi

# 9. export-ignore の attributes で重複を検査から隠せない
reset_case
printf 'issues/%s-chore-dup.md export-ignore\n' "$existing_num" >"$work/.git/info/attributes"
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-dup.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m dupattr
before=$(remote_head)
push attr HEAD:refs/heads/master
rm -f "$work/.git/info/attributes"
if [ "$(cat "$T/attr.rc")" != 0 ] && [ "$(remote_head)" = "$before" ]; then
  pass "export-ignore の attributes でも重複を見落とさない"
else
  fail "export-ignore で隠した重複が通った: $(cat "$T/attr.err")"
fi

# 10. issues/ の外の削除・移動だけの push は、リンクの検査だけを回す (master が別の理由で赤くても止めない)
reset_case
cp "$work/issues/$existing" "$work/issues/${existing_num}-chore-dup.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m dup
git -C "$work" push -q --no-verify origin HEAD:refs/heads/master
git -C "$work" mv README.md README-moved.md
git -C "$work" commit -q --no-verify -m mvreadme
push mvonly HEAD:refs/heads/master
n_link=$("$HOOK_DIR/pre-push" --list-checks | grep -c '^link ' || true)
if [ "$(cat "$T/mvonly.rc")" = 0 ] && grep -q "消す・動かす push なので issue の整合検査を $n_link 本回す" "$T/mvonly.err" \
  && [ "$n_link" -gt 0 ] && ! grep -q 'test_issue_numbers_unique' "$T/mvonly.err"; then
  pass "issues/ の外の移動だけの push はリンクの検査だけを回す ($n_link 本)"
else
  fail "issues/ の外の移動だけの push が止まった / 別の検査を回した: $(cat "$T/mvonly.err")"
fi

# 11. hook の展開先 (mktemp の論理パス。macOS では /var → /private/var) でもリンク検査の件数の下限が効く
# 論理パスと実パスが違う置き場を symlink で必ず作る (TMPDIR が実パスの環境でも、旧版の穴が見えるように)
mkdir -p "$T/mini-real"
ln -s "$T/mini-real" "$T/mini"
mini="$T/mini"
if [ "$(cd "$mini" && pwd)" = "$(cd "$mini" && pwd -P)" ]; then
  fail "fixture の前提が崩れた: $mini の論理パスと実パスが同じ"
fi
mkdir -p "$mini/tests/issues" "$mini/issues"
cp "$ROOT_DIR/tests/issues/test_issue_links_valid.sh" "$mini/tests/issues/"
printf '# 001 (chore): x\n' >"$mini/issues/001-chore-x.md"
if (cd / && bash "$mini/tests/issues/test_issue_links_valid.sh") >"$T/mini.out" 2>&1; then
  fail "展開先で件数の下限が効かない (md 1 本で緑): $(cat "$T/mini.out")"
elif grep -q '走査件数が少なすぎる' "$T/mini.out"; then
  pass "展開先でもリンク検査の件数の下限が効く"
else
  fail "展開先のリンク検査が別の理由で落ちた: $(cat "$T/mini.out")"
fi

# 12. push する commit から検査が 1 本欠けたら止め、欠けたものの名前と残りの結果を正しい名前で出す
# 欠けさせるのは真ん中の検査 (並列に起こした検査と結果の対応がずれる退行は、末尾以外が欠けたときに出る)
reset_case
mid=$("$HOOK_DIR/pre-push" --list-checks | sed -n 's/^check //p' | sed -n 3p)
[ -n "$mid" ] || { echo "✗ --list-checks から 3 本目の検査を取れない" >&2; exit 1; }
git -C "$work" rm -q "tests/issues/$mid.sh"
printf '\n- 追記 3\n' >>"$work/issues/$existing"
git -C "$work" commit -q --no-verify -am rmcheck
push rmcheck HEAD:refs/heads/master
n_ok=$(grep -c '^pre-push:   ✓ ' "$T/rmcheck.err" || true)
if [ "$(cat "$T/rmcheck.rc")" != 0 ] && [ "$(remote_head)" = "$base" ] \
  && grep -q "✗ $mid が push する commit に無い" "$T/rmcheck.err" && [ "$n_ok" = $((n_checks - 1)) ] \
  && ! grep -q "✓ $mid\$" "$T/rmcheck.err"; then
  pass "欠けた検査があると止め、残り $n_ok 本の結果を正しい名前で出す"
else
  fail "欠けた検査の扱いが違う (rc=$(cat "$T/rmcheck.rc") / ✓ $n_ok 本): $(cat "$T/rmcheck.err")"
fi

# 13. 後ろの方の検査 1 本だけが落ちたとき、✗ はその検査の名前に付き、他は ✓ のまま
# (並列に起こした検査と結果の名前の対応がずれる退行は、ケース 2 / 3 の先頭 2 本だけでは見えない)
reset_case
printf '# %s (human): x\n\n起票日: 2026-10-21\n' "$free_num" >"$work/issues/${free_num}-human-no-deadline.md"
git -C "$work" add issues && git -C "$work" commit -q --no-verify -m nodeadline
push nodeadline HEAD:refs/heads/master
n_ng=$(grep -c '^pre-push:   ✗ ' "$T/nodeadline.err" || true)
if [ "$(cat "$T/nodeadline.rc")" != 0 ] && [ "$(remote_head)" = "$base" ] \
  && grep -q '^pre-push:   ✗ test_human_issues_have_deadline$' "$T/nodeadline.err" && [ "$n_ng" = 1 ]; then
  pass "落ちた検査の名前に ✗ が付き、他は ✓ のまま"
else
  fail "落ちた検査と ✗ の名前が合わない (rc=$(cat "$T/nodeadline.rc") / ✗ $n_ng 本): $(cat "$T/nodeadline.err")"
fi

# 14. hook が中断されたら、起こした検査とその子を残さない (検査は別のプロセスグループで走るので、hook の trap が止める)
# 検査の 1 本を「子を起こして待つだけ」の偽物に差し替え、子が走っている間に hook へ TERM を送る
reset_case
first=$("$HOOK_DIR/pre-push" --list-checks | sed -n 's/^check //p' | sed -n 1p)
cat >"$work/tests/issues/$first.sh" <<FAKE
#!/usr/bin/env bash
sleep 60 & # sleep-ok: dummy: 中断で止まるかを見る子。止められなければ残る
printf '%s %s\\n' "\$\$" "\$!" >"$T/fake.pids"
wait
FAKE
printf '\n- 追記 4\n' >>"$work/issues/$existing"
git -C "$work" commit -q --no-verify -am fakecheck
printf 'refs/heads/master %s refs/heads/master %s\n' "$(git -C "$work" rev-parse HEAD)" "$base" >"$T/int.stdin"
(cd "$work" && exec bash "$HOOK_DIR/pre-push" origin <"$T/int.stdin" >"$T/int.out" 2>"$T/int.err") &
hook_pid=$!
fake_ready() { [ -s "$T/fake.pids" ]; }
if ! tt_wait_until fake_ready; then
  fail "偽の検査が起動しない: $(cat "$T/int.err")"
  kill -TERM "$hook_pid"; wait "$hook_pid" || true
else
  read -r fake_pid fake_child <"$T/fake.pids"
  kill -TERM "$hook_pid"
  wait "$hook_pid" || true
  fake_gone() { ! kill -0 "$fake_pid" 2>/dev/null && ! kill -0 "$fake_child" 2>/dev/null; }
  if tt_wait_until fake_gone; then
    pass "中断された hook は起こした検査とその子を残さない"
  else
    fail "中断された hook の後に検査が残った (pid $fake_pid / 子 $fake_child): $(cat "$T/int.err")"
    kill -TERM "$fake_child" "$fake_pid" 2>/dev/null || true # 後始末は自分が控えた pid だけ
  fi
fi

# 15. linked worktree からの push でも、検査には git の場所の環境変数を渡さない。git はこのとき hook に
#     GIT_DIR=<本体>/.git/worktrees/<名前> を付けるので、外さないと検査の中の git が展開先ではなく push 中の repo を見る。
#     今の検査はどれも git を呼ばず漏れが結果に出ないので、検査の 1 本を「漏れていたら落ちる」偽物に差し替えて確かめる
reset_case
git -C "$work" worktree add -q -b wt15 "$T/wt15" "$base"
first=$("$HOOK_DIR/pre-push" --list-checks | sed -n 's/^check //p' | sed -n 1p)
cat >"$T/wt15/tests/issues/$first.sh" <<'FAKE'
#!/usr/bin/env bash
leaked=$(env | grep -oE '^(GIT_DIR|GIT_WORK_TREE|GIT_INDEX_FILE|GIT_COMMON_DIR|GIT_PREFIX)=' || true)
[ -z "$leaked" ] || { echo "git の場所の環境変数が検査に漏れた: $leaked"; exit 1; }
FAKE
printf '\n- 追記 15\n' >>"$T/wt15/issues/$existing"
git -C "$T/wt15" commit -q --no-verify -am wt15
set +e
git -C "$T/wt15" push origin HEAD:refs/heads/wt15 >"$T/wt15.out" 2>"$T/wt15.err"
wt15_rc=$?
set -e
if [ "$wt15_rc" = 0 ] && [ "$(git -C "$remote" rev-parse -q --verify refs/heads/wt15)" = "$(git -C "$T/wt15" rev-parse HEAD)" ]; then
  pass "linked worktree からの push でも、検査に git の場所の環境変数を渡さない"
else
  fail "linked worktree からの push が rc=$wt15_rc: $(cat "$T/wt15.err")"
fi
grep -q "$first" "$T/wt15.err" || fail "linked worktree からの push で偽の検査 ($first) が走っていない: $(cat "$T/wt15.err")"

if [ "$fails" -ne 0 ]; then
  printf '✗ pre-push: %s 件失敗\n' "$fails" >&2
  exit 1
fi
printf '✓ pre-push: 全ケース通過\n'
