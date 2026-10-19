package extcmd

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/foreground"
)

// 前景のプロセスから戻ったときの Msg は起動元を載せる (載せないと、F・L のシェルから戻ったときに「エディタが異常終了しました」へ流れる)。
func TestClosedMsgCarriesOrigin(t *testing.T) {
	x := foreground.NewExec(exec.Command("true")) // subproc: no-waitdelay — 起動しない (Msg を組むだけ)
	want := errors.New("exit status 1")
	for _, o := range []ExecOrigin{OriginEditor, OriginFiler, OriginLan} {
		msg, ok := closedMsg(o, x)(want).(EditorClosedMsg)
		if !ok || msg.Origin != o || !errors.Is(msg.Err, want) {
			t.Errorf("origin %d: msg = %+v", o, msg)
		}
	}
}
