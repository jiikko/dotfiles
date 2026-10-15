package waitdelay

import (
	"context"
	"os/exec"
)

func assign(ctx context.Context) error {
	c := exec.CommandContext(ctx, "gh")
	c.WaitDelay = 2
	_, err := c.Output()
	return err
}
