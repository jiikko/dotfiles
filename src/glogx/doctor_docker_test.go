package main

import (
	"context"
	"strings"
	"testing"

	"doctor/docker"
)

// Docker タブに 4 群が並び、docker 申告の回収可能量が見出しに出る。
func TestDoctorDockerTabListsGroups(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	out := doctorText(v, 40)
	for _, want := range []string{"▌Docker", "停止したコンテナ", "使われていないイメージ", "ビルドキャッシュ", "参照されていないボリューム"} {
		if !strings.Contains(out, want) {
			t.Errorf("Docker タブに %q が出ていない:\n%s", want, out)
		}
	}
	// 🚨 見出しに出すのは docker 自身の申告。候補の見積もり (共有レイヤーを重複計上する) ではない
	if !strings.Contains(out, "docker 申告の回収可能 4.5GB") {
		t.Errorf("見出しの数字が docker 申告になっていない:\n%s", out)
	}
	// タブ行にも同じ数字が出る (切り替えずに異常の有無が分かる)
	if !strings.Contains(out, "Docker 4.5GB") {
		t.Errorf("タブ行に Docker の要約が出ていない:\n%s", out)
	}
}

// Enter で内訳 (注記・コマンド・候補) が開く。この画面は docker を実行しないので、
// 出るのは「コピーして自分で叩くコマンド」だけ。
func TestDoctorDockerDetailShowsCommandAndItems(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	_ = doctorText(v, 40)
	for range 20 {
		if v.cur.key == "docker:containers" {
			break
		}
		v.handleKey("j", 40)
		_ = doctorText(v, 40)
	}
	if v.cur.key != "docker:containers" {
		t.Fatalf("コンテナの行へ行けない: %q", v.cur.key)
	}
	v.handleKey("enter", 40)
	out := doctorText(v, 40)
	if !strings.Contains(out, "docker container prune --filter until=336h") {
		t.Errorf("回収コマンドが出ていない:\n%s", out)
	}
	if !strings.Contains(out, "old-web") {
		t.Errorf("候補の内訳が出ていない:\n%s", out)
	}
}

// 🚨 ボリュームは「まとめて消すコマンド」を出さない (docker volume prune -a は戻せない)。
func TestDoctorDockerVolumesHaveNoBulkCommand(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	v.expanded = map[string]bool{"docker:volumes": true}
	out := doctorText(v, 40)
	if strings.Contains(out, "docker volume prune -a\n") {
		t.Errorf("まとめて消すコマンドを提示している:\n%s", out)
	}
	if !strings.Contains(out, "まとめて消すコマンドは出しません") {
		t.Errorf("出さない理由が書かれていない:\n%s", out)
	}
}

// Docker Desktop が入っていない環境ではタブごと出さず、tab の送りも飛ばす。
func TestDoctorDockerTabHiddenWhenNotInstalled(t *testing.T) {
	v := doctorTestView(t)
	v.dockerOpts = noDockerOptions
	runDoctorCmds(t, v, v.open())
	if v.docker == nil || v.docker.Installed {
		t.Fatalf("前提が作れていない: %+v", v.docker)
	}
	out := doctorText(v, 40)
	if strings.Contains(out, "Docker") {
		t.Errorf("入っていないのにタブが出ている:\n%s", out)
	}
	v.tab = tabDisk
	for _, want := range []doctorTab{tabSvc, tabBrew, tabDisk} {
		v.handleKey("tab", 40)
		if v.tab != want {
			t.Fatalf("tab が Docker を飛ばさない: %v (期待 %v)", v.tab, want)
		}
	}
}

// 🚨 診断できなかったときに「候補なし」と同じ見え方にしない (false green)。
func TestDoctorDockerUnavailableIsShown(t *testing.T) {
	v := doctorTestView(t)
	v.dockerOpts = func() docker.Options {
		o := fakeDockerOptions(fakeDockerDF)
		run := o.Run
		o.Run = func(ctx context.Context, name string, args ...string) (string, string, int, error) {
			if len(args) >= 2 && args[0] == "system" && args[1] == "df" {
				return "", "Cannot connect to the Docker daemon", 1, nil
			}
			return run(ctx, name, args...)
		}
		return o
	}
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	out := doctorText(v, 40)
	if !strings.Contains(out, "診断できず") || !strings.Contains(out, "Cannot connect") {
		t.Errorf("診断できなかったことが出ていない:\n%s", out)
	}
	if strings.Contains(out, "🎉") {
		t.Errorf("診断できていないのに祝っている:\n%s", out)
	}
}

// 🚨 記号の表示幅は語ごとに違う ("✅ 安全" と "⛔ 要確認")。揃えないと後ろのラベルが
// 行ごとにずれる (幅の計算では気づけず、人が見るまで分からない類)。
func TestDoctorDockerMarksAreAligned(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	_ = doctorText(v, 40)
	var offsets []int
	for _, row := range v.rows {
		for _, label := range []string{"停止したコンテナ", "使われていないイメージ", "ビルドキャッシュ", "参照されていないボリューム"} {
			if i := strings.Index(row.text, label); i >= 0 {
				offsets = append(offsets, dispWidth(row.text[:i]))
			}
		}
	}
	if len(offsets) != 4 {
		t.Fatalf("群の行が 4 つ見つからない: %v", offsets)
	}
	for _, w := range offsets {
		if w != offsets[0] {
			t.Errorf("ラベルの開始位置が揃っていない: %v", offsets)
		}
	}
}

// 群のまとめコマンドが無いボリュームでも、1 件ずつのコマンドを y でコピーできる
// (選べないと「1 件ずつ選んでください」と言うだけで手段が無い)。
func TestDoctorDockerItemRowCopiesItsCommand(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	v.expanded = map[string]bool{"docker:volumes": true}
	_ = doctorText(v, 40)
	var found *doctorRow
	for i, row := range v.rows {
		if row.key == "dockeritem:volumes:old_data" {
			found = &v.rows[i]
		}
	}
	if found == nil {
		t.Fatalf("ボリュームの行が選べない: %v", rowKeys(v.rows))
	}
	if !found.selectable {
		t.Errorf("行が選べない (選べないと y が届かない)")
	}
	if found.copyPath != "docker volume rm old_data" {
		t.Errorf("y でコピーされるのが %q", found.copyPath)
	}
}

func rowKeys(rows []doctorRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.key != "" {
			out = append(out, r.key)
		}
	}
	return out
}

// 🚨 画面の復元 (doctor_resume.go) は保存されたタブ番号を戻すので、Docker タブを開いたまま
// 閉じ、次の起動で Docker が無い環境になっていると**どの節も返さない空画面**になりうる。
// 走査結果が届いた時点で既定のタブへ戻す。
func TestDoctorDockerTabFallsBackWhenRestoredButNotInstalled(t *testing.T) {
	v := doctorTestView(t)
	v.dockerOpts = noDockerOptions
	v.tab = tabDocker // 前回の画面を復元した状態
	runDoctorCmds(t, v, v.open())
	if v.tab != tabDisk {
		t.Fatalf("消えたタブに留まっている: %v", v.tab)
	}
	if out := doctorText(v, 20); !strings.Contains(out, "▌ディスク占有") {
		t.Errorf("空画面になっている:\n%s", out)
	}
}

// Space で選んで x → 確認画面。実行の相の機械は brew と共有する (jobCmd)。
func TestDoctorDockerSelectAndRun(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	_ = doctorText(v, 40)
	if act := v.handleKey(" ", 40); act != doctorSwallow {
		t.Fatalf("先頭の群を選べない: %v", act)
	}
	if n := v.selectedRunCount(); n != 1 {
		t.Fatalf("選択件数が %d", n)
	}
	if act := v.handleKey("x", 40); act != doctorSwallow {
		t.Fatalf("x が効かない: %v", act)
	}
	if !v.del.confirm || v.del.kind != jobCmd {
		t.Fatalf("確認画面に入っていない: %+v", v.del)
	}
	if len(v.del.cmdPlan) != 1 || v.del.cmdPlan[0].Cmd != "docker container prune --filter until=336h -f" {
		t.Fatalf("実行するコマンドが %+v", v.del.cmdPlan)
	}
	// 打つ前に知っておくことが確認画面に載る
	if !strings.Contains(v.del.cmdPlan[0].Note, "書き込みレイヤー") {
		t.Errorf("注記が %q", v.del.cmdPlan[0].Note)
	}
}

// 🚨 ボリュームはこの画面から消さない。断る理由を出す (無言で何もしない、にしない)。
func TestDoctorDockerVolumesCannotBeSelectedForRun(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	_ = doctorText(v, 40)
	for range 20 {
		if v.cur.key == "docker:volumes" {
			break
		}
		v.handleKey("j", 40)
		_ = doctorText(v, 40)
	}
	if v.cur.key != "docker:volumes" {
		t.Fatalf("ボリュームの行へ行けない: %q", v.cur.key)
	}
	if act := v.handleKey(" ", 40); act != doctorToast {
		t.Fatalf("断っていない: %v", act)
	}
	if !strings.Contains(v.pendingToast, "y でコマンドをコピー") {
		t.Errorf("案内が %q", v.pendingToast)
	}
	if len(v.selectedActions) != 0 {
		t.Errorf("選択されてしまった: %v", v.selectedActions)
	}
}

// 🚨 選択の map は brew と共有している。件数を len(selectedActions) で数えると、
// 別のタブの選択まで数えて hint が嘘になる。
func TestDoctorSelectedRunCountIsPerTab(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	v.tab = tabDocker
	_ = doctorText(v, 40)
	v.handleKey(" ", 40)
	v.tab = tabBrew
	_ = doctorText(v, 40)
	if n := v.selectedRunCount(); n != 0 {
		t.Fatalf("Homebrew タブが Docker の選択を数えている: %d", n)
	}
	v.handleKey("enter", 40) // 警告を開くと手の行が出る
	_ = doctorText(v, 40)
	for range 20 {
		if strings.HasPrefix(v.cur.key, "brewact:") {
			break
		}
		v.handleKey("j", 40)
		_ = doctorText(v, 40)
	}
	if !strings.HasPrefix(v.cur.key, "brewact:") {
		t.Fatalf("brew の手の行へ行けない: %q", v.cur.key)
	}
	v.handleKey(" ", 40) // brew の手を 1 つ選ぶ
	_ = doctorText(v, 40)
	if n := v.selectedRunCount(); n != 1 {
		t.Fatalf("Homebrew タブの件数が %d", n)
	}
	v.tab = tabDocker
	_ = doctorText(v, 40)
	if n := v.selectedRunCount(); n != 1 {
		t.Fatalf("Docker タブの件数が %d (brew の分まで数えている)", n)
	}
}

// ディスク / サービスのタブで x を押したら、どこで押すかを案内する。
func TestDoctorRunKeyOnWrongTabExplains(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	for _, tb := range []doctorTab{tabDisk, tabSvc} {
		v.tab = tb
		_ = doctorText(v, 40)
		if act := v.handleKey("x", 40); act != doctorToast {
			t.Fatalf("tab=%v で x が無言: %v", tb, act)
		}
		if !strings.Contains(v.pendingToast, "Homebrew か Docker") {
			t.Errorf("tab=%v の案内が %q", tb, v.pendingToast)
		}
	}
}
