#!/usr/bin/env bash
# go_env_trimpath.sh — go env のファイルの GOFLAGS に -trimpath を足す (setup.sh から呼ぶ)。
#
# なぜ: -trimpath が無いと、コンパイル結果のキャッシュの鍵に package のディレクトリの絶対パスが入り
# (cmd/go/internal/work/exec.go の buildActionID)、同じソースでも worktree ごとに別の項目として GOCACHE に溜まる。
# 2026-10-17 の実測: 同じ commit の 2 つ目の worktree の増分 +54.7 MB → -trimpath で +0.5 MB。
#
# 規則:
#   - -trimpath が**どの形でも**既にあれば何もしない (-trimpath / --trimpath / -trimpath=true / -trimpath=false)。
#     利用者が明示した -trimpath=false を、後ろに -trimpath を足して上書きしない (go は GOFLAGS を順に適用し、後が勝つ)
#   - GOFLAGS の値は Go と同じ規則で語に割る (cmd/internal/quoted.Split: 空白で区切り、'…' / "…" で囲んだ所は 1 語。
#     エスケープは無い)。素の部分文字列で探すと、'-gcflags=all=-trimpath /old=>/new' のような別のフラグの引数を取り違える
#   - 読むときは GOFLAGS='' を付けて**ファイルの値**を読む (親の環境変数 GOFLAGS をファイルへ写さない)。
#     書くのは go env -w (書き先は GOENV。未設定なら ~/Library/Application Support/go/env)
#   - 外すときはそのコマンドに -trimpath=false を付ける。GOFLAGS= (空) では外れない (空なら go env のファイルの値に戻る)
#
# 終了コード: 0 = 足した / 既にあった、1 = go env の読み書きに失敗、2 = GOFLAGS の引用符が閉じていない (触らない)
set -uo pipefail

flags=$(GOFLAGS='' go env GOFLAGS) || { echo "go env GOFLAGS を読めない" >&2; exit 1; }

# split_goflags は cmd/internal/quoted.Split と同じ規則で 1 語ずつ出力する (引用符は外す)
split_goflags() {
	local s=$1 i=0 n=${#1} c q word
	while [ "$i" -lt "$n" ]; do
		c=${s:i:1}
		case "$c" in
		' ' | $'\t' | $'\n' | $'\r') i=$((i + 1)); continue ;;
		"'" | '"')
			q=$c
			i=$((i + 1))
			word=
			while [ "$i" -lt "$n" ] && [ "${s:i:1}" != "$q" ]; do
				word+=${s:i:1}
				i=$((i + 1))
			done
			[ "$i" -lt "$n" ] || return 2 # 引用符が閉じていない
			i=$((i + 1))
			printf '%s\n' "$word"
			;;
		*)
			word=
			while [ "$i" -lt "$n" ]; do
				c=${s:i:1}
				case "$c" in ' ' | $'\t' | $'\n' | $'\r') break ;; esac
				word+=$c
				i=$((i + 1))
			done
			printf '%s\n' "$word"
			;;
		esac
	done
}

words=$(split_goflags "$flags") || { echo "GOFLAGS の引用符が閉じていないので触らない: $flags" >&2; exit 2; }
while IFS= read -r w; do
	case "$w" in
	-trimpath | --trimpath | -trimpath=* | --trimpath=*) exit 0 ;;
	esac
done <<<"$words"

go env -w GOFLAGS="${flags:+$flags }-trimpath" || { echo "go env -w GOFLAGS に失敗した" >&2; exit 1; }
