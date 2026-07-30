get_tmux_option() {
	local option="$1"
	local default_value="$2"
	local option_value=$(tmux show-option -gqv "$option")
	if [ -z "$option_value" ]; then
		echo "$default_value"
	else
		echo "$option_value"
	fi
}

set_tmux_option() {
	local option="$1"
	local value="$2"
	tmux set-option -gq "$option" "$value"
}

# multiple tmux server detection helpers

current_tmux_server_pid() {
	echo "$TMUX" |
		cut -f2 -d","
}

all_tmux_processes() {
	# ignores `tmux source-file .tmux.conf` command used to reload tmux.conf
	local user_id=$(id -u)
	ps -u $user_id -o "command pid" |
		\grep "^tmux" |
		\grep -v "^tmux source"
}

number_tmux_processes_except_current_server() {
	all_tmux_processes |
		\grep -v " $(current_tmux_server_pid)$" |
		wc -l |
		sed "s/ //g"
}

number_current_server_client_processes() {
	tmux list-clients |
		wc -l |
		sed "s/ //g"
}

# ── Vendored patch (2026-07-30, dotfiles) ──────────────────────────────────────
# 多重サーバ検出を「ps の ^tmux カウント」から「自分が default socket のサーバか」に置換。
# 上流の目的 (別環境の autosave/auto-restore が保存を壊し合うのを防ぐ) は保ったまま、
# -L / TMUX_TMPDIR で隔離されたテスト・検証サーバの残骸プロセスが存在するだけで default
# サーバの autosave interpolation 導入と auto-restore が恒久 skip される誤爆を排除する
# (実害: 2026-06-11〜28 の 17 日間復元不発、2026-07-30 再発 + 周期保存も不発と判明)。
# 判定式は scripts/lib/tmux_resurrect_guards.sh の tt_on_default_server と同一に保つこと
# (canonical /tmp 基準 + realpath。macOS の /tmp は /private/tmp への symlink で
#  #{socket_path} は解決済みパスを返すため期待値側も realpath で解決する)。
# default socket 以外のサーバでは常に「他サーバあり」を返す = テストサーバは autosave も
# auto-restore もしない。これは上流挙動より安全側 (テストサーバが -f _tmux.conf で本 plugin
# を load しても HOME 共有の保存を触らない)。
# 上流の ps カウント系 helper (all_tmux_processes 等) は最小 diff のため残置 (未使用)。
on_default_socket_server() {
	local actual expected
	actual="$(tmux display-message -p '#{socket_path}' 2>/dev/null)"
	# 取れない環境 (古い tmux / テストスタブ) は fail-open で上流既定 (単一サーバ扱い) に寄せる
	[ -n "$actual" ] || return 0
	expected="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"
	[ "$actual" = "$expected" ]
}

another_tmux_server_running_on_startup() {
	# upstream: [ "$(number_tmux_processes_except_current_server)" -gt 1 ]
	! on_default_socket_server
}
