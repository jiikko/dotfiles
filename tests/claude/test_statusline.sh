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
assert_contains "$out" "5h ["            "5 時間ウィンドウのラベル"
assert_contains "$out" "42%"             "5h の残量% (seven_day の 93% と入れ替わらない)"
# 5h / 7d は同じ形 (ペース行) で出す。旧セグメント形式 ("7d:[██░░]93%(残:…)") は廃止した
assert_lacks    "$out" "7d:"             "旧セグメント形式は使わない"
assert_contains "$out" "7d ["            "7 日ウィンドウはペース行が持つ"
assert_contains "$out" "93%"             "7d の残量%"
assert_contains "$out" "残"              "resets_at から残り時間ラベルが出る"
# 残り時間の後ろにリセットの絶対時刻が括弧で続く形まで見る (ここを緩くしておくと、
# epoch → 日時の変換が丸ごと落ちても "残2日0時間" だけで green になる。実際 GNU date では
# 長く空だった)。7d は "(8月25日13:56)" の形
case "$out" in
  *"("*月*日*")"*) printf '✓ %s\n' "残り時間の後ろにリセット日時が続く" ;;
  *) printf '✗ %s\n  実際: %s\n' "残り時間の後ろにリセット日時が続く" "$out" >&2
     fails=$(( fails + 1 )) ;;
esac

# workspace.current_dir が無いときは .cwd にフォールバックする
assert_contains "$(render '{"cwd":"/tmp"}')" "/tmp" "current_dir 不在で cwd へフォールバック"

# 値が無いフィールドはセグメント自体を出さない (空行で読み順がズレていない証拠にもなる)
minimal="$(render '{"cwd":"/tmp"}')"
assert_lacks "$minimal" "effort:" "effort 不在ならセグメントを出さない"
assert_lacks "$minimal" "ctx:"    "context 不在ならセグメントを出さない"
assert_lacks "$minimal" "5h"      "rate limit 不在ならセグメントを出さない"

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
  # ⚠️ 境界値を使わない。now をテスト側と statusline 側で別々に取るため 1〜2 秒ずれる。
  #   表示は「日+時間」なので **時間の境界**に余白が要る (180000 = ちょうど 2 日 2 時間 は
  #   1 秒ずれると "2日1時間" に化けて flaky。実測 2026-08-23)
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":42,\"resets_at\":$(( now + 181800 ))},\"seven_day\":{\"used_percentage\":62,\"resets_at\":$(( now + 181800 ))}}}"
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
assert_contains "$nodate_out" "残2日2時間" "変換手段が無くても残り時間は出す"
assert_lacks "$nodate_out" "()"            "日時が取れないとき空の \"()\" をぶら下げない"

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
  assert_lacks "$out" "7d"       "整数化できない used% ($bad) では 7d を一切出さない"
  assert_contains "$out" "5h ["   "整数化できない used% ($bad) でも 5h 側は生き残る"
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
pace_raw() { # pace_raw <used%> <残り秒> → ペース行 (ANSI つき)
  local now; now="$(date +%s)"
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"seven_day\":{\"used_percentage\":$1,\"resets_at\":$(( now + $2 ))}}}" \
    | "$SL" | tail -1
}

# 残り 2 日 1 時間半で 62% → 想定 70% を 8pt 下回る。±10pt 帯なので語を出さない
out="$(pace_render 62 178200)"
assert_contains "$out" "7d ["                        "7d が揃えばペース行が出る"
assert_contains "$out" " 62% 想定 70%   -8pt"            "想定消化率と乖離 pt"
assert_lacks    "$out" "-8pt 先行"                   "帯の中では状態の語を出さない"
assert_contains "$out" "残2日1時間 (" "残り時間の後ろにリセットの絶対時刻が続く"
assert_contains "$out" "· 18.4%/日 · このままでちょうど" \
  "1 日予算とひとことアドバイスが数値の後ろに出る"
# 残り 1 日で 50% 残 = 余らせ過ぎ。乖離 35pt は 7 日窓で 2.4 日分 (= 35 * 7 / 100)
overspare="$(pace_render 50 "$(day 1)")"
assert_contains "$overspare" " 50% 想定 85%  -35pt 余らせ過ぎ" "余らせ過ぎの判定"
assert_contains "$overspare" "2.4日分の使い残し"            "乖離 pt を日数へ換算したアドバイス"
# 残り 5 日で 80% 使用 = 超過
over="$(pace_render 80 "$(day 5)")"
assert_contains "$over" " 80% 想定 28%  +52pt 超過" "超過の判定"
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
assert_contains "$capped" "100% 想定 99%   +1pt 上限超過" "100% 到達は乖離に関わらず上限超過"
assert_contains "$capped" "残枠なし・リセットまで待つ" "上限超過のアドバイスは残枠を待つ側"
assert_lacks    "$capped" "このままでちょうど"         "上限超過を想定通りと呼ばない"
# resets_at が窓幅 (7 日) を超えて返っても、経過をマイナスにしない (0 に clamp)
assert_contains "$(pace_render 3 "$(day 10)")" "想定  0%" "残りが 7 日超なら経過 0% に clamp"
# リセット済み (resets_at が現在以下) = 窓は終わったのにデータが更新されていない。
# 窓の中の「想定ペース」に意味が無くなる (想定 100% との比較は実績 62% を "-38pt
# 余らせ過ぎ・もっと使える" という逆向きの助言にしてしまう) ので、想定・乖離・残り時間・
# 予算・アドバイスを落として、点滅する (リセット!) だけを出す。
# ⚠️ ゲージ自体は出し続ける。以前は 2 行目の旧セグメント形式へフォールバックしていたが、
#   同じウィンドウが 2 つの見た目を持つのをやめた (表示が突然「前の実装」に戻って見える)。
edge="$(pace_render 62 0)"
assert_contains "$edge" "7d ["        "リセット済みでもゲージは出す (見た目を切り替えない)"
assert_contains "$edge" "62%"         "リセット済みでも残量% は出す"
assert_contains "$edge" "(リセット!)" "リセット済みは (リセット!) で知らせる"
assert_lacks "$edge" "想定"           "リセット済みに想定ペースを出さない"
assert_lacks "$edge" "余らせ過ぎ"     "リセット済みを余らせ過ぎと呼ばない"
assert_lacks "$edge" "残枠"           "リセット済みに予算を出さない"
# 窓が終わっているので、塗った先は全部「使い残し」= シアン。実績 62% は 9 カラム目まで
# なので 6 番はシアンになる
assert_contains "$(pace_raw 62 0)" $'\033[36m'"6" "リセット済みの未使用分は使い残し (シアン)"
assert_lacks "$(pace_raw 62 0)" $'\033[4;1m' "リセット済みには現在位置の下線を引かない"
# ⚠️ 窓が終わっているので、乖離が想定帯の中に入っていても使い残しは出す。実績 91% は
#   13 カラム目まで塗られ、最後の 1 カラムが使い残しになる (帯の判定をすると消える)
assert_contains "$(pace_raw 91 0)" $'\033[36m'" " "リセット済みは帯に関係なく使い残しを出す"
# resets_at が過去 (データが更新される前) も同じ扱い。ここが「窓の中」に紛れると
# 予算計算が 0 除算になるので、境界の両側を固定する
stale="$(pace_render 62 -3600)"
assert_contains "$stale" "(リセット!)" "resets_at が過去でも (リセット!) を出す"
assert_lacks    "$stale" "想定"        "resets_at が過去でも想定ペースを出さない"

# 色: 状態ごとに色が変わること (想定通り=緑 / 先行=黄 / 超過=赤 / 余裕=シアン /
# 余らせ過ぎ=マゼンタ)。ANSI を残した生出力で見る (render は色を落とすので使わない)。
# 状態色が乗るのは残量% なので、"<色>NN%" の形で見る。
assert_contains "$(pace_raw 28 "$(day 5)")" $'\033[32m 28%' "想定通りは緑"
assert_contains "$(pace_raw 38 "$(day 5)")" $'\033[33m 38%' "先行は黄"
assert_contains "$(pace_raw 80 "$(day 5)")" $'\033[31m 80%' "超過は赤"
assert_contains "$(pace_raw 17 "$(day 5)")" $'\033[36m 17%' "余裕はシアン"
assert_contains "$(pace_raw 2 "$(day 5)")"  $'\033[35m  2%'  "余らせ過ぎはマゼンタ"
assert_contains "$(pace_raw 100 3690)"      $'\033[31m100%' "上限超過は赤"
# 残り時間・予算・アドバイスも状態色で出す (足りないのか余っているのかを行のどこを読んでも
# 同じ色で言う)。従来はグレーで従属させていた
assert_contains "$(pace_raw 28 "$(day 5)")" $'\033[32m'"残" "想定通りの残り時間・アドバイスは緑"
assert_contains "$(pace_raw 38 "$(day 5)")" $'\033[33m'"残" "先行の残り時間・アドバイスは黄"
assert_contains "$(pace_raw 80 "$(day 5)")" $'\033[31m'"残" "超過の残り時間・アドバイスは赤"
assert_contains "$(pace_raw 17 "$(day 5)")" $'\033[36m'"残" "余裕の残り時間・アドバイスはシアン"
assert_contains "$(pace_raw 2 "$(day 5)")"  $'\033[35m'"残" "余らせ過ぎの残り時間・アドバイスはマゼンタ"
# 上限超過だけアドバイスを背景で強調する (行動が強制される唯一の状態)
assert_contains "$(pace_raw 100 3690)" $'\033[41;30m'"残枠なし" "上限超過のアドバイスは赤背景"
# 想定% は「比較対象の目盛り」なので状態色に混ぜない (混ぜると符号を取り違える)
assert_contains "$(pace_raw 80 "$(day 5)")" $'\033[37m'"想定" "想定% はグレーのまま"

# 小数の used_percentage (API は 62.7 のような値を返しうる) と、日境界でない resets_at。
# ここを丸い値だけで固めると小数部の抽出・切り捨て/四捨五入の違いがテストから
# 構造的に見えなくなる。
assert_contains "$(pace_render 62.7 "$(day 2)")" " 62% 想定 71%" "小数の used% は切り捨てて整数で出す"
# 残 2.58 日 (= 222912 秒): 残日数・1 日予算のどちらも小数部が 0 でない値になる
frac="$(pace_render 33 222912)"
assert_contains "$frac" " 33% 想定 63%  -30pt 余らせ過ぎ" "日境界でない resets_at でも想定率が合う"
assert_contains "$frac" "残2日13時間 ("  "日境界でない残りは日+時間で出す"
assert_contains "$frac" "· 25.9%/日"     "1 日予算は小数部まで一致する"
assert_contains "$frac" "2.1日分の使い残し" "乖離 pt の日換算も小数部まで一致する"
# 予算の表記: 残りが 1 日未満なら %/日 を出さない (「その 1 日」が来ないので実行不能な
# 数字になる。残 12 時間で 110.0%/日 のような表示を作らない)
assert_contains "$(pace_render 45 43200)"      "· 残枠55%" "残り 1 日未満は残枠% で出す"
assert_contains "$(pace_render 0 3690)"        "残1時間1分 ("  "残り 1 時間でも残り時間ラベルは出る"
assert_contains "$(pace_render 0 3690)"        "· 残枠100%"    "残り 1 時間でも残枠% で出す"
assert_contains "$(pace_render 120 87000)"     "· 0.0%/日"  "used% > 100 でも 1 日予算はマイナスにしない"
assert_contains "$(pace_render 0 87000)"       "· 99.3%/日"            "残り 1 日強で残枠 100% なら 1 日予算は 100% 近傍"

# --- 窓を等分したスロットの格子 ---------------------------------------------
# ペース行は「窓を ncells 等分したスロット」を描く。1 スロット = 「番号 (半角 1 桁) +
# 空白」の 2 カラムで、カラム単位に塗るので 0.5 スロット (半日 / 半時間) の端数が見える。
# ⚠️ カレンダー基準 (ローカル 0 時 / 毎時 00 分に揃えた格子) にはしない。曜日ラベルを
#   出していた頃は必要だったが、曜日を出さなくなった時点で端に半端なセルを 1 つ増やす
#   だけになった (5 時間窓が 6 セル、7 日窓が 8 セル)。曜日・日付をバーに戻すときは
#   カレンダー基準に戻す必要がある。
# ⚠️ この仕様のおかげで、塗り位置・想定線・下線の位置は **used% と残り秒だけで決まる**
#   (実時刻・TZ に依存しない)。以前は格子のセル数が実時刻依存で、1 秒のズレで 7 と 8 が
#   入れ替わって flaky になっていた (実測 2026-08-23)。
bar_of() { local l=${1#*[}; printf '%s' "${l%%]*}"; }

grid_bar="$(bar_of "$(pace_render 62 178200 | tail -1)")"
if [[ "$grid_bar" == " 1 2 3 4 5 6 7 " ]]; then
  printf '✓ %s\n' "7d のバーは常に 7 スロット (先頭に余白 1 カラム)"
else
  printf '✗ %s\n  実際: [%s]\n' "7d のバーは常に 7 スロット (先頭に余白 1 カラム)" "$grid_bar" >&2
  fails=$(( fails + 1 ))
fi
# 下線は「いま居るスロット」の番号に引く (経過を 1 セルで割った位置)。
# 残り 2 日 1 時間半 = 経過 4.93 日なので 5 番
assert_contains "$(pace_raw 62 178200)" $'\033[4;1m'"5" "いま居るスロット (経過 / 1 日) の番号に下線が入る"
assert_contains "$(pace_raw 62 604700)" $'\033[4;1m'"1" "窓の頭では 1 番に下線"
assert_contains "$(pace_raw 62 3600)"   $'\033[4;1m'"7" "窓の終盤では 7 番に下線"

# 塗り: 想定内の消化は緑背景、前借りは赤背景、使い残しはシアン。位置が used% と残り秒
# だけで決まるので、期待するカラムの文字まで指定して固定できる。
# ⚠️ 色だけを見る assert にしないこと。シアン (36m) は「余裕」の状態色にも使われるので、
#   色の存在だけでは塗りを何も主張しない (実測: 変異させても green のまま通った)。
# 残 5 日 = 想定 28%。実績 80% は 12 カラム目まで塗られ、想定線 (4 カラム) の先は前借り
over_raw="$(pace_raw 80 "$(day 5)")"
assert_contains "$over_raw" $'\033[42;30m'" 1" "想定内の消化は緑背景 (左端の余白も同じ塗り)"
assert_contains "$over_raw" $'\033[41;30m\033[4;1m'"3" "想定線を越えた分は赤背景 (いま居る 3 番に下線)"
assert_contains "$over_raw" $'\033[90m'"7"           "まだ来ていない先は暗灰"
# 実績 10% / 想定 28% (-18pt) は 2 番が使い残し (2 カラム目までしか塗られない)
assert_contains "$(pace_raw 10 "$(day 5)")" $'\033[36m'"2" "使い残しはシアンで出る"
# 実績 115% / 想定 99% (+16pt) は塗りが窓の右端に clamp され、想定線も同じカラムに届く。
# 補正が無いと **超過が 1 マスも出ない** (最後の空白カラムが赤になることで分かる)
assert_contains "$(pace_raw 115 3690)" $'\033[41;30m'" " \
  "帯の外なら、丸めで潰れても最低 1 カラムは前借りの赤を出す"
# ⚠️ 実績 100% / 想定 99% は乖離が +1pt しかなく **帯の中**なので、バーに赤は出ない
#   (それが正しい: 使い過ぎではなく窓が終わるだけ)。赤背景はアドバイスにだけ付く
assert_lacks "$(pace_raw 100 3690)" $'\033[41;30m'" " "帯の中 (実績 100% / 想定 99%) では超過の赤を出さない"
# 帯の中 (±10pt) では前借り/使い残しの色を出さない。1 カラム = 7.1pt なので、乖離が
# 数 pt でもカラム境界を跨げば半日分の赤が出て「想定通り」のラベルと矛盾する。
# 実績 30% / 想定 28% (+2pt) は塗り 5 カラム・想定線 4 カラムで、帯の規則が無いと
# 5 カラム目 (3 番) が赤くなる
assert_contains "$(pace_render 30 "$(day 5)")" " 30% 想定 28%   +2pt" "帯の中の fixture が意図した位置になっている"
assert_lacks    "$(pace_raw 30 "$(day 5)")" $'\033[41;30m' "帯の中では前借りの赤を出さない"
assert_lacks    "$(pace_raw 30 "$(day 5)")" $'\033[36m'"3" "帯の中では使い残しのシアンを出さない"
# 塗りのカラム数は切り上げ (少しでも掛かったカラムは塗る)。実績 15% は 2.1 カラム分なので
# 3 カラム目 (2 番) まで緑になる (四捨五入だと 2 カラムで止まって 2 番が暗くなる)
assert_contains "$(pace_raw 15 483840)" $'\033[42;30m\033[4;1m'"2" \
  "塗りは切り上げ: 1 カラムに少しでも掛かったら塗る"
# 想定線も切り上げ。経過 1.05 日 (残 514080 秒) は 3 カラム目に少しだけ掛かるので、
# 実績 3% では 3 カラム目 (2 番) が使い残しになる (四捨五入だと暗灰に落ちる)
assert_contains "$(pace_raw 3 514080)" $'\033[36m\033[4;1m'"2" \
  "想定線も切り上げ: いま居るカラムは経過済みとして扱う"

# 5h と 7d で数値の縦が揃うこと (狭い窓は括弧の後ろを空白で埋める)。括弧の中に空白を
# 入れると「空のスロット」に見えるので、外に出していることも併せて見る
align_now="$(date +%s)"
align_out="$(render "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":34,\"resets_at\":$(( align_now + 7200 ))},\"seven_day\":{\"used_percentage\":25,\"resets_at\":$(( align_now + 457200 ))}}}")"
align_five="$(printf '%s\n' "$align_out" | grep '^5h ')"
align_seven="$(printf '%s\n' "$align_out" | grep '^7d ')"
align_five_col=${align_five%%34%*}
align_seven_col=${align_seven%%25%*}
if [[ ${#align_five_col} -eq ${#align_seven_col} ]]; then
  printf '✓ %s\n' "5h と 7d で使用率の桁位置が揃う (${#align_five_col} カラム)"
else
  printf '✗ %s\n  5h: %d カラム / 7d: %d カラム\n' "5h と 7d で使用率の桁位置が揃う" \
    "${#align_five_col}" "${#align_seven_col}" >&2
  fails=$(( fails + 1 ))
fi
assert_contains "$align_five" "5 ]      34%" "5h の空白は括弧の外に置く (括弧内に空スロットを作らない)"

# --- 5h ウィンドウ ----------------------------------------------------------
# 5h も同じ関数で描く (1 セル = 1 時間、窓 5 時間 = 5 スロット)。
# ⚠️ 想定帯は 7d と別で ±25pt。5 時間窓は本質的にバースト的で「1 時間目に 40% 使った」
#   = +20pt が常態になるため、±10pt では赤が出続けて信号にならない。
pace5_render() {  # pace5_render <used%> <残り秒>
  local now; now="$(date +%s)"
  render "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":$1,\"resets_at\":$(( now + $2 ))}}}"
}
pace5_raw() {
  local now; now="$(date +%s)"
  printf '%s' "{\"cwd\":\"/tmp\",\"rate_limits\":{\"five_hour\":{\"used_percentage\":$1,\"resets_at\":$(( now + $2 ))}}}" \
    | "$SL" | tail -1
}
# ⚠️ 7200 (= ちょうど 2 時間) は境界値なので使わない (1 秒ずれると "1時間59分" になる)
five_out="$(pace5_render 34 7130)"     # 残 1 時間 58 分 50 秒 → 想定 60%
assert_contains "$five_out" "5h ["                 "5h もペース行で出る"
assert_contains "$five_out" " 34% 想定 60%  -26pt"    "5h の想定率は 5 時間窓で計算する"
assert_contains "$five_out" "· 33.3%/時"           "5h の予算は %/時"
assert_contains "$five_out" "1.3時間分の余り"       "5h の乖離換算は時間分"
assert_contains "$five_out" "残1時間58分 ("         "5h も残り時間の後ろに絶対時刻が付く"
five_bar="$(bar_of "$(printf '%s\n' "$five_out" | tail -1)")"
if [[ "$five_bar" == " 1 2 3 4 5 " ]]; then
  printf '✓ %s\n' "5h のバーは常に 5 スロット"
else
  printf '✗ %s\n  実際: %s\n' "5h のバーは常に 5 スロット" "$five_bar" >&2
  fails=$(( fails + 1 ))
fi
# 絶対時刻の書式は 5h が時刻のみ (7d は月日つき)。分単位まで出ることを見る
case "$five_out" in
  *"("[0-9][0-9]:[0-9][0-9]")"*) printf '✓ %s\n' "5h の絶対時刻は HH:MM" ;;
  *) printf '✗ %s\n  実際: %s\n' "5h の絶対時刻は HH:MM" "$five_out" >&2
     fails=$(( fails + 1 )) ;;
esac
# 帯の境界: 5h は ±25pt。経過 2 時間 (残 3 時間) → 想定 40%
assert_contains "$(pace5_render 65 10800)" "+25pt 先行" "5h は +25pt で先行に切り替わる"
assert_contains "$(pace5_render 64 10800)" "+24pt" "5h は +24pt までは想定通り"
assert_lacks    "$(pace5_render 64 10800)" "先行"  "5h の +24pt に先行の語を付けない"
assert_contains "$(pace5_render 90 10800)" "+50pt 超過"  "5h は +50pt で超過に切り替わる"
assert_contains "$(pace5_render 89 10800)" "+49pt 先行"  "5h は +49pt までは先行"
assert_contains "$(pace5_render 15 10800)" "-25pt" "5h は -25pt までは想定通り"
assert_lacks    "$(pace5_render 15 10800)" "余裕"  "5h の -25pt に余裕の語を付けない"
assert_contains "$(pace5_render 14 10800)" "-26pt 余裕"  "5h は -26pt で余裕に切り替わる"
# 7d と同じ delta でもラベルが違う (帯が別であることの直接確認)
assert_contains "$(pace5_render 80 10800)" "+40pt 先行"  "5h の +40pt は先行 (7d なら超過)"
assert_contains "$(pace_render 68 "$(day 5)")" "+40pt 超過" "7d の +40pt は超過"
# 5h の残り 1 時間未満は残枠% (「その 1 時間」が来ないので %/時 を出さない)
assert_contains "$(pace5_render 60 1800)" "· 残枠40%" "5h も残り 1 セル未満は残枠% で出す"
# 5h がリセット済みなら 2 行目にフォールバックする (残量% はどこかに必ず出る)
five_edge="$(pace5_render 80 0)"
assert_contains "$five_edge" "5h ["       "5h リセット済みでもゲージは出す"
assert_contains "$five_edge" "(リセット!)" "5h リセット済みは (リセット!) で知らせる"
assert_lacks    "$five_edge" "想定"       "5h リセット済みに想定ペースを出さない"
# 縦揃えのパディングはリセット済みでも効く (5h だけリセットされた行が横にずれない)
assert_contains "$five_edge" "5 ]      80%" "5h リセット済みでも桁位置を揃える"

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
assert_contains "$(render "$five_only")" "5h [" "5h だけでもペース行は出る"
assert_lacks "$(render "$five_only")" "7d [" "7d 不在ならペース行を出さない"
# used_percentage はあるが resets_at が無い = 窓のどこにいるか分からない。ゲージも想定も
# 出せないので残量% だけを出す (残量% はどの経路でも必ずどこかに出る、が不変条件)
noreset="$(render '{"cwd":"/tmp","rate_limits":{"seven_day":{"used_percentage":50}}}')"
assert_contains "$noreset" "7d  50%" "resets_at 不在なら残量% だけを出す"
assert_lacks    "$noreset" "7d ["   "resets_at 不在ならゲージは出さない (窓の位置が不明)"
assert_lacks    "$noreset" "想定"   "resets_at 不在なら想定ペースを出さない"
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
