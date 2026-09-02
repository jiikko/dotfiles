package svc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"doctor/runner"
)

// LaunchDir は走査するディレクトリと、その登録が属する launchd ドメイン。
type LaunchDir struct {
	Path   string
	Domain string // "gui/<uid>" か "system"
}

// DefaultDirs はサードパーティ領域だけ。/System/Library と /usr/lib は Apple 管理 (SIP 下で消せず
// ユーザーが判断できない) なので走査しない。除外は**パス基準**で、ラベル名 (com.apple.*) では
// 除外しない: ラベルはただの文字列で、/Library に第三者が置いた plist も同じ名前を名乗れる。
func DefaultDirs(home string, uid int) []LaunchDir {
	gui := fmt.Sprintf("gui/%d", uid)
	return []LaunchDir{
		{Path: filepath.Join(home, "Library", "LaunchAgents"), Domain: gui},
		{Path: "/Library/LaunchAgents", Domain: gui},
		{Path: "/Library/LaunchDaemons", Domain: "system"},
	}
}

// Options は Scan の入力。テストは Dirs / Run / Stat を差し替える。
type Options struct {
	Dirs []LaunchDir
	Run  runner.Runner
	Stat func(string) error // 実行ファイルの存在確認 (nil なら os.Stat)
}

// Finding は壊れていると判定した 1 登録。Reasons は「なぜ出ているか」で、判断材料なしの指摘を
// 出さないために必ず 1 つ以上入る。
type Finding struct {
	Label     string
	PlistPath string
	Domain    string
	Reasons   []string
	// MissingExec は A で不在だった実行ファイル (A に該当しなければ "")
	MissingExec string
	// LastExit は launchctl list の正の exit code (B)。HasLastExit=false なら状態を取れていない
	LastExit    int
	HasLastExit bool
	RestartKeys []string
	PenaltyBox  bool
	// BrewOrphan は C: homebrew.mxcl.<formula> の formula が brew list に無い
	BrewOrphan   bool
	BrewFormula  string
	AppleLikeOut bool // ラベルは com.apple.* を名乗るが管理領域の外にある (表示で注記)
	Commands     []string
}

// Undiagnosed は判定できなかった 1 件。黙って消さずにリストに出す。
type Undiagnosed struct {
	PlistPath string
	Reason    string
}

// Report は Scan の結果。「候補 0 件」と「診断できず」を区別する:
// StatusErr が非空なら launchctl を実行できず B を評価していない。BrewErr が非空なら C を評価していない。
type Report struct {
	Findings    []Finding
	Undiagnosed []Undiagnosed
	Scanned     int
	StatusErr   string
	BrewErr     string
	DirErrs     []string // 走査できなかったディレクトリ (存在しないものは含めない)
}

// Scan は走査して Report を返す。停止・削除は行わない (その経路はこのパッケージに存在しない)。
func Scan(ctx context.Context, opt Options) Report {
	stat := opt.Stat
	if stat == nil {
		stat = func(p string) error { _, err := os.Stat(p); return err }
	}
	rep := Report{}
	statuses, err := launchctlList(ctx, opt.Run)
	if err != nil {
		rep.StatusErr = err.Error()
	}
	var formulae map[string]bool
	brewChecked := false
	for _, d := range opt.Dirs {
		entries, err := os.ReadDir(d.Path)
		if err != nil {
			if !os.IsNotExist(err) {
				rep.DirErrs = append(rep.DirErrs, fmt.Sprintf("%s: %v", d.Path, err))
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".plist") {
				continue
			}
			rep.Scanned++
			path := filepath.Join(d.Path, e.Name())
			j, err := parseJob(path)
			if err != nil {
				rep.Undiagnosed = append(rep.Undiagnosed, Undiagnosed{PlistPath: path, Reason: err.Error()})
				continue
			}
			f := Finding{Label: j.Label, PlistPath: path, Domain: d.Domain}
			// A: 起動対象の不在 (主判定)
			t := resolveExecTarget(j, stat)
			if !t.Skip && t.Missing {
				f.MissingExec = t.Path
				if t.Relative {
					f.Reasons = append(f.Reasons, fmt.Sprintf("実行ファイルが _PATH_STDPATH で解決できません: %s", t.Path))
				} else {
					f.Reasons = append(f.Reasons, fmt.Sprintf("実行ファイルがありません: %s", t.Path))
				}
			}
			// B: 正の exit code + 再起動条件 (launchctl が使えたときだけ)
			if st, ok := statuses[j.Label]; ok && st.HasExit {
				f.LastExit, f.HasLastExit = st.Exit, true
				if st.Exit > 0 && len(j.restartKeys) > 0 {
					f.RestartKeys = j.restartKeys
					f.Reasons = append(f.Reasons, fmt.Sprintf("起動に失敗し続けています: last exit %d / %s", st.Exit, strings.Join(j.restartKeys, ", ")))
				}
			}
			// C: brew の台帳に無い (brew 登録のときだけ台帳を引く。1 回だけ)
			if formula := brewFormulaOf(j.Label); formula != "" {
				if !brewChecked {
					brewChecked = true
					formulae, err = brewFormulae(ctx, opt.Run)
					if err != nil {
						rep.BrewErr = err.Error()
					}
				}
				f.BrewFormula = formula
				if formulae != nil && !formulae[formula] {
					f.BrewOrphan = true
					f.Reasons = append(f.Reasons, fmt.Sprintf("Homebrew の台帳にありません: brew list に %s が無い", formula))
				}
			}
			if len(f.Reasons) == 0 {
				continue
			}
			f.AppleLikeOut = strings.HasPrefix(j.Label, "com.apple.")
			// 補助情報は候補に絞ってから取る (全ラベルに print を呼ばない)
			if rep.StatusErr == "" {
				f.PenaltyBox = launchctlPrint(ctx, opt.Run, d.Domain+"/"+j.Label).PenaltyBox
			}
			f.Commands = manualCommands(f)
			rep.Findings = append(rep.Findings, f)
		}
	}
	sort.Slice(rep.Findings, func(a, b int) bool { return rep.Findings[a].Label < rep.Findings[b].Label })
	return rep
}

// manualCommands は人が手で実行するためのコマンド。このツールはこれを実行しない。
// bootout の sudo はドメインで決まる (system だけ)。rm の sudo はファイルの置き場で決まる:
// /Library/LaunchAgents は gui ドメインだが root 所有なので、sudo なしの rm は permission denied になる。
func manualCommands(f Finding) []string {
	bootSudo, rmSudo := "", ""
	if f.Domain == "system" {
		bootSudo = "sudo "
	}
	if strings.HasPrefix(f.PlistPath, "/Library/") {
		rmSudo = "sudo "
	}
	return []string{
		fmt.Sprintf("%slaunchctl bootout %s/%s", bootSudo, f.Domain, f.Label),
		fmt.Sprintf("%srm %s", rmSudo, shellQuote(f.PlistPath)),
	}
}

func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t'\"$`\\") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
