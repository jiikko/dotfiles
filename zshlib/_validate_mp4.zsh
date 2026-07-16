# shellcheck shell=bash
# shellcheck disable=SC2154,SC2296

# ffprobe 単一フィールド取得は共通ヘルパーを使う (テストが本ファイルを単体 source するため自己 source)
# shellcheck disable=SC1091,SC2296,SC2298  # zsh 固有の自ファイルパス展開 (shellcheck は解析不可)
source "${${(%):-%x}:A:h}/_ffprobe_helpers.zsh"
# ------------------------------------------------------------------------------
# validate-mp4 — MP4 が全フレーム再生可能かを ffmpeg フルデコードで検証する
#
# video_health (_video_health.zsh) と補完関係にある:
#   - video_health : エンコード前の polyglot 入力を ffprobe メタで軽くチェック (高速)
#   - validate_mp4 : .mp4 出力を ffmpeg 全デコードで確実にチェック (重いが確実)
#
# 2 層構造 (_video_health.zsh と同じ契約):
#   __validate_mp4_check <file>   inner: 単一 .mp4 を検証。return 0/1 + REPLY=NG理由
#   validate_mp4 [opts] <file..>  outer: CLI (--mark/--limit/glob/loop/print)
#
# av1ify は inner __validate_mp4_check を in-process で直呼びする
# (_av1ify_encode.zsh の __av1ify_finalize から。先例: __video_health_check)。
# dm (good-chrome-extensions) は bin/validate-mp4 → outer を --mark 付きで呼ぶ。
# このため av1ify 側に `command -v validate-mp4` のような存在チェックは不要
# (av1ify ロード時に _av1ify.zsh が本ファイルを source 済みで関数が必ず居る)。
# ------------------------------------------------------------------------------

# デコード破損を示す既知の文字列。良性警告で誤爆しないよう、具体的なものだけを列挙する。
# (bare "error" 等は入れない — ffmpeg は良性警告にも error の語を含めるため)
__VALIDATE_MP4_DECODE_ERROR_RE='corrupt|Invalid NAL|Error splitting|Invalid data found|moov atom not found|Error while decoding'

# inner: 単一ファイルのフルデコード検証 (フィルタリングや表示はしない=outer の責務)
# $1: ファイルパス (呼び出し側が「検証対象の .mp4」と判断済みである前提)
# 戻り値: 0=正常 / 1=破損 (REPLY=理由)
# REPLY: NG 理由 (unreadable/no-video/no-audio/decode-error/truncated/trim-drops-audio)
#
# チェックは上から順に実行し、最初に引っかかった理由で早期確定する。
__validate_mp4_check() {
  emulate -L zsh
  local file="$1"
  local declared vcodec acodec acodec_names decode_log actual_hms truncated cn trim_risk

  # --- チェック1: コンテナが読めるか (ffprobe一発, 速い) ---
  declared=$(__ff_format_field "$file" format=duration || true)
  if [[ -z "$declared" || "$declared" == "N/A" ]]; then
    REPLY="unreadable"; return 1
  fi

  # --- チェック2: 映像ストリームの有無 (速い) ---
  vcodec=$(__ff_stream_field "$file" v:0 stream=codec_type || true)
  if [[ "$vcodec" != "video" ]]; then
    REPLY="no-video"; return 1
  fi

  # --- チェック3: 音声ストリームの有無 (速い) ---
  acodec=$(__ff_stream_field "$file" a:0 stream=codec_type || true)
  if [[ "$acodec" != "audio" ]]; then
    REPLY="no-audio"; return 1
  fi
  # 全音声ストリームの codec_name (チェック6 のトリム安全性判定で使用)。
  # 注1: codec_type と codec_name を1回の -show_entries にまとめると ffprobe が指定順で
  #      出力せず (codec_name が先に出る) パースが壊れるため、codec_name 単一フィールドで取る。
  # 注2: a:0 ではなく全音声ストリーム (-select_streams a) を見る。先頭が AAC でも 2 本目以降に
  #      mp3 等が混ざると、その音声トラックがトリムで失われるため (複数音声 MP4 対策)。
  #      複数行取得 (head しない) ため __ff_stream_field は使えない (あれは単一フィールド用)。
  acodec_names=$(ffprobe -v error -select_streams a -show_entries stream=codec_name -of default=nk=1:nw=1 -- "$file" 2>/dev/null || true)

  # --- チェック4+5: 全デコード1パスで「破損」と「実デコード終端」を両方取る (重い) ---
  # -stats で進捗の time= を強制出力させ、最後の time= を実デコード終端とみなす。
  #
  # ⚠️ ffmpeg の exit code は意図的に見ない (|| true で捨てる)。破損判定は exit code でなく
  # DECODE_ERROR_RE の特定文字列マッチで行う (理由は上の DECODE_ERROR_RE コメント参照)。
  # ffmpeg/ffprobe 不在の環境ではチェック1 の ffprobe が空を返し unreadable で確定するため
  # ここに到達しない。この前提が崩れない限り exit code 判定は追加しない。
  decode_log=$(ffmpeg -v warning -stats -i "$file" -f null - 2>&1 || true)

  # チェック4: デコードエラー
  if print -r -- "$decode_log" | grep -qE "$__VALIDATE_MP4_DECODE_ERROR_RE"; then
    REPLY="decode-error"; return 1
  fi

  # チェック5: 宣言duration vs 実デコード終端 (clean truncation検出。同じデコードパスから無料)
  # 「宣言 duration より実デコード終端が 10s 以上手前」= シークバーは長いのに途中で止まる状態のみ
  # NG とする (declared - actual > 10)。実デコード終端が宣言より長いケースは NG にしない
  # (絶対値化すると latent bug になるため directional のまま維持すること)。
  actual_hms=$(print -r -- "$decode_log" | grep -oE 'time=[0-9:.]+' | tail -n1 | cut -d= -f2 || true)
  if [[ -n "$actual_hms" ]]; then
    truncated=$(awk -F: -v d="$declared" '{ a=($1*3600)+($2*60)+$3; print (d-a>10)?1:0 }' <<< "$actual_hms")
    if [[ "$truncated" == "1" ]]; then
      REPLY="truncated"; return 1
    fi
  fi

  # --- チェック6: トリムで音声が消えるリスク (MP4音声が非AAC) ---
  # 再生・デコードは通る (壊れてはいない) が、QuickTime/Finder/AVFoundation のトリムは
  # pass-through (再エンコードなし) で動くため、MP4 コンテナの音声として実質 AAC/ALAC しか
  # コピーできない。mp3 等が mp4a タグで格納されていると、トリム時に「映像だけ残って音声が
  # 無言で消える」事故になる。トリム用途では事実上使えないため NG 扱い。
  # 音声ストリームのうち 1 本でも AAC/ALAC 以外があれば NG (そのトラックがトリムで失われる)。
  # codec_name が1本も取れない稀なケース ($acodec_names 空) は誤爆を避けて素通しする。
  trim_risk=0
  while IFS= read -r cn; do
    [[ -z "$cn" ]] && continue
    if [[ "$cn" != "aac" && "$cn" != "alac" ]]; then trim_risk=1; break; fi
  done <<< "$acodec_names"
  if [[ "$trim_risk" -eq 1 ]]; then
    REPLY="trim-drops-audio"; return 1
  fi

  REPLY=""; return 0
}

__validate_mp4_usage() {
  print -u2 -- "Usage: validate-mp4 [--mark] [--limit N] <file.mp4|glob> ..."
  print -u2 -- "       --mark     破損のみ <name>.broken(reason).mp4 にリネーム (正常はリネームしない)"
  print -u2 -- "       --limit N  処理数の上限"
  print -u2 -- ""
  print -u2 -- "  チェック (上から順に実行し、最初に引っかかった理由で早期確定):"
  print -u2 -- "    unreadable    ... コンテナが壊れている (moov欠損等, ffprobeで読めない)"
  print -u2 -- "    no-video      ... 映像ストリームなし"
  print -u2 -- "    no-audio      ... 音声ストリームなし"
  print -u2 -- "    decode-error  ... 全デコードで破損フレーム検出"
  print -u2 -- "    truncated     ... 宣言durationより実デコード終端が大きく手前で切れている"
  print -u2 -- "    trim-drops-audio ... 再生可だがMP4音声が非AAC(mp3等)。QuickTime/Finder等のトリムで音声が無言で消える"
}

# 既に判定済み (broken(...) / -enc 派生) はスキップ。
# correct は現在は付与しないが、過去に付けた旧ファイルを再検証しないよう残す。
__VALIDATE_MP4_SKIP_RE='\.(correct|broken)(\([^)]*\))?(-enc)?\.mp4$'

# <name>.<label>.mp4 / <name>.<label>-enc.mp4 を組み立てる (-enc の前に label を挿入)
__validate_mp4_mark_path() {
  local file="$1" label="$2"
  if [[ "$file" == *-enc.mp4 ]]; then
    print -r -- "${file%-enc.mp4}.${label}-enc.mp4"
  else
    print -r -- "${file%.mp4}.${label}.mp4"
  fi
}

# outer: CLI 層。検証対象のフィルタリング・進捗表示・--mark リネーム・集計を担う。
validate_mp4() {
  emulate -L zsh
  if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    __validate_mp4_usage; return 0
  fi

  local mark=0 limit=0
  local -a raw=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --mark) mark=1 ;;
      --limit) shift; limit="$1" ;;
      -*) print -u2 -- "エラー: 不明なオプション: $1"; return 1 ;;
      *) raw+=("$1") ;;
    esac
    shift
  done

  if (( ${#raw[@]} == 0 )); then
    __validate_mp4_usage; return 1
  fi

  # クォートされたグロブ ('path/*') を展開する
  local -a files=() expanded
  local pat
  for pat in "${raw[@]}"; do
    # ${~pat}(N): クォートされたグロブを展開 (N=nullglob, マッチ無しは空配列)。zsh 専用構文。
    # shellcheck disable=SC1036,SC2206
    expanded=( ${~pat}(N) )
    if (( ${#expanded[@]} > 0 )); then
      files+=("${expanded[@]}")
    else
      files+=("$pat")
    fi
  done

  # 色付け (stdout が TTY のときのみ。パイプ/リダイレクト時は制御文字を出さない)
  # CR/CLR: TTY では「処理中」行を結果行で上書きするためのキャリッジリターン+行クリア
  local C_OK C_NG C_SKIP C_RUN C_RST CR CLR
  if [[ -t 1 ]]; then
    C_OK=$'\e[32m'; C_NG=$'\e[31m'; C_SKIP=$'\e[2m'; C_RUN=$'\e[36m'; C_RST=$'\e[0m'
    CR=$'\r'; CLR=$'\e[K'
  else
    C_OK=''; C_NG=''; C_SKIP=''; C_RUN=''; C_RST=''
    CR=''; CLR=''
  fi

  typeset -F SECONDS   # 経過時間計測用に SECONDS を浮動小数化

  local exit_code=0 count=0
  local FILE start rc reason es new
  for FILE in "${files[@]}"; do
    (( limit > 0 && count >= limit )) && break

    # 検証対象の絞り込み (フルデコード前の安いフィルタ。inner は呼ばない)
    # .mp4 以外 (.part 等のDL途中ファイル含む) は対象外
    if [[ "$FILE" != *.mp4 || "$FILE" == *.part* ]]; then
      print -r -- "${C_SKIP}SKIP: ${FILE:t} (.mp4ではない)${C_RST}"
      continue
    fi
    if [[ "$FILE" =~ $__VALIDATE_MP4_SKIP_RE ]]; then
      print -r -- "${C_SKIP}SKIP: ${FILE:t} (判定済み)${C_RST}"
      continue
    fi

    count=$(( count + 1 ))
    start=$SECONDS

    # 処理中のファイル名を出す。TTY は改行なし (結果行が上書き)、非TTYは1行残す (ハング箇所特定用)。
    if [[ -t 1 ]]; then
      printf '%s%s%s… 処理中: %s%s' "$CR" "$CLR" "$C_RUN" "${FILE:t}" "$C_RST"
    else
      print -r -- "… 処理中: ${FILE:t}"
    fi

    __validate_mp4_check "$FILE"
    rc=$?
    reason="$REPLY"
    es=$(printf '(%.1fs)' "$(( SECONDS - start ))")

    if (( rc == 1 )); then
      exit_code=1
      if (( mark == 1 )); then
        new=$(__validate_mp4_mark_path "$FILE" "broken(${reason})")
        mv -- "$FILE" "$new"
        print -r -- "${CR}${CLR}${C_NG}NG -> ${new:t} ${es}${C_RST}"
      else
        print -r -- "${CR}${CLR}${C_NG}NG: $FILE [${reason}] ${es}${C_RST}"
      fi
    else
      # 正常ファイルはリネームしない (--mark でも)。破損だけマークすれば十分で、
      # 正常ファイル名に .correct を足すとダウンロード後の本来のファイル名が汚れるため。
      print -r -- "${CR}${CLR}${C_OK}OK: $FILE ${es}${C_RST}"
    fi
  done

  return $exit_code
}
