package commitview

import (
	"glogx/internal/gitdata"
	"glogx/internal/testenv"
	"testing"
)

// PR の枠に載るブランチ名は外部由来。git は ref に ASCII 制御文字を許さないが C1 は許すので、
// 8bit の OSC/CSI が実際に入ってくる (直上の Title は無害化していたのにここだけ抜けていた)。
func TestPRStatusBoxSanitizesBranchNames(t *testing.T) {
	o := NewPRStatusOverlay()
	o.sha = "deadbeef"
	o.cache["deadbeef"] = &gitdata.PRStatus{
		PRRef: gitdata.PRRef{Number: 12, State: "OPEN"}, Title: "PR タイトル",
		HeadRefName: "feat/" + testenv.OSC8 + "0;PWNED" + testenv.ST8 + "x",
		BaseRefName: "master" + testenv.CSI8 + "2J",
	}
	testenv.AssertSanitizedAndVisible(t, "PR の枠", o.BoxLines(80, false, "⠋", ""), "feat/", "master")
}
