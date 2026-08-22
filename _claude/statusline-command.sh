#!/usr/bin/env bash
# Claude Code statusLine command
# Mirrors the zsh PROMPT configuration from ~/.zshrc
#
# ⚠️ shebang は bash 必須 (sh 不可)。パス整形が bash 専用の置換に依存している:
#   ${cwd/#$home/$tilde} (先頭一致の置換) / ${short_cwd: -48} (末尾からの部分文字列)。
#   後者は bash が文字単位で数えるため、日本語を含むパスでも文字境界で切れる (POSIX の
#   tail -c はバイト単位なのでマルチバイト文字を割る)。
#   macOS の /bin/sh は bash なので #!/bin/sh でも動いていたが、Linux (dash) では
#   「Bad substitution」で即死する。CI で実際に踏んで発覚 (2026-07-25)。

input=$(cat)

# stdin JSON から必要な値を 1 回の jq でまとめて読む。
# ⚠️ フィールドごとに jq を起動しないこと: statusline は再描画ごとに走るため、以前は
# 1 レンダーで jq を 10 プロセス起動して実測 26ms (レンダー全体 75ms) を払っていた。
# ⚠️ jq の出力順とこの read 順は 1:1 の契約。フィールドを足すときは両方を同じ位置に足す。
# ⚠️ 各フィールドは `// ""` で必ず 1 行出すこと (`// empty` だと値が無い行が消えて以降が全部ズレる)。
# 値が空のときの下流の扱いは従来と同じ ([ -n "$x" ] ガード)。
{
  IFS= read -r cwd
  IFS= read -r model_name
  IFS= read -r five_pct
  IFS= read -r seven_pct
  IFS= read -r five_reset
  IFS= read -r seven_reset
  IFS= read -r ctx_used
  IFS= read -r ctx_size
  IFS= read -r ctx_pct
  IFS= read -r effort_level
  IFS= read -r transcript
} <<JSON_FIELDS
$(printf '%s' "$input" | jq -r '
  (.workspace.current_dir // .cwd // ""),
  (.model.display_name // ""),
  (.rate_limits.five_hour.used_percentage // ""),
  (.rate_limits.seven_day.used_percentage // ""),
  (.rate_limits.five_hour.resets_at // ""),
  (.rate_limits.seven_day.resets_at // ""),
  (.context_window.total_input_tokens // ""),
  (.context_window.context_window_size // ""),
  (.context_window.used_percentage // 0),
  (.effort.level // ""),
  (.transcript_path // "")
' 2>/dev/null)
JSON_FIELDS

# 数値フィールドは「整数化できなければ空」に正規化してから使う。
# ⚠️ bash の $(( )) は "08" (8 進数として解釈され失敗) / "5E+1" / 全角数字で **fatal**
#   になり、その時点で囲みブロックの残りが丸ごとスキップされる (実測: 3 行目が無言で
#   消え、2 行目のバーが空になる)。数値でない値を数値として扱わないのが前提の是正で、
#   空にしておけば下流の [ -n "$x" ] ガードが「そのセグメントを出さない」に落とす。
#   小数は切り捨てる (API は 62.7 のような値を返す)。
to_int() {  # 結果は REPLY (整数化できなければ空)。$(...) を使わないため出力しない
  REPLY=${1%%.*}
  # ⚠️ 数字の判定に範囲 [!0-9] を使わないこと。locale の照合順では全角数字が 0-9 の
  #   範囲に入り、"６２" が素通りする (実測: そのまま $(( )) へ流れて fatal)。列挙で書く。
  case "$REPLY" in
    ''|*[!0123456789]*) REPLY=""; return ;;   # 空 / 数字以外 (符号・小数点以外の記号・指数・全角)
  esac
  # 先頭ゼロを落とす ("08" は $(( )) で 8 進数扱いになり失敗する)。ここでも算術は
  # 使わない: 桁あふれや想定外の値で fatal になる経路をこの関数に残さないため。
  while [ "${#REPLY}" -gt 1 ] && [ "${REPLY#0}" != "$REPLY" ]; do REPLY=${REPLY#0}; done
}
to_int "$five_pct";  five_pct=$REPLY
to_int "$seven_pct"; seven_pct=$REPLY
to_int "$ctx_used";  ctx_used=$REPLY
to_int "$ctx_size";  ctx_size=$REPLY
to_int "$ctx_pct";   ctx_pct=${REPLY:-0}

# Shorten the path: replace $HOME with ~, truncate to 50 chars with leading ..
# (置換文字列の ~ は変数経由で渡す。リテラル \~ だとバックスラッシュごと表示される)
home="$HOME"
tilde="~"
short_cwd="${cwd/#$home/$tilde}"
if [ ${#short_cwd} -gt 50 ]; then
  short_cwd="..${short_cwd: -48}"
fi

# Git branch via vcs_info equivalent
branch=""
changed_count=0
untracked_count=0
if git -C "$cwd" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  branch=$(git -C "$cwd" symbolic-ref --short HEAD 2>/dev/null || git -C "$cwd" rev-parse --short HEAD 2>/dev/null)
  porcelain=$(git -C "$cwd" status --porcelain 2>/dev/null)
  if [ -n "$porcelain" ]; then
    # 変更数と untracked 数は 1 回の awk で数える (grep -c を 2 回起動しない。
    # 判定は従来と同じ「行頭が ? でない = 変更」「?? = untracked」)
    counts=$(printf "%s\n" "$porcelain" | awk '/^\?\?/ { u++ } /^[^?]/ { c++ } END { print (c+0), (u+0) }')
    changed_count=${counts% *}
    untracked_count=${counts#* }
  fi
fi

# ANSI colors
reset="\033[0m"
bold="\033[1m"
black_fg="\033[30m"
green_bg="\033[42m"
cyan_fg="\033[36m"
magenta_fg="\033[35m"
red_fg="\033[31m"
green_fg="\033[32m"
yellow_fg="\033[33m"

# Directory segment (bold path).
dir_part="${bold}${short_cwd}${reset}"

# Branch segment (leading space, empty when not in a git repo).
branch_part=""
if [ -n "$branch" ]; then
  git_info="${branch}"
  if [ "$changed_count" -gt 0 ] || [ "$untracked_count" -gt 0 ]; then
    git_info="${git_info} ~${changed_count} ?${untracked_count}"
  fi
  branch_part=" ${black_fg}${green_bg}[${git_info}]${reset}"
fi

# Rate limits with visual bar
# Usage color: green (<50%) -> yellow (50-79%) -> red (>=80%)
rate_color() {
  pct=$1
  if [ "$pct" -ge 80 ]; then
    printf "\033[31m"  # red
  elif [ "$pct" -ge 50 ]; then
    printf "\033[33m"  # yellow
  else
    printf "\033[32m"  # green
  fi
}

# Build a 4-slot bar: e.g. [||..] for 50%
rate_bar() {
  pct=$1
  filled=$(( (pct + 12) / 25 ))  # nearest-quarter rounding: 0-12%->0, 13-37%->1, 38-62%->2, 63-87%->3, 88-100%->4
  [ "$filled" -gt 4 ] && filled=4
  empty=$(( 4 - filled ))
  bar=""
  i=0; while [ $i -lt $filled ]; do bar="${bar}█"; i=$((i+1)); done
  i=0; while [ $i -lt $empty ];  do bar="${bar}░"; i=$((i+1)); done
  printf "%s" "$bar"
}

# Remaining-time label until a reset epoch: "3日2時間" / "1時間23分" / "45分"。
# ⚠️ 結果は stdout でなく REPLY で返す。statusline は再描画ごとに走るため、$(...) は
# 1 呼び出しごとに subshell を fork する (ファイル冒頭の jq 一括化と同じ理由)。
fmt_remaining() {
  secs=$1
  [ "$secs" -lt 0 ] && secs=0
  d=$(( secs / 86400 ))
  h=$(( (secs % 86400) / 3600 ))
  m=$(( (secs % 3600) / 60 ))
  if [ "$d" -gt 0 ]; then
    printf -v REPLY "%d日%d時間" "$d" "$h"
  elif [ "$h" -gt 0 ]; then
    printf -v REPLY "%d時間%d分" "$h" "$m"
  else
    printf -v REPLY "%d分" "$m"
  fi
}

# epoch を "8月24日21:31" のような表示に整形する。結果は REPLY (整形できなければ空)。
# ⚠️ epoch から日時への変換はプラットフォームで別物: BSD (macOS) は `date -r <epoch>`、
#   GNU (Linux/CI) の -r は「参照ファイルの時刻」なので epoch をファイル名として探し、
#   `date: 1787410219: No such file or directory` を stderr へ吐いて何も出さない
#   (CI で実測: 2 行目のリセット日時が Linux では前からずっと空だった)。
#   両方試して先に成功した方を採る。stderr は捨てる (statusline に出す先が無く、
#   出しても "沈黙" と区別できないため。整形できなければ REPLY が空になり、
#   呼び出し側が日時部分を落とす = 表示の劣化だけで済む)。
fmt_epoch() {
  REPLY=$(date -r "$1" "$2" 2>/dev/null || date -d "@$1" "$2" 2>/dev/null)
}

# Short human label for a token count: <1M -> "269k", >=1M -> "1M" / "1.5M".
human_tokens() {
  n=$1
  if [ "$n" -ge 1000000 ]; then
    if [ $(( n % 1000000 )) -eq 0 ]; then
      printf "%dM" $(( n / 1000000 ))
    else
      printf "%d.%dM" $(( n / 1000000 )) $(( (n % 1000000) / 100000 ))
    fi
  else
    printf "%dk" $(( (n + 500) / 1000 ))
  fi
}

rate_part=""
# five_pct / seven_pct / five_reset / seven_reset は冒頭の一括 jq で読んでいる
# (resets_at = 各ウィンドウがリセットされる時刻 (Unix epoch 秒)。残り時間表示に使う)
# 残り時間ラベルの色。90 (dark gray) は暗すぎたので 37 (light gray) にしている
gray_fg="\033[37m"
now=$(date +%s)

# リセット時刻到達後の強調色。SGR 5 (blink) は端末/tmux の対応に依存するため、
# 再描画ごとに epoch 秒の偶奇で赤/黄を入れ替える擬似点滅を重ねる
# (描画が更新されない間は片方の色で止まる)。
blink_color() {
  if [ $(( now % 2 )) -eq 0 ]; then
    printf "\033[5;1;31m"  # blink bold red
  else
    printf "\033[5;1;33m"  # blink bold yellow
  fi
}

# 1 ウィンドウ分のセグメント: "5h:[████]87%(残:1時間23分)"。
# resets_at を過ぎてもデータが更新されるまでは消さず、"(リセット!)" を点滅表示する。
rate_segment() {
  seg_label=$1; seg_pct=$2; seg_reset_at=$3
  p=$seg_pct
  printf "%s%s:[%s]%s%%%s" "$(rate_color "$p")" "$seg_label" "$(rate_bar "$p")" "$p" "$reset"
  if [ -n "$seg_reset_at" ] && [ "$seg_reset_at" -gt "$now" ] 2>/dev/null; then
    # %-m / %-d はゼロ埋めなし (BSD/GNU の差は fmt_epoch が吸収する)
    fmt_remaining $(( seg_reset_at - now ))
    seg_remaining=$REPLY
    fmt_epoch "$seg_reset_at" "+%-m月%-d日%H:%M"
    if [ -n "$REPLY" ]; then
      printf "%s(残:%s / %s)%s" "$gray_fg" "$seg_remaining" "$REPLY" "$reset"
    else
      printf "%s(残:%s)%s" "$gray_fg" "$seg_remaining" "$reset"
    fi
  elif [ -n "$seg_reset_at" ] && [ "$seg_reset_at" -gt 0 ] 2>/dev/null; then
    printf "%s(リセット!)%s" "$(blink_color)" "$reset"
  fi
}

if [ -n "$five_pct" ] || [ -n "$seven_pct" ]; then
  parts=""
  if [ -n "$five_pct" ]; then
    parts="$(rate_segment 5h "$five_pct" "$five_reset")"
  fi
  if [ -n "$seven_pct" ]; then
    if [ -n "$parts" ]; then parts="$parts "; fi   # 5h の後ろに区切りの空白
    parts="${parts}$(rate_segment 7d "$seven_pct" "$seven_reset")"
  fi
  rate_part=" ${parts}"
fi

# 3 行目: weekly (7d) ウィンドウの消化ペース。
# weekly の窓幅は 7 日固定なので、resets_at から「窓のどこまで来たか」= 想定消化率
# (経過割合) を出し、実績 (used_percentage) との差 pt で先行/余裕を表す。
# 「残り 1 日で 50% 余っている」= 余らせ過ぎ、「残り 5 日で 80% 使った」= 超過、を
# 一目で判定するための行 (残量% だけでは窓のどこにいるか分からない)。
# ⚠️ 窓幅 7 日は API から取れないため定数。resets_at - now が 7 日を超える形で
#   返ってきたら経過 0 に clamp する (負の経過で想定率がマイナスになるのを防ぐ)。
pace_part=""
week_secs=604800
# ⚠️ 条件は `-gt "$now"` (「窓の中にいる」)。リセット済み / 未更新のデータを弾く役目と、
#   下の 1 日予算が pace_rem で割るための「残り >= 1 秒」保証を兼ねている。緩めると
#   0 除算で 3 行目が無言で消える (tests/claude/test_statusline.sh が境界の両側を固定)。
if [ -n "$seven_pct" ] && [ -n "$seven_reset" ] && [ "$seven_reset" -gt "$now" ] 2>/dev/null; then
  pace_used=$seven_pct
  pace_rem=$(( seven_reset - now ))
  [ "$pace_rem" -gt "$week_secs" ] && pace_rem=$week_secs
  pace_exp=$(( (week_secs - pace_rem) * 100 / week_secs ))
  pace_delta=$(( pace_used - pace_exp ))

  # 乖離 pt を「何日分か」に換算する: 1 日分 = 100/7 = 14.29pt なので 日 = pt * 7 / 100。
  # 小数第 1 位まで見せたいので 100 倍のまま持ち、表示時に整数部/小数部へ割る。
  pace_abs=$pace_delta
  [ "$pace_abs" -lt 0 ] && pace_abs=$(( -pace_abs ))
  pace_days100=$(( pace_abs * 7 ))
  pace_days="$(( pace_days100 / 100 )).$(( (pace_days100 % 100) / 10 ))"

  # 想定線からの乖離 pt で 5 段階。±10pt (≒0.7 日分のズレ) を「想定通り」の帯とし、
  # 超過側/余裕側に 2 段ずつ。余らせ過ぎは異常ではなく「使えるのに使っていない」信号
  # なので警告色 (赤/黄) を使わず magenta にする。
  if [ "$pace_delta" -ge 20 ]; then
    pace_color="$red_fg";     pace_label="超過";       pace_advice="${pace_days}日分の前借り・使うのを絞る"
  elif [ "$pace_delta" -ge 10 ]; then
    pace_color="$yellow_fg";  pace_label="先行";       pace_advice="${pace_days}日分の前借り・やや速い"
  elif [ "$pace_delta" -ge -10 ]; then
    pace_color="$green_fg";   pace_label="想定通り";   pace_advice="このままでちょうど"
  elif [ "$pace_delta" -ge -25 ]; then
    pace_color="$cyan_fg";    pace_label="余裕";       pace_advice="${pace_days}日分の余り・もう少し使える"
  else
    pace_color="$magenta_fg"; pace_label="余らせ過ぎ"; pace_advice="${pace_days}日分の使い残し・かなり余る"
  fi

  # 10 スロットのバーに想定位置を | で刻む: [███|█████░░] なら想定線を 5 スロット
  # 追い越している = 使い過ぎ。バー長 (10) が刻みの分解能でもある。
  # ⚠️ 実績が 100% を超え、かつ窓の終盤 (想定 >= 95%) では塗りと想定線がどちらも
  #   右端に飽和し、バーだけでは先行が見えなくなる。バーを歪めて表現するより
  #   「(+12pt 先行)」の数値ラベルに任せる (バーは概観、符号は数値が持つ)。
  pace_fill=$(( (pace_used * 10 + 50) / 100 ))
  [ "$pace_fill" -gt 10 ] && pace_fill=10
  pace_mark=$(( (pace_exp * 10 + 50) / 100 ))
  [ "$pace_mark" -gt 10 ] && pace_mark=10
  pace_bar=""
  i=0
  while [ $i -lt 10 ]; do
    [ $i -eq "$pace_mark" ] && pace_bar="${pace_bar}|"
    if [ $i -lt "$pace_fill" ]; then pace_bar="${pace_bar}█"; else pace_bar="${pace_bar}░"; fi
    i=$((i+1))
  done
  [ "$pace_mark" -eq 10 ] && pace_bar="${pace_bar}|"

  # 残り時間と、それで割った 1 日あたり予算。窓終端 (残 0) では除算できないので省く。
  # 残り時間の表記は 2 行目 (rate limit の "残:") と同じ fmt_remaining を使う
  # (「残5.4日」のような小数表記より「残5日9時間」の方が体感に直結する。表記を
  # 2 か所に持たないことで、片方だけ書式が変わる乖離も起きない)。
  pace_burn10=$(( (100 - pace_used) * 864000 / pace_rem ))
  [ "$pace_burn10" -lt 0 ] && pace_burn10=0
  [ "$pace_burn10" -gt 9999 ] && pace_burn10=9999
  fmt_remaining "$pace_rem"
  printf -v pace_budget " / 残%s %d.%d%%/日" "$REPLY" \
    $(( pace_burn10 / 10 )) $(( pace_burn10 % 10 ))

  # 状態色は「バー〜ラベル」まで。アドバイスと 1 日予算はグレーで従属させる
  # (2 行目の rate limit 行が同じ配色規則なので視線の流れが揃う)。
  printf -v pace_part "%b7dペース:[%s] 実績%d%% 想定%d%% (%+dpt %s)%b %s%s%s%b" \
    "$pace_color" "$pace_bar" "$pace_used" "$pace_exp" "$pace_delta" "$pace_label" \
    "$reset" "$gray_fg" "$pace_advice" "$pace_budget" "$reset"
fi

# Model segment (leading space, empty when not provided).
model_part=""
if [ -n "$model_name" ]; then
  model_part=" ${cyan_fg}[${model_name}]${reset}"
fi

# Context window usage segment (sits to the right of the model). Claude Code
# provides the live numbers on stdin under .context_window, so we read them
# directly: total_input_tokens is what occupies the window,
# context_window_size is the model's limit, used_percentage drives the
# fullness color. (No transcript parsing: this is exact and per-render cheap.)
ctx_part=""
if [ -n "$ctx_used" ] && [ "$ctx_used" -gt 0 ] 2>/dev/null; then
  used_label=$(human_tokens "$ctx_used")
  if [ -n "$ctx_size" ] && [ "$ctx_size" -gt 0 ] 2>/dev/null; then
    ctx_disp="${used_label}/$(human_tokens "$ctx_size")"
  else
    ctx_disp="$used_label"
  fi
  cc=$(rate_color "$ctx_pct")
  ctx_part=" ${cc}[ctx:${ctx_disp}]${reset}"
fi

# Effort segment (leading space). .effort.level is the current reasoning
# effort (e.g. low / medium / high / xhigh), provided directly on stdin.
effort_part=""
if [ -n "$effort_level" ]; then
  effort_part=" ${magenta_fg}[effort:${effort_level}]${reset}"
fi

# Advisor segment (effort の右). advisor は executor(本体モデル) と対の概念なので
# 設定時の色は model セグメントと同じ cyan に揃える。未設定時は赤で「未設定」と明示。
# statusLine の stdin JSON に advisor フィールドは無い (公式スキーマで確認済み) ため、
# transcript から読む: 各 assistant 行トップレベルの .advisorModel が現在値 (未選択時
# はキー自体が無い)。末尾の該当行が現在の選択を反映する (/advisor 変更後、次の
# assistant ターンで更新される)。transcript_path も stdin JSON から取る。
# settings.json の advisorModel は使わない: user 設定のみで project/local 上書きを
# 取りこぼす上、運用によっては頻繁にリセットされ (dotfiles では make pull) 「実際に
# 効いている advisor」を transcript ほど正確に反映しないため。代償は設定直後〜次の
# assistant ターンまで赤い「未設定」が出る 1 ターンのラグ (自己修復する)。
advisor_label="未設定"
advisor_color="$red_fg"
if [ -n "$transcript" ] && [ -f "$transcript" ]; then
  # 末尾から辿り、最初に見つかった advisorModel 行 = 最新。行を逆順に流すコマンドは
  # 環境で違う (GNU: tac / BSD・macOS: tail -r) ので、OS 名ではなく tac の有無で選ぶ
  # (Darwin + coreutils・FreeBSD・GNU をどれも取り違えない)。`command -v` は bash の
  # builtin でフォークは増えない。tac も tail -r も無い環境 (applet を削った busybox 等)
  # では advisor が黙って「未設定」に劣化する — 表示の劣化だけで他への影響はない。
  # 全行 grep して tail -1 を採る方式にはしない: advisor 設定済みなら該当行は末尾付近に
  # あり、逆順 + grep -m1 は巨大な transcript でも最初の一致で打ち切れる (該当行が 1 つも
  # 無いときだけは、どちらの方式でも全走査になる)。
  if command -v tac >/dev/null 2>&1; then
    advisor_rev_cat="tac"
  else
    advisor_rev_cat="tail -r"
  fi
  advisor_model=$($advisor_rev_cat "$transcript" 2>/dev/null | grep -m1 '"advisorModel"' | jq -r '.advisorModel // empty' 2>/dev/null)
  if [ -n "$advisor_model" ]; then
    # 表示名は id の構造から導出する (model.display_name のような整形名は stdin に無い)。
    # `claude-<family>-<major>[-<minor>]` の family を頭大文字にし、続く 1〜2 桁の数値
    # フィールドを "." で繋ぐ: claude-opus-4-8 → Opus 4.8 / claude-haiku-4-5-20251001 →
    # Haiku 4.5 (数値でないフィールドで打ち切るので末尾の日付は落ちる)。version に食い込む
    # 修飾子は先に切る: Vertex の `@YYYYMMDD` と Bedrock の `:0` を残すと、minor が
    # 「数値でない」と判定されて claude-sonnet-4-5@20250929 が "Sonnet 4" になる (誤表示は
    # 生 id 表示より悪い)。
    # id → 表示名のハードコード表は持たない: 新モデルが出るたび追加漏れでドリフトする
    # (claude-opus-5 が未登録で生の id を出していた実例あり、2026-07-27 に発覚)。
    # 導出できない形 (旧 `claude-3-5-sonnet-*` のような version 先行 id、version を持たない
    # id、provider 修飾付きの `us.anthropic.claude-*`) は従来どおり生の id をそのまま出す。
    advisor_label="$advisor_model"
    if [ "${advisor_model#claude-}" != "$advisor_model" ]; then
      advisor_rest="${advisor_model#claude-}"
      advisor_rest="${advisor_rest%%@*}"
      advisor_rest="${advisor_rest%%:*}"
      advisor_family="${advisor_rest%%-*}"
      advisor_ver=""
      if [[ "$advisor_family" =~ ^[a-z]+$ && "$advisor_rest" == *-* ]]; then
        IFS='-' read -r -a advisor_ver_fields <<< "${advisor_rest#*-}"
        for advisor_field in "${advisor_ver_fields[@]}"; do
          [[ "$advisor_field" =~ ^[0-9]{1,2}$ ]] || break
          advisor_ver="${advisor_ver:+$advisor_ver.}$advisor_field"
        done
      fi
      [ -n "$advisor_ver" ] && advisor_label="${advisor_family^} $advisor_ver"
    fi
    advisor_color="$cyan_fg"
  fi
fi
advisor_part=" ${advisor_color}[advisor:${advisor_label}]${reset}"

# 1 行目: directory, branch, model, context, effort, advisor / 2 行目: rate limits /
# 3 行目: weekly の消化ペース。
# statusline は複数行出力をサポートする (公式 docs の Display multiple lines)。
# rate limit が無いとき (Free tier 等) は 2 行目自体を出さない。3 行目は 7d の
# used_percentage と resets_at が両方揃ったときだけ出す。
# Each non-first segment carries its own leading space. (No right-alignment:
# the statusLine command runs without a controlling TTY so `tput cols` reports
# the wrong width and the line would overflow past the right edge.)
printf "%b%b%b%b%b%b" "$dir_part" "$branch_part" "$model_part" "$ctx_part" "$effort_part" "$advisor_part"
if [ -n "$rate_part" ]; then
  printf "\n%b" "${rate_part# }"
fi
if [ -n "$pace_part" ]; then
  printf "\n%b" "$pace_part"
fi
