package svc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

// job は launchd.plist から判定に要るキーだけを取り出したもの。
type job struct {
	Label            string
	Program          string
	ProgramArguments []string
	BundleProgram    string
	// restartKeys は再起動条件を持つキーのうち plist に存在したもの (B の前提)。
	restartKeys []string
	RunAtLoad   bool
}

// restartKeyNames は「失敗しても launchd が再び起動する」条件。RunAtLoad は含めない:
// RunAtLoad 単独はログイン時に一度起動して終わりで、失敗が繰り返されない (issue 148 codex 反証)。
var restartKeyNames = []string{"KeepAlive", "StartInterval", "StartCalendarInterval", "WatchPaths", "QueueDirectories"}

// parseJob は plist を読む。XML / binary の両形式を受ける。壊れていれば error (呼び出し側が
// その 1 件だけを「診断できず」にする)。
func parseJob(path string) (job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return job{}, err
	}
	var raw map[string]any
	if _, err := plist.Unmarshal(data, &raw); err != nil {
		return job{}, fmt.Errorf("plist を解釈できない: %w", err)
	}
	j := job{}
	j.Label, _ = raw["Label"].(string)
	if j.Label == "" {
		// Label 欠落は launchd 側も拒否するが、表示上はファイル名で追える
		j.Label = strings.TrimSuffix(filepath.Base(path), ".plist")
	}
	j.Program, _ = raw["Program"].(string)
	j.BundleProgram, _ = raw["BundleProgram"].(string)
	if args, ok := raw["ProgramArguments"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				j.ProgramArguments = append(j.ProgramArguments, s)
			}
		}
	}
	j.RunAtLoad, _ = raw["RunAtLoad"].(bool)
	for _, k := range restartKeyNames {
		if v, ok := raw[k]; ok && v != nil {
			// KeepAlive は false でも「キーがある」だけでは再起動条件にならない
			if b, isBool := v.(bool); isBool && !b {
				continue
			}
			j.restartKeys = append(j.restartKeys, k)
		}
	}
	return j, nil
}

// stdPath は launchd が ProgramArguments[0] の相対名を解決する検索パス (_PATH_STDPATH)。
// launchd は呼び出し側の $PATH を見ないので、ここで $PATH を使ってはいけない。
// paths.h の定義と一致することは TestStdPathMatchesPathsH が固定する。
const stdPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// execTarget は A の判定に使う実行ファイルのパスと、判定結果。
type execTarget struct {
	Path     string // 判定に使ったパス (相対名は解決後。解決できなければ元の相対名)
	Skip     bool   // 判定対象外 (BundleProgram のみ / 実行対象の指定が無い)
	Missing  bool   // 不在 = 壊れて確定
	Relative bool   // 相対名だった (解決を試みた)
}

// resolveExecTarget は man 5 launchd.plist の規則で起動対象を決める。
//
//   - Program があれば絶対パス必須。相対なら launchd も起動できないので不在扱い
//   - Program が無ければ ProgramArguments[0]。絶対パスならそのまま、相対名なら stdPath で解決
//   - BundleProgram だけなら判定しない (SMAppService の app-bundle 相対パスで規則が別)
//   - どれも無ければ判定しない (launchd が拒否する登録で、ここで壊れていると断定する材料が無い)
func resolveExecTarget(j job, stat func(string) error) execTarget {
	if j.Program != "" {
		if !filepath.IsAbs(j.Program) {
			return execTarget{Path: j.Program, Missing: true}
		}
		return execTarget{Path: j.Program, Missing: stat(j.Program) != nil}
	}
	if len(j.ProgramArguments) > 0 && j.ProgramArguments[0] != "" {
		first := j.ProgramArguments[0]
		if filepath.IsAbs(first) {
			return execTarget{Path: first, Missing: stat(first) != nil}
		}
		for _, dir := range strings.Split(stdPath, ":") {
			cand := filepath.Join(dir, first)
			if stat(cand) == nil {
				return execTarget{Path: cand, Relative: true}
			}
		}
		return execTarget{Path: first, Relative: true, Missing: true}
	}
	return execTarget{Skip: true}
}
