# 957 (research): dupscan を使って 1307 の続き (macOS と shared にまたがる組) を進めて見つかった問題と提案 (着手者向けの詳細)

> 🚨 **担当中: air5 (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 対応結果 (2026-10-29)

起票時の観察と、現行版での再現結果を区別する。以下の「概要」から「見送り」までは起票時の記録を保存したもの。
今回の調査は固定 dotfiles `d50cff98` からビルドした binary、Go 1.26.0、macOS SDK 27.0 で実施。
同時に更新される共有 checkout の wrapper を結果の比較対象にはしなかった。hook の検証は Homebrew Bash 5 を使用。

- Q1: 2定義の差分は既存の `explain -compare` (918) で取得できる。README に L2 の組から使う導線を追加。
- Q2: 古い除外と現在の組を結び付ける案内を採用し、表示のみの改善を実装・検証する。
- Q3: 設定編集 CLI は保留。一括 `remove --stale` は不採用。956 の ungrouped は sweep に必要な許可を含む。
- Q4/Q6: JSON の種別・範囲集計を README に追加。専用フラグ・既定の分類変更は今回採用しない。
- Q5: 複数行は原因ではない。引数付き call と前置文が原因で、現行の保守的な preset 条件を維持する。
- 954 P3/P8: 現行では解消済み。実ファイルと実 hook の A-B で確認した。
- 関連: [954](done/954-research-dupscan-dogfood-feedback-obaket-1307-2026-10-28.md)、[955](955-research-dupscan-dogfood-vlc-test-dedup-2026-10-28.md)、[956](956-research-dupscan-dogfood-self-dedup-2026-10-29.md)。955/956 全体の完了判断・担当には介入しない。

## 概要

obaket の issue 1307 (テストの類似メソッドの統合) の続きを dupscan-dogfood で進めたときの所見。`954` (前半。done に移っている) の続きで、**954 の P1〜P8 のうち直った・直っていないものの確認**も含む。着手するのは別の人なので、再現手順・数字・未確認のことを分けて書く。

- **自分の所見だけを書いた記録で、既存 issue (955・956 ほか 932 配下) との重複照合も反証レビューも通していない**。重複していたら統合してよい
- 測定の条件: obaket `master` の `7d2daefdc` 付近 (2026-10-29 00:40)、Swift、macOS。dupscan は dotfiles の `bin/dupscan` (その時点の master。他のセッションが同時に `src/dupscan` を変更していた)。dupscan の版 (commit) は取っていない
- 作業の規模: この続きの間に 12 回 commit・push し、dupscan の組は repo root から全体 210 → 148、メンバーに `macOS/ObaketTests/` を含むもの 98 → 33、macOS 内で完結するもの 49 → 0 になった (数え方は skill dupscan-dogfood 3 節)

## 使った場面と数字

| 場面 | コマンド | 所要・出力 |
|---|---|---|
| 組の全体を機械で数える・各組の除外の 1 行を取る | `dupscan -format json -limit 0 .` (repo root) | **1.34 秒**・489 KB・stderr 0。10 回以上使った |
| 実装が終わったとき | `dupscan gate -base origin/master` | **2.98 秒**・stdout **2,032 B** (rc=0 のとき)。lint の `check-dupscan-gate` でも同じ gate が走る |
| 寄せ先が既にないか | `dupscan index -returns <戻り型>` | 1.14 秒・4,828 B (stdout) + 321 B (stderr)。**954 の P1 のあとから引くようにした** |
| 定義・参照 | `dupscan def` / `refs` | 数回。refs は読まなかった (呼び出しの字面は rg で数えた) |

grep / rg に戻した場面: 呼び出しの**書き方の種類**を数えるとき、変更後に取り残した旧名の呼び出しを探すとき (字面の検索で、dupscan の守備範囲ではない)。

## 役に立った場面 (効果の主張は、この 3 点に限る)

1. **`gate` が新しい重複を commit の前に止めた (今回 2 回)**: ① 954 の `ViewModelTestContext.location(path:)` が shared の `MoveObjectsUseCaseTests.Fixture.location` と同形 (前回分) ② 待機者を値型に出したら、`HistoryCountWaiters.cancel` が別の fake の `BroadcastRecorder.cancelWaiter` と新しい組になり `check-dupscan-gate` (lint) が rc=2 で落ちた。人の目では離れた場所の同形に気づけない
2. **`-format json` の各組の `exclude` フィールド**: 寄せないと決めた組の除外の 1 行 (`group` のキー) を手で組まずに済んだ。今回 9 回使った
3. **`partition` と「消えた組」**: 寄せた結果を「消えた組 N」の数で確かめられた (`gate -v`)

## 954 の所見が直ったか (確認)

| 954 | 今回の確認 |
|---|---|
| P5 (gate の出力が大きい: base と head の読めないファイルの二重列挙) | **直っている**。`読めないファイル (base・head 同じ): …` と 1 回にまとまり、rc=0 の gate の stdout は 2,032 B (954 の時点は 4,215〜6,755 B) |
| P6-2 (`-base` を渡しているのに「CI なら -base を渡す」が出る) | **直っている**。`base が HEAD と同じ (見たのは未コミットの変更だけ)` だけになった |
| P6-1 (「新しく足された除外 14」がメンバー数) | 今回も `新しく足された除外 6 (組・単位・glob・プリセットの要素と、釘の個数の制約を 1 つずつ数えた数で、組の数ではない)` と**説明が足されて**いた。数え方の説明は付いたが、組の数そのものは出ない |
| P3 (`QuickSearchClickCoordinatorTests.swift` が読めない構文) | **直っていない**。今回新しく書いた `ManualQuickSearchWaitState.wait()` も同じ形で読めない (読めない構文のファイル 54) |
| P8 (案内の注入が雑音) | **直っていない**。dupscan を使い続けている Bash でも毎回 `dupscan def <コマンド中の語>` が付く (回数は数えていない。多数) |

## 問題と提案

### Q1. L2 の組 (リテラルだけが違う本体) で、**何が違うか**が出ない — 「データ表」と「意図した差」を見分けるのに毎回本体を読んだ

- **場面**: L2 の組 (「リテラルの値だけ違う本体」) が 15 組ほどあった。結論は次のように割れた:
  - **寄せるべきデータ表**: `*SmallWireFixture` 10 個 (違うのは `rootPath`・small の文字列・ページ数・`runID` の 4 つだけ。それ以外の 14 フィールドは構造が同一)
  - **寄せない意図した差**: `ProviderContractSFTPClientFactory.connect` と `ProviderFiniteSFTPClientFactory.connect` (`probe` が `.cancelled` と「upload lane に無い」`invalidArgument` を投げる)、2 つの `openFile` (`concrete test handle is unused` と `finite SFTP uses the upload-handle seam`)
  - **根拠の無い差**: `dropboxFileEntryJSON` の `rev` (`contract-rev` と `contract-state`。どの assert も読まない)
- **実際**: 報告は「2 つの本体がリテラル以外同じ」までで、**どのリテラルがどう違うか**を出さない。違いが「データ」か「意図」かは、違うリテラルの中身 (パスの接頭辞か、エラーの文言か) を見て初めて分かる。私は 1 組ごとに本体を開いて読み、10 個の fixture は Python でフィールドごとに突き合わせた (全部で数十分)。codex の敵対的レビューでも、除外理由の事実誤認 4 件の原因は「違いを読み違えた」ことだった
- **提案**: L2 の組を出すとき、メンバーごとの**違うリテラルだけ**を表で出す。例:
  ```
  差分のリテラル (位置は本体の何番目の文字列か):
    #1 "obaket-contract-tests/r2/wire-mvp"      | #2 "obaket-contract-tests/s3-compatible/wire-mvp"
  ```
  リテラルの差が 1〜3 個で、すべて文字列の接頭辞・数値なら「データ表の可能性」、エラー型や文言なら「意図した差の可能性」と**分類のヒントを付ける**のは判断が要る (分類は提案しない・差分を出すだけでも十分)。未確認: `dupscan near` や `-v` に同等の出力が既にあるか (`dupscan help` では見つけられなかった)
- **検証の方法**: 今回の 10 個の `*SmallWireFixture` (寄せる前は `7711fc278`) で、差分が 4 つだけと出ること

### Q2. 除外のキーに本体のハッシュ (`@b25042f4354e`) が入っていて、片方を少し直しただけで除外が古くなり、組が別のキーで再び出る — 古くなった理由と**新しいキー**が出ない

- **場面**: `TransferActivityCenterTests.swift` の `CountingHistoryRepository.waitUntilInsertCount` と `SQLiteTransferHistoryStoreDecorator.waitUntilInsertCount` を「寄せない」と除外 (キー末尾 `@b25042f4354e`) したあと、**待機者の管理だけ別の型に出す**修正をした (2 つの本体は今も同じ形)。結果、除外が「効いていない除外」になり (`gate` の 1 行目で 1 → 2 に増えた)、同じ 2 メンバーの組が**新しいハッシュで再び出た** (n=101 → 84)
- **実際**: 「効いていない除外」の一覧は古いキーを出すだけで、(a) どのメンバーの本体が変わったか (b) 同じメンバー名の組が今は別のキーで存在すること (c) 新しい除外の 1 行 を出さない。私は JSON の `groups` を撮り直して、メンバー名 (`waitUntilInsertCount`) で突き合わせた
- **提案**: 効いていない除外について、`file|型|シグネチャ` が同じメンバーを持つ組が今あれば、**「同じメンバーの組がハッシュ違いで残っている。本体が変わった。新しい除外の 1 行: {…}」**と出す。意図した除外を残すなら貼り替えるだけでよく、寄せたなら除外を消せる
- **補足**: ハッシュを含める設計の理由 (本体が変わったら除外の前提も変わる) は妥当。変えるのは出力だけ

### Q3. 除外の追加・削除が手作業 (JSON を Python で書き換えた) — 道具があれば 3 つの使い捨てスクリプトが要らなかった

- **場面**: 今回、除外の追加・削除・理由の直しを 5 回、そのたびに Python で書いた (`tmp/dupscan-dogfood/…/addexcl*.py`)。`.dupscan.json` は `json.dumps(j, ensure_ascii=False, indent=2) + "\n"` で往復して**バイト単位で同一**だったので機械編集は安全だったが、その確認も毎回要った
- **提案**: `dupscan exclude add --kind intentional --reason '…' <グループの指定>` と `dupscan exclude remove <グループの指定>` (または `--stale` で効いていない除外を全部消す)。グループの指定は「メンバー名」でも「`gate` が出す除外の 1 行」でもよい。書式は既存の `.dupscan.json` と同じ整形を保つ
- 参考: 理由の文を書く作業は人が必要。道具は**理由を空にして書けない**ようにする (`kind` と `reason` が空の除外は gate が受け付けない現状に合わせる)

### Q4. 本番とテストが混在する組 (`partition: "mixed"`) が「統合する」候補に混ざる

- **数字**: obaket `7d2daefdc` の組 152 のうち `partition` は `test` 142・`mixed` 7・`prod` 3 (撮った時点)。1307 の 48 組 (メンバーに `macOS/ObaketTests/` を含む) のうち本番のメンバーを含むのが 6 組 (`PikPakWebDAVAdapter.capabilities` など本番の adapter の `capabilities`・`HTTPStreamingDelegate.apply`・`SMBAdapter.transferWireCapabilities`・`InMemoryCredentialRepository.store` と、テストの fake)
- **実際**: これらは「本番の実装を真似たテストの fake」で、本番とテストはドメインが違う (obaket の `CLAUDE.md`「ドメインを混ぜない」)。寄せる対象ではないのに、通常の組と同じ並びで出る。1307 の issue でも、除外に回すか判断する作業を 6 組分した
- **提案**: `mixed` の組は既定で別の節 (「本番とテストが混在。寄せ先に本番の API を足さない限り寄せられない」) にまとめるか、`-partition test` で絞れるようにする。**消してしまわない** (本番側に写しがあるのは本物の情報)。未確認: `-partition` のような絞り込みが既にあるか

### Q5. `l2-single-call` プリセットが効かない形がある (単一呼び出しのデータ表)

- **場面**: `makeSmallWireFixture(rootPath:…, smallObjectBytes:…, maxExpectedPages:…)` に定数だけを渡す関数が 10 個残った (L2 の組が 3 組: smb と sftp (n=33)、s3・dropbox・googleDrive (n=28)、r2 と s3-compatible (n=33))。`.dupscan.json` の先頭には `preset: l2-single-call` (「同じ呼び出しに違う定数を渡すだけの 1 行の委譲」) があるのに、この 3 組は**報告に残った**ので、手で 3 件の除外 (`false-positive`) を足した
- **実際**: 原因は**調べていない** (推測: 呼び出しが複数行・引数のラベルが関数ごとに違う・`let base = …` の文が先にある、のどれか)
- **提案**: プリセットが当たらなかった理由を `-v` で 1 行出せるようにする (「複数行の呼び出し」「前に文がある」「引数の個数が違う」など)。または、複数行の単一呼び出しにも当てる

### Q6. 範囲を絞った組の数を数える手段が無い (JSON を自前で集計した)

- **場面**: 「メンバーが全部 `macOS/ObaketTests/` の組」「1 つでも含む組」を作業の前後で比べた (210/98/49 → 148/33/0)。`dupscan -format json -limit 0 .` を撮り、Python で `members[].file` の接頭辞を見て数えた (skill dupscan-dogfood 3 節に書いた数え方)
- **提案**: `dupscan -format json` の最後、または text の 1 行目に、`-within <接頭辞>` を渡したときの「その範囲で完結する組・含む組」の数を出す。954 の P4 (cwd で走査範囲が変わる) と合わせて、「走査の根は repo root・数える範囲は `-within`」に分けられる
- 未確認: `-within` のような範囲指定が既にあるか

## 未確認・測っていないこと (着手前に埋めてほしい)

- Q1: `dupscan near` や `-v` に、L2 の差分リテラルを出す機能が既にあるか
- Q4: `partition` で絞る既存オプション
- Q5: プリセットが 3 組に当たらなかった理由
- Q6: 範囲指定の既存オプション
- 954 の P3 (読めない構文の原因) は今回も未調査
- dupscan の版 (commit) は取っていない。他セッションが並行して `src/dupscan` を変更していたので、「直っている」と書いた P5・P6-2 が**いつの変更**かは未確認

## 着手の順 (提案)

1. Q1 (違うリテラルを出す): 判断の手間が一番大きかった。出力だけの変更で済む可能性がある
2. Q2 (古くなった除外の新しいキー): 寄せる作業のたびに起きる。Q3 (除外の追加・削除の道具) と同じ作業で扱える
3. Q4・Q5・Q6: 雑音と手作業の削減
4. 954 の P3・P8 (読めない構文・案内の雑音) は続き

## 見送り

- 特になし (どれも価値の判断は着手者に任せる)


## 裏どり・採否の根拠

### Q1: 既存の差分表示を使う

`explain -compare` は text/json で直接の literal token を表示し、参照式は未解決として残す。
obaket `7711fc278` の `ProviderContractFixtures.swift` を取り出し、`s3SmallWireFixture` と
`dropboxSmallWireFixture` を比較すると 15 differences / 15 returned / truncated=false / parse_error_files=0。
root 由来の文字列14箇所と small bytes 1箇所。既存 compare が返すのは、設定項目4種類への要約ではなく AST 上の出現ごとの差分であり、この2fixtureでは15箇所になる。
統合後 `7d2daefdc` の s3 と googleDrive は3差分 (root・small文字列・ページ数)。

既存コマンドの利用例: `dupscan explain -limit 50 -compare Fixtures.swift:dropboxSmallWireFixture Fixtures.swift:s3SmallWireFixture`。
AST path はソース位置や意味的な項目名ではない。group 全員の一括表・共通接頭辞の集約・意図の自動分類は未実装。
この点を「新しい差分表示が必要」とは扱わず、まず既存コマンドを使う。
一括比較は、既存コマンドを使っても多数のペア選択が繰り返し律速になる実例が出たとき再検討する。

### Q2: 除外の更新候補を表示する設計

Go の同形2関数を27 nodesで除外し、両方の `return x * 2` を `return x * 2 + 3` に変える再現例では、
同じ鍵の29 nodesの組ができ、旧釘 `@18fea7600f09` は `@e85619f08857` に変わる。
`report -v` には現在の除外 JSON が組の箇所に既に出るが、旧 stale rule と関連付けない。
`gate -v -base HEAD` は rc=0 / changed=2 / stale=1 で、組を既存と扱うため現在の除外 JSON 自体が出ない。
したがって「新しいキーが一切出ない」は report について過大で、gate と相関の導線が実際の gap。
956 の duplicate/ungrouped 分類はこの hash 変更例を解消しない。

採用するのは表示のみ。verbose の stale group の直後に、次を満たす場合だけ現在の除外 JSON を候補として添える。

- 同じ段、hash を除いた鍵の多重集合が完全一致し、現在の組と一意に対応する。
- closure の序数や多重定義の番号を省く pinKey が重複する場合、複数候補、顔ぶれの増減は案内しない。
- report は除外前の組、gate は新規違反に絞る前の head の組から照合する。
- 「同じ鍵の組候補。除外理由の再審査が必要」と示し、原因を本体変更と断定しない。
- 旧 kind/reason のコピー、設定書換え、候補の自動採用、終了コード・JSON schema の変更はしない。

### Q3: 書込み操作を増やす前の条件

`exclude` サブコマンドは無い (現状ではディレクトリ指定として解釈され rc=2)。
5回の手編集だけでは、read-only の道具に書込み・整形保持・並行更新の扱いを足す費用の優位は実証されていない。
Q2 の候補表示を先行させる。特に `--stale` 一括削除は、sweep のための許可まで消すので不採用。
再開条件は、候補表示後も除外編集が反復して作業の律速になることを記録できたとき。
将来は read-only の変更案と明示 apply を分け、exact group指定・必須kind/reason・差分preview・
不正設定拒否・元bytesの再照合・通常gateでの確認を設計条件にする。
956 F4 の allow-group は別の表現形式の提案であり、同時実装する必然はない。

### Q4/Q6: 全体を走査してから集計する

`-partition test` と `-within macOS/ObaketTests` はいずれも未定義flagとして rc=2。
一方、既存 JSON に `partition` と `members[].file` があるので、専用フラグがなくても集計できる。
README に repo root の全件 JSON から `all` / `within` / `contains` / `partition` を数える実行可能な手順を追加した。
走査の dir を絞ると範囲外の相手を失うので、集計対象の範囲と走査範囲を混同しない。

固定版で obaket `7d2daefdc` の archive の通常ファイルを git fixture に登録して測定
(symlink は dupscan 自体も読まないため除く): 1,682 files / 26 files with parse errors、149組。
種別は test 139 / mixed 7 / prod 3。`macOS/ObaketTests/` を含む34組、範囲内で完結する1組。
起票時の数字との変化を、共通化や効率改善の証拠には使わない (当時のdupscan版が不明)。

mixed の印は各単位の test 判定から付く構文・配置上の分類で、ドメイン境界の証明ではない。
本番 API をテストから再利用できる場合もあり、一律の「寄せられない」分類や自動除外は不採用。
JSON の手順を使っても範囲集計の反復が律速になるなら、report 専用の表示後filter/集計を再検討する。
その際も group形成・除外・gate の判定には波及させず、全体の総数と filter 後の数を区別する。

### Q5: preset が残す3組の原因

6関数の最小例では、除外なしの L2 が3組。preset を適用すると複数行の単一callの組だけ消え、
引数の `Array("hello".utf8)` を含む組と、`let base = …` を前置する組が残る。
実 fixture も同じ: smb/sftpとs3/dropbox/googleDrive/pikpakWebDAV/genericWebDAVは引数内の Array call、
r2/s3-compatible は前置 let があり、preset だけで3組 (2・5・2メンバー) が残る。

本体直下1文・引数中callは0引数だけ、という実装条件と一致する。複数行対応は既存なので追加不要。
型未解決の任意の引数付きcallを純粋な変換とは判定できないため、preset は広げず、README に対象外の具体例を加えた。
全候補の不一致理由を verbose に出す案は、今回の原因調査には不要なので見送る。
再検討するのは、既知の条件を読んでも判定理由を識別できない反例が出たとき。

### 954 P3/P8 の現行確認

- P3: obaket `7d2daefdc` の `QuickSearchClickCoordinatorTests.swift` は 1 file / 77 units、解析失敗通知なし。
  以前失敗した `FakeQuickSearchRuntimeLease.end()`・`SequencedQuickSearchWindowResolver.resolveQuickSearchWindow(...)`・
  `ManualQuickSearchWaitState.wait()` を `def` で全文取得できた。いずれも `continuation.yield(())` を含み、
  954 の空タプル引数を読み直す修正 `8ca19372` の対象。新たな parser 修正は不要。
- P8: 実hookを隔離sessionで A-B確認。通常Bash 0 B / 最初のSwift symbol rg 574 B /
  同session再度のrg 0 B / 別sessionのdupscan利用 0 B / 直後のrg 0 B。全rc=0、stderr=0。
  現行は通知間隔900秒・dupscan利用後600秒抑止。既存hook suiteも145 passed / 0 failed。
  したがって「毎Bash」「直っていない」は起票時の観察として保存し、現行の事実としては訂正する。

### 改善の評価

既存の比較機能とpreset条件を確認する手順を明示し、不要な新機能・危険なstale一括削除を避けられるようにした。
JSON集計の手順は範囲外の相手を落とさず数字を取れる。一方、一般的なtoken削減・開発時間短縮は今回測定していない。
Q2の表示改善は、除外理由の再審査を保ちながら旧除外と現在の候補を人が突き合わせる往復を減らすことを狙う。

反証レビュー: 論点別のread-only subagent 2体で照合。P3として、設定項目数とAST出現数の違いを元記述の誤りと断定していた文を訂正し、hook検証のBash条件を追記した。もう一方は数値を独立再実行して一致し、追加指摘なし。
