#!/usr/bin/env bats

# bin/codex-fanout の正常系 + 異常系 (一部失敗 / 全滅 / 起動前検証 / タイムアウト / 中断 /
# digest 空 / outdir 使い回し)。実 codex は起動せず、PATH 先頭の stub で置き換える。

# shellcheck source=tests/lib/wait_until.sh
. "$BATS_TEST_DIRNAME/lib/wait_until.sh"

# proc_gone: pid が居ない、または zombie (回収待ち) か。🚨 kill -0 は zombie にも成功するので、それだけで生死を見ない
proc_gone() { ! kill -0 "$1" 2>/dev/null || [[ "$(ps -o stat= -p "$1" 2>/dev/null)" == Z* ]]; }

setup() {
  DRIVER="$BATS_TEST_DIRNAME/../bin/codex-fanout"
  WORK="$BATS_TEST_TMPDIR/work"
  mkdir -p "$WORK/bin"
  export CODEX_STUB_CALLS="$WORK/calls.log"
  # codex stub: -o の次の引数へ本文を書き、stdout にも 1 行出す。argv は CODEX_STUB_CALLS に記録
  # (mode 分岐 ro/review が正しいサブコマンドに写像されることを検証するため)。
  # プロンプトのマーカー: FAIL_MARKER = 失敗 / SLEEP_MARKER = 長時間スリープ (timeout・中断試験用) /
  # EMPTY_OUT_MARKER = exit 0 だが -o へ何も書かない (digest 空チェックの検証用)。
  # sleep-ok: dummy: hang する偽 codex が起こす常駐プロセス
  cat >"$WORK/bin/codex" <<'EOS'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CODEX_STUB_CALLS:-/dev/null}"
out=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "-o" ]]; then out="${args[$((i + 1))]}"; fi
done
prompt="${args[$((${#args[@]} - 1))]}"
# 実 codex と同じく stderr の冒頭にプロンプト全文を出す (log にプロンプトが残る形を検出の検査に含める)
printf 'user\n%s\n' "$prompt" >&2
if [[ "$prompt" == *FAIL_MARKER* ]]; then
  echo "stub: fail" >&2
  exit 1
fi
if [[ "$prompt" == *LIMIT_MARKER* ]]; then
  # 実測した codex の利用上限の終わり方 (アポストロフィは U+2019。ERROR の行の後に tokens used と数値が付くことがある)
  echo "ERROR: You’ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at Oct 10th, 2026 7:29 AM." >&2
  printf 'tokens used\n96,277\n' >&2
  exit 1
fi
if [[ "$prompt" == *MIXED_MARKER* ]]; then
  # codex の発言が上限の行の引用で終わり、直後に別の原因の ERROR で落ちる
  printf 'codex\nERROR: You’ve hit your usage limit. try again at Oct 10th, 2026 7:29 AM.\nERROR: stream disconnected before completion\n' >&2
  exit 1
fi
if [[ "$prompt" == *KILLED_MARKER* ]]; then
  # 上限の行で終わるコマンドの出力の後、何も言わずに外から kill される (rc=137)
  printf 'exec\ntail -4 err.log succeeded in 1ms:\nERROR: You’ve hit your usage limit. try again at Oct 10th, 2026 7:29 AM.\ntokens used\n96,277\n\n' >&2
  kill -KILL $$
fi
if [[ "$prompt" == *LEDGER_PROBE* ]]; then
  # merger の実行中に runs.tsv が見えているか (待つ側は runs.tsv ができたら digest.md を読む)
  if [ -e "$(dirname "$out")/runs.tsv" ]; then echo "ledger-seen" >"$out"; else echo "ledger-not-seen" >"$out"; fi
  exit 0
fi
if [[ "$prompt" == *CMDOUT_MARKER* ]]; then
  # 実行したコマンドの出力として上限の行が残る (実 codex は字下げせずに出す)。その後の終わり方はプロンプトで選ぶ
  printf 'exec\n/bin/zsh -lc "tail -2 err.log" in /repo succeeded in 1ms:\n' >&2
  echo "ERROR: You’ve hit your usage limit. Upgrade to Pro or try again at Oct 10th, 2026 7:29 AM." >&2
  if [[ "$prompt" == *CMDOUT_HANG* ]]; then
    sleep 60 # sleep-ok: dummy: 時間切れ (rc=124) にする偽 codex
  fi
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
  printf 'LIMIT_MARKER\n' >"$WORK/brief_limit.md"
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
  # 台帳の列: label / rc / started_at / elapsed_s / out / log / cause。ヘッダは完全一致で固定する
  [ "$(head -1 "$WORK/out1/runs.tsv")" = "$(printf 'label\trc\tstarted_at\telapsed_s\tout\tlog\tcause')" ]
  [ "$(awk -F'\t' '$1=="a"{print $7}' "$WORK/out1/runs.tsv")" = "-" ]
  # started_at は ISO 8601 (YYYY-MM-DDTHH:MM:SS+ZZZZ)、elapsed_s は整数。placeholder "-" ではない
  awk -F'\t' '$1=="a"{print $3}' "$WORK/out1/runs.tsv" | grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[+-][0-9]{4}$'
  awk -F'\t' '$1=="a"{print $4}' "$WORK/out1/runs.tsv" | grep -qE '^[0-9]+$'
  # merger を回したときは merger 行が末尾に載る (-o は digest.md)
  [ "$(tail -1 "$WORK/out1/runs.tsv" | cut -f1,2)" = "$(printf 'merger\t0')" ]
  tail -1 "$WORK/out1/runs.tsv" | cut -f5 | grep -q "digest.md$"
  awk -F'\t' '$1=="merger"{print $4}' "$WORK/out1/runs.tsv" | grep -qE '^[0-9]+$'
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

@test "利用上限で落ちた run は台帳・要約・merger への注記で上限と分かり、rc とほかの失敗の表示は変わらない (issue 762)" {
  printf 'good\tro\tm1\thigh\t%s\nlim\tro\tm1\thigh\t%s\nbad\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_limit.md" "$WORK/brief_fail.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_lim"
  [ "$status" -eq 2 ]
  [ "$(awk -F'\t' '$1=="lim"{print $2 "/" $7}' "$WORK/out_lim/runs.tsv")" = "1/usage_limit" ]
  [ "$(awk -F'\t' '$1=="bad"{print $2 "/" $7}' "$WORK/out_lim/runs.tsv")" = "1/-" ]
  [[ "$output" == *"failed: lim:rc=1(利用上限) bad:rc=1"* ]]
  [[ "$output" == *"codex の利用上限で失敗した run: 1 本"*"try again at Oct 10th"* ]]
  grep -qF -- "- lim:rc=1(利用上限)" "$WORK/out_lim/merger.prompt.md"
  grep -qxF -- "- bad:rc=1" "$WORK/out_lim/merger.prompt.md"
}

@test "上限以外の失敗には上限の注記を出さない (issue 762)" {
  printf 'good\tro\tm1\thigh\t%s\nbad\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_fail.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_nolim"
  [ "$status" -eq 2 ]
  [[ "$output" != *"利用上限"* ]]
  ! grep -q "利用上限" "$WORK/out_nolim/merger.prompt.md"
}

@test "プロンプトが上限の行を引用しているだけの失敗は上限にしない。本物の上限ではプロンプトの古い行でなく最後の行を出す (issue 762)" {
  printf 'FAIL_MARKER\nERROR: You’ve hit your usage limit. try again at OLD-QUOTED\n' >"$WORK/brief_quote_fail.md"
  printf 'LIMIT_MARKER\nERROR: You’ve hit your usage limit. try again at OLD-QUOTED\n' >"$WORK/brief_quote_limit.md"
  printf 'good\tro\tm1\thigh\t%s\nq\tro\tm1\thigh\t%s\nl\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_quote_fail.md" "$WORK/brief_quote_limit.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_quote"
  [ "$status" -eq 2 ]
  # 前提: 引用した行は log に残っている (stub がプロンプトを stderr に出す)
  grep -q "OLD-QUOTED" "$WORK/out_quote/q.log"
  [ "$(awk -F'\t' '$1=="q"{print $7}' "$WORK/out_quote/runs.tsv")" = "-" ]
  [ "$(awk -F'\t' '$1=="l"{print $7}' "$WORK/out_quote/runs.tsv")" = "usage_limit" ]
  [[ "$output" == *"failed: q:rc=1 l:rc=1(利用上限)"* ]]
  [[ "$output" == *"try again at Oct 10th"* ]]
  [[ "$output" != *"try again at OLD-QUOTED"* ]]
}

@test "時間切れの run とコマンドの出力に上限の行があるだけの run は上限にしない。プロンプトが同じ行を引用していても本物の上限は拾う (issue 762)" {
  printf 'CMDOUT_MARKER CMDOUT_HANG\n' >"$WORK/brief_cmd_hang.md"
  printf 'CMDOUT_MARKER\n' >"$WORK/brief_cmd_fail.md"
  printf 'LIMIT_MARKER\nERROR: You’ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at Oct 10th, 2026 7:29 AM.\n' >"$WORK/brief_same_line.md"
  printf 'good\tro\tm1\thigh\t%s\nhang\tro\tm1\thigh\t%s\t2\ncmd\tro\tm1\thigh\t%s\nsame\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_cmd_hang.md" "$WORK/brief_cmd_fail.md" "$WORK/brief_same_line.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_pos"
  [ "$status" -eq 2 ]
  # 前提: 時間切れの run の log の最後の行は上限の行 (時間切れを候補から外す判定だけが上限と読ませない)
  [ "$(awk -F'\t' '$1=="hang"{print $2}' "$WORK/out_pos/runs.tsv")" = 124 ]
  tail -1 "$WORK/out_pos/hang.log" | grep -q "hit your usage limit"
  [ "$(awk -F'\t' '$1=="hang"{print $7}' "$WORK/out_pos/runs.tsv")" = "-" ]
  [ "$(awk -F'\t' '$1=="cmd"{print $7}' "$WORK/out_pos/runs.tsv")" = "-" ]
  [ "$(awk -F'\t' '$1=="same"{print $7}' "$WORK/out_pos/runs.tsv")" = "usage_limit" ]
}

@test "上限の行の引用に別の原因の ERROR が続く run と、kill された run は上限にしない。プロンプトに -J の区切り行の字面があっても本物の上限は拾う (issue 762)" {
  printf 'MIXED_MARKER\n' >"$WORK/brief_mixed.md"
  printf 'KILLED_MARKER\n' >"$WORK/brief_killed.md"
  printf 'LIMIT_MARKER\n--- Codex JSONL result ---\n' >"$WORK/brief_mark.md"
  printf 'good\tro\tm1\thigh\t%s\nmixed\tro\tm1\thigh\t%s\nkilled\tro\tm1\thigh\t%s\nmark\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_ok.md" "$WORK/brief_mixed.md" "$WORK/brief_killed.md" "$WORK/brief_mark.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_r3"
  [ "$status" -eq 2 ]
  [ "$(awk -F'\t' '$1=="mixed"{print $7}' "$WORK/out_r3/runs.tsv")" = "-" ]
  [ "$(awk -F'\t' '$1=="killed"{print $2 "/" $7}' "$WORK/out_r3/runs.tsv")" = "137/-" ]
  [ "$(awk -F'\t' '$1=="mark"{print $7}' "$WORK/out_r3/runs.tsv")" = "usage_limit" ]
}

@test "runs.tsv は merger の後に公開する (待つ側が runs.tsv を見て digest.md を読みに行ったとき、merger は終わっている)" {
  printf 'LEDGER_PROBE\n' >"$WORK/merger_probe.md"
  printf 'a\tro\tm1\thigh\t%s\nb\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger_probe.md" "$WORK/m.tsv" "$WORK/out_ledger"
  [ "$status" -eq 0 ]
  grep -qx "ledger-not-seen" "$WORK/out_ledger/digest.md"
  [ "$(tail -1 "$WORK/out_ledger/runs.tsv" | cut -f1,2)" = "$(printf 'merger\t0')" ]
  [ ! -e "$WORK/out_ledger/runs.tsv.tmp" ]
}

@test "全滅が利用上限によるものなら、全滅の要約に上限の本数と行を出す (issue 762)" {
  printf 'x\tro\tm1\thigh\t%s\ny\tro\tm1\thigh\t%s\n' "$WORK/brief_limit.md" "$WORK/brief_limit.md" >"$WORK/m.tsv"
  run "$DRIVER" -m "$WORK/merger.md" "$WORK/m.tsv" "$WORK/out_alllim"
  [ "$status" -eq 1 ]
  [[ "$output" == *"全 2 本が失敗"* ]]
  [[ "$output" == *"codex の利用上限で失敗した run: 2 本"* ]]
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
# 「重複検出を消した」という退行でも別の理由で 1 になり、テストは緑のまま通る。
# 落ちた理由をメッセージで固定する。
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

# 壊しにいく lens だけ思考時間が長く既定 timeout で時間切れになるため、
# 行単位で timeout を上書きできる (省略行は CODEX_FANOUT_TIMEOUT のまま)
@test "manifest の timeout_s 列: その行だけ上書きされ、省略行は env 既定のまま走る" {
  printf 'hang\tro\tm1\thigh\t%s\t2\nok\tro\tm1\thigh\t%s\n' \
    "$WORK/brief_sleep.md" "$WORK/brief_ok.md" >"$WORK/m.tsv"
  # env は十分長い値: hang 行が 2 秒で殺されたなら行の列が効いた証拠
  CODEX_FANOUT_TIMEOUT=600 run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_rowto"
  [ "$status" -eq 2 ]
  grep -q "^hang	124	" "$WORK/out_rowto/runs.tsv"
  grep -q "^ok	0	" "$WORK/out_rowto/runs.tsv"
}

@test "timeout_s 列の非数値は起動前に弾く (watchdog の無効化を防ぐ)" {
  printf 'a\tro\tm1\thigh\t%s\tabc\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_rowtv"
  [ "$status" -eq 1 ]
  [[ "$output" == *"timeout_s は秒数の整数"* ]]
  [ ! -e "$WORK/out_rowtv/a.rc" ]
}

# 実測: どちらも通すと [ -ge ] が integer expected で常に偽になり、
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
  # 時間切れの run は runtimeout の rc=124 で台帳に残る
  grep -q "^hang	124	" "$WORK/out6/runs.tsv"
  # elapsed_s は実測: 2 秒で殺した run は 2 秒以上 (壊れて 0 や "-" になっていれば落ちる)
  elapsed="$(awk -F'\t' '$1=="hang"{print $4}' "$WORK/out6/runs.tsv")"
  [[ "$elapsed" =~ ^[0-9]+$ ]] && [ "$elapsed" -ge 2 ]
  # codex (stub) が立てた孫プロセスも死んでいる (process group kill)
  [ -s "$WORK/out6/hang.out.md.grandchild" ] || { echo "前提: stub が孫を立てる前に kill された (孫の検査が成り立たない)"; false; }
  gpid="$(cat "$WORK/out6/hang.out.md.grandchild")"
  # 死ぬまで上限つきで待つ (固定の sleep 0.5 は負荷の下で足りず、make test の中でだけ落ちた。2026-09-25)
  tt_wait_until proc_gone "$gpid" || { echo "10 秒待っても孫 $gpid が残っている"; ps -o pid,stat,command -p "$gpid"; false; }
}

@test "SIGTERM 中断: 走行中の codex を kill してから死ぬ (孤児化させない)" {
  printf 'hang\tro\tm1\thigh\t%s\n' "$WORK/brief_sleep.md" >"$WORK/m.tsv"
  "$DRIVER" -M "$WORK/m.tsv" "$WORK/out7" >"$WORK/driver7.log" 2>&1 &
  dpid=$!
  # codex (stub) が起動して pid ファイルが出るまで待つ
  . "$BATS_TEST_DIRNAME/lib/wait_until.sh"
  TT_WAIT_TICKS=50 TT_WAIT_TICK=0.1 tt_wait_until test -f "$WORK/out7/hang.pid" || :
  cpid="$(cat "$WORK/out7/hang.pid" 2>/dev/null || true)"
  [ -n "$cpid" ]
  # 孫プロセスの pid が出るまで待つ (stub が書く)
  TT_WAIT_TICKS=50 TT_WAIT_TICK=0.1 tt_wait_until test -f "$WORK/out7/hang.out.md.grandchild" || :
  gpid="$(cat "$WORK/out7/hang.out.md.grandchild" 2>/dev/null || true)"
  [ -n "$gpid" ]
  kill -TERM "$dpid"
  wait "$dpid" || true
  # driver の死後に codex もその孫も生き残っていないこと (process group kill)。死ぬまで上限つきで待つ (固定の sleep は負荷の下で足りない)
  tt_wait_until proc_gone "$cpid" || { echo "10 秒待っても codex $cpid が残っている"; false; }
  tt_wait_until proc_gone "$gpid" || { echo "10 秒待っても孫 $gpid が残っている"; ps -o pid,stat,command -p "$gpid"; false; }
}

@test "-M で merger を省略し、成功なら exit 0" {
  printf 'solo\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out8"
  [ "$status" -eq 0 ]
  [ ! -e "$WORK/out8/digest.md" ]
  [ -s "$WORK/out8/solo.out.md" ]
  # merger を回していないので台帳にも merger 行は無い (行数 = ヘッダ + 1 run)
  ! grep -q "^merger	" "$WORK/out8/runs.tsv"
  [ "$(wc -l <"$WORK/out8/runs.tsv" | tr -d ' ')" -eq 2 ]
}

@test "label 'merger' は予約済みとして起動前に弾く (merger 自身の成果物・台帳行と衝突する)" {
  printf 'merger\tro\tm1\thigh\t%s\n' "$WORK/brief_ok.md" >"$WORK/m.tsv"
  run "$DRIVER" -M "$WORK/m.tsv" "$WORK/out_reserved"
  [ "$status" -eq 1 ]
  [[ "$output" == *"予約済み"* ]]
  [ ! -e "$WORK/out_reserved/merger.rc" ]
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
  printf '%s' "$merger_call" | grep -q -- "-m gpt-6-astra"
  printf '%s' "$merger_call" | grep -q -- "model_reasoning_effort=medium"
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
