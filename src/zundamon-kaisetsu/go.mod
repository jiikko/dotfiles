module zundamon-kaisetsu

go 1.25.0

require (
	github.com/jiikko/dotfiles/src/ctxwait v0.0.0
	github.com/jiikko/dotfiles/src/testwait v0.0.0
	proctree v0.0.0
)

replace proctree => ../proctree

replace github.com/jiikko/dotfiles/src/testwait => ../testwait

replace github.com/jiikko/dotfiles/src/ctxwait => ../ctxwait
