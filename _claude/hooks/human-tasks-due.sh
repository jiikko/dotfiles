#!/usr/bin/env bash
#
# SessionStart フック: 未完了の human タスク issue (`NNN-human-*.md` = 人間しかできない作業:
# 動作確認・目視レビュー・外部サービスの操作・判断待ち) と、期限 (`期限:`) が切れた/迫っている
# issue をセッション開始時にモデルのコンテキストへ注入する。
#
# なぜ: 動作確認を応答本文に書くと chat に流れて存在自体が忘れられる。issue に起こす運用
# (issues/README.md) にしても、「読む契機」が glogx の viewer を開くか issue-sync を叩くかに
# 依存する = 起動しなければ永久に気づかない。セッション開始という必ず通る場所で催促する。
# 出典: ~/.claude/CLAUDE.md「Issue管理」/ issues/README.md。
#
# 状態の正本はファイルの位置: issues/ 直下 = 未完了、issues/pending/ = 着手保留 (期限は追う)、
# issues/done/ = 完了 (対象外)。本文の既読ヘッダーは見ない (書き換え忘れで嘘が残るため)。
# 🚨 pending も走査する: 「保留」に置いた人間タスクの期限切れを黙らせると、期限を書いた本人
# だけが忘れる形になる (issue-sync の Step 0 も pending を見る。片方だけ黙ると検査が食い違う)。
#
# 入力: SessionStart の hook JSON (stdin)。`.cwd` があれば使い、無ければ $PWD。
# 出力: 報告することがあるときだけ additionalContext を emit (jq が無ければ素の stdout)。
#       未確認 0 件 + 期限問題なしのときは何も出さない (毎セッションのノイズにしない)。
#       🚨 「期限が読めなかった」は黙って捨てない — 書式不正・期限なしとして必ず出す
#       (取りこぼしを「期限なし」と区別できないと、検査が静かに空回りする)。

set -u

lib="$(dirname "$0")/lib/issue-hooks.sh"
# 🚨 source の失敗を黙らせない: set -e が無いので `.` が失敗しても次行へ進み、関数未定義の
# 非 0 が `|| exit 0` に吸われて「点検して報告なし」と区別できなくなる (実測 2026-08-21)
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_resolve_dir >/dev/null 2>&1; then
  printf '%s を読めないためhuman タスク issue の点検を省略した (hook の配線を確認する)\n' "$lib"
  exit 0
fi
issue_hook_resolve_dir || exit 0
root="$ISSUE_HOOK_ROOT"
dir="$ISSUE_HOOK_DIR"

today=$(date +%F)
# +3 日は BSD date (-v) と GNU date (-d) の両方を試す。どちらも無ければ「期限間近」の
# 判定だけ諦める (期限切れの判定は文字列比較なので date に依存しない)。
# 🚨 諦めたことは報告に明記する — 黙って消すと「期限間近が 0 件」と区別できない
degraded=""
soon=$(date -v+3d +%F 2>/dev/null || date -d '+3 days' +%F 2>/dev/null || true)
if [ -z "$soon" ]; then
  soon="$today"
  degraded="  (date が +3 日を計算できないため「期限間近」の判定は省略。期限切れのみ判定)"$'\n'
fi

overdue="" ; upcoming="" ; broken="" ; later=0 ; unread=0

for f in "$dir"/*.md "$dir"/pending/*.md; do
  [ -e "$f" ] || continue
  base=${f##*/}
  case "$base" in
    README.md | readme.md) continue ;;
  esac
  rel="${f#"$root"/}"
  # 保留は未完了件数には数えない (着手条件待ちなので「今やる」対象ではない) が、
  # 期限は同じ基準で追う。ラベルで区別する
  held=""
  case "$f" in
    "$dir"/pending/*) held=" [保留]" ;;
  esac
  # カテゴリ判定は lib の共通実装に任せる (部分一致の誤検出を防ぐ。理由はそちらのコメント)
  is_human=0
  if [ "$(issue_hook_category "$base" || true)" = "human" ]; then
    is_human=1
    [ -z "$held" ] && unread=$((unread + 1))
  fi

  # 読めないファイルは「期限なし」と混ぜない (誤ラベルより「読めなかった」と言う方が正確)
  if [ ! -r "$f" ]; then
    broken="${broken}  読み取り不可 ${rel}${held}"$'\n'
    continue
  fi

  # 🚨 grep の終了コードを見る: 0 = 見つかった / 1 = 無い / >1 = grep 自体の失敗。
  # パイプで繋ぐと status が消え、「依存コマンドが壊れている」を「期限なし」と誤報する
  # (実測 2026-08-20: 壊れた grep で全 verify issue が「期限なし」と出た)
  line=$(grep -m1 -E '^期限[:：]' "$f" 2>/dev/null)
  case "$?" in
    0 | 1) ;;
    *) broken="${broken}  抽出失敗     ${rel}${held} (grep が失敗)"$'\n' ; continue ;;
  esac
  due=$(printf '%s' "$line" | tr -d '\r' | sed -E 's/^期限[:：][[:space:]]*//' | awk '{print $1}')
  if [ -z "$due" ]; then
    # human は期限必須。他カテゴリは任意なので黙って飛ばす
    [ "$is_human" -eq 1 ] && broken="${broken}  期限なし     ${rel}${held}"$'\n'
    continue
  fi
  case "$due" in
    [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
    *) broken="${broken}  書式不正     ${rel}${held} (期限: ${due})"$'\n' ; continue ;;
  esac

  if [ "$due" \< "$today" ]; then
    overdue="${overdue}  期限切れ ${due}  ${rel}${held}"$'\n'
  elif [ "$due" \> "$soon" ]; then
    # 🚨 later は「未完了 N 件の**うち**」として印字するので、母集団を unread と揃えること
    # (human かつ pending 以外)。揃えないと規約準拠のデータだけで
    # 「未完了 1 件 (うち期限に余裕あり 2 件)」のような部分集合でない表示が出る
    # (実測 2026-08-21: pending を走査へ加えたときに later 側だけ母集団が広がった)。
    if [ "$is_human" -eq 1 ] && [ -z "$held" ]; then
      later=$((later + 1))
    fi
  else
    upcoming="${upcoming}  期限間近 ${due}  ${rel}${held}"$'\n'
  fi
done

# 報告するものが何も無ければ黙る
[ -n "$overdue$upcoming$broken" ] || [ "$unread" -gt 0 ] || exit 0

report=$(
  printf '未完了の human タスク issue: %d 件' "$unread"
  [ "$later" -gt 0 ] && printf ' (うち期限に余裕あり %d 件)' "$later"
  printf '\n'
  [ -n "$overdue" ] && printf '%s' "$overdue"
  [ -n "$upcoming" ] && printf '%s' "$upcoming"
  [ -n "$broken" ] && printf '%s' "$broken"
  [ -n "$degraded" ] && printf '%s' "$degraded"
  printf '確認できたものは %s/done/ へ移動する (既読の出典はファイルの位置)。\n' "${dir#"$root"/}"
)

issue_hook_emit '未完了の human タスク issue (人間しかできない作業) がある。期限切れがあれば最初に一言で伝えること:' "$report"
exit 0
