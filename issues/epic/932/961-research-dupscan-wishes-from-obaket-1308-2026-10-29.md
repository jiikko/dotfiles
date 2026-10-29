# 961 (research): dupscan を obaket 1308 (Infrastructure テストの 60 組) で使って、あると便利な機能

> 🚨 **担当中: air5 (glogx)**（2026-10-29〜）

起票日: 2026-10-29

> 反証レビュー: **未実施** (観察と、観察から出した「案」の記録)。案に着手する人は、着手前に反証レビューを通すこと (issue 規約)。
> 数字は 1308 の作業 (obaket `32bca1a74`〜`bc93d8dff`) 中に実際に見たもの。測っていないものは「未計測」と書く。

## 使った場面

obaket 1308 で `dupscan -format json -limit 0 .` を約 6 回、`dupscan -v -limit 0 .` を 3 回、`make lint` の dupscan gate を 1 回回した。
60 組を寄せる作業の「前後の組の差」と「除外の整理」に使った。結果は obaket の 1308 (done) に書いてある。
`index` / `callers` / `near` は今回ほぼ使わず、書く前の検索は PreToolUse hook の注入に頼った。

## 役に立った点 (そのまま残したい)

- gate の「消えた組 55・縮んだ組 5」で成果が数字で言えた。
- `-v` の「効いていない除外」で、私の変更で効かなくなった除外 8 件を見つけられた (放置すると除外が嘘になる)。
- PreToolUse hook が新しい関数名に近い既存の定義を出した。`register` で既存の `RecordUpdateWaiters.register` が出て、設計の参考になった。

## あると便利な機能 (要望)

### W1. JSON の組に「除外の 1 行」をそのまま入れる (または鍵の hash を入れる)

- 場面: 寄せないと決めた 11 組を `.dupscan.json` に足すとき。
- 困りごと: 除外の鍵には本体の hash (`…@9eea113e6240`) が要るが、`-format json` の `members[].key` には hash が無く (例: `shared/Tests/…/RefusedRedirectProviderTests.swift|ProviderRedirectStubProtocol|startLoading()`)、
  `-v` のテキスト行「除外の 1 行: {…}」にしか出ない。テキストを正規表現で切って JSON を取り出した。
  最初の切り方 (ブロックの見出しで split) は、見出しの無いブロックと混ざって別の組のメンバーを拾い、組の対応を誤った (気づいて、行ごとに JSON として読む形に直した)。
- 案: JSON の各 group に `exclude_entry` (`{"group":[…],"level":…,"kind":"","reason":""}`) を入れる。これで機械的に「この組を除外する」を書ける。

### W2. 「効いていない除外」の原因を言う

- 場面: 8 件の「効いていない除外」。理由の文は 2 通り: 「同じ鍵 (ファイル・型・署名) の単位は今も在る。効かない原因は特定していない」が 3 件、「指す単位が無い」が 5 件。
- 困りごと: 前者は、本体が変わって hash の部分だけ合わなくなったのか、組の顔ぶれが変わったのかを、自分で組を引き直して比べるまで分からない。
- 案: 前者について、記録された hash と今の hash を並べて出す。顔ぶれが変わったなら、増えた・減ったメンバーを出す。後者は、似た名前の単位 (改名の候補) を出す。

### W3. 寄せた結果に残る「薄い委譲」を、新しい組として出す前に一言添える (または既定で別扱いにする)

- 場面: 待機者の仕組みを共通部品に寄せると、各 fake に `func waitUntilX(_ n: Int) async { await waiters.wait(until: { self.x >= n }) }` のような 1 行の委譲が残り、これが 3〜4 個で新しい組になった。
  1308 で追加した除外 11 組のうち、この形が 4 組 (waitUntilUpdateCallCount・waitUntilRequestedDurationCount・waitUntilUpdated・waitUntilCallFinished)。他に finishCall 型の 2 行が 1 組。
- 困りごと: 寄せた直後に新しい組が出ると、寄せたことが退行に見える。実際は「共通の道具を呼ぶ 1 文」で、これ以上寄せる先が無い。除外の理由を 4 回ほぼ同じ文で書いた。
- 案: L1 の組で、全メンバーの本体が「同じ関数を 1 回呼ぶだけ」のものに印 (`thin-delegation`) を付け、`-v` で別の節に出す。既存のプリセット `l2-single-call` (リテラルだけが違う 1 行の委譲) の L1 版。
  **失うもの**: 本当は寄せられる 1 行の写しを見逃すかもしれないので、既定では隠さず、印だけにする。

### W4. 新しく足す「型名」が既存と衝突することを、書く前に知らせる

- 場面: 新しい gate の型を `ArrivalGate` と名付けて書いた。同じモジュールに無関係の `final class ArrivalGate` (`WebDAVAuthNegotiationTests.swift`) が既にあり、build で初めて衝突に気づいた (hook は出さなかった)。
- 困りごと: PreToolUse hook は「近い名前の既存の関数」を出すが、型名の完全一致は出さなかった (今回は出なかった。仕様として出す対象に型が入っているかは未確認)。
- 案: 書く内容に `class|struct|actor|enum|protocol <名前>` があり、同じ名前の型が repo に在れば (private を含め) 1 行で知らせる。`index -name` の完全一致の型版。
  **失うもの**: 無い (注入の 1 行が増えるだけ)。

### W5. 新しい名前の近さの判定を、語の意味で絞る

- 場面: `resumeReady` を書いたとき、近い既存として `HTTPStreamingPullDelivery.resume()` `RelaunchResumeCoordinator.resume(_:)` `DebugResumableMoveScenario.resume()` が出た。`resumeAll` では `TransferEngine.resumeAll()` が出た。
- 困りごと: `resume` という語が同じだけで、役割は無関係。hook の注入は毎回の注意書きの分だけ長く、今回は役に立たない候補が 3〜5 件混ざった (件数は目視。未計測)。
- 案: 署名 (戻り値・引数の型・async/throws) が近いものを先に出す。名前だけが近いものは下げる。

### W6. 共有の置き場を、各 test target が実際に取り込んでいるかを組ごとに出す

- 場面: SharedTestSupport に置くとき、Platform / SFTP の test target は `Helpers/SharedTestSupport` の下の `Asserts` と `Async` だけを symlink で取り込み、Infrastructure / Core / Presentation は全体を取り込む。
  `seededBytes` の置き場を決めるのに、symlink を手で `ls -l` して確かめた (今回 2 回)。
- 困りごと: これは 959 / 960 (skill 側の追記) の続き。skill に書いた手順は人が守る形で、dupscan は組のメンバーが属する target と、置きたい場所がその target から届くかを知らない。
- 案: `dupscan near` / `sweep` の組に、メンバーごとの target 名と、`.dupscan.json` の `shared` の各ディレクトリがその target から届くか (symlink を辿る) を出す。**未確認**: SwiftPM の target と symlink の対応を dupscan がどこまで読めるか。

### W7. 寄せる前後の組を `hash` で突き合わせた差を、dupscan 自身が出す

- 場面: 毎回、`-format json` の 2 つのファイルを自作の python で `hash` 突き合わせして GONE / SHRUNK / NEW を出した。NEW が薄い委譲 (W3) の発見に役立った。
- 困りごと: gate は「消えた組 N・縮んだ組 M」を数えるが、**新しく生じた組** の一覧は出さない (出ても除外済みなら見えない)。作業の途中で見たいのは「いま自分が生んだ組」。
- 案: `dupscan diff <base>` (または gate の `-v`) で、消えた組・縮んだ組・**新しく生じた組** を一覧する。作業中の自己点検に使う。

## 優先度の見立て (未検証の感想)

W1 (小さく、機械処理の足場) → W7 (作業中の自己点検) → W3 (除外の文の重複を減らす) → W4 (型名の衝突。実害は build で分かるので急がない) → W2 → W6 → W5。

## 関連

- 959 / 960: 1307 のときの所見と、skill の追記。
- 958: Swift の読めない構文 (今回も `MoveReconcileExecutor.swift` 1 ファイルが「読めない」のまま。base と同じで、今回の変更とは無関係)。
  - 958 は done (2026-10-29)。このファイルの `catch A, B {` は直さないと決めた既知の限界
- obaket `issues/epic/dupscan-consolidation/done/1308-…`: 今回の作業の記録。
