package waitdelay

import "os/exec"

func missing() ([]byte, error) {
	c := exec.Command("gh")
	// 理由は subproc.WaitDelay の doc を見ること (語がコメントに出るだけでは通さない。issue 303)
	return c.Output()
}
