# shellcheck shell=bash
# runtimeout.sh — runtimeout (src/runtimeout。時間の上限付き実行) のバイナリを解決する。bash / zsh の両方から source する。
#
# runtimeout_resolve <repo root>: RUNTIMEOUT にバイナリの絶対パスを入れて export する。
#   既に RUNTIMEOUT にこの checkout のバイナリが入っていれば、それを使う (PATH を絞った子へ渡す形)。
#   無ければラッパー (bin/runtimeout) を 1 回通してビルドを済ませる。ビルド・起動できなければ rc=1。
# 呼び出しは "$RUNTIMEOUT" [-f] <秒> <コマンド>… (時間切れは rc=124)。
#
# 🚨 ラッパーを毎回通さず、最初に 1 回だけ解決する。ラッパーは古ければ同期ビルドするので、
#   - ビルドの失敗 (rc=1) が子の rc=1 と見分けられない (変異検証の red と読みうる)
#   - ビルドの進捗が片方の出力にだけ混ざる / 1 回目のビルドの待ちが上限を食う
#   - PATH を絞った環境 (tests/bin/test_claude_bin.sh) では zsh / go が見つからない
# 🚨 テストは PATH・HOME・偽の go を差し替える**前**に呼ぶ (HOME を隔離すると GOCACHE が空になり、ビルドが cold になる)。
runtimeout_resolve() {  # $1=repo root
  # 表記を揃える (`bin/..`・symlink)。揃えないと同じバイナリを「別物」と読んでラッパーを通し直す
  local root
  # CDPATH / chpwd の出力を root に混ぜない
  root="$(CDPATH='' cd -- "$1" >/dev/null && pwd -P)" || { echo "✗ runtimeout_resolve: repo root に入れない: $1" >&2; return 1; }
  # 🚨 既にある値は、この checkout のバイナリを指しているときだけ使う。別の checkout の版 (bin/mutate-verify が
  #    export したまま検証コマンドへ渡す) や偽物を信用すると、変異したコードがビルドされずに古い版で緑になる
  if [ "${RUNTIMEOUT:-}" = "$root/src/runtimeout/runtimeout" ] && [ -x "$RUNTIMEOUT" ]; then
    export RUNTIMEOUT
    return 0
  fi
  if ! "$root/bin/runtimeout" 0 true </dev/null >/dev/null; then
    echo "✗ runtimeout をビルド・起動できない ($root/bin/runtimeout。go が無いか、ビルドが落ちた)" >&2
    return 1
  fi
  RUNTIMEOUT="$root/src/runtimeout/runtimeout"
  if [ ! -x "$RUNTIMEOUT" ]; then
    echo "✗ runtimeout のバイナリが無い: $RUNTIMEOUT" >&2
    return 1
  fi
  export RUNTIMEOUT
}
