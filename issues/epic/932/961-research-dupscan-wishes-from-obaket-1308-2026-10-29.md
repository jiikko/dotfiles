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

## 進捗

- 2026-10-29: 着手。要望を今のコードで 1 件ずつ確かめてから直した (下の表)

| 要望 | 結果 | 中身 |
|---|---|---|
| W1 JSON の除外の 1 行 | **既にあった** (案内だけ足した) | report の各組と gate の各違反の `exclude` に、hash 付きの釘の除外の 1 行が 2026-10-25 (`feat(dupscan): 除外の設定 (.dupscan.json) を report と gate に効かせる`) から入っている。obaket で `groups[0].exclude` に `…@8d4da9e97936` を確かめた。skill の 3 節が「違反の `-v` の除外の 1 行」しか書いていなかったので、`-format json` の `exclude` を使う (text を切り出さない) と書いた |
| W2 効いていない除外の原因 | **実装** | `-v` の「効いていない除外」の下に「詳しく:」の行を釘ごとに足す (`exclude.Set.StaleDetail`)。本体が記録と違う釘は `記録 @… → 今 @…` (釘はクロージャの番号を持たないので、本体が変わったのか、消えたクロージャの鍵に外側の関数が当たったのかは言い切らない)、指す単位が無い釘は同じ本体 (L1) の単位を改名・移動の候補に、組の除外で釘が全部今のまま在るなら組の段で釘の外に増えた単位を出す。理由が "hash" と "missing" のときだけ。json の形は変えない |
| W3 薄い委譲の組 | **実装 (印だけ)** | adapter に `tree.Unit.SingleCall` を足し、組の全員が当たれば text に「印: 全員の本体が 1 つの呼び出しを 1 回するだけ」、json に `single_call: true` を付ける (gate の違反も)。隠さず、gate の判定も変えない。条件は 1 文の呼び出しで、呼ぶ先が原子・引数が後置のクロージャを含めて 2 つまで・どれも原子か、文が 1 つで制御文でないクロージャ (中のクロージャも同じ条件)。添字 (`values[key]`) は呼び出しに数えない。Go のクロージャにも付ける。引数を問わない版は obaket の除外なしの 345 組中 155 組に付き (大半は引数を 20 個並べた初期化子)、2 つまでに絞って 37 組 (反例探しの 2 周目の後は 36 組)。1308 で除外した waitUntil の 4 組は全部入っている |
| W4 型名の衝突 | **実装** | hook `inject-dupscan-candidates-on-new-func.sh` が、書く内容の型の宣言 (Swift の class / struct / actor / enum / protocol、Go の行頭の `type`) のうち書き換え前に無いものを、同じ言語のファイルの型の宣言と `git grep -E -w` の完全一致で照らす。dupscan は使わない (`index` は型を返さず、Go は `def` が使えない)。型の注入は計測ログに書かない (ログは関数の注入の A-B を数える) |
| W5 名前の近さ | **実装 (語の数で絞る)** | 前方一致の候補を、短い方の名前が 2 語以上のときだけ出す (`resumeReady` に対する `resume()` を出さない)。失うもの: 1 語の既存の関数 (`upload`) に語を足した新しい名前 (`uploadAll`) では出ない。案の「署名の近さで並べ替える」は採らなかった: hook は書く前の文字列から新しい関数の型を確定できず (推論・generic・別の行の型)、並べ替えても件数は減らない |
| W6 共有の置き場の取り込み | **見送り (963 へ)** | dupscan は target も symlink の取り込みも読まず、SwiftPM / XcodeGen の target の解決を足すのは dupscan の責務 (構文木) の外。手順は skill の 4 節 (959 / 960) にある。再開の条件「同じ取り違えで build が落ちた例が 1308 の後にもう 1 件出たとき」は、963 (obaket 1309 の `UnusedFileBodyCache.swift` が macOS の Xcode ターゲットだけで落ちた) で満たされた。続きは 963 で扱う |
| W7 作業中の組の差 | **大半は既にあった** (案内だけ足した) | gate の違反が「新しい写しを含む組」、`-v` に「消えた組・縮んだ組」と「新しく足された除外」 (足した除外の要素。黙らせた組の一覧ではない) が出る。gate は前後の hash の差と同じではない (前からある写しに同じ修正を入れて形が移った組は違反にならず、組の除外は違反を数える前にかかる)。skill の 3 節に、まず `gate -v`、それも見たいときだけ report の json を前後で突き合わせる、と書いた (962 の反証レビューで「足りる」と書いた誤りを直した) |

- 検証: `make -C src/dupscan test lint` rc=0、`bash tests/claude/test_inject_dupscan_candidates_on_new_func.sh` ok=32 fail=0、`make test-lint` rc=0、`dupscan gate` で新しい重複なし。dotfiles の report は変更の前後で同じ (組 59・効いていない除外 8、出力の差 0 行)
- 変異 (mutate-verify-list、実装の 11 本。どれもビルドが通り、狙ったテストが赤): W3 の引数の上限を外す / クロージャの文 1 つを 1 つ以上にする / Go を呼ぶ先だけの判定に戻す / gate の印を付けない、W2 の改名の候補から自分の釘を外さない / 増えたメンバーを出さない / 今の hash に記録の hash を出す、W5 の語の数の条件を外す、W4 の書き換え前の型を新しいと数える / `-w` を外す / `class func` を型と読む。`-w` を外す変異は最初は緑で、テストの向き (足す名前の方が長い形) が逆だった。既存の長い名前の前方 (`Loose` ⊂ `LooseType`) の負例を足して赤を確かめた
- `bin/skill-eval dupscan`: PASS (cases 2/2)
- **反例探し 1 周目 (codex sol-high、読み取り 1 本)**: P1 なし・P2 8 件。8 件とも先にテストで赤を確かめてから直し、直した箇所に変異 8 本 (どれも狙ったテストが赤):
  ① W2 の増えたメンバーを釘で数えていた (同じ関数に並んだ同じ本体のクロージャは同じ釘になるので 2 個が 1 個になる) → 単位で数える
  ② W2 が、消えたクロージャの鍵に残った外側の関数が当たる形を「本体が変わった」と断定していた → 言い切らない文にした
  ③ W3 が Swift の添字 (`values[key, default: 0]`) を呼び出しと数えた → 丸括弧の引数だけ
  ④ W3 がクロージャの 1 文が `for` / `if` のような制御文でも印を付けた (中に任意の量の処理を入れられる) → 制御文を数えない (Swift・Go)
  ⑤ W3 が Go のクロージャに印を付けなかった (Swift は付ける) → 付ける (Delegation は関数の宣言だけのまま。プリセットの対象を変えない)
  ⑥ W4 が Go の generic の型 (`type X[T any]`) を取りこぼした ⑦ W4 が Swift の `package` 修飾子を取りこぼした ⑧ W5 が全大文字の 1 語 (`RESUME`) を 6 語と数えた → 大文字の連なりを 1 語にする
  - `soleCall` に分けたことで既存の `Delegation` が変わる入力は、反例なし
- **反例探し 2 周目 (直した差分と help・文書。codex sol-high、読み取り 1 本。codex の 5h 枠のリセット後に回すのはユーザー決定)**: P1 なし・P2 6 件。全部テストで赤を確かめてから直した:
  ① Go のクロージャの 1 文が代入 (`ready = true`) だと印が付かなかった (Swift は付く) → 制御文でない 1 文を許す
  ② Swift の `defer { … }` を文法が呼び出しとして読み、制御文の除外をすり抜けた (Go の `defer func(){…}()` も同じ形) → クロージャの 1 文の中のクロージャも同じ条件 (制御文でない文 1 つ) を満たすときだけ印を付ける。
     最初は入れ子を一律に断ったが、obaket で 1308 の waitUntil の 1 組 (`{ self.updates.contains { $0.id == id } }`) の印が外れたので、入れ子の文の数で決める形にした
  ③ hook の語の数で `BASE64` を `BASE` と `64` の 2 語と数えた → 大文字の連なりに続く数字を同じ語にする
  ④ 同じ釘のクロージャが減った組の除外 (記録 2 個 → 今 1 個) で「詳しく:」が 0 行だった → 釘ごとに記録と今の単位の数を比べて出す
  ⑤ 同じ鍵の単位が複数のとき、今の hash を出していなかった → 並べる
  ⑥ hook の型の注入文が「同じ module なら衝突」のままだった (skill は package に直していた) → Go は同じ package、Swift は同じ module の top-level どうしか同じ型の中、と書く。skill の 1 節も同じ文にした
  - 変異 11 本 (どれも狙ったテストが赤): ①を戻す (代入を断る) / ②の入れ子の判定を外す (Swift・Go) / ②を入れ子の一律拒否にする (Swift・Go) / ③の数字を語に含めない / ④の数の比較を外す / ⑤の今の hash を出さない / ⑥の文を module に戻す
  - 反例なし: Swift の添字の除外・後置のクロージャの数え方、通常の形の Go generic と Swift `package` の型の照合
- **反例探し 3 周目 (2 周目の直しの差分だけ。codex sol-high、読み取り 1 本)**: P1 なし・P2 3 件。
  ① Swift の唯一の文がクロージャ式 (`saveFactory { { validate(); persist() } }`) のとき、そのクロージャ自身を検査していなかった → 入れ子の判定を文そのものから始める
  ② 型の注入文が、別のファイルの Swift の `private` の型どうしも衝突すると読める → 「Swift の private / fileprivate の型は同じファイルの中だけ」を足した (skill の 1 節も)
  ③ 1 文に大きな処理を詰めた写し (`_, _, _ = validate(), persist(), publish()`・複数の束縛の let) にも印が付く → **直さない**。2 周目の前から式 1 つ (`use(a(), b(), c())`) には付いていた基準の限界で、
     印は組を隠さず gate も変えない (skill も「印だけでは除外しない」)。コード (`oneStatementClosure` のコメント) と README に検出しない形として書いた。
     再開の条件: 大きな 1 文に印が付いて除外を誤った例が出たら、節の数の上限を足す
  - 変異 2 本 (①の自身の検査を外す / ②の注記を外す) はどちらも狙ったテストが赤。4 周目は回していない: 3 周目の直しは判定の始点 1 か所と文言で、どちらも変異で固定した。残る形 (③) は検出しない範囲として書いて閉じる
  - StaleDetail の誤報・二重表示、語の数え方の退行、薄い委譲の印が外れる新しい反例は「反例なし」
- obaket (`bc93d8dff` の snapshot、読むだけ・除外なし) の印は 1 周目の直しの後 37 / 345 組、2 周目と 3 周目の後 36 / 345 組。外れた 1 組は probe の、文が 2 つのクロージャを入れ子に持つ組で、1308 の waitUntil の 4 組は全部残っている
- 962 の反証レビューで、W7 を「既にあった」とした記述と skill の 3 節の「`gate -v` で足りる」が言い過ぎと分かった (上の W7 の行)。W6 の再開の条件は 963 で満たされた (上の W6 の行)
- help・文書への反映 (ユーザー指示 2026-10-29): `-v` の flag の help (`capabilities` の flags_help にも出る) と README の flag の表に、効いていない除外の「詳しく:」を足した。README に `single_call` の印と json の `exclude` の段落、`docs/dupscan-overview.md` の hook の表に型の照らし (W4) と前方一致の条件 (W5)、skill の 1 節に注入の hook を見たときの扱い、3 節に json の `exclude`・印・作業中の `gate -v` を足した。skill の整合を 3 つの見方で見直し (Go で衝突する単位を module から package に直した)、`bin/skill-eval dupscan` は再び PASS (cases 2/2)。3 周目の直しの後の検証: `make -C src/dupscan test lint` rc=0、hook のテスト ok=38 fail=0、`make test-lint` rc=0、`dupscan gate` で新しい重複なし、`bin/skill-eval dupscan` PASS (cases 2/2)
