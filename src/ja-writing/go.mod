module ja-writing

go 1.25.0

require (
	atomicfile v0.0.0
	subproc v0.0.0
)

require (
	github.com/dlclark/regexp2/v2 v2.5.2
	github.com/jiikko/dotfiles/src/testwait v0.0.0
)

replace atomicfile => ../atomicfile

replace subproc => ../subproc

replace github.com/jiikko/dotfiles/src/testwait => ../testwait
