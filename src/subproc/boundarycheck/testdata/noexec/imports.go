package noexec

import (
	"fmt"
	"os/exec"
)

func imports() { fmt.Println(exec.ErrNotFound) }
