// zundamon-kaisetsu は VOICEVOX で台本を合成し、四国めたんとずんだもんの掛け合い動画 (HTML プレイヤー / mp4) を作る。
//
// 使い方は skill の _claude/skills/zundamon-kaisetsu/SKILL.md。bin/zundamon-kaisetsu から起動する
// (skill のディレクトリを環境変数 ZUNDAMON_KAISETSU_SKILL_DIR で渡し、テンプレートと立ち絵の既定の置き場をそこから読む)。
//
//	zundamon-kaisetsu check                          # 必要なコマンドとエンジンの状態を確かめる
//	zundamon-kaisetsu up / down                      # エンジンをコンテナで起動 / 停止 (container を優先、無ければ docker)
//	zundamon-kaisetsu speakers                       # 話者とスタイル ID の一覧
//	zundamon-kaisetsu kana "文" … / kana --script script.json  # 文か台本の全行の読み (合成はしない)
//	zundamon-kaisetsu synth  script.json             # セリフごとに wav を作る (キャッシュあり)
//	zundamon-kaisetsu build  script.json -o out --format html|mp4|both  # 連結・口パク・HTML / mp4 化
//
// HTML は音声 1 本 + タイムライン JSON をブラウザで再生し、字幕・立ち絵・口パクを audio.currentTime から毎フレーム描く
// (シークしても音と絵がずれない)。mp4 はそのプレイヤーの描画モードを Chrome で撮って並べる。
//
// Python 版 (dialogue_video.py) から書き直した (issue 641)。合成のキャッシュの鍵と build のデータは Python 版と
// 同じ値になるよう作っていて、testdata/ の golden (Python 版から生成) で突き合わせている。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const skillDirEnv = "ZUNDAMON_KAISETSU_SKILL_DIR"

// Env は外の世界 (出力先・エンジン・skill の置き場・時計) をまとめたもの。テストで差し替える。
type Env struct {
	Engine   string
	SkillDir string
	// AssetsFaces は立ち絵の既定の置き場 (skill のディレクトリからの相対)。素材の再配布になるので公開リポジトリには置かない。
	// dotfiles では非公開リポジトリをここにサブモジュールとして置いている。それ以外の環境は README の Setup で用意する
	AssetsFaces    string
	Stdout, Stderr io.Writer
	Now            func() time.Time
	// EngineUp / EngineDown / SpawnReaper はエンジンの自動起動と、使われなくなったら止める見張り (engine_auto.go)。
	// nil なら自動で管理しない (テストは本物のコンテナに触れない)。StateDir は印・最後に使った時刻・見張りのロックの置き場
	EngineUp    func() error
	EngineDown  func(runtime string) error
	SpawnReaper func() error
	StateDir    string
	Sleep       func(time.Duration)
	LoadAvg     func() (float64, bool)
}

// Template はプレイヤーのテンプレート。
func (e *Env) Template() string { return filepath.Join(e.SkillDir, "templates", "player.html") }

func newEnv() *Env {
	engine := "http://127.0.0.1:50021"
	if v, ok := os.LookupEnv("VOICEVOX_URL"); ok {
		engine = v // Python 版と同じく、空文字列でも上書きする
	}
	e := &Env{Engine: engine, Stdout: os.Stdout, Stderr: os.Stderr, Now: time.Now, Sleep: sleepCtx, LoadAvg: loadAvg1}
	e.setSkillDir(os.Getenv(skillDirEnv))
	e.StateDir = defaultStateDir()
	e.EngineUp = func() error { return startEngine(e) }
	e.EngineDown = func(rt string) error {
		ctx, cancel := cleanupContext()
		defer cancel()
		_, err := stopContainers(ctx, rt)
		return err
	}
	e.SpawnReaper = func() error { return spawnReaper(e) }
	return e
}

func (e *Env) setSkillDir(d string) {
	e.SkillDir = d
	e.AssetsFaces = ""
	if d != "" {
		e.AssetsFaces = filepath.Join(d, "assets", "zundamon-kaisetsu", "faces")
	}
}

// requireSkillDir は skill のディレクトリが要るコマンドで、それが渡されていなければ止める。
// 黙って続けると、立ち絵の既定の置き場を見ずに丸アバターの動画ができる (issue 641 の設計レビュー P2-10)。
func (e *Env) requireSkillDir() error {
	if e.SkillDir == "" {
		return fail("%s が無い。bin/zundamon-kaisetsu から起動する (skill のディレクトリをこの環境変数で渡す)", skillDirEnv)
	}
	if !isFile(e.Template()) {
		return fail("%s=%s に templates/player.html が無い (skill のディレクトリを指していない)", skillDirEnv, e.SkillDir)
	}
	return nil
}

// --- 中断 (Ctrl-C) ---
//
// appCtx は SIGINT / SIGTERM / SIGHUP で取り消される。外部コマンド・HTTP・待ちはすべてこれに結び付ける。取り消されると
// Go が子プロセスを止め、Wait は子が終わってから戻るので、呼び出し側の defer (一時ディレクトリ・一時ファイルの削除) は
// 子が止まった後に、後から登録したものから順に走る。Python 版の KeyboardInterrupt と同じ順序 (敵対的レビュー 2 周目 P2-1。
// 自前の後始末の登録表は、子を止める前に一時ディレクトリを消しにいき、書き続ける子に消し残された)。
// 後始末の後は同じ signal を受け直して死ぬ (rc=130 で普通に終えると、bash のループが Ctrl-C で止まらない。同 P2-2)。
var appCtx = context.Background()

// sleepCtx は appCtx が取り消されたら待つのをやめる time.Sleep。
func sleepCtx(d time.Duration) {
	select {
	case <-appCtx.Done():
	case <-time.After(d):
	}
}

// errInterrupted は中断で処理を打ち切ったことを表す (main が同じ signal で死に直す)。
var errInterrupted = errors.New("中断した")

func interruptedErr() error {
	if appCtx.Err() != nil {
		return errInterrupted
	}
	return nil
}

func dieBySignal(s os.Signal) {
	signal.Reset(s)
	if ss, ok := s.(syscall.Signal); ok {
		_ = syscall.Kill(os.Getpid(), ss)
		time.Sleep(2 * time.Second) // 届くまでの間 (届けばここで終わる)
		os.Exit(128 + int(ss))
	}
	os.Exit(130)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	appCtx = ctx
	var got atomic.Value
	sigs := make(chan os.Signal, 2)
	// 起動時に無視されていた signal (nohup の SIGHUP、背景のジョブの SIGINT) は受けない。Notify は無視を解いてしまう (3 周目 P2-2)
	for _, s := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(s) {
			signal.Notify(sigs, s)
		}
	}
	// umask は起動時 (goroutine がファイルを作る前) に 1 度だけ読む。Umask は読むために書き換えるので、後で読むと競合する
	um := syscall.Umask(0)
	syscall.Umask(um)
	fileUmask = os.FileMode(um)
	go func() {
		s := <-sigs
		got.Store(s)
		cancel()
		dieBySignal(<-sigs) // 2 回目は後始末を待たずに死ぬ
	}()
	env := newEnv()
	rc := run(os.Args[1:], env)
	if s, ok := got.Load().(os.Signal); ok {
		fmt.Fprintln(env.Stderr, "zundamon-kaisetsu: 中断した (一時ファイルを片付けて終わる)")
		dieBySignal(s)
	}
	os.Exit(rc)
}

// --- 引数の解析 (Python 版の argparse と同じ意味にする) ---
//
// - オプションと位置引数を混ぜて書ける (build script.json -o out --format mp4 も build -o out script.json も通る)
// - 長いオプションは一意な前方部分に略せる (--form mp4)。--opt=value も受ける
// - --engine はサブコマンドより前にだけ書ける
// - 解析の誤りは rc=2、実行時の失敗は rc=1

type usageError struct{ msg, prog string }

func (e *usageError) Error() string { return e.msg }

type optSpec struct {
	names   []string // 例: {"-o", "--output"}
	dest    string
	takes   bool // 値を取るか
	choices []string
	check   func(string) error
}

type parsed struct {
	opts       map[string]string
	pos        []string
	help       bool
	posGroups  int // 位置引数のまとまりの数 (オプションを挟んで分かれると増える。kana の nargs='*' 用)
	firstGroup int // 最初のまとまりの位置引数の数
	lastPos    int // 直前の位置引数の添字 (まとまりの切れ目を見る)
}

// negNumRe は argparse (3.13 以降) が負の数とみなす形 (- の後に数字か .数字)。
var negNumRe = regexp.MustCompile(`^-\.?\d`)

func exactOpt(specs []optSpec, name string) *optSpec {
	for i := range specs {
		if slices.Contains(specs[i].names, name) {
			return &specs[i]
		}
	}
	return nil
}

// classifyArg は argparse の _parse_optional と同じ順で、a がオプションかを決める。
// 完全一致 → "名前=値" → 前方一致 (長い名前の略記 / 短い名前に値を続けた -oout) → 負の数 → 空白を含む の順。
// 空白を含むかは最後に見る (先に見ると --output="my file" を位置引数と取り違える。敵対的レビュー 2 周目 P2-4)。
// spec が nil で isOpt が真なら未知のオプション。
func classifyArg(a string, specs []optSpec) (spec *optSpec, isOpt bool, val string, hasVal bool, err error) {
	if !strings.HasPrefix(a, "-") || a == "-" {
		return nil, false, "", false, nil
	}
	if s := exactOpt(specs, a); s != nil {
		return s, true, "", false, nil
	}
	if name, v, ok := strings.Cut(a, "="); ok {
		if s := exactOpt(specs, name); s != nil {
			return s, true, v, true, nil
		}
	}
	if strings.HasPrefix(a, "--") {
		name, v, has := strings.Cut(a, "=")
		var hits []*optSpec
		var hitNames []string
		for i := range specs {
			for _, n := range specs[i].names {
				if strings.HasPrefix(n, "--") && strings.HasPrefix(n, name) {
					hits = append(hits, &specs[i])
					hitNames = append(hitNames, n)
				}
			}
		}
		switch {
		case len(hits) == 1:
			return hits[0], true, v, has, nil
		case len(hits) > 1:
			return nil, true, "", false, fmt.Errorf("ambiguous option: %s could match %s", name, strings.Join(hitNames, ", "))
		}
	} else if len(a) > 2 {
		if s := exactOpt(specs, a[:2]); s != nil {
			return s, true, a[2:], true, nil
		}
	}
	if negNumRe.MatchString(a) || strings.Contains(a, " ") {
		return nil, false, "", false, nil
	}
	return nil, true, "", false, nil
}

func parseArgs(args []string, specs []optSpec, stopAtPositional bool) (*parsed, []string, error) {
	p := &parsed{opts: map[string]string{}}
	var unknown []string // argparse は未知の引数の誤りを最後に出す (後ろに -h があればヘルプが勝つ)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if stopAtPositional {
				return p, args[i+1:], checkUnknown(unknown)
			}
			if rest := args[i+1:]; len(rest) > 0 {
				// "--" の後ろは、オプションを挟んだ位置引数と同じく別のまとまりに数える (kana a --who x -- b は argparse では余り。4 周目 P2-2)
				if len(p.pos) == 0 || p.lastPos != i-1 {
					p.posGroups++
				}
				if p.posGroups == 1 {
					p.firstGroup += len(rest)
				}
				p.pos = append(p.pos, rest...)
			}
			break
		}
		if isHelp(a, specs) {
			p.help = true // argparse は -h を見た時点でヘルプを出して終わる
			return p, nil, nil
		}
		spec, isOpt, val, hasVal, err := classifyArg(a, specs)
		if err != nil {
			return nil, nil, err
		}
		if !isOpt {
			if stopAtPositional {
				// サブコマンドより前の未知のオプションも捨てない (--enigne=… の打ち間違いを黙って無視しない。3 周目 P2-1)
				return p, args[i:], checkUnknown(unknown)
			}
			if len(p.pos) == 0 || p.lastPos != i-1 {
				p.posGroups++
			}
			if p.posGroups == 1 {
				p.firstGroup++
			}
			p.pos = append(p.pos, a)
			p.lastPos = i
			continue
		}
		if spec == nil {
			unknown = append(unknown, a)
			continue
		}
		if !spec.takes {
			if hasVal {
				return nil, nil, fmt.Errorf("argument %s: ignored explicit argument '%s'", strings.Join(spec.names, "/"), val)
			}
			p.opts[spec.dest] = "true"
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("argument %s: expected one argument", strings.Join(spec.names, "/"))
			}
			if _, nextIsOpt, _, _, _ := classifyArg(args[i+1], specs); nextIsOpt {
				return nil, nil, fmt.Errorf("argument %s: expected one argument", strings.Join(spec.names, "/"))
			}
			i++
			val = args[i]
		}
		if len(spec.choices) > 0 && !containsStr(spec.choices, val) {
			return nil, nil, fmt.Errorf("argument %s: invalid choice: '%s' (choose from %s)", strings.Join(spec.names, "/"), val, strings.Join(spec.choices, ", "))
		}
		if spec.check != nil {
			if err := spec.check(val); err != nil {
				return nil, nil, fmt.Errorf("argument %s: %v", strings.Join(spec.names, "/"), err)
			}
		}
		p.opts[spec.dest] = val
	}
	return p, nil, checkUnknown(unknown)
}

func checkUnknown(unknown []string) error {
	if len(unknown) > 0 {
		return fmt.Errorf("unrecognized arguments: %s", strings.Join(unknown, " "))
	}
	return nil
}

// isHelp は -h / --help とその略記 (--he など。ほかの長いオプションと紛れないとき) か。
func isHelp(a string, specs []optSpec) bool {
	if a == "-h" || a == "--help" {
		return true
	}
	if len(a) < 3 || !strings.HasPrefix(a, "--") || !strings.HasPrefix("--help", a) {
		return false
	}
	for _, s := range specs {
		for _, n := range s.names {
			if strings.HasPrefix(n, a) {
				return false
			}
		}
	}
	return true
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func jobsArg(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || strings.ContainsAny(v, "+- ") || n < 1 || n > 16 {
		return errors.New("1〜16 で指定する")
	}
	return nil
}

var kbpsRe = regexp.MustCompile(`^(\d+)k?$`)

func kbpsValue(v string) (int, error) {
	m := kbpsRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(v)))
	if m == nil {
		return 0, errors.New("8〜320 の kbps で指定する (例: 64k / 96)")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 8 || n > 320 {
		return 0, errors.New("8〜320 の kbps で指定する (例: 64k / 96)")
	}
	return n, nil
}

func kbpsArg(v string) error { _, err := kbpsValue(v); return err }

func styleIDArg(v string) error {
	if _, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err != nil {
		return fmt.Errorf("invalid int value: '%s'", v)
	}
	return nil
}

const usageText = `usage: zundamon-kaisetsu [--engine URL] {check,up,down,speakers,kana,synth,build} ...

  check     必要なコマンド・コンテナ・エンジンの状態を確かめる (足りなければ rc=1)
  up        エンジンをコンテナ ` + containerName + ` で起動し、応答するまで待つ (down まで動き続ける)
  down      コンテナ ` + containerName + ` を止める
            (synth / kana / speakers は止まっているエンジンを自動で起動し、最後に使ってから 10 分で自動で止める。up は要らない)
  speakers  話者とスタイル ID を一覧する
  kana      文ごとの読み (audio_query の kana) を出す。read の候補を合成せずに比べる
              kana "文" … [--who metan|zundamon] [--style-id ID]  /  kana --script 台本.json
  synth     セリフごとに wav を合成する (<台本名>.work/ にキャッシュ)。synth 台本.json [--force]
  build     合成済みの wav を連結し、HTML プレイヤーか mp4 (か両方) を書き出す
              build 台本.json -o 出力 [--format html|mp4|both] [--jobs N] [--bitrate 64k]

  --engine URL  VOICEVOX エンジンの URL (既定 http://127.0.0.1:50021、環境変数 VOICEVOX_URL)
`

// run は CLI の本体。終了コードを返す (main 以外からも呼べるよう os.Exit しない)。
func run(args []string, env *Env) int {
	err := dispatch(args, env)
	var ue *usageError
	var ee *errExit
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprint(env.Stderr, usageText)
		prog := "zundamon-kaisetsu"
		if ue.prog != "" {
			prog += " " + ue.prog
		}
		fmt.Fprintf(env.Stderr, "%s: error: %s\n", prog, ue.msg)
		return 2
	case errors.Is(err, errInterrupted):
		return 130 // main が同じ signal で死に直す (ここでは文言を出さない)
	case errors.As(err, &ee):
		if ee.msg != "" {
			fmt.Fprintf(env.Stderr, "error: %s\n", ee.msg)
		}
		return 1
	default:
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
}

func dispatch(args []string, env *Env) error {
	global := []optSpec{{names: []string{"--engine"}, dest: "engine", takes: true}}
	gp, rest, err := parseArgs(args, global, true)
	if err != nil {
		return &usageError{msg: err.Error()}
	}
	if gp.help {
		fmt.Fprint(env.Stdout, usageText)
		return nil
	}
	if v, ok := gp.opts["engine"]; ok {
		env.Engine = v
	}
	if len(rest) == 0 {
		return &usageError{msg: "the following arguments are required: cmd"}
	}
	sub, subArgs := rest[0], rest[1:]
	if sub == reapCmd {
		return cmdReap(env) // 見張り (engine_auto.go)。自動で起動したときに切り離して起こされる
	}
	specs := map[string][]optSpec{
		"check": nil, "up": nil, "down": nil, "speakers": nil,
		"kana": {
			{names: []string{"--script"}, dest: "script", takes: true},
			{names: []string{"--who"}, dest: "who", takes: true, choices: castKeys()},
			{names: []string{"--style-id"}, dest: "style_id", takes: true, check: styleIDArg},
		},
		"synth": {{names: []string{"--force"}, dest: "force"}},
		"build": {
			{names: []string{"-o", "--output"}, dest: "output", takes: true},
			{names: []string{"--format"}, dest: "format", takes: true, choices: []string{"html", "mp4", "both"}},
			{names: []string{"--jobs"}, dest: "jobs", takes: true, check: jobsArg},
			{names: []string{"--bitrate"}, dest: "bitrate", takes: true, check: kbpsArg},
		},
	}
	spec, ok := specs[sub]
	if !ok {
		return &usageError{msg: fmt.Sprintf("argument cmd: invalid choice: '%s' (choose from check, up, down, speakers, kana, synth, build)", sub)}
	}
	p, _, err := parseArgs(subArgs, spec, false)
	if err != nil {
		return &usageError{msg: err.Error(), prog: sub}
	}
	if p.help {
		fmt.Fprint(env.Stdout, usageText)
		return nil
	}
	if sub == "kana" && p.posGroups > 1 {
		// argparse の nargs='*' は、最初のまとまりだけを取り、オプションの後ろの位置引数は余りとして拒否する
		return &usageError{msg: "unrecognized arguments: " + strings.Join(p.pos[p.firstGroup:], " ")}
	}
	nPos := map[string]int{"check": 0, "up": 0, "down": 0, "speakers": 0, "synth": 1, "build": 1, "kana": -1}[sub]
	if nPos >= 0 && len(p.pos) > nPos {
		return &usageError{msg: "unrecognized arguments: " + strings.Join(p.pos[nPos:], " ")}
	}
	if nPos > 0 && len(p.pos) < nPos {
		return &usageError{msg: "the following arguments are required: script", prog: sub}
	}
	switch sub {
	case "check":
		return cmdCheck(env)
	case "up":
		return cmdUp(env)
	case "down":
		return cmdDown(env)
	case "speakers":
		return withEngine(env, func() error { return cmdSpeakers(env) })
	case "kana":
		who := p.opts["who"]
		if who == "" {
			who = "metan"
		}
		var sid *int64
		if v, ok := p.opts["style_id"]; ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			sid = &n
		}
		if p.opts["script"] != "" {
			if err := env.requireSkillDir(); err != nil {
				return err
			}
		}
		return withEngine(env, func() error { return cmdKana(env, p.pos, p.opts["script"], who, sid) })
	case "synth":
		if err := env.requireSkillDir(); err != nil {
			return err
		}
		return withEngine(env, func() error { return cmdSynth(env, p.pos[0], p.opts["force"] == "true") })
	case "build":
		output, ok := p.opts["output"]
		if !ok {
			return &usageError{msg: "the following arguments are required: -o/--output", prog: sub}
		}
		if err := env.requireSkillDir(); err != nil {
			return err
		}
		format := p.opts["format"]
		if format == "" {
			format = "html"
		}
		jobs := 4
		if v, ok := p.opts["jobs"]; ok {
			jobs, _ = strconv.Atoi(v)
		}
		kbps := 64
		if v, ok := p.opts["bitrate"]; ok {
			kbps, _ = kbpsValue(v)
		}
		return cmdBuild(env, p.pos[0], output, format, jobs, kbps)
	}
	return nil
}
