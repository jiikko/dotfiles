# 671 (chore): `src/` の Go module を go.work で 1 つの workspace にし、module をまたいで探せるようにする

起票日: 2026-10-08

## 概要

`src/` には Go の module が 17 個あり (`src/*/go.mod`)、go.work は無い (2026-10-08)。gopls は module ごとに閉じているので、
定義へのジャンプ・参照・workspace のシンボル検索が module をまたがない。
**既にある再利用できる package に気づけない**。実例: `bin/tmux-toast` は表示セル幅を python3 で自前に数えているが、
`src/tuikit/termwidth` が既にある (issue 670)。

go.work を置き、エディタ (nvim の gopls) と LLM の LSP ツールから全 module を 1 つの workspace として引けるようにする。

## 🚨 ビルドの解決を変えないこと (この issue の主なリスク)

- `go` コマンドは cwd から親へ go.work を探す。`src/go.work` を置くと、`src/<module>` で走る `go build` / `go test` / `go list` が
  **黙って workspace mode に入る**
- 今のビルドは module 単位で、依存先は各 `go.mod` の `replace` で解決している (`replace` を持つ module: glogx / ratelimit / doctor /
  pro-con / schedkeys / runtimeout / tuikit / zundamon-kaisetsu の 8 個)。workspace mode では go.work の `use` が優先され、解決結果が変わりうる
- 影響を受ける経路: `bin/lib/go_autobuild.zsh` (bin のラッパーの再ビルド)、各 `src/<module>/Makefile` の lint / test、
  root の `make test-go` / `make test-go-lint`、`.github/workflows/src_*.yml`、`go mod tidy`
- `GOWORK` を既に使っている箇所は `tests/scripts/test_tuikit_consumers_aligned.sh` だけ (`GOWORK=off go list -m`。「go.work があると全 module が
  workspace の版で解決され、ずれが見えなくなる」ため)。go_autobuild・Makefile・workflow には 0 件 (2026-10-08、`git grep GOWORK`)。
  **go.work があると、go.mod 間の版のずれの検出が構造的に弱まる**。この検査は今の形のまま GOWORK=off を保つ
- `bin/lib/go_autobuild.zsh` の指紋 (`_go_autobuild_inputs`) は `*.go` と各 root の `go.mod` / `go.sum` だけで、go.work を数えない。
  go.work が解決を変えると、**指紋の外で**ビルド結果が変わる (再ビルドも起きない)
- go.work を置くと `go.work.sum` もできる (sum の置き場が増える)。`go mod tidy` は workspace を無視して module 単位で動く
- CI の `cache-dependency-path` / `go-version-file` は go.work を見ないので、キャッシュの鍵は変わらない

## 対応方針 (案。実装の前に決める)

| 案 | 中身 | 欠点 |
|---|---|---|
| A. `src/go.work` を commit し、ビルド・テストの全経路で `GOWORK=off` | エディタは自動で拾う | 経路を 1 つでも漏らすと workspace mode で黙ってビルドされる。漏れを止める検査が要る |
| B. go.work を `src/` の外 (例: `nvim/` 側や `~/.cache`) に置き、gopls にだけ `GOWORK` を渡す (gopls の `build.env`。効くかは未確認) | ビルドの経路は何も変わらない | 手で打つ `go` には効かない。gopls を起動する経路ごとに設定が要る: `nvim/lua/dotfiles/lsp.lua` / `nvim/ftplugin/go.lua` / `_claude/settings.json` の 3 か所 |
| C. go.work を生成物にして gitignore し、使うときに作る | commit されたファイルがビルドを変えない | 手元に作った時点で A と同じリスクが出る |

どれを選ぶにしても:

- 採用前に、go.work の有無で解決が変わらないことを確かめる。🚨 **`go list -deps -f '{{.ImportPath}} {{.Module}}'` をそのまま比べない**:
  ローカル module の表示が `atomicfile v0.0.0 => ../atomicfile` から `atomicfile` に変わるので、解決が同じでも必ず差が出る
  (2026-10-08 の実測で glogx 58 行・pro-con 50・ratelimit 20・tuikit 10・doctor 4)。比べるのは
  **第三者の依存の版** (`.Module.Path` と `.Module.Version`、ローカル module を除く) と、**ローカル module の `.Module.Dir`** (同じ実体を指すか)。
  式は、上の既知の差が消えることで先に較正してから使う
- 実際に module をまたいで引けることを確かめる: gopls の workspace シンボル検索で、`src/glogx` を開いた状態から `src/tuikit/termwidth` のシンボルが出る
- A を選ぶなら、`go` を呼ぶ全 script / Makefile / workflow / `go_autobuild.zsh` の `go build` を機械で列挙して `GOWORK=off` を付け、
  抜けた経路を検出する。手で書いた一覧で経路を数えない

## 受け入れ条件

- [x] 案を決め、理由を本 issue に書く (go.work を置かない。下の進捗)
- [x] ~~go.work の有無で解決が一致することを確認した~~ → 測ったら一致しなかった (18 行の差。下の進捗) ので go.work を置かない方針に変えた。代わりに「repo に go.work / go.work.sum が無い・ビルド経路を変えていない」を確認
- [x] `make test-go` と CI の `src_*.yml` が今までどおり通る (make test-go: 17 module すべて rc=0。CI: commit 1b2c0a2e の src/doctor・glogx・ratelimit・restartable・pro-con すべて success)
- [x] gopls で module をまたいだシンボル検索が効くことを確認した (nvim。下の進捗の実機)
- [x] Claude Code の gopls プラグインが `bin/gopls` 経由で起動する (下の進捗)
- [x] 使い方 (go.work を置かない理由・nvim と Claude の経路) を `src/README.md` に書く

## 関連ファイル

- `src/*/go.mod` / `bin/lib/go_autobuild.zsh` / `Makefile` (`test-go`) / `.github/workflows/src_*.yml`
- gopls の設定: `nvim/lua/dotfiles/lsp.lua` / `nvim/ftplugin/go.lua` / `_claude/settings.json`
- `tests/scripts/test_tuikit_consumers_aligned.sh` (GOWORK=off の既存の使用)
- `src/README.md`

## 進捗

- 2026-10-08: 起票
- 2026-10-08: 反証レビュー (Claude のサブエージェント 1 本。codex は 5h 枠切れ) を反映。比較の式がそのままでは必ず差を出す (P1)、GOWORK の既存使用と版ずれの検出が弱まる点・go.work.sum・go_autobuild の指紋の外・gopls の設定 3 か所 (P2)。反証できなかった主張: module 17 / replace 8 / go が親へ go.work を探すこと。gopls の `build.env` で GOWORK が効くかは未確認
- 2026-10-08: codex-drive で着手。D1 (独立 2 案) と Claude の実測で、**go.work を置くと依存の解決が実際に変わる**ことが分かった
  (一時コピーに全 17 module を use した go.work を置き、`go list -deps` の module / version / dir を比べて 160 行中 18 行の差:
  chromecookie の golang.org/x/sys v0.9.0 → v0.47.0 / restartable が GitHub の pseudo-version で取っている termsafe・tuikit がローカルの実体に /
  go-colorful・go-runewidth が 3 module で上がる)。案 A (ビルドから見える go.work) も、エディタだけの go.work (案 B/C) も、gopls の解析がビルドと違う版になる
- ユーザー判断 (2026-10-08): **go.work は置かない**。nvim の gopls に dotfiles の全 module を workspace folder として渡し (`GOWORK=off`)、
  Claude Code の gopls は `bin/gopls` (mason の実体を絶対パスで起動する shim) で PATH に通す。Claude からの module 横断の検索は issue 672 の `src/PACKAGES.md` で代替する
- 受け入れ条件の読み替え: R2 は「Claude Code の gopls が起動できる」に、R3 は「go.work を導入しない・ビルド経路を変えない」に縮めた (ユーザー承認済み)
- 設計の敵対レビュー (codex 1 本): 6 件すべて採用 (別 repo の誤認 → git common dir で判定 / root_dir の中で共有設定を書き換えない → before_init で client ごと /
  module 外は丸めない / 失敗は既定に戻す / shim が他の gopls を奪う → mason → 自分を除いた PATH の順 / 受け入れに実 nvim と Claude の経路を入れる)
- 実装の敵対レビュー (codex 2 本、状態と shim): 採用 5 件 (再起動で folder が消える / root の無い Go ファイルの再利用の退行 / ハードリンクの相互 exec /
  shim が nvim の有無判定を通す → nvim は mason の絶対パス / stdio の透過のテスト)。
  **却下 1 件**: `PATH=""` のときの cwd 探索 (LSP のプラグインが空の PATH で起動する経路が無い。再提起するなら、空の PATH で gopls を起動する実際の経路を示すこと)
- 変異検証 (Claude、bin/mutate-verify-list): lsp.lua の dotfiles 判定・module 配下の判定・GOWORK=off の付与を外す 3 本は red。
  bin/gopls の自分のディレクトリの除外を外すのは red。mason の優先を外すのは**緑のままだった** (偽の実体が呼び出し側の環境から名前を取っていて、どちらが呼ばれても同じ名前を記録していた) → テストを直した
- 2026-10-08: push (1b2c0a2e) と `~/dotfiles` の pull の後の確認
  - R2: 新しい headless の Claude (haiku) の LSP ツールで workspaceSymbol が gopls から結果を返した。PATH 上の gopls は `bin/gopls` だけ (`which -a gopls`) なので、必ず shim を経由している。
    Claude の検索範囲は開いた module とその依存まで (lockman の NewLocker は出ない)。承認済みの方針どおりで、横断の発見は `src/PACKAGES.md` で補う
  - R1 (実機、glogx のファイルだけを開いた状態): folder は src の 17 module ちょうど。lockman の NewLocker / ローカルの tuikit の TruncateMeasure が返る。
    dotfiles の外の Go プロジェクト (一時ディレクトリの git repo + go.mod) は別 client・folder 1・GOWORK 無し
- 2026-10-08: 要件照合の敵対レビュー (codex 1 本) の未充足を直した: `:lsp restart` が保存済みの folder を使い回し、module を足しても反映されなかった
  → 再起動のとき列挙し直す (commit「fix(nvim): gopls の再起動で dotfiles の module を列挙し直す」)。実機の `:lsp restart gopls` の後も folder 17・GOWORK=off。変異で red を確認
- 未確認として残すもの: git が無い・時間切れ・rc≠0・列挙失敗の**個別の**注入テスト (今は「例外は既定の root に戻す」の一般の経路だけをテストしている)。
  trigger: gopls が開かない・既定の root に戻らないという報告が出たとき
