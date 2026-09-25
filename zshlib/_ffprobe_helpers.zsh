# ffprobe 単一フィールド取得ヘルパー
#
# `ffprobe -v error [-select_streams X] -show_entries Y -of default=nk=1:nw=1 -- <file>
#  2>/dev/null | head -n1` という単一フィールド取得の定型 idiom を一元化する
# (av1ify / postcheck / video_health / validate_mp4 / repair_mp4 で ~30 箇所の手書き反復だった)。
#
# 対象は「1 フィールド 1 呼び出し」の形だけ。以下は形が違うため対象外で、各所に残す:
#   - `-of csv=p=0` (packet 走査や time_base 比較。先頭行を取るなら下の __ff_first_row を通す)
#   - `-read_intervals` 付き区間スキャン
#   - 複数行取得 (例: _validate_mp4.zsh の全 audio stream codec 列挙は head しない)
#
# 🚨 複数フィールドを 1 回の ffprobe に統合する改修はしないこと:
#   tests/zshrc/av1ify/test_helper.sh の mock ffprobe がクエリ文字列の部分一致で応答を
#   分岐するため、統合すると mock を書き直すことになる (_av1ify_encode.zsh の
#   __av1ify_probe_source 近傍の意図コメントが一次情報)。本ヘルパーは 1 フィールド
#   1 呼び出しを保ち、引数列も従来と同一なので mock に影響しない。

# __ff_stream_field <file> <select_streams> <entries>
#   例: __ff_stream_field "$in" v:0 stream=width
__ff_stream_field() {
  ffprobe -v error -select_streams "$2" -show_entries "$3" -of default=nk=1:nw=1 -- "$1" 2>/dev/null | head -n1
}

# __ff_format_field <file> <entries>
#   例: __ff_format_field "$in" format=duration
__ff_format_field() {
  ffprobe -v error -show_entries "$2" -of default=nk=1:nw=1 -- "$1" 2>/dev/null | head -n1
}

# __ff_first_row — stdin の最初の「空でない行」だけを出す (`-of csv=p=0` の先頭行を取る用)。
#
# 🚨 csv 出力の先頭行を `head -n1` / `${out%%$'\n'*}` で取らないこと:
#   stream group を持つ入力 (例: LCEVC 拡張レイヤ付き mp4) に対し、ffprobe 9 は
#   `-show_entries stream=...` でも stream_group セクションを出し、csv ではそれが
#   先頭の空行になる (実測 ffprobe 9.0.2: `-select_streams a -show_entries stream=index
#   -of csv=p=0` が "\n1\n")。先頭行を取ると空になり、「音声ストリームなし」等の誤判定になる。
#   `-show_entries stream_group=` 等で group 出力を抑止する指定は無かった (同版で実測)。
#   default=nk=1:nw=1 は空行を出さない (同版で実測) ので __ff_stream_field は対象外。
#   ffprobe が group を出さなくなれば (または抑止指定ができれば) 再評価。
__ff_first_row() {
  awk 'NF { print; exit }'
}
