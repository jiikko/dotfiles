module tmpclean

go 1.25.0

require (
	safefs v0.0.0
	subproc v0.0.0
)

require (
	golang.org/x/sys v0.47.0
	proctree v0.0.0
)

replace safefs => ../safefs

replace subproc => ../subproc

replace proctree => ../proctree

replace github.com/jiikko/dotfiles/src/testwait => ../testwait
