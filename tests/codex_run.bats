#!/usr/bin/env bats

# bin/codex-run (codex 単発実行の薄い前面) の正常系 + 異常系。
# 実 codex は起動せず PATH 先頭の stub で置き換える (tests/codex_fanout.bats と同じ作法)。

setup() {
  RUN="$BATS_TEST_DIRNAME/../bin/codex-run"
  WORK="$BATS_TEST_TMPDIR/work"
  mkdir -p "$WORK/bin"
  export CODEX_STUB_CALLS="$WORK/calls.log"
  cat >"$WORK/bin/codex" <<'EOS'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CODEX_STUB_CALLS:-/dev/null}"
printf 'cwd=%s\n' "$PWD" >>"${CODEX_STUB_CALLS:-/dev/null}"
out=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "-o" ]]; then out="${args[$((i + 1))]}"; fi
done
prompt="${args[$((${#args[@]} - 1))]}"
if [[ "$prompt" == *FAIL_MARKER* ]]; then echo "stub: fail" >&2; exit 1; fi
if [[ "$prompt" == *EMPTY_OUT_MARKER* ]]; then : >"$out"; echo "stdout-only body"; exit 0; fi
printf 'stub-body: %s\n' "${prompt:0:40}" >"$out"
echo "stub-stdout"
EOS
  chmod +x "$WORK/bin/codex"
  export PATH="$WORK/bin:$PATH"
  printf 'review this diff\n' >"$WORK/prompt.md"
  printf 'FAIL_MARKER\n' >"$WORK/prompt_fail.md"
  printf 'EMPTY_OUT_MARKER\n' >"$WORK/prompt_empty.md"
  cd "$WORK"
}

@test "review mode は codex exec review へ写像し、本文と rc を残す" {
  run "$RUN" -o "$WORK/out" -l adv review "$WORK/prompt.md"
  [ "$status" -eq 0 ]
  [ -s "$WORK/out/adv.out.md" ]
  [ -f "$WORK/out/adv.log" ]
  grep -q "exec review" "$CODEX_STUB_CALLS"
  echo "$output" | grep -q "本文:"
}

@test "ro mode は codex exec -s read-only へ写像する" {
  run "$RUN" -o "$WORK/out" ro "$WORK/prompt.md"
  [ "$status" -eq 0 ]
  grep -q -- "exec -s read-only" "$CODEX_STUB_CALLS"
}

@test "-C で指定したディレクトリが codex の cwd になる (review は -C を取れないため)" {
  mkdir -p "$WORK/tree"
  run "$RUN" -C "$WORK/tree" -o "$WORK/out" review "$WORK/prompt.md"
  [ "$status" -eq 0 ]
  # stub が記録した cwd が -C のディレクトリであること。-C が codex の引数に混ざっていないこと
  grep -q "cwd=$WORK/tree" "$CODEX_STUB_CALLS"
  ! grep -qE '(^| )-C( |$)' "$CODEX_STUB_CALLS"
}

@test "モデルと effort の既定は luna / max" {
  run "$RUN" -o "$WORK/out" review "$WORK/prompt.md"
  [ "$status" -eq 0 ]
  grep -q "gpt-5.6-luna" "$CODEX_STUB_CALLS"
  grep -q "model_reasoning_effort=max" "$CODEX_STUB_CALLS"
}

@test "rc が 0 でも本文が空なら警告を出す (成果物で判定させる)" {
  run "$RUN" -o "$WORK/out" -l adv review "$WORK/prompt_empty.md"
  echo "$output" | grep -q "本文が空"
  echo "$output" | grep -q "未検証"
  # review では stdout に本文が出ることがある旨も案内する
  echo "$output" | grep -q "stdout"
}

@test "codex が失敗したら rc を伝播する" {
  run "$RUN" -o "$WORK/out" review "$WORK/prompt_fail.md"
  [ "$status" -ne 0 ]
}

@test "stdin からプロンプトを受け取れる" {
  run bash -c "printf 'from stdin\n' | '$RUN' -o '$WORK/out' review -"
  [ "$status" -eq 0 ]
  grep -q "from stdin" "$CODEX_STUB_CALLS"
}

@test "workspace-write は拒否する (codex-fanout と同じ安全域)" {
  run "$RUN" -o "$WORK/out" write "$WORK/prompt.md"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "workspace-write は扱わない"
}

@test "不明な mode / プロンプト不在 / 空プロンプトは起動前に落とす" {
  run "$RUN" -o "$WORK/out1" bogus "$WORK/prompt.md"
  [ "$status" -ne 0 ]
  run "$RUN" -o "$WORK/out2" review "$WORK/missing.md"
  [ "$status" -ne 0 ]
  : >"$WORK/empty.md"
  run "$RUN" -o "$WORK/out3" review "$WORK/empty.md"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "プロンプトが空"
  [ ! -s "$CODEX_STUB_CALLS" ]
}

@test "-t は数字のみを受ける" {
  run "$RUN" -t abc -o "$WORK/out" review "$WORK/prompt.md"
  [ "$status" -ne 0 ]
  run "$RUN" -t １２ -o "$WORK/out" review "$WORK/prompt.md"
  [ "$status" -ne 0 ]
}

@test "outdir 未指定なら cwd 配下の tmp/codex-run に出す (-C の影響を受けない)" {
  mkdir -p "$WORK/tree"
  run "$RUN" -C "$WORK/tree" -l adv review "$WORK/prompt.md"
  [ "$status" -eq 0 ]
  [ -n "$(find "$WORK/tmp/codex-run" -name 'adv.out.md' 2>/dev/null)" ]
  [ -z "$(find "$WORK/tree/tmp" -name 'adv.out.md' 2>/dev/null)" ]
}
