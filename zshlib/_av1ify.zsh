# shellcheck shell=bash
# ------------------------------------------------------------------------------
# av1ify — 入力された動画ファイル、またはディレクトリ内の動画ファイルをAV1形式のMP4に一括変換します。
# ------------------------------------------------------------------------------

__AV1IFY_VERSION="1.7.2"
__AV1IFY_SPEC_VERSION="1.7.0"

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
typeset -gi __AV1IFY_FORCE=0
typeset -gi __AV1IFY_DELETE_ORIGIN=0
# NG 発生時の理由文字列。__av1ify_one / __av1ify_postcheck が return 1 直前に設定し、
# バッチループ (__av1ify_run_batch) が末尾の NG 一覧で使用する。
typeset -g  __AV1IFY_LAST_NG_REASON=""
# 走行中の prefetch (バックグラウンド先読み) の PID。中断時に __av1ify_kill_prefetches でまとめて掃除する。
typeset -ga __AV1IFY_PREFETCH_PIDS=()

# 内部補助: 次に処理予定のファイルを background で先読みし、
# Dropbox / iCloud の File Provider materialize を現エンコード中に進めておく。
#
# 仕組み: head -c 1 で open() させると File Provider が fetchContents を発火し、
# replicated extension モデルでは「全体ダウンロードが終わるまで read() がブロック」
# する。1 byte 読めば materialize は完了済みなので、プロセス終了後もファイルは
# ローカルに残る。cat /dev/null と違い、materialize 後にローカル SSD 全バイトを
# 再読みする無駄が無い。
#
# range-based fetch の File Provider (一部の iCloud 使い方など) では先頭 1 byte
# だけしか落ちない可能性があるが、その場合でも prefetch しない場合と比べて損は
# しないため、最悪ケースでも安全 (= "効かない" だけ)。
#
# 引数: $1 = 先読みしたいファイルパス
# 副作用: __AV1IFY_PREFETCH_PIDS に PID を追加 (中断時掃除用)
__av1ify_prefetch() {
  local target="$1"
  [[ -z "$target" || ! -f "$target" ]] && return
  (( __AV1IFY_DRY_RUN )) && return
  ( head -c 1 < "$target" > /dev/null 2>&1 ) &
  __AV1IFY_PREFETCH_PIDS+=("$!")
}

# 内部補助: 走行中の prefetch を全て kill (中断時の掃除用)
# 既に終了している PID への kill は exit 1 を返すが、err_exit 環境 (テスト harness 等)
# でも安全に呼べるよう `|| true` で吸収する。
__av1ify_kill_prefetches() {
  local pid
  for pid in "${__AV1IFY_PREFETCH_PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  __AV1IFY_PREFETCH_PIDS=()
}

__av1ify_on_interrupt() {
  if (( __AV1IFY_ABORT_REQUESTED )); then
    return
  fi
  __AV1IFY_ABORT_REQUESTED=1
  __av1ify_kill_prefetches
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
# shellcheck disable=SC1091
source "${0:A:h}/_video_health.zsh"
# shellcheck disable=SC1091
source "${0:A:h}/_av1ify_postcheck.zsh"
# shellcheck disable=SC1091
source "${0:A:h}/_av1ify_encode.zsh"

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
    print -P -- "%F{cyan}>> 解像度 '${input}' → ${matches[1]} に解決しました%f"
    REPLY="${matches[1]}"
    return 0
  fi

  if (( ${#matches[@]} > 1 )); then
    # shellcheck disable=SC2296
    print -P -- "%F{cyan}>> 解像度 '${input}' → ${matches[1]} に解決しました (候補: ${(j:, :)matches})%f"
    REPLY="${matches[1]}"
    return 0
  fi

  # 一致なし → エラー
  print -r -- "エラー: 無効な解像度: ${input}" >&2
  print -r -- "  有効な値: 480p, 720p, 1080p, 1440p, 4k, または 16-8640 の数値" >&2
  return 1
}

# fps 値の検証 (0 < fps <= 240 の数値)
# 引数: $1 = fps 文字列
# 戻り値: 0=有効, 1=無効
__av1ify_validate_fps() {
  local fps="$1"
  [[ "$fps" =~ ^[0-9]+(\.[0-9]+)?$ ]] || return 1
  local ok
  ok=$(awk -v fps="$fps" 'BEGIN { print (fps > 0 && fps <= 240) ? 1 : 0 }')
  (( ok ))
}

# 内部補助: バッチ処理ループ + 末尾 NG 一覧の出力
# 引数: 処理対象ファイル/ディレクトリのパスを位置引数で渡す
# 出力: 各ファイルの処理ログ + 末尾サマリ + (NG があれば) NG 一覧
# 戻り値: 0=全件OK, 1=NG あり, 130=中断
# 副作用: __AV1IFY_LAST_NG_REASON を反復ごとにクリアする。
#         NG ありで返るときは __AV1IFY_LAST_NG_REASON に集計理由をセットする
#         (バッチ内にディレクトリが混在するケースで、外側のバッチが NG として
#          集計できるようにするため)
__av1ify_run_batch() {
  local target ok=0 ng=0
  local -a ng_list=()
  local -a targets=( "$@" )
  local n=${#targets[@]} i next exit_status
  # 直前 av1ify 呼び出しから持ち越した stale PID をクリア (PID 再利用での誤 kill を避ける)
  __AV1IFY_PREFETCH_PIDS=()
  for (( i = 1; i <= n; i++ )); do
    target="${targets[i]}"
    print -r -- "---- 処理: $target"
    __AV1IFY_LAST_NG_REASON=""
    # 次のファイルを background で先読み (クラウド materialize を現エンコード中に進める)。
    # ただしファイル名/ローカル glob だけで SKIP 確定の対象 (-enc.mp4 自体や既存出力済) は
    # __av1ify_one が即座に return 0 して終わるので、materialize させる意味が無い。
    # ディレクトリ指定で大量の既変換ファイルが含まれているケースで「全部 prefetch されて
    # 不要なダウンロードが走る」事故を防ぐため、prefetch 前にゲートする。
    next="${targets[i+1]:-}"
    if [[ -n "$next" ]] && ! __av1ify_skip_by_name "$next"; then
      __av1ify_prefetch "$next"
    fi
    if __AV1IFY_INTERNAL_CALL=1 av1ify "$target"; then
      ((ok++))
    else
      exit_status=$?
      if (( exit_status == 130 || __AV1IFY_ABORT_REQUESTED )); then
        __av1ify_kill_prefetches
        print -r -- "✋ 中断: 残りのファイルをスキップします"
        return 130
      fi
      ((ng++))
      # TAB を区切りに使い、ファイルパスに改行が混じっても扱えるようにする
      ng_list+=("${target}"$'\t'"${__AV1IFY_LAST_NG_REASON:-理由不明 (上のログを参照)}")
    fi
  done
  # 視認性: NG が無ければ緑 (= 全部 OK で安心), NG があれば黄 (= 下の一覧確認)
  local sum_color="green"
  (( ng > 0 )) && sum_color="yellow"
  print -P -- "%F{${sum_color}}== サマリ: OK=$ok / NG=$ng / ALL=$((ok+ng))%f"
  if (( ng > 0 )); then
    print -r -- "── NG 一覧 (${ng}件) ──"
    local entry f r
    for entry in "${ng_list[@]}"; do
      f="${entry%%$'\t'*}"
      r="${entry#*$'\t'}"
      print -r -- "  ✗ $f"
      print -r -- "    └─ $r"
    done
    # NG を exit code に反映する (bin/av1ify・Finder action・スクリプト連携が失敗を検知できる)。
    # ディレクトリがバッチに混在した場合、ネストしたバッチの NG はこの非0で外側に伝搬する。
    __AV1IFY_LAST_NG_REASON="バッチ内に NG ${ng}件 (内訳は上の NG 一覧を参照)"
    return 1
  fi
  return 0
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
  local opt_force=0
  local opt_delete_origin=0
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
      --force)
        opt_force=1
        ;;
      --delete-origin-if-success-and-no-ng)
        opt_delete_origin=1
        ;;
      --no-delete-origin-if-success-and-no-ng)
        opt_delete_origin=0
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
    # CLI オプションと環境変数 (AV1_RESOLUTION / AV1_FPS / AV1_DENOISE) をここで統合
    # → 各ファイル処理 (__av1ify_one) での二重バリデーションを回避
    __AV1IFY_RESOLUTION="${opt_resolution:-${AV1_RESOLUTION:-}}"
    __AV1IFY_FPS="${opt_fps:-${AV1_FPS:-}}"
    __AV1IFY_DENOISE="${opt_denoise:-${AV1_DENOISE:-}}"
    __AV1IFY_COMPACT=$opt_compact
    __AV1IFY_FORCE=$opt_force
    __AV1IFY_DELETE_ORIGIN=$opt_delete_origin
  else
    dry_run="${__AV1IFY_DRY_RUN:-$dry_run}"
  fi

  # バナー出力（内部呼び出し・ヘルプ時は除く）
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    __av1ify_banner
  fi

  # resolution / fps / denoise の早期バリデーション (fail-fast)。
  # 旧実装は fps/denoise をファイルごとに警告して黙って無視していたため、`--fps abc` の
  # ようなタイポでも全ファイルが fps 指定なしでフルエンコードされてしまっていた。
  # 配置: バナー出力後 (解決メッセージの表示順を統一)。
  # ゲート: help 表示・引数なしのときは検証しない (無効な AV1_* 環境変数が残っていても
  # `av1ify --help` が読めなくなる regression を防ぐ。codex P2 指摘)。
  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    if [[ -n "$__AV1IFY_RESOLUTION" ]]; then
      if __av1ify_resolve_resolution "$__AV1IFY_RESOLUTION"; then
        __AV1IFY_RESOLUTION="$REPLY"
        opt_resolution="$REPLY"
      else
        return 1
      fi
    fi
    if [[ -n "$__AV1IFY_FPS" ]] && ! __av1ify_validate_fps "$__AV1IFY_FPS"; then
      print -r -- "エラー: 無効なfps指定: ${__AV1IFY_FPS}（0より大きく240以下で指定してください）" >&2
      return 1
    fi
    if [[ -n "$__AV1IFY_DENOISE" ]]; then
      case "${__AV1IFY_DENOISE:l}" in
        light|medium|strong)
          __AV1IFY_DENOISE="${__AV1IFY_DENOISE:l}"
          ;;
        *)
          print -r -- "エラー: 無効なdenoise指定: ${__AV1IFY_DENOISE}（light/medium/strong から選択してください）" >&2
          return 1
          ;;
      esac
    fi
  fi

  if (( ! __av1ify_internal )) && (( ! show_help )) && (( $# > 0 )); then
    if (( opt_compact )); then
      print -P -- "%F{cyan}>> compact モード: -r ${opt_resolution} --fps ${opt_fps}%f"
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
  --force: 入力ファイルの健全性チェックに失敗してもエンコードを続行します。
      軽微なA/V音ズレなど、許容できる問題がある場合に使用してください。
  --delete-origin-if-success-and-no-ng: 変換成功かつpostcheckでNG無しの場合、元ファイルを削除します。
      av1c (compactショートハンド) ではデフォルトで有効です。
      --no-delete-origin-if-success-and-no-ng で明示的に無効化できます。

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

  AV1IFY_FRAME_TOLERANCE (デフォルト: 24)
    変換前後のフレーム数差がこの値以下であれば警告しません。
    再エンコード時の数フレームの差異は通常無害なため、既定値は24（約1秒分）です。

  AV1IFY_SYNC_TOLERANCE (デフォルト: 2.0)
    encode 前後で「音声 - 映像 duration」の関係差がこの値[秒]以下であれば警告しません。
    ソース時点で音ズレしている素材 (末尾無音映像が残る MKV 等) を encode 由来と
    誤判定しないよう、絶対値ではなく enc 前後の差分のみを評価します。
    MKV など stream duration を出さないコンテナでは packet PTS を走査して
    真の duration を測ります (5GB クラスで数秒オーダーの追加コスト)。
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

    __av1ify_run_batch "${files[@]}"
    return $?
  fi

  # 複数の引数がある場合は、それぞれを順番に処理
  if (( $# > 1 )); then
    __av1ify_run_batch "$@"
    return $?
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
    # 各ファイルは av1ify() を通して単体処理ルートを再利用（直列実行）
    __av1ify_run_batch "${files[@]}"
  else
    __av1ify_one "$target"
  fi
}
