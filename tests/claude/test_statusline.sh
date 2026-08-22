#!/usr/bin/env bash
# _claude/statusline-command.sh の出力テスト。
#
# 主目的は「冒頭の一括 jq の出力順と read 順が 1:1 で対応していること」の担保。
# ここがズレると statusline は落ちずに**別のフィールドの値を表示する** (例: model 欄に
# 残量% が出る) ため、目で気づくまで無言で壊れ続ける。フィールドを足すときはこのテストの
# 期待値も足すこと。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SL="$ROOT_DIR/_claude/statusline-command.sh"

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

fails=0
# render <json> → statusline の出力 (ANSI を除去した平文)
render() {
  printf '%s' "$1" | "$SL" | sed $'s/\033\[[0-9;]*m//g'
}
assert_contains() {
  local out="$1" needle="$2" label="$3"
  if [[ "$out" == *"$needle"* ]]; then
    printf '✓ %s\n' "$label"
  else
    printf '✗ %s\n  期待: %s\n  実際: %s\n' "$label" "$needle" "$out" >&2
    fails=$(( fails + 1 ))
  fi
}
assert_lacks() {
  local out="$1" needle="$2" label="$3"
  if [[ "$out" != *"$needle"* ]]; then
    printf '✓ %s\n' "$label"
  else
    printf '✗ %s\n  出てはいけない: %s\n  実際: %s\n' "$label" "$needle" "$out" >&2
    fails=$(( fails + 1 ))
  fi
}

# 全フィールドあり: 各値が「自分のセグメント」に出ること (= jq 出力順と read 順の対応)
full='{"workspace":{"current_dir":"/tmp"},"model":{"display_name":"Opus 5"},
  "rate_limits":{"five_hour":{"used_percentage":42,"resets_at":9999999999},
                 "seven_day":{"used_percentage":93,"resets_at":9999999999}},
  "context_window":{"total_input_tokens":269000,"context_window_size":1000000,"used_percentage":27},
  "effort":{"level":"high"},"transcript_path":""}'
out="$(render "$full")"
assert_contains "$out" "/tmp"            "cwd がディレクトリ欄に出る"
assert_contains "$out" "[Opus 5]"        "model が model 欄に出る"
assert_contains "$out" "[effort:high]"   "effort が effort 欄に出る"
assert_contains "$out" "[ctx:269k/1M]"   "context の used/size が ctx 欄に出る"
assert_contains "$out" "5h:"             "5 時間ウィンドウのラベル"
assert_contains "$out" "42%"             "5h の残量% (seven_day の 93% と入れ替わらない)"
# 7d はペース行 (3 行目) が持つので、2 行目には出さない (同じ量を 2 か所に描かない)
assert_lacks    "$out" "7d:"             "ペース行が出るなら 2 行目に 7d セグメントを出さない"
assert_contains "$out" "7d ["            "7 日ウィンドウはペース行が持つ"
assert_contains "$out" "93%"             "7d の残量%"
assert_contains "$out" "残:"             "resets_at から残り時間ラベルが出る"
# 残り時間の後ろにリセット日時が続く形まで見る (ここを緩くしておくと、epoch → 日時の
# 変換が丸ごと落ちても "残:2日0時間" だけで green になる。実際 GNU date では長く空だった)
case "$out" in
  *"残:"*" / "*月*日*) printf '✓ %s\n' "残り時間の後ろにリセット日時が続く" ;;
  *) printf '✗ %s\n  実際: %s\n' "残り時間の後ろにリセット日時が続く" "$out" >&2
     fails=$(( fails + 1 )) ;;
esac

# workspace.current_dir が無いときは .cwd にフォールバックする
assert_contains "$(render '{"cwd":"/tmp"}')" "/tmp" "current_dir 不在で cwd へフォールバック"

# 値が無いフィールドはセグメント自体を出さない (空行で読み順がズレていない証拠にもなる)
minimal="$(render '{"cwd":"/tmp"}')"
assert_lacks "$minimal" "effort:" "effort 不在ならセグメントを出さない"
assert_lacks "$minimal" "ctx:"    "context 不在ならセグメントを出さない"
assert_lacks "$minimal" "5h:"     "rate limit 不在ならセグメントを出さない"

# rate limit が無いときは 2 行目自体を出さない (1 行のみ)
lines="$(printf '%s' '{"cwd":"/tmp"}' | "$SL" | wc -l | tr -d ' ')"
if [[ "$lines" == "0" ]]; then
  printf '✓ rate limit 不在なら 1 行だけ (末尾改行なし)\n'
else
  printf '✗ rate limit 不在なのに複数行: wc -l=%s\n' "$lines" >&2
  fails=$(( fails + 1 ))
fi

# advisor: transcript 末尾の advisorModel を拾い、model id から表示名を導出すること。
# ハードコード表を持たない導出なので、新モデル id (未知の family / version) でも
# 追加作業なしで整形されることをここで固定する (表方式は追加漏れでドリフトしていた)。
advisor_render() {  # advisor_render <model-id...> → 最後の id が表示されるはず
  local tr="$TMP_DIR/transcript-$1.jsonl" id
  : > "$tr"
  for id in "$@"; do
    printf '{"type":"assistant","advisorModel":"%s"}\n' "$id" >> "$tr"
  done
  render "{\"cwd\":\"/tmp\",\"transcript_path\":\"$tr\"}"
}
assert_contains "$(advisor_render claude-opus-5)"            "[advisor:Opus 5]"    "advisor: family + 1 桁 version"
assert_contains "$(advisor_render claude-opus-4-8)"          "[advisor:Opus 4.8]"  "advisor: major-minor を . で繋ぐ"
assert_contains "$(advisor_render claude-haiku-4-5-20251001)" "[advisor:Haiku 4.5]" "advisor: 末尾の日付は落とす"
assert_contains "$(advisor_render claude-nebula-7-2)"        "[advisor:Nebula 7.2]" "advisor: 未知の family も表方式なしで整形"
# provider 修飾: minor に食い込む @date / :0 を落とさないと "Sonnet 4" のような誤表示になる
assert_contains "$(advisor_render 'claude-sonnet-4-5@20250929')" "[advisor:Sonnet 4.5]" "advisor: Vertex の @date を落として minor を残す"
assert_contains "$(advisor_render 'claude-opus-4-5-v1:0')"   "[advisor:Opus 4.5]"  "advisor: Bedrock の -v1:0 を落として minor を残す"
# 導出できない形は「誤って整形する」より「生の id を出す」を選ぶ
assert_contains "$(advisor_render claude-3-5-sonnet-20241022)" "[advisor:claude-3-5-sonnet-20241022]" "advisor: version 先行の旧 id は生のまま出す"
assert_contains "$(advisor_render claude-mythos-preview)"    "[advisor:claude-mythos-preview]" "advisor: version を持たない id は生のまま出す"
assert_contains "$(advisor_render 'us.anthropic.claude-opus-4-5-v1:0')" "[advisor:us.anthropic.claude-opus-4-5-v1:0]" "advisor: provider 修飾付き id は生のまま出す"
latest="$(advisor_render claude-opus-4-8 claude-fable-5)"
assert_contains "$latest" "[advisor:Fable 5]" "advisor: 複数行なら末尾 (= 最新) を採る"
assert_lacks "$latest" "Opus 4.8" "advisor: 古い行の値は出さない"
# transcript はあるが advisorModel 行が無いケース (逆順パイプを通って空になる経路)
: > "$TMP_DIR/transcript-empty.jsonl"
printf '{"type":"assistant","message":{"role":"assistant"}}\n' >> "$TMP_DIR/transcript-empty.jsonl"
assert_contains "$(render "{\"cwd\":\"/tmp\",\"transcript_path\":\"$TMP_DIR/transcript-empty.jsonl\"}")" \
  "[advisor:未設定]" "advisor: transcript に advisorModel が無ければ未設定"
assert_contains "$(render '{"cwd":"/tmp","transcript_path":""}')" "[advisor:未設定]" "advisor: transcript 不在なら未設定"

# git 情報: 使い捨て repo で branch と ~変更数 ?untracked 数 を検証
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
repo="$TMP_DIR/repo"
git init -q -b main "$repo"
git -C "$repo" config user.email t@example.com
git -C "$repo" config user.name tester
printf 'one\n' > "$repo/tracked"; git -C "$repo" add tracked; git -C "$repo" commit -qm one
clean="$(render "{\"cwd\":\"$repo\"}")"
assert_contains "$clean" "[main]" "clean な repo はブランチ名だけ"
printf 'two\n' > "$repo/tracked"        # 変更 1
printf 'new\n' > "$repo/untracked"      # untracked 1
dirty="$(render "{\"cwd\":\"$repo\"}")"
assert_contains "$dirty" "[main ~1 ?1]" "変更数と untracked 数 (awk の 1 パス集計)"

# --- epoch → 日時の変換 (BSD / GNU 両対応) ----------------------------------
# `date -r <epoch>` は BSD (macOS) 専用。GNU (Linux = CI) の -r は「参照ファイルの
# 時刻」なので epoch をファイル名として探し、`date: 178...: No such file or directory`
# を stderr へ吐いて何も出さない。CI では 2 行目のリセット日時が長く空だった
# (2026-08-22、下の stderr assert が検出)。macOS 上でも踏めるように、GNU date を模す
# shim を PATH 先頭に置いて再現させる (この形は macOS では絶対に自然発生しない)。
gnu_dir="$TMP_DIR/gnu-date"
mkdir -p "$gnu_dir"
real_date="$(command -v date)"   # ⚠️ PATH を汚す前に実体を絶対パスで解決する
case "$real_date" in
  ""|"$gnu_dir"/*)
    printf '✗ date の実体を解決できない (%s) — shim が自分自身に解決する\n' "$real_date" >&2
    fails=$(( fails + 1 )) ;;
esac
cat > "$gnu_dir/date" <<SHIM
#!/bin/sh
# GNU date の模擬。-r は epoch でなく参照ファイルを取るので失敗させ、-d "@epoch" を通す
case "\$1" in
  -r) printf 'date: %s: No such file or directory\n' "\$2" >&2; exit 1 ;;
  # -d の中身は実 date へ委譲する。委譲先が GNU (-d) か BSD (-r) かは環境で違うので
  # 両方試す (ここを BSD 決め打ちにすると、この shim 自体が Linux で壊れる。実測済み)
  -d) spec=\${2#@}; { "$real_date" -d "@\$spec" "\$3" || "$real_date" -r "\$spec" "\$3"; } 2>/dev/null; exit \$? ;;
esac
exec "$real_date" "\$@"
SHIM
chmod +x "$gnu_dir/date"
# ⚠️ five_hour を必ず入れる: epoch → 日時の変換 (fmt_epoch) を使うのは 2 行目の
#   rate_segment だけで、7d はペース行へ移ったため 7d だけの fixture ではこの経路を
#   1 度も通らない (テストが空振りする)。
gnu_json_body() {
  local now; now="$(date +%s)"
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":42,\"resets_at\":$(( now + 172800 ))},\"seven_day\":{\"used_percentage\":62,\"resets_at\":$(( now + 172800 ))}}}"
}
gnu_json() { gnu_json_body; }
# epoch → 日時の変換手段がどちらも無い環境では、日時を落として残り時間だけ出す
# (中身の無い " / " をぶら下げない)。BSD 形式も GNU 形式も失敗する date を模す
nodate_dir="$TMP_DIR/no-date"
mkdir -p "$nodate_dir"
cat > "$nodate_dir/date" <<SHIM
#!/bin/sh
case "\$1" in
  -r|-d) printf 'date: unsupported\n' >&2; exit 1 ;;
esac
exec "$real_date" "\$@"
SHIM
chmod +x "$nodate_dir/date"
nodate_out="$(gnu_json_body | PATH="$nodate_dir:$PATH" "$SL" 2>/dev/null | sed $'s/\033\[[0-9;]*m//g')"
assert_contains "$nodate_out" "(残:" "変換手段が無くても残り時間は出す"
assert_lacks "$nodate_out" " / )"    "日時が取れないとき空の \" / \" をぶら下げない"

gnu_out="$(gnu_json | PATH="$gnu_dir:$PATH" "$SL" 2>/dev/null | sed $'s/\033\[[0-9;]*h//g;s/\033\[[0-9;]*m//g')"
gnu_err="$(gnu_json | PATH="$gnu_dir:$PATH" "$SL" 2>"$TMP_DIR/gnu_err" >/dev/null; cat "$TMP_DIR/gnu_err")"
assert_contains "$gnu_out" "月"     "GNU date でもリセット日時を出す (date -d @epoch へフォールバック)"
assert_contains "$gnu_out" "7d ["     "GNU date でもペース行は出る"
if [[ -z "$gnu_err" ]]; then
  printf '✓ %s\n' "GNU date でも stderr を出さない"
else
  printf '✗ %s\n  stderr: %s\n' "GNU date でも stderr を出さない" "$gnu_err" >&2
  fails=$(( fails + 1 ))
fi

# --- 数値フィールドの正規化 -------------------------------------------------
# bash の $(( )) は "08" (8 進数) / "5E+1" / 全角数字で **fatal** になり、囲みブロックの
# 残りが丸ごとスキップされる。以前はこれで 3 行目が無言で消え、2 行目のバーも空になって
# いた (stderr は statusline に出ないので気づけない)。整数化できない値は「その段を
# 出さない」へ正規化する契約をここで固定する。**stderr が空であることも必ず見る**
# (壊れ方が無言だったのが本質なので、出力だけ見ても退行を検出できない)。
num_json() { # num_json <used_percentage の JSON 値>
  local now; now="$(date +%s)"
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":42,\"resets_at\":$(( now + 3600 ))},\"seven_day\":{\"used_percentage\":$1,\"resets_at\":$(( now + 259200 ))}}}"
}
num_render() { num_json "$1" | "$SL" 2>/dev/null | sed $'s/\033\[[0-9;]*m//g'; }
# stderr だけを取る。`2>&1 1>/dev/null` は正しく動くが読み手に「両方 /dev/null」と
# 誤読される形 (shellcheck SC2069) なので、いったんファイルへ落とす
num_stderr() { num_json "$1" | "$SL" 2>"$TMP_DIR/stderr" >/dev/null; cat "$TMP_DIR/stderr"; }

assert_contains "$(num_render 62.7)"     "62% 想定" "小数の used% は切り捨てて読む"
assert_contains "$(num_render '"08.5"')" "8% 想定" "先頭ゼロ付き (08.5) を 8 進数エラーにせず 8% と読む"
# ⚠️ JSON の数値リテラル (5e1 等) は使わない: jq 1.6 は 50 に正規化し、jq 1.7 は
#   5E+1 のまま出すため、どちらの経路を試しているのか環境で変わる (実測)。文字列で渡す。
for bad in '"5E+1"' '"6２.7"' '"-5"' '"N/A"' '"１００"'; do
  out="$(num_render "$bad")"
  assert_lacks "$out" "7dペース" "整数化できない used% ($bad) ではペース行を出さない"
  assert_lacks "$out" "7d:"      "整数化できない used% ($bad) では 7d セグメントも出さない"
  assert_contains "$out" "5h:"   "整数化できない used% ($bad) でも 5h 側は生き残る"
  err="$(num_stderr "$bad")"
  if [[ -z "$err" ]]; then
    printf '✓ %s\n' "整数化できない used% ($bad) で stderr を出さない"
  else
    printf '✗ %s\n  stderr: %s\n' "整数化できない used% ($bad) で stderr を出さない" "$err" >&2
    fails=$(( fails + 1 ))
  fi
done

# --- 3 行目: weekly (7d) の消化ペース --------------------------------------
# 「残量% だけでは窓のどこにいるか分からない」を埋める行なので、想定消化率 (経過割合)・
# 乖離 pt・ラベル・日数換算のアドバイス・1 日予算が数値として正しいことを固定する。
# resets_at は「今から N 日後」で作る (窓幅 7 日固定という前提もここで pin される)。
# ⚠️ 残り時間ラベル (残N日N時間 / 残N時間N分) を assert する fixture は、表示される
#   最小単位の中に 30 秒以上の余白を持たせること。now はテスト側と statusline 側で
#   別々に取るため 1〜2 秒ずれ、`86400` のような境界値だと "1日0時間" が
#   "23時間59分" に化けて flaky になる (実測 2026-08-22)。
pace_render() {  # pace_render <used%> <残り秒>
  local used="$1" rem_secs="$2" now
  now="$(date +%s)"
  render "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":$used,\"resets_at\":$(( now + rem_secs ))}}}"
}
day() { printf '%d' $(( $1 * 86400 )); }

# 残り 2 日 1 時間半で 62% → 想定 70% を 8pt 下回る。±10pt 帯なので語を出さない
out="$(pace_render 62 178200)"
assert_contains "$out" "7d ["                        "7d が揃えばペース行が出る"
assert_contains "$out" "62% 想定70% -8pt"            "想定消化率と乖離 pt"
assert_lacks    "$out" "-8pt 先行"                   "帯の中では状態の語を出さない"
assert_contains "$out" "残2日1時間 · 18.4%/日 · このままでちょうど" \
  "残り時間・1 日予算・ひとことアドバイスが数値の後ろに出る"
# 残り 1 日で 50% 残 = 余らせ過ぎ。乖離 35pt は 7 日窓で 2.4 日分 (= 35 * 7 / 100)
overspare="$(pace_render 50 "$(day 1)")"
assert_contains "$overspare" "50% 想定85% -35pt 余らせ過ぎ" "余らせ過ぎの判定"
assert_contains "$overspare" "2.4日分の使い残し"            "乖離 pt を日数へ換算したアドバイス"
# 残り 5 日で 80% 使用 = 超過
over="$(pace_render 80 "$(day 5)")"
assert_contains "$over" "80% 想定28% +52pt 超過" "超過の判定"
assert_contains "$over" "3.6日分の前借り"        "超過側も日数換算する"
# 帯の境界 (±10pt = 想定通り / +20pt から超過 / -25pt から余らせ過ぎ)。残 5 日 = 想定 28%
assert_contains "$(pace_render 38 "$(day 5)")" "+10pt 先行"        "+10pt で先行に切り替わる"
assert_contains "$(pace_render 37 "$(day 5)")" "+9pt"              "+9pt はまだ想定通り"
assert_lacks    "$(pace_render 37 "$(day 5)")" "先行"              "+9pt に先行の語を付けない"
assert_contains "$(pace_render 48 "$(day 5)")" "+20pt 超過"        "+20pt で超過に切り替わる"
assert_contains "$(pace_render 47 "$(day 5)")" "+19pt 先行"        "+19pt はまだ先行"
assert_contains "$(pace_render 18 "$(day 5)")" "-10pt"             "-10pt はまだ想定通り"
assert_lacks    "$(pace_render 18 "$(day 5)")" "余裕"              "-10pt に余裕の語を付けない"
assert_contains "$(pace_render 17 "$(day 5)")" "-11pt 余裕"        "-11pt で余裕に切り替わる"
assert_contains "$(pace_render 3 "$(day 5)")"  "-25pt 余裕"        "-25pt はまだ余裕"
assert_contains "$(pace_render 2 "$(day 5)")"  "-26pt 余らせ過ぎ"  "-26pt で余らせ過ぎに切り替わる"
# 100% 到達は乖離に関わらず「上限超過」。残 1 時間で 100% は乖離 +1pt しかないので、
# 帯だけで判定すると「想定通り (緑)」になってしまう (上限に届いた事実の方が重い)
capped="$(pace_render 100 3690)"
assert_contains "$capped" "100% 想定99% +1pt 上限超過" "100% 到達は乖離に関わらず上限超過"
assert_contains "$capped" "残枠なし・リセットまで待つ" "上限超過のアドバイスは残枠を待つ側"
assert_lacks    "$capped" "このままでちょうど"         "上限超過を想定通りと呼ばない"
# resets_at が窓幅 (7 日) を超えて返っても、経過をマイナスにしない (0 に clamp)
assert_contains "$(pace_render 3 "$(day 10)")" "想定0%" "残りが 7 日超なら経過 0% に clamp"
# リセット済み (resets_at が現在以下) ではペース行を出さない。窓の外なので
# 「想定 100% との比較」に意味がなく、放っておくと実績 62% が "-38pt 余らせ過ぎ・
# もっと使える" という逆向きの助言になる。このときは 2 行目の 7d セグメントが復活し、
# "(リセット!)" を出す (7d の残量% はどの経路でも必ずどこかに出る、が不変条件)。
edge="$(pace_render 62 0)"
assert_contains "$edge" "(リセット!)" "リセット済みは 2 行目が知らせる"
assert_contains "$edge" "7d:"         "ペース行が出ないときは 2 行目に 7d が戻る"
assert_lacks "$edge" "7d ["           "リセット済みならペース行を出さない (逆向きの助言を出さない)"
assert_lacks "$edge" "余らせ過ぎ"     "リセット済みを余らせ過ぎと呼ばない"
# resets_at が過去 (データが更新される前) も同じ扱い。`-gt "$now"` ガードが
# 1 日予算の 0 除算も同時に防いでいるので、境界の両側を固定する
stale="$(pace_render 62 -3600)"
assert_lacks    "$stale" "7d [" "resets_at が過去でもペース行を出さない"
assert_contains "$stale" "7d:"  "resets_at が過去でも 7d セグメントは 2 行目に出る"

# 色: 状態ごとに色が変わること (想定通り=緑 / 先行=黄 / 超過=赤 / 余裕=シアン /
# 余らせ過ぎ=マゼンタ)。ANSI を残した生出力で見る (render は色を落とすので使わない)。
# 状態色が乗るのは残量% なので、"<色>NN%" の形で見る。
pace_raw() { # pace_raw <used%> <残り秒> → ペース行 (ANSI つき)
  local now; now="$(date +%s)"
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":$1,\"resets_at\":$(( now + $2 ))}}}" \
    | "$SL" | tail -1
}
assert_contains "$(pace_raw 28 "$(day 5)")" $'\033[32m28%' "想定通りは緑"
assert_contains "$(pace_raw 38 "$(day 5)")" $'\033[33m38%' "先行は黄"
assert_contains "$(pace_raw 80 "$(day 5)")" $'\033[31m80%' "超過は赤"
assert_contains "$(pace_raw 17 "$(day 5)")" $'\033[36m17%' "余裕はシアン"
assert_contains "$(pace_raw 2 "$(day 5)")"  $'\033[35m2%'  "余らせ過ぎはマゼンタ"
assert_contains "$(pace_raw 100 3690)"      $'\033[31m100%' "上限超過は赤"

# 小数の used_percentage (API は 62.7 のような値を返しうる) と、日境界でない resets_at。
# ここを丸い値だけで固めると小数部の抽出・切り捨て/四捨五入の違いがテストから
# 構造的に見えなくなる。
assert_contains "$(pace_render 62.7 "$(day 2)")" "62% 想定71%" "小数の used% は切り捨てて整数で出す"
# 残 2.58 日 (= 222912 秒): 残日数・1 日予算のどちらも小数部が 0 でない値になる
frac="$(pace_render 33 222912)"
assert_contains "$frac" "33% 想定63% -30pt 余らせ過ぎ" "日境界でない resets_at でも想定率が合う"
assert_contains "$frac" "残2日13時間 · 25.9%/日" "日境界でない残りは日+時間で出し、1 日予算は小数部まで一致する"
assert_contains "$frac" "2.1日分の使い残し" "乖離 pt の日換算も小数部まで一致する"
# 予算の表記: 残りが 1 日未満なら %/日 を出さない (「その 1 日」が来ないので実行不能な
# 数字になる。残 12 時間で 110.0%/日 のような表示を作らない)
assert_contains "$(pace_render 45 43200)"      "残12時間0分 · 残枠55%" "残り 1 日未満は残枠% で出す"
assert_contains "$(pace_render 0 3690)"        "残1時間1分 · 残枠100%" "残り 1 時間でも残枠% で出す"
assert_contains "$(pace_render 120 87000)"     "残1日0時間 · 0.0%/日"  "used% > 100 でも 1 日予算はマイナスにしない"
assert_contains "$(pace_render 0 87000)"       "· 99.3%/日"            "残り 1 日強で残枠 100% なら 1 日予算は 100% 近傍"

# --- カレンダー格子 ---------------------------------------------------------
# ペース行は「カレンダーの日と 1:1 の格子」(1 セル = 1 日 = 全角 1 文字 = 2 カラム) に
# 描く。窓の区切りは reset 時刻なので、窓自体を格子にすると当日のセルが実際の今日と
# 食い違う (実測: 日曜 00:40 のセルが土曜始まりになる)。
# ⚠️ ここは実時刻を使わない。格子のセル数は「窓の開始がローカル 0 時に揃うか」で 7/8 に
#   変わるため、`now + N` で作ると 1 秒のズレで 7 と 8 が入れ替わって flaky になる
#   (実測 2026-08-23: 同じテストが連続実行で成功/失敗した)。now を固定する shim を置き、
#   TZ も固定して「statusline の %z 算術」と「date に聞いた日付」を突き合わせる。
# 丸め補正が実際に発火する条件は「窓がローカル 0 時に揃い、かつ窓の終盤で 100% 超」の
# ときだけなので、`now` を固定する shim を置いて作る (実時刻依存だと日中の時間帯によって
# 発火せず、補正を削っても green のまま通る = テストが主張を守らない。実測 2026-08-23)。
# shim は `+%s %z` だけを固定し、他の書式は実 date に委譲する。
fixed_dir="$TMP_DIR/fixed-now"
mkdir -p "$fixed_dir"
cat > "$fixed_dir/date" <<SHIM
#!/bin/sh
# ⚠️ 実体は絶対パスで解決済みのものを exec する (相対名だと PATH 先頭の自分に戻る)
if [ "\$1" = '+%s %z' ]; then printf '%s +0900\n' "\$FIXED_NOW"; exit 0; fi
exec "$real_date" "\$@"
SHIM
chmod +x "$fixed_dir/date"
# resets_at は JST の 0 時ちょうど (窓の開始も 0 時に揃う = w0 が 0)。残り 2.5 時間で
# 想定 98%、実績 115% にすると、塗りも想定線も窓の右端に丸められて超過が消える条件になる。

# JST 基準の固定値。1787497200 = 2026-08-24 00:00 JST (0 時ちょうど)
jst_midnight=1787497200
jst_noon=$(( jst_midnight + 43200 ))
# fixed_render <resets_at> <now> <used%> → ペース行 (ANSI つき)
fixed_render() {
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":$3,\"resets_at\":$1}}}" \
    | TZ=Asia/Tokyo FIXED_NOW="$2" PATH="$fixed_dir:$PATH" "$SL" | tail -1
}
strip_ansi() { sed $'s/\033\[[0-9;]*m//g'; }
bar_of() { local l=${1#*7d [}; printf '%s' "${l%%]*}"; }
pace_fw=(１ ２ ３ ４ ５ ６ ７ ８)

# reset が 0 時ちょうど → 窓もカレンダーの 7 日にちょうど収まる
mid_bar="$(bar_of "$(fixed_render "$jst_midnight" $(( jst_midnight - 216000 )) 62 | strip_ansi)")"
if [[ "$mid_bar" == "１２３４５６７" ]]; then
  printf '✓ %s\n' "窓が 0 時に揃うなら格子は 7 セル"
else
  printf '✗ %s\n  実際: %s\n' "窓が 0 時に揃うなら格子は 7 セル" "$mid_bar" >&2
  fails=$(( fails + 1 ))
fi
# reset が正午 → 窓 (7 日) はカレンダー上 8 日にまたがる
noon_bar="$(bar_of "$(fixed_render "$jst_noon" $(( jst_noon - 216000 )) 62 | strip_ansi)")"
if [[ "$noon_bar" == "１２３４５６７８" ]]; then
  printf '✓ %s\n' "reset が 0 時でなければ格子は 8 セル"
else
  printf '✗ %s\n  実際: %s\n' "reset が 0 時でなければ格子は 8 セル" "$noon_bar" >&2
  fails=$(( fails + 1 ))
fi
# 当日の下線が「窓の開始日から今日まで」の日数と一致すること。期待値は date に聞いて作る
# (statusline は %z + 日数算術で出しているので、別経路の突き合わせになる)。
# ここは reset = 正午、now = その 2.5 日前 = 2026-08-22 00:00 JST、窓の開始は
# 2026-08-17 12:00 JST なので、当日は 6 日目 (1 始まり) になる。
noon_now=$(( jst_noon - 216000 ))
day_of() { TZ=Asia/Tokyo date -r "$1" '+%Y-%m-%d' 2>/dev/null || TZ=Asia/Tokyo date -d "@$1" '+%Y-%m-%d'; }
# 窓の開始日と今日の日付を date から取り、その差 (日) をセルの添字にする
w_start_date="$(day_of $(( jst_noon - 604800 )) )"
today_date="$(day_of "$noon_now")"
today_idx=0
while [[ "$(day_of $(( jst_noon - 604800 + today_idx * 86400 )) )" != "$today_date" ]]; do
  today_idx=$(( today_idx + 1 ))
  if (( today_idx > 8 )); then
    printf '✗ 当日が窓の中に見つからない (開始 %s / 今日 %s)\n' "$w_start_date" "$today_date" >&2
    fails=$(( fails + 1 )); break
  fi
done
assert_contains "$(fixed_render "$jst_noon" "$noon_now" 62)" $'\033[4;1m'"${pace_fw[$today_idx]}" \
  "当日のセル (窓の開始日から数えて N 日目) に下線が入る"

# 塗り: 想定内の消化は緑背景、前借りは赤背景、使い残しはシアン。
# ⚠️ 実績が 100% を超えて窓の終盤にいると、塗りも想定線も右端に張り付いて **超過が
#   1 セルも出ない**。想定帯の外なら最低 1 セルは色を出す補正が入っていることを
#   ここで固定する (この主張が無いと補正を削っても green のまま通る)。
# 背景色 (41;30m / 42;30m) はセルの塗りにしか使わないので、存在だけで主張になる
assert_contains "$(pace_raw 80 "$(day 5)")" $'\033[41;30m' "超過分は赤背景で出る"
assert_contains "$(pace_raw 115 14400)"      $'\033[41;30m' "窓の終盤で 100% 超でも超過を 1 セル出す"
# セル数は切り上げ (少しでも掛かったセルは掛かっている扱い)。四捨五入にすると、窓の終端が
# 日の手前に落ちたときに **今いる日が塗られない** (実測: 残 4 時間で当日のセルが暗いまま)。
# 当日 (下線) の直前が赤背景であることでこれを固定する。
assert_contains "$(pace_raw 115 14400)" $'\033[41;30m\033[4;1m' \
  "塗りは切り上げ: 窓の終端が日の途中でも、今いる日を塗る"
# ⚠️ シアン (36m) は「余裕」の状態色にも使うので、色の存在だけでは塗りを何も主張しない。
#   使い残しの塗りは下の固定 now fixture で「セルの文字まで」見る。
assert_contains "$(pace_raw 62 178200)" $'\033[42;30m' "想定内の消化は緑背景で出る"

fixed_now=1787488200
fixed_reset=1787497200
fixed_raw="$(printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":115,\"resets_at\":$fixed_reset}}}" \
  | FIXED_NOW="$fixed_now" PATH="$fixed_dir:$PATH" "$SL" | tail -1)"
assert_contains "$fixed_raw" "想定98%" "固定 now の fixture が意図した窓の位置になっている"
# 想定 98% / 実績 115% は 7 セル目 (窓の最終日 = この fixture では当日) が超過分になる。
# 当日は色の後ろに下線が入るので、色 → 下線 → 文字の順まで含めて見る
assert_contains "$fixed_raw" $'\033[41;30m\033[4;1m'"７" \
  "窓が 0 時に揃い 100% 超のときも、丸めで超過が消えないよう最低 1 セル赤を出す"
# 余り側も同じ: 1 セル = 14.3pt なので乖離 -11pt は塗りと想定線が同じセルに丸まりうる。
# 残 5 日 (想定 28%) で実績 17% = -11pt。窓が 0 時に揃う条件を固定 now で作る。
spare_now=$(( fixed_reset - 432000 ))
spare_raw="$(printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":17,\"resets_at\":$fixed_reset}}}" \
  | FIXED_NOW="$spare_now" PATH="$fixed_dir:$PATH" "$SL" | tail -1)"
assert_contains "$(printf '%s' "$spare_raw" | sed $'s/\033\[[0-9;]*m//g')" \
  "17% 想定28% -11pt 余裕" "固定 now の fixture が意図した窓の位置になっている"
# 想定 28% / 実績 17% は 2 セル目が使い残しになる (補正を外すと 1 セル目と同じ緑に潰れる)
assert_contains "$spare_raw" $'\033[36m'"２" \
  "窓が 0 時に揃う -11pt でも、丸めで使い残しが消えないよう最低 1 セルシアンを出す"

# 想定線 (経過) 側も切り上げであること。残り 18 時間 (経過 6.25 日) は「今いる日」が
# 半分より手前なので、四捨五入だと今日が「まだ来ていない未来」に落ちて使い残しが 1 日
# 消える。実績 70% / 想定 89% (= 帯の外) で 7 セル目 (= 当日) がシアンになることで固定する。
mark_now=$(( fixed_reset - 64800 ))
mark_raw="$(printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":70,\"resets_at\":$fixed_reset}}}" \
  | FIXED_NOW="$mark_now" PATH="$fixed_dir:$PATH" "$SL" | tail -1)"
assert_contains "$(printf '%s' "$mark_raw" | sed $'s/\033\[[0-9;]*m//g')" \
  "70% 想定89% -19pt 余裕" "固定 now の fixture が意図した窓の位置になっている"
assert_contains "$mark_raw" $'\033[36m\033[4;1m'"７" \
  "経過が日の前半でも、今いる日は経過済みとして扱う (使い残しが 1 日消えない)"

# 帯の中 (±10pt) では前借り/使い残しの色を出さない。1 セル = 14.3pt なので、乖離が
# +1pt でもセル境界を跨げば 1 日分の赤が出て「想定通り」のラベルと矛盾する (実測)。
# ⚠️ fixture は「窓の開始が 0 時に揃わない (reset が正午)」側で作る。0 時に揃うと塗りと
#   想定線が同じセルに落ちてしまい、帯の規則を外しても赤が出ず、テストが空振りする。
band_now=$(( jst_noon - 475200 ))     # 経過 1.5 日 → 想定 21%、実績 25% で +4pt
band_raw="$(fixed_render "$jst_noon" "$band_now" 25)"
assert_contains "$(printf '%s' "$band_raw" | strip_ansi)" \
  "25% 想定21% +4pt" "固定 now の fixture が意図した窓の位置になっている"
assert_lacks "$band_raw" $'\033[41;30m' "帯の中では前借りの赤を出さない"
assert_lacks "$band_raw" $'\033[36m'    "帯の中では使い残しのシアンを出さない"

# 行末で必ず色を戻す (戻さないと statusline の次の描画まで色が残る)
pace_line="$(pace_raw 62 178200)"
case "$pace_line" in
  *$'\033[0m') printf '✓ %s\n' "ペース行は reset で終わる" ;;
  *) # 末尾は途中でマルチバイトを割るので od で ASCII 化して出す (不正バイトを stderr に流さない)
     printf '✗ %s\n  実際の末尾: %s\n' "ペース行は reset で終わる" \
       "$(printf '%s' "$pace_line" | tail -c 12 | od -An -c | tr -s ' ')" >&2
     fails=$(( fails + 1 )) ;;
esac

# 7d が無ければペース行を出さない (5h だけの契約でも落ちない)
five_only='{"cwd":"/tmp","rate_limits":{"five_hour":{"used_percentage":42,"resets_at":9999999999}}}'
assert_contains "$(render "$five_only")" "5h:" "5h だけでも 2 行目は出る"
assert_lacks "$(render "$five_only")" "7d [" "7d 不在ならペース行を出さない"
# used_percentage はあるが resets_at が無い (= 窓の位置が分からない) 場合も出さない
assert_lacks "$(render '{"cwd":"/tmp","rate_limits":{"seven_day":{"used_percentage":50}}}')" \
  "7d [" "resets_at 不在ならペース行を出さない"
# 行数: 5h + 7d が揃えば 3 行 (末尾改行なしなので wc -l は 2)
lines="$(printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":42,\"resets_at\":$(( $(date +%s) + 3600 ))},\"seven_day\":{\"used_percentage\":62,\"resets_at\":$(( $(date +%s) + 172800 ))}}}" | "$SL" | wc -l | tr -d ' ')"
if [[ "$lines" == "2" ]]; then
  printf '✓ 5h + 7d 揃いなら 3 行 (末尾改行なし)\n'
else
  printf '✗ 3 行にならない: wc -l=%s\n' "$lines" >&2
  fails=$(( fails + 1 ))
fi

if (( fails > 0 )); then
  printf '[test-statusline] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-statusline] すべて成功\n'
