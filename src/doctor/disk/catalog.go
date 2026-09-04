package disk

import (
	"path/filepath"
	"strings"
)

// 注記の文言。**同じ「再起動で消える」という事実でも、中身によって伝えるべき含意が逆になる**。
//   - 再生成されるキャッシュ → 放置してよい (急いで消さなくてよい)
//   - ユーザーファイルかもしれないもの (Inspect / RiskConfirm) → 放置すると**失われる**。
//     ここに「急がなくてよい」と出すのは有害で、finder-nsird の Recover
//     (「唯一のコピーである可能性を否定できません」) と正面から矛盾する (敵対レビュー 2026-09-04)
const (
	RebootClearsNote = "再起動すると消える置き場です (急いで消さなくても構いません)"
	RebootLosesNote  = "再起動すると失われる置き場です (要るものは先に取り出してください)"
)

// dirhelperRoot は macOS が起動時と毎日 03:35 に掃除する置き場の根。
//
// 根拠 (2026-09-04 実測): `/System/Library/LaunchDaemons/com.apple.bsd.dirhelper.plist` が
// `RunAtLoad = true` / `StartCalendarInterval 03:35` / `CLEAN_FILES_OLDER_THAN_DAYS = 3` で、
// `strings /usr/libexec/dirhelper` に `Cleanup At Startup` と `/var/folders/` が在る
// (対象は **/var/folders 配下だけ**。`/private/var/tmp` の文字列は無い)。
//
// 🚨 **「起動時の掃除が 3 日ルールを無視するか」は未実測**。無視しないなら、直近 3 日に
// 作られたものは再起動しても残る。注記が「急がなくてよい」と言うぶんには安全側だが、
// 断定を強めるとき (「必ず消える」等) はここを実測してからにすること。
const dirhelperRoot = "/private/var/folders/"

// RebootNote はそのエントリに添える再起動の注記 (無ければ空文字)。
//
// 🚨 **判定に `$TMPDIR` という文字列を使わない**。テンプレートが `$TMPDIR/...` でも、
// 実効の TMPDIR が `/var/folders` の外を指していれば dirhelper は掃除しない
// (`TMPDIR=$HOME/tmp glogx doctor` で成立する)。文字列だけを見ると、**永久に残る
// ユーザーデータへ「放置してよい」と表示する** (敵対レビュー 2026-09-04 の P1)。
// 判定の出典は走査と同じ実効値 (env.TmpDir) に寄せる。
func RebootNote(e Entry, env Env) string {
	if len(e.Paths) == 0 {
		// 走査する場所が分かっていない (Guard だけで対象を決めるエントリ) 以上、
		// その置き場の性質は言えない
		return ""
	}
	for _, p := range e.Paths {
		if p != "$TMPDIR" && !strings.HasPrefix(p, "$TMPDIR/") {
			return ""
		}
	}
	if !tmpDirClearedOnReboot(env) {
		return ""
	}
	if e.Inspect || e.Risk == RiskConfirm {
		return RebootLosesNote
	}
	return RebootClearsNote
}

// tmpDirClearedOnReboot は実効 TMPDIR が dirhelper の担当範囲にあるか。
func tmpDirClearedOnReboot(env Env) bool {
	if env.TmpDir == "" {
		return false
	}
	p := normalizeSystemLinks(filepath.Clean(env.TmpDir))
	return strings.HasPrefix(p+"/", dirhelperRoot)
}

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
	// Chromium 由来のキャッシュだけを残し、そのアプリが起動中のものを落とす (guard.go)
	GuardChromiumCache Guard = "chromium-cache"
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
	// NotFreeable は「Size を『解放可能』の合計に足さない」印。
	// 🚨 サイズは測れるが、**その手順を踏んでも同じ量が macOS に返るとは限らない**対象のため
	// (docker-vm-disk: prune はイメージの中を空けるだけで .raw は縮まない)。
	// 行にはサイズを出す (どれくらい抱えているかは知りたい) が、見出しの「合計 N 解放可能」と
	// 起動時トーストの閾値には入れない。`go-modcache-old` のように提示した手順がそのまま
	// その量を解放するものは、propose でもこの印を付けない
	NotFreeable bool
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
//   - `~/src/**` のビルド成果物 / `node_modules` — プロジェクト単位の判断が要り、
//     allowlist の枠組みに合わない (issue 220)
//
// 🚨 **「この機に存在しないから載せない」を却下理由に使わない** (2026-09-04)。
// `electron-builder` / `deno` / `SpeechModelCache` の 3 件はこの理由で載せていなかったが、
// 再測したら **3 件とも実在した** (SpeechModelCache は 841MB)。不在は時間で覆るうえ、
// 覆ったことを誰も知らせてくれない (走査していないので doctor にも出ない)。
// 却下理由には不在ではなく**性質** (ユーザーデータ / 復元不可 / 枠組みに合わない) を書く。
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
	// `npm-cache` と分けてあるのは復元の契機が違うため (install ではなく次の npx 実行)。
	// 実測 2026-09-04: 108MB / 9 エントリ。
	// 🚨 **`_cacache` と同じ「安全」にしない** (敵対レビュー 2026-09-04)。_cacache は
	// content-addressed の読み取り専用アーカイブで、消しても走行中のプロセスに影響しないが、
	// _npx は**展開済みの node_modules で、そこから直接 require されている**。
	// `npx` で起動した常駐プロセス (MCP サーバ / dev サーバ / watcher) が動いている間に消すと、
	// 遅延 require の時点で `Cannot find module` で落ちる。プロセス判定を付けられない
	// (実行しているのは `node` で、名前から npx 由来だと分からない) ので、Risk を上げて
	// Detail で伝える形にしてある
	{ID: "npx-cache", Label: "npx の使い捨てインストール", Tier: 1, Risk: RiskCaution, DeleteVia: "rm",
		Recover: "次に同じ npx コマンドを叩いたとき再取得されます",
		Detail:  "npx で起動した常駐プロセス (MCP サーバ等) が動いている間は消さない (実行中の実体を消す)",
		Paths:   []string{"~/.npm/_npx"}},
	{ID: "swiftpm-cache", Label: "SwiftPM キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回の依存解決で再取得されます", Paths: []string{"~/Library/Caches/org.swift.swiftpm"}},
	{ID: "electron-cache", Label: "Electron キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "再ダウンロードされます", Paths: []string{"~/Library/Caches/electron"}},
	// 実測 2026-09-04: 82MB。`electron-cache` と同型 (ビルド時に落としてくる配布物のキャッシュ) だが、
	// 使う側のツールが違うので復元の契機を分けて書く
	{ID: "electron-builder-cache", Label: "electron-builder キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回のパッケージングで再ダウンロードされます", Paths: []string{"~/Library/Caches/electron-builder"}},
	// 実測 2026-09-04: 27MB。Deno の依存 (registry / npm / gen) の置き場で、
	// `DENO_DIR` を明示していなければここが既定
	{ID: "deno-cache", Label: "Deno キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "次回の実行で再取得されます", Detail: "DENO_DIR を設定している場合はここではない。上流が消えた版は再取得できない",
		Paths: []string{"~/Library/Caches/deno"}},
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
	// Electron / Chromium アプリの HTTP・コード・GPU キャッシュ。**この機で最大の未カバー領域**
	// (実測 2026-09-04: 合計 2.7GB。Slack 1.7GB / Multi-Video Player 系 509MB / Electron 495MB)。
	//
	// 🚨 **`~/Library/Application Support/*` に glob を張る唯一のエントリ**なので、2 つの絞り込みを
	// guard で必ず通す (catalog に書いた glob だけでは危ない):
	//   - **Chromium 由来だと確かめる**。同じ階層に `Code Cache` / `Preferences` / `Local Storage` が
	//     揃うことを見る (実測 2026-09-04: `Cache` を持つ 6 アプリすべてで 4 点セットが揃っていた)。
	//     Chromium でないアプリの `Cache` に価値あるものが入っている可能性を切る
	//   - **起動中のアプリは外す**。Chromium は走行中に書き続けるので、消すと不整合を起こす
	//     (`chrome-tmp` と同じ理由)。判定はアプリのディレクトリ名 = プロセス名の完全一致
	//
	// 🚨 **mtime では起動中を判定できない** (実測 2026-09-04): 稼働中の Dropbox を含む全 6 アプリの
	// `Cache` ディレクトリの mtime が 39 日前の起動時刻より古かった (Chromium は配下のファイルを
	// 書き換えるのでディレクトリの mtime が動かない)。GuardBoottime をここに使ってはいけない。
	// 🚨 `Unverified` を付けてある = **0 件でも行を畳まない**。目印 (Chromium 由来かの判定) は
	// この機の 6 件で確かめただけの経験則なので、Electron がプロファイルの形を変えたら
	// **全アプリが目印で落ちて候補 0 件**になる。畳むとそれが「候補なし = きれい」と
	// 同じ見え方になり、2.3GB が画面から消える (敵対レビュー 2 周目)。
	// 起動中で全滅したときは blocked (guard 側) が受けるが、**形状で全滅した場合はここが受ける**
	{ID: "chromium-cache", Label: "Electron アプリのキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "アプリが次に必要としたとき再取得されます", Detail: "起動中のアプリのぶんは候補から外れる (終了して r で再スキャン)",
		Unverified: "Chromium の目印 (Preferences / Local Storage) は 1 機 6 件の実測。形が変われば黙って 0 件になる",
		Paths: []string{
			"~/Library/Application Support/*/Cache",
			"~/Library/Application Support/*/Code Cache",
			"~/Library/Application Support/*/GPUCache",
			// 🚨 `Service Worker/CacheStorage` は**意図的に載せない** (敵対レビュー 2026-09-04。
			// この機では Slack の 408MB が該当するが見送る): Cache API は**アプリが任意のデータを
			// 置ける場所**で、認証が要る資産やもう配信されていないメディアが入りうる。
			// 「アプリが次に必要としたとき再取得されます」を無条件には言えないので、
			// Risk: safe / rm の枠に入れられない。載せるなら別エントリ (caution) にして、
			// 中身が再取得できることを実測してから
		}, Guard: GuardChromiumCache},
	// 現在このカタログを作った機には**存在しない**が、性質で登録する (issue 218 の「不在を却下理由に
	// しない」。実測 2026-09-04 時点で 0 件 = 畳まれる)。どちらも Xcode が再生成する純キャッシュで、
	// 溜まるときは GB 単位になる
	{ID: "xcode-doccache", Label: "Xcode ドキュメントキャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "Xcode がドキュメントを開いたとき再取得されます",
		Paths:   []string{"~/Library/Developer/Xcode/DocumentationCache/*"}},
	// 🚨 起動中のシミュレータがあるときは触らない (敵対レビュー 2026-09-04)。dyld キャッシュは
	// 再生成されるのでデータは失わないが、走行中のシミュレータではアプリの起動が失敗しうる。
	// 他のシミュレータ系エントリが guard を持つのにここだけ素通しだった
	{ID: "coresimulator-caches", Label: "シミュレータの dyld キャッシュ", Tier: 1, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "シミュレータを次に起動したとき再生成されます (初回起動が遅くなるだけ)",
		Paths:   []string{"~/Library/Developer/CoreSimulator/Caches/*"},
		Guard:   GuardProcessAbsent, Processes: []string{"Simulator"}},
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
		// ✅ **接頭辞は実測で確定した** (2026-09-04。issue 169 のうちこのエントリぶんは解決)。
		// この機の /private/var/tmp に `XCTestTesting.<日時>.spindump.txt` が **12 件 92MB** 在る
		// (`XCTestTesting.2026-04-08-00:37:03.spindump.txt` 等)。glob はそのまま当たる。
		// 🚨 **上の xctest-logarchive の未実測は解決していない**: `.logarchive` は同じ時点で
		// **0 件**で、接頭辞を共有する根拠にはならなかった (2 つの前提が同時に立ったわけではない)。
		// 🚨 `Unverified` を外したので、このエントリは **0 件のとき畳まれる側**へ移った
		// (`Foldable`)。将来 Xcode が接頭辞を変えたら「候補なし」と同じ見え方になる。
		// 根拠は 1 台の 12 件なので、他機で 0 件が続くなら実名を採り直すこと。
		Paths: []string{"/private/var/tmp/XCTestTesting.*.spindump.txt"}, Guard: GuardBoottime},
	{ID: "coresimulator-orphan", Label: "孤児シミュレータの作業領域", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "現存しないデバイスの残骸。再生成されません", Detail: "simctl list devices に無い UUID だけ (現役の作業領域は候補にしない)",
		Paths: []string{"/private/var/tmp/com.apple.CoreSimulator.SimDevice.*"}, Guard: GuardSimDevice},
	{ID: "launchd-tmp", Label: "launchd の一時ディレクトリ残骸", Tier: 2, Risk: RiskSafe, DeleteVia: "rm",
		Recover: "容量はほぼ 0。件数削減が目的", Paths: []string{"/private/var/tmp/com.apple.launchd.*"}, Guard: GuardBoottime},
	// 音声認識モデル (BNNS の推論グラフ) の置き場。issue 218 は「この機に存在しない」を理由に
	// 載せていなかったが、**探した場所が違った** (~/Library/Caches ではなく /private/var/tmp)。
	// 実測 2026-09-04: **841MB / 12 ファイル**、所有者は本人 (koji:wheel)、mtime は 2026-01-28〜02-12。
	// 🚨 **再生成されるかは未実測**なので `trash` にしてある (rm ではない): 音声入力が壊れても
	// ゴミ箱から戻せる状態でだけ触ってよい。rm へ格上げするなら「消した後に音声入力を使って
	// 再ダウンロードされる」ことを実測してから。
	// 🚨 **GuardBoottime は「使われていない」を意味しない** (敵対レビュー 2026-09-04 の指摘で訂正)。
	// mtime は書き込み時刻で、**読み出しでは更新されない**ので、現に今日使っているモデルも
	// mtime が古ければ候補に入る。この guard が落とすのは「起動後に新規ダウンロードされた分」だけ。
	// 実測 2026-09-04 のこの機では 12 件すべてが起動より古く、**1 件も落ちていない**。
	{ID: "speech-model-cache", Label: "音声認識モデルのキャッシュ", Tier: 2, Risk: RiskCaution, DeleteVia: "trash",
		Recover: "音声入力/音声認識を次に使うとき再ダウンロードされます (未実測。だからゴミ箱経由)",
		Detail:  "起動より古いものだけ。/var/tmp は再起動で消えないので溜まり続ける",
		Paths:   []string{"/private/var/tmp/SpeechModelCache/*"}, Guard: GuardBoottime},
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
		// 🚨 **`~/.anyenv/envs/*` は意図的に走査しない** (2026-09-04 に足して撤回した。同じ提案を再生成しないこと):
		//   - **原理的に孤児と判定できない**。`effectiveVMRoot` は「<TOOL>_ROOT が無ければ
		//     `~/.anyenv/envs/<tool>` が実効 root」という規則 (= `_zshrc` のローダと同じ) なので、
		//     anyenv 配下のディレクトリは**存在すること自体が「現役」の定義**になる。
		//     結果はいつも「現役 (候補なし)」か「診断できず」のどちらかで、候補は出ない
		//   - **走らせるとリスクだけが増える**。Go の `filepath.Glob` は shell と違い先頭ドットにも
		//     マッチする (実測 2026-09-04) ので、`.git` / `.DS_Store` / 手で退避した `nodenv-backup` が
		//     流れ込み、`filepath.Base` 由来の tool 名 (`.git` → `git`) が `~/.git` と比較されて
		//     不一致 = 孤児になる。「そのマネージャで入れた言語の全世代が消えます」の説明つきで
		//     ゴミ箱候補に出る (敵対レビュー 2026-09-04 の P1)
		//   再開の trigger: anyenv 配下の残骸で実際に容量を食っているのを見たとき。そのときは
		//   このカタログではなく「anyenv の台帳 (`anyenv versions`) と突合する」別の判定が要る
		Paths: []string{"~/.rbenv", "~/.nodenv", "~/.goenv"}, Guard: GuardVMRoot},
	// Docker Desktop の VM ディスクイメージ。実測 2026-09-04: 1,035MB。
	// 🚨 **rm しない / 数字は「イメージの大きさ」であって解放できる量ではない** (だから NotFreeable)。`docker system prune` は
	// **イメージの中**を空けるだけで .raw は縮まず、macOS 側に容量が戻るのは Docker Desktop の
	// "Clean / Purge data" か .raw ごと消したときだけ (消すとイメージ・コンテナ・ボリュームが全部消える)。
	// だから `propose` (コマンドを出すだけ。`go-modcache-old` と同じ扱い) にしてある
	// Risk が confirm でなく caution なのは、**engine が confirm × 非 trash を拒否する**ため
	// (delete.go の「risk: confirm はゴミ箱移動でなければ削除しません」)。propose はそもそも
	// ツールが何も触らない経路なので、`go-modcache-old` と同じ組み合わせに揃える。
	// 失うものの説明は Recover に置いてあり、そちらは常に表示される
	{ID: "docker-vm-disk", Label: "Docker の VM ディスクイメージ", Tier: 2, Risk: RiskCaution, DeleteVia: "propose", NotFreeable: true,
		Recover: "消すとイメージ・コンテナ・ボリュームが全部消えます (再取得・再ビルドが要る)",
		Detail:  "prune では .raw は縮まない。容量を戻すには Docker Desktop の Clean/Purge data",
		Paths:   []string{"~/Library/Containers/com.docker.docker/Data/vms/*/data/Docker.raw"}},
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
