# 957 (research): dupscan を使って 1307 の続き (macOS と shared にまたがる組) を進めて見つかった問題と提案 (着手者向けの詳細)

起票日: 2026-10-29

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
