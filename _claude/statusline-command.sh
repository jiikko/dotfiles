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

# five_pct / seven_pct / five_reset / seven_reset は冒頭の一括 jq で読んでいる
# (resets_at = 各ウィンドウがリセットされる時刻 (Unix epoch 秒)。残り時間表示に使う)
# 目盛り (想定%) の色。90 (dark gray) は暗すぎたので 37 (light gray) にしている
gray_fg="\033[37m"
dim_fg="\033[90m"        # ペース行の「まだ来ていない未来」だけに使う (地の色として沈ませたい)
bg_in="\033[42;30m"      # ペース行: 想定内の消化 (緑背景 + 黒文字。1 行目の branch と同配色)
bg_over="\033[41;30m"    # ペース行: 前借り (赤背景 + 黒文字)
under_sgr="\033[4;1m"    # ペース行の当日 (背景色と反転は競合するので下線を使う)

now=$(date +%s)
# 最も広い窓のスロット数 (7d = 7)。狭い窓は括弧の後ろをこの幅まで空白で埋めて、
# 行をまたいだ数値の縦を揃える
PACE_MAX_CELLS=7

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

# ペース行: 各ウィンドウの消化ペースを 1 行で出す。
# 窓を等分したスロット (時 / 日) を「番号 + 空白」の 2 カラムとして並べ、その「背景」で
# 消化量を描く。カラム単位に塗るので 0.5 スロット (半日 / 半時間) の端数まで見える。
#   背景緑 = 想定内の消化 / 背景赤 = 前借り / シアン = 使えるのに使っていない過去 /
#   暗灰 = まだ来ていない未来。いま居るスロットは下線。
# 「残り 1 日で 50% 余っている」= 余らせ過ぎ、「残り 5 日で 80% 使った」= 超過、を
# 一目で判定するための行 (残量% だけでは窓のどこにいるか分からない)。
#
# $1 = ラベル ("5h" / "7d") / $2 = 窓の種別 (hour | day) / $3 = used% (整数) / $4 = resets_at
# 結果は PACE_ROW (組めなければ空)。$(...) を使わないため出力しない。
#
# ⚠️ 窓幅は API から取れないため種別ごとの定数。resets_at - now が窓幅を超える形で
#   返ってきたら経過 0 に clamp する (負の経過で想定率がマイナスになるのを防ぐ)。
# ⚠️ 想定帯 (band) は種別ごとに変える。5 時間窓は本質的にバースト的で、作業中は
#   「1 時間目に 40% 使った」= +20pt が常態になる。7d と同じ ±10pt では赤が出続けて
#   信号にならないので ±25pt (= ±1.25 時間) にしている。
pace_row() {
  PACE_ROW=""
  local pr_label=$1 pr_kind=$2 pr_used=$3 pr_reset=$4
  local pr_window pr_cell pr_band pr_bunit pr_aunit pr_efmt
  case $pr_kind in
    hour) pr_window=18000;  pr_cell=3600;  pr_band=25; pr_bunit="時"; pr_aunit="時間"; pr_efmt="+%H:%M" ;;
    # ⚠️ 7d は絶対時刻を出さない (pr_efmt が空)。残り日数で十分で、"8月26日19:18" は
    #   14 カラム使う割に読まれない。5h は「何時に戻るか」が効くので時刻だけ出す。
    day)  pr_window=604800; pr_cell=86400; pr_band=10; pr_bunit="日"; pr_aunit="日";   pr_efmt="" ;;
    *) return ;;
  esac
  [ -n "$pr_used" ] || return
  # resets_at が無い / 数値でない = 窓のどこにいるか分からない。ゲージも想定も出せないので
  # 残量% だけを出す (残量% はどの経路でも必ずどこかに出る、が不変条件)。
  if [ -z "$pr_reset" ] || ! [ "$pr_reset" -ge 0 ] 2>/dev/null; then
    printf -v PACE_ROW "%s %b%3d%%%b" "$pr_label" "$(rate_color "$pr_used")" "$pr_used" "$reset"
    return
  fi
  # リセット時刻を過ぎている = 窓は終わったのにデータが更新されていない。窓の中の
  # 「想定ペース」に意味が無くなる (想定 100% との比較は「余らせ過ぎ」という逆向きの助言に
  # なる) ので、ゲージは「使った分 (緑) と使い残し (シアン)」だけにして、点滅する
  # (リセット!) を出す。以前は 2 行目の旧セグメント形式へフォールバックしていたが、
  # 同じウィンドウが 2 つの見た目を持つのをやめた。
  local pr_stale=0
  if [ "$pr_reset" -le "$now" ]; then
    pr_stale=1
  fi

  local pr_rem=$(( pr_reset - now ))
  [ "$pr_stale" -eq 1 ] && pr_rem=0
  [ "$pr_rem" -gt "$pr_window" ] && pr_rem=$pr_window
  local pr_elapsed=$(( pr_window - pr_rem ))
  local pr_exp=$(( pr_elapsed * 100 / pr_window ))
  local pr_delta=$(( pr_used - pr_exp ))
  # ⚠️ stale では pr_rem が 0 なので、以降で pr_rem を割ってはいけない (予算は出さない)。
  local pr_ncells=$(( pr_window / pr_cell ))          # 窓が名目上いくつのセルか (5 / 7)

  # 格子は「窓を ncells 等分したスロット」。位置は「1 セル = 20」の固定小数で持ち、
  # 整数演算だけで済ませる。
  # ⚠️ カレンダー基準 (ローカルの 0 時 / 毎時 00 分に揃えた格子) にはしない。以前は
  #   曜日ラベルを出していたため必要だったが (窓の区切りが reset 時刻なので、窓を等分した
  #   セルの開始日は実際の今日と最大 12 時間ずれる)、**曜日を出さなくなった時点でカレンダー
  #   基準は何も買わず、端に半端なセルを 1 つ増やすだけ**になった (5 時間窓が 6 セル、
  #   7 日窓が 8 セルになり、両端が窓に少ししか掛からないセルになる)。
  #   曜日・日付をバーに戻すときはカレンダー基準に戻す必要がある。
  local pr_fill=$(( pr_used * pr_ncells * 20 / 100 ))
  [ "$pr_fill" -gt $(( pr_ncells * 20 )) ] && pr_fill=$(( pr_ncells * 20 ))
  local pr_mark=$(( pr_elapsed * 20 / pr_cell ))
  # カラム数に落とす。1 スロットは 2 カラム (「半角数字 + 空白」) で、カラム単位に塗るので
  # 0.5 スロット (半日 / 半時間) の端数まで見える。
  # 切り上げ (少しでも掛かったカラムは掛かっている扱い) を塗りと想定線の両方に同じ規則で
  # 使う。四捨五入にすると、窓の終端がカラムの手前に落ちたときに **今いるカラムが
  # 塗られない** (実測: 残 4 時間で当日のセルが暗いまま)。
  local pr_nfill=$(( (pr_fill + 9) / 10 ))
  local pr_nmark=$(( (pr_mark + 9) / 10 ))
  # 塗りの色は数値ラベルと同じ想定帯に従わせる。
  # ⚠️ 帯の中では前借り (赤) / 使い残し (シアン) を出さない。1 カラム = 50/ncells pt なので、
  #   乖離が数 pt でもカラム境界を跨げば半日分の赤が出てしまい、「想定通り」のラベルと
  #   矛盾して見える (実測 2026-08-23)。
  # ⚠️ 逆に帯の外では、塗りが窓の右端に clamp されると想定線と同じカラムに入り、超過が
  #   1 マスも出なくなる (実績 115% / 想定 99%)。そのときは想定線を 1 カラム戻す。
  #   余り側に同じ補正は要らない: 塗りは clamp されないので、帯の外なら乖離が
  #   1 カラム (= 50/ncells pt) を必ず超え、切り上げ後も必ず差が出る。
  if [ "$pr_stale" -eq 1 ]; then
    :   # 窓は終わっている: 塗った先は全部「使い残し」= シアン (帯の判定はしない)
  elif [ "$pr_delta" -le "$pr_band" ] && [ "$pr_delta" -ge $(( -pr_band )) ]; then
    pr_nmark=$pr_nfill
  elif [ "$pr_delta" -gt "$pr_band" ] && [ "$pr_nfill" -le "$pr_nmark" ]; then
    pr_nmark=$(( pr_nfill - 1 )); [ "$pr_nmark" -lt 0 ] && pr_nmark=0
  fi

  # 乖離 pt を「何セル分か」に換算する (1 セル = 100/ncells pt)。小数第 1 位まで見せたいので
  # 100 倍のまま持ち、表示時に整数部/小数部へ割る。
  local pr_abs=$pr_delta
  [ "$pr_abs" -lt 0 ] && pr_abs=$(( -pr_abs ))
  local pr_amt100=$(( pr_abs * pr_ncells ))
  local pr_amt="$(( pr_amt100 / 100 )).$(( (pr_amt100 % 100) / 10 ))"

  # 状態色とラベルと、ひとことアドバイス。帯の外に 2 段ずつ置く (先行/超過・余裕/余らせ過ぎ)。
  # ⚠️ 帯の中 (想定通り) も語を出す。空にすると乖離 pt の直後がその行だけ残り時間になり、
  #   他の状態の行と縦が揃わない (実測: 5h に「余裕」があるのに 7d には何も無い、と読めた)。
  # 余らせ過ぎは異常ではなく「使えるのに使っていない」信号なので警告色 (赤/黄) を使わず
  # magenta にする。
  # ⚠️ 100% 到達は乖離に関わらず赤 + 「上限超過」にする。乖離が +18pt でも「先行 (黄)」で
  #   済ませない (上限に届いている事実の方が重い)。
  # ⚠️ 状態の語は全て 2 文字 (4 カラム) に揃える。語の幅が変わると、その後ろの残り時間・
  #   予算が行ごとに横へずれる (実測: 余裕 4 カラム / 想定通り 8 / 余らせ過ぎ 10)。
  # ⚠️ アドバイスは「語で言えないこと」だけを持つ: 帯の中は語 (適正) で足りるので出さない。
  #   命令句 (使うのを絞る / もう少し使える) も語と色が既に言っているので持たない。
  local pr_color pr_word pr_advice pr_advice_sgr=""
  if [ "$pr_stale" -eq 1 ]; then
    pr_color="$(rate_color "$pr_used")"; pr_word=""; pr_advice=""
  elif [ "$pr_used" -ge 100 ]; then
    pr_color="$red_fg";     pr_word=" 上限"; pr_advice="リセット待ち"
    pr_advice_sgr="$bg_over"     # 行動が強制される唯一の状態なので背景で強調する
  elif [ "$pr_delta" -ge $(( pr_band * 2 )) ]; then
    pr_color="$red_fg";     pr_word=" 超過"; pr_advice="${pr_amt}${pr_aunit}分の前借り"
  elif [ "$pr_delta" -ge "$pr_band" ]; then
    pr_color="$yellow_fg";  pr_word=" 先行"; pr_advice="${pr_amt}${pr_aunit}分の前借り"
  elif [ "$pr_delta" -ge $(( -pr_band )) ]; then
    pr_color="$green_fg";   pr_word=" 適正"; pr_advice=""
  elif [ "$pr_delta" -ge $(( -pr_band * 5 / 2 )) ]; then
    pr_color="$cyan_fg";    pr_word=" 余裕"; pr_advice="${pr_amt}${pr_aunit}分の余り"
  else
    pr_color="$magenta_fg"; pr_word=" 余剰"; pr_advice="${pr_amt}${pr_aunit}分の余り"
  fi

  # カラムを組む。1 スロット = 「スロット番号 (半角 1 桁) + 空白」の 2 カラムで、偶数
  # カラムに番号、奇数カラムは空白。塗りはカラムごとなので、番号と空白で色が違えば
  # そのスロットが半分だけ消化されていることを表す。
  # 下線は現在いるスロットの番号に引く (経過を 1 セルで割った位置)。
  # ⚠️ 先頭に空白を 1 つ置く (末尾は最終スロットの空白カラムが担うので、括弧の中が
  #   " 1 2 3 4 5 " になって左右の余白が揃い、数字が括弧に貼り付かない)。この空白も
  #   1 カラム目と同じ塗りにする — 塗らないとゲージの左端に穴が空いて見える。
  local pr_cells="" pr_c=0 pr_ncols=$(( pr_ncells * 2 )) pr_at_col=$(( (pr_elapsed / pr_cell) * 2 ))
  while [ "$pr_c" -lt "$pr_ncols" ]; do
    if [ "$pr_c" -lt "$pr_nfill" ] && [ "$pr_c" -lt "$pr_nmark" ]; then
      pr_cells="${pr_cells}${bg_in}"
    elif [ "$pr_c" -lt "$pr_nfill" ]; then
      pr_cells="${pr_cells}${bg_over}"
    elif [ "$pr_c" -lt "$pr_nmark" ]; then
      pr_cells="${pr_cells}${cyan_fg}"
    else
      pr_cells="${pr_cells}${dim_fg}"
    fi
    [ "$pr_c" -eq 0 ] && pr_cells="${pr_cells} "   # 左端の余白 (1 カラム目と同じ塗り)
    if [ $(( pr_c % 2 )) -eq 0 ]; then
      # 下線は「いま居るスロット」。stale (窓が終わっている) では pr_elapsed が窓幅ちょうどに
      # なり pr_at_col が範囲外 (= ncols) になるので、ここは自然に一致しない
      [ "$pr_c" -eq "$pr_at_col" ] && pr_cells="${pr_cells}${under_sgr}"
      pr_cells="${pr_cells}$(( pr_c / 2 + 1 ))"
    else
      pr_cells="${pr_cells} "
    fi
    pr_cells="${pr_cells}${reset}"
    pr_c=$(( pr_c + 1 ))
  done

  # 5h (5 スロット) と 7d (7 スロット) で数値の縦を揃えるため、狭い方の後ろを空白で埋める。
  # ⚠️ 空白は括弧の**外**に置く。括弧の中に入れると「空のスロット」に見えて、その窓が
  #   5 スロットであること自体が読めなくなる。
  local pr_pad="" pr_padn=$(( (PACE_MAX_CELLS - pr_ncells) * 2 ))
  while [ "$pr_padn" -gt 0 ]; do pr_pad="${pr_pad} "; pr_padn=$(( pr_padn - 1 )); done

  # stale はここで終わり: 残り時間も予算もアドバイスも無く、点滅する (リセット!) を出す。
  if [ "$pr_stale" -eq 1 ]; then
    printf -v PACE_ROW "%s [%b]%s %b%3d%%%b %b(リセット!)%b" \
      "$pr_label" "$pr_cells" "$pr_pad" "$pr_color" "$pr_used" "$reset" \
      "$(blink_color)" "$reset"
    return
  fi

  # 1 セルあたり予算。残りが 1 セル未満のときは %/セル を出さない: 「残 12 時間で
  # 110.0%/日」はその 1 日が来ないので実行不能な数字になる。残枠をそのまま出す。
  local pr_left=$(( 100 - pr_used )) pr_budget pr_burn10
  [ "$pr_left" -lt 0 ] && pr_left=0
  if [ "$pr_rem" -ge "$pr_cell" ]; then
    pr_burn10=$(( pr_left * pr_cell * 10 / pr_rem ))
    printf -v pr_budget "%d.%d%%/%s" $(( pr_burn10 / 10 )) $(( pr_burn10 % 10 )) "$pr_bunit"
  else
    printf -v pr_budget "残枠%d%%" "$pr_left"
  fi

  # 残り時間 + リセットの絶対時刻。絶対時刻が取れない環境 (fmt_epoch 失敗) では括弧を
  # 落とす (中身の無い "()" をぶら下げない)。
  fmt_remaining "$pr_rem"
  local pr_remlab=$REPLY pr_at=""
  # ⚠️ このガードは出力を変えない (空の書式で呼んでも fmt_epoch は空を返す) が、外すと
  #   7d の描画ごとに date が 1〜2 回 fork される。統合テストからは見えないので、
  #   「冗長だから」と外さないこと (冒頭の jq 一括化と同じ理由)。
  if [ -n "$pr_efmt" ]; then
    fmt_epoch "$pr_reset" "$pr_efmt"
    [ -n "$REPLY" ] && pr_at=" ($REPLY)"
  fi

  # アドバイスが無い状態 (適正) では区切りごと落とす (末尾に空白をぶら下げない)。
  # ⚠️ 区切りは中黒でなく空白 1 つ。項目 (残N / N%/日 / N日分の…) は形が違うので
  #   中黒が無くても切れ目が読め、1 行あたり 4 カラム縮む。
  local pr_tail_advice=""
  [ -n "$pr_advice" ] && printf -v pr_tail_advice " %b%s" "$pr_advice_sgr" "$pr_advice"

  # 残り時間・予算・アドバイスも状態色で出す (足りていないのか余っているのかを、行の
  # どこを読んでも同じ色で言う)。想定% だけはグレーのままにする — これは状態ではなく
  # 「比較対象の目盛り」なので、状態色に混ぜると読み手が符号を取り違える。
  # ⚠️ 数値は桁を固定して右詰めにする (使用率・想定率は 3 桁、乖離 pt は符号込み 4 桁)。
  #   桁数で後ろがずれると、5h と 7d の行で同じ項目が縦に揃わない。
  printf -v PACE_ROW "%s [%b]%s %b%3d%%%b %b想定%3d%%%b %b%+4dpt%s %b残%s%s %s%b%b" \
    "$pr_label" "$pr_cells" "$pr_pad" "$pr_color" "$pr_used" "$reset" \
    "$gray_fg" "$pr_exp" "$reset" "$pr_color" "$pr_delta" "$pr_word" \
    "$pr_color" "$pr_remlab" "$pr_at" "$pr_budget" "$pr_tail_advice" "$reset"
}

pace_row 5h hour "$five_pct"  "$five_reset";  pace_five=$PACE_ROW
pace_row 7d day  "$seven_pct" "$seven_reset"; pace_seven=$PACE_ROW

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

# 1 行目: directory, branch, model, context, effort, advisor / 2 行目以降: 各ウィンドウの
# 消化ペース (5h → 7d)。
# statusline は複数行出力をサポートする (公式 docs の Display multiple lines)。
# rate limit が無いとき (Free tier 等) は 2 行目以降を出さない。ペース行は
# used_percentage があれば必ず出す (resets_at が無い / リセット済みのときは、出せる範囲に
# 縮めて出す — 残量% がどの経路でも必ずどこかに出る、が不変条件)。
# Each non-first segment carries its own leading space. (No right-alignment:
# the statusLine command runs without a controlling TTY so `tput cols` reports
# the wrong width and the line would overflow past the right edge.)
printf "%b%b%b%b%b%b" "$dir_part" "$branch_part" "$model_part" "$ctx_part" "$effort_part" "$advisor_part"
if [ -n "$pace_five" ]; then
  printf "\n%b" "$pace_five"
fi
if [ -n "$pace_seven" ]; then
  printf "\n%b" "$pace_seven"
fi
