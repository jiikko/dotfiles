# shellcheck shell=bash
# shellcheck disable=SC2154,SC2076,SC2207,SC2296
# ------------------------------------------------------------------------------
# concat helpers — concat コマンドの内部補助関数群
# ------------------------------------------------------------------------------

# 許可される拡張子のリスト
__concat_allowed_extensions=(mp4 avi mov mkv webm flv wmv m4v mpg mpeg 3gp ts m2ts)

# 内部補助: 拡張子が許可されているか確認
__concat_is_allowed_ext() {
  local ext="${1:l}"  # 小文字に変換
  local allowed
  for allowed in "${__concat_allowed_extensions[@]}"; do
    [[ "$ext" == "$allowed" ]] && return 0
  done
  return 1
}

# 内部補助: ファイルから拡張子を取得
__concat_get_ext() {
  local file="$1"
  local base="${file:t}"
  if [[ "$base" == *.* ]]; then
    print -r -- "${base##*.}"
  else
    print -r -- ""
  fi
}

# 内部補助: ファイルからベースネーム（拡張子なし）を取得（NFC正規化済み）
__concat_get_stem() {
  local file="$1"
  local base="${file:t}"
  local stem
  if [[ "$base" == *.* ]]; then
    stem="${base%.*}"
  else
    stem="$base"
  fi
  # macOSのファイルシステムはNFDを使う場合があるためNFCに正規化
  # macOS: iconv -f UTF-8-MAC, Linux: uconv or perl
  local __nfc
  if __nfc=$(printf '%s' "$stem" | iconv -f UTF-8-MAC -t UTF-8 2>/dev/null); then
    printf '%s' "$__nfc"
  elif command -v uconv >/dev/null 2>&1; then
    printf '%s' "$stem" | uconv -x nfc 2>/dev/null || printf '%s' "$stem"
  elif command -v perl >/dev/null 2>&1; then
    printf '%s' "$stem" | perl -CSA -MUnicode::Normalize -ne 'print NFC($_)' 2>/dev/null || printf '%s' "$stem"
  else
    printf '%s' "$stem"
  fi
}

# 内部補助: 複数の文字列から共通サフィックスを見つける
# $1...: 文字列の配列
# 戻り値: REPLY に共通サフィックスを設定
__concat_find_common_suffix() {
  local -a strings=("$@")
  (( ${#strings[@]} == 0 )) && { REPLY=""; return 0; }

  local first="${strings[1]}"
  local suffix=""
  local i len char all_match

  # 末尾から1文字ずつ比較
  len=${#first}
  for (( i = 0; i < len; i++ )); do
    char="${first:(-1-i):1}"
    all_match=1
    for s in "${strings[@]:1}"; do
      if (( ${#s} <= i )) || [[ "${s:(-1-i):1}" != "$char" ]]; then
        all_match=0
        break
      fi
    done
    if (( all_match )); then
      suffix="${char}${suffix}"
    else
      break
    fi
  done

  REPLY="$suffix"
}

# 内部補助: 連番パターンを検出して番号、サフィックス、プレフィックスを抽出
# 戻り値: REPLY に "番号:サフィックス:プレフィックス" を設定
__concat_extract_number() {
  local stem="$1"
  local num="" suffix="" prefix=""

  # パターン: _NNN または -NNN (末尾)
  if [[ "$stem" =~ ^(.*)_([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  elif [[ "$stem" =~ ^(.*)-([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: (N)
  elif [[ "$stem" =~ '^(.*)\(([0-9]+)\)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: partN
  elif [[ "$stem" =~ ^(.*)part([0-9]+)$ ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: 第N話 (Japanese episode numbering, e.g., 第1話, 第2話)
  elif [[ "$stem" =~ '^(.*)第([0-9]+)話$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: 英字ワード+数字 (e.g., _Scene1, _Part2, _Vol3)
  elif [[ "$stem" =~ '^(.*[-_][a-zA-Z]+)([0-9]+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  # パターン: -N-<suffix> または _N_<suffix>（サフィックスに数字を含む場合も対応）
  elif [[ "$stem" =~ '^(.*)[-_]([0-9]+)([-_].+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix="${match[3]}"
  # パターン: 末尾が数字（区切り文字なし、例: clip28）
  # 共通サフィックス除去後のフォールバックとして使用
  elif [[ "$stem" =~ '^(.*[^0-9])([0-9]+)$' ]]; then
    prefix="${match[1]}"
    num="${match[2]}"
    suffix=""
  fi

  REPLY="${num}:${suffix}:${prefix}"
  [[ -n "$num" ]]
}

# 内部補助: 連番の連続性を検証
__concat_validate_sequence() {
  local -a numbers=("$@")
  local -a sorted_nums
  local min max

  # 数値としてソート
  sorted_nums=($(printf '%s\n' "${numbers[@]}" | sort -n))

  min="${sorted_nums[1]}"
  max="${sorted_nums[-1]}"

  # 欠番チェック
  local -a missing=()
  for (( i = min; i <= max; i++ )); do
    local found=0
    for n in "${sorted_nums[@]}"; do
      if (( n == i )); then
        found=1
        break
      fi
    done
    if (( ! found )); then
      missing+=("$(printf '%03d' $i)")
    fi
  done

  if (( ${#missing[@]} > 0 )); then
    local missing_str="${(j:, :)missing}"
    REPLY="連番に欠番があります: $missing_str が見つかりません"
    return 1
  fi

  REPLY=""
  return 0
}

# 内部補助: ffprobeで映像情報を取得
__concat_get_video_info() {
  local file="$1"
  ffprobe -v error -select_streams v:0 \
    -show_entries stream=codec_name,width,height,r_frame_rate,pix_fmt \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: ffprobeで映像のtime_baseを取得
__concat_get_video_time_base() {
  local file="$1"
  ffprobe -v error -select_streams v:0 \
    -show_entries stream=time_base \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: ffprobeで音声情報を取得
__concat_get_audio_info() {
  local file="$1"
  ffprobe -v error -select_streams a:0 \
    -show_entries stream=codec_name,sample_rate,channels \
    -of csv=p=0 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: ffprobeでdurationを取得
__concat_get_duration() {
  local file="$1"
  ffprobe -v error -show_entries format=duration \
    -of default=nk=1:nw=1 -- "$file" 2>/dev/null | head -n1
}

# 内部補助: パスをエスケープしてconcat用に整形
__concat_escape_path() {
  local path="$1"
  # FFmpeg concat demuxerのエスケープ:
  # 1. バックスラッシュを先にエスケープ（順序重要）
  # 2. シングルクォートで囲み、' は '\'' でエスケープ
  path="${path//\\/\\\\}"
  path="${path//\'/\'\\\'\'}"
  print -r -- "file '${path}'"
}

# 内部補助: MP4ファイルの実効サイズを返す（有効なトップレベルボックスの合計）
# 孤立データ（moovから参照されないmdat以降のゴミ）を除外したサイズを計算
# $1: ファイルパス
# 標準出力: 実効サイズ（バイト）
__concat_mp4_effective_size() {
  python3 -c "
import struct, sys
try:
    with open(sys.argv[1], 'rb') as f:
        f.seek(0, 2)
        fsize = f.tell()
        f.seek(0)
        total = 0
        while f.tell() < fsize:
            pos = f.tell()
            hdr = f.read(8)
            if len(hdr) < 8:
                break
            raw_size, = struct.unpack('>I', hdr[:4])
            btype = hdr[4:8]
            if raw_size == 1:
                ext = f.read(8)
                if len(ext) < 8:
                    break
                raw_size, = struct.unpack('>Q', ext)
            elif raw_size == 0:
                raw_size = fsize - pos
            if not all(0x20 <= b < 0x7f for b in btype):
                break
            if raw_size < 8 or pos + raw_size > fsize:
                break
            total += raw_size
            f.seek(pos + raw_size)
        print(total)
except Exception:
    print(0)
" "$1" 2>/dev/null || echo "0"
}

# 内部補助: 出力ファイルの診断
# $1: 出力ファイルパス
# $2: 期待されるduration
# $3: 入力に音声があったかどうか (1=あり, 0=なし)
# $4: 入力ファイル合計サイズ (bytes, optional)
# $5...: 入力ファイルパス（サイズ乖離時の原因特定用、省略可）
__concat_diagnose_output() {
  local outfile="$1"
  local expected_duration="$2"
  local has_input_audio="${3:-1}"
  local expected_size="${4:-0}"
  shift 4
  local -a input_files=("$@")

  # 1. メタデータ取得
  local info
  info=$(ffprobe -v error -show_entries format=duration,bit_rate:stream=codec_type,codec_name \
    -of json -- "$outfile" 2>/dev/null)

  if [[ -z "$info" ]]; then
    REPLY="メタデータの取得に失敗しました"
    return 1
  fi

  # 映像ストリームの存在確認（スペースの有無に対応）
  if ! print -r -- "$info" | grep -q '"codec_type"[[:space:]]*:[[:space:]]*"video"'; then
    REPLY="映像ストリームが存在しません"
    return 1
  fi

  # 音声ストリームの存在確認（入力にあれば）
  if (( has_input_audio )); then
    if ! print -r -- "$info" | grep -q '"codec_type"[[:space:]]*:[[:space:]]*"audio"'; then
      REPLY="音声ストリームが存在しません（入力には音声がありました）"
      return 1
    fi
  fi

  # duration > 0 のチェック
  local actual_duration
  actual_duration=$(__concat_get_duration "$outfile")
  if [[ -z "$actual_duration" ]] || (( $(echo "$actual_duration <= 0" | bc -l) )); then
    REPLY="durationが0以下または取得できません"
    return 1
  fi

  # duration乖離チェック（入力合計と±N%以上乖離していれば異常、10秒未満はスキップ）
  local _dur_tol="${CONCAT_DURATION_TOLERANCE:-5}"
  local _dur_lo _dur_hi
  _dur_lo=$(awk -v t="$_dur_tol" 'BEGIN{ printf "%.4f", 1 - t/100 }')
  _dur_hi=$(awk -v t="$_dur_tol" 'BEGIN{ printf "%.4f", 1 + t/100 }')
  if [[ -n "$expected_duration" ]] && (( $(echo "$expected_duration > 10" | bc -l) )); then
    local dur_ratio
    dur_ratio=$(awk -v a="$actual_duration" -v e="$expected_duration" 'BEGIN{ if(e==0){print "1.0000";exit} printf "%.4f", a/e }')
    if (( $(echo "$dur_ratio < $_dur_lo" | bc -l) )); then
      local dur_pct expected_hms actual_hms
      dur_pct=$(awk -v r="$dur_ratio" 'BEGIN{ printf "%.1f", (1-r)*100 }')
      expected_hms=$(awk -v s="$expected_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      actual_hms=$(awk -v s="$actual_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      REPLY="出力durationが入力合計より${dur_pct}%短い (入力: ${expected_hms}, 出力: ${actual_hms})"
      return 1
    fi
    if (( $(echo "$dur_ratio > $_dur_hi" | bc -l) )); then
      local dur_pct expected_hms actual_hms
      dur_pct=$(awk -v r="$dur_ratio" 'BEGIN{ printf "%.1f", (r-1)*100 }')
      expected_hms=$(awk -v s="$expected_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      actual_hms=$(awk -v s="$actual_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      REPLY="出力durationが入力合計より${dur_pct}%長い (入力: ${expected_hms}, 出力: ${actual_hms})"
      return 1
    fi
  fi

  # サイズ乖離チェック（入力合計と±5%以上乖離していれば異常、1MB未満はスキップ）
  if (( expected_size > 1048576 )); then
    local actual_size
    actual_size=$(stat -f%z -- "$outfile" 2>/dev/null || stat -c%s -- "$outfile" 2>/dev/null)
    if [[ -n "$actual_size" ]] && (( actual_size > 0 )); then
      local ratio
      ratio=$(awk -v a="$actual_size" -v e="$expected_size" 'BEGIN{ printf "%.4f", a/e }')
      if (( $(echo "$ratio < 0.95" | bc -l) )); then
        local pct
        pct=$(awk -v r="$ratio" 'BEGIN{ printf "%.1f", (1-r)*100 }')
        local _msg="出力サイズが入力合計より${pct}%小さい (入力: $((expected_size/1024/1024))MB, 出力: $((actual_size/1024/1024))MB)"

        # 入力ファイルの実効サイズを調べて原因候補を特定
        if (( ${#input_files[@]} > 0 )); then
          local _eff _stat _orphaned _orphaned_mb _suspect=""
          local _effective_total=0
          for _infile in "${input_files[@]}"; do
            _eff=$(__concat_mp4_effective_size "$_infile")
            _stat=$(stat -f%z -- "$_infile" 2>/dev/null || stat -c%s -- "$_infile" 2>/dev/null)
            (( _effective_total += _eff ))
            if [[ -n "$_eff" ]] && [[ -n "$_stat" ]] && (( _stat > 0 && _eff > 0 )); then
              _orphaned=$((_stat - _eff))
              if (( _orphaned > 10485760 )); then  # > 10MB
                _orphaned_mb=$((_orphaned / 1024 / 1024))
                _suspect="${_suspect}"$'\n'"  ⚠️  ${_infile:t}: 未参照データ ${_orphaned_mb}MB (実効: $((_eff/1024/1024))MB / ファイル: $((_stat/1024/1024))MB)"
              fi
            fi
          done
          if [[ -n "$_suspect" ]]; then
            # 実効サイズ合計で再判定: 出力が実効合計の95%以上なら正常
            local _eff_ratio
            _eff_ratio=$(awk -v a="$actual_size" -v e="$_effective_total" 'BEGIN{ printf "%.4f", (e>0) ? a/e : 0 }')
            if (( $(echo "$_eff_ratio >= 0.95 && $_eff_ratio <= 1.05" | bc -l) )); then
              REPLY="${_msg}${_suspect}"$'\n'"  → 再生時間・映像・音声に影響はありません"
              return 2  # 警告（出力は正常だが入力にゴミデータあり）
            fi
            _msg="${_msg}${_suspect}"
          fi
        fi

        REPLY="$_msg"
        return 1
      fi
      if (( $(echo "$ratio > 1.05" | bc -l) )); then
        local pct
        pct=$(awk -v r="$ratio" 'BEGIN{ printf "%.1f", (r-1)*100 }')
        REPLY="出力サイズが入力合計より${pct}%大きい (入力: $((expected_size/1024/1024))MB, 出力: $((actual_size/1024/1024))MB)"
        return 1
      fi
    fi
  fi

  REPLY=""
  return 0
}

# 内部補助: 指定タイムスタンプのフレームをハッシュ化
# ffmpegが失敗した場合は空文字を返す（shasumの空入力ハッシュによる偽一致を防止）
#
# シーク精度のために目的 PTS から 1ms 引いてから -ss する（_EPS=0.001）。
# 理由: -ss T は「PTS >= T の最初のフレーム」を返すが、T が目的フレームの
# PTS と厳密に一致する場合、浮動小数点丸め（container timescale と
# 引数表記の食い違い、例: 4312.331966 vs 4312.332）により次フレームに
# 超過してしまうことがある。1ms 手前にオフセットすれば必ず目的フレームに
# 着地する（通常のフレーム間隔 16〜42ms より十分小さい）。
__concat_frame_hash() {
  local file="$1" timestamp="$2"
  local _EPS=0.001
  local _target _approx _fine
  _target=$(awk -v t="$timestamp" -v e="$_EPS" 'BEGIN{ a=t-e; if(a<0) a=0; printf "%.3f", a }')
  # 2段階シーク: input seeking (高速・近傍keyframeへ) + output seeking (正確なフレーム位置)
  # input seekingだけではconcat出力ファイルの深い位置で誤ったフレームに到達する場合がある
  _approx=$(awk -v t="$_target" 'BEGIN{ a=t-5; if(a<0) a=0; printf "%.3f", a }')
  _fine=$(awk -v t="$_target" -v a="$_approx" 'BEGIN{ printf "%.3f", t-a }')
  local _raw
  _raw=$(ffmpeg -hide_banner -nostdin -loglevel error \
    -ss "$_approx" -i "$file" -ss "$_fine" \
    -vframes 1 -f rawvideo -pix_fmt rgb24 pipe:1 2>/dev/null)
  if [[ -z "$_raw" ]]; then
    print -r -- ""
    return 1
  fi
  print -r -- "$_raw" | shasum | cut -d' ' -f1
}

# 内部補助: 結合後のフレーム順序を検証
# $1: 出力ファイル
# $2...: 入力ファイル（未ソート — 本関数が独自にソートする）
# 目的: メインのソートロジックと独立した順序決定により、誤った結合順序を検出する
__concat_verify_frame_order() {
  local outfile="$1"
  shift
  local -a raw_files=("$@")

  # --- 独自ソートロジック ---
  # ファイル名から連番部分を抽出し、数値昇順でソートする
  # メインのconcat関数とは独立した実装にすることで、ソートバグの検出が可能
  local -a sorted_files=()
  local -A num_map  # ファイルパス → 抽出された連番
  local f basename num

  for f in "${raw_files[@]}"; do
    basename="${f:t:r}"  # 拡張子なしのファイル名
    # 末尾の連番を抽出（_NNN, -NNN, (N), partN パターン）
    num=""
    if [[ "$basename" =~ '_([0-9]+)$' ]]; then
      num="${match[1]}"
    elif [[ "$basename" =~ '-([0-9]+)(-[^0-9].*)?$' ]]; then
      num="${match[1]}"
    elif [[ "$basename" =~ '\(([0-9]+)\)$' ]]; then
      num="${match[1]}"
    elif [[ "$basename" =~ 'part([0-9]+)' ]]; then
      num="${match[1]}"
    elif [[ "$basename" =~ '([0-9]+)$' ]]; then
      # フォールバック: 末尾の数値
      num="${match[1]}"
    fi
    if [[ -z "$num" ]]; then
      # 連番を抽出できない場合は検証をスキップ（メインと同一ソートでは検証の意味がない）
      REPLY=""
      return 0
    fi
    # 先頭ゼロを除去して数値化
    num_map[$f]=$((10#$num))
  done

  if (( ${#num_map} > 0 )); then
    # 連番の数値昇順でソート
    sorted_files=()
    local -a pairs=()
    for f in "${raw_files[@]}"; do
      pairs+=("${num_map[$f]}"$'\t'"${f}")
    done
    local line
    for line in "${(f)$(printf '%s\n' "${pairs[@]}" | sort -t$'\t' -k1,1n)}"; do
      sorted_files+=("${line#*$'\t'}")
    done
  fi

  # --- フレームハッシュ比較 ---
  # cumulative は format=duration で積む（concat demuxer が各セグメントを
  # その値ぶんシフトするため、次セグメントの映像フレームは output 側で
  # PTS = cumulative + file内PTS の位置に並ぶ）
  local cumulative=0
  local file dur sample_t output_t
  local input_hash output_hash

  for file in "${sorted_files[@]}"; do
    dur=$(__concat_get_duration "$file")
    if [[ -z "$dur" ]]; then
      REPLY="duration取得失敗: ${file:t}"
      return 1
    fi
    sample_t=$(awk -v d="$dur" 'BEGIN{
      t=d*0.3; if(t<0.5) t=0.5; if(t>10) t=10; if(t>d*0.8) t=d*0.8
      printf "%.3f", t
    }')
    input_hash=$(__concat_frame_hash "$file" "$sample_t")
    output_t=$(awk -v c="$cumulative" -v s="$sample_t" 'BEGIN{ printf "%.3f", c+s }')
    if [[ -z "$input_hash" ]]; then
      # フレーム抽出失敗は結合エラーではなく検証の限界 — スキップして続行
      print -r -- "⚠️  フレーム抽出スキップ: ${file:t} (結合順序が正しいか手動で確認してください)" >&2
      cumulative=$(awk -v c="$cumulative" -v d="$dur" 'BEGIN{ printf "%.3f", c+d }')
      continue
    fi
    # concat demuxer がストリーム start_time を数十ms シフトすることがあるため、
    # ±50ms の窓内で一致するフレームを探す(~1-3 フレーム相当の許容)
    local matched=0
    local probe_skipped=1
    local probe_t=""
    local probe_hash=""
    local _offset=""
    for _offset in 0 0.033 -0.033 0.050 -0.050; do
      probe_t=$(awk -v t="$output_t" -v o="$_offset" 'BEGIN{ v=t+o; if(v<0) v=0; printf "%.3f", v }')
      probe_hash=$(__concat_frame_hash "$outfile" "$probe_t")
      if [[ -n "$probe_hash" ]]; then
        probe_skipped=0
        if [[ "$probe_hash" == "$input_hash" ]]; then
          matched=1
          break
        fi
      fi
    done
    if (( probe_skipped )); then
      print -r -- "⚠️  フレーム抽出スキップ: ${file:t} (結合順序が正しいか手動で確認してください)" >&2
      cumulative=$(awk -v c="$cumulative" -v d="$dur" 'BEGIN{ printf "%.3f", c+d }')
      continue
    fi
    if (( ! matched )); then
      REPLY="フレーム不一致: ${file:t} (入力 ${sample_t}s の周辺 ±50ms に一致フレームなし)"
      return 1
    fi
    cumulative=$(awk -v c="$cumulative" -v d="$dur" 'BEGIN{ printf "%.3f", c+d }')
  done
  REPLY=""
  return 0
}
