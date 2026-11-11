# serial_run_shim.sh — 重いコマンドの PATH shim (bin/swift・bin/xcodebuild) の共通部分。bash 3.2 から source する。
# shellcheck shell=bash
#
# shim は PATH の先頭 (~/dotfiles/bin) で同名のコマンドを受け、直列化の対象の呼び出しを serial-run の順番待ち (既定は repo ごとの列) に通す。
# Claude Code の hook (_claude/hooks/serial-run-wrap.sh) は Bash ツールが打った字面しか包めない。スクリプトの中・コマンド置換の中・
# 人の端末から打ったものは、PATH の shim でしか拾えない。
# 直列化の対象かの判定は serial-run --if-heavy (src/serialrun/hook.go の isHeavy) に任せ、ここに写さない。
#
#   serial_run_shim_resolve <名前>   実体を real に入れる (見つからなければ 127、自分を選んだら 70 で終わる)
#   serial_run_shim_exec <引数…>     列に並ばない条件なら実体を、それ以外は serial-run --if-heavy を exec する (戻らない)
#
# 列に並ばないとき: CI がある (runner は 1 台に 1 本) / SERIAL_RUN_SHIM_DISABLE がある (人が外す) / serial-run が無い。
# 🚨 列の中 (SERIAL_RUN_HELD がある) でも serial-run を通す。値の有無だけで素通しにすると、終わった列から漏れて残った値
# (列の中で背景に回した孫) で、走っている他の 1 本と重なる。札・nonce・祖先の照合は serial-run の入れ子の判定にだけ置く
# (その代わり、列の中の呼び出しも serial-run の起動の分だけ遅くなる)。
#
# 待ちの上限は serial-run の既定 (1 時間。hook が包んだ経路と同じ) のままにする。--max-wait 0 にしない: 下の「パイプで止まる形」が
# 永久に解けなくなる。上限で抜けたら rc 125 で、stderr の serial-run: の行に待ちの上限と出る (呼び出し元のスクリプトはビルドの失敗と読む)。
# serial-run がビルドできない日は SERIAL_RUN_FAIL_OPEN で、順番を待たずに実体を起こす (bin/serial-run の冒頭)。
# 呼ぶ serial-run はこの shim と同じ checkout のもの (bin/serial-run)。本体 checkout に固定すると、shim と serial-run のバージョンが
# ずれたとき (worktree の shim から --if-heavy を知らない serial-run を呼ぶ) に 125 で落ち、ビルドは通るので fail-open も働かない
#
# 適用されない経路: PATH に ~/dotfiles/bin が無い起動元 (Dock から起動した Xcode・launchd・cron・xcodebuild test が起こすテストのプロセス)、
# 絶対パス (/usr/bin/swift)、`xcrun swift`・`xcrun xcodebuild`、swift-build / swift-test の直叩き。
# 🚨 1 つのパイプラインに重いコマンドを 2 段並べた形 (`swift build | swift test`) では、後ろの段が列で待つ間パイプを読まないので、
# 前の段の出力がパイプの容量を超えると、待ちの上限 (1 時間) まで両方が止まる。hook は字面で見送れるが、shim には字面が見えない。
# stdin がパイプなら並ばない、で避けない: fastlane (Ruby の Open3) などは子の stdin にパイプを渡すので、その経路が丸ごと並ばなくなる。
# 再び考えるなら、この形で実際に止まった記録 (serial-run status の先頭と待ち手のコマンド) を添えて、stdin 以外で見分ける手段を示す
# 🚨 列を持ったコマンドが、環境を引き継がずに (SERIAL_RUN_HELD を落として) 子孫ではないプロセスに重いコマンドを打たせると、
# そのプロセスは持ち主の終わりを待ち、持ち主はそのプロセスを待って止まる (serial-run は祖先のプロセスも見るが、子孫でなければ届かない)。

# 実体は PATH を走査して決める。/usr/bin/swift に固定しない (swiftly などが PATH で選んだ toolchain を上書きする)。
# 🚨 dotfiles の bin (別の checkout・worktree も。隣に _claude/settings.json がある) の同名のファイルは shim なので飛ばす。
# 飛ばさないと 2 つの checkout の shim が互いを実体と読んで回り続ける。symlink (~/.local/bin/swift -> 別の checkout の shim) は
# 実体の置き場で見る (symlink のときだけ readlink を起こす。普段の /usr/bin/swift は symlink ではない)。
# readlink は絶対パスで呼び、解決できない symlink は実体に採らない (PATH に /usr/bin が無いと解決に失敗し、別の checkout の shim を
# 実体と読んで 2 つの shim が互いを exec し続ける)
serial_run_shim_resolve() {  # $1=名前
  local name=$1 rest d c t
  real=
  rest=$PATH:
  while [ -n "$rest" ]; do
    d=${rest%%:*}
    rest=${rest#*:}
    [ -n "$d" ] || d=.
    c=$d/$name
    [ -f "$c" ] && [ -x "$c" ] || continue
    [ "$c" -ef "$0" ] && continue
    [ -f "$d/../_claude/settings.json" ] && continue
    if [ -L "$c" ]; then
      t=$(/usr/bin/readlink -f "$c") || continue
      [ -f "${t%/*}/../_claude/settings.json" ] && continue
    fi
    real=$c
    break
  done
  if [ -z "$real" ]; then
    printf '%s (dotfiles の shim): PATH に実体の %s が無い (shim 自身と dotfiles の bin を除いて探した)\n' "$name" "$name" >&2
    exit 127
  fi
  # 自己参照の検出 (上の走査が壊れて自分を返したら、exec せずに止める。無限の再帰はエラーを出さずに回り続ける)
  if [ "$real" -ef "$0" ]; then
    printf '%s (dotfiles の shim): 実体として自分自身を選んだ (%s)。中止\n' "$name" "$real" >&2
    exit 70
  fi
}

serial_run_shim_exec() {
  if [ -n "${CI:-}" ] || [ -n "${SERIAL_RUN_SHIM_DISABLE:-}" ]; then
    exec "$real" "$@"
  fi
  local sr=${SERIAL_RUN_BIN:-${BASH_SOURCE[0]%/*}/../serial-run}
  [ -x "$sr" ] || exec "$real" "$@"
  # zsh は -f で起こす (bin/serial-run の shebang は env zsh)。-f が無いと ~/.zshenv を読み、そこで xcodebuild / swift を呼ぶ設定
  # (`export XCODE_VERSION=$(xcodebuild -version)` など) があると、shim → serial-run → .zshenv → shim と再帰して実体まで届かない
  SERIAL_RUN_FAIL_OPEN=1 exec /bin/zsh -f "$sr" --if-heavy -- "$real" "$@"
}
