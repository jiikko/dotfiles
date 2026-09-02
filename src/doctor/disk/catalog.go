package disk

// Risk は表示の記号と、削除時の扱い (④) を決める階級。
type Risk string

const (
	RiskSafe    Risk = "safe"    // 再生成される。副作用は速度低下のみ
	RiskCaution Risk = "caution" // 再取得に時間・通信が要る
	RiskConfirm Risk = "confirm" // ユーザーデータの可能性。中身を見せてから確認 (④ ではゴミ箱へ)
)

// Guard は候補を絞る追加判定の種類。判定の実体は guard.go。
type Guard string

const (
	GuardNone          Guard = ""
	GuardBoottime      Guard = "boottime"       // mtime が起動時刻より古いものだけ
	GuardSimDevice     Guard = "sim-device"     // simctl list devices に無い UUID だけ
	GuardProcessAbsent Guard = "process-absent" // Process のプロセスが無いときだけ (完全一致)
	GuardOrphanApp     Guard = "orphan-app"     // /Applications に bundle id を持つ .app が無い
	GuardBrewOrphan    Guard = "brew-orphan"    // 対応 formula が brew list に無い
	GuardBrewCleanup   Guard = "brew-cleanup"   // brew cleanup --dry-run が挙げるものだけ
	GuardVMRoot        Guard = "vm-root"        // <TOOL>_ROOT の実効値と一致しないものだけ
	GuardSimRuntime    Guard = "sim-runtime"    // simctl runtime list から取る (走査しない)
)

// Entry はカタログの 1 行。UI に出す文言 (Recover / Detail) はここにデータとして持つ。
type Entry struct {
	ID        string
	Label     string
	Tier      int
	Risk      Risk
	Recover   string // 復元方法の一文 (必ず表示する)
	Detail    string // 補足 (任意)
	DeleteVia string // rm | trash | cli:<cmd> | propose (④ で使う。② では表示だけ)
	Paths     []string
	Guard     Guard
	Processes []string // GuardProcessAbsent の判定名 (完全一致。推測しない。1 つでも起動中なら blocked)
	Inspect   bool     // 中身一覧を必ず見せる (ユーザーファイルの可能性)
}

// catalog はカタログ本体 (issue 148 の 1 章の写し)。載せる条件は「消したまま戻らないこと」を
// 実測で確認したもの。サイズだけで拾わない (sleepimage / unified log / Chrome cache は対象外)。
var catalog = []Entry{
	// --- Tier 1: 純粋なキャッシュ ---
	{ID: "xcode-deriveddata", Label: "Xcode DerivedData", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回ビルドで再生成されます", Detail: "初回ビルドが遅くなるだけ",
		Paths: []string{"~/Library/Developer/Xcode/DerivedData/*"}},
	// iOS 限定にしていないのは watchOS / tvOS DeviceSupport も同型のため。他 OS 版は未実測 (issue 148 未決)
	{ID: "xcode-devicesupport", Label: "Xcode DeviceSupport", Tier: 1, Risk: RiskCaution, DeleteVia: "rm",
		Recover: "実機を再接続すると自動で再取得されます (数分かかります)",
		Paths:   []string{"~/Library/Developer/Xcode/* DeviceSupport/*"}},
	{ID: "npm-cache", Label: "npm キャッシュ (_cacache)", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回 npm install 時に再取得されます", Paths: []string{"~/.npm/_cacache"}},
	{ID: "swiftpm-cache", Label: "SwiftPM キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回の依存解決で再取得されます", Paths: []string{"~/Library/Caches/org.swift.swiftpm"}},
	{ID: "electron-cache", Label: "Electron キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再ダウンロードされます", Paths: []string{"~/Library/Caches/electron"}},
	{ID: "playwright-cache", Label: "Playwright ブラウザ", Tier: 1, Risk: RiskCaution, DeleteVia: "rm",
		Recover: "npx playwright install で再取得 (数百MB の通信)", Paths: []string{"~/Library/Caches/ms-playwright"}},
	{ID: "homebrew-cache", Label: "Homebrew ダウンロードキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再ダウンロードされます", Detail: "先に公式経路 `brew cleanup` を実行してください。それでも残る分がここ",
		Paths: []string{"~/Library/Caches/Homebrew"}},
	{ID: "simulator-runtimes", Label: "シミュレータランタイム", Tier: 1, Risk: RiskCaution, DeleteVia: "cli:xcrun simctl runtime delete <id>",
		Recover: "Xcode か `simctl runtime add` で再取得 (数GB/個)", Detail: "SIP 配下なので rm できない。削除後は `xcrun simctl delete unavailable` で孤児デバイスも掃除する",
		Guard: GuardSimRuntime},
	{ID: "go-modcache", Label: "Go module キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "cli:go clean -modcache",
		Recover: "次回 build で再取得されます", Detail: "read-only で作られるので rm には強制が要る。全世代の GOPATH を見る",
		Paths: []string{"~/go/*/pkg/mod", "~/go/pkg/mod"}},
	{ID: "go-build", Label: "Go build キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回 build で再生成されます", Paths: []string{"~/Library/Caches/go-build"}},
	{ID: "headless-chrome-dl", Label: "ヘッドレス Chrome のダウンロード", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "各ツール (rod / chrome-devtools-mcp) が再ダウンロードします", Paths: []string{"~/.cache/rod", "~/.cache/chrome-devtools-mcp"}},
	{ID: "node-gyp-cache", Label: "node-gyp ヘッダキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回ネイティブビルドで再取得されます", Paths: []string{"~/Library/Caches/node-gyp"}},
	// --- Tier 2: 残骸 (孤児判定が要る) ---
	{ID: "xctest-logarchive", Label: "XCTest ログ (/var/tmp/*.logarchive)", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "特定のテストセッションの産物。再生成されません (不要)", Detail: "最終起動より古いものだけ。/var/tmp は再起動で消えない",
		// `sudo log collect --output /var/tmp/x.logarchive` で人が採った証跡と区別するため、XCTest 由来の名前に限る。
		// ⚠️ **この glob 自体が未実測** (issue 169): 元の実測記録 (issue 148) は `/var/tmp/*.logarchive` が
		// 1.8GB あったという**サイズだけ**でファイル名が残っておらず、`XCTestTesting.*` / `xctest-*` は推定。
		// Xcode 26 のバイナリを grep しても `XCTestTesting` は 0 件で、実機にも現物が無い (2026-09-03 再確認)。
		// 名前が違えばこの検出項目は黙って 0 件になる (false negative)。
		// **確定手順**: `xcodebuild test` を回した直後に `ls -la /private/var/tmp/*.logarchive` で実名を採り、
		// この glob をその名前に合わせて fixture テストで固定する。
		Paths: []string{"/private/var/tmp/XCTest*.logarchive", "/private/var/tmp/xctest*.logarchive"}, Guard: GuardBoottime},
	{ID: "xctest-spindump", Label: "XCTest spindump", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再生成されません (不要)", Paths: []string{"/private/var/tmp/XCTestTesting.*.spindump.txt"}, Guard: GuardBoottime},
	{ID: "coresimulator-orphan", Label: "孤児シミュレータの作業領域", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "現存しないデバイスの残骸。再生成されません", Detail: "simctl list devices に無い UUID だけ (現役の作業領域は候補にしない)",
		Paths: []string{"/private/var/tmp/com.apple.CoreSimulator.SimDevice.*"}, Guard: GuardSimDevice},
	{ID: "launchd-tmp", Label: "launchd の一時ディレクトリ残骸", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "容量はほぼ 0。件数削減が目的", Paths: []string{"/private/var/tmp/com.apple.launchd.*"}, Guard: GuardBoottime},
	{ID: "finder-nsird", Label: "Finder の中断残骸 (NSIRD_Finder_*)", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "コピー/移動が中断した残骸。コピー元が残っているか確認してください", Detail: "唯一のコピーである可能性を否定できません",
		Paths: []string{"$TMPDIR/TemporaryItems/NSIRD_Finder_*"}},
	{ID: "swiftui-drag-cache", Label: "SwiftUI ドラッグの取り残し", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "ドラッグが完了せず取り残された実体。元ファイルが残っているか確認してください", Detail: "Caches 配下だが中身はユーザーファイル",
		Paths: []string{"~/Library/Caches/com.apple.SwiftUI.Drag-*"}},
	{ID: "orphan-container", Label: "アプリ実体の無い sandbox コンテナ", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "アプリを再インストールしても設定は戻りません", Detail: "/Applications と ~/Applications の Info.plist を実走査して突合 (mdfind は使わない)",
		Paths: []string{"~/Library/Containers/*"}, Guard: GuardOrphanApp},
	{ID: "brew-orphan-state", Label: "アンインストール済み formula の状態 (brew prefix の var)", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "DB データ等の本体。formula が無くても中身は価値を持ちうる", Detail: "同名の launchd 登録が残っていれば svcdoctor にも出る",
		// etc は対象にしない: ImageMagick-7 / certs / fonts / openldap のように formula 名と一致しない共有
		// ディレクトリが並び、台帳突合が雑音になる (2026-09-02 実測)。容量も小さい
		// prefix は brew --prefix から解決する (Apple Silicon /opt/homebrew と Intel /usr/local で違い、
		// 直書きすると Intel で「候補なし」に化ける: issue 176)
		Paths: []string{"$BREW_PREFIX/var/*"}, Guard: GuardBrewOrphan},
	{ID: "brew-cleanup-residue", Label: "brew cleanup が消す対象", Tier: 2, Risk: RiskSafe, DeleteVia: "cli:brew cleanup",
		Recover: "brew cleanup で消えるもの (古い portable-ruby / ダウンロード等)", Detail: "~/Library/Caches/Homebrew の外 (vendor/portable-ruby) も含む",
		Guard: GuardBrewCleanup},
	{ID: "versionmanager-orphan-root", Label: "未参照のバージョンマネージャ root", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "そのマネージャで入れた言語の全世代が消えます", Detail: "<TOOL>_ROOT の実効値と一致しないものだけ (存在するだけでは候補にしない)",
		Paths: []string{"~/.rbenv", "~/.nodenv", "~/.goenv"}, Guard: GuardVMRoot},
	// --- Tier 3: アプリ起動中は触らない ---
	// glob は Canary / Beta / Dev の tmp (.com.google.Chrome.canary.* 等) にも当たるので、プロセス判定も全系列を見る
	// (Stable 終了・Canary 起動中に Canary の生きた tmp を消さない。敵対レビュー 2026-09-02)
	{ID: "chrome-tmp", Label: "Chrome 一時ファイル", Tier: 3, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "Chrome が再生成します", Paths: []string{"$TMPDIR/.com.google.Chrome.*"}, Guard: GuardProcessAbsent,
		Processes: []string{"Google Chrome", "Google Chrome Canary", "Google Chrome Beta", "Google Chrome Dev"}},
}

// containerExcludePrefixes は orphan-container で決して孤児にしない bundle id の接頭辞。
// 自作・開発中アプリは「実体が見つからない」状態を頻繁に通過し (ビルド前 / TestFlight 版のみ)、
// コンテナの中身は entitlement・トライアル状態などの検証用 state (issue 148 で偽陽性の実例)。
// com.apple.* は /Applications に無い (System/Applications や daemon のコンテナ) ので突合の対象外。
// CloudDocs 等は TCC で読めもしない (2026-09-02 実測: operation not permitted)。
var containerExcludePrefixes = []string{"com.jiikko.", "com.apple."}

// excludedRoots は allowlist の外側の番人。カタログの Paths がここに踏み込んでいないことをテストで固定する。
// (CloudStorage は走査するとオンライン専用ファイルを materialize して逆にディスクを食う)
var excludedRoots = []string{
	"~/Library/CloudStorage", "~/Library/Messages", "~/Documents", "~/Desktop", "~/Pictures", "~/Movies",
	"~/Downloads", "~/src", "~/.cache/dein", "~/Library/Application Support/Google",
}

// CatalogSize は既定カタログのエントリ数 (UI の進捗表示の分母)。
func CatalogSize() int { return len(catalog) }
