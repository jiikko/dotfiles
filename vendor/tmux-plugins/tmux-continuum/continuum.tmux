#!/usr/bin/env bash

# Vendored patches (VERSIONS.txt 参照):
# - upstream had `set -x` enabled; disable to avoid noisy debug output.
# - perf (2026-07-11): 本スクリプトは boot 時に同期実行され、上流実装は tmux クライアント
#   fork とサブスクリプト起動を十数回繰り返していた (実測 ~145ms/ロード)。以下で削減する:
#     1. 読み取り (version / start_time / @continuum-* / status-right / status-left) を
#        1 クライアントに集約 (display-message と show-option の \; 連結)
#     2. check_tmux_version.sh を同一ロジック (数字だけ抽出して整数比較) でインライン化
#     3. handle_tmux_automatic_start.sh の common path (@continuum-boot=off) をインライン化
#        (on の稀パスは従来どおりスクリプトへ委譲)
#     4. status-right/left の read-modify-write 往復を bash 内合成 + 末尾 1 回の書き込みへ
#   ガード分岐 (version / automatic-start / 他サーバ / 初回 restore) の判定意味と実行順序は
#   上流のまま。ps ベースの多重サーバ検出はデータ上書き防止の要なので手を入れていない。

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source "$CURRENT_DIR/scripts/helpers.sh"
source "$CURRENT_DIR/scripts/variables.sh"
source "$CURRENT_DIR/scripts/shared.sh"

save_command_interpolation="#($CURRENT_DIR/scripts/continuum_save.sh)"

# 集約読み取りの結果 (main 冒頭で 1 回だけ埋める)
_ver='' _start_time='' _boot_opt='' _restore_max_delay='' _last_save=''
_status_right='' _status_left=''

read_server_state() {
	# display-message -F は user option (#{@x}) と組み込み値を安全に読める。
	# ⚠️ display-message は出力中の改行を "_" に置換するため複数行 format は使えない
	#   (実測でハマった)。スカラー値 (version/epoch/on-off) は ";" 区切りの 1 行で読む
	#   (これらの値に ";" は現れない)。status-right/left は値に任意の format 文字列を
	#   含むため区切り文字が衝突しうる。show-option -gqv の行で読む (グローバル
	#   オプションは常に値を持つので行ズレしない)。
	{
		IFS=';' read -r _ver _start_time _boot_opt _restore_max_delay _last_save
		IFS= read -r _status_right
		IFS= read -r _status_left
	} < <(tmux display-message -p -F '#{version};#{start_time};#{@continuum-boot};#{@continuum-restore-max-delay};#{@continuum-save-last-timestamp}' \; \
	           show-option -gqv "status-right" \; \
	           show-option -gqv "status-left")
}

# check_tmux_version.sh と同一の判定 (数字以外を落として整数比較)
supported_tmux_version_ok() {
	local cur="${_ver//[^0-9]/}" sup="${SUPPORTED_VERSION//[^0-9]/}"
	[ -n "$cur" ] && [ "$cur" -ge "$sup" ]
}

# handle_tmux_automatic_start.sh の common path (off) インライン。
# on は launchd/systemd の登録を伴う稀パスなので従来スクリプトへ委譲する。
handle_tmux_automatic_start() {
	if [ "${_boot_opt:-off}" == "on" ]; then
		"$CURRENT_DIR/scripts/handle_tmux_automatic_start.sh"
	elif [ "$(uname)" == "Darwin" ]; then
		rm "$osx_auto_start_file_path" > /dev/null 2>&1
	elif [ "$(ps -o comm= -p1)" == "systemd" ]; then
		"$CURRENT_DIR/scripts/handle_tmux_automatic_start/systemd_disable.sh"
	fi
}

another_tmux_server_running() {
	if just_started_tmux_server; then
		another_tmux_server_running_on_startup
	else
		# script loaded after tmux server start can have multiple clients attached
		[ "$(number_tmux_processes_except_current_server)" -gt "$(number_current_server_client_processes)" ]
	fi
}

just_started_tmux_server() {
	local restore_max_delay="${_restore_max_delay:-$auto_restore_max_delay_default}"
	[ "$_start_time" == "" ] || [ "$_start_time" -gt "$(($(date +%s)-${restore_max_delay}))" ]
}

start_auto_restore_in_background() {
	"$CURRENT_DIR/scripts/continuum_restore.sh" &
}

main() {
	read_server_state
	if supported_tmux_version_ok; then
		handle_tmux_automatic_start

		local -a batch=()

		# Advanced edge case handling: start auto-saving only if this is the
		# only tmux server. We don't want saved files from more environments to
		# overwrite each other.
		if ! another_tmux_server_running; then
			# give user a chance to restore previously saved session
			# (= delay_saving_environment_on_first_plugin_load)
			if [ -z "$_last_save" ]; then
				# last save option not set, this is first time plugin load
				batch+=(set-option -gq "$last_auto_save_option" "$(current_timestamp)" ";")
			fi
			# add_resurrect_save_interpolation (check interpolation not already added)
			if ! [[ "$_status_right" == *"$save_command_interpolation"* ]]; then
				_status_right="${save_command_interpolation}${_status_right}"
			fi
		fi

		if just_started_tmux_server; then
			start_auto_restore_in_background
		fi

		# Put "#{continuum_status}" interpolation in status-right or
		# status-left tmux option to get current tmux continuum status.
		# (= update_tmux_option: replace interpolation string with a script to execute)
		batch+=(set-option -gq "status-right" "${_status_right/$status_interpolation_string/$status_script}" ";")
		batch+=(set-option -gq "status-left" "${_status_left/$status_interpolation_string/$status_script}")
		tmux "${batch[@]}"
	fi
}
main
