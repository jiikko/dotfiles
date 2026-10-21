package doctorview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"github.com/jiikko/dotfiles/src/termsafe"
	"glogx/internal/app"
	"glogx/internal/term"
	"tmpclean"
)

// doctor の「一時領域」タブ。起動時に macOS が消すとされる場所 (/private/tmp・$TMPDIR) の件数を**数えるだけ**で、
// 消す経路は持たない。片付けは Y でコピーするプロンプトから Claude に頼む (skill cleanup-tmp-dir → bin/tmpclean が
// 生きているセッションを避けて承認された分だけをゴミ箱へ移す)。数え方の正本は src/tmpclean の Count。
//
// 🚨 **結果はキャッシュに載せない** (SSD・Docker と同じ)。数えるのは読み取りだけで速く、保存すると古い件数が今の件数に見える。

const (
	// 起動時に消える場所の合計の閾値。根拠は 1 件だけ: /private/tmp の Claude Code の作業ファイル約 99 万件で起動が 20 分
	// 止まったと筆者が推定した事例 (zenn.dev/singularity/articles/vibe-coding-mac-boot-20min)。このマシンは 2026-10-21 に約 2.3 万件。
	// 間の値は未実測なので仮置き。次に再起動したときの件数と起動時間で詰める
	tmpWarnTotal = 100_000
	tmpBadTotal  = 300_000
	// tmpTopShown は Enter で開いたときに出す内訳の数
	tmpTopShown = 5
)

const (
	tmpSessionRowKey rowKey = "tmp:session"
	tmpTmpdirRowKey  rowKey = "tmp:tmpdir"
	tmpRepoRowKey    rowKey = "tmp:repo"
)

type tmpReport struct {
	counts  tmpclean.Counts
	err     string
	repoTmp string // この repo の tmp/ ("" = repo の外)
	repo    tmpclean.AreaCount
}

type doctorTmpMsg struct {
	gen int
	rep tmpReport
}

// tmpCmd は数える Cmd。ctx (閉じる / 再スキャンで cancel される) を数える処理に通す: 閉じるときは走査の完了を待つ
// (doctor_cleanup.go) ので、件数が多くても途中で打ち切れるようにする。
func (v *DoctorView) tmpCmd(ctx context.Context, gen int) tea.Cmd {
	countFn := tmpclean.Count
	if v.deps.TmpCount != nil {
		countFn = v.deps.TmpCount
	} else if runningUnderTest {
		// テストが差し替えずに開いたら本物の /private/tmp を数えない (数千〜数十万件を歩いて並列のテストを遅らせ、
		// 機械ごとに結果が変わる)。ファイルの走査は外部コマンドではないので tests/glogx/test_no_real_commands_in_tests.sh では
		// 捕まらない。判定は safefs.ProductionGuard と同じく実行ファイル名で、完全ではない (差し忘れを止めるだけ)
		countFn = func(context.Context) (tmpclean.Counts, error) {
			return tmpclean.Counts{}, errors.New("テストが Deps.TmpCount を差し替えずに doctor を開いた")
		}
	}
	repoFn := func() string { return repoTmpDir(app.CurrentDir()) }
	if v.deps.RepoTmpDir != nil {
		repoFn = v.deps.RepoTmpDir
	}
	return func() tea.Msg {
		var rep tmpReport
		doctorTrack(func() {
			c, err := countFn(ctx)
			if err != nil {
				rep.err = termsafe.PlainLine(err.Error())
			}
			rep.counts = c
			if dir := repoFn(); dir != "" && err == nil {
				if r, err := tmpclean.CountDir(ctx, dir); err == nil {
					rep.repoTmp, rep.repo = dir, r
				}
			}
		})
		return doctorTmpMsg{gen: gen, rep: rep}
	}
}

var runningUnderTest = strings.HasSuffix(os.Args[0], ".test")

// repoTmpDir は cwd から親へたどって最初に .git (dir でも worktree のファイルでも) がある dir の tmp/。repo の外なら ""。
func repoTmpDir(cwd string) string {
	for dir := cwd; dir != "" && dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			tmp := filepath.Join(dir, "tmp")
			if fi, err := os.Lstat(tmp); err == nil && fi.IsDir() {
				return tmp
			}
			return ""
		}
	}
	return ""
}

// receiveTmp は結果を受け取り、出すべき toast の文言を返す ("" = 出さない)。多いときだけ、開くたびに出す。
func (v *DoctorView) receiveTmp(msg doctorTmpMsg) string {
	if msg.gen != v.gen || !v.shown {
		return ""
	}
	rep := msg.rep
	v.tmp = &rep
	if rep.err == "" && tmpBootTotal(rep.counts) >= tmpBadTotal {
		return "一時領域が " + tmpCount(tmpBootTotal(rep.counts)) + " 溜まっています (再起動が遅くなります)。一時領域のタブで Y を押すと片付けを頼むプロンプトをコピーできます"
	}
	return ""
}

// tmpBootTotal は起動時に消える場所 (/private/tmp と $TMPDIR) の合計。/private/tmp のその他は直下の数しか数えない。
func tmpBootTotal(c tmpclean.Counts) int {
	n := c.Session.Total + c.OtherPrivate
	if c.Tmpdir != nil {
		n += c.Tmpdir.Total
	}
	return n
}

// tmpMark は合計の状態の印 (画面の語彙は disk / ssd と揃える)。
func tmpMark(total int) string {
	switch {
	case total >= tmpBadTotal:
		return "⛔ 多い"
	case total >= tmpWarnTotal:
		return "🚨 注意"
	}
	return "✅ ok"
}

// tmpCount は件数を「1.6万件」「7,117件」の形にする。
func tmpCount(n int) string {
	if n >= 10_000 {
		return strings.TrimSuffix(strconv.FormatFloat(float64(n)/10_000, 'f', 1, 64), ".0") + "万件"
	}
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s + "件"
}

// tmpTabSummary はタブ行の要約。**印だけ** (SSD と同じ。どれも 2 桁): 件数まで載せると 6 つのタブが幅 60 に収まらず、
// 末尾 (このタブ) の要約が切られる。件数はタブを開いた見出しに出す。印は行の印 (tmpMark) の先頭と同じにする
func (v *DoctorView) tmpTabSummary(o DoctorRenderOpts) string {
	switch {
	case v.tmp == nil:
		return o.Spinner
	case v.tmp.err != "":
		return "❓"
	}
	mark, _, _ := strings.Cut(tmpMark(tmpBootTotal(v.tmp.counts)), " ")
	return mark
}

func (v *DoctorView) tmpSection(o DoctorRenderOpts) []doctorRow {
	if v.tmp == nil {
		return sectionHeader(o, "一時領域", o.Spinner+" 数えています")
	}
	rep := *v.tmp
	if rep.err != "" {
		rows := sectionHeader(o, "一時領域", "❓ 判定不能")
		return append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, "数えられません: "+rep.err)})
	}
	c := rep.counts
	total := tmpBootTotal(c)
	rows := sectionHeader(o, "一時領域", "起動時に消える場所 "+tmpCount(total)+" "+tmpMark(total))
	rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim,
		fmt.Sprintf("  (注意 %s / 多い %s)", strings.TrimSuffix(tmpCount(tmpWarnTotal), "件"), strings.TrimSuffix(tmpCount(tmpBadTotal), "件")))})

	labelW := term.DispWidth("Claude Code の作業場所")
	line := func(label string, a tmpclean.AreaCount, note string) string {
		return label + term.PadSpaces(labelW-term.DispWidth(label)) + "  " +
			fmt.Sprintf("%8s  %7s", tmpCount(a.Total), disk.HumanSize(a.Bytes)) + note
	}
	// most は行の下の「最多:」。Enter で内訳を開いている間は出さない (内訳の後に出て、順番が逆に見える)
	most := func(key rowKey, parts []string) []doctorRow {
		if len(parts) == 0 || v.expanded[key] {
			return nil
		}
		return []doctorRow{{text: doctorColor(o.Colored, term.AnsiDim, "      最多: "+strings.Join(parts, " / "))}}
	}

	sess := c.Session
	rows = append(rows, tmpAreaRow(tmpSessionRowKey, line("Claude Code の作業場所", sess, ""), sess.Root, sess.Projects, "",
		tmpPrompt("Claude Code の作業場所 (/private/tmp の scratchpad)", sess.Total, sess.Projects, "", 0), o))
	if len(sess.Projects) > 0 {
		rows = append(rows, most(tmpSessionRowKey, []string{tmpPartText(sess.Projects[0], "")})...)
	}
	if c.Tmpdir != nil {
		t := *c.Tmpdir
		rows = append(rows, tmpAreaRow(tmpTmpdirRowKey, line("$TMPDIR", t, ""), t.Root, t.Prefixes, "*",
			tmpPrompt("$TMPDIR の直下", t.Total, t.Prefixes, "*", t.EmptyDirs), o))
		var parts []string
		if t.EmptyDirs > 0 {
			parts = append(parts, "空の dir "+tmpCount(t.EmptyDirs))
		}
		if len(t.Prefixes) > 0 {
			parts = append(parts, tmpPartText(t.Prefixes[0], "*"))
		}
		rows = append(rows, most(tmpTmpdirRowKey, parts)...)
	}
	rows = append(rows, doctorRow{text: "/private/tmp のその他" +
		term.PadSpaces(labelW-term.DispWidth("/private/tmp のその他")) + fmt.Sprintf("  %8s", tmpCount(c.OtherPrivate)) +
		doctorColor(o.Colored, term.AnsiDim, "           (消さない)")})
	if rep.repoTmp != "" {
		r := rep.repo
		rows = append(rows, tmpAreaRow(tmpRepoRowKey, line("この repo の tmp/", r, doctorColor(o.Colored, term.AnsiDim, " (参考)")),
			rep.repoTmp, nil, "", tmpPrompt("この repo の tmp/ ("+termsafe.PlainLine(rep.repoTmp)+"。起動時の削除には関係しない)", r.Total, nil, "", 0), o))
	}
	return rows
}

// copyTextHint は最下行の Y の案内。一時領域のタブでは、コピーするのが解説ではなく片付けを頼むプロンプトなので語を変える。
func (v *DoctorView) copyTextHint() string {
	if v.tab == TabTmp {
		return "Y: 片付けを頼むプロンプトをコピー"
	}
	return "Y: 解説をコピー"
}

// tmpAreaRow は領域の行。Enter で内訳 (上位 tmpTopShown)、y で根のパス、Y で片付けを頼むプロンプト。
func tmpAreaRow(key rowKey, text, root string, parts []tmpclean.Part, suffix, prompt string, o DoctorRenderOpts) doctorRow {
	row := doctorRow{
		text:       text,
		selectable: true,
		key:        key,
		hasDetail:  len(parts) > 0,
		copyPath:   termsafe.PlainLine(root),
		copyKind:   copyKindPath,
		copyText:   prompt,
	}
	for i, p := range parts {
		if i >= tmpTopShown {
			break
		}
		row.detail = append(row.detail, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "        "+tmpPartText(p, suffix))})
	}
	return row
}

// tmpPartText は内訳の 1 件。suffix は接頭辞でまとめた名前に付ける "*" (そのままだと名前そのものに見える)。
func tmpPartText(p tmpclean.Part, suffix string) string {
	return termsafe.PlainLine(p.Name) + suffix + " " + tmpCount(p.Count)
}

// tmpPrompt は Y でコピーする「片付けを頼むプロンプト」。承認を省く書き方はしない (skill が手順 4 で必ず承認を取る)。
func tmpPrompt(target string, total int, parts []tmpclean.Part, suffix string, empty int) string {
	var b strings.Builder
	b.WriteString("/cleanup-tmp-dir で一時領域を片付けて。対象は " + target + "。glogx の doctor では " + tmpCount(total))
	most := make([]string, 0, 4)
	if empty > 0 {
		most = append(most, "空の dir "+tmpCount(empty))
	}
	for i, p := range parts {
		if i >= 3 {
			break
		}
		most = append(most, tmpPartText(p, suffix))
	}
	if len(most) > 0 {
		b.WriteString("で、多いのは " + strings.Join(most, " / "))
	}
	b.WriteString("。消す前に、種類ごとの件数と失うものを見せて確認して。")
	return b.String()
}
