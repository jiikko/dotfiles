package svc

import (
	"errors"
	"fmt"
	"io/fs"
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
		v, ok := raw[k]
		if !ok || v == nil {
			continue
		}
		if k == "KeepAlive" && !keepAliveRestartsOnFailure(v) {
			continue
		}
		j.restartKeys = append(j.restartKeys, k)
	}
	return j, nil
}

// keepAliveRestartsOnFailure は KeepAlive の値が「異常終了 (正の exit code) の後に再起動する」意味か。
//   - true → 常に再起動 / false → しない
//   - dict: SuccessfulExit=false (異常終了時に再起動) なら該当。SuccessfulExit=true は正常終了時のみ、
//     Crashed=true はクラッシュ (シグナル) 時のみで、正の exit code では再起動しない → 該当しない。
//     PathState / OtherJobEnabled 等の条件付きは判定できないので該当させない (偽陽性側に倒さない)。
//     (敵対レビュー 2026-09-02: dict を一律「再起動条件あり」と読んで B の偽陽性を作っていた)
func keepAliveRestartsOnFailure(v any) bool {
	switch kv := v.(type) {
	case bool:
		return kv
	case map[string]any:
		if se, ok := kv["SuccessfulExit"].(bool); ok && !se {
			return true
		}
		return false
	}
	return false
}

// stdPath は launchd が ProgramArguments[0] の相対名を解決する検索パス (_PATH_STDPATH)。
// launchd は呼び出し側の $PATH を見ないので、ここで $PATH を使ってはいけない。
// paths.h の定義と一致することは TestStdPathMatchesPathsH が固定する。
const stdPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// execTarget は A の判定に使う実行ファイルのパスと、判定結果。
type execTarget struct {
	Path     string // 判定に使ったパス (相対名は解決後。解決できなければ元の相対名)
	Skip     bool   // 判定対象外 (BundleProgram のみ / 実行対象の指定が無い)
	Missing  bool   // 不在 = 壊れて確定 (ErrNotExist のときだけ)
	Relative bool   // 相対名だった (解決を試みた)
	// Unknown は stat が不在以外の理由で失敗した (EACCES 等)。「実行ファイルがありません」と断定できないので
	// 診断できずにする (root 700 の dir 配下の Program を不在扱いして sudo rm を提示していた。敵対レビュー 2026-09-02)
	Unknown error
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
		return statTarget(j.Program, false, stat)
	}
	if len(j.ProgramArguments) > 0 && j.ProgramArguments[0] != "" {
		first := j.ProgramArguments[0]
		if filepath.IsAbs(first) {
			return statTarget(first, false, stat)
		}
		var unknown error
		for _, dir := range strings.Split(stdPath, ":") {
			cand := filepath.Join(dir, first)
			err := stat(cand)
			if err == nil {
				return execTarget{Path: cand, Relative: true}
			}
			if !errors.Is(err, fs.ErrNotExist) {
				unknown = err
			}
		}
		if unknown != nil {
			return execTarget{Path: first, Relative: true, Unknown: unknown}
		}
		return execTarget{Path: first, Relative: true, Missing: true}
	}
	return execTarget{Skip: true}
}

func statTarget(p string, relative bool, stat func(string) error) execTarget {
	err := stat(p)
	switch {
	case err == nil:
		return execTarget{Path: p, Relative: relative}
	case errors.Is(err, fs.ErrNotExist):
		return execTarget{Path: p, Relative: relative, Missing: true}
	default:
		return execTarget{Path: p, Relative: relative, Unknown: err}
	}
}
