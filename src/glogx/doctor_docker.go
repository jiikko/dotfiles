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

// dockerRunnable は「この群のコマンドを x で実行してよいか」。
//
// 🚨 **判定の出典は `Group.Confirm` (走査側のデータ) 1 つ**。Kind から独立に導き直さない —
// 以前は Kind の switch で同じ結論を作っており、走査側が `Confirm: true` の群を足しても
// こちらは素通しする形だった (敵対レビュー 2 周目 P2-4。`mutation-verify-new-tests.md` の
// 「同じ判定を 2 箇所で別実装していないか」)。
//
// 今 false になるのはボリュームだけ: 中身はデータで消すと戻らず、しかも判定は
// 「今どのコンテナからも参照されていない + 作成日」の近似でしかない (最後にマウントされた
// 日時を Docker が持っていないため)。y でコピーして人がシェルで叩く (ユーザー決定 2026-09-04)。
func dockerRunnable(g docker.Group) bool { return !g.Confirm }

// dockerRunNote は実行する前に知っておくこと (確認画面に出る)。
func dockerRunNote(k docker.Kind) string {
	switch k {
	case docker.KindContainers:
		return "停止したコンテナの書き込みレイヤーが消えます (中で作った成果物が残っていないか確認してください)"
	case docker.KindImages:
		return "消したイメージは次に使うとき再ダウンロードになります (数 GB の通信)"
	case docker.KindBuildCache:
		return "次のビルドがキャッシュ無しになります (壊れはしません)"
	case docker.KindVolumes:
		return "🚨 中身はデータです。戻せません"
	}
	return ""
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
	v.registerDockerAction("まとめて回収する", d.SystemPrune, d.SystemPruneNote)
	rows = append(rows,
		doctorRow{text: ""},
		doctorRow{text: doctorColor(o.colored, ansiDim, "   まとめて回収するなら (Space で選んで x):")},
		doctorRow{
			text: "  " + v.dockerSelectMark(o, d.SystemPrune, true) + " " +
				doctorColor(o.colored, ansiBold, d.SystemPrune),
			selectable: true,
			key:        "dockerprune",
			detail: textRows([]string{
				doctorColor(o.colored, ansiYellow, "     🚨 "+d.SystemPruneNote),
				doctorColor(o.colored, ansiDim, "     Space で選んで x、または y でコピーして自分で叩いてください"),
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
		row := doctorRow{text: v.dockerSelectMark(o, it.Command, dockerRunnable(g)) + dockerItemLine(o, it)}
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
	if dockerRunnable(g) {
		v.registerDockerAction(g.Label+" をまとめて回収", g.Command, dockerRunNote(g.Kind))
		for _, it := range g.Items {
			v.registerDockerAction(it.Name+" を消す ("+it.SizeText+")", it.Command, dockerRunNote(g.Kind))
		}
	}
	return doctorRow{
		text: v.dockerSelectMark(o, g.Command, dockerRunnable(g)) +
			dockerMark(g.Kind) + padSpaces(max(0, dockerMarkWidth()-dispWidth(dockerMark(g.Kind)))) + " " + g.Label + "   " +
			doctorColor(o.colored, ansiDim, summary),
		selectable: true,
		key:        "docker:" + string(g.Kind),
		detail:     detail,
		// 🚨 群のコマンドが無い (ボリューム) 行でも y が無言にならないよう、
		// 1 件ずつのコマンドを並べたものを渡す (hint は「y: コマンドをコピー」と言っている)
		copyPath: dockerGroupCopyPath(g),
		copyText: dockerGroupCopyText(g),
	}
}

// dockerSelectMark は行頭の選択欄。実行できる行だけ印を出す (実行できない行に空欄を置くと
// 「選べるのに選ばれていない」に見える)。あわせて x の実体をコマンド文字列で登録する。
func (v *doctorView) dockerSelectMark(o doctorRenderOpts, cmd string, runnable bool) string {
	if cmd == "" || !runnable {
		return " "
	}
	if v.selectedActions[cmd] {
		return doctorColor(o.colored, ansiBold, "*")
	}
	return " "
}

// registerDockerAction は x で実行する手を登録する (brew と同じ map を共有する。
// 同一性は**コマンド文字列**で、行の key ではない — 再スキャンで行の並びは変わる)。
//
// 🚨 **描画順も覚える** (dockerOrder)。選択の集計を「今 v.rows に出ている行」から作ると、
// 群を畳んだ瞬間に中の選択が 0 件に見え、x が「選んでください」と言い出す
// (選択自体は残っているので、開き直すと戻る = 同じ選択が開閉で増減する。
// 敵対レビュー 2 周目 P2-2)。畳まれている行も登録は通るので、こちらを出典にする。
func (v *doctorView) registerDockerAction(label, cmd, note string) {
	if cmd == "" {
		return
	}
	if v.actionByCmd == nil {
		v.actionByCmd = map[string]doctorCmdAction{}
	}
	if _, dup := v.actionByCmd[cmd]; !dup {
		v.dockerOrder = append(v.dockerOrder, cmd)
	}
	v.actionByCmd[cmd] = doctorCmdAction{Label: label, Cmd: cmd, Note: note}
}

// selectedDockerActions は選ばれた手を**描画順**で返す (brew 側と同じ規律。map をそのまま
// 回すと確認画面の順と実行の順が食い違い、中断したときにどこまで走ったか読めない)。
//
// 🚨 出典は dockerOrder (登録順) で、`v.rows` ではない。理由は registerDockerAction の 🚨。
// 🚨 登録に無いコマンドは返さない (「(不明な手)」で実行しない) — 実行してよい手の集合は
// 登録が唯一の出典で、fallback を置くと実行不可の群の手が紛れ込む口になる
func (v *doctorView) selectedDockerActions() []doctorCmdAction {
	if len(v.selectedActions) == 0 {
		return nil
	}
	out := make([]doctorCmdAction, 0, len(v.selectedActions))
	for _, cmd := range v.dockerOrder {
		if !v.selectedActions[cmd] {
			continue
		}
		if act, ok := v.actionByCmd[cmd]; ok {
			out = append(out, act)
		}
	}
	return out
}

// toggleDockerAction は Docker の行の選択を切り替える。
//
// 🚨 **実行できない行は断る理由を返す** (無言で何もしない、にしない)。ボリュームは
// 「y でコピーして自分で叩く」しか手段が無いので、そう案内する。
func (v *doctorView) toggleDockerAction() (string, bool) {
	cmd := v.rows[v.cur.index].copyPath
	if cmd == "" {
		return "この行には実行するコマンドがありません", false
	}
	// 🚨 実行してよい手だけを選ばせる。**登録されていないコマンドは実行できない** —
	// 実行不可の群 (ボリューム) の手は registerDockerAction を通っていないので、ここで落ちる
	// (行の key から Kind を導き直すと、判定が 2 実装になる)
	if _, ok := v.actionByCmd[cmd]; !ok {
		return "この行はこの画面から実行しません (消すと戻らないので y でコマンドをコピーしてください)", false
	}
	return v.toggleCmdAction()
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

// dockerGroupCopyPath は y でコピーするコマンド。群のまとめコマンドが無い群では
// 1 件ずつのコマンドを改行区切りで返す (無言で何もしない、にしない)。
func dockerGroupCopyPath(g docker.Group) string {
	if g.Command != "" {
		return g.Command
	}
	cmds := make([]string, 0, len(g.Items))
	for _, it := range g.Items {
		if it.Command != "" {
			cmds = append(cmds, it.Command)
		}
	}
	return strings.Join(cmds, "\n")
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

// isDockerRowKey は Docker タブの行か (Space の分岐に使う)。
func isDockerRowKey(key string) bool {
	return strings.HasPrefix(key, "docker:") || strings.HasPrefix(key, "dockeritem:") || key == "dockerprune"
}
