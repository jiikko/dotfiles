module doctor

go 1.25.0

require (
	github.com/jiikko/dotfiles/src/ctxwait v0.0.0
	github.com/jiikko/dotfiles/src/termsafe v0.0.0
	github.com/jiikko/dotfiles/src/testwait v0.0.0
	golang.org/x/sys v0.47.0
	howett.net/plist v1.0.1
	proctree v0.0.0
	safefs v0.0.0
)

replace github.com/jiikko/dotfiles/src/termsafe => ../termsafe

replace safefs => ../safefs

replace github.com/jiikko/dotfiles/src/testwait => ../testwait

replace proctree => ../proctree

replace github.com/jiikko/dotfiles/src/ctxwait => ../ctxwait
