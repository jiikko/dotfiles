# 024 feat(glogx): 起動時に Claude Code の新バージョンをバックグラウンド検出してトースト通知

## 背景

glogx には既に `C` キーで `claude update` を即実行する機能がある (action_modal.go)。しかし
「新バージョンが出ていること」に気づく手段がなく、ユーザーが能動的に `C` を押すか
claude 側の通知を見るしかない。glogx は日常的に起動する TUI なので、起動のついでに
バックグラウンドでバージョン確認し、更新可能なら右下トーストで知らせると `C` の導線が生きる。

## やりたいこと

- glogx 起動時、**バックグラウンドで** Claude Code CLI のインストール済みバージョンと
  最新公開バージョンを比較する (起動をブロックしない)
- 最新の方が新しければ、右下トーストで「Claude Code v2.1.220 が公開されています (C で更新)」
  のような通知を 1 回出す
- 最新バージョンの取得結果は **約 1 時間キャッシュ** し、起動のたびに外部へ問い合わせない
- 取得失敗 (オフライン / npm 不在 / タイムアウト) は完全に無音でスキップする
  (バージョン通知は付加情報。`usage.FetchVersion` の「欠けても主処理は成立」と同じ方針)

## 実装詳細

### 1. 最新バージョンの取得 (external_commands.go)

`runClaudeUpdate` / `usage.FetchVersion` と並ぶ差し替え点として追加:

```go
// fetchLatestClaudeVersion はテストで実ネットワークに触れないための差し替え点。
// npm registry の dist-tags から latest を取る (claude update の配布元と同じ)。
var fetchLatestClaudeVersion = func(ctx context.Context) string {
    // GET https://registry.npmjs.org/@anthropic-ai/claude-code/latest → JSON の "version"
}
```

- 取得手段は **npm registry への HTTP GET** を第 1 候補とする
  (`https://registry.npmjs.org/@anthropic-ai/claude-code/latest` は `{"version":"x.y.z",...}` を返す)。
  `npm view` の exec より依存 (npm の有無) が少なく、stdlib `net/http` + `encoding/json` で足りる
- timeout は短め (3〜5s) の `context.WithTimeout`。起動直後のバックグラウンド処理であり、
  遅延してもユーザーは気づかないが、goroutine を長く残さない
- native installer 配布でも npm registry の latest タグは同一バージョン系列なので比較指標として有効

### 2. インストール済みバージョンの取得

既存の `usage.FetchVersion(ctx)` (`claude --version` → "2.1.216") をそのまま使う。
空文字 (取得失敗) なら比較せず終了。

### 3. バージョン比較

semver ライブラリは入れず、`"2.1.216"` を `.` で 3 分割して数値比較する小さな純関数
`versionLess(installed, latest string) bool` を書く (パース失敗は false = 通知しない)。
テストは純関数単体で書ける。

### 4. 1 時間キャッシュ (cache.go の隣に claude_version_cache.go)

CI キャッシュ (`cache.go`) と同じ流儀 (JSON + `writeAtomic`) で、リポジトリ非依存の別ファイルにする:

- パス: `$XDG_CACHE_HOME/glog/claude-latest-version.json` (`CachePath` と同じ base 解決を再利用)
- 内容: `{"latest": "2.1.220", "fetchedAt": "..."}`
- TTL: `const claudeVersionTTL = time.Hour`
- fresh ならネットワークに出ずキャッシュ値で比較する。**キャッシュするのは「latest の取得結果」
  だけ**で、「通知したかどうか」は永続化しない (毎起動でトーストが出るのは TTL 内なら同じ
  latest との比較なので許容。むしろ更新し忘れのリマインドになる)
- 取得失敗時はキャッシュを更新しない (古い fresh 値もない場合は単に無通知)

### 5. TUI への接続 (tui.go)

bubbletea の作法に沿って `Init()` の `tea.Batch` に 1 つ Cmd を足す:

```go
type claudeUpdateAvailableMsg struct{ latest string }

// checkClaudeVersionCmd: キャッシュ読込 → (stale なら) fetch → 比較 → 新しければ msg、
// それ以外は nil を返す tea.Cmd
```

`Update` で `claudeUpdateAvailableMsg` を受けたら既存 toast を使う:

```go
case claudeUpdateAvailableMsg:
    m.toast.show("Claude Code v"+msg.latest+" が公開されています (C で更新)", true)
```

- toast は既に seq 世代管理・上書き対応済み (toast.go) なので、push/pull 完了トーストと
  競合しても後勝ちで自然に振る舞う。専用の表示機構は作らない
- `C` (claude update) の実行導線は既存のまま。この issue は「気づかせる」だけ

### 6. テスト

- `versionLess` の表 (等しい / patch 差 / minor 差 / 桁違い / パース不能)
- キャッシュの fresh/stale 判定と writeAtomic の往復 (cache_test.go と同型)
- `fetchLatestClaudeVersion` を差し替えた上で、`checkClaudeVersionCmd` が
  「新しい → msg」「同じ / 取得失敗 → nil」を返すこと (toast_integration_test.go の流儀)
- 実ネットワーク・実 claude CLI に触るテストは書かない

## スコープ外

- 自動で `claude update` を実行すること (通知のみ。実行は既存の `C`)
- glogx 自身のバージョン更新通知
- npm 以外の配布チャネル (native installer) のバージョン照会 API 対応
