module serialrun

go 1.25.0

require (
	github.com/jiikko/dotfiles/src/testwait v0.0.0
	proctree v0.0.0
	subproc v0.0.0
)

replace (
	proctree => ../proctree
	subproc => ../subproc
)

replace github.com/jiikko/dotfiles/src/testwait => ../testwait
