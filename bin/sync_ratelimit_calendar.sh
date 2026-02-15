#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG="${SCRIPT_DIR}/../ratelimit_resets.yaml"
TMPDIR_ICS="$(mktemp -d)"
PREFIX="[RateLimit]"

cleanup() {
  rm -rf "$TMPDIR_ICS"
}
trap cleanup EXIT

# 依存コマンドチェック
for cmd in gcalcli yq; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "Error: $cmd が見つかりません。brew install $cmd でインストールしてください。" >&2
    exit 1
  fi
done

if [ ! -f "$CONFIG" ]; then
  echo "Error: 設定ファイルが見つかりません: $CONFIG" >&2
  exit 1
fi

# 曜日名 → 次の該当日の日付 (YYYYMMDD)
next_day_date() {
  local day_name="$1"
  local target_dow

  case "$day_name" in
    Monday)    target_dow=1 ;;
    Tuesday)   target_dow=2 ;;
    Wednesday) target_dow=3 ;;
    Thursday)  target_dow=4 ;;
    Friday)    target_dow=5 ;;
    Saturday)  target_dow=6 ;;
    Sunday)    target_dow=7 ;;
    *) echo "Error: 不明な曜日: $day_name" >&2; exit 1 ;;
  esac

  local current_dow
  current_dow=$(date +%u)
  local days_until=$(( (target_dow - current_dow + 7) % 7 ))
  if [ "$days_until" -eq 0 ]; then
    days_until=7
  fi

  date -v+"${days_until}"d +%Y%m%d
}

# ICS ファイル生成 (週次繰り返しイベント)
generate_ics() {
  local name="$1"
  local day="$2"
  local time="$3"
  local output="$4"

  local date_str
  date_str=$(next_day_date "$day")
  local time_str="${time/:/}00" # HH:MM -> HHMMSS

  cat > "$output" <<EOF
BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//dotfiles//ratelimit//EN
BEGIN:VEVENT
DTSTART;TZID=Asia/Tokyo:${date_str}T${time_str}
DURATION:PT15M
RRULE:FREQ=WEEKLY
SUMMARY:${PREFIX} ${name}
DESCRIPTION:Weekly rate limit reset - ${name}
END:VEVENT
END:VCALENDAR
EOF
}

# --- メイン処理 ---

count=$(yq '.services | length' "$CONFIG")
echo "==> ${count} 件のサービスを処理します"

# 1. 既存の [RateLimit] イベントを削除
echo "==> 既存の ${PREFIX} イベントを削除中..."
if gcalcli search "$PREFIX" 2>/dev/null | grep -q "$PREFIX"; then
  gcalcli delete --iamaexpert "$PREFIX" 2>/dev/null || true
  echo "    削除完了"
else
  echo "    既存イベントなし"
fi

# 2. 各サービスの繰り返しイベントを作成
for i in $(seq 0 $((count - 1))); do
  name=$(yq -r ".services[$i].name" "$CONFIG")
  day=$(yq -r ".services[$i].day" "$CONFIG")
  time=$(yq -r ".services[$i].time" "$CONFIG")
  calendar=$(yq -r ".services[$i].calendar" "$CONFIG")

  echo "==> [${name}] ${day} ${time} JST"

  ics_file="${TMPDIR_ICS}/event_${i}.ics"
  generate_ics "$name" "$day" "$time" "$ics_file"

  if [ "$calendar" = "default" ]; then
    gcalcli import "$ics_file"
  else
    gcalcli import --calendar "$calendar" "$ics_file"
  fi

  echo "    登録完了"
done

echo ""
echo "==> 全て完了！ gcalcli agenda で確認できます"
