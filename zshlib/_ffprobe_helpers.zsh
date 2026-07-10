# ffprobe 単一フィールド取得ヘルパー
#
# `ffprobe -v error [-select_streams X] -show_entries Y -of default=nk=1:nw=1 -- <file>
#  2>/dev/null | head -n1` という単一フィールド取得の定型 idiom を一元化する
# (av1ify / postcheck / video_health / validate_mp4 / repair_mp4 で ~30 箇所の手書き反復だった)。
#
# 対象は「1 フィールド 1 呼び出し」の形だけ。以下は形が違うため対象外で、各所に残す:
#   - `-of csv=p=0` (packet 走査や time_base 比較)
#   - `-read_intervals` 付き区間スキャン
#   - 複数行取得 (例: _validate_mp4.zsh の全 audio stream codec 列挙は head しない)
#
# ⚠️ 複数フィールドを 1 回の ffprobe に統合する改修はしないこと:
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
