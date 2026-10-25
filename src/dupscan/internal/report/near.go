package report

import (
	"encoding/json"
	"fmt"
	"io"

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// NearMatch は near の一致 1 つ。
type NearMatch struct {
	Level string `json:"level"`
	// Folded は局所の名前を参照しない単位どうしの L3 の一致 (中身を見た一致ではない)。
	Folded bool    `json:"folded,omitempty"`
	Unit   UnitRef `json:"unit"`
}

// NearSummary は near の出力。
type NearSummary struct {
	Target  UnitRef     `json:"target"`
	Matches []NearMatch `json:"matches"`
	// Skipped は、指した単位をその段で比べない理由 (段 → 理由)。
	Skipped map[string]string `json:"skipped"`
	// Excluded は除外 (組の除外・パスの除外) で外した一致の数。
	Excluded int `json:"excluded"`
}

// BuildNear は near の出力を組み立てる。ex の組の除外とパスの除外は、指した単位と相手の 2 人の組に掛ける (report と同じ関数)。
func BuildNear(target *group.Entry, matches []group.Match, opt group.Options, ex *exclude.Set) NearSummary {
	s := NearSummary{Target: ref(target.Unit), Matches: []NearMatch{}, Skipped: map[string]string{}}
	for _, lv := range opt.Levels {
		if why := group.Skip(target, lv, opt); why != "" {
			s.Skipped[lv.String()] = why
		}
	}
	for _, m := range matches {
		pair := []*group.Entry{target, m.Entry}
		if ex != nil && (ex.PathCovers(m.Level, pair) || ex.PresetCovers(m.Level, pair) || ex.GroupExcluded(m.Level, pair)) {
			s.Excluded++
			continue
		}
		u := ref(m.Entry.Unit)
		u.Allowed = ex != nil && ex.Allowed(m.Entry)
		s.Matches = append(s.Matches, NearMatch{Level: m.Level.String(), Folded: m.Folded, Unit: u})
	}
	return s
}

// NearText は near を人が読む形で出す。1 行目に指した単位と件数、続けて一致を 1 行ずつ。
func NearText(w io.Writer, s NearSummary, limit int) error {
	head := fmt.Sprintf("dupscan near: %s:%d %s", s.Target.File, s.Target.Line, label(s.Target))
	if len(s.Matches) == 0 {
		head += ": 近いものはない"
	} else {
		head += fmt.Sprintf(": 近いもの %d", len(s.Matches))
	}
	if s.Excluded > 0 {
		head += fmt.Sprintf("・除外で外した %d", s.Excluded)
	}
	fmt.Fprintln(w, head)
	for _, lv := range norm.Levels {
		if why, ok := s.Skipped[lv.String()]; ok {
			fmt.Fprintf(w, "%s は比べていない: %s\n", lv, why)
		}
	}
	for i, m := range s.Matches {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (-limit 0 か -format json で全部)\n", len(s.Matches)-limit)
			break
		}
		lv := m.Level
		if m.Folded {
			lv += " (局所の名前を参照しない単位どうしの一致)"
		}
		fmt.Fprintf(w, "%s %s:%d %s%s\n", lv, m.Unit.File, m.Unit.Line, label(m.Unit), allowedMark(m.Unit))
	}
	return nil
}

// ExplainLevel は explain の段 1 つ。
type ExplainLevel struct {
	Level string `json:"level"`
	Hash  string `json:"hash"`
	// Skip は、その段で比べない理由 (比べるなら空)。
	Skip string `json:"skip,omitempty"`
	Form string `json:"form"`
}

// ExplainSummary は explain の出力。
type ExplainSummary struct {
	Target   UnitRef        `json:"target"`
	Nodes    int            `json:"nodes"`
	MinNodes int            `json:"min_nodes"`
	Levels   []ExplainLevel `json:"levels"`
}

// BuildExplain は explain の出力を組み立てる。
func BuildExplain(e *group.Entry, opt group.Options) ExplainSummary {
	s := ExplainSummary{Target: ref(e.Unit), Nodes: e.Result.Nodes, MinNodes: group.MinNodes(e, opt)}
	for _, lv := range norm.Levels {
		s.Levels = append(s.Levels, ExplainLevel{Level: lv.String(), Hash: short(e.Result.Hash[lv]), Skip: group.Skip(e, lv, opt), Form: e.Result.Forms[lv]})
	}
	return s
}

// ExplainText は explain を人が読む形で出す。
func ExplainText(w io.Writer, s ExplainSummary) error {
	fmt.Fprintf(w, "dupscan explain: %s:%d %s (節 %d・下限 %d)\n", s.Target.File, s.Target.Line, label(s.Target), s.Nodes, s.MinNodes)
	for _, l := range s.Levels {
		fmt.Fprintf(w, "%s %s", l.Level, l.Hash)
		if l.Skip != "" {
			fmt.Fprintf(w, " (比べない: %s)", l.Skip)
		}
		fmt.Fprintf(w, "\n  %s\n", l.Form)
	}
	return nil
}

// WriteJSON は v を字下げした JSON で出す。
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
