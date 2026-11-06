// Package uninstall は Mac のアプリと Homebrew の formula を、残骸ごと安全に片付ける。CLI は bin/mac-uninstall、
// 手順 (候補を見る → 選ぶ → 計画を見せる → 承認 → 実行) は skill mac-uninstall。
//
// 消し方は 2 通りだけ: brew の管理下のもの (cask・formula) は `brew uninstall`、それ以外 (App Store・手で入れたアプリと残骸) は
// ゴミ箱へ移す (safefs.TrashMove。Finder から戻せる)。rm はしない。`brew uninstall --zap` と `brew autoremove` は自動では走らせない
// (どちらも計画に載っていないものを消しうる)。
//
// 計画のファイルは信用しない: apply は計画の ID を計算し直して照合し、その時点の候補を作り直して、計画との交わりのうち
// 実体 (dev, ino) が変わっていないものだけを動かす。計画を書き換えても、動かすものは増やせない。
package uninstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"howett.net/plist"

	"doctor/runner"
)

// Source はアプリの入り方。
type Source string

const (
	SourceCask   Source = "cask"   // brew の cask
	SourceMAS    Source = "mas"    // App Store (Contents/_MASReceipt がある)
	SourceManual Source = "manual" // 手で入れたもの
)

// App は 1 つのアプリ。
type App struct {
	Name      string    `json:"name"`
	BundleID  string    `json:"bundle_id"`
	Version   string    `json:"version,omitempty"`
	Path      string    `json:"path"`
	Source    Source    `json:"source"`
	CaskToken string    `json:"cask_token,omitempty"`
	SizeKB    int64     `json:"size_kb"`
	LastUsed  time.Time `json:"last_used,omitzero"` // 取れないときはゼロ値 (= 不明。未使用とは読まない)
	Running   bool      `json:"running"`
	// PkgID は pkg のインストーラで入れたときの受領の ID (pkgutil --file-info)。本体の外にドライバ・常駐・システムの拡張を
	// 置いていることがあり、本体をゴミ箱へ移すだけでは片付かない。空でなければ動かさず、付属のアンインストーラを案内する
	PkgID string `json:"pkg_id,omitempty"`
	// Manual は本体を動かしてはいけない理由 (pkg・root の常駐・sudo の要る cask の手順)。空でなければ本体も残骸も動かさない
	Manual string `json:"manual,omitempty"`
	// zap は cask の定義が挙げる掃除の対象 (glob を含む ~ 始まりのパス)。表示はしない
	zap []string
}

// Formula は brew の formula のうち、他の formula から依存されていないもの (brew leaves)。
type Formula struct {
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	InstalledAt time.Time `json:"installed_at,omitzero"`
	OnRequest   bool      `json:"on_request"` // 自分で入れた (依存として入ったものではない)
	SizeKB      int64     `json:"size_kb"`
	Running     bool      `json:"running"`
	Path        string    `json:"path"` // Cellar/<name>
	// Protected は消してはいけない理由 (ログインシェル・/etc/shells に載ったシェル)。空でなければ計画を作らない
	Protected string `json:"protected,omitempty"`
}

// Inventory は候補の一覧。
type Inventory struct {
	Apps     []App     `json:"apps"`
	Formulae []Formula `json:"formulae"`
	// Notes は一覧の一部が取れなかった理由 (判定不能)。空でなければ一覧は全体ではない
	Notes []string `json:"notes,omitempty"`
	// RunningUnknown は「動いているか」を確かめられなかったこと。立っていれば計画を作らない (動いているものを消しうる)
	RunningUnknown bool `json:"running_unknown,omitempty"`
	// exes は動いているプロセスの実行ファイル・ライブラリのパス (lsof の txt と ps の comm)。itemsFor が同じ bundle ID のコピーにも当てる
	exes []string
}

// Options は外の世界への口。テストは全部差し替える。
type Options struct {
	Run     runner.Runner
	Home    string
	AppDirs []string // アプリを探す置き場 (既定は /Applications と ~/Applications)
	Cellar  string   // brew --cellar (空なら brew に聞く)
	// CaskAppDir は brew の cask が .app を置く場所 (既定 /Applications)。ここの .app だけを cask の台帳と突き合わせる
	CaskAppDir string
	Now        func() time.Time
}

// DefaultAppDirs はアプリの置き場。
func DefaultAppDirs(home string) []string {
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

func (o *Options) fill(ctx context.Context) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.CaskAppDir == "" {
		o.CaskAppDir = "/Applications"
	}
	if o.Cellar == "" {
		if out, _, rc, err := runner.WithTimeout(ctx, o.Run, 30*time.Second, "brew", "--cellar"); err == nil && rc == 0 {
			o.Cellar = strings.TrimSpace(out)
		}
	}
}

// List は候補を集める (読み取りだけ)。Apple のアプリ (bundle ID が com.apple.) は対象にしない。
func List(ctx context.Context, o Options) Inventory {
	o.fill(ctx)
	var inv Inventory
	casks, err := caskIndex(ctx, o.Run, o.CaskAppDir)
	if err != nil {
		inv.Notes = append(inv.Notes, "cask の台帳が読めないので、cask のアプリも手で入れたものとして扱う: "+err.Error())
	}
	exes, args, err := runningPaths(ctx, o.Run)
	if err != nil {
		inv.RunningUnknown = true
		inv.Notes = append(inv.Notes, "動いているかを確かめられないので、計画を作らない: "+err.Error())
	}
	inv.exes = exes
	traces := systemTraces(ctx, o.Run)
	for _, dir := range o.AppDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				inv.Notes = append(inv.Notes, dir+" を読めない")
			}
			continue
		}
		for _, e := range ents {
			// . で始まるものは他のアプリの補助 (Karabiner のドライバの管理など) なので候補にしない
			if !strings.HasSuffix(e.Name(), ".app") || strings.HasPrefix(e.Name(), ".") || e.Type()&os.ModeSymlink != 0 || !e.IsDir() {
				continue
			}
			app, ok := readApp(filepath.Join(dir, e.Name()))
			if !ok {
				inv.Notes = append(inv.Notes, e.Name()+" の Info.plist が読めない")
				continue
			}
			if isApple(app.BundleID) {
				continue
			}
			if c, ok := casks[app.Path]; ok {
				if c.token == "" {
					inv.Notes = append(inv.Notes, e.Name()+" を複数の cask が置くことになっていて、どれのものか決められないので候補にしない")
					continue
				}
				app.Source, app.CaskToken, app.zap = SourceCask, c.token, c.zap
				if c.risky != "" {
					app.Manual = "cask の uninstall の手順が計画の外のものを消すか sudo を要しうる (" + c.risky + ")。中身を確かめてから、ターミナルで brew uninstall --cask " + c.token + " を打つ"
				}
			}
			if app.Source != SourceCask {
				app.PkgID = pkgID(ctx, o.Run, app.Path)
			}
			app.Running = runningUnder(exes, app.Path)
			app.SizeKB = duKB(ctx, o.Run, app.Path)
			app.LastUsed = lastUsed(ctx, o.Run, app.Path)
			inv.Apps = append(inv.Apps, app)
		}
	}
	// 開発元の痕跡は全部のアプリを読んでから見る: App Store は bundle ID の名前で受領を作るので、入っているアプリの bundle ID と
	// 同じ名前の受領は痕跡に数えない (数えると、同じ開発元の手入れのアプリが全部「手で確かめる」になる)
	traces.dropPkgs(inv.Apps)
	for i := range inv.Apps {
		app := &inv.Apps[i]
		if app.Source == SourceCask || app.Manual != "" {
			continue
		}
		switch v := traces.vendor(app.BundleID, app.Source == SourceManual); {
		case app.PkgID != "":
			app.Manual = "pkg のインストーラで入れた (" + app.PkgID + ")。付属のアンインストーラか開発元の手順で消す"
		case v != "":
			app.Manual = "同じ開発元の " + v + " がある (pkg の受領か root の常駐)。付属のアンインストーラか開発元の手順で消す"
		}
	}
	slices.SortFunc(inv.Apps, func(a, b App) int { return strings.Compare(a.Name, b.Name) })
	fs, err := formulae(ctx, o, exes, args, protectedShells(ctx, o.Run, o.Home), shellConfig(o.Home))
	if err != nil {
		inv.Notes = append(inv.Notes, "formula の一覧が取れない: "+err.Error())
	}
	inv.Formulae = fs
	return inv
}

// readApp は .app の Info.plist から名前と bundle ID を読む。Mac で動く iPhone / iPad のアプリは
// <名前>.app/Wrapper/<中身>.app/Info.plist に持ち、App Store から入る (iTunesMetadata.plist がある)。
func readApp(path string) (App, bool) {
	ios := false
	data, err := os.ReadFile(filepath.Join(path, "Contents", "Info.plist"))
	if err != nil {
		inner, _ := filepath.Glob(filepath.Join(path, "Wrapper", "*.app", "Info.plist"))
		if len(inner) != 1 {
			return App{}, false
		}
		if data, err = os.ReadFile(inner[0]); err != nil {
			return App{}, false
		}
		ios = true
	}
	var info struct {
		ID      string `plist:"CFBundleIdentifier"`
		Name    string `plist:"CFBundleName"`
		Version string `plist:"CFBundleShortVersionString"`
	}
	if _, err := plist.Unmarshal(data, &info); err != nil || !validBundleID(info.ID) {
		return App{}, false
	}
	app := App{Name: strings.TrimSuffix(filepath.Base(path), ".app"), BundleID: info.ID, Version: info.Version, Path: path, Source: SourceManual}
	if st, err := os.Stat(filepath.Join(path, "Contents", "_MASReceipt")); err == nil && st.IsDir() {
		app.Source = SourceMAS
	}
	if _, err := os.Stat(filepath.Join(path, "iTunesMetadata.plist")); ios && err == nil {
		app.Source = SourceMAS
	}
	return app, true
}

// validBundleID は bundle ID の形 (英数字と . - _ だけで、. で始まらない)。🚨 残骸のパスは bundle ID から組むので、
// Info.plist に "../" などを書いたアプリが ~/Library の外を指せないようにここで弾く (Info.plist はアプリの作者が書く外の値)。
func validBundleID(id string) bool {
	// 2 段以上の reverse-DNS に限る: 1 段の ID (Homebrew・ByHost・Google) は ~/Library の共有の dir の名前と重なる
	if id == "" || strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") || strings.Contains(id, "..") || !strings.Contains(id, ".") {
		return false
	}
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// isApple は Apple のアプリか (大小文字を区別しない)。
func isApple(id string) bool {
	l := strings.ToLower(id)
	return l == "com.apple" || strings.HasPrefix(l, "com.apple.")
}

type caskInfo struct {
	token string // 空 = 複数の cask が同じ場所を指していて決められない
	zap   []string
	risky string // uninstall の手順にある、計画の外を消す・sudo が要りうる指示 (空なら無い)
}

// caskIndex は「cask が置く .app の絶対パス → cask」の表。
func caskIndex(ctx context.Context, run runner.Runner, appDir string) (map[string]caskInfo, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, 120*time.Second, "brew", "info", "--cask", "--json=v2", "--installed")
	if err != nil || rc != 0 {
		return nil, cmdErr("brew info --cask", rc, err, stderr)
	}
	return parseCasks(out, appDir)
}

// parseCasks は cask の JSON を読む。app の artifact の置き場は target (絶対パス) を正とし、無ければ appDir/<名前>。
// 🚨 名前だけで突き合わせない: target で別名に置く cask があると、手で入れた同名の .app を cask のものと取り違え、
// 選んでいない別のアプリを brew uninstall する。
func parseCasks(out, appDir string) (map[string]caskInfo, error) {
	var d struct {
		Casks []struct {
			Token     string                       `json:"token"`
			Artifacts []map[string]json.RawMessage `json:"artifacts"`
		} `json:"casks"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return nil, err
	}
	m := map[string]caskInfo{}
	for _, c := range d.Casks {
		info := caskInfo{token: c.Token}
		var apps []string
		for _, a := range c.Artifacts {
			if raw, ok := a["app"]; ok {
				names := stringsIn(raw)
				var target string
				_ = json.Unmarshal(a["target"], &target)
				switch {
				case strings.HasPrefix(target, "/"):
					apps = append(apps, target)
				case len(names) > 0:
					apps = append(apps, filepath.Join(appDir, filepath.Base(names[0])))
				}
			}
			if raw, ok := a["uninstall"]; ok {
				info.risky = riskyDirectives(raw)
			}
			if raw, ok := a["zap"]; ok {
				var zs []map[string]json.RawMessage
				if json.Unmarshal(raw, &zs) == nil {
					for _, z := range zs {
						// trash も delete も「掃除の対象」の一覧として読む (消し方はこちらで決める: ゴミ箱へ移す)
						info.zap = append(info.zap, stringsIn(z["trash"])...)
						info.zap = append(info.zap, stringsIn(z["delete"])...)
					}
				}
			}
		}
		if len(apps) > 1 && info.risky == "" {
			// 1 つを選んでも brew は全部を消す。選んでいない方は計画に載らず、動いているかも見ていない
			info.risky = "app を " + strconv.Itoa(len(apps)) + " 個置く"
		}
		for _, p := range apps {
			if prev, ok := m[p]; ok && prev.token != info.token {
				m[p] = caskInfo{} // 曖昧
				continue
			}
			m[p] = info
		}
	}
	return m, nil
}

// riskyDirectives は cask の uninstall の手順のうち、計画に載らないものを消すか root の権限が要りうる指示の名前を返す。
// pkgutil (pkg の受領を消す)・script / early_script (任意のスクリプト)・kext・launchctl (system の常駐のことがある)・
// delete / trash (-data を選んでいなくても brew が消す)。brew は非対話で走らせるので、sudo は失敗するか止まり、
// アプリの終了や常駐の停止という副作用だけが残る。quit・signal・rmdir (空の dir だけ) は数えない。
//
// 判定は許可した指示の一覧で行う (知らない指示・読めない手順は危ない側に数える。brew が指示を増やしても素通りさせない)。
func riskyDirectives(raw json.RawMessage) string {
	var steps []map[string]json.RawMessage
	if json.Unmarshal(raw, &steps) != nil {
		return "uninstall の手順が読めない"
	}
	var found []string
	for _, st := range steps {
		for k := range st {
			if !slices.Contains(safeUninstallKeys, k) && !slices.Contains(found, k) {
				found = append(found, k)
			}
		}
	}
	slices.Sort(found)
	return strings.Join(found, ", ")
}

// safeUninstallKeys は cask の uninstall の手順のうち、計画の外を消さず sudo も要らない指示 (アプリを終了させる・空の dir を消す・
// ログイン項目から外す)。
var safeUninstallKeys = []string{"quit", "signal", "rmdir", "login_item"}

// stringsIn は文字列か、文字列の配列 (要素に {target: ...} の object が混ざることがある) から文字列だけを集める。
func stringsIn(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []json.RawMessage
	if json.Unmarshal(raw, &many) != nil {
		return nil
	}
	var out []string
	for _, r := range many {
		if json.Unmarshal(r, &one) == nil {
			out = append(out, one)
		}
	}
	return out
}

// runningPaths は動いているプロセスが使っている実行ファイルとライブラリのパス。2 つの材料を合わせる:
//   - lsof の txt: カーネルが掴んでいる実体のパス (Cellar の実パスで出る。brew の bin・opt の symlink や、comm が素の名前の tmux / nvim も拾う)。
//     自分のユーザーのプロセスしか見えないが、消すのは自分のアプリと brew なので足りる
//   - ps の comm と、それの symlink を解いた先 (lsof に見えないプロセスの保険)
//
// 🚨 lsof が取れなければ error を返し、呼び出し側は計画を作らない (動いているものを「動いていない」と読まない)。
//
// args は全プロセスの引数の行。🚨 表示しない (秘密が載りうる)。formula のパスを含むかの判定だけに使う: root で動く brew の常駐 (lsof に見えない)・
// Cellar の解釈系で動くスクリプト・comm が setproctitle で書き換わったもの (redis-server 127.0.0.1:6379) を拾う。
func runningPaths(ctx context.Context, run runner.Runner) (paths, args []string, err error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, 60*time.Second, "/usr/sbin/lsof", "-nP", "-Fn", "-d", "txt")
	if err != nil || (rc != 0 && strings.TrimSpace(out) == "") {
		return nil, nil, cmdErr("lsof", rc, err, stderr)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if p, ok := strings.CutPrefix(line, "n"); ok && strings.HasPrefix(p, "/") {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, nil, errors.New("lsof が実行ファイルを 1 つも返さない")
	}
	psOut, stderr, rc, err := runner.WithTimeout(ctx, run, 20*time.Second, "/bin/ps", "-axww", "-o", "comm=")
	if err != nil || rc != 0 {
		return nil, nil, cmdErr("ps", rc, err, stderr)
	}
	for line := range strings.SplitSeq(psOut, "\n") {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "/") {
			continue
		}
		paths = append(paths, s)
		if real, err := filepath.EvalSymlinks(s); err == nil && real != s {
			paths = append(paths, real)
		}
	}
	argOut, stderr, rc, err := runner.WithTimeout(ctx, run, 20*time.Second, "/bin/ps", "-axww", "-o", "args=")
	if err != nil || rc != 0 {
		return nil, nil, cmdErr("ps", rc, err, stderr)
	}
	args = strings.Split(argOut, "\n")
	return paths, args, nil
}

// traces は root の常駐と pkg の受領の名前 (本体の外に何かを置いたアプリを見分ける材料)。
// 🚨 2 つを分けて持つ: App Store のインストールも bundle ID の名前で pkg の受領を作る (Quick Camera で実測) ので、
// App Store のアプリは受領を見ず、常駐だけで判定する。
type traces struct {
	daemons []string // /Library の LaunchDaemons・LaunchAgents・PrivilegedHelperTools の名前
	pkgs    []string // pkgutil --pkgs
}

// systemTraces は /Library の常駐の置き場と pkg の受領の名前を集める。読めなければ空 (この材料が無いだけで、pkgutil --file-info は別に見る)。
func systemTraces(ctx context.Context, run runner.Runner) traces {
	var t traces
	for _, d := range systemDaemonDirs {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			t.daemons = append(t.daemons, e.Name())
		}
	}
	if out, _, rc, err := runner.WithTimeout(ctx, run, 30*time.Second, "/usr/sbin/pkgutil", "--pkgs"); err == nil && rc == 0 {
		t.pkgs = strings.Fields(out)
	}
	return t
}

// dropPkgs は入っている App Store のアプリの bundle ID と同じ名前の受領を落とす (App Store が作る受領)。手で入れたアプリの受領は
// 落とさない (pkg で入れたのに置き場を移して pkgutil --file-info が外れたとき、ここが最後の手がかり)。
func (t *traces) dropPkgs(apps []App) {
	t.pkgs = slices.DeleteFunc(t.pkgs, func(p string) bool {
		return slices.ContainsFunc(apps, func(a App) bool { return a.Source == SourceMAS && strings.EqualFold(a.BundleID, p) })
	})
}

// systemDaemonDirs は root の常駐の置き場。テストは差し替える。
var systemDaemonDirs = []string{"/Library/LaunchDaemons", "/Library/LaunchAgents", "/Library/PrivilegedHelperTools"}

// vendor は bundle ID の上 2 段 (開発元。us.zoom.xos なら us.zoom) で始まる常駐 (と、withPkgs なら pkg の受領) の名前を 1 つ返す。無ければ空。
func (t *traces) vendor(bid string, withPkgs bool) string {
	parts := strings.Split(bid, ".")
	if len(parts) < 2 {
		return ""
	}
	prefix := strings.ToLower(parts[0] + "." + parts[1] + ".")
	names := t.daemons
	if withPkgs {
		names = append(slices.Clone(t.daemons), t.pkgs...)
	}
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			return name
		}
	}
	return ""
}

var (
	brewPathRe = regexp.MustCompile(`/(?:bin|sbin|opt|share|lib|libexec|etc)/[^\s"'$()<>|;&]+`)
	cmdSubstRe = regexp.MustCompile(`\$\(\s*([A-Za-z0-9_.+@-]+)`)
)

// brewRefs は文 (シェルの設定・プロセスの引数) の中の brew のパスとコマンドを、symlink を解いた実体のパスにして返す。
//   - <prefix>/bin/nvim・<prefix>/opt/fzf/shell/x.zsh のような brew の置き場のパス
//   - $(direnv hook zsh) のようにコマンドの名前だけで呼ぶもの (<prefix>/bin/<名前> と読む)
//
// formula の名前とコマンドの名前が違う (neovim と nvim) ので、文字列ではなく解いた先が Cellar/<name>/ の下かで判定する。
// $brew_prefix のような変数の形は解けないので、呼び出し側は名前の文字列の照合も併せて行う。
func brewRefs(text, prefix string) []string {
	var cands []string
	for _, m := range brewPathRe.FindAllStringIndex(text, -1) {
		// 直前が prefix で終わっているものだけ (/usr/bin/... を拾わない)
		if m[0] >= len(prefix) && text[m[0]-len(prefix):m[0]] == prefix {
			cands = append(cands, prefix+text[m[0]:m[1]])
		}
	}
	for _, m := range cmdSubstRe.FindAllStringSubmatch(text, -1) {
		cands = append(cands, filepath.Join(prefix, "bin", m[1]))
	}
	var real []string
	for _, c := range cands {
		if r, err := filepath.EvalSymlinks(c); err == nil {
			real = append(real, r)
		}
	}
	return real
}

// shellConfigFiles はシェルと tmux の設定ファイル。formula の中のファイルを source しているかを見る。
var shellConfigFiles = []string{".zshrc", ".zshenv", ".zprofile", ".zlogin", ".bashrc", ".bash_profile", ".profile", ".tmux.conf", ".config/tmux/tmux.conf"}

// shellConfig は設定ファイルの中身をつないだもの (読めないものは飛ばす)。
func shellConfig(home string) string {
	var b strings.Builder
	for _, f := range shellConfigFiles {
		if data, err := os.ReadFile(filepath.Join(home, f)); err == nil {
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// protectedShells はログインシェルと /etc/shells に載ったシェルの実体のパス (symlink を解いたもの)。
func protectedShells(ctx context.Context, run runner.Runner, home string) []string {
	var shells []string
	if out, _, rc, err := runner.WithTimeout(ctx, run, 10*time.Second, "/usr/bin/dscl", ".", "-read", home, "UserShell"); err == nil && rc == 0 {
		if _, v, ok := strings.Cut(out, "UserShell:"); ok {
			shells = append(shells, strings.TrimSpace(v))
		}
	}
	if data, err := os.ReadFile("/etc/shells"); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if s := strings.TrimSpace(line); strings.HasPrefix(s, "/") {
				shells = append(shells, s)
			}
		}
	}
	var real []string
	for _, s := range shells {
		real = append(real, s)
		if r, err := filepath.EvalSymlinks(s); err == nil {
			real = append(real, r)
		}
	}
	return real
}

// runningUnder は dir の下の実行ファイルが動いているか。comm は argv[0] で偽れるが、ここでの誤りは「動いている」の側
// (= 消さない側) に倒れるだけなので、判定に使ってよい。
func runningUnder(exes []string, dir string) bool {
	return slices.ContainsFunc(exes, func(e string) bool { return strings.HasPrefix(e, dir+"/") })
}

// pkgID は pkg のインストーラが置いたものなら、その受領の ID を返す (pkgutil は該当が無いと rc=1)。
func pkgID(ctx context.Context, run runner.Runner, path string) string {
	out, _, rc, err := runner.WithTimeout(ctx, run, 20*time.Second, "/usr/sbin/pkgutil", "--file-info", path)
	if err != nil || rc != 0 {
		return ""
	}
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "pkgid:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func duKB(ctx context.Context, run runner.Runner, path string) int64 {
	out, _, rc, err := runner.WithTimeout(ctx, run, 60*time.Second, "/usr/bin/du", "-sk", path)
	if err != nil || rc != 0 {
		return -1
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return -1
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// lastUsed は Finder の「最後に開いた日」(kMDItemLastUsedDate)。cask の更新でアプリが差し替わると消えるので、
// 取れないものは「不明」(ゼロ値) にして、未使用とは読まない。
func lastUsed(ctx context.Context, run runner.Runner, path string) time.Time {
	out, _, rc, err := runner.WithTimeout(ctx, run, 10*time.Second, "/usr/bin/mdls", "-raw", "-name", "kMDItemLastUsedDate", path)
	if err != nil || rc != 0 {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(out))
	if err != nil {
		return time.Time{}
	}
	return t
}

// formulae は brew leaves (他から依存されていない formula) を、入れた日時と大きさつきで返す。
func formulae(ctx context.Context, o Options, exes, args, shells []string, shellRC string) ([]Formula, error) {
	if o.Cellar == "" {
		return nil, errors.New("brew --cellar が取れない")
	}
	out, stderr, rc, err := runner.WithTimeout(ctx, o.Run, 60*time.Second, "brew", "leaves")
	if err != nil || rc != 0 {
		return nil, cmdErr("brew leaves", rc, err, stderr)
	}
	leaves := strings.Fields(out)
	infoOut, stderr, rc, err := runner.WithTimeout(ctx, o.Run, 120*time.Second, "brew", "info", "--json=v2", "--installed")
	if err != nil || rc != 0 {
		return nil, cmdErr("brew info", rc, err, stderr)
	}
	meta, err := parseFormulae(infoOut)
	if err != nil {
		return nil, err
	}
	prefix := filepath.Dir(o.Cellar)
	rcRefs, argRefs := brewRefs(shellRC, prefix), brewRefs(strings.Join(args, "\n"), prefix)
	fs := make([]Formula, 0, len(leaves))
	for _, name := range leaves {
		f := meta[name]
		f.Name = name
		f.Path = filepath.Join(o.Cellar, filepath.Base(name))
		f.SizeKB = duKB(ctx, o.Run, f.Path)
		base := filepath.Base(name)
		opt := filepath.Join(filepath.Dir(o.Cellar), "opt", base) + "/"
		under := func(p string) bool { return strings.HasPrefix(p, f.Path+"/") }
		f.Running = runningUnder(exes, f.Path) || runningUnder(exes, strings.TrimSuffix(opt, "/")) ||
			slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, f.Path+"/") || strings.Contains(a, opt) }) ||
			slices.ContainsFunc(argRefs, under)
		switch {
		case slices.ContainsFunc(shells, under):
			f.Protected = "ログインシェルか /etc/shells に載ったシェル"
		case slices.ContainsFunc(rcRefs, under) || strings.Contains(shellRC, "/"+base+"/") || strings.Contains(shellRC, "/"+base+"\""):
			// シェル・tmux の設定が formula の中のファイルを source するか、コマンドとして呼んでいる (lsof にも ps にも出ない使われ方)
			f.Protected = "シェルか tmux の設定から使われている (" + base + ")"
		}
		fs = append(fs, f)
	}
	return fs, nil
}

func parseFormulae(out string) (map[string]Formula, error) {
	var d struct {
		Formulae []struct {
			Name      string `json:"name"`
			FullName  string `json:"full_name"`
			Installed []struct {
				Version   string `json:"version"`
				Time      int64  `json:"time"`
				OnRequest bool   `json:"installed_on_request"`
			} `json:"installed"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return nil, err
	}
	m := map[string]Formula{}
	for _, f := range d.Formulae {
		if len(f.Installed) == 0 {
			continue
		}
		in := f.Installed[len(f.Installed)-1]
		v := Formula{Version: in.Version, OnRequest: in.OnRequest}
		if in.Time > 0 {
			v.InstalledAt = time.Unix(in.Time, 0)
		}
		m[f.Name], m[f.FullName] = v, v
	}
	return m, nil
}

// cmdErr は外部コマンドの失敗を 1 つの error にする。起動できなかったときは元の error を包み (%w)、rc が 0 でないときは stderr の先頭行を添える。
func cmdErr(what string, rc int, err error, stderr string) error {
	if err != nil {
		return fmt.Errorf("%s を起動できない: %w", what, err)
	}
	return fmt.Errorf("%s が rc=%d: %s", what, rc, runner.FirstLine(stderr))
}
