# shellcheck shell=bash

# ファイルシステム種別の判定ヘルパー (av1ify / concat が共有)。
#
# ネットワークボリューム (smbfs/afpfs/nfs/webdav/cifs) は macOS のゴミ箱を持たないため、
# `trash` / Finder delete が必ず失敗する。元ファイルを消す処理はこのヘルパーで
# 判定して rm (物理削除) へ倒す。

# 内部補助: 与えられたパスが属するマウントポイントの filesystem type を返す
# 例: /Volumes/koji (smbfs マウント) -> "smbfs"
# 取得できない場合は空文字列
# macOS の `stat -f "%T"` は file type (ls -F 形式) を返すため使えない。
# `mount` 出力をパースしてパスにマッチする最長 mount point を選ぶ。
# ⚠️ macOS の mount 出力形式 "device on /mp (fstype, opts)" 前提。Linux の
# "device on /mp type ext4 (opts)" 形式はパースできない (空文字列 fallback になり
# 呼び出し側は trash 経路へ倒れる)。Linux 対応が必要になったら type 形式の分岐を追加すること。
__fs_type_for() {
  # 注意: zsh では `path` (小文字) は `PATH` (大文字) の配列形 tied parameter なので
  # `local path=...` で書くと関数内 PATH を引数値で上書きしてしまい、
  # `command -v` や process substitution での `mount` lookup が壊れる。
  # 必ず別名 (target_path) を使う。
  local target_path="$1"
  [[ -z "$target_path" ]] && return 0
  local mount_bin="mount"
  command -v "$mount_bin" >/dev/null 2>&1 || mount_bin="/sbin/mount"
  local line best="" best_len=0 mp
  while IFS= read -r line; do
    # フォーマット: "device on /mount/point (fstype, opts...)"
    mp="${line#* on }"
    mp="${mp%% \(*}"
    [[ -z "$mp" ]] && continue
    if [[ "$target_path" == "$mp" || "$target_path" == "$mp/"* || "$mp" == "/" ]]; then
      if (( ${#mp} > best_len )); then
        best_len=${#mp}
        best="$line"
      fi
    fi
  done < <("$mount_bin" 2>/dev/null)
  local fs="${best##*\(}"
  fs="${fs%%,*}"
  fs="${fs%%\)*}"
  print -r -- "$fs"
}

# 内部補助: パスがゴミ箱を持たないネットワークボリューム上にあれば 0 を返す。
# REPLY に判定に使った fs type を入れる (メッセージ表示用)。
__fs_is_network_volume() {
  REPLY=$(__fs_type_for "$1")
  case "$REPLY" in
    smbfs|afpfs|nfs|webdav|cifs) return 0 ;;
    *) return 1 ;;
  esac
}
