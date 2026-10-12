module doctor

go 1.25.0

require (
	github.com/jiikko/dotfiles/src/termsafe v0.0.0
	golang.org/x/sys v0.47.0
	howett.net/plist v1.0.1
	safefs v0.0.0
)

replace github.com/jiikko/dotfiles/src/termsafe => ../termsafe

replace safefs => ../safefs
