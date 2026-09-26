module ratelimit

go 1.25.0

require (
	atomicfile v0.0.0
	doctor v0.0.0
	golang.org/x/term v0.45.0
	subproc v0.0.0
	termsafe v0.0.0
	tuikit v0.0.0
)

require (
	github.com/charmbracelet/x/ansi v0.11.7 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/quasilyte/go-ruleguard/dsl v0.3.23
	golang.org/x/sys v0.47.0 // indirect
)

replace atomicfile => ../atomicfile

replace doctor => ../doctor

replace subproc => ../subproc

replace termsafe => ../termsafe

replace tuikit => ../tuikit
