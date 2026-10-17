package doctorview

import (
	"testing"

	"doctor/docker"
)

// 🚨 実行可否の出典は走査側の Confirm 1 つ (Kind から導き直さない)。
// 走査側が Confirm を立てた群は、Kind が何であれ実行できない。
func TestDoctorDockerRunnableFollowsConfirmNotKind(t *testing.T) {
	// Kind は実行できる群のものだが、走査側が「戻らない」と言っている
	if dockerRunnable(docker.Group{Kind: docker.KindImages, Confirm: true}) {
		t.Errorf("Confirm を無視して Kind で判定している")
	}
	if !dockerRunnable(docker.Group{Kind: docker.KindVolumes}) {
		t.Errorf("Confirm が立っていない群を実行不可にしている")
	}
}
