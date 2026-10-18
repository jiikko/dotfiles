package tools

import "os/exec"

// SkipDirs の tools は Root の直下だけを外す。深い位置の同じ名前のディレクトリは走査する (名前で外さない)
func deep() ([]byte, error) {
	c := exec.Command("gh")
	return c.Output()
}
