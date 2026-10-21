package tmpclean

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cmdTimeout = 60 * time.Second

// liveKey は生きている claude の project 名 (scratchpad の根の直下の dir 名)。prefix は 200 字を超えて切り詰められた名前で、
// 本体は先頭 200 字 + -<hash> を使う (hash は再現しないので、先頭 200 字の前方一致で守る)。
type liveKey struct {
	key    string
	prefix bool
	pid    int
}

func (k liveKey) matches(project string) bool {
	if k.prefix {
		return strings.HasPrefix(project, k.key)
	}
	return project == k.key
}

type liveInfo struct {
	projects []liveKey
	ids      map[string]bool
}

func (l liveInfo) projectLive(project string) bool {
	for _, k := range l.projects {
		if k.matches(project) {
			return true
		}
	}
	return false
}

// projectKey は cwd から Claude Code 本体と同じ規則で project 名を導く: 英数字以外を - にし、200 字を超えたら切り詰める。
// ok=false は判定不能: lsof はロケールによって ASCII 以外や制御文字を \xNN・\t の形で返す (\ を含む)。ASCII 以外は本体
// (UTF-16 の単位で置き換える) とバイト単位の置き換えで結果がずれうる。どちらも導いた名前が本物と食い違うので、守れない。
func projectKey(cwd string) (key string, prefix, ok bool) {
	if cwd == "" || strings.ContainsRune(cwd, '\\') {
		return "", false, false
	}
	b := []byte(cwd)
	for i, c := range b {
		if c < 0x20 || c >= 0x7f {
			return "", false, false
		}
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '-'
		}
	}
	if len(b) > 200 {
		return string(b[:200]), true, true
	}
	return string(b), false, true
}

var (
	versionComm = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	// 拾いすぎは守りすぎになるだけなので広めに取る: 版の実体の置き場・npm の package 名・パスの最後の要素が claude
	claudeCommand = regexp.MustCompile(`claude-code|/claude/versions/|/claude( |$)`)
	argvSessionID = regexp.MustCompile(`(?:--resume|--session-id|-r)[= ]([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	envSessionID  = regexp.MustCompile(`CLAUDE_CODE_SESSION_ID=([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	envPWD        = regexp.MustCompile(` PWD=([^ ]+)`)
	// 実行ファイルのパス (空白を含まない) + --chrome-native-host だけ。引数が他にあれば普通のセッション
	chromeNativeHost = regexp.MustCompile(`^[^ ]*/claude --chrome-native-host$`)
)

type proc struct {
	ppid    int
	command string
}

// runOK は外部コマンドを上限つきで呼び、起動の失敗・期限切れ・想定外の rc を stopError にする。okRC は受け入れる rc。
func runOK(ctx context.Context, d deps, okRC []int, name string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	out, errOut, rc, err := d.run(ctx, name, args...)
	if err != nil {
		return "", rc, stopf("%s を実行できない: %v", name, err)
	}
	for _, ok := range okRC {
		if rc == ok {
			return out, rc, nil
		}
	}
	return "", rc, stopf("%s が rc=%d で失敗した: %s", name, rc, strings.TrimSpace(errOut))
}

// pidRest は "  123 rest..." の行を pid と残りに分ける。
func pidRest(line string) (int, string, bool) {
	line = strings.TrimLeft(line, " ")
	i := strings.IndexByte(line, ' ')
	if i < 0 {
		pid, err := strconv.Atoi(line)
		return pid, "", err == nil
	}
	pid, err := strconv.Atoi(line[:i])
	return pid, strings.TrimLeft(line[i+1:], " "), err == nil
}

// collectLive は生きている claude の project と、生きているセッションの ID を集める。
// 判定不能 (cwd が読めない・導けない・project が無い・起動時の PWD とずれる・自分の確認が合わない) は stopError。
func collectLive(ctx context.Context, d deps, r roots) (liveInfo, error) {
	info := liveInfo{ids: map[string]bool{}}
	if d.selfSID == "" {
		return info, stopf("CLAUDE_CODE_SESSION_ID が空 (自分のセッションを確かめられない)")
	}
	info.ids[d.selfSID] = true

	out, _, err := runOK(ctx, d, []int{0}, "ps", "-Ao", "pid=,ppid=,command=")
	if err != nil {
		return info, err
	}
	procs := map[int]proc{}
	claude := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		pid, rest, ok := pidRest(line)
		if !ok || rest == "" {
			continue
		}
		ppid, command, _ := pidRest(rest)
		procs[pid] = proc{ppid: ppid, command: command}
		if claudeCommand.MatchString(command) {
			claude[pid] = true
		}
		if m := argvSessionID.FindStringSubmatch(command); m != nil {
			info.ids[m[1]] = true
		}
	}
	if len(procs) == 0 {
		return info, stopf("ps の出力からプロセスを 1 つも読めない")
	}
	// UCOMM が版番号の形 = 版の実体を直接起動した claude (argv[0] が claude でない)
	out, _, err = runOK(ctx, d, []int{0}, "ps", "-Ao", "pid=,ucomm=")
	if err != nil {
		return info, err
	}
	for _, line := range strings.Split(out, "\n") {
		if pid, comm, ok := pidRest(line); ok && versionComm.MatchString(comm) {
			claude[pid] = true
		}
	}
	// pgrep は既定で自分と祖先を外すので -a。-x は argv[0] の名前で照合する。rc=1 は「該当なし」
	out, _, err = runOK(ctx, d, []int{0, 1}, "pgrep", "-a", "-x", "claude")
	if err != nil {
		return info, err
	}
	for _, f := range strings.Fields(out) {
		if pid, err := strconv.Atoi(f); err == nil {
			claude[pid] = true
		}
	}
	// 環境変数: 子孫のシェルが持つ CLAUDE_CODE_SESSION_ID (claude 本体は持たない) と、CLI から起動した claude の起動時の PWD
	out, _, err = runOK(ctx, d, []int{0}, "ps", "eww", "-Ao", "pid=,command=")
	if err != nil {
		return info, err
	}
	pwd := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		pid, rest, ok := pidRest(line)
		if !ok {
			continue
		}
		for _, m := range envSessionID.FindAllStringSubmatch(rest, -1) {
			info.ids[m[1]] = true
		}
		if claude[pid] {
			if m := envPWD.FindStringSubmatch(rest); m != nil {
				pwd[pid] = m[1]
			}
		}
	}

	pids := make([]int, 0, len(claude))
	for pid := range claude {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		p, known := procs[pid]
		if known && chromeNativeHost.MatchString(p.command) {
			// ブラウザ連携の常駐 (<実行ファイル> --chrome-native-host だけの形)。scratchpad を持たない。🚨 語が含まれるか・末尾にあるかでは
			// 判定しない: ps の command は引用符を落とすので、`claude "直して --chrome-native-host"` で起動したセッションまで守りから外れる
			continue
		}
		cwd, err := processCwd(ctx, d, pid)
		if err != nil {
			return info, err
		}
		if cwd == "" {
			continue // 調べている間に終わった
		}
		key, prefix, ok := projectKey(cwd)
		if !ok {
			return info, stopf("claude (%d) の cwd から project 名を確実に導けない: %q", pid, cwd)
		}
		if !projectExists(r.session, key, prefix) {
			return info, stopf("claude (%d) の cwd (%s) に当たる project が scratchpad に無い (起動後に cwd を移した claude を守れない恐れ)", pid, cwd)
		}
		if p0, ok := pwd[pid]; ok && filepath.Clean(p0) != filepath.Clean(cwd) {
			return info, stopf("claude (%d) の cwd (%s) が起動時の PWD (%s) と違う (起動後に cwd を移した claude を守れない恐れ)", pid, cwd, p0)
		}
		info.projects = append(info.projects, liveKey{key: key, prefix: prefix, pid: pid})
	}
	if err := checkSelf(d, r, procs, claude, info); err != nil {
		return info, err
	}
	return info, nil
}

// processCwd は pid の cwd。プロセスが既に無ければ "" (止めない)。生きているのに読めなければ stopError。
func processCwd(ctx context.Context, d deps, pid int) (string, error) {
	ps := strconv.Itoa(pid)
	out, rc, err := runOK(ctx, d, []int{0, 1}, "lsof", "-a", "-d", "cwd", "-p", ps, "-Fn")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:], nil
		}
	}
	if rc == 1 {
		alive, _, err := runOK(ctx, d, []int{0, 1}, "ps", "-p", ps, "-o", "pid=")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(alive) == "" {
			return "", nil
		}
	}
	return "", stopf("claude (%d) の cwd を読めない", pid)
}

func projectExists(sessionRoot, key string, prefix bool) bool {
	if !prefix {
		fi, err := os.Lstat(filepath.Join(sessionRoot, key))
		return err == nil && fi.IsDir()
	}
	ents, err := os.ReadDir(sessionRoot)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), key) {
			return true
		}
	}
	return false
}

// checkSelf は導出が壊れていないかを自分で確かめる: 祖先をたどって見つけた自分の claude の project 名が、
// 自分の scratchpad の project と一致するか。一致しなければ、他の claude の導出も信用しない。
func checkSelf(d deps, r roots, procs map[int]proc, claude map[int]bool, info liveInfo) error {
	matches, _ := filepath.Glob(filepath.Join(r.session, "*", d.selfSID))
	if len(matches) != 1 {
		return stopf("自分の scratchpad (%s) が根の下に 1 つだけ見つからない (%d 個)", d.selfSID, len(matches))
	}
	own := filepath.Base(filepath.Dir(matches[0]))
	mine := 0
	for p, n := d.selfPID, 0; p > 1 && n < 64; n++ {
		if claude[p] {
			mine = p
			break
		}
		p = procs[p].ppid
	}
	if mine == 0 {
		return stopf("祖先に claude が見つからない (claude の拾い方の失敗)")
	}
	for _, k := range info.projects {
		if k.pid == mine {
			if !k.matches(own) {
				return stopf("自分の claude (%d) の cwd から導いた名前 (%s) が自分の project (%s) と違う", mine, k.key, own)
			}
			return nil
		}
	}
	return stopf("自分の claude (%d) の project を導けていない", mine)
}

// collectProcNames は動いているプロセスの名前 (実行ファイルの basename と、16 字に切られた UCOMM) の集合。
func collectProcNames(ctx context.Context, d deps) (map[string]bool, error) {
	names := map[string]bool{}
	for _, col := range []string{"comm=", "ucomm="} {
		out, _, err := runOK(ctx, d, []int{0}, "ps", "-Ao", "pid="+","+col)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			if _, v, ok := pidRest(line); ok && v != "" {
				names[filepath.Base(v)] = true
			}
		}
	}
	if len(names) == 0 {
		return nil, stopf("ps からプロセスの名前を 1 つも読めない")
	}
	return names, nil
}

// openSet は lsof で開かれているパスの写し。
type openSet struct{ sorted []string }

// normalizeLsofPath は lsof が返す形 (/var/...・/private/var/...・/System/Volumes/Data/private/...) を /private 付きに揃える。
func normalizeLsofPath(p string) string {
	p = strings.TrimPrefix(p, "/System/Volumes/Data")
	for _, pre := range []string{"/var/", "/tmp/"} {
		if strings.HasPrefix(p, pre) {
			return "/private" + p
		}
	}
	return p
}

func collectOpen(ctx context.Context, d deps) (openSet, error) {
	out, _, err := runOK(ctx, d, []int{0, 1}, "lsof", "+c", "0", "-b", "-w", "-Fn")
	if err != nil {
		return openSet{}, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "n/") {
			seen[normalizeLsofPath(line[1:])] = true
		}
	}
	// 本物の macOS で開かれているパスが 100 に満たないことは無い。少なければ lsof が途中で失敗した
	if len(seen) < 100 {
		return openSet{}, stopf("lsof が開かれているパスを %d 件しか返さない (失敗とみなす)", len(seen))
	}
	s := openSet{sorted: make([]string, 0, len(seen))}
	for p := range seen {
		s.sorted = append(s.sorted, p)
	}
	sort.Strings(s.sorted)
	return s, nil
}

// has は p 自体か、その中のどれかが開かれているか。
func (s openSet) has(p string) bool {
	i := sort.SearchStrings(s.sorted, p)
	if i < len(s.sorted) && s.sorted[i] == p {
		return true
	}
	j := sort.SearchStrings(s.sorted, p+"/")
	return j < len(s.sorted) && strings.HasPrefix(s.sorted[j], p+"/")
}

type openState int

const (
	notOpen openState = iota
	isOpen
	openUnknown
)

// openNow は移す直前の 1 件の取り直し。lsof 4.91 の実測 (2026-10-21):
//   - +D <dir> は開いているものがあっても **rc=1** で stdout に出す。-- <file> は rc=0 で出す。どちらも無ければ rc=1・出力なし
//   - -b を付けると +D は usage、-- は "status error" で、開いているかに関係なく常に失敗する (付けない)
//
// なので stdout に出たら rc に関係なく isOpen、rc=1 で stdout も stderr も空なら notOpen、それ以外 (stderr あり・期限切れ・
// 起動失敗) は openUnknown (残し、移せなかったものとして数える)。
func openNow(ctx context.Context, d deps, path string, dir bool) openState {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	args := []string{"-w", "-Fn"}
	if dir {
		args = append(args, "+D", path)
	} else {
		args = append(args, "--", path)
	}
	out, errOut, rc, err := d.run(ctx, "lsof", args...)
	switch {
	case err != nil:
		return openUnknown
	case strings.Contains(out, "\nn/") || strings.HasPrefix(out, "n/"):
		return isOpen
	case rc == 1 && out == "" && errOut == "":
		return notOpen
	default:
		return openUnknown
	}
}
