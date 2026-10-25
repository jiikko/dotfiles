package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"dupscan/internal/group"

	"dupscan/internal/exclude"
	"dupscan/internal/gate"
)

// GateSummary は gate の出力の形。
type GateSummary struct {
	Root       string          `json:"root"`
	Base       string          `json:"base"`
	Head       string          `json:"head"`
	Changed    int             `json:"changed_units"`
	BaseIsHead bool            `json:"base_is_head"` // base が head の commit と同じ (CI で -base を付け忘れた疑い)
	Failed     bool            `json:"failed"`
	Violations []ViolationJSON `json:"violations"`
	Unreadable []UnitRef       `json:"unreadable_changed_units"`
	// NewExclusions は base の設定に無く head にある除外の要素 (glob と段・組の段と釘・単位の釘)。人が見る一覧。
	NewExclusions []exclude.Element `json:"new_exclusions"`
	// Untracked は head の作業ツリーで、ignore されていない untracked から読んだファイル (commit を head にしたときは空)。
	Untracked []string `json:"untracked_files"`
	// PathPassed は、パスの除外の下にあるので寄せる側に名指ししなかった、この変更で触った単位の数 (除外で通した写し)。
	PathPassed     int       `json:"path_excluded_passed_units"`
	PathPassedList []UnitRef `json:"path_excluded_passed"`
	// PresetPassedList は、プリセットが組を外したので違反にしなかった、この変更で触った単位 (止めないが人が見る一覧)。
	PresetPassedList []UnitRef `json:"preset_passed"`
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
}

// BuildGate は gate の出力の形を組み立てる。
func BuildGate(root, base, head string, r gate.Result, ex *exclude.Set) GateSummary {
	s := GateSummary{Root: root, Base: base, Head: head, Changed: r.Changed, Failed: r.Failed(),
		Violations: []ViolationJSON{}, Unreadable: []UnitRef{}, NewExclusions: []exclude.Element{}, Untracked: []string{}, Exclusions: exclusions(ex),
		PathPassed: len(r.PathPassed), PathPassedList: []UnitRef{}, PresetPassedList: []UnitRef{}}
	for _, e := range r.PathPassed {
		s.PathPassedList = append(s.PathPassedList, ref(e.Unit))
	}
	for _, e := range r.PresetPassed {
		s.PresetPassedList = append(s.PresetPassedList, ref(e.Unit))
	}
	for _, v := range r.Violations {
		vj := ViolationJSON{Level: v.Level.String(), Fail: v.Fail, New: []UnitRef{}, Same: []UnitRef{},
			Exclude: exclude.GroupSnippet(v.Level, append(append([]*group.Entry(nil), v.New...), v.Same...))}
		for _, m := range v.New {
			vj.New = append(vj.New, ref(m.Unit))
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
		note += "・base が HEAD と同じ (見たのは未コミットの変更だけ。CI なら -base を渡す)"
	}
	if n := len(s.Unreadable); n > 0 {
		note += fmt.Sprintf("・読めない構文で確かめられなかった変わった単位 %d (-v で一覧)", n)
	}
	if n := len(s.NewExclusions); n > 0 {
		note += fmt.Sprintf("・新しく足された除外 %d (-v で一覧)", n)
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
	if n := len(s.Untracked); n > 0 {
		note += fmt.Sprintf("・untracked から読んだファイル %d (CI には無い。-v で一覧)", n)
	}
	note += s.note()
	if len(s.Violations) == 0 {
		fmt.Fprintf(w, "dupscan gate: 新しい重複なし (base %s・変わった単位 %d)%s\n", short(s.Base), s.Changed, note)
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
				fmt.Fprintf(w, "  新しい: %s:%d %s\n", m.File, m.Line, label(m))
			}
			for _, m := range v.Same {
				fmt.Fprintf(w, "  同じ形: %s:%d %s%s\n", m.File, m.Line, label(m), allowedMark(m))
			}
			if opt.Verbose {
				fmt.Fprintf(w, "  除外の 1 行: %s\n", v.Exclude)
			}
		}
		if rest := len(s.Violations) - len(shown); rest > 0 {
			fmt.Fprintf(w, "… 他 %d 件 (-limit 0 か -format json で全部)\n", rest)
		}
	}
	if opt.Verbose {
		for _, el := range s.NewExclusions {
			fmt.Fprintf(w, "新しく足された除外: %s%s\n", el.Key, commentAt(el))
		}
		for _, f := range s.Untracked {
			fmt.Fprintf(w, "untracked: %s\n", f)
		}
		for _, u := range s.PathPassedList {
			fmt.Fprintf(w, "パスの除外で通した: %s:%d %s\n", u.File, u.Line, label(u))
		}
		for _, u := range s.PresetPassedList {
			fmt.Fprintf(w, "プリセットで通した: %s:%d %s\n", u.File, u.Line, label(u))
		}
		if s.BaseConfigError != "" {
			fmt.Fprintf(w, "base の除外の設定: %s\n", s.BaseConfigError)
		}
		s.write(w)
		for _, u := range s.Unreadable {
			fmt.Fprintf(w, "読めない: %s:%d %s\n", u.File, u.Line, label(u))
		}
	}
	return nil
}

// GateXcode は Xcode / SwiftLint と同じ形式で書く。止める段は warning、報告だけの段と同じ形の単位は note。
func GateXcode(w io.Writer, s GateSummary) error {
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

func commentAt(el exclude.Element) string {
	if el.Comment == "" {
		return ""
	}
	return " (" + el.Comment + " のコメント)"
}

func short(h string) string { return h[:min(len(h), 12)] }

// AppendRecord は 1 回の gate の結果を、時刻を添えた 1 行の JSON として path に追記する (回をまたいで違反・除外・許可を数えるため)。
// 1 行を 1 回の write で書く (同じファイルへ並行に追記しても行が混ざらない。O_APPEND の 1 回の write)。
func AppendRecord(path string, s GateSummary, now time.Time) error {
	b, err := json.Marshal(struct {
		Time string `json:"time"`
		GateSummary
	}{now.Format(time.RFC3339), s})
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
