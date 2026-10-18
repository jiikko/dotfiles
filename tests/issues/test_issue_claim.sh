#!/usr/bin/env bash
# scripts/issue_claim.sh の検査。
#
# 一時ディレクトリに bare の origin と clone を作り、本物の git と bin/issue-claim を通す。確かめること:
#   - claim が 1 コマンドで origin へ載り (目印の symlink + 本文冒頭のバナー)、手元の checkout は触らない
#   - 既に claim 済み / done / 直下に無い / 番号の衝突では何も push しない
#   - origin が先へ進んでいたら (non-fast-forward) 取り直して載せる。上限を超えたら何も push しない
#   - pre-push で落ちたら何も push しない
#   - どの終わり方でも一時 worktree と一時ディレクトリを残さない
set -euo pipefail
unset CDPATH GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

# shellcheck source=tests/lib/wait_until.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib/wait_until.sh"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/issue_claim.sh"
[ -x "$SCRIPT" ] || { printf '✗ scripts/issue_claim.sh が無い / 実行できない\n' >&2; exit 1; }

# bin/issue-claim のビルドは HOME 等を差し替える前に 1 回だけ済ませる (bin/lib/go_tool.sh の注記)
# shellcheck source=bin/lib/go_tool.sh
. "$ROOT_DIR/bin/lib/go_tool.sh"
go_tool_resolve ISSUE_CLAIM "$ROOT_DIR" glogx/cmd/issue-claim issue-claim help ||
  { printf '✗ bin/issue-claim を用意できない\n' >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export TMPDIR="$work/tmp" # 一時ディレクトリの残骸を数えるため、スクリプトの mktemp をここへ寄せる
mkdir -p "$TMPDIR"
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t

fail() { printf '✗ %s\n' "$1" >&2; exit 1; }

# setup <name>: $work/<name>/origin.git (bare) と $work/<name>/clone を作る
setup() {
  local d="$work/$1" seed
  seed="$d/seed"
  mkdir -p "$seed/issues/done" "$seed/issues/epic/900" "$seed/issues/pending"
  printf '# 101 (feat): a\n\n起票日: 2026-10-15\n\n## 概要\n' > "$seed/issues/101-feat-a.md"
  printf '# 102 (feat): b\n\n## 概要\n' > "$seed/issues/done/102-feat-b.md"
  printf '# 103 (feat): c\n\n## 概要\n' > "$seed/issues/pending/103-feat-c.md"
  printf '# 901 (feat): e\n\n## 概要\n' > "$seed/issues/epic/900/901-feat-e.md"
  printf '# 104 (feat): x\n\n## 概要\n' > "$seed/issues/104-feat-x.md"
  printf '# 104 (feat): y\n\n## 概要\n' > "$seed/issues/epic/900/104-feat-y.md"
  printf '# PRO-105 (feat): p\n\n## 概要\n' > "$seed/issues/PRO-105-feat-p.md" # 接頭辞つきの ID は番号 105 では拾わない
  git -C "$seed" init -q -b master
  git -C "$seed" add -A
  git -C "$seed" commit -qm init
  git clone -q --bare "$seed" "$d/origin.git"
  git clone -q "$d/origin.git" "$d/clone"
}

# origin_has_claim <name> <issue の issues/ からの相対> <目印の issues/ からの相対>
origin_has_claim() {
  local o="$work/$1/origin.git"
  [ "$(git -C "$o" ls-tree master -- "issues/$3" | cut -d' ' -f1)" = 120000 ] || return 1
  grep -q '^> 🚨 \*\*担当中: ' <<< "$(git -C "$o" show "master:issues/$2" | head -4)"
}

# no_leftovers <name>: 一時 worktree も一時ディレクトリも残っていない
no_leftovers() {
  [ "$(git -C "$work/$1/clone" worktree list | wc -l | tr -d ' ')" -eq 1 ] || { git -C "$work/$1/clone" worktree list >&2; return 1; }
  [ -z "$(ls -A "$TMPDIR")" ] || { ls -A "$TMPDIR" >&2; return 1; }
}

# --- 1. 正常系: origin に載り、手元の checkout は触らない ---------------------------------------
setup ok
head_before="$(git -C "$work/ok/clone" rev-parse HEAD)"
(cd "$work/ok/clone" && "$SCRIPT" 101) > "$work/ok.log" 2>&1 || { cat "$work/ok.log" >&2; fail "正常系が落ちた"; }
origin_has_claim ok 101-feat-a.md next/101-feat-a.md || fail "origin に目印とバナーが載っていない"
[ "$(git -C "$work/ok/origin.git" log -1 --format=%s master)" = "claim: issue 101 に着手" ] || fail "claim の commit の件名が違う"
[ "$(git -C "$work/ok/origin.git" show --stat --format= master | grep -c '|')" -eq 2 ] || fail "claim の commit に目印と本文以外が入っている"
[ "$(git -C "$work/ok/clone" rev-parse HEAD)" = "$head_before" ] || fail "手元の HEAD を動かした"
[ -z "$(git -C "$work/ok/clone" status --porcelain)" ] || fail "手元の作業ツリーを変えた"
no_leftovers ok || fail "正常系: 一時 worktree / 一時ディレクトリが残った"

# --- 2. 既に claim 済みは拒否 (origin の木で判定する: 手元の clone は古いまま) ------------------
before="$(git -C "$work/ok/origin.git" rev-parse master)"
if (cd "$work/ok/clone" && "$SCRIPT" 101) > "$work/again.log" 2>&1; then fail "claim 済みの issue をもう一度 claim した"; fi
grep -q '既に claim されている' "$work/again.log" || { cat "$work/again.log" >&2; fail "claim 済み: 想定と違う理由で落ちた"; }
[ "$(git -C "$work/ok/origin.git" rev-parse master)" = "$before" ] || fail "claim 済み: origin を動かした"
no_leftovers ok || fail "claim 済み: 一時 worktree / 一時ディレクトリが残った"

# --- 3. done / pending / 番号の衝突 / 実体なし は拒否 -------------------------------------------
for c in "102:実体が無い" "103:直下 (open) に無い" "104:2 件ある" "999:実体が無い" "105:実体が無い"; do
  n="${c%%:*}"; why="${c#*:}"
  if (cd "$work/ok/clone" && "$SCRIPT" "$n") > "$work/refuse-$n.log" 2>&1; then fail "issue $n を claim した (拒否すべき: $why)"; fi
  grep -q "$why" "$work/refuse-$n.log" || { cat "$work/refuse-$n.log" >&2; fail "issue $n: 想定 ($why) と違う理由で落ちた"; }
  [ "$(git -C "$work/ok/origin.git" rev-parse master)" = "$before" ] || fail "issue $n: 拒否したのに origin を動かした"
done
no_leftovers ok || fail "拒否: 一時 worktree / 一時ディレクトリが残った"

# --- 4. epic の issue は group の next/ に目印を置く -----------------------------------------
(cd "$work/ok/clone" && "$SCRIPT" 901) > "$work/epic.log" 2>&1 || { cat "$work/epic.log" >&2; fail "epic の issue を claim できない"; }
origin_has_claim ok epic/900/901-feat-e.md epic/900/next/901-feat-e.md || fail "epic の目印が group の next/ に無い"

# --- 5. origin が先へ進んでいたら取り直して載せる ------------------------------------------------
setup race
git clone -q "$work/race/origin.git" "$work/race/other"
printf 'other\n' > "$work/race/other/other.txt"
git -C "$work/race/other" add other.txt && git -C "$work/race/other" commit -qm other
# 1 回目の push の直前にだけ、別の clone から origin を進める
race_cmd="[ -e '$work/race/raced' ] || { git -C '$work/race/other' push -q origin HEAD:master && : > '$work/race/raced'; }"
(cd "$work/race/clone" && ISSUE_CLAIM_TEST_BEFORE_PUSH="$race_cmd" "$SCRIPT" 101) > "$work/race.log" 2>&1 ||
  { cat "$work/race.log" >&2; fail "origin が先へ進んだときに claim できない"; }
grep -q '取り直して再試行する (1/3)' "$work/race.log" || { cat "$work/race.log" >&2; fail "non-fast-forward を経ずに通った (前提が崩れた)"; }
origin_has_claim race 101-feat-a.md next/101-feat-a.md || fail "再試行の後に origin へ claim が載っていない"
git -C "$work/race/origin.git" cat-file -e master:other.txt || fail "再試行で origin の先の commit を消した"
no_leftovers race || fail "再試行: 一時 worktree / 一時ディレクトリが残った"

# --- 6. 上限まで origin が先へ進み続けたら何も push しない ----------------------------------------
setup exhaust
git clone -q "$work/exhaust/origin.git" "$work/exhaust/other"
always_cmd="cd '$work/exhaust/other' && date +%s%N >> bump.txt && git add bump.txt && git commit -qm bump && git push -q origin HEAD:master"
if (cd "$work/exhaust/clone" && ISSUE_CLAIM_ATTEMPTS=2 ISSUE_CLAIM_TEST_BEFORE_PUSH="$always_cmd" "$SCRIPT" 101) > "$work/exhaust.log" 2>&1; then
  fail "上限を超えても成功を返した"
fi
grep -q '2 回とも origin が先へ進んでいて' "$work/exhaust.log" || { cat "$work/exhaust.log" >&2; fail "上限: 想定と違う理由で落ちた"; }
if [ -n "$(git -C "$work/exhaust/origin.git" ls-tree master -- issues/next/101-feat-a.md)" ]; then fail "上限: origin に claim が載った"; fi
no_leftovers exhaust || fail "上限: 一時 worktree / 一時ディレクトリが残った"

# --- 7. pre-push で落ちたら何も push しない -----------------------------------------------------
setup hook
mkdir -p "$work/hook/hooks"
printf '#!/bin/sh\necho "pre-push: 検査が赤い" >&2\nexit 1\n' > "$work/hook/hooks/pre-push"
chmod +x "$work/hook/hooks/pre-push"
git -C "$work/hook/clone" config core.hooksPath "$work/hook/hooks"
before="$(git -C "$work/hook/origin.git" rev-parse master)"
if (cd "$work/hook/clone" && "$SCRIPT" 101) > "$work/hook.log" 2>&1; then fail "pre-push が落ちたのに成功を返した"; fi
grep -q 'pre-push: 検査が赤い' "$work/hook.log" || { cat "$work/hook.log" >&2; fail "pre-push の出力を見せていない"; }
! grep -q '再試行' "$work/hook.log" || fail "pre-push の失敗を non-fast-forward と読んで再試行した"
[ "$(git -C "$work/hook/origin.git" rev-parse master)" = "$before" ] || fail "pre-push が落ちたのに origin を動かした"
no_leftovers hook || fail "pre-push: 一時 worktree / 一時ディレクトリが残った"

# --- 7.5 commit に入るのは本文と目印だけ (新品の checkout で正規化により変更扱いになるファイルを巻き込まない) ------
setup crlf
printf '# 200 (feat): z\r\n\r\n## 概要\r\n' > "$work/crlf/seed/issues/200-feat-z.md"
git -C "$work/crlf/seed" add issues/200-feat-z.md && git -C "$work/crlf/seed" commit -qm crlf
printf '*.md text eol=lf\n' > "$work/crlf/seed/.gitattributes"
git -C "$work/crlf/seed" add .gitattributes && git -C "$work/crlf/seed" commit -qm attrs
git -C "$work/crlf/seed" push -q "$work/crlf/origin.git" master
git -C "$work/crlf/clone" pull -q
[ -n "$(git -C "$work/crlf/clone" status --porcelain -- issues)" ] || fail "前提が崩れた: 新品の checkout で 200 が変更扱いにならない"
(cd "$work/crlf/clone" && "$SCRIPT" 101) > "$work/crlf.log" 2>&1 || { cat "$work/crlf.log" >&2; fail "正規化のある repo で claim できない"; }
[ "$(git -C "$work/crlf/origin.git" show --name-only --format= master | sort | tr '\n' ' ')" = "issues/101-feat-a.md issues/next/101-feat-a.md " ] ||
  fail "claim の commit に本文と目印以外が入った: $(git -C "$work/crlf/origin.git" show --name-only --format= master | tr '\n' ' ')"

# --- 7.6 origin の手前で non-fast-forward (手元の追跡 ref が先の commit を既に知っている) も再試行する -------------
setup nonff
git clone -q "$work/nonff/origin.git" "$work/nonff/other"
printf 'other\n' > "$work/nonff/other/other.txt"
git -C "$work/nonff/other" add other.txt && git -C "$work/nonff/other" commit -qm other
nonff_cmd="[ -e '$work/nonff/raced' ] || { git -C '$work/nonff/other' push -q origin HEAD:master && git -C '$work/nonff/clone' fetch -q origin && : > '$work/nonff/raced'; }"
(cd "$work/nonff/clone" && ISSUE_CLAIM_TEST_BEFORE_PUSH="$nonff_cmd" "$SCRIPT" 101) > "$work/nonff.log" 2>&1 ||
  { cat "$work/nonff.log" >&2; fail "(non-fast-forward) の経路で再試行しない"; }
grep -q '取り直して再試行する (1/3)' "$work/nonff.log" || { cat "$work/nonff.log" >&2; fail "(non-fast-forward) を経ずに通った (前提が崩れた)"; }
origin_has_claim nonff 101-feat-a.md next/101-feat-a.md || fail "(non-fast-forward) の再試行の後に claim が載っていない"

# --- 7.7 push 先が fetch 先と違う (pushurl) と、載ったかを確かめられないので rc=3 ------------------------------
setup pushurl
git clone -q --bare "$work/pushurl/origin.git" "$work/pushurl/mirror.git"
git -C "$work/pushurl/clone" config remote.origin.pushurl "$work/pushurl/mirror.git"
rc=0; (cd "$work/pushurl/clone" && "$SCRIPT" 101) > "$work/pushurl.log" 2>&1 || rc=$?
[ "$rc" -eq 3 ] || { cat "$work/pushurl.log" >&2; fail "pushurl が別の repo のとき rc=3 でない (rc=$rc)"; }
no_leftovers pushurl || fail "pushurl: 一時 worktree / 一時ディレクトリが残った"

# --- 7.8 継承した GIT_DIR が別の repo を指していても、cwd の repo に claim する -----------------------------------
setup gitdir
setup decoy
before_decoy="$(git -C "$work/decoy/origin.git" rev-parse master)"
(cd "$work/gitdir/clone" && GIT_DIR="$work/decoy/clone/.git" GIT_WORK_TREE="$work/decoy/clone" "$SCRIPT" 101) > "$work/gitdir.log" 2>&1 ||
  { cat "$work/gitdir.log" >&2; fail "GIT_DIR を継承した状態で claim できない"; }
origin_has_claim gitdir 101-feat-a.md next/101-feat-a.md || fail "GIT_DIR を継承した状態で、cwd の repo に claim が載っていない"
[ "$(git -C "$work/decoy/origin.git" rev-parse master)" = "$before_decoy" ] || fail "継承した GIT_DIR の repo へ push した"

# --- 7.9 push の途中で SIGTERM を受けても、結果を確かめてから終わる ------------------------------------------------
setup sig
mkdir -p "$work/sig/hooks"
# pre-push が始まったら印を書き、続けてよいという印が来るまで待つ (待ちは条件で。上限つき)
printf '#!/bin/sh
: > "%s/sig/in-push"
i=0
while [ ! -e "%s/sig/go" ] && [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done # sleep-ok: tick: 条件待ちの刻み
exit 0
' "$work" "$work" > "$work/sig/hooks/pre-push"
chmod +x "$work/sig/hooks/pre-push"
git -C "$work/sig/clone" config core.hooksPath "$work/sig/hooks"
(cd "$work/sig/clone" && exec "$SCRIPT" 101) > "$work/sig.log" 2>&1 &
spid=$!
tt_wait_until test -e "$work/sig/in-push" || { kill "$spid" 2>/dev/null || true; fail "pre-push まで進まない"; }
kill -TERM "$spid"
: > "$work/sig/go"
src=0; wait "$spid" || src=$?
[ "$src" -eq 0 ] || { cat "$work/sig.log" >&2; fail "push の途中の SIGTERM で、claim が載ったのに rc=$src を返した"; }
grep -q '中断の合図を受けたが、claim は載っている' "$work/sig.log" || { cat "$work/sig.log" >&2; fail "SIGTERM を受けたことを出していない"; }
origin_has_claim sig 101-feat-a.md next/101-feat-a.md || fail "SIGTERM: claim が origin に無い"
no_leftovers sig || fail "SIGTERM: 一時 worktree / 一時ディレクトリが残った"

# --- 7.10 ref を更新した後に push ごと中断されても (push は rc≠0)、載ったかを確かめて rc=0 で終わる -------------------
setup grp
mkdir -p "$work/grp/origin.git/hooks"
# post-receive は ref を更新した後に走る。印を書き、続けてよいという印が来るまで待つ (この間にプロセスグループごと SIGINT を送る)
printf '#!/bin/sh
: > "%s/grp/updated"
i=0
while [ ! -e "%s/grp/go" ] && [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done # sleep-ok: tick: 条件待ちの刻み
exit 0
' "$work" "$work" > "$work/grp/origin.git/hooks/post-receive"
chmod +x "$work/grp/origin.git/hooks/post-receive"
set -m # 独立したプロセスグループで起こす (端末の Ctrl-C と同じく、子の git push にも合図が届く)
(cd "$work/grp/clone" && exec "$SCRIPT" 101) > "$work/grp.log" 2>&1 &
gpid=$!
set +m
tt_wait_until test -e "$work/grp/updated" || { kill -- -"$gpid" 2>/dev/null || true; fail "post-receive まで進まない"; }
kill -INT -- -"$gpid"
: > "$work/grp/go"
grc=0; wait "$gpid" || grc=$?
[ "$grc" -eq 0 ] || { cat "$work/grp.log" >&2; fail "ref の更新後に push ごと中断されたとき、claim が載ったのに rc=$grc を返した"; }
grep -q 'claim は載っている' "$work/grp.log" || { cat "$work/grp.log" >&2; fail "載っていることを出していない"; }
origin_has_claim grp 101-feat-a.md next/101-feat-a.md || fail "push ごとの中断: claim が origin に無い (前提が崩れた)"
no_leftovers grp || fail "push ごとの中断: 一時 worktree / 一時ディレクトリが残った"

# --- 7.11 確かめの fetch が関係の無い ref で失敗しうる状態 (別の fetch が持つ lock) でも、載ったと確かめられる ------------
setup lock
lock_cmd="git -C '$work/lock/seed' push -q '$work/lock/origin.git' master:refs/heads/z && : > '$work/lock/clone/.git/refs/remotes/origin/z.lock'"
(cd "$work/lock/clone" && ISSUE_CLAIM_TEST_BEFORE_PUSH="$lock_cmd" "$SCRIPT" 101) > "$work/lock.log" 2>&1 ||
  { cat "$work/lock.log" >&2; fail "関係の無い ref の lock があると、載ったのに失敗を返した"; }
origin_has_claim lock 101-feat-a.md next/101-feat-a.md || fail "lock: claim が origin に無い"
# run ごとに sha を一意にする印 (同じ秒の二重 claim で commit が丸ごと同じにならない)
grep -q '^Claim-Id: .' <<< "$(git -C "$work/lock/origin.git" log -1 --format=%B master)" || fail "claim の commit に Claim-Id が無い"

# --- 8. 引数の誤り ---------------------------------------------------------------------------
if (cd "$work/ok/clone" && "$SCRIPT" 12) >/dev/null 2>&1; then fail "3 桁未満の番号を受け付けた"; fi
if (cd "$work" && "$SCRIPT" 101) >/dev/null 2>&1; then fail "git の repo の外で動いた"; fi

printf '✓ issue_claim.sh: 正常系 (手元を触らない) / claim 済み・done・pending・番号の衝突・実体なし・接頭辞つきの ID の拒否 / epic / 再試行 (fetch first・non-fast-forward) と上限 / pre-push の失敗 / commit は本文と目印だけ / pushurl の rc=3 / 継承した GIT_DIR / push 中の SIGTERM / ref の更新後の中断 (push は rc≠0) / 関係の無い ref の lock / Claim-Id / 引数の誤り。どれも一時 worktree と一時ディレクトリを残さない\n'
