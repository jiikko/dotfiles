package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ciBatchSize = 30
const ciCacheTTL = 60 * time.Second

const (
	ciJobSuccess = '\u2713'
	ciJobFailure = '\u2717'
	ciJobRunning = '\u25cf'
	ciJobSkipped = '\u25cb'
)

type CIState int

const (
	CIUnknown CIState = iota
	CISuccess
	CIFailure
	CIPending
	CINone
)

func loadCI(commits []Commit) map[string]CIState {
	count := min(len(commits), ciBatchSize)
	if count == 0 {
		return map[string]CIState{}
	}
	shas := make([]string, count)
	for i := range shas {
		shas[i] = commits[i].SHA
	}
	// owner/name はキャッシュ読み取り前に確定する。同一 SHA/件数でも repo (fork 含む) が
	// 違えば CI は別物なのでキーに含める必要がある。gh 無し/非 GitHub はここで degrade。
	owner, name, err := ghRepo()
	if err != nil {
		return map[string]CIState{}
	}
	if cached, ok := readCICache(owner, name, shas[0], count); ok {
		return cached
	}
	result, err := ghGraphQL(owner, name, shas)
	if err != nil {
		return map[string]CIState{} // 失敗結果はキャッシュしない (次回再試行できるように)
	}
	writeCICache(owner, name, shas[0], count, result)
	return result
}

func ghRepo() (string, string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "owner,name", "--jq", ".owner.login+\"\\t\"+.name")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", "", err
	}
	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
		return "", "", os.ErrInvalid
	}
	return fields[0], fields[1], nil
}

// loadCIJobsPreview は GitHub の check-runs を best-effort で取得する。
// gh が無い、remote が GitHub でない、または API が失敗した場合は空文字を返す。
func loadCIJobsPreview(sha string) string {
	owner, name, err := ghRepo()
	if err != nil {
		return ""
	}
	path := ciJobsCachePath(owner, name, sha)
	if path != "" {
		if data, ok := readCIJobsCache(path); ok {
			return formatCIJobs(string(data))
		}
	}

	cmd := exec.Command("gh", "api", "repos/"+owner+"/"+name+"/commits/"+sha+"/check-runs",
		"--jq", `.check_runs[] | "\(.conclusion // .status)\t\(.name)"`)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// 取得失敗は CI 無しとして扱い git show の preview を阻害しない。ただし失敗結果は
		// キャッシュしない (一時的な失敗で 60 秒 CI が消えたままにならないよう次回再試行)。
		return ""
	}
	if path != "" {
		writeCIJobsCache(path, out.Bytes())
	}
	return formatCIJobs(out.String())
}

func classifyCIJob(state string) rune {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success":
		return ciJobSuccess
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
		return ciJobFailure
	case "skipped", "neutral":
		return ciJobSkipped
	default:
		return ciJobRunning
	}
}

func colorCIJobMark(mark rune) string {
	color := "38;5;3"
	switch mark {
	case ciJobSuccess:
		color = "38;5;2"
	case ciJobFailure:
		color = "38;5;1"
	case ciJobSkipped:
		return "\x1b[2m" + string(mark) + "\x1b[0m"
	}
	return "\x1b[" + color + "m" + string(mark) + "\x1b[0m"
}

// formatCIJobs は gh の jq 整形済み出力を preview 用ブロックへ変換する。
func formatCIJobs(raw string) string {
	raw = strings.TrimRight(raw, "\r\n")
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b[2m── CI ──\x1b[0m\n")
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.SplitN(strings.TrimSuffix(line, "\r"), "\t", 2)
		if len(fields) != 2 || fields[1] == "" {
			continue
		}
		b.WriteString("  ")
		b.WriteString(colorCIJobMark(classifyCIJob(fields[0])))
		b.WriteString(" ")
		b.WriteString(fields[1])
		b.WriteString("\n")
	}
	if b.Len() == len("\x1b[2m── CI ──\x1b[0m\n") {
		return ""
	}
	b.WriteString("\x1b[2m──────────\x1b[0m\n\n")
	return b.String()
}

func ciJobsCachePath(owner, name, sha string) string {
	cacheRoot, err := os.UserCacheDir()
	if custom := os.Getenv("XDG_CACHE_HOME"); custom != "" {
		cacheRoot = custom
	} else if err != nil {
		return ""
	}
	key := sha256.Sum256([]byte(owner + "\x00" + name + "\x00" + sha))
	return filepath.Join(cacheRoot, "git-popup", "ci-jobs-"+hex.EncodeToString(key[:])+".txt")
}

func readCIJobsCache(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > ciCacheTTL {
		return nil, false
	}
	data, err := os.ReadFile(path)
	return data, err == nil
}

func writeCIJobsCache(path string, data []byte) {
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ci-jobs-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err == nil {
		_ = os.Rename(tmpName, path)
	}
}

func ghGraphQL(owner, name string, shas []string) (map[string]CIState, error) {
	query := buildCIQuery(owner, name, shas)
	cmd := exec.Command("gh", "api", "graphql", "-F", "owner="+owner, "-F", "name="+name, "-f", "query="+query)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return parseCIResponse(out.Bytes(), shas)
}

func buildCIQuery(owner, name string, shas []string) string {
	var b strings.Builder
	b.WriteString(`query($owner:String!,$name:String!){repository(owner:$owner,name:$name){`)
	for i, sha := range shas {
		// alias ブロック間は空白で区切る。無いと GraphQL が `...}}}c1:` を
		// malformed と解釈して "Expected string or block string" で全体が失敗する (実測)。
		b.WriteString(" c")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`:object(oid:"`)
		b.WriteString(sha)
		b.WriteString(`"){...on Commit{statusCheckRollup{state}}}`)
	}
	b.WriteString("}}")
	return b.String()
}

type ciResponse struct {
	Data struct {
		Repository map[string]struct {
			StatusCheckRollup *struct {
				State string `json:"state"`
			} `json:"statusCheckRollup"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// parseCIResponse は gh api graphql の JSON を map へ変換する。GraphQL は HTTP 成功でも
// body に errors を含めうる (query 不正・rate limit・部分失敗) ため、errors や
// data.repository 欠落はエラーとして返し、呼び出し側が「失敗をキャッシュしない」判断に使う。
func parseCIResponse(data []byte, shas []string) (map[string]CIState, error) {
	var response ciResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if len(response.Errors) > 0 {
		return nil, errors.New("graphql: " + response.Errors[0].Message)
	}
	if response.Data.Repository == nil {
		return nil, errors.New("graphql: data.repository が空")
	}
	result := make(map[string]CIState, len(shas))
	for i, sha := range shas {
		entry, ok := response.Data.Repository["c"+strconv.Itoa(i)]
		if !ok || entry.StatusCheckRollup == nil {
			result[sha] = CINone
			continue
		}
		result[sha] = classifyCIState(entry.StatusCheckRollup.State)
	}
	return result, nil
}

func classifyCIState(state string) CIState {
	switch state {
	case "SUCCESS":
		return CISuccess
	case "FAILURE", "ERROR":
		return CIFailure
	case "PENDING", "EXPECTED":
		return CIPending
	default:
		return CINone
	}
}

func ciCachePath(owner, name, firstSHA string, count int) string {
	cacheRoot, err := os.UserCacheDir()
	if custom := os.Getenv("XDG_CACHE_HOME"); custom != "" {
		cacheRoot = custom
	} else if err != nil {
		return ""
	}
	// repo (owner/name) をキーに含める。同一 SHA/件数でも fork 等で CI は異なるため。
	key := sha256.Sum256([]byte(owner + "\x00" + name + "\x00" + firstSHA + "\x00" + strconv.Itoa(count)))
	return filepath.Join(cacheRoot, "git-popup", hex.EncodeToString(key[:])+".json")
}

type ciCache struct {
	Created int64              `json:"created"`
	States  map[string]CIState `json:"states"`
}

func readCICache(owner, name, firstSHA string, count int) (map[string]CIState, bool) {
	path := ciCachePath(owner, name, firstSHA, count)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached ciCache
	if json.Unmarshal(data, &cached) != nil || time.Since(time.Unix(0, cached.Created)) > ciCacheTTL {
		return nil, false
	}
	return cached.States, true
}

func writeCICache(owner, name, firstSHA string, count int, states map[string]CIState) {
	path := ciCachePath(owner, name, firstSHA, count)
	if path == "" {
		return
	}
	data, err := json.Marshal(ciCache{Created: time.Now().UnixNano(), States: states})
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ci-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	}
	if err == nil {
		_ = os.Rename(tmpName, path)
	} else {
		_ = tmp.Close()
	}
}
