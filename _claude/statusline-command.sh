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

# Remaining-time label until a reset epoch: "3日2時間" / "1時間23分" / "45分".
fmt_remaining() {
  secs=$1
  [ "$secs" -lt 0 ] && secs=0
  d=$(( secs / 86400 ))
  h=$(( (secs % 86400) / 3600 ))
  m=$(( (secs % 3600) / 60 ))
  if [ "$d" -gt 0 ]; then
    printf "%d日%d時間" "$d" "$h"
  elif [ "$h" -gt 0 ]; then
    printf "%d時間%d分" "$h" "$m"
  else
    printf "%d分" "$m"
  fi
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
  p=${seg_pct%.*}
  printf "%s%s:[%s]%s%%%s" "$(rate_color "$p")" "$seg_label" "$(rate_bar "$p")" "$p" "$reset"
  if [ -n "$seg_reset_at" ] && [ "$seg_reset_at" -gt "$now" ] 2>/dev/null; then
    # date -r は BSD (macOS) 形式。%-m / %-d はゼロ埋めなし
    printf "%s(残:%s / %s)%s" "$gray_fg" "$(fmt_remaining $(( seg_reset_at - now )))" "$(date -r "$seg_reset_at" "+%-m月%-d日%H:%M")" "$reset"
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
  cc=$(rate_color "${ctx_pct%.*}")
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

# 1 行目: directory, branch, model, context, effort, advisor / 2 行目: rate limits。
# statusline は複数行出力をサポートする (公式 docs の Display multiple lines)。
# rate limit が無いとき (Free tier 等) は 2 行目自体を出さない。
# Each non-first segment carries its own leading space. (No right-alignment:
# the statusLine command runs without a controlling TTY so `tput cols` reports
# the wrong width and the line would overflow past the right edge.)
printf "%b%b%b%b%b%b" "$dir_part" "$branch_part" "$model_part" "$ctx_part" "$effort_part" "$advisor_part"
if [ -n "$rate_part" ]; then
  printf "\n%b" "${rate_part# }"
fi
