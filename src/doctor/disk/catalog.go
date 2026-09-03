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
	// go env GOMODCACHE と一致するものだけ / しないものだけ。`go clean -modcache` は
	// GOMODCACHE の 1 世代しか消さないので、走査の範囲を削除の範囲に合わせる (issue 235)
	GuardGoModcacheCurrent Guard = "go-modcache-current"
	GuardGoModcacheOld     Guard = "go-modcache-old"
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
	// Unverified は「**検出条件そのものが未実測**」の印 (空でなければ未検証。中身は短い理由)。
	// 走査は正常に終わっているので Status は ok のままだが、**0 件を「候補なし」と読んではいけない**
	// エントリを区別する。表示側は 0 件でもこの行を隠さない (隠すと「探せていない」が
	// 「きれいです」と同じ見え方になる = false green)。
	// 実測で名前を確定したらこのフィールドを消す (issue 169)。
	Unverified string
}

// catalog はカタログ本体 (issue 148 の 1 章の写し)。載せる条件は「消したまま戻らないこと」を
// 実測で確認したもの。サイズだけで拾わない (sleepimage / unified log / Chrome cache は対象外)。
//
// 🚨 **意図的に載せていないもの** (issue 218 で判断。次の audit が同じ提案を再生成しないため):
//
//   - `~/.Trash` — このカタログの `DeleteVia: "trash"` の**避難先そのもの**。同じ画面に
//     「ゴミ箱へ移す」と「ゴミ箱を空にする」を並べるのは危険で、空にすると復元不可。
//     Finder が標準機能で持っている。実測 2026-09-03 は 0 件。
//     再開の trigger: ゴミ箱が数 GB 溜まったまま放置されているのを見たとき
//   - `~/Downloads/*.crdownload` — **ユーザーデータ領域**で、中断した DL の再開情報を持つ。
//     消すと DL のやり直しが要るので登録条件「消したまま戻る」を満たさない。実測 0 件
//   - `~/Library/Caches/electron-builder` / `deno` — 実測 2026-09-03 で**存在しない**。
//     現れたら `typescript-ata` と同型なので同じ形で足せる
//   - speech モデルのキャッシュ — この機に存在しない (`com.apple.SpeechRecognitionCore` /
//     `SpeechModelCache` とも無し)。載せるには `lsof` 判定と「削除後に音声機能が壊れない」
//     実測が要る。現物が無いので実測そのものができない
//   - `~/src/**` のビルド成果物 / `node_modules` — プロジェクト単位の判断が要り、
//     allowlist の枠組みに合わない (issue 220)
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
	// tsserver の型自動取得 (ATA) キャッシュ。TypeScript のバージョンごとにディレクトリが分かれ、
	// 中身は `types-registry` と `@types/*` の node_modules。実測 2026-09-03: 216MB
	// (5.2 が 122MB / 5.8 が 91.6MB / 4.9 が 2.8MB)。**5.2 は 2023-11 から触られていない**。
	// 登録条件 (消したまま戻ることを実測) の根拠: 各ディレクトリに package.json と
	// package-lock.json があり中身は npm から復元できる。4.9 の更新日が 2026-07-15 で、
	// tsserver が実際に作り直していることも確認済み (issue 218)。
	{ID: "typescript-ata", Label: "TypeScript 型キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "エディタが型を必要としたときに npm 経由で再取得されます", Detail: "TypeScript のバージョンごとに溜まる。古い版の分は使われない",
		Paths: []string{"~/Library/Caches/typescript/*"}},
	{ID: "playwright-cache", Label: "Playwright ブラウザ", Tier: 1, Risk: RiskCaution, DeleteVia: "rm",
		Recover: "npx playwright install で再取得 (数百MB の通信)", Paths: []string{"~/Library/Caches/ms-playwright"}},
	{ID: "homebrew-cache", Label: "Homebrew ダウンロードキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再ダウンロードされます", Detail: "先に公式経路 `brew cleanup` を実行してください。それでも残る分がここ",
		Paths: []string{"~/Library/Caches/Homebrew"}},
	{ID: "simulator-runtimes", Label: "シミュレータランタイム", Tier: 1, Risk: RiskCaution, DeleteVia: "cli:xcrun simctl runtime delete <id>",
		Recover: "Xcode か `simctl runtime add` で再取得 (数GB/個)", Detail: "SIP 配下なので rm できない。削除後は `xcrun simctl delete unavailable` で孤児デバイスも掃除する",
		Guard: GuardSimRuntime},
	// 🚨 **2 エントリに分けてある** (issue 235)。`go clean -modcache` が消すのは
	// `go env GOMODCACHE` の 1 世代だけなのに、`~/go/*/pkg/mod` は複数世代 (goenv / asdf 等) に
	// 当たる。1 エントリのままだと削除後の再走査に他の世代が残り、**毎回必ず「未完了」**になって
	// 時間をおいても消えなかった。走査の範囲を削除の範囲へ合わせ、残りは別エントリで見せる。
	{ID: "go-modcache", Label: "Go module キャッシュ (現行)", Tier: 1, Risk: RiskSafe, DeleteVia: "cli:go clean -modcache",
		Recover: "次回 build で再取得されます", Detail: "read-only で作られるので rm には強制が要る。go env GOMODCACHE の 1 世代だけ",
		Paths: []string{"~/go/*/pkg/mod", "~/go/pkg/mod"}, Guard: GuardGoModcacheCurrent},
	// 古い世代は **propose** (コマンドを出すだけ)。read-only で作られるので rm には強制が要り、
	// `go clean` は GOMODCACHE しか見ないため道具からは消せない。消さずに見せるのは、
	// 走査から落とすと「無い」ことになり false green になるため
	{ID: "go-modcache-old", Label: "Go module キャッシュ (使っていない世代)", Tier: 1, Risk: RiskCaution, DeleteVia: "propose",
		Recover: "その Go を使うときに再取得されます", Detail: "go clean -modcache では消せない (GOMODCACHE の 1 世代しか見ない)。消すなら chmod -R u+w してから rm -rf",
		Paths: []string{"~/go/*/pkg/mod", "~/go/pkg/mod"}, Guard: GuardGoModcacheOld},
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
		// 🚨 **この glob 自体が未実測** (issue 169): 元の実測記録 (issue 148) は `/var/tmp/*.logarchive` が
		// 1.8GB あったという**サイズだけ**でファイル名が残っておらず、`XCTestTesting.*` / `xctest-*` は推定。
		// Xcode 26 のバイナリを grep しても `XCTestTesting` は 0 件で、実機にも現物が無い (2026-09-03 再確認)。
		// 🚨 **静的探索は尽きている** (2026-09-03。同じ grep を繰り返さないこと):
		//   - 名前を作る側は `XCTAutomationSupport` の `collectLogArchiveWithStartDate:outputPath:withReply:` で、
		//     **outputPath は呼び出し側が渡す**ため、生成側のバイナリに名前のリテラルは無い
		//   - `.logarchive` のリテラルを Xcode 全体 (Platforms / PrivateFrameworks / Frameworks /
		//     SharedFrameworks / usr) から拾っても、当たるのは `logdump` と `LoggingSupportHost` の
		//     拡張子検査だけ (`File name does not end with .logarchive (%@)`)
		//   つまり**実行時に採る以外に確定手段が無い**。
		// 名前が違えばこの検出項目は黙って 0 件になる (false negative)。
		// **確定手順**: `xcodebuild test` を回した直後に `ls -la /private/var/tmp/*.logarchive` で実名を採り、
		// この glob をその名前に合わせて fixture テストで固定する。
		Paths: []string{"/private/var/tmp/XCTest*.logarchive", "/private/var/tmp/xctest*.logarchive"}, Guard: GuardBoottime,
		Unverified: "ファイル名が未実測 (issue 169)"},
	{ID: "xctest-spindump", Label: "XCTest spindump", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再生成されません (不要)",
		// 🚨 **この glob も未実測** (issue 169 と同型。2026-09-03 に発見)。上の xctest-logarchive と
		// **同じ `XCTestTesting.` 接頭辞を推測で共有している**が、その接頭辞は実在が確認できていない:
		// `grep -rl --binary-files=text 'XCTestTesting' <Xcode>/Platforms/MacOSX.platform` が **0 件**
		// (Xcode 26.3。バイナリも含めた全走査)。実機の /private/var/tmp にも現物が無い。
		// 名前が違えばこの検出項目も黙って 0 件になる。確定手順は上のエントリと同じ。
		Paths: []string{"/private/var/tmp/XCTestTesting.*.spindump.txt"}, Guard: GuardBoottime,
		Unverified: "ファイル名が未実測 (issue 169)"},
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
	// 🚨 Label は表示幅 40 桁に収める (ディスク行のラベル列幅)。超えると UI で末尾が切れ、
	// 括弧の補足だけが「(/o…」のように残る (実測 2026-09-03。issue 182)。場所は Detail へ
	{ID: "brew-orphan-state", Label: "アンインストール済み formula の状態", Tier: 2, Risk: RiskConfirm, DeleteVia: "trash", Inspect: true,
		Recover: "DB データ等の本体。formula が無くても中身は価値を持ちうる", Detail: "brew prefix の var 配下。同名の launchd 登録が残っていれば svcdoctor にも出る",
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

// CatalogEntry は既定カタログの ID からエントリを引く。復元した Result の Entry を
// **カタログの今の定義へ束ね直す**ために使う: snapshot / キャッシュには Entry が丸ごと保存されるので、
// そのまま使うと (a) カタログを直しても古い文言が出続け (b) 一般ユーザー権限で書き換えた
// 文言がそのまま画面と y のコピーに載る (issue 229)。
func CatalogEntry(id string) (Entry, bool) {
	for _, e := range catalog {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// CatalogHasID は既定カタログにその ID があるか。キャッシュ / snapshot から復元した Result が
// 「今のカタログに実在するエントリか」を確かめるために使う。snapshot は一般ユーザー権限で
// 書き換えられるので、そこに書かれた ID をそのまま行にしない (issue 178)。
func CatalogHasID(id string) bool {
	for _, e := range catalog {
		if e.ID == id {
			return true
		}
	}
	return false
}
