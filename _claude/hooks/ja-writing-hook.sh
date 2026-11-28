#!/usr/bin/env bash
# UserPromptExpansion / UserPromptSubmit / PreToolUse(Bash・mcp) / Stop / SessionEnd フック: `ja-writing hook --host claude-code` を起こす
# (本家 yomiyasu_hook.py の Go の写し。src/ja-writing の README の「hook」)。
# `/yomiyasu <原稿>` か `ja-writing check start` で登録した原稿だけを見て、返答の終わりに最終稿が検査記録と合うかを確かめ、
# 合わなければ作業を続けさせる。登録した原稿に関係する gh の投稿で、本文が検査記録と照合できないときは止める。登録が無ければ何もしない。
#
# 🚨 **ビルドしない** (go_autobuild_exec --no-build)。ビルドが無い・古いときは何もせず rc 0 (bin/ja-writing を打つとビルドする。何もビルドされていない木では 2 回打つ:
#   go:embed を持つ module の最初のビルドは、次の起動で 1 回作り直す印を付ける (bin/lib/go_autobuild.zsh の _go_autobuild_build))。
#   同期でビルドすると、src を変えた直後の最初のイベントが数十秒止まる。--async にしない: 古いバイナリが今の登録の形を読み違える。
# 🚨 **rc はいつも 0**。hook 自身も、知らない引数・入力の誤り・内部の誤りで 0 を返す (止めるのは標準出力の JSON だけ)。
# 時間の上限は hook 自身が持つ (イベントごとの締め切り。settings の timeout は本家の --print-config と同じ値)。
set -uo pipefail
unset CDPATH
self=$(readlink -f "$0" 2>/dev/null) || exit 0
repo=$(cd "$(dirname "$self")/../.." && pwd -P) || exit 0
# shellcheck disable=SC2016  # 単一引用符の中は zsh -c のプログラム ($1 は zsh の引数)
zsh -c 'source "$1"; shift; go_autobuild_exec --no-build "$1" ja-writing -- "${@:2}"' \
  _ "$repo/bin/lib/go_autobuild.zsh" "$repo/src/ja-writing" hook --host claude-code 2>/dev/null
exit 0
