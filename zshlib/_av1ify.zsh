# shellcheck shell=bash
# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

__AV1IFY_VERSION="1.6.1"
__AV1IFY_SPEC_VERSION="1.6.0"

# 内部補助: バナー出力
__av1ify_banner() {
  print -ru2 -- "av1ify v${__AV1IFY_VERSION} (spec: v${__AV1IFY_SPEC_VERSION})"
}

typeset -gi __AV1IFY_ABORT_REQUESTED=0
typeset -g  __AV1IFY_CURRENT_TMP=""
typeset -gi __AV1IFY_DRY_RUN=0
typeset -g  __AV1IFY_RESOLUTION=""
typeset -g  __AV1IFY_FPS=""
typeset -g  __AV1IFY_DENOISE=""
typeset -gi __AV1IFY_COMPACT=0

__av1ify_on_interrupt() {
  if (( __AV1IFY_ABORT_REQUESTED )); then
    return
  fi
  __AV1IFY_ABORT_REQUESTED=1
  local tmp="${__AV1IFY_CURRENT_TMP:-}"
  if [[ -n "$tmp" && -e "$tmp" ]]; then
    rm -f -- "$tmp"
    print -r -- "✋ 中断要求: 進行中の一時ファイルを削除しました ($tmp)"
  else
    print -r -- "✋ 中断要求: 残りの処理を停止します"
  fi
}

# 分割構成:
#   _av1ify_postcheck.zsh — __av1ify_mark_issue() + __av1ify_postcheck() (変換後チェック)
#   _av1ify_encode.zsh    — __av1ify_pre_repair() + __av1ify_one() (エンコード処理)
#   _av1ify.zsh (本ファイル) — 状態変数, バナー, 割り込み処理, av1ify() エントリポイント
# 読み込み順: postcheck → encode（__av1ify_one が __av1ify_postcheck を呼ぶため）
source "$HOME/dotfiles/zshlib/_av1ify_postcheck.zsh"
source "$HOME/dotfiles/zshlib/_av1ify_encode.zsh"

# 解像度値の検証と部分一致解決
# 入力: $1 = 解像度文字列
# 出力: REPLY = 解決後の解像度値
# 戻り値: 0=成功, 1=エラー
__av1ify_resolve_resolution() {
  local input="$1"
  local input_lower="${input:l}"

  # 完全一致（プリセット名）
  case "$input_lower" in
    480p|720p|1080p|1440p|4k)
      REPLY="$input_lower"
      return 0
      ;;
  esac

  # 純粋な数値で有効範囲内
  if [[ "$input" =~ ^[0-9]+$ ]] && (( input >= 16 && input <= 8640 )); then
    REPLY="$input"
    return 0
  fi

  # 部分一致（プリセット名の前方一致）
  local -a presets=(480p 720p 1080p 1440p 4k)
  local -a matches=()
  local p
  for p in "${presets[@]}"; do
    if [[ "$p" == "$input_lower"* ]]; then
      matches+=("$p")
    fi
  done

  if (( ${#matches[@]} == 1 )); then
    print -r -- ">> 解像度 '${input}' → ${matches[1]} に解決しました"
    REPLY="${matches[1]}"
    return 0
  fi

  if (( ${#matches[@]} > 1 )); then
    print -r -- ">> 解像度 '${input}' → ${matches[1]} に解決しました (候補: ${(j:, :)matches})"
    REPLY="${matches[1]}"
    return 0
  fi

  # 一致なし → エラー
  print -r -- "エラー: 無効な解像度: ${input}" >&2
  print -r -- "  有効な値: 480p, 720p, 1080p, 1440p, 4k, または 16-8640 の数値" >&2
  return 1
}

av1ify() {
  local __av1ify_internal=0
  if [[ -n ${__AV1IFY_INTERNAL_CALL:-} ]]; then
    __av1ify_internal=1
    unset __AV1IFY_INTERNAL_CALL
  fi

  setopt LOCAL_OPTIONS localtraps

  # ルート呼び出しでは毎回デフォルト（内部呼び出しのみ伝搬）
  local dry_run=0
  local show_help=0
  local opt_resolution=""
  local opt_fps=""
  local opt_denoise=""
  local opt_compact=0
  local -a positional=()
  while (( $# > 0 )); do
    case "$1" in
      --dry-run|-n)
        dry_run=1
        ;;
      -h|--help)
        (( ! __av1ify_internal )) && show_help=1
        ;;
      -c|--compact)
        opt_compact=1
        ;;
      -r|--resolution)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --resolution には値が必要です" >&2
          return 1
        fi
        opt_resolution="$1"
        ;;
      --fps)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --fps には値が必要です" >&2
          return 1
        fi
        opt_fps="$1"
        ;;
      --denoise)
        shift
        if (( $# == 0 )); then
          print -r -- "エラー: --denoise には値が必要です" >&2
          return 1
        fi
        opt_denoise="$1"
        ;;
      -f)
        positional+=("$1")
        ;;
      -*)
        print -r -- "エラー: 不明なオプション: $1" >&2
        return 1
        ;;
      *)
        positional+=("$1")
        ;;
    esac
    shift
  done
  set -- "${positional[@]}"

  # --compact: 720p + 30fps プリセット（明示的な -r/--fps が優先）
  if (( opt_compact )); then
    [[ -z "$opt_resolution" ]] && opt_resolution="720p"
    [[ -z "$opt_fps" ]] && opt_fps="30"
  fi

  if (( ! __av1ify_internal )); then
    __AV1IFY_DRY_RUN=$dry_run
    # CLI オプションと環境変数 AV1_RESOLUTION をここで統合
    # → 各ファイル処理 (__av1ify_one) での二重バリデーションを回避
    __AV1IFY_RESOLUTION="${opt_resolution:-${AV1_RESOLUTION:-}}"
    __AV1IFY_FPS="$opt_fps"
    __AV1IFY_DENOISE="$opt_denoise"
    __AV1IFY_COMPACT=$opt_compact
  else
    dry_run="${__AV1IFY_DRY_RUN:-$dry_run}"
  fi

  # バナー出力（内部呼び出し・ヘルプ時は除く）
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    __av1ify_banner
  fi

  # 解像度の早期バリデーション（バナー出力後に配置し、解決メッセージの表示順を統一）
  if (( ! __av1ify_internal )) && [[ -n "$__AV1IFY_RESOLUTION" ]]; then
    if __av1ify_resolve_resolution "$__AV1IFY_RESOLUTION"; then
      __AV1IFY_RESOLUTION="$REPLY"
      opt_resolution="$REPLY"
    else
      return 1
    fi
  fi

  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    if (( opt_compact )); then
      print -r -- ">> compact モード: -r ${opt_resolution} --fps ${opt_fps}"
    fi
  fi

  (( ! __av1ify_internal && dry_run )) && print -r -- "[DRY-RUN] ファイルは変更しません"

  if (( ! __av1ify_internal )) && { (( show_help )) || (( $# == 0 )); }; then
    cat <<'EOF'
av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。

機能:
  - 指定されたファイルまたはディレクトリを対象に処理を実行します。
  - 出力ファイル名は `<元のファイル名>-enc.mp4` となります。
  - 既に変換済みのファイルが存在する場合は、処理をスキップします。
  - 処理中には `<ファイル名>.in_progress` という一時ファイルを作成し、変換成功後にリネームします。
  - 変換後に音声ストリームと音ズレを簡易チェックし、問題が見つかればファイル名末尾に注意書きを付けます。
  - ffprobeを使用して入力ファイルの音声コーデックを判別し、可能であれば音声を無劣化でコピーします。
    (デフォルトでAAC, ALAC, MP3に対応)
    対応していない形式の場合は、AAC (96kbps, 48kHz, 2ch) に再エンコードします。
  - ディレクトリを指定した場合、再帰的に動画ファイル (avi, mkv, rm, wmv, mpg) を検索して変換します。
    (ファイル名の大文字・小文字は区別しません)

使い方:
  av1ify [オプション] <ファイルパス または ディレクトリパス> [<ファイルパス2> ...]
  av1ify -f <ファイルリスト>

  例:
    # 単一のファイルを変換
    av1ify "/path/to/movie.avi"

    # 複数のファイルを順番に変換
    av1ify xxx.mp4 yyy.mp4 zzz.mp4

    # ファイルリストから変換（改行区切り）
    av1ify -f list.txt

    # ディレクトリ内のすべての動画ファイルを変換
    av1ify "/path/to/dir"

    # CRF値を指定して画質を調整
    AV1_CRF=35 av1ify "/path/to/movie.mp4"

    # 720pに解像度を変更して変換（アスペクト比は維持）
    av1ify -r 720p "/path/to/movie.mp4"

    # 24fpsに変更して変換
    av1ify --fps 24 "/path/to/movie.mp4"

    # 解像度とfpsを両方指定
    av1ify -r 1080p --fps 30 "/path/to/movie.mp4"

    # ノイズ除去で圧縮率を上げる（ノイジーな素材に効果的）
    av1ify --denoise medium "/path/to/movie.mp4"

    # 720p + ノイズ除去の組み合わせ
    av1ify -r 720p --denoise light "/path/to/movie.mp4"

    # 保存用プリセット（720p + 30fps）
    av1ify --compact "/path/to/movie.mp4"

    # --compact + 解像度だけ上書き（480p + 30fps）
    av1ify --compact -r 480p "/path/to/movie.mp4"

オプション:
  -h, --help: このヘルプメッセージを表示します。
  -n, --dry-run: 実行内容のみを表示し、ファイルを変更しません。
  -f <ファイル>: 改行区切りでファイルパスが記載されたリストファイルを読み込んで処理します。
  -r, --resolution <値>: 出力解像度（縦）を指定します。アスペクト比は維持されます。
      480p / 720p / 1080p / 1440p / 4k または数値（例: 540）
  --fps <値>: 出力フレームレートを指定します（例: 24, 30, 60）。
  -c, --compact: 保存用プリセット（720p + 30fps）。-r や --fps で個別に上書き可能。
  --denoise <レベル>: ノイズ除去を適用します。圧縮率が向上しますが、ディテールが失われます。
      light: 軽度（hqdn3d=2:2:3:3）
      medium: 中程度（hqdn3d=4:4:6:6）
      strong: 強め（hqdn3d=6:6:9:9）

依存関係:
  - ffmpeg: 動画のエンコードとデコードに使用します。
  - ffprobe: (ffmpegに含まれます) メディアファイルの情報を取得するために使用します。

環境変数による設定:
  以下の環境変数を設定することで、エンコードの挙動を調整できます。

  AV1_CRF (デフォルト: 40)
    品質を制御します (Constant Rate Factor)。値が低いほど高画質・高ビットレートになります。
    SVT-AV1の推奨範囲は 20 (高画質) から 50 (低画質) です。

  AV1_PRESET (デフォルト: 5)
    エンコード速度と圧縮率のバランスを調整します。値が小さいほど高品質（高圧縮）になりますが、
    エンコードに時間がかかります。SVT-AV1では 0 (最高品質) から 12 (最速) の範囲で設定します。

  AV1_COPY_OK (デフォルト: "aac,alac,mp3")
    MP4コンテナで音声を無劣化コピーすることを許可する音声コーデックをカンマ区切りで指定します。

  AV1_RESOLUTION (デフォルト: なし)
    出力解像度を指定します。--resolution オプションと同等です。
    CLIオプションが優先されます。

  AV1_FPS (デフォルト: なし)
    出力フレームレートを指定します。--fps オプションと同等です。
    CLIオプションが優先されます。

  AV1_DENOISE (デフォルト: なし)
    ノイズ除去レベルを指定します。--denoise オプションと同等です。
    light / medium / strong から選択。CLIオプションが優先されます。
EOF
    return 0
  fi

  local __av1ify_is_root=0
  if (( ! __av1ify_internal )); then
    __av1ify_is_root=1
    __AV1IFY_ABORT_REQUESTED=0
    __AV1IFY_CURRENT_TMP=""
    trap '__av1ify_on_interrupt' INT TERM HUP
  fi

  set -o pipefail

  # -f オプションでファイルリストを読み込む
  if [[ "$1" == "-f" ]]; then
    if (( $# < 2 )); then
      print -r -- "エラー: -f オプションにはファイルパスが必要です" >&2
      return 1
    fi
    local listfile="$2"
    if [[ -z "$listfile" ]]; then
      print -r -- "エラー: -f オプションにはファイルパスが必要です" >&2
      return 1
    fi
    if [[ ! -f "$listfile" ]]; then
      print -r -- "エラー: ファイルが見つかりません: $listfile" >&2
      return 1
    fi

    local -a files=()
    while IFS= read -r line; do
      # 空行とコメント行（#で始まる）をスキップ
      [[ -z "$line" || "$line" == \#* ]] && continue
      files+=("$line")
    done < "$listfile"

    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $listfile）"
      return 0
    fi

    local target ok=0 ng=0
    for target in "${files[@]}"; do
      print -r -- "---- 処理: $target"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
    return 0
  fi

  # 複数の引数がある場合は、それぞれを順番に処理
  if (( $# > 1 )); then
    local target ok=0 ng=0
    for target in "$@"; do
      print -r -- "---- 処理: $target"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
    return 0
  fi

  local target="$1"
  if [[ -d "$target" ]]; then
    # 再帰で対象拡張子のみ列挙（(#i)で大文字小文字無視、.Nで通常ファイルのみ）
    setopt LOCAL_OPTIONS extended_glob null_glob
    unsetopt LOCAL_OPTIONS SH_WORD_SPLIT
    local -a files=()
    while IFS= read -r -d '' f; do
      files+=("$f")
    done < <(find "$target" -type f \( \
        -iname '*.avi' -o -iname '*.mkv' -o -iname '*.rm' -o -iname '*.wmv' -o \
        -iname '*.mpg' -o -iname '*.mpeg' -o -iname '*.mov' -o -iname '*.mp4' -o \
        -iname '*.flv' -o -iname '*.webm' -o -iname '*.3gp' -o -iname '*.ts' \
      \) -print0 | sort -z)
    if (( ${#files[@]} == 0 )); then
      print -r -- "（対象ファイルなし: $target）"; return 0
    fi
    local f ok=0 ng=0
    # 各ファイルは av1ify() を通して単体処理ルートを再利用（直列実行）
    for f in "${files[@]}"; do
      print -r -- "---- 処理: $f"
      if __AV1IFY_INTERNAL_CALL=1 av1ify "$f"; then
        ((ok++))
      else
        local exit_status=$?
        if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
          print -r -- "✋ 中断: 残りのファイルをスキップします"
          return 130
        fi
        ((ng++))
      fi
    done
    print -r -- "== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))"
  else
    __av1ify_one "$target"
  fi
}
