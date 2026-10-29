# 979 (research): SAC (swift-standard-app-components) の重複の統合で使った所見

> 🚨 **担当中: air5 (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

`/dupscan-dogfood sacの重複メソッドを統合して` で、my-products の `lib/swift-standard-app-components` (Swift、56 ファイル・385 単位) の重複を寄せたときの所見。
摩擦は 3 件 (分かりにくい 2・雑音 1)。所見の記録だけの issue なので反証レビューは省いた (dupscan-dogfood の 5 節)。
既存の issue との重複照合はしていない (気づいた既出だけ各項に書いた)。
起票の時点では、別のセッションが同じ時間帯に dupscan を直していた (`src/dupscan` に 1 時間以内の commit・epic の `next/` に claim が 2 本) ので直さなかった。
その後、ユーザーの指示 (2026-10-29「今作成した dupscan への改善案 issue は今対応して」) で F1〜F3 を直した (下の「対応 (2026-10-30)」)。

## 開発タスクの結果

SAC の master に 3 commit (`test(toast): 2 つのテストの waitUntil を共通の 1 本に寄せる` / `refactor(window-background): makeNSView と updateNSView の material / blendingMode の代入を 1 つにする` /
`refactor(toggle): ラベルの解決先の enum を LaunchAtLoginToggle と MenuBarVisibilityToggle で 1 つにする`)。`swift test` は 165 件で失敗 0 (前後で同じ件数)、SwiftLint `--strict` は 0 件。

組の数 (repo root から `dupscan -format json -limit 0 .` の `groups` を数えた): **前 4 → 後 0**。内訳は寄せて消えた 1 (waitUntil の L1)、寄せた後の 1 行の委譲を除外 1 (トグルの L2)、
寄せない判断で除外 2 (Examples/ ↔ Tools/ScreenshotGenerator の L2・L1。Examples/ はビルド対象外のコピペ元で import できない)。ほかに sweep の節 5 の 1 件 (WindowBackgroundView の makeNSView に updateNSView の本体) を寄せた。

## dupscan を使った回数と役に立った場面

- dupscan 23 回 (capabilities 1・report 6・sweep 3・gate 1・explain 1・def 1・refs 1・index 7・callers 2)、コードの検索で grep に戻したのは 3 回
  - テスト関数の中に手書きした待ちのループ (`for _ in 0..<200` / `Task.sleep`) を探す: sweep の節 5 はテスト関数の中を見ないため (965 の F1 と同じ理由)。見つかったのは固定の `Task.sleep` の待ちだけで、寄せる対象ではなかった
  - `libDefault` の case の使われ方: `refs LabelSource` は型の位置の参照だけを返すので、case を名指しした行を見るために grep した
  - `waitUntil(` の呼び出しの数を base の版で数える: F1 の裏を取るため (`git show <base>:<file> | grep -c`)
- 役に立った場面 4 件:
  - report の 4 組が、そのまま寄せる候補と寄せない候補の全部だった
  - `explain -compare` で、トグルの L2 の違いが文言のリテラル 1 つだけと分かった (寄せ方を決める根拠になった)
  - sweep の節 5 が WindowBackgroundView の手書きの写しを出した (report には出ない形)
  - gate `-v` が、消えた組と自分で足した除外 4 要素を並べた (報告に書く除外の一覧にそのまま使えた)

## 摩擦

### F1 `index` の呼び出しの数が、同名の private 関数どうしで合算されて両方に同じ値が付く

- 場面: 寄せ先を書く前に `dupscan index -name wait` を打った (SAC の repo root)
- 期待: 2 本の private な `waitUntil` に、それぞれの呼び出しの数が付く
- 実際: 両方に `(呼び出し ~20)` が付いた。base の版を数えると ToastManagerTests が 12、ToastContainerFrameTests が 8 で、20 は 2 本の合計 (`git show 636dd3f:<file> | grep -c 'waitUntil("'`)。
  private なので、ファイルをまたいで呼ぶことはできない
- その後: どちらを寄せ先にするかの判断には使わなかった (新しいファイルに置いたので影響なし)
- 種類: 使い方が分かりにくい (注意書きの「型未解決で同名の別定義が混ざりうる」から読み取れはするが、`[private]` と出ている単位でも合算される)
- 提案: `[private]` / `[fileprivate]` の単位は、同じファイルの呼び出しだけを数える

### F2 Swift の `callers` で、XCTest のテストメソッドの中の呼び出しが全部「(単位の外)」になる

- 場面: 寄せた後に `dupscan callers -limit 0 waitUntil` を打った
- 期待: 呼び出し元のテストメソッドの名前 (`testReportsTheBubble…`)
- 実際: 21 行のうち 20 件 (全部の呼び出し) が「(単位の外) [test]」。README の 75 行目に Swift の比較対象外のテストはそう出ると書いてあるので仕様どおり。
  `index -file Tests/…/ToastContainerFrameTests.swift` にもテストメソッドは出ず、`host` だけが出る
- その後: テストの名前は要らない場面だったので困らなかった
- 種類: 未対応 (既出の可能性: 937 は Go のテスト関数だけを名前で出すようにした。Swift は残っている)
- 提案: 937 と同じ形で、Swift の XCTest の `func test…` も呼び出し元として名前を出す

### F3 sweep の節 2 に、`Text("…", bundle: .module)` を 1 つ返すだけのクロージャが 12 件並ぶ

- 場面: 既定の下限で 0 組にした後、`dupscan sweep .` の節 2 (下限 12 節) を見た
- 期待: 寄せる価値のある候補だけ。あるいは report のような「1 つの呼び出しを 1 回するだけ」の印
- 実際: `[L2] prod ×12 節 14` の組。どれも lib の文言 (`Text("Language", bundle: .module)` など) を返す header / label のクロージャで、
  xcstrings の抽出のためリテラルで書く必要があり寄せられない。report の 1 組目には `印: 全員の本体が 1 つの呼び出しを 1 回するだけ` が付いたが、sweep の節 2 には印が出ない
- その後: 12 件を目で確かめて見送った。同じ節の L1m の 3 組 (別の型の `init` がフィールドを代入するだけ) も見送った
- 種類: 雑音 (955 の「sweep の節 2 は 20 組中 1 か所だけが寄せる価値」と同じ傾向)
- 提案: sweep の節 2 にも report と同じ `single_call` の印を付ける

## 見送り

- 見送り: gate `-v` の「消えた組・縮んだ組」に、形が移ったトグルの組 (節 35 → 22) が出なかった。skill dupscan の 3 節に「前からある写しに同じ修正を入れて形が移った組は違反にならず」とあり、文書どおり

## 対応 (2026-10-30)

- **F1**: `index` の「呼び出し ~N」で、他のファイルから呼べない単位は呼べる範囲の呼び出しだけを数える (Swift の private / fileprivate は同じファイル、Go の小文字は同じ dir)。`internal/report/index.go` の `callableFrom`
  - SAC の寄せる前の版 (`636dd3f`) で、前 `~20` / `~20` → 後 `~12` / `~8` (base の版を `grep -c 'waitUntil("'` で数えた数と一致)
  - Go の `_test` package も同じ package と数える (変更前からの過大計数。区別しない)
- **F2**: Swift の `callers` で、比べる単位にしないテスト関数 (XCTest の `func test…()`・swift-testing の `@Test`) と、その中の局所の関数を、呼び出し元として名指しする。`internal/lang/swift/swift.go` の `calls` / `callOwner` (署名の組み立ては `declSignature` で `declUnit` と共有)
  - SAC の今の版で、前は 21 行中 20 件が「(単位の外)」→ 後はテストメソッドの名前 (例: `ToastContainerFrameTests.testReportsTheBubble…`)。テストメソッドの中の局所の関数 `height(for:)` からの呼び出しは、その関数を名指しする
  - テスト関数の中の局所の型の property / didSet は、持ち主を「不明」と明示する (署名の無い `CallOwner{Test: true}`)。「(単位の外) [test]」と出る
- **F3**: sweep の節 1・2 の組にも、report と同じ「1 つの呼び出しを 1 回するだけ」の印を付ける (`report.SingleCall` / `report.SingleCallMark` を公開して共有)
  - SAC の今の版で、節 2 の印は前 0 → 後 2 組 (`Text("…", bundle: .module)` の 12 件の組と L1 の組)。`init` の代入だけの L1m の組には付かない

確かめ方:

- 変異 (`mutate-verify`。全部で想定のテストが赤): sweep の印を消す / 印を常に出す / Go の dir の判定を外す / テスト関数に持ち主を付けない / 局所の型で持ち主を外さない / 局所の型で持ち主を nil にする / クロージャの組だけ印を消す
- `make -C src/dupscan test lint` rc=0。ルートの `make test` は rc=2 で、赤は dupscan の外 (`test_dangling_symlinks` の `~/.config/nvim/coc-settings.json`・`test_dotfiles_check_result_ownership` の一時 dir の `rm: Directory not empty`) と、
  別のセッションが origin で既に消した `query_ux.go:relevantParseErrors` の unused (`2f146e3e8`。rebase で取り込む)
- codex (gpt-6.1-sol) の敵対的レビューを 3 周。1 周目 P2 1 件 (テスト関数の中の局所の型の property がテスト関数に帰属) と P3 2 件、2 周目 P2 1 件 (持ち主を nil にすると同じ行の別の関数へ帰属し、test の印も落ちる) と P3 1 件 (sweep のテストにクロージャの組が無い)。どれも直した。3 周目は「壊せなかった」

残り (不具合ではない、テストの射程の外):

- json の `caller` の欄が省略されること自体と、`context` の callers 節は、テストで直接見ていない (`BuildCallers` の同じ経路を通る)
- テスト関数の中の局所の型の `init` / `subscript` / 入れ子の型は、テストで直接見ていない (`callOwner` は関数と同じ分岐で作る)

## 進捗

- [x] 起票 (この issue)
- [x] F1〜F3 を直す (上の「対応 (2026-10-30)」)
