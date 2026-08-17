#!/usr/bin/env bats

# bin/codex-fanout の正常系 + 異常系 (一部失敗 / 全滅 / 起動前検証 / タイムアウト / 中断 /
# digest 空 / outdir 使い回し)。実 codex は起動せず、PATH 先頭の stub で置き換える。

setup() {
  DRIVER="$BATS_TEST_DIRNAME/../bin/codex-fanout"
  WORK="$BATS_TEST_TMPDIR/work"
  mkdir -p "$WORK/bin"
  export CODEX_STUB_CALLS="$WORK/calls.log"
  # codex stub: -o の次の引数へ本文を書き、stdout にも 1 行出す。argv は CODEX_STUB_CALLS に記録
  # (mode 分岐 ro/review が正しいサブコマンドに写像されることを検証するため)。
  # プロンプトのマーカー: FAIL_MARKER = 失敗 / SLEEP_MARKER = 長時間スリープ (timeout・中断試験用) /
  # EMPTY_OUT_MARKER = exit 0 だが -o へ何も書かない (digest 空チェックの検証用)。
  cat >"$WORK/bin/codex" <<'EOS'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CODEX_STUB_CALLS:-/dev/null}"
out=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "-o" ]]; then out="${args[$((i + 1))]}"; fi
done
prompt="${args[$((${#args[@]} - 1))]}"
if [[ "$prompt" == *FAIL_MARKER* ]]; then
  echo "stub: fail" >&2
  exit 1
fi
if [[ "$prompt" == *SLEEP_MARKER* ]]; then
  sleep 60
fi
if [[ "$prompt" == *EMPTY_OUT_MARKER* ]]; then
  : >"$out"
  exit 0
fi
printf 'stub-body head: %s\n' "${prompt:0:60}" >"$out"
echo "stub-stdout"
EOS
  chmod +x "$WORK/bin/codex"
  export PATH="$WORK/bin:$PATH"
  printf 'template head\n' >"$WORK/tpl.md"
  printf 'attack the target\n' >"$WORK/brief_ok.md"
  printf 'FAIL_MARKER\n' >"$WORK/brief_fail.md"
  printf 'SLEEP_MARKER\n' >"$WORK/brief_sleep.md"
  printf 'merge instructions\n' >"$WORK/merger.md"
  printf 'EMPTY_OUT_MARKER\n' >"$WORK/merger_empty.md"
}

@test "全 run 成功: digest が出て exit 0、プロンプト連結と mode→サブコマンド写像が正しい" {
  printf 'a\tro\tm1\thigh\t%s,%s\nb\treview\tm1\thigh\t%s\n' \
    "$WORK/tpl.md" "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out1"
  [ "$status" -eq 0 ]
  [ -s "$WORK/out1/digest.md" ]
  grep -q "^a	0	" "$WORK/out1/runs.tsv"
  grep -q "^b	0	" "$WORK/out1/runs.tsv"
  # prompt part の連結 (tpl + brief) が効いている (stub は先頭 60 文字を out に写す)
  grep -q "template head" "$WORK/out1/a.out.md"
  # mode 列が正しいサブコマンドに写像される (ro → exec -s read-only / review → exec review)。
  # どの label がどの mode かまで見る (-o パスに label が入る) — 入れ替わりを検出するため
  grep "a.out.md" "$CODEX_STUB_CALLS" | grep -q "^exec -s read-only "
  grep "b.out.md" "$CODEX_STUB_CALLS" | grep -q "^exec review "
  # merger プロンプトは指示部 + ファイル一覧で組まれ、merger は read-only で起動される
  grep -q "merge instructions" "$WORK/out1/merger.prompt.md"
  grep -q "a.out.md" "$WORK/out1/merger.prompt.md"
}

@test "一部失敗: exit 2、runs.tsv に非0、digest は出て未実施が merger に伝わる" {
  printf 'good\tro\tm1\thigh\t%s\nbad\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_fail.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out2"
  [ "$status" -eq 2 ]
  grep -q "^bad	1	" "$WORK/out2/runs.tsv"
  [ -s "$WORK/out2/digest.md" ]
  grep -q "失敗した run" "$WORK/out2/merger.prompt.md"
  grep -q "bad:rc=1" "$WORK/out2/merger.prompt.md"
}

@test "全滅: merger を回さず exit 1" {
  printf 'x\tro\tm1\thigh\t%s\n' "$WORK/brief_fail.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out3"
  [ "$status" -eq 1 ]
  [ ! -e "$WORK/out3/digest.md" ]
}

@test "merger が exit 0 でも digest 空なら失敗にする (false green の防止)" {
  printf 'a\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger_empty.md" "$WORK/m.tsv" "$WORK/out_e"
  [ "$status" -eq 1 ]
  [[ "$output" == *"digest が空"* ]]
}

@test "prompt part 欠損: 1 本も起動せずに落ちる (部分起動を作らない)" {
  printf 'a\tro\tm1\thigh\t%s\nb\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/no_such_file.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out4"
  [ "$status" -eq 1 ]
  # 検証は起動前に全行行う: 正常な a も起動されていない
  [ ! -e "$WORK/out4/a.rc" ]
}

@test "mode 不正と label 重複は manifest エラー" {
  printf 'a\twrite\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out5"
  [ "$status" -eq 1 ]
  printf 'a\tro\tm1\thigh\t%s\na\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m2.tsv"
  run "$DRIVER" -M "$WORK/m2.tsv" "$WORK/out5b"
  [ "$status" -eq 1 ]
}

@test "CRLF manifest: 末尾 \\r を剥がして正常に走る" {
  printf 'a\tro\tm1\thigh\t%s\r\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_crlf"
  [ "$status" -eq 0 ]
  [ -s "$WORK/out_crlf/a.out.md" ]
}

@test "outdir の使い回しを拒否する (前回世代の残骸混入の防止)" {
  printf 'a\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_reuse"
  [ "$status" -eq 0 ]
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_reuse"
  [ "$status" -eq 1 ]
  [[ "$output" == *"使い回し"* ]]
}

@test "CODEX_FANOUT_TIMEOUT 非数値は起動前に弾く (watchdog の無効化を防ぐ)" {
  printf 'a\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  CODEX_FANOUT_TIMEOUT=abc run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_tv"
  [ "$status" -eq 1 ]
  [ ! -e "$WORK/out_tv/a.rc" ]
}

@test "timeout: hang した codex を kill して失敗扱いにする" {
  printf 'hang\tro\tm1\thigh\t%s\n' "$WORK/brief_sleep.md" >"$WORK/m.tsv"
  CODEX_FANOUT_TIMEOUT=2 run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out6"
  [ "$status" -eq 1 ]
  # SIGTERM で殺された run は rc 非0 (143) で台帳に残る
  grep -q "^hang	143	" "$WORK/out6/runs.tsv"
}

@test "SIGTERM 中断: 走行中の codex を kill してから死ぬ (孤児化させない)" {
  printf 'hang\tro\tm1\thigh\t%s\n' "$WORK/brief_sleep.md" >"$WORK/m.tsv"
  "$DRIVER" -M "$WORK/m.tsv" "$WORK/out7" >"$WORK/driver7.log" 2>&1 &
  dpid=$!
  # codex (stub) が起動して pid ファイルが出るまで待つ
  cpid=""
  for _ in $(seq 1 50); do
    if [ -f "$WORK/out7/hang.pid" ]; then
      cpid="$(cat "$WORK/out7/hang.pid")"
      break
    fi
    sleep 0.1
  done
  [ -n "$cpid" ]
  kill -TERM "$dpid"
  wait "$dpid" || true
  sleep 0.5
  # driver の死後に codex が生き残っていないこと
  run kill -0 "$cpid"
  [ "$status" -ne 0 ]
}

@test "-M で merger を省略し、成功なら exit 0" {
  printf 'solo\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out8"
  [ "$status" -eq 0 ]
  [ ! -e "$WORK/out8/digest.md" ]
  [ -s "$WORK/out8/solo.out.md" ]
}
