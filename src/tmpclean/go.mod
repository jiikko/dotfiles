module tmpclean

go 1.25.0

require (
	safefs v0.0.0
	subproc v0.0.0
)

require golang.org/x/sys v0.47.0

replace safefs => ../safefs

replace subproc => ../subproc
