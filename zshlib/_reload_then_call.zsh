# 動画系コマンドの lazy-reload ラッパーの共通実体。呼び出しごとに lib を source し直し、
# シェル再起動なしで lib の編集を反映する。lib は公開名と同名の関数を再定義してラッパー自身を
# 上書きするため、実行後に save/restore で復元が必須 (t/tt は公開名≠実体名で復元不要な別 idiom。
# ここに統合しないこと)。
#
# _zshrc でなくこのファイルに置く理由: Claude Code の shell snapshot は `_` 始まりの関数を
# 含めない。snapshot に載った公開ラッパーが呼ばれたとき、この関数が無ければここを source して
# 自己修復する (各ラッパーの先頭ガード)。_zshrc 内定義だと修復元が無い (issue 152)。
#
# 引数: $1=復元する公開関数名 (空白区切りで複数可: 1 つの lib が複数のラッパーを上書きする場合、
#          挙げ漏らした名前は実体で固定されて lazy-reload が黙って死ぬ)
#       $2=lib パス $3=lib 内の実体関数名 (以降が実引数)
_reload_then_call() {
  local _fnames="$1" _lib="$2" _impl="$3"
  shift 3
  local -A _saved
  local _f
  for _f in ${=_fnames}; do _saved[$_f]="${functions[$_f]}"; done
  source "$_lib"
  "$_impl" "$@"
  local _ret=$?
  for _f in ${=_fnames}; do functions[$_f]="${_saved[$_f]}"; done
  return $_ret
}
