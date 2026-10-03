package runner

import "testing"

// Q で終える途中 (Exiting、強制停止は済んで ready の後片付けを待っている等) に来た Ctrl-C も rc 130 で終える。
// Exiting の model は ForceEvent を素通りするので、rc は actor が書く。
func TestCtrlCWhileExitingAfterQuitUsesSignalCode(t *testing.T) {
	a := &actor{model: Model{State: Exiting, Intent: IntentExit}, presenter: headlessPresenter{}}
	a.beginForce(130)
	if !a.finished || a.retCode != 130 {
		t.Fatalf("finished=%v retCode=%d, want finished with 130", a.finished, a.retCode)
	}
}
