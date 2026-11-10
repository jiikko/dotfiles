# 1061 (research): obaket の Box の調査で dupscan を使った所見

起票日: 2026-11-11

所属: [932](932-epic-dupscan-accuracy.md)

## 概要

obaket の Box 対応の子 issue (epic `box`) を反証レビューの指摘で直す作業で、Swift の定義・参照・呼び出し元の確認に dupscan を使った所見。観察の記録で、仕様や設計の主張ではないので反証レビューは省略した。既存の issue との重複照合はしていない。

## 集計

- 摩擦 6 件 (見逃し 3 / 未対応 1 / 使い方が分かりにくい 1 / 入口 1)。提案は摩擦の項に 1 行ずつ添えた。
- dupscan を打った回数 45 (def・callers・refs・index・context・search・near)。別に `capabilities` 1 回。結果のファイルの数で数えた。
- 識別子の grep に戻した回数 0。ファイルの中の文字列を読む grep は 2 回 (`ImportConnectionsUseCase` の `case .none`、`HTTPStreamingDelegate` の `200..<300`)。
- 役に立った場面 3:
  - `def` が swift-testing の `@Test` 関数を本体つきで返し、`selectableSetMatchesRequiredSet` と `productionFactoryTransferExecution` の主張をそれぞれ 1 回で確かめられた。
  - `refs HTTPRangeResponseValidator` (7,016 B) が provider ごとの呼び出しを囲む関数つきで返し、202 の経路の確認に使えた。
  - `callers assertOrUpdateGoldens` が runner の呼び出しを示し、「Goldens は必須でない」という issue の誤りの裏が取れた。
- 効果の測定はしていない。上は使った場面の記録。

## 摩擦

### F1 `callers Type.member` が、呼び出しがあっても 0 件と答える (修正済み)

- 場面: `dupscan callers -limit 0 HTTPRangeResponseValidator.validate`
- 期待: 呼び出しの一覧か、絞れないという案内。
- 実際: rc=0、「呼び出しなし」。素の `validate` (30,950 B) には該当の呼び出しがある。`callers DropboxPKCE.generate` も 0 件で、素の `generate` なら 4 件。
- 種類: 見逃し (不存在の誤答)。`refs` は同じ形を rc=2 で拒否済みだった。
- 対応: `callers` も rc=2 で拒否し、単独名で探し直す `next_action` を出すようにした。判定を `parseQualifiedQuery` に寄せた。
- 対応 commit: `fix(dupscan): callers に Type.member を渡したら、呼び出しなしと答えず拒否する`。
- 確かめたこと: obaket の repo root と `Storage/HTTP` の両方の起点で、直す前は rc=0 で 0 件、直した後は rc=2 で stdout 空。変異 4 種 (拒否を外す、`-selector` を next_action から落とす、ラベルを落とす、点の演算子を拒否する) で red を確認した。`-selector` の変異は最初に緑で通り、fixture に同名の別の `Join` を足して red にした。

### F2 `index -name` は型の名前を引かず、0 件のときに `def` へ案内しない

- 場面: `dupscan index -name PKCE -limit 0` と `dupscan index -name DropboxPKCE -limit 0`
- 期待: `DropboxPKCE` と `GoogleDrivePKCE` が見つかる。
- 実際: どちらも rc=0、「当たる単位なし (単位 17842)」。`def DropboxPKCE` (1,351 B) は型のメンバーの一覧を返す。
- 種類: 見逃し。
- 提案: 0 件のとき、問い合わせを名前に含む型があれば `def` を `next_action` で案内する。

### F3 `nonisolated(nonsending)` を含むファイルが読めない構文になる

- 場面: obaket の repo root で `dupscan -v`。読めないファイルが 3 個で、`StorageReadUseCase.swift` と `TransferRetryCoordinator.swift` は `nonisolated(nonsending)` を引数の型に持つ。
- 再現: 4 行の fixture で出る。`struct S { func a(_ open: nonisolated(nonsending) () async throws -> Int) async throws -> Int { try await open() } }`
- 残り 1 個の `MoveReconcileExecutor.swift` は `nonsending` を含まず、原因は未調査 (`resolveRow` とその中の closure が読めない単位に出る)。
- 種類: 見逃し (読めない単位の定義・参照・呼び出しは欠けうる)。
- 提案: Swift 6.2 の `nonisolated(nonsending)` を読めるようにする。

### F4 enum に case を足したときに止まらない switch を列挙できない

- 場面: provider の追加で触る箇所を洗い出す作業。`dupscan refs ftp` (3,630 B、21 行) は `case .ftp` の行を返すが、その switch が `default:` を持つかは出ない。
- 実際: `ProviderProbeValidator` の `default:` は `refs` の出力に出ず、足し忘れが compile で止まらない箇所は Read と grep で探した。
- 種類: 未対応。
- 提案: enum の case 名から、その case を含む switch と `default:` の有無を出すサブコマンド。

### F5 report の起点にファイルを渡すと原因の分からない文言になる

- 場面: `dupscan -v -limit 1 shared/.../StorageReadUseCase.swift`
- 実際: rc=2、`は git の repo の中ではない: git rev-parse --show-toplevel: exit status 128: ... Not a directory`
- 種類: 使い方が分かりにくい。
- 提案: 起点がファイルなら「起点は dir を渡す。ファイルの中は `index -file`」と案内する。

### F6 サブエージェントは許可されていても dupscan を使わなかった

- 場面: 同じ依頼の調査と反証で動かしたサブエージェント 6 本。指示に「dupscan def/refs/callers/index は使ってよい」と書いた。
- 実際: Bash 269 回のうち dupscan は 0 回。grep の案内の hook は 3 本で 1 回ずつ出たが、その後も grep が続いた。
- 種類: 入口。
- 提案: 指示で「使ってよい」と書くだけでは使われない。Swift の定義と呼び出し元を確かめる作業の指示は、最初の手段として dupscan を名指しする形にする。

## 検証の結果

`make -C src/dupscan test lint` は rc=0。ルートの `make test` は rc=2 で赤のまま push した。

- 赤の内訳: Go の 3 パッケージ (`mac-lan-manager/setup`、`pro-con/ui`、`ratelimit/usage`) と、シェルのテスト 4 本 (`test_claude_bin`、`test_dangling_symlinks`、`test_ratelimit_warn`、`test_tmux`)。
- 変更は `src/dupscan` だけで、3 パッケージの go.mod は dupscan を参照しない。
- Go の 3 パッケージと `test_claude_bin`、`test_ratelimit_warn`、`test_tmux` は、単独で流し直すと通った (実行時の load average は 17 前後)。
- `test_dangling_symlinks` は手元の壊れたリンク 2 本で落ちたまま (`~/.claude/rules/ping-poing-immediate-reply.md`、`~/.config/nvim/coc-settings.json`)。

## 残タスク

- 未着手: F2〜F6。F2 と F3 は別のセッションが同じファイルを修正中なので、その後に見る。
- 未調査: `MoveReconcileExecutor.swift` が読めない原因。

## 関連

- 1057 の F2・F3 (`index -returns`、`refs` の引数ラベル) は別のセッションが担当中で、F2 の `index` の変更と同じファイルに触る。
