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
  # 孫プロセスを立てる: driver の kill が「codex 本体だけ」でなくグループごと殺すことの検証用
  sleep 60 &
  echo $! >"${out}.grandchild"
  wait
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
  printf 'EMPTY_OUT_MARKER\n' >"$WORK/brief_empty_out.md"
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
  # ro run は out.md だけ渡す (log は思考ストリーム込みで merger 入力を無駄に倍加させる)。
  # review run は本文が -o に出ないことがあるため out と log の両方を渡す
  ! grep -q "a\.log" "$WORK/out1/merger.prompt.md"
  grep -q "b\.out\.md" "$WORK/out1/merger.prompt.md"
  grep -q "b\.log" "$WORK/out1/merger.prompt.md"
}

@test "ro run の out が空のときだけ log を merger に渡す (本文の取りこぼし防止)" {
  printf 'empty\tro\tm1\thigh\t%s\nfull\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_empty_out.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_eo"
  [ "$status" -eq 0 ]
  grep -q "empty\.log" "$WORK/out_eo/merger.prompt.md"
  ! grep -q "full\.log" "$WORK/out_eo/merger.prompt.md"
}

@test "1 本だけの manifest は merger を自動スキップする (1 出力の digest は言い換えでしかない)" {
  printf 'solo\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" "$WORK/m.tsv" "$WORK/out_solo"
  [ "$status" -eq 0 ]
  [ ! -e "$WORK/out_solo/digest.md" ]
  [[ "$output" == *"自動スキップ"* ]]
  # codex は fan-out の 1 回だけ (merger 分が起動されていない)
  [ "$(wc -l <"$CODEX_STUB_CALLS")" -eq 1 ]
}

@test "1 本でも -m 明示なら merger を回す (1 出力の後処理という別意図を尊重)" {
  printf 'solo\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_solo_m"
  [ "$status" -eq 0 ]
  [ -s "$WORK/out_solo_m/digest.md" ]
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

# 🚨 status だけを見ないこと: die() はどの起動前検証でも 1 を返すので、「mode を見なくなった」
# 「重複検出を消した」という退行でも別の理由で 1 になり、テストは緑のまま通る (2026-08-21 の
# テスト監査 issue 072)。落ちた理由をメッセージで固定する。
@test "mode 不正と label 重複は manifest エラー" {
  printf 'a\twrite\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out5"
  [ "$status" -eq 1 ]
  [[ "$output" == *"mode は ro|review"* ]]
  printf 'a\tro\tm1\thigh\t%s\na\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m2.tsv"
  run "$DRIVER" -M "$WORK/m2.tsv" "$WORK/out5b"
  [ "$status" -eq 1 ]
  [[ "$output" == *"label が重複している"* ]]
  # 起動前に落ちる = 1 本も codex を起こしていない (部分起動を作らない契約)
  [ ! -e "$WORK/out5/a.rc" ] && [ ! -e "$WORK/out5b/a.rc" ]
}

@test "CRLF manifest: 末尾 \\r を剥がして正常に走る (timeout 列の有無どちらでも)" {
  # \r は最終列に付く: 5 列なら prompt_parts、6 列なら timeout_s (両方の剥がしを検証)
  printf 'a\tro\tm1\thigh\t%s\r\nb\tro\tm1\thigh\t%s\t60\r\n' \
    "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_crlf"
  [ "$status" -eq 0 ]
  [ -s "$WORK/out_crlf/a.out.md" ]
  [ -s "$WORK/out_crlf/b.out.md" ]
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

# issue 150: 敵対レビュー lens だけ思考時間が長く既定 timeout で rc=143 になるため、
# 行単位で timeout を上書きできる (省略行は CODEX_FANOUT_TIMEOUT のまま)
@test "manifest の timeout_s 列: その行だけ上書きされ、省略行は env 既定のまま走る" {
  printf 'hang\tro\tm1\thigh\t%s\t2\nok\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_sleep.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  # env は十分長い値: hang 行が 2 秒で殺されたなら行の列が効いた証拠
  CODEX_FANOUT_TIMEOUT=600 run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_rowto"
  [ "$status" -eq 2 ]
  grep -q "^hang	143	" "$WORK/out_rowto/runs.tsv"
  grep -q "^ok	0	" "$WORK/out_rowto/runs.tsv"
}

@test "timeout_s 列の非数値は起動前に弾く (watchdog の無効化を防ぐ)" {
  printf 'a\tro\tm1\thigh\t%s\tabc\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_rowtv"
  [ "$status" -eq 1 ]
  [[ "$output" == *"timeout_s は秒数の整数"* ]]
  [ ! -e "$WORK/out_rowtv/a.rc" ]
}

# 敵対レビューの実測 (issue 150): どちらも通すと [ -ge ] が integer expected で常に偽になり、
# watchdog が「無音で」無効化される (die もせず hang した codex を永久に待つ)
@test "全角数字 (ロケール照合) と 19 桁 (桁あふれ) の timeout も起動前に弾く" {
  # 全角数字: ja_JP.UTF-8 の照合順では範囲式 [0-9] を素通りする → 明示列挙で弾く
  printf 'a\tro\tm1\thigh\t%s\t３\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  LC_ALL= LANG=ja_JP.UTF-8 run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_zen"
  [ "$status" -eq 1 ]
  [[ "$output" == *"timeout_s は秒数の整数"* ]]
  # 純数字の桁あふれ (19 桁): 文字種検査は通るが整数評価が壊れる → 桁数上限で弾く
  printf 'a\tro\tm1\thigh\t%s\t9999999999999999999\n' "$WORK/brief_ok.md" >"$WORK/m2.tsv"
  run "$DRIVER" -M "$WORK/m2.tsv" "$WORK/out_ovf"
  [ "$status" -eq 1 ]
  [[ "$output" == *"大きすぎる"* ]]
  # env 側 (CODEX_FANOUT_TIMEOUT) も同じ検証を通る
  CODEX_FANOUT_TIMEOUT=9999999999999999999 run "$DRIVER" -M "$WORK/m2.tsv" "$WORK/out_ovfenv"
  [ "$status" -eq 1 ]
  [[ "$output" == *"CODEX_FANOUT_TIMEOUT が大きすぎる"* ]]
}

@test "timeout: hang した codex をグループごと kill して失敗扱いにする (孫も残さない)" {
  printf 'hang\tro\tm1\thigh\t%s\n' "$WORK/brief_sleep.md" >"$WORK/m.tsv"
  CODEX_FANOUT_TIMEOUT=2 run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out6"
  [ "$status" -eq 1 ]
  # SIGTERM で殺された run は rc 非0 (143) で台帳に残る
  grep -q "^hang	143	" "$WORK/out6/runs.tsv"
  # codex (stub) が立てた孫プロセスも死んでいる (process group kill)
  gpid="$(cat "$WORK/out6/hang.out.md.grandchild")"
  sleep 0.5
  run kill -0 "$gpid"
  [ "$status" -ne 0 ]
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
  # 孫プロセスの pid が出るまで待つ (stub が書く)
  gpid=""
  for _ in $(seq 1 50); do
    if [ -f "$WORK/out7/hang.out.md.grandchild" ]; then
      gpid="$(cat "$WORK/out7/hang.out.md.grandchild")"
      break
    fi
    sleep 0.1
  done
  [ -n "$gpid" ]
  kill -TERM "$dpid"
  wait "$dpid" || true
  sleep 0.5
  # driver の死後に codex もその孫も生き残っていないこと (process group kill)
  run kill -0 "$cpid"
  [ "$status" -ne 0 ]
  run kill -0 "$gpid"
  [ "$status" -ne 0 ]
}

@test "-M で merger を省略し、成功なら exit 0" {
  printf 'solo\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out8"
  [ "$status" -eq 0 ]
  [ ! -e "$WORK/out8/digest.md" ]
  [ -s "$WORK/out8/solo.out.md" ]
}

# 既定の merger モデル / effort を pin する。回帰 (2026-08-20): 既定を書き換えても、manifest が
# 常に model/effort を明示する fixture だけでは誰も気づけなかった (呼び出し側 skill が既定を
# 下げても merger だけ上位モデルで走る、という食い違いが無検出で通る)。
@test "merger の既定モデル/effort と env 上書きが効く" {
  printf 'a\tro\tm1\tlow\t%s\nb\tro\tm1\tlow\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" "$WORK/m.tsv" "$WORK/out_def"
  [ "$status" -eq 0 ]
  # merger の起動行 (digest.md を -o に取る行) だけを見る
  merger_call="$(grep "digest.md" "$CODEX_STUB_CALLS")"
  [ -n "$merger_call" ]
  printf '%s' "$merger_call" | grep -q -- "-m gpt-5.6-luna"
  printf '%s' "$merger_call" | grep -q -- "model_reasoning_effort=max"
  # run 側の指定は merger に漏れない (manifest の model/effort と混ざらない)
  printf '%s' "$merger_call" | grep -qv -- "-m m1"

  # env で上書きできる (呼び出し側 skill が方針を変えたときの逃げ道)
  : >"$CODEX_STUB_CALLS"
  CODEX_FANOUT_MERGER_MODEL=gpt-5.6-sol CODEX_FANOUT_MERGER_EFFORT=high \
    run "$DRIVER" "$WORK/m.tsv" "$WORK/out_env"
  [ "$status" -eq 0 ]
  merger_call="$(grep "digest.md" "$CODEX_STUB_CALLS")"
  printf '%s' "$merger_call" | grep -q -- "-m gpt-5.6-sol"
  printf '%s' "$merger_call" | grep -q -- "model_reasoning_effort=high"
}
