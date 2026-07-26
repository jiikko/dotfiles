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
assert_contains "$out" "7d:"             "7 日ウィンドウのラベル"
assert_contains "$out" "93%"             "7d の残量%"
assert_contains "$out" "残:"             "resets_at から残り時間ラベルが出る"

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

if (( fails > 0 )); then
  printf '[test-statusline] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-statusline] すべて成功\n'
