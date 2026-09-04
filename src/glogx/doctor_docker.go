package main

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"doctor/docker"
	"doctor/runner"
)

// doctor の Docker タブ (ユーザー要望 2026-09-04)。
//
// 出すのは 4 群 (停止コンテナ / 使われていないイメージ / ビルドキャッシュ / 参照されていない
// ボリューム) と、それぞれを回収するコマンド。**この画面から docker は実行しない** —
// disk の d のような削除の導線を持たない。prune は取り消せず、ボリュームはユーザーデータ
// そのものになりうるので、判断と実行は人が行う (doctor/docker の README と同じ線)。
//
// 🚨 **Docker Desktop が無い環境ではタブごと出さない**。空のタブを置くと、tab を送るたびに
// 「何も無いタブ」を通ることになる。走査中は「入っているか」がまだ分からないので出しておく
// (docker の走査は実測 0.3 秒なので、消えるまでの窓は短い)。
//
// 🚨 **結果はキャッシュに載せない** (disk / svc / brew と違う)。走査が速いので開くたびに
// 取り直せばよく、保存すると復元経路の無害化・カタログ突合を新設することになる。
// snapshot を復元する経路でも docker だけは走らせる (start を参照)。

type doctorDockerMsg struct {
	gen int
	rep docker.Report
}

// dockerCmd は docker の走査を 1 本の Cmd にする。
func (v *doctorView) dockerCmd(ctx context.Context, gen int) tea.Cmd {
	opt := docker.Options{Run: runner.Exec}
	if v.dockerOpts != nil {
		opt = v.dockerOpts()
	}
	return func() tea.Msg {
		var rep docker.Report
		doctorTrack(func() { rep = docker.Scan(ctx, opt) })
		return doctorDockerMsg{gen: gen, rep: rep}
	}
}

func (v *doctorView) receiveDocker(msg doctorDockerMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	rep := msg.rep
	v.docker = &rep
	// タブが消えることがあるので、今いるタブが消えたら先頭へ戻す
	// (消えたタブに留まると、buildRows がどの節も返さず空画面になる)
	if !v.tabVisible(v.tab) {
		v.tab = tabDisk
	}
}

// tabVisible は「そのタブを出すか」。Docker だけが条件つき。
func (v *doctorView) tabVisible(t doctorTab) bool {
	if t != tabDocker {
		return true
	}
	// 走査中 (nil) は出す。入っていないと**分かった**ときだけ消す
	return v.docker == nil || v.docker.Installed
}

// dockerMarkWidth は記号列の幅。🚨 **記号は語ごとに表示幅が違う** ("✅ 安全" と "⛔ 要確認") ので、
// 揃えないと後ろのラベルが行ごとにずれる。disk 側の doctorMaxMarkWidth と同じ扱い。
func dockerMarkWidth() int {
	w := 0
	for _, k := range []docker.Kind{docker.KindContainers, docker.KindImages, docker.KindBuildCache, docker.KindVolumes} {
		w = max(w, dispWidth(dockerMark(k)))
	}
	return w
}

// dockerMark は群のリスク記号。語彙は disk.Mark と揃える (同じ画面で 2 つの語彙を持たない)。
// 🚨 exhaustive (default なし) なので、docker.Kind が増えたらここが compile error になる。
func dockerMark(k docker.Kind) string {
	switch k {
	case docker.KindContainers:
		return "🚨 注意" // 書き込みレイヤーに作業結果が残っていることがある
	case docker.KindImages:
		return "🚨 注意" // 消すと再取得に通信が要る
	case docker.KindBuildCache:
		return "✅ 安全" // 次のビルドが遅くなるだけ
	case docker.KindVolumes:
		return "⛔ 要確認" // 中身はデータ。戻せない
	}
	return string(k)
}

func (v *doctorView) dockerSection(o doctorRenderOpts) []doctorRow {
	if v.docker == nil {
		return sectionHeader(o, "Docker", o.spinner+" docker system df を実行中")
	}
	d := v.docker
	if d.Unavailable != "" {
		return append(sectionHeader(o, "Docker", "診断できず"),
			doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 "+d.Unavailable)})
	}
	rows := sectionHeader(o, "Docker", fmt.Sprintf("docker 申告の回収可能 %s (Enter で内訳)",
		docker.HumanSize(d.DockerReclaimable())))
	for _, n := range d.Notes {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 "+n)})
	}
	for _, g := range d.Groups {
		rows = append(rows, v.dockerGroupRow(o, g))
	}
	rows = append(rows,
		doctorRow{text: ""},
		doctorRow{text: doctorColor(o.colored, ansiDim, "   まとめて回収するなら:")},
		doctorRow{
			text:       "   ▸ " + doctorColor(o.colored, ansiBold, d.SystemPrune),
			selectable: true,
			key:        "dockerprune",
			detail: textRows([]string{
				doctorColor(o.colored, ansiYellow, "     🚨 "+d.SystemPruneNote),
				doctorColor(o.colored, ansiDim, "     この画面は docker を実行しません (コピーして自分で叩いてください)"),
			}),
			copyPath: d.SystemPrune,
			copyText: d.SystemPrune + "\n# " + d.SystemPruneNote + "\n",
		})
	return rows
}

// dockerGroupRow は群 1 つの行 (Enter で注記と内訳を開く)。
func (v *doctorView) dockerGroupRow(o doctorRenderOpts, g docker.Group) doctorRow {
	summary := fmt.Sprintf("候補 %d/%d 件  見積もり %s  (docker 申告 %s)",
		len(g.Items), g.Total, docker.HumanSize(g.Size), docker.HumanSize(g.Reclaimable))
	detail := make([]doctorRow, 0, len(g.Notes)+len(g.Items)+4)
	for _, n := range g.Notes {
		// 🚨 注記は**折り返す**。ここが「何を数えたか」の説明なので、末尾から削られると
		// 意味が壊れる (brew の説明と同じ扱い)
		for _, line := range wrapToWidth(n, max(20, o.width-8)) {
			detail = append(detail, doctorRow{text: doctorColor(o.colored, ansiDim, "     "+line)})
		}
	}
	if g.Command != "" {
		detail = append(detail,
			doctorRow{text: ""},
			doctorRow{text: doctorColor(o.colored, ansiDim, "     この群をまとめて回収するなら:")},
			doctorRow{text: "       $ " + doctorColor(o.colored, ansiBold, g.Command)})
	} else {
		detail = append(detail,
			doctorRow{text: ""},
			doctorRow{text: doctorColor(o.colored, ansiYellow,
				"     🚨 まとめて消すコマンドは出しません (1 件ずつ選んでください)")},
			doctorRow{text: doctorColor(o.colored, ansiDim,
				"        下の行を選んで y を押すと、その 1 件を消すコマンドをコピーします")})
	}
	if len(g.Items) == 0 {
		detail = append(detail, doctorRow{text: ""},
			doctorRow{text: doctorColor(o.colored, ansiGreen, "     🎉 古い候補はありません")})
	}
	for _, it := range g.Items {
		row := doctorRow{text: dockerItemLine(o, it)}
		if it.Command != "" {
			// 🚨 1 件ずつ消したいときの導線。ボリュームには群のコマンドが無いので、
			// ここが選べないと「1 件ずつ選んでください」と言うだけで手段が無い
			row.selectable = true
			row.key = "dockeritem:" + string(g.Kind) + ":" + it.Name
			row.copyPath = it.Command
			row.copyText = it.Name + " (" + it.SizeText + "):\n" + it.Command + "\n"
		}
		detail = append(detail, row)
	}
	return doctorRow{
		text: " " + dockerMark(g.Kind) + padSpaces(max(0, dockerMarkWidth()-dispWidth(dockerMark(g.Kind)))) + " " + g.Label + "   " +
			doctorColor(o.colored, ansiDim, summary),
		selectable: true,
		key:        "docker:" + string(g.Kind),
		detail:     detail,
		copyPath:   g.Command,
		copyText:   dockerGroupCopyText(g),
	}
}

func dockerItemLine(o doctorRenderOpts, it docker.Item) string {
	age := "  日数不明"
	if it.AgeKnown {
		age = fmt.Sprintf("%4.0f日前", it.Age.Hours()/24)
	}
	line := fmt.Sprintf("     %9s %s  %s", it.SizeText, age, it.Name)
	if it.Detail != "" {
		line += doctorColor(o.colored, ansiDim, "  "+it.Detail)
	}
	return truncateDisp(line, max(20, o.width-2), "…")
}

// dockerGroupCopyText は Y でコピーする解説 (別セッションの LLM にそのまま投げられる形)。
func dockerGroupCopyText(g docker.Group) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s\n", g.Kind, g.Label)
	fmt.Fprintf(&b, "候補 %d 件 / 全 %d 件、見積もり %s (docker 申告の回収可能 %s)\n",
		len(g.Items), g.Total, docker.HumanSize(g.Size), docker.HumanSize(g.Reclaimable))
	for _, n := range g.Notes {
		b.WriteString("- " + n + "\n")
	}
	if g.Command != "" {
		b.WriteString("まとめて: " + g.Command + "\n")
	}
	for _, it := range g.Items {
		fmt.Fprintf(&b, "  %s  %s", it.SizeText, it.Name)
		if it.Command != "" {
			b.WriteString("  -> " + it.Command)
		}
		b.WriteString("\n")
	}
	return b.String()
}
