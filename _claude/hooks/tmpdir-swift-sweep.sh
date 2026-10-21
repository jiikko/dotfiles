#!/bin/bash
# SessionStart フック: $TMPDIR の直下に Swift のツールチェーンが残す空の TemporaryDirectory.* を、1 日 1 回、裏で消す
# (`tmpclean sweep`。何を消してよいかの判定は全部 tmpclean 側)。セッションの起動を待たせないため、すぐ返り、
# 何もコンテキストへ出さない。結果は ~/.cache/dotfiles-tmpdir-sweep/ の sweep.log (sweep 1 回に `rc=` の 1 行。lockman の出力 (lock を
# 取れなかった行・初回のビルド) も入る) と last.json / last.err。
#
# 排他: 同時に開いたセッションが同時に走らせないよう、lockman で 1 本にする (取れなければ何もしない)。「1 日 1 回」の判定は
# lock の中でもう一度見る (lock の外の判定だけだと、同時に起動した 2 本が両方とも「まだ」と読む)。
#
#   tmpdir-swift-sweep.sh          # hook として: 走らせる日なら裏で --run を起こして返る
#   tmpdir-swift-sweep.sh --run    # lock の中で: もう一度日付を見て sweep を走らせ、記録する
set -u
state=${TMPDIR_SWEEP_STATE:-${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles-tmpdir-sweep} # テストは差し替える
dotfiles=${DOTFILES_DIR:-$HOME/dotfiles}
tmpclean=${TMPDIR_SWEEP_TMPCLEAN:-$dotfiles/bin/tmpclean}
lockman=${TMPDIR_SWEEP_LOCKMAN:-$dotfiles/bin/lockman}
runtimeout=${TMPDIR_SWEEP_RUNTIMEOUT:-$dotfiles/bin/runtimeout}
interval_min=${TMPDIR_SWEEP_INTERVAL_MIN:-1440}

# due は前回 (sweep が判定まで走った回) から interval_min 分を過ぎたか。印が無ければ走らせる
due() {
	[ ! -e "$state/stamp" ] || [ -z "$(find "$state/stamp" -mmin "-$interval_min" 2>/dev/null)" ]
}

if [ "${1:-}" = "--run" ]; then
	due || exit 0
	# 記録は伸び続けないように、大きくなったら後ろだけ残す (lock を取れなかった行は毎セッション足されうる)。
	# 🚨 mv で別のファイルに差し替えない: 既に >> で開いている書き手 (この run を起こした lockman の stderr・同時に起きた hook) が、
	# 名前の無くなった古いファイルへ書き続け、その行が記録から消える。同じファイルのまま中身を書き戻す
	if [ -n "$(find "$state/sweep.log" -size +256k 2>/dev/null)" ]; then
		tail -n 500 "$state/sweep.log" > "$state/sweep.log.tmp" && cat "$state/sweep.log.tmp" > "$state/sweep.log"
		rm -f "$state/sweep.log.tmp"
	fi
	"$runtimeout" 1800 "$tmpclean" sweep -json > "$state/last.json" 2> "$state/last.err"
	rc=$?
	removed=$(grep -c '^    "/' "$state/last.json" 2>/dev/null) # "removed" の配列の要素 (writeJSON の 2 字の字下げで 2 段目)
	printf '%s rc=%s removed=%s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$rc" "${removed:-?}" >> "$state/sweep.log"
	# 0 = 走り切った / 1 = 消せなかったものがある / 2 = 判定不能で止めた (何も消していない)。どれも今日の分は済んだとする
	# (判定不能をセッションのたびに再試行すると、lsof の全体を毎回取る)。3 以上 (実行環境の失敗・時間切れ) は次のセッションで再試行する
	case $rc in 0 | 1 | 2) touch "$state/stamp" ;; esac
	exit "$rc"
fi

cat > /dev/null # hook の JSON は使わない (読まずに返すと、書き手が SIGPIPE を受けうる)
mkdir -p "$state/lock" || exit 0
due || exit 0
# 裏で走らせ、hook は待たない。setsid で別のセッションにする (hook の process group ごと止められても巻き込まれない)
/usr/bin/perl -MPOSIX -e 'POSIX::setsid(); exec @ARGV or exit 127' -- \
	"$lockman" with "$state/lock" --ttl 1h --label "tmpdir-swift-sweep" -- "$0" --run \
	< /dev/null >> "$state/sweep.log" 2>&1 &
exit 0
