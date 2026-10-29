# 959 (research): dupscan を使って 1307 を最後まで進めて見つかった問題と提案 (着手者向けの詳細)

起票日: 2026-10-29

957 の続き。obaket の issue 1307 (macOS の `ObaketTests` の重複を寄せる) の最後の組を寄せ、`macOS/ObaketTests/` を含む dupscan の組を **0** にした区間 (obaket `23585f68c`) で、困った点と役に立った点を記録する。
所見の記録と、提案 (未レビュー) を分けて書く。

- 既存 issue との重複照合はしていない (skill dupscan-dogfood 3 節)。957 の Q2 (古い除外の案内) と重なる所見は 4 に書いた。
- 提案を含むので反証レビューを通す必要があるが、**まだ通していない**。着手前に通すこと (反証されていない仕様として扱う)。
- 使った dupscan は、セッション中の dotfiles の `bin/dupscan`。どの commit の版かは控えていない (**未確認**)。958 の修正前後の版が混ざっている可能性がある。

## 数字 (obaket の repo root から `dupscan -format json -limit 0 .` の `groups`)

| 範囲 | 作業前 (2026-10-28) | 今 |
|---|---|---|
| 全体 | 210 | 106 |
| メンバーに `macOS/ObaketTests/` を含む | 98 | 0 |
| `stale_exclusions` | — | 0 |

`dupscan gate` を回した回数と、`make lint` の `check-dupscan-gate` で落ちた回数は数えていない (未計測。この区間の最後の lint の失敗は 1 回確認している)。

## 所見

### 1. 寄せ先に置いた新しい正本そのものが「新しい重複」と数えられる (摩擦。提案つき)

- 場面: 「`insert` / `update` / `terminal` のどれでも `records` に積んで受理を返す」だけの spy 2 型 (`FinishSpyHistory`・`FinalLocationSpyHistory`) を、共通の `AppendingTransferWriteSink` (`shared/Tests/SharedTestSupport/Stubs/`) に置き換えた。
- 実際: 置き換えて 2 型を消したのに、`dupscan gate -base origin/master` は「新しい重複 1 (止める 1)」で rc=1。新しいメンバーとして出たのは、**寄せ先に足した `AppendingTransferWriteSink.insert` / `update` / `terminal` の 3 単位**で、組には寄せられなかった 3 型 (`Issue732TransferHistory` の `insert`・`update`、`CopyTransferHistory.insert`) と `ResumableMoveRecorderSink.update` が入っていた。
- その後: 3 型は `terminal` ・`update` の中身が違う (ledger に印を付ける・id で上書きする・operations も積む) ので寄せられない。事実を確かめた理由で除外 1 件 (`kind: intentional`) を `.dupscan.json` に足して通した。
- 期待: 寄せた変更で組のメンバーが減った (寄せ元 2 型が消えた) ことは、gate の出力にも出ている (「消えた組 34・縮んだ組 12」)。寄せ先の追加が「縮んだ組」の一部なら、止めるのではなく報告だけにできるか。
- **提案 (未レビュー)**: 新しい単位が、その変更で縮んだ既存の組に入るなら、「寄せ先の追加」として報告に回す。判定に使える情報は gate が既に持っている (縮んだ組の一覧)。ただし、縮んだ組に紛れて本当の新しい写しを通す穴になるので、「縮んだ組のメンバー数が、足した単位の数より多く減っている」ことを条件にする案を、着手者が検討すること。

### 2. 本体を共通 helper の呼び出しに替えた単位が「新しい」と数えられる (観察)

- 場面: `ResumableMoveRecorderSink.update` は、これまで private の `acceptedResult(for:)` を呼んでいた。共通の `TransferWriteResult.accepted(for:)` を呼ぶ形に替えた。
- 実際: 替えた単位だけが「新しい」で、同じ形の既存の 7 単位 (`FinishSpyHistory` など) の仲間として出た。本体が変わったので単位の同一性が変わるのは仕様どおり (除外キーが本体の hash を含むのと同じ)。
- 困った点: 出力は「新しい: …update」としか言わない。自分が private helper を共有に替えたために形が揃ったことは、読み手が `git diff` で突き合わせないと分からなかった。
- 提案 (未レビュー): 「新しい」の単位が、base に同名の単位があって本体だけが変わったものなら、「本体が変わった (base と同名)」と一言添える。着手者は、同名で本体だけ変わる場合の判定を、`gate` の base 側の単位表で引けるか確認すること。

### 3. rc=0 でも先頭行が「新しい重複 0」ではなく「解析が不完全」になる (摩擦。**原因は未確認**)

- 場面: 除外を足した後の再実行 (`dupscan gate -base origin/master`)。rc=0。
- 実際: 先頭の 1 行は `dupscan gate: 解析が不完全 (base 4815fbdb8b25・変わった単位 216)・base が HEAD と同じ …・読めない構文のファイル base 26 / head 26 …`。「新しい重複 0」と書いた行が無い。読めないファイルは base・head 同じで 26 件 (`macOS/ObaketTests/ConnectionRepositoryEventsTests.swift` など)。
- 期待: 通ったときに「新しい重複 0」が先頭に出る。
- 困った点: 「解析が不完全」は失敗に読める。rc を見るまで通ったか分からず、`| head` で先頭だけ読むと誤読する。
- 推測 (未確認): 読めない構文のファイルが 1 件でもあると、先頭の語が「解析が不完全」に変わる。958 (Swift の読み違え) の修正で解消するかは、現行版で再現して確かめること。
  - 958 は done (2026-10-29)。obaket で読めないのは `MoveReconcileExecutor.swift` (`catch A, B {`) の 1 ファイルだけになったので、再現はこのファイルの有無で切り分けられる (未再現)

### 4. 除外を足した後に同じ単位を触ると、除外が古くなる (957 の Q2 と同じ。再発の記録)

- 場面: 理由つきの除外を足した単位の本体を、後の変更で直した (除外のキーは `file|型|シグネチャ@本体の hash`)。
- 実際: 古くなった除外が `stale_exclusions` に出た。`.dupscan.json` と obaket の `macOS/bin/test-helper-copy-exceptions.json` から古い項目を手で消した (後者は別の gate の除外)。
- 957 の Q2 で入った案内が、この版に入っていたかは未確認。ここでは `stale_exclusions` の一覧が**役に立った**ことだけを記録する。

### 5. 寄せ先を dupscan は教えてくれない (範囲外。skill dupscan への追記案)

- 場面: 共通 helper を `shared/Tests/SharedTestSupport/` (全 test target が compile する) に置こうとした。
- 実際: **2 回**、ビルドが落ちた。`CreditedTransferSleepGate` は `ObaketInfrastructure` の型 (`TransferSleeper`) に、`InMemoryUploadBookmarkStore` は `ObaketPlatformContracts` の型に依存するのに、その module に依存しない test target (`ObaketCoreTests` など) も SharedTestSupport を compile するため。
- 原因は dupscan の外 (どの target が何を compile するかは SwiftPM の `Package.swift` と XcodeGen の `project.yml` の知識)。dupscan は型を解決しないので、検出は期待しない。
- 提案 (未レビュー): skill dupscan の 4 節 (寄せるとき) に 1 項。「共通の置き場所へ移す前に、その型が参照する module を、置き場所を compile する全 target が依存しているかを `Package.swift` で確かめる。満たさなければ、その module に依存する target の中に置く」。これは skill の変更なので、**ユーザーの承認が要る** (dupscan-dogfood の 7 節)。 → 承認を得て 960 で対応済み。

### 6. 機械変換の入力を作るのに JSON を python で読んだ (観察。**代替手段の有無は未確認**)

- 場面: 48 個の fake の `capabilities` を共通 stub に替える前に、対象の一覧と各 fake の位置 (行) が要った。
- 実際: `dupscan -format json` の `members` を python で読んで、対象ファイルと型を取り出した。変換自体は別のスクリプト。
- 困った点: 「このシグネチャの単位の本体の範囲 (開始・終了行)」を平文で出す口は、`index` の `file:開始-終了` 以外に見つけられなかった。`index` は名前で引くので、同名が多い `capabilities` では出力が大きかった (件数と byte 数は**測っていない**)。
- 提案はしない。着手者は、`sweep` / `near` / `context` に目的の出力があるかを先に調べること。

## 役に立った場面

- `dupscan gate` が、commit の前に自分が足した薄い写しを止めた: `connectionBucketLocation(path: String)` と `(path: ObjectPath)` の対 (片方が `ObjectPath` を受けるだけ)。`ObjectPath` が `ExpressibleByStringLiteral` で、呼び出し 2 件を `path.rawValue` にすれば足りると分かり、overload を消した。
- gate の「除外の 1 行」(JSON) をそのまま `.dupscan.json` の `exclude` に追記し、`kind` と `reason` を足すだけで済んだ (組のメンバーの key を手で組まなかった)。
- `index -returns` で、足そうとしていた `StorageConnection.location(path:)` が本番の `location(at:)` と同じだと気づいた (1307 の前の区間。skill 1.4.2 に反映済み)。

## 見送り

- 6 は提案を出さない (調べ足りない)。
- 2 の「本体が変わった」の添え書きは、情報の追加だけで、判定は変えない案。優先度は低い。

## 受け入れ条件

- [ ] 1・2・3 を現行版の dupscan で再現し、再現したものだけ直す (再現しなければ、その結果を書いて閉じる)
- [ ] 5 の skill 追記について、ユーザーの承認を取る
- [ ] 提案 (1・2・5) の反証レビューを通す
