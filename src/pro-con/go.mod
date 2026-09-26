module pro-con

go 1.25.0

require (
	charm.land/bubbletea/v2 v2.0.8
	github.com/BurntSushi/toml v1.6.0
	github.com/charmbracelet/x/ansi v0.11.7
	glogx v0.0.0
	golang.org/x/sys v0.47.0
	process_supervisor v0.0.0
	termsafe v0.0.0
	tuikit v0.0.0
)

require (
	atomicfile v0.0.0 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260803092147-8b693049ce2a // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/sync v0.22.0 // indirect
	subproc v0.0.0 // indirect
)

replace (
	atomicfile => ../atomicfile
	doctor => ../doctor
	glogx => ../glogx
	process_supervisor => ../process_supervisor
	ratelimit => ../ratelimit
	subproc => ../subproc
	termsafe => ../termsafe
	tuikit => ../tuikit
)
