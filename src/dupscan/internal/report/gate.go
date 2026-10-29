package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"dupscan/internal/group"

	"dupscan/internal/exclude"
	"dupscan/internal/gate"
	"dupscan/internal/jsonout"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// GateSummary は gate の出力の形。
type GateSummary struct {
	Root       string `json:"root"`
	Base       string `json:"base"`
	Head       string `json:"head"`
	Changed    int    `json:"changed_units"`
	BaseIsHead bool   `json:"base_is_head"` // base が head の commit と同じ (CI で -base を付け忘れた疑い)
	// BaseGiven は -base を渡して決めた base か (渡したのに「-base を渡す」と案内しない)。
	BaseGiven bool `json:"base_given"`
	// Partial は走査の範囲が repo 全体でないこと (起点がサブディレクトリ)。Prefixes がその起点。
	Partial    bool            `json:"scope_partial"`
	Prefixes   []string        `json:"scope_prefixes,omitempty"`
	Failed     bool            `json:"failed"`
	Violations []ViolationJSON `json:"violations"`
	Unreadable []UnitRef       `json:"unreadable_changed_units"`
	// 単位を抽出できない解析失敗も、base/headのどちらを比べられなかったかを残す。
	AnalysisIncomplete  bool     `json:"analysis_incomplete,omitempty"`
	StrictAnalysis      bool     `json:"strict_analysis,omitempty"`
	BaseFilesWithErrors []string `json:"base_files_with_errors,omitempty"`
	HeadFilesWithErrors []string `json:"head_files_with_errors,omitempty"`
	// NewGeneratedFiles は head で生成されたファイルとして単位を出さなかったファイルのうち、base に無いか、base では生成でなかったか、中身が base と違うもの。
	// 見出しは 1 行のコメントで手でも書けるので、そこに入った写しは「変わった単位 0」で通る。止めはしない (本物の生成コードで毎回赤くしない)
	NewGeneratedFiles []string `json:"new_generated_files"`
	// NewExclusions は base の設定に無く head にある除外の要素 (glob と段・組の段と釘・単位の釘)。人が見る一覧。
	NewExclusions []exclude.Element `json:"new_exclusions"`
	// Untracked は head の作業ツリーで、ignore されていない untracked から読んだファイル (commit を head にしたときは空)。
	Untracked []string `json:"untracked_files"`
	// PathPassed は、パスの除外の下にあるので寄せる側に名指ししなかった、この変更で触った単位の数 (除外で通した写し)。
	PathPassed     int       `json:"path_excluded_passed_units"`
	PathPassedList []UnitRef `json:"path_excluded_passed"`
	// PresetPassedList は、プリセットが組を外したので違反にしなかった、この変更で触った単位 (止めないが人が見る一覧)。
	PresetPassedList []UnitRef `json:"preset_passed"`
	// Resolved は base の組のうち head で消えた・縮んだもの (寄せた結果。除外を掛けない素の組で比べる)。
	Resolved []ResolvedJSON `json:"resolved"`
	// BaseConfigError は base の設定を読めなかった理由 (空とみなした)。
	BaseConfigError string `json:"base_config_error,omitempty"`
	Exclusions
}

// ViolationJSON は違反 1 つの出力。
type ViolationJSON struct {
	Level string    `json:"level"`
	Fail  bool      `json:"fail"`
	New   []UnitRef `json:"new"`  // 組に新しく入った単位 (寄せる側)
	Same  []UnitRef `json:"same"` // 組に前からいた単位 (寄せる先の候補)
	// Exclude はこの組を指す組の除外の 1 行 (誤検知なら kind と reason を埋めて .dupscan.json に貼る)。
	Exclude string `json:"exclude"`
	// SingleCall は組の全員の本体が、1 つの呼び出しを 1 回するだけか (GroupJSON.SingleCall と同じ)。
	SingleCall bool `json:"single_call,omitempty"`
	// Origins は New の単位のうち、base に出どころ (相手) を当てた単位ごとの出どころ (新しく書いた単位は入らない)。
	Origins []OriginJSON `json:"origins"`
	// compare はメンバーの違いを確かめる手 (text の -v だけに出す。compareHints)。
	compare []string
}

// OriginJSON は寄せる側の単位 1 つの出どころ。
type OriginJSON struct {
	Key  string  `json:"key"` // 寄せる側の単位の鍵 (ViolationJSON.new の key)
	From UnitRef `json:"from"`
	// BodyChanged は出どころから本体 (L1) を変えたか。
	BodyChanged bool `json:"body_changed"`
	// Sure は出どころを確かに言えるか: 同じ鍵 (クロージャを除く) か、本体が L2 まで同じ。L3 の形やクロージャの順番だけで
	// 当てた相手 (gate の origins の緩い段) は、関係の無い単位のことがあるので false にし、text では名指しを弱める。
	Sure bool `json:"sure"`
}

// ResolvedJSON は消えた・縮んだ組 1 つの出力。Members は base の側の位置。
type ResolvedJSON struct {
	Level   string    `json:"level"`
	Members []UnitRef `json:"members"`
	Remain  int       `json:"remain"` // head で同じ組に揃っている数 (0 は消えた)
}

// BuildGate は gate の出力の形を組み立てる。headEntries は head の単位の全部 (compareHints)。explain は作業ツリーを読むので、
// head が commit なら nil を渡す (行が作業ツリーとずれる argv を出さない)。
func BuildGate(root, base, head string, r gate.Result, ex *exclude.Set, headEntries []*group.Entry) GateSummary {
	s := GateSummary{Root: root, Base: base, Head: head, Changed: r.Changed, Failed: r.Failed(),
		Violations: []ViolationJSON{}, Unreadable: []UnitRef{}, NewGeneratedFiles: []string{}, NewExclusions: []exclude.Element{}, Untracked: []string{}, Exclusions: exclusions(ex),
		PathPassed: len(r.PathPassed), PathPassedList: []UnitRef{}, PresetPassedList: []UnitRef{}, Resolved: []ResolvedJSON{}}
	for _, g := range r.Resolved {
		rj := ResolvedJSON{Level: g.Level.String(), Remain: g.Remain}
		for _, m := range g.Members {
			rj.Members = append(rj.Members, ref(m.Unit))
		}
		s.Resolved = append(s.Resolved, rj)
	}
	for _, e := range r.PathPassed {
		s.PathPassedList = append(s.PathPassedList, ref(e.Unit))
	}
	for _, e := range r.PresetPassed {
		s.PresetPassedList = append(s.PresetPassedList, ref(e.Unit))
	}
	for _, v := range r.Violations {
		members := append(append([]*group.Entry(nil), v.New...), v.Same...)
		vj := ViolationJSON{Level: v.Level.String(), Fail: v.Fail, New: []UnitRef{}, Same: []UnitRef{},
			Exclude: exclude.GroupSnippet(v.Level, members), SingleCall: SingleCall(members), compare: compareHints(root, v.Level, members, headEntries)}
		vj.Origins = []OriginJSON{}
		for _, m := range v.New {
			vj.New = append(vj.New, ref(m.Unit))
			if b := v.From[m]; b != nil {
				sameKey := b.Unit.Key == m.Unit.Key && m.Unit.Kind != tree.KindClosure
				vj.Origins = append(vj.Origins, OriginJSON{Key: m.Unit.Key, From: ref(b.Unit),
					BodyChanged: b.Result.Hash[norm.L1] != m.Result.Hash[norm.L1],
					Sure:        sameKey || b.Result.Hash[norm.L2] == m.Result.Hash[norm.L2]})
			}
		}
		for _, m := range v.Same {
			vj.Same = append(vj.Same, ref(m.Unit))
		}
		s.Violations = append(s.Violations, vj)
	}
	for _, e := range r.Unreadable {
		s.Unreadable = append(s.Unreadable, ref(e.Unit))
	}
	return s
}

// GateJSON は JSON で書く。
func GateJSON(w io.Writer, s GateSummary) error {
	return WriteJSON(w, s)
}

// GateText は人が読む形で書く。新しい重複が無ければ 1 行だけにする (セルフレビューと make test で何度も回すため)。
func GateText(w io.Writer, s GateSummary, opt TextOptions) error {
	note := ""
	if s.BaseIsHead {
		note += "・base が HEAD と同じ (見たのは未コミットの変更だけ"
		if !s.BaseGiven {
			note += "。CI なら -base を渡す"
		}
		note += ")"
	}
	if s.Partial {
		note += "・repo の一部だけを見ている (起点 " + strings.Join(s.Prefixes, ", ") + "。repo の root から打つと別の dir の同じ形の相手も見える)"
	}
	if s.AnalysisIncomplete {
		note += fmt.Sprintf("・読めない構文のファイル base %d / head %d", len(s.BaseFilesWithErrors), len(s.HeadFilesWithErrors))
	}
	if n := len(s.Unreadable); n > 0 {
		note += fmt.Sprintf("・読めない構文で確かめられなかった変わった単位 %d (-v で一覧)", n)
	}
	if n := len(s.NewGeneratedFiles); n > 0 {
		note += fmt.Sprintf("・生成ファイルとして読まなかった新しい・書き換えたファイル %d (そこの写しは比べていない。-v で一覧)", n)
	}
	if n := len(s.NewExclusions); n > 0 {
		note += fmt.Sprintf("・新しく足された除外 %d (組・単位・glob・プリセットの要素と、釘の個数の制約を 1 つずつ数えた数で、組の数ではない。-v で一覧)", n)
	}
	if s.PathPassed > 0 {
		note += fmt.Sprintf("・パスの除外で通した変わった単位 %d", s.PathPassed)
	}
	if n := len(s.PresetPassedList); n > 0 {
		note += fmt.Sprintf("・プリセットで通した変わった単位 %d (-v で一覧)", n)
	}
	if s.BaseConfigError != "" {
		note += "・base の除外の設定を読めないので空とみなした"
	}
	if gone, shrunk := s.resolvedCounts(); gone+shrunk > 0 {
		note += fmt.Sprintf("・消えた組 %d・縮んだ組 %d (-v で一覧)", gone, shrunk)
	}
	if n := len(s.Untracked); n > 0 {
		note += fmt.Sprintf("・untracked から読んだファイル %d (CI には無い。-v で一覧)", n)
	}
	note += s.note()
	if len(s.Violations) == 0 {
		// 読めないファイルがあるときに「なし」と言い切らない (そこに写しがあっても見えない)。ただし通ったことは先頭で分かる形にする
		// (「解析が不完全」だけだと失敗に読め、rc を見るまで通ったか分からない)。
		status := "新しい重複なし"
		if s.AnalysisIncomplete {
			status = "新しい重複 0 (読めたファイルの中で)・解析が不完全"
		}
		fmt.Fprintf(w, "dupscan gate: %s (base %s・変わった単位 %d)%s\n", status, short(s.Base), s.Changed, note)
	} else {
		fails := 0
		for _, v := range s.Violations {
			if v.Fail {
				fails++
			}
		}
		fmt.Fprintf(w, "dupscan gate: 新しい重複 %d (止める %d・報告だけ %d)・base %s・変わった単位 %d%s\n",
			len(s.Violations), fails, len(s.Violations)-fails, short(s.Base), s.Changed, note)
		shown := s.Violations
		if opt.Limit > 0 && len(shown) > opt.Limit {
			shown = shown[:opt.Limit]
		}
		for _, v := range shown {
			kind := "止める"
			if !v.Fail {
				kind = "報告だけ"
			}
			fmt.Fprintf(w, "%s (%s):\n", v.Level, kind)
			for _, m := range v.New {
				fmt.Fprintf(w, "  新しい: %s %s%s\n", m.Where(), label(m), fromNote(m, v.Origins))
			}
			for _, m := range v.Same {
				fmt.Fprintf(w, "  同じ形: %s %s%s\n", m.Where(), label(m), allowedMark(m))
			}
			writeSingleCallMark(w, v.SingleCall)
			if opt.Verbose {
				vline(w, verboseExclude, "%s", v.Exclude)
				writeCompare(w, v.compare)
			}
		}
		if rest := len(s.Violations) - len(shown); rest > 0 {
			fmt.Fprintf(w, "… 他 %d 件 (-limit 0 か -format json で全部)\n", rest)
		}
	}
	sources := []struct {
		name  string
		files []string
	}{{"base", s.BaseFilesWithErrors}, {"head", s.HeadFilesWithErrors}}
	if slices.Equal(s.BaseFilesWithErrors, s.HeadFilesWithErrors) {
		// 同じ一覧を base と head で 2 度書かない (57 個なら 114 行。読んでほしい数行が埋もれる)
		sources = sources[:1]
		sources[0].name = "base・head 同じ"
	}
	for _, source := range sources {
		files := source.files
		if opt.Limit > 0 && len(files) > opt.Limit {
			files = files[:opt.Limit]
		}
		for _, file := range files {
			fmt.Fprintf(w, "読めないファイル (%s): %s\n", source.name, file)
		}
		if rest := len(source.files) - len(files); rest > 0 {
			fmt.Fprintf(w, "… %sの読めないファイルは他%d件 (-limit 0か-format jsonで全部)\n", source.name, rest)
		}
	}
	if opt.Verbose {
		for _, f := range s.NewGeneratedFiles {
			vline(w, verboseNewGenerated, "%s", f)
		}
		for _, el := range s.NewExclusions {
			vline(w, verboseNewExclusion, "%s%s", el.Key, commentAt(el))
		}
		for _, f := range s.Untracked {
			vline(w, verboseUntracked, "%s", f)
		}
		for _, u := range s.PathPassedList {
			vline(w, verbosePathPassed, "%s:%d %s", u.File, u.Line, label(u))
		}
		for _, u := range s.PresetPassedList {
			vline(w, verbosePresetPassed, "%s:%d %s", u.File, u.Line, label(u))
		}
		if s.BaseConfigError != "" {
			vline(w, verboseBaseConfig, "%s", s.BaseConfigError)
		}
		for _, g := range s.Resolved {
			kind := verboseResolvedGone
			if g.Remain > 0 {
				kind = verboseResolvedShrunk
			}
			vline(w, kind, "%s (%d → %d)", g.Level, len(g.Members), g.Remain)
			for _, m := range g.Members {
				vline(w, verboseResolvedMember, "%s %s", m.Where(), label(m))
			}
		}
		s.write(w)
		for _, u := range s.Unreadable {
			vline(w, verboseUnreadable, "%s:%d %s", u.File, u.Line, label(u))
		}
	}
	return nil
}

// GateXcode は Xcode / SwiftLint と同じ形式で書く。止める段は warning、報告だけの段と同じ形の単位は note。
func GateXcode(w io.Writer, s GateSummary) error {
	for _, source := range []struct {
		name  string
		files []string
	}{{"base", s.BaseFilesWithErrors}, {"head", s.HeadFilesWithErrors}} {
		for _, file := range source.files {
			fmt.Fprintf(w, "%s: warning: %sの読めない構文で解析が不完全 (dupscan.gate.parse)\n", filepath.Join(s.Root, file), source.name)
		}
	}
	for _, v := range s.Violations {
		sev := "warning"
		if !v.Fail {
			sev = "note"
		}
		for _, m := range v.New {
			fmt.Fprintf(w, "%s:%d:%d: %s: %s は既にある %d 個と本体が同じ形 (dupscan.gate.%s)\n",
				filepath.Join(s.Root, m.File), m.Line, m.Col, sev, label(m), len(v.Same), v.Level)
		}
		for _, m := range v.Same {
			fmt.Fprintf(w, "%s:%d:%d: note: 同じ形: %s (dupscan.gate.%s)\n", filepath.Join(s.Root, m.File), m.Line, m.Col, label(m), v.Level)
		}
	}
	for _, u := range s.Unreadable {
		fmt.Fprintf(w, "%s:%d:%d: note: 読めない構文があるので確かめられなかった: %s (dupscan.gate)\n", filepath.Join(s.Root, u.File), u.Line, u.Col, label(u))
	}
	for _, f := range s.NewGeneratedFiles {
		fmt.Fprintf(w, "%s:1:1: warning: 生成ファイルとして読まなかった新しい・書き換えたファイル (そこの写しは比べていない) (dupscan.gate.generated)\n", filepath.Join(s.Root, f))
	}
	for _, u := range s.PathPassedList {
		fmt.Fprintf(w, "%s:%d:%d: warning: パスの除外の下なので名指ししなかった写し: %s (dupscan.gate.exclude)\n", filepath.Join(s.Root, u.File), u.Line, u.Col, label(u))
	}
	for _, u := range s.PresetPassedList {
		fmt.Fprintf(w, "%s:%d:%d: note: プリセットで通した写し (定数だけが違う 1 行の委譲): %s (dupscan.gate.preset)\n", filepath.Join(s.Root, u.File), u.Line, u.Col, label(u))
	}
	for _, el := range s.NewExclusions {
		at := exclude.FileName + ":1"
		if el.Comment != "" {
			at = el.Comment
		}
		fmt.Fprintf(w, "%s:1: warning: 新しく足された除外: %s (dupscan.gate.exclude)\n", filepath.Join(s.Root, at), el.Key)
	}
	return nil
}

// resolvedCounts は消えた組と縮んだ組の数。
func (s GateSummary) resolvedCounts() (gone, shrunk int) {
	for _, g := range s.Resolved {
		if g.Remain == 0 {
			gone++
		} else {
			shrunk++
		}
	}
	return gone, shrunk
}

func commentAt(el exclude.Element) string {
	if el.Comment == "" {
		return ""
	}
	return " (" + el.Comment + " のコメント)"
}

func short(h string) string { return h[:min(len(h), 12)] }

// GateRecord は -record の 1 行。最終の終了結果 (出力の失敗・strict の判定不能を含む) と再現条件を持つ。
// 判定まで届いた run は GateSummary の欄 (failed など) を前と同じ形で持つ。判定の前に止まった run (base が決まらない・設定が壊れている) は
// phase と error だけで、GateSummary の欄は無い。起動前・強制終了・書けなかった run は行そのものが無い (記録の欠測で、成功ではない)。
type GateRecord struct {
	Time string `json:"time"`
	// Version は記録の形の版 (exit_code などを足した版が 2。無い行は 1 で、failed だけが判定を表す)。
	Version int `json:"record_version"`
	// ExitCode は gate の最終の終了コード (0 / 1 / 2)。failed (重複の違反) と違い、新しい除外・strict・出力の失敗も含む。
	ExitCode int `json:"exit_code"`
	// Reasons は rc を非 0 にした理由の全部 (duplicates / new_exclusions / path_excluded_passed / strict_incomplete / output_error、
	// 判定の前に止まったら error)。併存する。rc が 0 のときだけ空。
	Reasons []string `json:"reasons"`
	// Phase は判定の前に止まった段階 (args / scope / head / base / config / scan / untracked)。判定まで届いたら無い。
	Phase string `json:"phase,omitempty"`
	Error string `json:"error,omitempty"`
	// Tool は dupscan の版 (build に VCS の情報が入っていれば)。
	Tool *Tool `json:"tool,omitempty"`
	// HeadCommit は head の commit の完全な SHA (作業ツリーを head にした run では HEAD の commit。作業ツリーの中身の証明ではない)。
	HeadCommit string         `json:"head_commit,omitempty"`
	Conditions GateConditions `json:"conditions"`
	*GateSummary
}

// GateConditions は run の再現条件 (渡した flag の値そのまま)。
type GateConditions struct {
	Levels            string `json:"levels"`
	FailLevels        string `json:"fail_levels"`
	MinNodes          int    `json:"min_nodes"`
	MinClosureNodes   int    `json:"min_closure_nodes"`
	FailNewExclusions bool   `json:"fail_new_exclusions"`
	StrictAnalysis    bool   `json:"strict_analysis"`
	NoExclude         bool   `json:"no_exclude"`
	Base              string `json:"base"` // -base に渡した ref (空は既定の upstream / origin/HEAD)。解決した SHA は base の欄
	Head              string `json:"head"` // -head に渡した ref (空は作業ツリー)
}

// Tool は dupscan の版。Revision は build に入った VCS の版 (go build が埋めたときだけ。go_autobuild の build や
// replace のある module では入らないことがある)、Binary は実行ファイルの sha256 (VCS の情報が無くても、同じ版かを照らせる)。
type Tool struct {
	Revision string `json:"revision,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Binary   string `json:"binary_sha256,omitempty"`
}

// ToolRevision は dupscan の版。どちらも取れなければ nil。
func ToolRevision() *Tool {
	var t Tool
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, kv := range info.Settings {
			switch kv.Key {
			case "vcs.revision":
				t.Revision = kv.Value
			case "vcs.modified":
				t.Modified = kv.Value == "true"
			}
		}
	}
	if exe, err := os.Executable(); err == nil {
		if f, err := os.Open(exe); err == nil {
			h := sha256.New()
			if _, err := io.Copy(h, f); err == nil {
				t.Binary = hex.EncodeToString(h.Sum(nil))
			}
			f.Close()
		}
	}
	if t == (Tool{}) {
		return nil
	}
	return &t
}

// AppendRecord は 1 回の gate の結果を、時刻を添えた 1 行の JSON として path に追記する (回をまたいで違反・除外・許可を数えるため)。
func AppendRecord(path string, r GateRecord, now time.Time) error {
	r.Time, r.Version = now.Format(time.RFC3339), 2
	b, err := jsonout.Line(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// fromNote は寄せる側の単位の出どころの注記。出どころの無い (新しく書いた) 単位には付けない。
// 同じ鍵なら「本体を変えた」だけ (その場で直した)、違う鍵なら base の単位を名指しする (移した・改名した・寄せ先に置いた)。
// 確かに言えない相手 (OriginJSON.Sure) は名指しを弱める (新しく書いた写しを「寄せた結果」と読ませない)。
func fromNote(m UnitRef, origins []OriginJSON) string {
	for _, o := range origins {
		if o.Key != m.Key {
			continue
		}
		if !o.Sure {
			return fmt.Sprintf(" (出どころは確かでない: base の %s:%d %s に形が近い)", o.From.File, o.From.Line, label(o.From))
		}
		changed := "移した (本体は同じ)"
		if o.BodyChanged {
			changed = "本体を変えた"
		}
		if o.From.Key == m.Key && m.Kind != string(tree.KindClosure) {
			return " (" + changed + "。base にも同じ単位)"
		}
		return fmt.Sprintf(" (base の %s:%d %s から。%s)", o.From.File, o.From.Line, label(o.From), changed)
	}
	return ""
}
