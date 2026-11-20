package visibility

import (
	"ja-writing/internal/pyos"
	"os/user"
	"strconv"
	"syscall"
)

// lookupHomeByUID は pwd.getpwuid(os.getuid()).pw_dir。
func lookupHomeByUID() (string, error) {
	u, err := user.LookupId(strconv.Itoa(syscall.Getuid()))
	if err != nil {
		return "", err
	}
	return pyos.Decode(u.HomeDir), nil
}
