#!/usr/bin/env bash
# src/ の Go が git を起動するときに、subproc.GitArgs (か GitCommand。遅延 fetch を許す GitArgsLazyFetch / GitCommandLazyFetch も可。issue 739) を通していることを pin する (issue 692)。
#
# なぜ: 開いた repo の `.git/config` の `core.fsmonitor=<cmd>` は status のたびにそのコマンドを走らせる。glogx・treefiler・pro-con が
# tarball・zip で配られた「repo」を開くだけで、キー操作なしに走った (git 2.55.0 で実測)。GitArgs が `-c core.fsmonitor=false` を付ける。
# 脅威モデル: うっかりの漏れ (新しく git を起動する所で GitArgs を通し忘れる) を止める。意図的な迂回は対象外。
# 拾う形: `"git"` / `` `git` `` / `"/usr/bin/git"` をコマンド名にした exec.Command(Context)・subproc.CommandContext (間に `)` があっても)。
# GitArgs( は行末のコメントを除いた部分で探す (コメントに書いただけで通さない)。
# 検出しない形: コマンド名を変数・定数で渡す (`exec.Command(gitBin, ...)`)・任意のコマンド名を取る helper (glogx の CommandRunner 等) に "git" を渡す・
# os.StartProcess / syscall・Command( と "git" が別の行にある書き方・GitArgs( をブロックコメントや同じ行の別の文に書く形・
# 差分を出す呼び出しの --no-textconv / --no-ext-diff の付け忘れ
# (字面で決まらない。レビューで見る)
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1

# 行で ( の後の最初の文字列が git (コマンド名) のものだけを拾う (`exec.Command("which", "git")` は起動ではない)
call_re='(exec\.Command(Context)?|subproc\.CommandContext)\([^"`]*["`](/[^"`]*/)?git["`]'
files=0; calls=0; bad=0
while IFS= read -r f; do
  files=$((files + 1))
  while IFS= read -r line; do
    calls=$((calls + 1))
    code=${line%%[[:space:]]//*} # 行末のコメントを除く (空白の後の // だけ。文字列の中の URL の // では切らない)
    case "$code" in
      *GitArgs\(* | *GitArgsLazyFetch\(*) ;;
      *)
        printf '✗ git を GitArgs を通さずに起動している: %s\n' "$f:$line" >&2
        bad=$((bad + 1))
        ;;
    esac
  done < <(grep -nE "$call_re" "$f" || true)
done < <(find src -name '*.go' ! -name '*_test.go' ! -path '*/vendor/*' ! -path 'src/subproc/*')

# 遅延 fetch を許す口 (subproc.GitArgsLazyFetch / GitCommandLazyFetch・pro-con の gitx.RunLazyFetch) は、開いた repo の
# core.sshCommand・remote.<名前>.uploadpack を走らせうる (issue 739)。使ってよいのはキーを押す操作か、自分の管理する repo に打つ操作だけなので、
# 使う場所を下の一覧に固定する (表示の経路をこれに替える誤用は字面では他と区別できない。2 周目のレビュー)。足すときは理由を書いて一覧へ。
# 照合はファイル単位 (: の後ろの関数名は人が読むための注記。一覧のファイルの中で別の関数が使い始めても検出しない)
lazy_allowed='src/glogx/external_commands.go:noPromptGitCmd (push / pull のキー操作)
src/pro-con/monitor/git.go:MergeTree (pro-con が見張る、自分で登録した repo)
src/pro-con/gitx/gitx.go:run (RunLazyFetch の実体)'
lazy_re='(GitArgsLazyFetch|GitCommandLazyFetch|RunLazyFetch)\('
lazy_seen=0
while IFS= read -r hit; do
  f=${hit%%:*}
  code=${hit#*:*:}
  case "$code" in
    func\ *) continue ;; # 定義
  esac
  lazy_seen=$((lazy_seen + 1))
  if ! grep -q "^$f:" <<<"$lazy_allowed"; then
    printf '✗ 遅延 fetch を許す口を一覧に無い場所で使っている (理由を書いて lazy_allowed へ足すか、GitArgs を使う): %s\n' "$hit" >&2
    bad=$((bad + 1))
  fi
done < <(grep -rnE "$lazy_re" src --include='*.go' | grep -v '_test\.go:' | grep -v '^src/subproc/' || true)
if [ "$lazy_seen" -lt 3 ]; then
  printf '✗ 遅延 fetch を許す口の使用が %d 件しか見つからない (正規表現が壊れている)\n' "$lazy_seen" >&2
  exit 1
fi

# 空振り (走査の根や正規表現が壊れて 0 件) を緑にしない
if [ "$files" -lt 100 ]; then
  printf '✗ 走査した Go のファイルが %d 件しかない (根か find が壊れている)\n' "$files" >&2
  exit 1
fi
if [ "$calls" -lt 5 ]; then
  printf '✗ git の起動が %d 件しか見つからない (正規表現が壊れている)\n' "$calls" >&2
  exit 1
fi
if [ "$bad" -gt 0 ]; then
  exit 1
fi
printf '✓ git の起動 %d 件はすべて GitArgs を通す (%d ファイルを走査)。遅延 fetch を許す口 %d 件はすべて一覧の場所\n' "$calls" "$files" "$lazy_seen"
