# video_args.sh — 動画ファイル引数を実パス配列に展開するヘルパー
#
# このファイルは source 専用 (実行不可)。
#
# 提供する関数:
#   expand_video_args <args...>
#     入力: コマンドライン引数 (個別パス または スペース区切り単一文字列)
#     出力: 実在する動画ファイルパスを NUL 区切りで stdout に出力
#     失敗: 見つからないパスがあれば 1 を返し、エラーを stderr に出す
#
# 利用例:
#   #!/usr/bin/env bash
#   source "$(dirname -- "${BASH_SOURCE[0]}")/lib/video_args.sh"
#
#   files=()
#   while IFS= read -r -d '' f; do files+=("$f"); done \
#     < <(expand_video_args "$@")
#
# 設計意図:
#   日本語ファイル名はスペースを含むことが多く、シェルの単純な単語分割では
#   分解できない。ユーザーが「a.mp4 b.mp4」のような複数ファイルを 1 引数で
#   渡しても、または個別に渡しても、どちらでも動くよう拡張子境界で分解する。

# 認識する動画拡張子。順序は重要ではないが、よく出現するものを先に並べる。
VIDEO_ARGS_EXTS=(
  .mp4 .MP4 .mov .MOV .mkv .MKV .avi .AVI
  .webm .WEBM .flv .FLV .wmv .WMV .m4v .M4V
  .mpg .MPG .mpeg .MPEG .3gp .3GP .ts .TS .m2ts .M2TS
)

expand_video_args() {
  local entry remaining ext path matched found_any
  for entry in "$@"; do
    if [[ -f "$entry" ]]; then
      printf '%s\0' "$entry"
      continue
    fi
    remaining="$entry"
    found_any=false
    while [[ -n "$remaining" ]]; do
      # 先頭空白除去
      remaining="${remaining#"${remaining%%[![:space:]]*}"}"
      [[ -z "$remaining" ]] && break
      matched=false
      for ext in "${VIDEO_ARGS_EXTS[@]}"; do
        if [[ "$remaining" == *"$ext"* ]]; then
          path="${remaining%%"$ext"*}${ext}"
          if [[ -f "$path" ]]; then
            printf '%s\0' "$path"
            remaining="${remaining#"$path"}"
            matched=true
            found_any=true
            break
          fi
        fi
      done
      if ! $matched; then
        if ! $found_any; then
          printf 'エラー: ファイルが見つかりません: %s\n' "$entry" >&2
          return 1
        fi
        remaining="${remaining#"${remaining%%[![:space:]]*}"}"
        if [[ -n "$remaining" ]]; then
          printf 'エラー: ファイルが見つかりません: %s\n' "$remaining" >&2
          return 1
        fi
        break
      fi
    done
  done
}
