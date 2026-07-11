#!/usr/bin/env bash

# Vendored perf patch (2026-07-11, VERSIONS.txt 参照):
# 上流はここで tmux クライアントを 8 回 fork していた (bind-key x2 + set-option x4 +
# get_tmux_option x2)。boot 時に同期実行されるため、書き込み系を 1 クライアントに
# バッチして fork を 3 回 (読み 2 + 書き 1) に削減する。設定内容は上流と同一。

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source "$CURRENT_DIR/scripts/variables.sh"
source "$CURRENT_DIR/scripts/helpers.sh"

main() {
	local -a batch=()
	local key

	for key in $(get_tmux_option "$save_option" "$default_save_key"); do
		batch+=(bind-key "$key" run-shell "$CURRENT_DIR/scripts/save.sh" ";")
	done
	for key in $(get_tmux_option "$restore_option" "$default_restore_key"); do
		batch+=(bind-key "$key" run-shell "$CURRENT_DIR/scripts/restore.sh" ";")
	done

	batch+=(set-option -gq "${restore_process_strategy_option}irb" "default_strategy" ";")
	batch+=(set-option -gq "${restore_process_strategy_option}mosh-client" "default_strategy" ";")
	batch+=(set-option -gq "$save_path_option" "$CURRENT_DIR/scripts/save.sh" ";")
	batch+=(set-option -gq "$restore_path_option" "$CURRENT_DIR/scripts/restore.sh")

	tmux "${batch[@]}"
}
main
