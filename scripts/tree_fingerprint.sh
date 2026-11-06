#!/usr/bin/env bash
# 作業ツリーの指紋 (HEAD・変更と untracked の一覧・その中身の hash) を取り、検査の前後で比べる。`make test` が使う。
#
#   tree_fingerprint.sh print           → 指紋を 1 行で出す (rc=0)。git の repo でない・git が失敗したら rc=2
#   tree_fingerprint.sh check <前の指紋> → 今の指紋と比べる。同じなら rc=0、違えば理由を出して rc=1、比べられなければ理由を出して rc=2
#
# なぜ: 背景の検査が走っている同じ worktree を編集すると、結果はどの木も検査していないのに緑 / 赤として読まれる。
# 規範 (verify-execution-not-just-exit-code.md) だけでは止まらなかったので、結果を読むときに見える形で出す。
#
# 🚨 検出しない形: 走っている間に変えて元に戻した編集 (前後の指紋が同じ)・読めないファイルの、大きさを変えない書き換え・ignore されたファイルと tmp/ の下 (テストの出力の置き場。
# CI の checkout では tmp/ が ignore されないので、ignore に頼らず除く)・HEAD を動かさない ref の変更 (fetch)・submodule の中身。
# 指紋は変更のあるファイルだけを hash するので、変更の無いファイルの数に依らず速い。
set -uo pipefail

fingerprint() {
  local head status paths
  head=$(git rev-parse -q --verify HEAD 2>/dev/null) || head=none
  # -z の NUL は変数に入れる前に改行へ替える (コマンド置換は NUL を黙って捨て、パスが 1 つに繋がる)
  status=$(git status --porcelain=v1 -z --untracked-files=all -- . ':(exclude)tmp' 2>/dev/null | tr '\0' '\n') || return 2
  paths=$(status_paths "$status")
  {
    printf 'HEAD %s\n' "$head"
    printf '%s\n' "$status"
    # 変更・untracked の各パスの属性: 同じ一覧のまま mode だけを変えた編集を見分ける。大きさも入れる (読めないファイルは中身の hash が
    # 取れないので、中身の変化をこちらで拾う)。更新時刻は入れない: 中身を変えない保存し直しで赤にしない。symlink は指す先で比べる。
    # 判定を `[ … ] && printf` にしない: このループが { } の最後の文になると、最後のパスが偽 (消えた・dir) のときにループが rc=1 を返し、
    # pipefail で指紋の取得の失敗 (比べられない) に化ける
    printf '%s\n' "$paths" | while IFS= read -r p; do
      if [ -L "$p" ]; then
        printf 'L %s %s\n' "$(readlink "$p")" "$p"
      elif [ -f "$p" ]; then
        printf 'F %s %s\n' "$(stat -f '%p %z' "$p")" "$p"
      fi
    done
    # 中身: 読める普通のファイルだけを、上と同じ順に 1 プロセスで hash する (読めないパスを渡すと hash-object がそこで止まり、
    # 後ろのファイルの中身の比べが黙って消える。読めないファイルは上の属性で比べる)
    printf '%s\n' "$paths" | while IFS= read -r p; do
      if [ -f "$p" ] && [ ! -L "$p" ] && [ -r "$p" ]; then printf '%s\n' "$p"; fi
    done | git hash-object --stdin-paths 2>/dev/null || :
  } | shasum -a 256 | cut -d' ' -f1
}

# status_paths は git status --porcelain=v1 -z (NUL を改行にしたもの) の各エントリのパス。rename / copy (R / C) の次の行は
# 旧パスで状態の 2 文字を持たないので、パスとして読み飛ばす (先頭の 3 文字を削ると別のパスになる)。
status_paths() {
  printf '%s\n' "$1" | LC_ALL=C awk '
    skip { skip = 0; next }
    length($0) < 4 { next }
    { st = substr($0, 1, 2); print substr($0, 4); if (st ~ /[RC]/) skip = 1 }'
}

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "tree_fingerprint: git の作業ツリーの外 ($(pwd))" >&2; exit 2; }
# status のパスは cwd に関係なく repo の root からの相対なので、root で読む (サブディレクトリからだと中身と属性の比べが黙って消える)
cd "$(git rev-parse --show-toplevel)" || exit 2

case "${1:-}" in
  print)
    fingerprint || exit 2
    ;;
  check)
    before="${2:-}"
    if [ -z "$before" ]; then
      echo "🚨 検査の前に作業ツリーの指紋を取れなかったので、走っている間に作業ツリーが変わったかを確かめていない" >&2
      exit 2
    fi
    after=$(fingerprint) || { echo "🚨 検査の後に作業ツリーの指紋を取れなかったので、走っている間に作業ツリーが変わったかを確かめていない" >&2; exit 2; }
    if [ "$before" != "$after" ]; then
      echo "✗ 検査が走っている間に作業ツリー ($(pwd)) が変わった (HEAD・変更・untracked の一覧か中身。tmp/ と ignore されたものは見ない)。" >&2
      echo "  この結果は、どの時点の木も検査していない。変更を止めてから回し直すこと (verify-execution-not-just-exit-code.md)" >&2
      exit 1
    fi
    ;;
  *)
    echo "使い方: tree_fingerprint.sh print | check <前の指紋>" >&2
    exit 64
    ;;
esac
