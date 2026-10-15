package clean

import (
	"errors"
	"os/exec"
)

// import しているが Cmd は作らない (glogx の cli_health.go の形)。Allow に載せない。
func notFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}
