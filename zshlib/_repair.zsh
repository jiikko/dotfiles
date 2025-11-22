# shellcheck shell=bash
# ------------------------------------------------------------------------------
# repair — ファイル形式に応じた修復コマンド
# ------------------------------------------------------------------------------

# 個別の修復コマンドをロード
local _repair_dir="${${(%):-%x}:A:h}"
source "$_repair_dir/_repair_mp4.zsh"

repair() {
  if [[ "$1" == "-h" || "$1" == "--help" || -z "$1" ]]; then
    cat <<'EOF'
repair — 問題のある動画ファイルを修復します

機能:
  ファイルの拡張子を判別し、適切な修復処理を行います。

対応形式:
  .mp4, .m4v, .mov, .ts, .mts, .m2ts

使い方:
  repair <ファイルパス> [<ファイルパス2> ...]

  例:
    repair movie.mp4
    repair video1.mp4 video2.ts video3.mov
EOF
    return 0
  fi

  local file
  local ok=0 ng=0 skip=0

  for file in "$@"; do
    if [[ ! -f "$file" ]]; then
      print -r -- "✗ ファイルが無い: $file"
      (( ng++ )) || true
      continue
    fi

    local ext="${file:e:l}"  # 拡張子を小文字で取得

    case "$ext" in
      mp4|m4v|mov|ts|mts|m2ts)
        if repair_mp4 "$file"; then
          (( ok++ )) || true
        else
          (( ng++ )) || true
        fi
        ;;
      *)
        print -r -- "⚠️ 未対応の形式: $file (.$ext)"
        (( skip++ )) || true
        ;;
    esac
  done

  if (( $# > 1 )); then
    print -r -- "== サマリ: OK=$ok / NG=$ng / SKIP=$skip / ALL=$#"
  fi
}
