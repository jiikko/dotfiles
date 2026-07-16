# GitHub ActionsのCI結果を非同期表示する`git log`ラッパーを作る

## 背景

ローカルでコミット履歴を確認するとき、各コミットに対応するGitHub Actions / GitHub Checksの成否も同時に確認したい。

現在は`git log`とGitHubのWeb画面または`gh`コマンドを往復する必要があり、特に複数コミットのCI状態を追うときに認知負荷が高い。

通常の`git log`と同程度の初動速度を維持しつつ、CI状態を後から非同期で埋めるラッパーコマンドをGoで実装する。

## 目標

- コマンド実行直後に、Git履歴を待たずに表示する
- GitHub APIの取得完了を待たず、CI状態をプレースホルダー付きで描画する
- CI状態を取得したら、該当コミットの表示を非同期更新する
- フルスクリーンの対話型TUIではなく、`git log`に近い使い捨てCLIとして動作する
- Bubble Teaをイベントループと再描画基盤として利用する
- GitHub API障害・未認証・ネットワーク障害があっても、Git履歴の表示自体は成立させる

## 想定コマンド名

仮称として`glog`を使用する。

```bash
glog
glog -n 1
glog -n20
glog --stat
glog -p
glog --cached
```

最終的なコマンド名は実装時に既存alias・コマンドとの衝突を確認して決定する。

## UIイメージ

初回描画:

```text
⠋ 91a72bd Fix invoice calculation      koji  2 hours ago
⠋ 7b18e20 Update GraphQL schema         koji  5 hours ago
✓ d81a991 Update README                  koji  1 day ago
```

GitHub API取得後:

```text
✓ 91a72bd Fix invoice calculation      koji  2 hours ago
✗ 7b18e20 Update GraphQL schema         koji  5 hours ago
✓ d81a991 Update README                  koji  1 day ago
```

状態表現の候補:

| 表示 | 意味 |
|---|---|
| `✓` | すべての対象Checkが成功 |
| `✗` | 1つ以上の対象Checkが失敗 |
| `●` | queued / in_progress / pending |
| `⊘` | cancelled / skipped / neutral |
| `–` | Checkが存在しない |
| `?` | 未取得・取得不能 |
| `⠋` | 取得中 |

絵文字ではなく、端末幅とフォント差異の影響が小さい1カラム記号を優先する。

## Bubble Teaの採用方針

Bubble Teaは対話操作のためではなく、以下を安全に扱うために使う。

- 初回表示と非同期API結果の再描画
- goroutineから直接stdoutを操作しない構造
- API完了・失敗・キャンセルをMessageとして集約するイベントループ
- `Ctrl-C`時の終了処理
- ターミナル幅変更への最低限の追従
- 将来的な`--watch`追加余地

### フルスクリーンTUIにはしない

- Alt Screenへ切り替えない
- カーソル移動や選択UIを実装しない
- GitHub API取得完了後は自動終了する
- 終了後、最終的なログ表示を通常のターミナル履歴に残す
- キー操作は原則`Ctrl-C`だけとする

Bubble Teaを「TUIアプリのUI部品」ではなく、「非同期レンダリング可能なCLIランタイム」として利用する。

## 実行フロー

```text
1. CLI引数を解析
2. GitリポジトリとGitHub remoteを解決
3. git logまたはgit diff --cachedを開始
4. ローカル履歴を即時描画
5. キャッシュ済みCI結果を反映
6. 未取得・期限切れのSHAをGitHubへ一括問い合わせ
7. Bubble Tea Messageで状態を受信
8. Modelを更新して再描画
9. 取得完了後に終了
```

Git履歴の取得とGitHub API取得は分離する。GitHub側の失敗で`git log`まで失敗させない。

## 対応する引数

`git log`の全引数互換は目標にしない。

利用頻度の高い引数を明示的なallowlistとして対応する。未対応引数を無条件に透過すると、出力構造が変わってパーサーやレンダラーが壊れるため、対応範囲を管理する。

### 初期対応

- `-n <count>`
- `-n<count>`（例: `-n1`, `-n20`）
- `--max-count=<count>`
- `--stat`
- `-p`
- `--patch`
- revision指定（例: `main`, `HEAD~10..HEAD`）
- `-- <pathspec>`

### `--cached`について

`git log`自体は`--cached`をサポートしないため、ラッパー独自のモードとして扱う。

```bash
glog --cached
```

この場合は以下を表示する。

1. `HEAD`コミットのCI状態
2. `git diff --cached`の内容

`--cached --stat`は`git diff --cached --stat`、`--cached -p`は`git diff --cached -p`相当として扱う。

ステージ済み変更そのものにはGitHub上のCI結果が存在しないため、「HEADのCI状態」であることをUI上で明示する。

例:

```text
HEAD CI: ✓ 91a72bd Fix invoice calculation
Staged changes:
 3 files changed, 42 insertions(+), 8 deletions(-)
```

### 初期対応しないもの

以下は初期スコープ外とする。

- `git log`の全オプション
- `--graph`の完全互換
- 独自`--pretty` / `--format`
- pagerとの完全互換
- `--follow`
- reflog表示
- bisect / replace refなど特殊な履歴表示
- GitHub以外のホスティングサービス
- GitHub Enterprise Server

未対応引数を受け取った場合は黙って無視せず、対応していない旨と代替の`git log`コマンドを表示する。

## `--stat` / `-p`の表示設計

`--stat`や`-p`では1コミットが複数行になる。CI記号はコミットヘッダー行にだけ付与する。

```text
✓ commit 91a72bd
  Author: koji
  Date: ...

      Fix invoice calculation

  app/models/invoice.rb | 12 ++++++------
  1 file changed, 6 insertions(+), 6 deletions(-)
```

パッチやstatの各行へ状態用プレフィックスを付けない。

### コミット境界の識別

人間向け出力を正規表現だけで解析しない。内部では`git log --pretty=format:`へ制御文字ベースのレコード区切りを追加する。

候補:

- commit record separator: `%x1e`
- field separator: `%x1f`

取得項目:

- full SHA
- short SHA
- subject
- author name
- relative date
- decorations

`--stat` / `-p`の本文はコミットレコードの後続部分として保持し、レンダリング時にヘッダーと本文へ分離する。

Gitの出力色を維持する場合も、ANSIエスケープシーケンスをコミット境界判定に利用しない。

## GitHub連携

### 認証

認証はGitHub CLIへ委譲する。

- `gh auth status`で認証確認
- API呼び出しは`gh api graphql`を第一候補とする
- 独自トークン保存は行わない

`gh`が未インストール・未認証の場合もGit履歴だけ表示し、CI欄は`?`または`–`にする。

### リポジトリ解決

以下からowner/repositoryを解決する。

1. 現在のupstream remote
2. `origin`
3. GitHub形式のremote URL

HTTPSとSSHの両方へ対応する。

```text
https://github.com/owner/repo.git
git@github.com:owner/repo.git
ssh://git@github.com/owner/repo.git
```

GitHub以外のremoteはCI取得対象外とする。

### API

GraphQLの`statusCheckRollup`を使用し、表示対象コミットのSHAを1リクエストへまとめる。

コミットごとにREST APIを逐次呼び出さない。

集約ルール:

1. 1件以上失敗がある → failure
2. queued / pending / in_progressがある → pending
3. 対象Checkがすべて成功 → success
4. cancelled / skipped / neutralのみ → neutral
5. Checkなし → none
6. APIエラー → unknown

必要になった場合のみ、詳細表示用にCheck Run名を取得する。初期版ではコミット単位の集約状態だけでよい。

## キャッシュ

初回表示の体験を改善し、APIレート消費を抑えるためローカルキャッシュを持つ。

候補パス:

```text
$XDG_CACHE_HOME/glog/github.com/<owner>/<repository>.json
```

`XDG_CACHE_HOME`未設定時は`~/.cache/glog/`を利用する。

状態別TTL候補:

| 状態 | TTL |
|---|---:|
| success / failure | 24時間 |
| cancelled / skipped / neutral | 1時間 |
| pending / in_progress | 5〜10秒 |
| no checks | 5分 |
| API error | 30秒 |

CIは再実行可能なため、完了状態も永久キャッシュにはしない。

将来的に以下を追加可能にする。

```bash
glog --refresh
glog --no-cache
```

## Bubble TeaのModel / Message設計

### Model

```text
Model
├── commits []Commit
├── cachedStatuses map[SHA]CIStatus
├── fetchedStatuses map[SHA]CIStatus
├── loading bool
├── fetchError error
├── terminalWidth int
├── mode log|cached
└── done bool
```

### Message

```text
GitLoadedMsg
CachedStatusesLoadedMsg
CIStatusesLoadedMsg
CIFetchFailedMsg
WindowSizeMsg
QuitMsg
```

GitHub API用goroutineがstdoutへ直接書き込まず、結果を必ず`tea.Msg`として返す。

初期版ではCI状態をコミットごとに細かく更新する必要はない。GraphQL一括取得の完了時にまとめて1回再描画すればよい。

```text
初回描画 → キャッシュ反映 → API結果反映
```

この2〜3段階の描画で十分な体感速度を得られる。

## TTYとパイプ出力

stdoutがTTYの場合のみBubble Teaによる動的更新を有効化する。

```bash
glog
```

stdoutがパイプ・リダイレクトの場合は動的描画を行わず、CI取得完了後に静的な最終結果を1回だけ出力する。

```bash
glog | grep '✗'
glog > commits.txt
```

機械処理向けJSON出力は初期スコープ外とするが、将来的な`--json`を妨げない内部モデルにする。

## pager

初期版では独自pagerを実装しない。

- 端末内に収まらない出力もそのままstdoutへ表示する
- Bubble Tea動作中に`less`を挟まない
- `-p`で巨大な出力になる場合は、`-n`併用を推奨する

将来pager対応を追加する場合は、「非同期更新完了後に静的出力をpagerへ渡す」方式を検討する。pager内の行を非同期更新する設計にはしない。

## エラーハンドリング

- Gitコマンド失敗: Gitのstderrと終了コードを返して終了
- Gitリポジトリ外: 明示的エラー
- remoteなし: CIなしでGit履歴だけ表示
- GitHub以外のremote: CIなしでGit履歴だけ表示
- `gh`未導入 / 未認証: 警告を1行表示し、Git履歴は表示
- APIタイムアウト: キャッシュまたはunknown状態で終了
- API rate limit: reset情報を短く表示し、Git履歴は表示
- 不正・未対応引数: 利用可能な引数一覧を表示して終了

APIエラーをコマンド全体の失敗扱いにするかは終了コード設計で分離する。基本方針は「Git履歴取得成功ならexit 0」とする。

## 想定ディレクトリ構成

実装時に既存のdotfiles構成へ合わせて調整するが、Goコードは1ファイルへ詰め込まない。

```text
tools/glog/
├── go.mod
├── cmd/glog/main.go
└── internal/
    ├── app/model.go
    ├── cli/options.go
    ├── git/log.go
    ├── github/checks.go
    ├── cache/store.go
    └── view/render.go
```

dotfilesからPATHへ公開する薄いshimまたはsymlinkを追加する。

## テスト方針

### Unit test

- CLI引数のallowlist判定
- `-n1` / `-n 1` / `--max-count=1`の正規化
- `--cached`と`git log`モードの排他制御
- remote URLのowner/repository変換
- Check状態の集約ルール
- キャッシュTTL判定
- commit record separatorの解析

### Snapshot test

- 通常ログ
- `--stat`
- `-p`
- pendingからsuccess / failureへの再描画
- 狭いターミナル幅
- API失敗時

### Integration test

一時Gitリポジトリを作成し、以下を確認する。

- 複数コミットの順序を維持する
- `-n1`で1件だけ出る
- `--stat`の本文を壊さない
- `-p`のpatchを壊さない
- `--cached`でstaged diffを表示する
- 非TTY時にANSIカーソル制御を出力しない

GitHub APIはfixtureまたはfake commandで置き換え、通常テストで外部通信しない。

## 実装フェーズ

### Phase 1: 静的MVP

- Go CLI作成
- 対応引数の解析
- `git log` / `git diff --cached`実行
- GitHub GraphQL一括取得
- CI付き最終結果を1回表示

### Phase 2: Bubble Teaによる非同期描画

- Git履歴を即時表示
- キャッシュ反映
- API結果受信後の再描画
- TTY判定
- `Ctrl-C`処理

### Phase 3: 実用性改善

- XDGキャッシュ
- タイムアウト
- remote解決強化
- 表示幅調整
- `--refresh` / `--no-cache`

### Phase 4: 任意機能

- `--watch`
- Check Run詳細
- 失敗したworkflowへのURL
- `--json`
- pager連携

## 完了条件

- [x] `glog`実行直後にコミット履歴が表示される
- [x] GitHub API取得後にCI状態が非同期で更新される
- [x] Alt Screenを使用せず、最終表示がターミナル履歴へ残る
- [x] `-n 1`、`-n1`、`--max-count=1`が動作する
- [x] `--stat`が動作し、コミットヘッダーにだけCI状態が付く
- [x] `-p` / `--patch`が動作し、patch本文を壊さない
- [x] `--cached`がwrapper独自モードとして動作する
- [x] 未対応引数を黙って無視しない
- [x] `git log`の全引数対応を要件にしない旨がヘルプとREADMEに明記される
- [x] GitHub API失敗時もGit履歴を表示できる
- [x] stdoutが非TTYの場合、動的描画用ANSI制御を出さない
- [x] GraphQLで表示対象コミットを一括取得する
- [x] キャッシュとAPI取得がテスト可能な境界に分離されている

## 非目標

- `git log`の完全なdrop-in replacement
- lazygitのようなコミットブラウザ
- キーボードで選択・展開する対話型UI
- GitHub Actionsのジョブログ閲覧
- CIの再実行操作
- 全Gitホスティングサービス対応

## 懸念点

- Bubble Teaのinline描画と長い`-p`出力の相性
- 端末幅による行折り返しで再描画範囲がずれる可能性
- ANSIカラーを維持したままcommit境界を安全に解析する必要がある
- GitHubのmerge queueやpull request用一時SHAは、ローカルコミットSHAと一致しない場合がある
- Checkが存在しない状態とAPI取得失敗を明確に区別する必要がある
- CI結果の再実行によるキャッシュ陳腐化

長いpatch表示の動的更新が不安定な場合は、`-p` / `--stat`ではコミットヘッダー部分だけを先に描画して本文を固定する、またはCI取得完了後に全体を1回だけ再描画する方式へフォールバックする。

---

## 追記: 完了後の拡張 (2026-07-16)

初期実装 (6e99167) の完了後、同日のユーザー指示で以下を拡張した。
**「キーボードで選択・展開する対話型UI」は本 issue の非目標だったが、明示指示により解禁**
(経緯は `src/glog/tui.go` 冒頭コメントと README 設計メモにも記載)。

- 既定の表示を git log 標準 (medium) 形式へ変更、コンパクト 1 行表示は `--oneline` に移設 (15987d6)
- less 風の対話ブラウズ: j/k (+ Ctrl-N/P a5d98ac) でカーソル移動・q で終了・
  終了時に最終結果を静的出力して履歴に残す。less -F 相当のショートカットと `--no-pager` (15987d6)
- CI job 一覧の表示: 当初インライン展開 + ツリーナビゲーション (c14dcf8) →
  開閉で高さがガタつくため画面上部のポップアップパネルへ変更 (c6afe57)。
  パネル内で job を選び Enter でジョブ詳細ページをブラウザ起動 (detailsUrl / targetUrl)
- TUI でコミットメッセージを端末幅で折り返し (git log の見え方に一致、021e1ca)
- キャッシュ肥大防止: TTL 切れの保存時間引き + エントリ数上限 2000 (56a2f89)
- `--help` の作り込みと README 全面改訂 (283d03a)

懸念点に挙げた「端末幅による行折り返しで再描画範囲がずれる可能性」は、
TUI 側で行を幅内に収める (メッセージは折り返し、他は切り詰め) ことで回避した。
未対応のまま残っているもの: `--watch` / `--json` / pager 連携 / GitHub Enterprise Server
(必要になったら新規 issue として起票する)。
