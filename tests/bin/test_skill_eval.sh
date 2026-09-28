#!/usr/bin/env bash
# bin/skill-eval の判定と plugin の組み立てを、偽の claude / ratelimit で固定する (本物の Claude は呼ばない)。
#
# 守るもの: ケースの無い skill を合格にしない (rc 3) / 結果の JSON が無い・ケース 0 件を合格にしない /
# claude の rc 1 / 2 をそのまま不合格・上限として返す / 5h 枠の超過で claude を呼ばない /
# 評価対象の plugin が「plugin.json + skills/<skill>/SKILL.md + evals/」で組み立てられる /
# 引数なしのとき --base からの差分 (未追跡を含む) で対象を決める / 一時ディレクトリを残さない。
set -euo pipefail
unset CDPATH
# hook から起動されたとき継承した GIT_DIR / GIT_WORK_TREE で fixture の git が外へ向かないようにする
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUT="$ROOT_DIR/bin/skill-eval"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

command -v jq >/dev/null 2>&1 || { echo "SKIP: jq が無い環境"; exit 77; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-skill-eval.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
export TMPDIR="$WORK/tmpdir"; mkdir -p "$TMPDIR"

# --- fixture の repo ---
REPO="$WORK/repo"
mkdir -p "$REPO/_claude/skills/alpha" "$REPO/_claude/skills/beta" "$REPO/_claude/skill-evals/alpha/case-one/graders"
printf -- '---\nname: alpha\ndescription: a\n---\nbody\n' > "$REPO/_claude/skills/alpha/SKILL.md"
printf -- '---\nname: beta\ndescription: b\n---\nbody\n' > "$REPO/_claude/skills/beta/SKILL.md"
printf -- '---\nruns: 1\n---\nhello\n' > "$REPO/_claude/skill-evals/alpha/case-one/prompt.md"
printf -- '---\ntype: regex\npattern: x\n---\n' > "$REPO/_claude/skill-evals/alpha/case-one/graders/g.md"
git -C "$REPO" init -q
git -C "$REPO" -c user.email=t@t -c user.name=t add -A
git -C "$REPO" -c user.email=t@t -c user.name=t commit -qm base
BASE="$(git -C "$REPO" rev-parse HEAD)"

# --- 偽の claude: 呼ばれ方と組み立てられた plugin を記録し、FAKE_* で結果を決める ---
FAKE_CLAUDE="$WORK/fake-claude"
cat > "$FAKE_CLAUDE" <<'EOF'
#!/usr/bin/env bash
set -u
log="$FAKE_LOG"
printf 'ARGS %s\n' "$*" >> "$log"
printf 'PWD %s\n' "$PWD" >> "$log"
[[ -f plugin/.claude-plugin/plugin.json ]] && printf 'MANIFEST %s\n' "$(cat plugin/.claude-plugin/plugin.json)" >> "$log"
for f in plugin/skills/*/SKILL.md; do [[ -f "$f" ]] && printf 'SKILL %s\n' "$f" >> "$log"; done
[[ -f plugin/evals/case-one/prompt.md ]] && printf 'CASE plugin/evals/case-one/prompt.md\n' >> "$log"
json=""
while (($#)); do [[ "$1" == --json ]] && { json="$2"; shift; }; shift; done
if [[ "${FAKE_NOJSON:-0}" != 1 && -n "$json" ]]; then
  printf '{"costUsd":0.01,"aggregates":{"casesTotal":%s,"casesPassed":%s,"overallScore":1,"meanDelta":1}}\n' \
    "${FAKE_TOTAL:-1}" "${FAKE_TOTAL:-1}" > "$json"
fi
exit "${FAKE_RC:-0}"
EOF
chmod +x "$FAKE_CLAUDE"
FAKE_RL="$WORK/fake-ratelimit"
printf '#!/usr/bin/env bash\necho "5h over"\nexit "${FAKE_RL_RC:-0}"\n' > "$FAKE_RL"; chmod +x "$FAKE_RL"

export SKILL_EVAL_ROOT="$REPO" SKILL_EVAL_CLAUDE="$FAKE_CLAUDE" SKILL_EVAL_RATELIMIT="$FAKE_RL"
export FAKE_LOG="$WORK/claude.log"

run() { # 結果を $out / $rc に置く
  : > "$FAKE_LOG"
  rc=0; out="$("$SUT" "$@" 2>&1)" || rc=$?
}

# 1. 合格: rc 0、plugin の組み立てと渡す option
run alpha
[[ $rc -eq 0 && "$out" == *"PASS     alpha"* ]] && ok "合格は rc 0 で PASS" || fail "合格: rc=$rc out=$out"
grep -q 'MANIFEST {"name":"alpha"' "$FAKE_LOG" && ok "plugin.json の name が skill 名" || fail "manifest: $(cat "$FAKE_LOG")"
grep -q '^SKILL plugin/skills/alpha/SKILL.md$' "$FAKE_LOG" && ok "skills/<skill>/SKILL.md が入る" || fail "SKILL.md が plugin に無い"
grep -q '^CASE plugin/evals/case-one/prompt.md$' "$FAKE_LOG" && ok "evals/ にケースが入る" || fail "ケースが plugin に無い"
args_line="$(grep '^ARGS ' "$FAKE_LOG")"
[[ "$args_line" == *"plugin eval ./plugin --trust-plugin --no-publish --max-cost-usd 1 "* ]] \
  && ok "--trust-plugin / --no-publish / 費用の上限を渡す" || fail "args: $args_line"

# 2. ケースの無い skill は合格にしない
run beta
[[ $rc -eq 3 && "$out" == *"NO-EVAL  beta"* ]] && ok "ケースが無ければ rc 3 (NO-EVAL)" || fail "no-eval: rc=$rc out=$out"
[[ ! -s "$FAKE_LOG" ]] && ok "ケースが無ければ claude を呼ばない" || fail "no-eval で claude が呼ばれた"

# 3. 不合格・上限・想定外の rc
FAKE_RC=1 run alpha
[[ $rc -eq 1 && "$out" == *"FAIL     alpha"* ]] && ok "claude rc 1 は FAIL (rc 1)" || fail "fail: rc=$rc out=$out"
FAKE_RC=2 run alpha
[[ $rc -eq 2 && "$out" == *"COST-CAP alpha"* ]] && ok "claude rc 2 は COST-CAP (rc 2)" || fail "cap: rc=$rc out=$out"
FAKE_RC=130 run alpha
[[ $rc -eq 3 && "$out" == *"ERROR    alpha"* ]] && ok "想定外の rc は判定不能 (rc 3)" || fail "other: rc=$rc out=$out"

# 4. rc 0 でも、結果の JSON が無い・ケース 0 件なら合格にしない
FAKE_NOJSON=1 run alpha
[[ $rc -eq 3 && "$out" == *"結果の JSON が無い"* ]] && ok "結果の JSON が無ければ rc 3 (JSON が無いと言う)" || fail "nojson: rc=$rc out=$out"
FAKE_TOTAL=0 run alpha
[[ $rc -eq 3 ]] && ok "実行ケース 0 件なら rc 3" || fail "zero cases: rc=$rc out=$out"

# 5. 不合格は判定不能より重い (両方あれば rc 1)
FAKE_RC=1 run alpha beta
[[ $rc -eq 1 ]] && ok "FAIL と NO-EVAL が混ざれば rc 1" || fail "worst: rc=$rc out=$out"

# 6. 5h 枠の超過では claude を呼ばない。--force なら呼ぶ
FAKE_RL_RC=1 run alpha
[[ $rc -eq 4 && ! -s "$FAKE_LOG" ]] && ok "5h 枠の超過で rc 4、claude を呼ばない" || fail "ratelimit: rc=$rc out=$out"
FAKE_RL_RC=1 run --force alpha
[[ $rc -eq 0 && -s "$FAKE_LOG" ]] && ok "--force なら枠を見ずに回す" || fail "force: rc=$rc out=$out"

# 7. 引数なし: --base からの差分で対象を決める (未追跡を含む)
run --dry-run --base "$BASE"
[[ $rc -eq 0 && "$out" == *"0 件"* ]] && ok "差分が無ければ 0 件と言って rc 0" || fail "no diff: rc=$rc out=$out"
printf 'more\n' >> "$REPO/_claude/skills/beta/SKILL.md"
mkdir -p "$REPO/_claude/skill-evals/alpha/case-two"; printf 'x\n' > "$REPO/_claude/skill-evals/alpha/case-two/prompt.md"
run --dry-run --base "$BASE"
[[ "$out" == *"TARGET   alpha (ケース 2 件)"* && "$out" == *"TARGET   beta (ケース 0 件)"* ]] \
  && ok "変更 (tracked の修正 + 未追跡のケース) から対象を拾う" || fail "changed: out=$out"

# 7b. rename の移動元の skill も拾う (git diff は既定で移動先しか出さない)
git -C "$REPO" -c user.email=t@t -c user.name=t add -A
git -C "$REPO" -c user.email=t@t -c user.name=t commit -qm step
BASE2="$(git -C "$REPO" rev-parse HEAD)"
printf 'ref\n' > "$REPO/_claude/skills/alpha/extra.md"
git -C "$REPO" -c user.email=t@t -c user.name=t add -A
git -C "$REPO" -c user.email=t@t -c user.name=t commit -qm add-extra
BASE3="$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" mv _claude/skills/alpha/extra.md _claude/skills/beta/extra.md
run --dry-run --base "$BASE3"
[[ "$out" == *"TARGET   alpha"* && "$out" == *"TARGET   beta"* ]] \
  && ok "skill 間の rename は移動元と移動先の両方を拾う" || fail "rename: out=$out"
git -C "$REPO" mv _claude/skills/beta/extra.md _claude/skills/alpha/extra.md
: "$BASE2"

# 8. 引数の誤り
run no-such-skill
[[ $rc -eq 3 && "$out" == *"NOT-FOUND no-such-skill"* ]] && ok "名前を指定した skill が無ければ rc 3 (打ち間違いを合格にしない)" || fail "not-found: rc=$rc out=$out"
run 'bad"name'
[[ $rc -eq 64 ]] && ok "skill 名の不正な文字は rc 64" || fail "bad name: rc=$rc"
run ..
[[ $rc -eq 64 ]] && ok "'..' は rc 64" || fail "dotdot: rc=$rc"

# 9. 出力先は実行ごとに新しい (前回の result.json を読んで合格にしない)
rm -rf "$REPO/tmp/skill-eval"
run alpha; run alpha
dirs="$(find "$REPO/tmp/skill-eval" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"
[[ "$dirs" == 2 ]] && ok "続けて 2 回回すと出力先が 2 つできる" || fail "出力先 $dirs 個"

# 10. 途中の異常終了を rc 0 に塗り替えない (出力先を作れない状態を作る)
rm -rf "$REPO/tmp"; printf 'not a dir\n' > "$REPO/tmp"
run alpha
[[ $rc -ne 0 ]] && ok "途中で落ちたら rc は 0 にならない (rc=$rc)" || fail "異常終了が rc 0 になった: $out"
rm -f "$REPO/tmp"

# 11. 一時ディレクトリを残さない
left="$(find "$TMPDIR" -maxdepth 1 -name 'skill-eval.*' | wc -l | tr -d ' ')"
[[ "$left" == 0 ]] && ok "一時ディレクトリを残さない" || fail "残骸 $left 件"

((fails == 0)) || { echo "FAIL: $fails 件"; exit 1; }
echo "OK: test_skill_eval"
