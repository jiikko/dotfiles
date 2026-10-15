package waitdelay

import "os/exec"

func notAllowed() error {
	c := exec.Command("x")
	c.WaitDelay = 2
	return c.Run()
}
