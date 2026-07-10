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
  # パターン: 英字ワード+数字 (e.g., _Scene1, _Part2, _Vol3, _#Ep1, _#Sp2)
  # `#?` はワードの直前にハッシュ記号が入る表記 ("_#Ep1" 等) の許容。
  # なくても part1 等は通常通り通る (互換)。prefix にワード部 (#Ep / Scene
  # 等) ごと取り込むので、同一 stem で異なるワード ("_#Ep1" と "_#Sp1") は
  # 別グループになる。
  elif [[ "$stem" =~ '^(.*[-_]#?[a-zA-Z]+)([0-9]+)$' ]]; then
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

  # 欠番チェック: sorted_nums は昇順。かつては [min,max] の全整数を走査し欠番ごとに
  # subshell を fork していたため、連番が離れている (例 _1 と _1000000) と O(範囲) の
  # ループ + 大量 fork で事実上ハングした。隣接要素の間隔だけを見て O(件数) で検出し、
  # 列挙は cap 件で打ち切って (メッセージ肥大 + subshell 暴走の抑止) 末尾に … を付す。
  local -a missing=()
  local prev="" cur j
  local -i cap=50 truncated=0
  for cur in "${sorted_nums[@]}"; do
    if [[ -n "$prev" ]] && (( cur > prev + 1 )); then
      for (( j = prev + 1; j < cur; j++ )); do
        if (( ${#missing[@]} >= cap )); then truncated=1; break; fi
        missing+=("$(printf '%03d' $j)")
      done
    fi
    prev="$cur"
    (( truncated )) && break
  done

  if (( ${#missing[@]} > 0 )); then
    local missing_str="${(j:, :)missing}"
    (( truncated )) && missing_str+=", …"
    REPLY="連番に欠番があります: $missing_str が見つかりません"
    return 1
  fi

  REPLY=""
  return 0
}

# --- グルーピング (ディレクトリモード / マルチグループモード共通) ---------------
# 以前は _concat.zsh のディレクトリモードとマルチグループモードがほぼ同一の
# グルーピング+実行ループを別実装で持っており、片方だけ修正されるドリフトが
# 実際に発生していた (連想配列アクセスのガード有無が食い違っていた)。
# ここに単一実装として集約する。

# __concat_group_files の結果格納先 (zsh は連想配列を戻り値にできないためグローバル)
typeset -gA __CONCAT_GROUP_KEY_OF   # 絶対パス → グループキー (prefix::suffix)
typeset -ga __CONCAT_GROUP_KEYS     # 検出順のユニークキー一覧
typeset -gi __CONCAT_GROUP_VIABLE   # 2 ファイル以上のグループ数

# 内部補助: ファイル群を連番パターンでグルーピングする
# 引数: ファイルパス (位置引数)
# 出力: 上記グローバル 3 つ。連番パターンに一致しないファイルは stderr に警告してスキップ
__concat_group_files() {
  __CONCAT_GROUP_KEY_OF=()
  __CONCAT_GROUP_KEYS=()
  __CONCAT_GROUP_VIABLE=0
  local f stem rest suffix prefix key
  local -a skipped=() all_keys=()
  for f in "$@"; do
    stem=$(__concat_get_stem "$f")
    if __concat_extract_number "$stem"; then
      rest="${REPLY#*:}"
      suffix="${rest%%:*}"
      prefix="${rest#*:}"
      key="${prefix}::${suffix}"
      __CONCAT_GROUP_KEY_OF[${f:A}]="$key"
      all_keys+=("$key")
    else
      skipped+=("${f:t}")
    fi
  done
  if (( ${#skipped[@]} > 0 )); then
    print -r -- "⚠️  連番パターンに一致しないファイルをスキップしました: ${(j:, :)skipped}" >&2
  fi
  __CONCAT_GROUP_KEYS=("${(u)all_keys[@]}")
  local k count
  for k in "${__CONCAT_GROUP_KEYS[@]}"; do
    count=0
    for f in "$@"; do
      [[ "${__CONCAT_GROUP_KEY_OF[${f:A}]-}" == "$k" ]] && count=$((count + 1))
    done
    (( count >= 2 )) && __CONCAT_GROUP_VIABLE=$((__CONCAT_GROUP_VIABLE + 1))
  done
}

# 内部補助: グルーピング済みファイル群をグループごとに concat へ流し、サマリを出す
# 前提: 直前に __concat_group_files を呼んでグローバルが populate 済みであること
#       (ここで再グルーピングすると skip 警告が二重に出るため、敢えて分離している)
# 引数: $1 = オプション個数 N, $2..$(N+1) = concat へ渡すオプション, 残り = ファイルパス
# 戻り値: 0=全グループ成功 (結合可能グループなしを含む), 1=失敗グループあり
__concat_run_groups() {
  local nopts="$1"; shift
  local -a opts=() files=()
  (( nopts > 0 )) && opts=( "${@[1,$nopts]}" )
  shift "$nopts"
  files=( "$@" )

  # グループ構成をループ開始前にローカルへ確定させる。
  # ループ内の再帰 concat 呼び出しは (3 ファイル以上のグループで) マルチグループ
  # 検出パスに入って __concat_group_files を再実行し、グローバルを上書きするため、
  # グローバルを直接参照し続けると後続グループが silent にスキップされる (codex P1)。
  local -a keys=( "${__CONCAT_GROUP_KEYS[@]}" )
  local -A key_of=( "${(kv)__CONCAT_GROUP_KEY_OF[@]}" )

  local _key _f _total=0 _ok=0 _fail=0
  local -a group_files sorted_group
  for _key in "${keys[@]}"; do
    group_files=()
    for _f in "${files[@]}"; do
      [[ "${key_of[${_f:A}]-}" == "$_key" ]] && group_files+=("$_f")
    done
    (( ${#group_files[@]} < 2 )) && continue

    _total=$((_total + 1))
    sorted_group=("${(on)group_files[@]}")

    print -r -- ""
    print -r -- "=========================================="
    print -r -- "グループ ${_total}: ${sorted_group[1]:t} 他${#sorted_group[@]}ファイル"
    print -r -- "=========================================="

    # 元ファイルの削除は単一グループ側 (concat 本体) が行う
    if concat "${opts[@]}" "${sorted_group[@]}"; then
      _ok=$((_ok + 1))
    else
      _fail=$((_fail + 1))
    fi
  done

  if (( _total == 0 )); then
    print -r -- "結合可能なグループが見つかりませんでした"
    return 0
  fi
  # 旧実装の ${_fail:+...} は "0" も非空のため全成功時にも ", 0失敗" が出ていた。
  # 失敗があるときだけ表示する。
  local fail_note=""
  (( _fail > 0 )) && fail_note=", ${_fail}失敗"
  print -r -- ""
  print -r -- "=========================================="
  print -r -- "完了: ${_ok}/${_total} グループ成功${fail_note}"
  print -r -- "=========================================="
  return $(( _fail > 0 ))
}

# 内部補助: 浮動小数点比較 (旧実装の bc 依存を、他の計算と同じ awk に統一)
# 引数: $1=左辺, $2=演算子 (lt/le/gt/ge), $3=右辺
# 戻り値: 0=真, 1=偽
# 注: 非数値は awk の数値強制で 0 として扱われる (bc はパースエラーで空を返し、
#     (( 空 )) が偽になって判定がすり抜けていた。awk 版は N/A 等も比較対象になる)
__concat_float() {
  awk -v a="$1" -v b="$3" -v op="$2" 'BEGIN {
    a += 0; b += 0
    if (op == "lt") exit !(a < b)
    if (op == "le") exit !(a <= b)
    if (op == "gt") exit !(a > b)
    if (op == "ge") exit !(a >= b)
    exit 1
  }'
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
  if [[ -z "$actual_duration" ]] || __concat_float "$actual_duration" le 0; then
    REPLY="durationが0以下または取得できません"
    return 1
  fi

  # duration乖離チェック（入力合計と±N%以上乖離していれば異常、10秒未満はスキップ）
  local _dur_tol="${CONCAT_DURATION_TOLERANCE:-5}"
  local _dur_lo _dur_hi
  _dur_lo=$(awk -v t="$_dur_tol" 'BEGIN{ printf "%.4f", 1 - t/100 }')
  _dur_hi=$(awk -v t="$_dur_tol" 'BEGIN{ printf "%.4f", 1 + t/100 }')
  if [[ -n "$expected_duration" ]] && __concat_float "$expected_duration" gt 10; then
    local dur_ratio
    dur_ratio=$(awk -v a="$actual_duration" -v e="$expected_duration" 'BEGIN{ if(e==0){print "1.0000";exit} printf "%.4f", a/e }')
    if __concat_float "$dur_ratio" lt "$_dur_lo"; then
      local dur_pct expected_hms actual_hms
      dur_pct=$(awk -v r="$dur_ratio" 'BEGIN{ printf "%.1f", (1-r)*100 }')
      expected_hms=$(awk -v s="$expected_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      actual_hms=$(awk -v s="$actual_duration" 'BEGIN{ h=int(s/3600); m=int((s%3600)/60); sec=s%60; printf "%d:%02d:%05.2f", h, m, sec }')
      REPLY="出力durationが入力合計より${dur_pct}%短い (入力: ${expected_hms}, 出力: ${actual_hms})"
      return 1
    fi
    if __concat_float "$dur_ratio" gt "$_dur_hi"; then
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
      if __concat_float "$ratio" lt 0.95; then
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
            if __concat_float "$_eff_ratio" ge 0.95 && __concat_float "$_eff_ratio" le 1.05; then
              REPLY="${_msg}${_suspect}"$'\n'"  → 再生時間・映像・音声に影響はありません"
              return 2  # 警告（出力は正常だが入力にゴミデータあり）
            fi
            _msg="${_msg}${_suspect}"
          fi
        fi

        REPLY="$_msg"
        return 1
      fi
      if __concat_float "$ratio" gt 1.05; then
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
  # raw フレーム (1080p rgb24 で ~6MB、NUL バイト含む) をシェル変数に取り込まず
  # shasum へ直接パイプする。
  # 「ffmpeg 失敗 (出力なし)」「成功だがフレーム 0 件」(seek 範囲外等) はどちらも
  # 空入力の既知 SHA-1 になるため、それを失敗扱いにして両側空ハッシュの偽一致を防ぐ
  # (旧実装が raw 変数の空チェックで防いでいたものと同等)。
  # 注: pipestatus はコマンド置換内のパイプラインには効かないため ffmpeg の
  # exit status は直接見ない (旧実装も見ていない)。
  local _hash
  _hash=$(ffmpeg -hide_banner -nostdin -loglevel error \
    -ss "$_approx" -i "$file" -ss "$_fine" \
    -vframes 1 -f rawvideo -pix_fmt rgb24 pipe:1 2>/dev/null | shasum | cut -d' ' -f1)
  local _EMPTY_SHA1="da39a3ee5e6b4b0d3255bfef95601890afd80709"
  if [[ -z "$_hash" || "$_hash" == "$_EMPTY_SHA1" ]]; then
    print -r -- ""
    return 1
  fi
  print -r -- "$_hash"
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

# 内部補助: ファイルをゴミ箱に移動する（macOS の /usr/bin/trash / osascript / Linux の gio trash の順で試す）
# 絶対パスに正規化して呼び出すため、ファイル名が "-" で始まっても安全
__concat_trash() {
  local file="$1"
  local _abs="${file:A}"
  if command -v trash >/dev/null 2>&1; then
    # /usr/bin/trash は "--" をファイル名として誤解するため付けない（絶対パス渡しで保護）
    trash "$_abs" >/dev/null 2>&1
    return $?
  fi
  if [[ "$OSTYPE" == darwin* ]]; then
    local _escaped="${_abs//\\/\\\\}"
    _escaped="${_escaped//\"/\\\"}"
    osascript -e "tell application \"Finder\" to delete POSIX file \"$_escaped\"" >/dev/null 2>&1
    return $?
  fi
  if command -v gio >/dev/null 2>&1; then
    gio trash -- "$_abs" >/dev/null 2>&1
    return $?
  fi
  print -r -- "❌ ゴミ箱への移動方法が見つかりません: $_abs" >&2
  return 1
}

# --- 連番解決 (stems → 連番リスト + 命名部品) -----------------------------------
# concat() 本体にあった 3 段リトライの状態機械を関数契約として切り出したもの:
#   1. 通常 stem で連番抽出 (prefix/suffix 一致を要求)
#   2. suffix 不一致なら「末尾数字」パターンで再試行
#   3. 連番未検出 / prefix 不一致なら共通サフィックスを除去して再試行
# 引数: stems... (拡張子なしベースネーム、NFC 正規化済み)
# 戻り値: 0=解決成功 (結果は下のグローバル)、1=失敗 (エラーメッセージは出力済み)
# zsh は配列を戻り値にできないため結果はグローバルに格納する (__CONCAT_GROUP_* と同じ規約)
typeset -ga __CONCAT_R_NUMBERS        # 解決した連番 (10 進整数, 入力順)
typeset -g  __CONCAT_R_COMMON_PREFIX  # 共通プレフィックス
typeset -g  __CONCAT_R_FIRST_SUFFIX   # 共通サフィックス (番号より後ろ, 無ければ空)
typeset -gi __CONCAT_R_USE_STRIPPED   # 1=共通サフィックス除去で解決した
typeset -g  __CONCAT_R_COMMON_SUFFIX  # 除去した共通サフィックス (USE_STRIPPED=1 のとき)
__concat_resolve_sequence() {
  local -a stems=("$@")
  __CONCAT_R_NUMBERS=()
  __CONCAT_R_COMMON_PREFIX=""
  __CONCAT_R_FIRST_SUFFIX=""
  __CONCAT_R_USE_STRIPPED=0
  __CONCAT_R_COMMON_SUFFIX=""

  local -a numbers=()
  local first_suffix="" first_prefix=""
  local use_stripped_stems=0
  local detected_common_suffix=""
  local stem

  # 最初のパス: 通常のstemで連番を検出
  local -a temp_numbers=()
  local -a temp_prefixes=()
  local -a temp_suffixes=()
  local all_matched=1
  local num_part rest suffix_part prefix_part
  for stem in "${stems[@]}"; do
    if __concat_extract_number "$stem"; then
      num_part="${REPLY%%:*}"
      rest="${REPLY#*:}"
      suffix_part="${rest%%:*}"
      prefix_part="${rest#*:}"
      temp_numbers+=("$((10#$num_part))")
      temp_prefixes+=("$prefix_part")
      temp_suffixes+=("$suffix_part")
    else
      all_matched=0
      break
    fi
  done

  # プレフィックスとサフィックスが一致するかチェック
  local p s
  if (( all_matched )); then
    local prefixes_match=1
    local suffixes_match=1
    for p in "${temp_prefixes[@]:1}"; do
      if [[ "$p" != "${temp_prefixes[1]}" ]]; then
        prefixes_match=0
        break
      fi
    done
    for s in "${temp_suffixes[@]:1}"; do
      if [[ "$s" != "${temp_suffixes[1]}" ]]; then
        suffixes_match=0
        break
      fi
    done

    if (( prefixes_match && suffixes_match )); then
      # 通常のstemで成功
      numbers=("${temp_numbers[@]}")
      first_prefix="${temp_prefixes[1]}"
      first_suffix="${temp_suffixes[1]}"
    elif (( ! suffixes_match )); then
      # サフィックスが異なる → 末尾数字パターンで再試行
      local -a retry_numbers=()
      local -a retry_prefixes=()
      local retry_all_matched=1
      for stem in "${stems[@]}"; do
        if [[ "$stem" =~ '^(.*[^0-9])([0-9]+)$' ]]; then
          retry_prefixes+=("${match[1]}")
          retry_numbers+=("$((10#${match[2]}))")
        else
          retry_all_matched=0
          break
        fi
      done

      if (( retry_all_matched )); then
        local retry_prefixes_match=1
        for p in "${retry_prefixes[@]:1}"; do
          if [[ "$p" != "${retry_prefixes[1]}" ]]; then
            retry_prefixes_match=0
            break
          fi
        done
        if (( retry_prefixes_match )); then
          numbers=("${retry_numbers[@]}")
          first_prefix="${retry_prefixes[1]}"
          first_suffix=""
        else
          print -r -- "エラー: サフィックスが異なります: '${temp_suffixes[1]}' と 異なるサフィックスがあります" >&2
          return 1
        fi
      else
        print -r -- "エラー: サフィックスが異なります: '${temp_suffixes[1]}' と 異なるサフィックスがあります" >&2
        return 1
      fi
    else
      # プレフィックスが一致しない: 共通サフィックスを除去して再試行
      use_stripped_stems=1
    fi
  else
    # 連番パターンが見つからない: 共通サフィックスを除去して再試行
    use_stripped_stems=1
  fi

  # 共通サフィックス除去が必要な場合
  if (( use_stripped_stems )); then
    __concat_find_common_suffix "${stems[@]}"
    detected_common_suffix="$REPLY"

    if [[ -z "$detected_common_suffix" ]]; then
      # 共通サフィックスがない場合は元のエラーを出力
      for stem in "${stems[@]}"; do
        if ! __concat_extract_number "$stem"; then
          print -r -- "エラー: ファイル名に連番パターンがありません: $stem" >&2
          return 1
        fi
      done
      print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスがありません" >&2
      return 1
    fi

    # 共通サフィックスを除去したstemsを作成
    local -a stripped_stems=()
    for stem in "${stems[@]}"; do
      stripped_stems+=("${stem%$detected_common_suffix}")
    done

    # 再試行
    numbers=()
    for stem in "${stripped_stems[@]}"; do
      if __concat_extract_number "$stem"; then
        num_part="${REPLY%%:*}"
        rest="${REPLY#*:}"
        suffix_part="${rest%%:*}"
        prefix_part="${rest#*:}"

        numbers+=("$((10#$num_part))")

        if [[ -z "$first_prefix" ]]; then
          first_suffix="$suffix_part"
          first_prefix="$prefix_part"
        else
          if [[ "$suffix_part" != "$first_suffix" ]]; then
            print -r -- "エラー: サフィックスが異なります: '$first_suffix' と '$suffix_part'" >&2
            return 1
          fi
          if [[ "$prefix_part" != "$first_prefix" ]]; then
            print -r -- "エラー: ファイル名に連続性がありません: 共通プレフィックスがありません ('$first_prefix' と '$prefix_part')" >&2
            return 1
          fi
        fi
      else
        print -r -- "エラー: ファイル名に連番パターンがありません: $stem" >&2
        return 1
      fi
    done
  fi

  __CONCAT_R_NUMBERS=("${numbers[@]}")
  __CONCAT_R_COMMON_PREFIX="$first_prefix"
  __CONCAT_R_FIRST_SUFFIX="$first_suffix"
  __CONCAT_R_USE_STRIPPED=$use_stripped_stems
  __CONCAT_R_COMMON_SUFFIX="$detected_common_suffix"
  return 0
}
