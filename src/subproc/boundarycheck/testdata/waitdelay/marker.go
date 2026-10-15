package waitdelay

import "os/exec"

func marker() error {
	return exec.Command("x").Run() // subproc: no-waitdelay — 出力を取らない Run() はパイプを作らない (fixture)
}
