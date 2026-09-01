#!/usr/bin/env bash
#
# SessionStart フック: 未決着の retro issue (`NNN-retro-*.md` = セッションの振り返り) を
# セッション開始時にモデルのコンテキストへ注入する。
#
# なぜ: retro は「反省・気づきを chat に流さない」ための器なので、器自体が読まれないと
# 目的を果たさない。retro の done 条件は「本文の残課題が空になったこと」= 実装の有無では
# 判定できず issue-sync の自動 done 判定の対象外 (issues/README.md「`retro`」)。したがって
# 「誰も読まなければ永久に open のまま溜まる」が既定の壊れ方で、セッション開始という必ず
# 通る場所で催促する。human タスクの期限催促 (human-tasks-due.sh) と同じ発想。
#
# 状態の正本はファイルの位置: issues/ 直下 = 未決着、issues/done/ = 決着済み (対象外)。
# 本文のチェックボックスは見ない (書き換え忘れで嘘が残るため)。
# ⚠️ 経過日数はファイル名末尾の `-YYYY-MM-DD` か本文の `起票日:` から取る。どちらも読めない
# ものは黙って捨てず「日付不明」として列挙する (取りこぼしを「新しい retro」と区別できないと
# 検査が静かに空回りする)。
#
# 入力: SessionStart の hook JSON (stdin)。出力: 未決着が 1 件以上のときだけ emit。

set -u

lib="$(dirname "$0")/lib/issue-hooks.sh"
# ⚠️ source の失敗を黙らせない: set -e が無いので `.` が失敗しても次行へ進み、関数未定義の
# 非 0 が `|| exit 0` に吸われて「点検して報告なし」と区別できなくなる (実測 2026-08-21)
# shellcheck source=_claude/hooks/lib/issue-hooks.sh
if ! . "$lib" || ! command -v issue_hook_resolve_dir >/dev/null 2>&1; then
  printf '%s を読めないためretro issue の点検を省略した (hook の配線を確認する)\n' "$lib"
  exit 0
fi
issue_hook_resolve_dir || exit 0
root="$ISSUE_HOOK_ROOT"
dir="$ISSUE_HOOK_DIR"

# midnight_epoch <YYYY-MM-DD>: その日の 0 時の epoch。読めなければ非 0。
#
# ⚠️ 時刻を明示すること。BSD の `date -j -f '%Y-%m-%d'` は**時刻を 00:00:00 にせず、実行時点の
# 時刻を埋める**。両辺を壁時計込みで引くと、today_epoch を取ってから日付を解釈するまでに
# 1 秒でも経った分だけ差が 86400 を割り、「1 日前」が「0 日前」に落ちる。単体実行では
# 通り抜け、負荷のかかった `make test` でだけ落ちる flake になっていた (実測 2026-09-02)。
# 経過日数は暦日の差なので、両辺を 0 時に丸めて壁時計を計算から外す。
midnight_epoch() {
  local d="$1"
  date -j -f '%Y-%m-%d %H:%M:%S' "$d 00:00:00" +%s 2>/dev/null ||
    date -d "$d 00:00:00" +%s 2>/dev/null || return 1
}

# today_epoch が取れない環境では日数を出さない (days_since が「日付不明」へ倒す)。
today_epoch=$(midnight_epoch "$(date +%F)" || true)

# days_since <YYYY-MM-DD>: 経過日数を stdout に出す。date が解釈できなければ非 0
days_since() {
  local d="$1" epoch=""
  [ -n "$today_epoch" ] || return 1
  epoch=$(midnight_epoch "$d" || true)
  [ -n "$epoch" ] || return 1
  printf '%d' $(((today_epoch - epoch) / 86400))
}

# dated = 経過日数が読めた行 (数値でソートする) / odd = 読めなかった行 (末尾へ回す)。
# ⚠️ 1 本の文字列を `sort -r` で並べると locale で「日付不明」の位置が反転し (実測 2026-08-21:
# en_US では末尾、C / ja_JP では先頭)、しかも未来日付の `-133` が「最古」として先頭に出る。
# 数値ソートと非数値行を分けることで locale から独立させる
dated="" ; odd="" ; count=0 ; held_count=0

for f in "$dir"/*.md "$dir"/pending/*.md; do
  [ -e "$f" ] || continue
  base=${f##*/}
  # カテゴリ判定は lib の共通実装に任せる (部分一致の誤検出を防ぐ。理由はそちらのコメント)
  case "$base" in
    *.md) ;;
    *) continue ;;
  esac
  [ "$(issue_hook_category "$base" || true)" = "retro" ] || continue
  rel="${f#"$root"/}"
  # 保留は未決着件数には数えない (着手条件待ち) が、一覧には出す。human-tasks-due.sh の
  # 「保留は未完了件数に数えない」と同じ規律に揃える (同じ「N 件」が別の意味になるのを防ぐ)
  held=""
  case "$f" in
    "$dir"/pending/*) held=" [保留]" ; held_count=$((held_count + 1)) ;;
  esac
  [ -n "$held" ] || count=$((count + 1))

  # 読めないファイルは「日付不明」と混ぜない (権限事故を「日付を書き忘れた retro」と
  # 同じ見た目にしない。human-tasks-due.sh の `読み取り不可` と同じラベルを使う)
  if [ ! -r "$f" ]; then
    odd="${odd}$(printf '  読み取り不可  %s%s' "$rel" "$held")"$'\n'
    continue
  fi

  # 起票日: ファイル名末尾の -YYYY-MM-DD を優先し、無ければ本文の `起票日:` 行
  stamp=""
  case "$base" in
    *-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9].md) stamp="${base%.md}" ; stamp="${stamp: -10}" ;;
  esac
  if [ -z "$stamp" ]; then
    line=$(grep -m1 -E '^起票日[:：]' "$f" 2>/dev/null)
    # grep の終了コードを見る: 0 = あり / 1 = なし / >1 = grep 自体の失敗。
    # パイプで繋ぐと status が消え、依存コマンドの故障を「日付なし」と誤報する
    case "$?" in
      0 | 1) ;;
      *) line="" ;;
    esac
    stamp=$(printf '%s' "$line" | tr -d '\r' | sed -E 's/^起票日[:：][[:space:]]*//' | awk '{print $1}')
    case "$stamp" in
      [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
      *) stamp="" ;;
    esac
  fi

  age=""
  [ -n "$stamp" ] && age=$(days_since "$stamp" || true)
  if [ -z "$age" ]; then
    odd="${odd}$(printf '  日付不明      %s%s' "$rel" "$held")"$'\n'
  elif [ "$age" -lt 0 ]; then
    # 未来日付は「最古」として先頭に出させない (年の打ち間違いが毎セッション最上段に居座る)
    odd="${odd}$(printf '  起票日が未来  %s%s (起票日: %s)' "$rel" "$held" "$stamp")"$'\n'
  else
    dated="${dated}$(printf '%d\t  %4d 日前     %s%s' "$age" "$age" "$rel" "$held")"$'\n'
  fi
done

# 未決着が無ければ黙る (毎セッションのノイズにしない)
[ -n "$dated$odd" ] || exit 0

report=$(
  printf '未決着の retro issue: %d 件' "$count"
  [ "$held_count" -gt 0 ] && printf ' (別に保留 %d 件)' "$held_count"
  printf '\n'
  [ -n "$dated" ] && printf '%s' "$dated" | sort -k1,1nr | cut -f2-
  [ -n "$odd" ] && printf '%s' "$odd"
  printf '残課題を issue / _claude/rules/ へ切り出す (または却下を理由つきで明記する) と %s/done/ へ移動できる。\n' "${dir#"$root"/}"
)

issue_hook_emit '未決着の retro issue (セッションの振り返り) がある。古いものが溜まっていれば最初に一言で伝えること:' "$report"
exit 0
