package main

import (
	"strings"
	"testing"

	"doctor/disk"
)

// TUI 側の配線。disk.RebootNote が正しいことと、doctor の画面がそれを描くことは別の主張。
//
// 🚨 fixture は blocked と ok の**両方**を置く。片方だけだと「blocked のときだけ出す」
// ような退行を検出できず、その形は候補が出ている finder-nsird (ユーザーファイルの
// 可能性がある行) の警告を殺す。
func TestDoctorShowsRebootNoteForTmpdirEntries(t *testing.T) {
	env := disk.Env{Home: "/Users/x", TmpDir: "/var/folders/k2/abc/T"}
	blocked := disk.Entry{ID: "b", Label: "揮発ブロック", Risk: disk.RiskSafe, Recover: "再生成されます",
		DeleteVia: "rm", Paths: []string{"$TMPDIR/x-*"}}
	confirm := disk.Entry{ID: "o", Label: "揮発オーケー", Risk: disk.RiskConfirm, Inspect: true,
		Recover: "元ファイルが残っているか確認してください", DeleteVia: "trash", Paths: []string{"$TMPDIR/y-*"}}
	keep := disk.Entry{ID: "k", Label: "永続する置き場", Risk: disk.RiskSafe, Recover: "再生成されません",
		DeleteVia: "rm", Paths: []string{"/private/var/tmp/w-*"}}
	v := &doctorView{shown: true, diskTotal: 3}
	v.diskRep = &disk.Report{Results: []disk.Result{
		{Entry: blocked, Status: disk.StatusBlocked, Reason: "アプリ起動中のため対象外"},
		{Entry: confirm, Status: disk.StatusOK, Size: 30, Items: []disk.Item{{Path: "/tmpdir/y-1", Size: 30}}},
		{Entry: keep, Status: disk.StatusOK, Size: 20, Items: []disk.Item{{Path: "/private/var/tmp/w-1", Size: 20}}},
	}}
	o := doctorTestOpts(100)
	o.env = env
	out := strings.Join(v.lines(o), "\n")
	if n := strings.Count(out, disk.RebootClearsNote); n != 1 {
		t.Errorf("「消える」注記の数が 1 でない (%d):\n%s", n, out)
	}
	// ユーザーファイルの可能性がある行では含意が逆になる (放置 = 失われる)
	if n := strings.Count(out, disk.RebootLosesNote); n != 1 {
		t.Errorf("「失われる」注記の数が 1 でない (%d):\n%s", n, out)
	}

	// 位置: ラベル行の直後 (助言の後ろに置くと blocked で continue して出ない)。
	// 🚨 ラベルは注記文と部分一致しない語にする (以前「消える置き場」にしていて、
	// 注記文「再起動すると消える置き場です」自身にマッチしていた)
	lines := strings.Split(out, "\n")
	for _, pair := range [][2]string{{"揮発ブロック", disk.RebootClearsNote}, {"揮発オーケー", disk.RebootLosesNote}} {
		idx := -1
		for i, l := range lines {
			if strings.Contains(l, pair[0]) {
				idx = i
				break
			}
		}
		if idx < 0 || idx+1 >= len(lines) || !strings.Contains(lines[idx+1], pair[1]) {
			t.Errorf("%s の直後に注記が無い:\n%s", pair[0], out)
		}
	}
	if !strings.Contains(out, "アプリ起動中のため対象外") {
		t.Errorf("blocked の理由が消えた (注記が理由を押し出している):\n%s", out)
	}

	// 実効 TMPDIR が dirhelper の担当外なら 1 件も出さない (判定は文字列でなく実効値)
	o2 := doctorTestOpts(100)
	o2.env = disk.Env{Home: "/Users/x", TmpDir: "/Users/x/tmp"}
	if out2 := strings.Join(v.lines(o2), "\n"); strings.Contains(out2, disk.RebootClearsNote) || strings.Contains(out2, disk.RebootLosesNote) {
		t.Errorf("TMPDIR が /var/folders の外なのに注記を出した:\n%s", out2)
	}
}

// 狭い幅では注記も切り詰められる (行は幅で切られる)。「切れない場所に置いた」とは
// 主張できないので、**切れても意味が読める前置き**になっているかだけを固定する。
func TestRebootNoteIsTruncatedOnNarrowWidth(t *testing.T) {
	env := disk.Env{Home: "/Users/x", TmpDir: "/var/folders/k2/abc/T"}
	e := disk.Entry{ID: "b", Label: "揮発ブロック", Risk: disk.RiskSafe, Recover: "再生成されます",
		DeleteVia: "rm", Paths: []string{"$TMPDIR/x-*"}}
	v := &doctorView{shown: true, diskTotal: 1}
	v.diskRep = &disk.Report{Results: []disk.Result{{Entry: e, Status: disk.StatusBlocked, Reason: "アプリ起動中のため対象外"}}}
	o := doctorTestOpts(40)
	o.env, o.width = env, 40
	out := strings.Join(v.lines(o), "\n")
	if strings.Contains(out, disk.RebootClearsNote) {
		t.Skip("幅 40 でも切れなかった (この端末幅では検証できない)")
	}
	if !strings.Contains(out, "再起動すると消える") {
		t.Errorf("幅 40 で注記の先頭が読めない (要点が末尾にある文言になっていないか):\n%s", out)
	}
}
