package main

import "os/exec"

// SkipDirs で飛ばすディレクトリ (走査されれば offender になる)。
func main() { _, _ = exec.Command("gh").Output() }
