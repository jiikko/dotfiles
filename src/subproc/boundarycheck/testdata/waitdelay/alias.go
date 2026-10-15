package waitdelay

import xe "os/exec"

func alias() error {
	c := xe.Command("x")
	c.WaitDelay = 2
	return c.Run()
}
