# 954 (research): dupscan を使って 1307 を進めて見つかった問題と提案 (着手者向けの詳細)

> 🚨 **担当中: Mac (glogx)**（2026-10-28〜）

起票日: 2026-10-28

## 概要

obaket の issue 1307 (macOS/ObaketTests の類似メソッドの統合。104 組) を dupscan-dogfood で進めたときの所見をまとめる。**着手するのは別の人なので、再現手順・数字・未確認のことを分けて書く**。
`950` (同じ作業の前半の短い記録) の続きで、950 に書いた F1 (読めないファイルの列挙) もここで詳しく扱う。

- **この issue は自分の所見だけを書いた記録で、既存 issue (932 配下の 941〜952 等) との重複照合も反証レビューも通していない**。重複が分かったら統合してよい
- 測定の条件: obaket `master` の `df0a6effd` 付近の worktree、Swift、macOS。dupscan は dotfiles の `bin/dupscan` (その時点の master。他のセッションが同時に `src/dupscan` を変更していた: `e9a07e7f` 20:59・`b94e2872` 20:35 など)。dupscan の版 (`dupscan --version` の類) は取っていない

## 作業の概要 (何を使って何をしたか)

1 組ずつ本体を読み、同じドメイン (同じ suite 群の同じ道具) の写しだけを寄せ、provider ごとに意味が違うものは `.dupscan.json` に理由つきで除外した。寄せたあとは `make lint` / `make test` と `dupscan gate` で確かめた。

使った dupscan のコマンドと結果:

| 場面 | コマンド | 結果 |
|---|---|---|
| 残りの組の全体を機械的に数える | `dupscan -format json -limit 0 .` | 750,848 B の JSON。`groups` 210 組 (その時点)。各組に除外用の 1 行 (`exclude`) が入っていて、そのまま `.dupscan.json` に足せた |
| 寄せ先の定義を引く | `dupscan def ViewModelTestContext` | 3,404 B (stdout) + 87 B (stderr) |
| 参照を引く | `dupscan refs location` | 4,246 B。**読まなかった** (呼び出しの字面の種類を数えるのは rg が早かった) |
| 実装が終わったとき | `dupscan gate -v -base origin/master` / `dupscan gate -base origin/master` | `-v` は 6,755 B (rc=1)、`-v` なしは 4,215 B (rc=0)。合計で 8 回前後 |

## 役に立った場面 (効果の主張は、この 2 点に限る)

1. **`gate` が commit の前に新しい重複を止めた**: `ViewModelTestContext.location(path:)` を新設したところ、shared の `MoveObjectsUseCaseTests.swift:523 Fixture.location(path:)` と同じ形の新しい組になり、gate が「新しい 1 (止める)」と、相手の位置と、`.dupscan.json` に足す除外の 1 行まで出した。別 target の同形で、目では気づかなかった
2. **`-format json` の `exclude` フィールド**: 寄せないと決めた組を `.dupscan.json` に足すとき、組のキー (`file|型|シグネチャ@ハッシュ`) を手で組まずに済んだ

## 問題と提案

### P1. 新しい helper を書く前に `index -returns` を引かず、本番にあった同じ関数の写しを自分で作った (使い方の問題 / 入口の問題)

- **場面**: `ViewModelTestContext` に `StorageLocation` を組む helper を足すとき、`dupscan index` を引かずに `StorageConnection` の拡張 (`location(path:)`) を新設して push した (obaket `931104de3`)。ObaketCore に `StorageConnection.location(at:)` が**すでにあった** (`shared/Sources/ObaketCore/Connection/StorageConnection.swift:1264`)。気づいたのは `.dupscan.json` の除外理由の文を読んだときで、dupscan で見つけたのではない。後続の commit で本番側に寄せ直して拡張を消した
- **引いていれば見つかったか (事後に計測)**:
  - `dupscan index -returns StorageLocation` → 2.14 秒・stdout 5,502 B・stderr 463 B。該当 173 件 (20 件表示、全部で約 34.8 KB)。`location(at:)` は**7 番目**に出る
  - `dupscan index -name location -returns StorageLocation` → 118 件 (20 件表示、約 23.2 KB)。`location(at:)` が**1 番目**
- **原因の見立て (推測)**: skill dupscan の「書く前に引く」は、新しい関数を書く場面でしか効かない。今回は「テスト helper の寄せ先を新設する」という**寄せる作業の途中**で、頭の中では「重複を直している」側だったため、引く場面だと認識しなかった。Swift の `Edit` / `Write` で `func` / `extension` を足したときの案内は見ていない (Go には `_claude/hooks/suggest-dupscan-for-go-edit.sh` がある)
- **提案**:
  - (a) Swift のファイルに新しい `func` / `extension` を足す `Edit` / `Write` でも、Go と同じように「`dupscan index -name <名前> -returns <戻り型>` を引く」案内を 1 セッションに 1 回出す。Swift 用の hook が既にあるかは確認していない
  - (b) skill dupscan (`~/.claude/skills/dupscan/SKILL.md`) の 1 節に「**寄せ先を新設するときも** 引く。戻り型で引く (`-returns`)」と、今回の `location(at:)` の例を足す
- **検証の方法**: (a) を入れたら、Swift の test helper を新設する作業を 1 本やり、案内が出て `index -returns` を引くかを見る

### P2. `index -returns` が stored / computed property を拾う (雑音)

- **場面**: P1 の `index -name location -returns StorageLocation` の結果に、関数ではなく **プロパティ**が混ざる: `MutationFinish.swift:18-23 MutationCacheInvalidationPlan.Entry.var location: StorageLocation`、`StorageOperationLock.swift:56 StorageOperationLock.var location: StorageLocation`、`BulkDownloadPlanner.swift:19 DownloadItem.var location`、`PersistentTransferIntent.swift:62 var location` など。2 番目以降の大半
- **期待**: 「戻り値で関数を探す」意図なので、プロパティは既定では出さないか、種類で絞れる
- **提案**: `-kind func,init` のような絞り込み、または `-returns` の既定を関数・メソッドだけにして、プロパティは `-kind var` で出す。出力が 118 件→関数だけ何件になるかは未計測

### P3. `QuickSearchClickCoordinatorTests.swift` が「読めない構文」で、gate が寄せた結果を確かめられなかった

- **場面**: 同じファイル内の 2 つの fake (`ManualAttachWaiter` / `ManualReadySource`) の待機の仕組みを、新しい private class `ManualQuickSearchWaitState` に出して委譲した (obaket `c5240a983`)。`dupscan gate` は「読めない構文で確かめられなかった変わった単位 4」と出し、**このファイルの組が消えたかを gate では確認できなかった**。代わりに `make test` の件数が変わっていないことで確認した
- **読めない単位の位置** (`dupscan -v .` の出力。obaket `df0a6effd`): `macOS/ObaketTests/QuickSearchClickCoordinatorTests.swift` の 1682 `FakeQuickSearchRuntimeLease.end()`、1782 `SequencedQuickSearchWindowResolver.resolveQuickSearchWindow(snapshot:openWindow:)`、1895 `ManualQuickSearchWaitState.wait() -> QuickSearchWaitResult` と、その中のクロージャ (1897・1898・1912)。ほかに `DestinationPickerViewModelTests.swift:226 SuspendedDestinationBrowsing.yieldConnectionEvent()`、`MenuBarStatusItemModelTests.swift:19 StreamObserver.events() -> AsyncStream<[TransferRecord]>` など。`dupscan -v .` の出力で「読めない:」の行は 236 行
- **1895 の `wait()` の形** (自分が書いた): `return await withTaskCancellationHandler(operation: { await withCheckedContinuation { continuation in ... } }, onCancel: { [weak self] in Task { @MainActor [weak self] in self?.cancel(waiterID) } })`。複数の trailing closure と、`guard !Task.isCancelled else { continuation.resume(returning: .cancelled); return }` を含む。**どの構文が原因かは調べていない** (推測: クロージャが複数ある `withTaskCancellationHandler` と `[weak self]` の捕捉、または `Task { @MainActor [weak self] in` の属性つきクロージャ)
- **件数**: `dupscan` の「読めない構文があるファイル」は obaket で 57 (その時点)。epic 932 の 946・948 の時点の件数との比較はしていない
- **提案**: 上の 3 単位を最小の再現にして、パースが落ちる構文を特定する (`dupscan -v` の「読めない:」の理由が出ていれば、それを読むだけでよい。理由が出ない場合は P3 に追記する)。直せるものは直す

### P4. 「読めない構文のファイル」の件数が 17 と 57 でぶれたのは、**実行した cwd が違っただけ** (訂正済み。最初は版の入れ替えと推測していた)

- **観測**: 同じ obaket の worktree で `dupscan gate -base origin/master` を打つと、1 行目の「読めない構文のファイル base N / head N」が 57 の回と 17 の回があった
- **原因 (計測して確定)**: `dupscan` は **cwd の配下だけ**を走査する。同じ worktree・同じ HEAD で cwd だけを変えて測った:

  | cwd | 読めない構文のファイル | 走査したファイル数 (`dupscan .` の 1 行目) |
  |---|---|---|
  | repo root | 57 | 1675 (26,084 単位) |
  | `macOS/` | 17 | 463 (6,628 単位) |
  | `shared/` | 37 | (未取得) |

  57 = 17 + 37 + 3 (残りの 3 は `probes/` など。内訳は未確認)
- **実害 (これが本題)**: `macOS/` から `gate` を打つと、**`shared/` 側にある同じ形の相手が見えない**。今回、root から打った gate は「`ViewModelTestContext.location(path:)` が shared の `MoveObjectsUseCaseTests.Fixture.location(path:)` と新しい重複」と止めたが、`macOS/` から打っていたら止まらなかったはず (未検証。同じ状態を `macOS/` から打って確かめるのは簡単)。1 行目の件数が 17 と小さいことだけが手がかりで、「走査の範囲が縮んでいる」とは出ない
- **提案**:
  - (a) 1 行目に走査の根と範囲 (`root=<path>・ファイル 463`) を出す。cwd が repo root でないときは、「repo の一部だけを見ている (root から打つと shared も見える)」と 1 行足す
  - (b) `gate` は cwd にかかわらず repo root 全体を見る (または `-root` の既定を repo root にする)。`dupscan` の設計上の判断 (サブディレクトリだけを見たい場面があるか) が要る
  - (c) skill dupscan の 3 節 (gate) に「**repo root から打つ**。サブディレクトリから打つと別 target の相手が見えない」と書く。すぐ入れられる
- **検証の方法**: 同じ変更で root と `macOS/` から gate を打ち、`新しい重複` の出方を比べる

### P5. `gate` の出力が大きく、読んでほしい数行が埋もれる

- **数字**: `gate -v` 6,755 B (rc=1)、`-v` なし 4,215 B (rc=0)。大半は「読めないファイル (base): …」と「読めないファイル (head): …」の列挙 (各 20 行 + 「他 37 件」。base と head で同じ 57 個)。今回の関心は「新しい重複」と「消えた組」の数行だけ
- **提案**: base と head で同一なら 1 回にまとめる。または件数だけにして、一覧は `-v` に出す。先頭 1 行 (集計) の中の「変わった単位に関係するものだけ」を出す案もある。どれを採るかは判断が要る
- 参考: `dupscan -v .` (報告全体) は 57,206 B

### P6. `gate` の表示がまぎらわしい (2 点)

- **P6-1 「新しく足された除外 14」**: 実際に足した除外は 4 件 (組の数)。14 は組に入っているメンバーの数の合計 (2+8+2+2)。`-v` の一覧は 14 行で、1 行ずつ `group:L2:…` と出る。「除外 4 件 (メンバー 14)」のように組の数も出す
- **P6-2 「base が HEAD と同じ (見たのは未コミットの変更だけ。CI なら -base を渡す)」**: `-base origin/master` を**渡している**のに毎回この文が出る (ローカルの HEAD が origin/master と同じだったため)。渡した場合は「CI なら -base を渡す」の部分を出さない

### P7. 古くなった除外 (「効いていない除外 1」) がすべての出力に毎回出る

- **場面**: `dupscan gate` も `dupscan -v .` も、1 行目に「効いていない除外 1」が出る。obaket の `.dupscan.json` の次の組:
  `macOS/ObaketTests/DebugScreenDriverTests.swift|DelayedDebugScreenCredentialRepository|fetch(_ id: CredentialID) -> Credential?@03d16ec2a18a` と `macOS/ObaketTests/StorageBrowserViewModelSidebarSelectTests.swift|DelayedSidebarCredentialRepository|fetch(_ id: CredentialID) -> Credential?@03d16ec2a18a` (L1・`kind: intentional`)
- **確認できたこと**: 2 つの型はどちらも今も存在する (`DebugScreenDriverTests.swift:4760`・`StorageBrowserViewModelSidebarSelectTests.swift:282`)。つまり**削除されたのが原因ではない**。ハッシュ `@03d16ec2a18a` が今の本体と合わなくなった (本体が変わって組にならなくなった) 可能性が高いが、**確かめていない**
- **提案**: 効いていない除外について、「メンバーは在るが組にならなくなった (どのメンバーのハッシュが変わったか)」と「メンバーが無い」を区別して出す。後者は除外の削除、前者は除外を外してよい (組が消えているので)。obaket 側では、この除外 1 件を `.dupscan.json` から外すだけで足りるかもしれない (別作業)

### P8. 案内の注入が dupscan 中心の作業では雑音になる

- **場面**: Bash を打つたびに「dupscan で Swift の定義・参照を引ける: `dupscan def …`」という PreToolUse の案内が付く。今回のように dupscan を使い続けている作業でも、直前に dupscan を打ったコマンドの後でも出た (回数は数えていない。多数)
- **提案**: そのコマンドが `dupscan` を含むときは出さない・直近 N 回以内に dupscan を使っていたら出さない。出している hook は未特定 (dotfiles の `_claude/hooks/` から探す)

## 未確認・測っていないこと (着手前に埋めてほしい)

- `index -returns` からプロパティを除いたときの件数 (P2)
- 読めない構文の原因となる具体的な構文 (P3)
- `macOS/` から打った gate が、shared 側の相手を実際に見逃すか (P4。root と `macOS/` で同じ変更を比べる)
- 効いていない除外 1 の本当の原因 (P7)
- `dupscan refs` を読まなかった理由は「呼び出しの字面を数えたかったから」で、refs の出力の質は評価していない
- hook の案内の出現回数 (P8)

## 着手の順 (提案)

1. P4 (cwd で走査範囲が縮む) と P3 (読めない構文): 重複を見逃す・寄せた結果を gate で確かめられない、は信頼に直結するので先
2. P1 の (a) / (b): 重複を自分で作ってしまう入口の問題。skill の文面 (b) は小さいので先に入れてよい
3. P5 / P6: 出力の整理 (判断が要る)
4. P2 / P7 / P8: 雑音の削減

## 見送り

- 特になし (どれも直す価値の判断は着手者に任せる)

## 対応の結果 (c4、2026-10-28)

dupscan の修正と skill の追記を入れた。敵対的な codex レビューを 2 周通し、採否を 1 件ずつ裏取りした (採用 5・却下 2・記録 1)。

| 項目 | 対応 | 状態 |
|---|---|---|
| P1 (b) | skill dupscan の 1 節に「寄せ先を新設するときも `-returns` で引く」を追記 | 済み (skill-eval PASS) |
| P1 (a) | Swift の `Edit` / `Write` の案内: 新しい関数を足す直前に名前の近い既存の定義を注入する hook (issue 948 の A1) が、字下げのある Swift の `func` も対象にしている | 既存で足りる (Swift の fixture での本物のセッションの観測は未実施) |
| P2 | `index -kind func,init,property` を追加 (既定は従来どおり全部。知らない種類はエラー) | 済み |
| P3 | **答えが出た**。関数の中の `nonisolated(unsafe) let` を tree-sitter の文法が呼び出し式として読み、続く `let` で ERROR になる (木で確認)。読めないのはその関数だけで、同じファイルの他の関数は索引にも組にも出る。955 の「ファイル全体が外れる」は実測と違った。直したのは `-v`: 重複が 1 組も無いとき、ファイル名の一覧しか出さず、読めない関数の行 (`読めない: file:line label`) が出なかった | `-v` は済み。文法側 (局所の `nonisolated(unsafe)` を読めるようにする) は文法の版を上げる別の判断 (未着手) |
| P4 | 件数のぶれは cwd の違いだった (訂正済み)。(a) `gate` の 1 行目に「repo の一部だけを見ている (起点 …)」を出す。(c) skill dupscan の 3 節に「root から打つ」を追記。`mac/` から打つと shared の相手が見えず止まらないことを再現するテストを追加 | (a)(c) 済み。(b) `gate` が cwd にかかわらず repo 全体を見る案は、設計の判断が要るので未着手 |
| P5 | 読めないファイルの一覧が base と head で同じなら 1 回にまとめる | 済み |
| P6 | (6-1)「新しく足された除外 14」は組の数でなく要素の数 (組・単位・glob・プリセットの要素と、釘の個数の制約) と明記。(6-2) `-base` を渡したときは「CI なら -base を渡す」と出さない | 済み |
| P7 | 効いていない除外の理由を分ける: 釘の指す単位のどれかが無い (削除・改名) / 同じ鍵の単位は全部在る (効かない原因は特定しない)。「効いていない除外: 」の行は JSON のまま変えず、理由は次の行 | 済み。**「外してよい」とは言わない**: codex が、除外を二重に書くと、本体も顔ぶれも変わっていないのに stale になる反例を示した |
| P8 | dupscan を含む Bash を打った後 600 秒 (`DUPSCAN_ACTIVE_WINDOW`) は、grep の案内を出さない。なお「Bash を打つたびに出る」は実体と違った: 案内は識別子の grep / rg のときだけで、15 分に 1 回 | 済み |

### codex のレビューの採否

- 採用: `pinKey` が最初の `@` で切る (署名の `@escaping`・パスの `@` で鍵が壊れる)、クロージャと外側の関数を鍵では区別できない、「組が消えている」の断定が偽になる反例 (写しが加わっただけ・除外の二重登録)、テストの穴 3 点 (`-kind init`・gate の 1 行目・hook の rc/stderr)、件数の説明が実体より狭い。
- 却下: session ID の変換で別 session が衝突 (実際の ID は UUID で、既存の案内の印も同じ変換)、時計の巻き戻りで抑止が長引く (epoch 秒で、結果は案内を 1 回逃すだけ。既存の印も同じ作り)。
- 記録: `-format xcode` は読めないファイルの一覧の統合と範囲の警告を持たない (text だけ。契約を text 以外に広げるときに足す)。

### 残りの対応 (c5、2026-10-28)

- **P4 (b) 済み**: `gate` は dir を書かなければ、cwd にかかわらず repo 全体を見る (`gate_cmd.go`)。dir を明示したときは従来どおりその配下だけで、1 行目に「repo の一部だけを見ている」と出る。Makefile (`bin/dupscan gate … src`) と CI は dir を明示しているので挙動は変わらない。`TestGateWithoutDirScansWholeRepoFromSubdirectory` で固定 (`mac/` を cwd にして shared の相手で止まる / `.` を明示すると絞る)。ブロックを外す変異で赤。skill dupscan の 3 節と README の `gate` の行を揃えた。
- **文法の版上げは効かない (実測)**: 現在の `82bb3a5` (0.7.4 の生成物) の後に upstream の main で 10 月に入った修正 (switch の case の後続の文の属性・修飾子、`open` の識別子など) を取り込んだ main (`9e3b87f`) から parser を生成し (`tree-sitter generate`)、`func f() { nonisolated(unsafe) let x = 1 }` を直接パースした。**同じ ERROR** (`nonisolated(unsafe)` が呼び出し式、続く `let` が別の文)。upstream に修正は無い。また main は生成物 (`parser.c`) を含まず、含むのは `with-generated-files` ブランチ (今使っている版) だけなので、main に上げるには生成物を自前で持つ必要があり、効果が無い今は上げない。
  - upstream への報告はしない (ユーザー決定 2026-10-28)。代わりに dupscan 側で回避した (次項)。
- **`nonisolated(unsafe) let` の回避 (済み)**: `swift.Parser.Parse` は、最初の木に ERROR があり、かつソースに `nonisolated(unsafe)` を含むときだけ、**ERROR 節の範囲の中にある出現だけ**を同じ長さの空白にして読み直し、ERROR の節が減れば採る (`reparseWithoutLocalIsolation`)。位置は変わらず、walker には元の src を渡すので節の字面も元のまま。落ちるのは修飾子の語だけ。文法が直った版では最初の木に ERROR が無く、ここへ来ない (撤去してよい目印は関数のコメント)。`isolation_test.go` に 6 本 (関数が読める・位置が同じ・修飾子の有無で L1 が同じ・別の構文エラーは隠さない・メンバーの修飾子は触らない・ERROR の外の修飾子と文字列リテラルの `\nonisolated(unsafe)` を壊さない)。読み直しの無効化で 2 本、範囲をファイル全体にする変異で別の 2 本が赤。
  - codex の敵対レビュー (1 周): 最初の版はファイル全体を置換しており、P2 を 2 件採用した (①ERROR の外の正常な修飾子まで消えて未変更の単位のハッシュが揺れ、gate が新規の重複と判定しうる ②文字列の `\nonisolated(unsafe)` を壊しても、他の関数の ERROR が減れば総数が減って採用される)。どちらも発火条件が具体的で、範囲を ERROR 節に限って直した。位置のずれ (UTF-8・CRLF)・コメント / 文字列の一致・メモリ解放は壊せなかった。実コマンドでも `nonisolated(unsafe) let` を含む関数が「読めない」から外れ、含まない写しと同じ形で比べられる。P3 の `-v` のテストは、読めない例を `nonisolated(unsafe)` から別の壊れた構文 (`let callback = ) block`) に替えた。
- **obaket の実ファイル**: この Mac に obaket が無く、実ファイルでは確認できない。issue の `ManualQuickSearchWaitState.wait()` の形 (`withTaskCancellationHandler` の 2 つの closure・`[weak self]`・`Task { @MainActor [weak self] in … }`・`guard !Task.isCancelled`) を再現して現行の文法で読むと**読めた** (`dupscan -v` に「読めない」の行は出ない)。したがって 1895 の原因はこの形ではない。実ファイルの同じ関数のほかの部分 (`nonisolated(unsafe)` など) が候補だが、確かめていない。obaket のある Mac で `dupscan -v` の「読めない:」の行を読めば足りる。

### 未着手・未確認

- 文法の版を上げて `nonisolated(unsafe) let` を局所で読めるようにする (別の判断)。
- `gate` が cwd にかかわらず repo 全体を見る案 (P4 の (b))。
- obaket の実ファイル `QuickSearchClickCoordinatorTests.swift` が読めない原因 (obaket がこの Mac に無い。issue の最小構文は読めた。`withTaskCancellationHandler` の形ではなく、955 が示した `nonisolated(unsafe)` の可能性があるが未確認)。
