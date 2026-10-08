# shellcheck shell=bash
# go_tool.sh — src/<module> の Go の道具のバイナリを、スクリプトの起動時に 1 回だけ解決する。bash / zsh の両方から source する。
#
# go_tool_resolve <変数名> <repo root> <src の module 名> <バイナリ名> [<ビルドを済ませるための引数>…]:
#   <変数名> にバイナリの絶対パス ($root/src/<module>/<バイナリ名>) を入れて export する。
#   既にその変数にこの checkout のバイナリが入っていれば、それを使う (PATH を絞った子へ渡す形)。
#   無ければラッパー (bin/<バイナリ名>) を引数つきで 1 回通してビルドを済ませる。ビルド・起動できなければ rc=1。
#   引数は「何もせず rc=0 で終わる」呼び方にする (runtimeout は `0 true`、codex-events は `help`)。
#
# 🚨 ラッパーを毎回通さず、最初に 1 回だけ解決する。ラッパーは古ければ同期ビルドするので、
#   - ビルドの失敗 (rc=1) が道具の rc=1 と見分けられない (変異検証の red と読みうる / codex の run の判定が失敗に落ちる)
#   - ビルドの進捗が片方の出力にだけ混ざる / 1 回目のビルドの待ちが上限を食う
#   - PATH を絞った環境 (tests/bin/test_claude_bin.sh) では zsh / go が見つからない
# 🚨 テストは PATH・HOME・偽の go を差し替える**前**に呼ぶ (HOME を隔離すると GOCACHE が空になり、ビルドが cold になる)。
go_tool_resolve() {
  # 🚨 変数名に path を使わない (zsh では PATH と結び付いた配列で、local にすると PATH が空になる)
  local var="$1" root_in="$2" module="$3" name="$4" root bin_path cur
  shift 4
  # 表記を揃える (`bin/..`・symlink)。揃えないと同じバイナリを「別物」と読んでラッパーを通し直す。CDPATH / chpwd の出力を混ぜない
  root="$(CDPATH='' cd -- "$root_in" >/dev/null && pwd -P)" || { echo "✗ go_tool_resolve: repo root に入れない: $root_in" >&2; return 1; }
  bin_path="$root/src/$module/$name"
  # 🚨 既にある値は、この checkout のバイナリを指しているときだけ使う。別の checkout の版 (bin/mutate-verify が
  #    export したまま検証コマンドへ渡す) や偽物を信用すると、変異したコードがビルドされずに古い版で緑になる
  eval "cur=\${$var:-}"
  if [ "$cur" = "$bin_path" ] && [ -x "$cur" ]; then
    export "${var?}"
    return 0
  fi
  if ! "$root/bin/$name" "$@" </dev/null >/dev/null; then
    echo "✗ ${name} をビルド・起動できない (${root}/bin/${name}。go が無いか、ビルドが落ちた)" >&2
    return 1
  fi
  if [ ! -x "$bin_path" ]; then
    echo "✗ ${name} のバイナリが無い: ${bin_path}" >&2
    return 1
  fi
  eval "$var=\$bin_path"
  export "${var?}"
}
