# dupscan

repo の中で、本体が同じ形のメソッド (関数・初期化子・クロージャ) を構文木で見つけて組にする CLI (`bin/dupscan`)。
Claude Code にコードを書かせている間に、似たメソッドが量産されるのを見つける。読む言語は Swift と Go (拡張子で adapter を選ぶ。`def` / `refs` は Swift だけ)。

設計と狙いは [`docs/dupscan-overview.md`](../../docs/dupscan-overview.md)、同じと判定する方法と誤検知は
[`docs/dupscan-detection.md`](../../docs/dupscan-detection.md)、要件の正本は epic 890 (issue 891)。

## ビルドしない起動

`bin/dupscan -no-build <command> ...` はwrapper専用の先頭option（issue 928）。ビルド済みバイナリの入力指紋が現在のものと一致するときだけ、先頭optionを外して同じargv・終了コードで実行する。例: `bin/dupscan -no-build -help`。通常起動の自動ビルドは従来どおり。

未ビルド・指紋不一致・指紋を取得できない場合はstdoutを空にしてrc=2と短いstderrを返す。同期/非同期ビルド、SDK取得、lock取得、失敗記録の更新は行わない。`GO_AUTOBUILD_SYNC`もこの指定を打ち消さない。古い入力で失敗した記録だけが残っていても、現在の指紋とビルド記録が一致するバイナリは実行できる。

判定は共通go_autobuild helperの既存規則（Goソース、go.mod/go.sum、相対パスのreplace先のパス・mtime・サイズ）を使う。内容hash・toolchain/SDK・環境変数・C/埋込asset等まで含む完全なビルド再現性の検証ではない。mtime/サイズを保った変更を見抜く保証も加えない。新しいworktreeはそこに対応するビルド記録が必要で、別worktreeの成果物を流用しない。

`-no-build`はGoバイナリ自身のflagではなく、先頭以外では消費しない。現在のバイナリを実行した後の解析量や動作は選んだcommandに従う。通知hookからindex/report等を起動して全repoを解析してよい、という意味ではない。capabilities/doctor/agent-guideを利用できる。Go向けgrep案内hookもこの経路で同梱CLIへ対応状況を問い合わせる。

## 対応状況と導入の確認

```sh
bin/dupscan -no-build capabilities -format json
bin/dupscan -no-build doctor -repo /path/to/repo -format json
bin/dupscan -no-build agent-guide -target claude -lang go -print
bin/dupscan -no-build agent-guide -target codex -lang swift -print
```

`capabilities` はrepo不要で、登録済みdispatchとadapterから対応コマンド・出力形式・言語・近似の限界を返す。`flags_help`は各コマンドの実際の`-h`経路から得るため、別のflag一覧を写さない。helpの構築だけで、source/config/gitやparserを起動しない。JSONは`schema_version: 1`の能力表で、926の分析結果用json-v2 envelopeとは別。text/jsonを明示的に選べ、両方とも最大65,536 bytes。登録されていないcommandは案内しない。

`doctor` は実行パス・git・repo/worktree・拡張子の列挙・optional config・ローカルのgate baseを確認する。構文解析・build・fetch・重複検出はしない。各checkはok/info/warning/error/not_checkedで、入力/実行/configエラーはrc=2、source/baseが無い等のwarningはrc=0。`.dupscan.json`不在は正常で、HEADとbaseが同じでもgate合格とは表示しない。text/jsonは最大65,536 bytes、reasonは最大4,096 bytes。

`agent-guide` は`-target claude|codex -print`を指定して、最大4,096 bytesの例だけを表示する。`-lang`はガイドの表示を選ぶもので、各検索コマンドのscopeを自動で絞るflagではない。例のTYPE/TERM/SYMBOLを置き換え、対象repoのcwdまたは末尾のdir指定で使う。instruction/設定の書換え・例の自動実行はしない。Claude固有hookがCodexでも動くとは案内しない。

Goのgrep案内は同じcheckoutのwrapperを固定argv `-no-build capabilities -format json`で呼び、index/callersがGo対応と確認できたときだけ具体例を出す。未準備/不正な能力情報なら手動確認の短い案内に留める。通知は既存のsession/言語単位の900秒制限を保ち、追加contextは最大1,024 bytes。Swiftの通常の案内文・stampは921の測定を妨げないよう維持する。通知からrepo全体の構文解析や自動buildは起こさない。

## 使い方

```sh
bin/dupscan                      # cwd の repo 全体。重複が無ければ 1 行だけ
bin/dupscan macOS Shared         # 起点を repo の中のディレクトリにする (大文字小文字は git が正規化する)
bin/dupscan -format json .       # 全部の組を JSON で
bin/dupscan -format xcode .      # Xcode / SwiftLint と同じ `ファイル:行:列: warning:` の形式
bin/dupscan -limit 0 -v .        # text で全部の組と、読めない構文の単位の一覧
bin/dupscan -levels L1,L2,L3 .   # 値の位置まで畳んだ一致 (L3) も見る (偶然の一致が多いので既定では見ない)
bin/dupscan -levels L1,L2,L1m .  # メンバー名 1 語だけが違う写し (L1m) も見る (Swift では SwiftUI の body などで組が倍増するので既定では見ない)

bin/dupscan gate                 # upstream との merge-base から作業ツリーまでに新しく増えた重複。無ければ 1 行
bin/dupscan gate -base origin/master .        # base を明示する (CI の push なら push の前の commit、PR なら対象のブランチ)
bin/dupscan gate -base HEAD~1 -head HEAD .    # 2 つの commit を比べる (過去の commit の流し直し。checkout しない)
bin/dupscan gate -v -base <寄せる前> .         # 寄せた結果: base の組のうち消えた・縮んだ組の一覧 (1 行目には数だけ)

bin/dupscan near Sources/A.swift:location     # この単位と同じ形の単位 (一致した最も細かい段で)。起点を書かなければ repo 全体と比べる
bin/dupscan near Sources/A.swift:42 Sources   # 行で指す (その行を含む単位のうち最も後で始まるもの)。起点を絞る
bin/dupscan explain Sources/A.swift:location  # 段ごとに潰した形・ハッシュと、比べない理由 (なぜ同じ / 違うと判定したか)

bin/dupscan search -query "connection bucket" -returns StorageLocation -min-matched-terms 2

bin/dupscan index -returns StorageLocation    # 書く前に引く: 戻り値の型の一部で既にある定義を (1 行 1 件・上位 20 件)
bin/dupscan index -name location -param-type String   # 名前の一部・引数の型の一部 (全部の条件に当たるものだけ)
bin/dupscan index -doc bucket                 # doc comment (/// か /** */) の最初の行の語
bin/dupscan index -file Sources/A.swift       # そのファイルの単位を行の順に (outline。件数で切らない)
bin/dupscan evidence bucket                   # index で見つからないとき: doc の全文・テスト・ソースの行にある語から候補を探す
bin/dupscan callers 'make(x:_:)'              # 呼び出しの箇所と、それを囲む単位 (名前だけでも引ける)
bin/dupscan def StorageOperationLockRegistry  # 定義の本体。型なら宣言とメンバー一覧
bin/dupscan def -max-lines 120 DropboxClient.upload
bin/dupscan def 'make(x:_:)'                  # 引数ラベルで完全一致
bin/dupscan def ObjectStorageProtocol -limit 10
bin/dupscan refs StorageOperationLockRegistry # 識別子の出現
bin/dupscan refs StorageStatsEvent -limit 10
bin/dupscan context DropboxClient.upload      # def・refs・callers を 1 回の走査でまとめて (変更の前に定義と使われ方を見る)
```

`callers` は呼び出しの箇所を `file:line 呼び出し元 [test] | その行` で出す (コメント・文字列の中は数えない。テスト関数の中も数える。Swiftの比較対象外テストの呼び出し元は「(単位の外)」)。
Goは比較対象外の `Test` / `Benchmark` / `Fuzz` / `Example` とtestify形のメソッドも、呼び出し元を名前・署名で表示する。匿名closure内は囲む名前つき関数に帰属する。top-levelや構文エラーのある関数内は所有者を確定せず「(単位の外)」とする。JSONは同じschemaを維持し、該当するGoテストの `caller` が省略から署名へ変わる。テスト本体/内部closureをreport/gateの比較へ追加する変更ではなく、indexの既定結果やGo def/refsの対応範囲も変えない。
型を解決しないので、同名の別のメソッド・別の型の初期化子の呼び出しも当たる (近似)。ラベルを書けば数と順で絞る。
後置クロージャのある呼び出しは括弧の中のラベルが先頭に一致すれば当てる。`index` / `callers` / `def` / `refs` / `context` / `evidence` はflagを名前・パスの前後で受け付ける (`callers make -format json`)。単値flagの繰り返しは最後の値を採用し、`--` 以降は名前・パスとして扱う (`callers make -- -literal-dir`)。値の必要なflagの直後の引数は、その値として扱う。


`callers -selector strings.Join Join [dir ...]` は、Goのソースに書いた単純な識別子chainを条件へ足す。名前の分からないhelperを、使うAPIの字面から探す補助経路で、囲む関数と位置を従来のcallers形式で返す。末尾名の位置引数は引き続き必要で、名前照合と、指定した場合の引数数条件へANDする（例: `callers -selector strings.Join 'Join(_:_:)'`）。queryのdot前後の空白・改行とsource側の空白・改行・括弧は正規化する。

selector指定時は同じscope内のGoだけを読み、Swiftは対象外。import先やreceiverの型を解決しないので、`tw.Join`へのalias変更やdot importは追わず、同じ字面のローカル変数によるshadowingは混ざる。`get().Join`、`items[i].Join`、`pkg.F[T]`のような動的/indexed calleeはこのfilterの対象外（通常のcallersへ戻して調べる）。コメントや文字列の字面からcallを捏造しない。0件でも検索範囲と限界を表示し、Go sourceのないscopeはrc=2でSwift対象外を説明する。

textにはselectorと限界の1行を添え、JSONは明示指定時だけ`selector`/`selector_limitations`を返す。既定modeのstdout/JSON・照合数・順位は変えない。`-limit`は従来どおりtextの件数上限で、JSONは全件を返す。parser errorは既存のstderr警告で通知する。別calleeを除けない問い合わせでは注意表示分だけ出力が増えるため、型index、evidence、適切なrg/Readも使い分ける。

`def` は名前の完全一致で定義を探す。`Type.member` で型を絞り、`make(x:_:)` のように引数ラベルも指定できる。
`count()` のように括弧を書いた場合は関数・初期化子・subscript・deinit・protocol の関数要件だけに当てる。
関数・初期化子・property・subscript は `file:line-endLine 型.署名` の見出しとソースを出し、既定 80 行を超えると残りの行数を示す。
struct / class / enum / protocol / actor / extension は宣言とメンバーの一覧を出す。protocol 要件・保存 property・enum case・typealias・associatedtype・入れ子の型も一覧に含める。
型の extension は宣言ごとに表示する。`Type.member` は generic 引数と `where` 句を除いた型名で照合するので、`Box<T> where T: P` は `Box.member`、条件付き extension は `Box.member`、`extension Foo.Bar` は `Foo.Bar.member` で引ける。
`Type.member` はその型の直接メンバーを探す。メソッド内の入れ子の `func` は対象外で、名前だけの `def` なら引ける。
同名の定義は全件を候補にし、text は `-limit` の既定 20 件まで出す (`0` は全部)。`-format json` は全件を返す。

`refs` の問い合わせは単独の識別子（`run` など）。`def` と異なり `Type.member` の絞り込みはなく、`refs Worker.run` のような識別子で始まるqualified問い合わせは、text/jsonともrc=2・stdout空で拒否する。stderrに非対応の理由と、同じscopeの単独名を探す`next_action`のargv配列を最大1個返す。型・ラベルを外すと同名の別型も含む近似になり、自動再検索はしない。単独のUnicode名・backtick付き名・dot operatorは一律に拒否しない（operatorの参照収集まで保証するものではない）。単独名のラベル指定など、その他の入力を網羅検証する変更ではない。
`refs` は識別子の構文上の位置を `file:line 囲む単位 [test] [型の位置|式の位置] | その行` で出す。これは意味上の型/値判定ではない。`Foo(...)` や `Foo.shared` の `Foo` も、構文上は式の位置として出る。
宣言名・呼び出しの引数ラベル・コメント・文字列本文は含めず、文字列補間の中の識別子は式の位置として数える。prod を test より先に並べる。
`@Test` / XCTest 関数の中の参照はその関数名を囲む単位として出す。メソッド内の入れ子の `func` は `Type.member` 照合の対象外。
単位の外でも型の中なら囲む型名を出す。`if let foo` などの省略束縛は外側の値への参照として数え、parameter pack の宣言名は除く。
text は `-limit` の既定 20 件まで (`0` は全部)、JSON は全件を返す。

`search -query "用途語..."` は名前・署名・doc先頭行を横断する字句検索。型の字面filter（`-returns` / `-param-type`）と`-file`はindexと同じAND条件で、型推論・alias解決はしない。語はUnicode小文字化して重複を除き、1〜8語・全体512bytes・各語64runes以内。camel/acronym/snakeの境界は小文字化前に分け、日本語は原文の部分一致だけを使う。各語が一致したfieldと不足語を示す。意味理解や再利用可能性の証明ではなく、すべて`reuse=not_proven`。

既定は1語以上が一致するOR候補検索。`-min-matched-terms`で必要語数を増やせる。順位は一致語数、語ごとの最強fieldの加点（name3/signature2/doc1）、名前完全一致、shared、非hidden、prod、場所の順。呼び出し数を順位に使わず、scoreを確率とは扱わない。`-lang auto|swift|go`、`-scope all|prod|test`を選べ、Goテスト関数等の索引除外はindexと同じ。

初期版はtextのみ。`-limit`は既定5/範囲1〜50、`-max-bytes`は既定8192/範囲1024〜1048576で要約・カード・案内全体に適用する。docは160runesで切り、候補数・表示数・省略・parse errorを示す。予算にカードが入らなくても候補数を0と偽らない。必須要約すら収まらない場合だけrc=2/空stdout、通常の0件はrc=0。json-v2/-repo/-path/cursorは926と統合するまで追加しない。

候補の`read_argv`は今回の走査位置から最大80行を確認する配列（自動実行しない）。`source_read_truncated`で残りを示し、元ソースの編集後は位置を再確認する。0件時は、必要一致語数の緩和、明示filter1個だけの解除、またはdoc全文/テスト/sourceへのevidence案を返す。変更条件をreasonに明記し、元の絶対directory scopeを保つ。再検索案は残余予算に収まるときだけ出す。ORで0件の語を1語に減らすだけの無益な再検索はしない。

単体`callers`のtextも先頭に構文近似の説明を1行出す。裸の名前は引数ラベル・個数で絞らず、label指定は名前とlabel/個数で照合する。Swiftの既定引数省略の注意は、Swiftを読むscopeでlabel指定した場合だけ示す。型未解決の同名混入と、0件でも未使用の証拠にならない点は共通。JSONと照合結果は維持する。

解析済み索引の型/file/scope候補が0件なら無益な語数緩和を飛ばす。明示filterがあれば1条件の解除案、なければ既存のevidence案を返し、いずれも予算内でのみ表示する。元の必要語数と他のfilterを維持し、追加説明は要約/argvの残余予算で表示する。型aliasの解決や意味上の不存在判定ではない。

`context` は `def` と同じ問い合わせ (`名前` / `Type.member` / `名前(ラベル:…)`) を受け、1 回の走査から `def`・`refs`・`callers` の節を並べて出す。
定義は `def <問い合わせ>` と同じ本体・型のoutlineを出す。参照・呼び出しは、同じファイルの連続したヒットを引用符付きのパス見出しにまとめ、各行を `line:col 囲む単位 [印] | ソース` として出す。行を抜き出して使うときは直前のファイル見出しも残す。938では単独の `def` / `refs` / `callers` の一覧を変更しなかった。現在の単体`callers`は上記の近似説明headerを追加する（issue 930）。
参照・呼び出しは `Type` では絞らない。`Type.init` は型の名前で引く (`Foo(…)` の形の呼び出し。参照は型の名前の全部の出現になる)。
呼び出しの数え方は `callers` の限界をそのまま引き継ぐ: `.init(`・`Self(…)`・`Foo<T>(…)` と書いた呼び出しは数えず、ラベルで引くと既定値のある引数を省いた呼び出しも数えない (要約の行にも出す)。
1 行目に3つの件数・近似の方法・省略の有無・構文解析失敗ファイル数・未対応言語の有無を書く。省略には件数制限、本体の行制限、選ばなかった非空の節を含む。続けて近似の範囲・定義が0件か1つに決まっていないこと・読めないファイル・読まなかった言語を書く。
Swift だけを読む (`def` / `refs` と同じ)。呼び出しの節も Swift の分だけで、Go の呼び出しも数える単独の `callers` とはここが違う。出力は text だけで、0 件でも rc=0 (入力の誤り・判定不能は rc=2)。`-limit` は節ごとの件数、`-max-lines` は定義のソース行。
`-sections definition,refs,callers` が既定。カンマ区切りで必要な節だけを選べる（出力順は定義・参照・呼び出し）。`-sections summary` は定義の署名・docの最初の行と件数だけにし、本体や参照の各行を省く。名前・パス・件数は維持するが、summaryだけでは実装や影響範囲は判断できない。短い出力を既定にはしない。
結果があれば `next_action: {"kind":...,"reason":...,"argv":[...]}` を節の前に表示する。先頭は従来どおり省略した定義の本体、参照一覧、または呼び出し元の取得。qualifiedな`Type.member`で参照または呼び出し一覧がsummary/件数上限で省略される場合だけ、同scopeの末尾型名を`evidence`で探す案内を2本目に追加する（最大2本）。generic/whereを外した末尾型名の語一致であり、同名別型・コメント・文字列も混ざる。型解決によるmember一覧の絞り込みではなく、構築/使用行から該当fileを確認するための回復導線。bare照会・一覧全件表示・定義本体だけの省略では追加しない。検索範囲を絶対パスで保持する。自動実行しない。argvはshell文字列ではなく配列として渡す。これを926のjson-v2 envelopeの実装済みとは扱わない。

json を出さないのは、既存コマンドの json の形を固定して変えない決まりのため (ここで出すと、状態・省略・継続取得を持つ共通の形より前に、固定する形が 1 つ増える。issue 926・927)。

`evidence` は語 (大文字小文字を区別しない) を含むソースの行を探し、3 つに分ける。同じ行を 2 つに数えない。語の一致であって、定義・呼び出しの照合ではない。
- doc: doc comment の全文のどこかにある行。その doc が付いた宣言を 1 件にまとめて出す (`index -doc` は最初の行しか見ない)。
  Swift は宣言 (関数・初期化子・property (保存も)・protocol の要件・enum の case・typealias・型など) の `///` / `/** */`、Go は関数の doc だけ。
  宣言に近い側から続く doc comment の塊だけを数える (doc と宣言の間の非 doc コメントは飛ばす。塊の上に `// MARK:` などを挟んだ別の doc は入れない。
  `index -doc` の「最初の行」は挟んだ doc も含めて探すので、この形では 2 つの範囲が食い違う)
- テスト: test のファイルかテスト関数の中の行。囲む関数 (関数の外なら型) と、その行で呼んでいる名前を添える (Go のテスト関数は「(単位の外)」と出る。単独の `callers` はテスト名を表示できるが、evidenceにはこの機能がない)
- その他のソース: 残りの行 (コメント・文字列を含む)。囲む単位と、その行で呼んでいる名前を添える

先頭に、テストとその他のソースの行で呼ばれている名前のうち、repo の定義と同じ名前 (関数・初期化子・property の名前と Swift の型の名前) を行の数の多い順に出す (使われ方から候補へ辿る入口)。
型を解決しないので、同名の外の関数 (`t.Fatalf`・`strings.Join`・`append(contentsOf:)` など) の呼び出しも数える。
Swift と Go の両方を読む。出力は text だけで、0 件でも rc=0。`-limit` は節ごとの件数。行は 160 文字、囲む単位の署名は 80 文字で切り、壊れた UTF-8 は置き換える。
1行目の省略には件数制限と文字数制限を含む。ファイル見出しの下へ行番号・囲む単位・本文・呼び出し名をまとめる。
`next_action`は最大2個。最初は`read_source`で、表示順（doc→test→source）の最初の一致行の4行前〜75行後を、最大80行・実ファイルの末尾まで確認する`sed`のargv配列を返す。走査したscope内の絶対fileを使い、別cwdからも実行できる。`source_read_truncated=true`はfile全体を含まないこと、`owner_unknown=true`は囲む単位のmetadataが未取得であることを示す。Go testの所有者を推測で埋めず、原文から確認する。argvは自動実行せず、編集後は再検索して位置を照合する。長い定義の全文や意味上の再利用可否を保証しない。
2個目は従来の候補探索で、呼び出し名があれば`callers`、docだけの候補ならSwiftは`context` / Goは`index -name`。同名の別責務も含みうるため、先に原文の否定条件や用途を確認する。名前のない文字列一致でも最初の原文確認は案内し、0件では案内しない。

report の text は、組を大きい順 (メンバーのうち最大の単位の節の数) に並べ、先頭に「ファイルの組」(同じファイルの組み合わせに組が 2 つ以上あるもの) を件数と節の合計の大きい順に 5 行出す。
同じ画面・同じ型の写しが細切れの組で並ぶのを 1 行で見せ、寄せる作業の単位を決めやすくする (json は `file_pairs` に全部・各組の `nodes`)。

`index` は名前の付いた単位 (関数・初期化子・computed property・subscript など。クロージャとテスト関数は除く) を
`file:開始行-終了行 型.署名 [shared,test,private] (呼び出し ~N) // doc の先頭` の 1 行で出す (1 行の単位は `file:行`)。終了行は Read の範囲 (offset / limit) にそのまま使える。grep と違い呼び出し側の行・コメント・文字列は混ざらない。
text の doc は最初の文まで・100 byte までに縮める (`-doc-bytes N`、0 で縮めない。JSON と `-doc` の絞り込みは全文)。略語 (`e.g.` `Dr.`) の後ろでは切らないが、2 文目に理由や条件がある doc も最初の文で切れる。`-limit` で打ち切ったときだけ、先頭に `該当 N 件 (M 件表示 / 全部で約 X)。…rg -n -i -e 名前 -g '*.go' -- dir` の 1 行を出す (`callers` は `呼び出し N 件`)。dupscan が `rg` を実行するのではなく、実行できる 1 行の提示で、構文木の結果ではなく文字列の一致 (コメント・文字列にも当たる) になる。`def` / `context` の型メンバーの行も同じ形 (終了行つき) になる。
並べ方は名前の一致の強さ (同じ > 前方 > 一部) → 共通の置き場所 (`.dupscan.json` の `"shared": ["Tests/SharedTestSupport/**"]` のような glob。`[shared]` と出る) を前 → prod を test より前 → private / fileprivate / unexported (Go の小文字で始まる関数) でないものを前（他module/targetからの利用可否は未解決） → 呼び出しの多いもの → 場所。
`shared`は設定globに一致する置き場所の印であり、可視性と同じく再利用可能性の証明ではない。所属target・依存/import・test-only・条件付きcompile等は別に確認する。

textの先頭には構文近似の説明を1行出す（0件でも同じ）。名前と引数ラベル/個数で照合し、型を解決しないので同名の別定義が混ざりうる。Swiftを読むscopeでは、既定引数を省略した呼び出しの取りこぼしも示す。0件は未使用の証拠ではない。JSONの形式・数値・順序は変えない。

呼び出しの数 (`~` は近似の印) は `callers` と同じ当て方 (名前と引数ラベル。初期化子は型の名前) で関数と初期化子だけ数える。既定値のある引数を省いた呼び出しは数えない。
型は字面の一部で引く (`Tag` で `[Tag]` も当たる。型は解決しないので、typealias・同名の別の型は区別しない)。
キャッシュは持たず、引くたびに読む (DualNoteApp の 1130 単位で 0.5 秒。issue 897)。`--check` はまだ無い。

`explain -compare <file>:<関数名|行> <file>:<関数名|行> [dir...]` は同scopeの2単位をL1の順序付きAST構造で比較する（text/json、独立schema `dupscan.explain.compare.v1`）。例: `dupscan explain -compare Sources/Other.swift:helper Sources/Main.swift:helper`。既定のexplain出力・hash・group/gate判定は維持する。
構造pathは正規化AST内の位置であり、source位置ではない。直接のliteral tokenは字面を別欄に残し、property/member参照は未解決の式として表示する。initializerや実行時値を解決しない。局所名は単位全体の順で番号化し、文の挿入は後続番号や位置をずらす。差分は子の同じ位置を比べ、最適な編集対応・同一動作・再利用可否を証明しない。
`-limit`は差分件数の上限1〜50（既定20）。snippet/tokenは各160runes、literal tokenは差分の各側最大8個。各対象のsource fileとL1表示はそれぞれ1MiBまで、AST深さは256まで。上限超過/対象の解析失敗/異言語はrc=2。差分全数・返却数・snippet等の省略・scope内parse-error file数と予算を表示し、解析不完全を差分なしと混同しない。選択group全員一括比較とmanifest/target解析は未実装。

`near` は report・gate と同じ比べ方 (`group.Eligible` と段のハッシュ) で、指した単位と同じハッシュの単位を出す。指した単位そのものは出さない。
組の作り方 (被った組・入れ子のクロージャの組を外す) は通さないので、クロージャを指してもその一致が出る。
本体が局所の名前を参照しない単位どうしの L3 の一致は、report・gate では組にしないので、中身を見た一致と分けて出す。
組の除外とパスの除外は、指した単位と相手の 2 人の組に report と同じ関数で掛け、外した数を 1 行目に出す。名前で 2 つ以上に当たれば候補の行を並べて rc=2。
`gate` を手で試すなら、既にある関数の写しをわざと足してから `bin/dupscan gate` を回すと、`make test` で落ちるときと同じ違反が出る。

`gate` の終了コード: 0 = 止める段の新しい重複なし / 1 = あり / 2 = 判定不能 (base が無い・読むファイルが 0 件など)。
`-fail-levels` (既定 `L1,L2`) の段の違反だけで止め、ほかの段は報告だけ (`-levels` に L3 を足したとき、L3 は報告だけ)。セルフレビューと make test・CI で回す。

解析漏れも失敗にする場合は `bin/dupscan gate -strict-analysis -base origin/master .`。base/headのいずれかにparse errorがあれば、抽出できた単位が0件でも通知しrc=2にする。違反もある場合は、検出済み違反を出力した上でrc=2を優先する。通常modeのrc契約は維持するが、対象fileと「解析が不完全」を表示する。textのfile一覧はbase/headごとに-limitで制限し、件数と省略を残す（0で全件）。xcodeは正確なエラー位置を持たないためfile単位のwarningを出す。

JSON/recordには必要な場合だけ `analysis_incomplete`、`base_files_with_errors`、`head_files_with_errors`、`strict_analysis` を追加する。`failed` は従来どおり重複違反による失敗を示し、strictによるrc=2とは別。正常入力の通常modeは追加fieldを出さない。対象は指定scope内の対応言語のparseであり、型解決・未対応言語・SDKを含む全解析を保証するmodeではない。

`gate` の判定: 変わった単位 (鍵が base に無いか、本体のハッシュが base と違う) ごとに、細かい段から順に、他と一致した段の組を見る。
組のメンバーごとに出どころ (base の側の相手) がその段で持っていた形を引き、組の大きさが同じ出どころから来たメンバーの最大の数より大きいときだけ違反。
違反になった最初の段だけを出し、違反でなければ粗い段も見る (写しの組に同じ修正を入れ、粗い段で触っていない単位と同じ形になった場合を拾う)。
出どころは、ずれに強い順に 1 対 1 で当てる: 読めない単位は鍵 → 鍵と本体 (L1) → 同じファイルで本体が同じ (名前が同じ方が先) → 本体 →
鍵 (クロージャを除く) → 同じ修正を入れた写し → L2 → L3 の形 → クロージャの鍵
(クロージャの番号のずれ・移動・改名しながらの小さな編集・写しの組への同じ修正でも前の相手を引ける)。
鍵と本体が同じ単位の後の緩い段は、比べる対象 (下限以上で読める) の単位どうし・そうでない単位どうしの中でだけ当てる
(下限をまたいで育った・縮んだ単位は、最後に同じ鍵で当てる)。
base で L3 だけ比べなかった相手 (L3Trivial) から来た別の単位は、比べる対象だった相手と別の出どころとして数える。
違反では、組に新しく入った単位を「新しい」、前からいた単位を「同じ形」として出す。この変更で触っていない単位は「新しい」に出さない。新しい単位どうしの組は、全部を「新しい」に出す。
- 落ちないもの: 移動・改名・整形・寄せる作業・既にある写しに同じ修正を入れること・組の古い写しを消す (か、編集して組から外す) のと同じ変更で新しい写しを足す入れ替え (検出しない形)
- `-fail-levels` に粗い段を入れれば、細かい段の一致も止める (L1 の一致は L2 の一致を含む)
- 読めない構文がある変わった単位と、base/headの解析失敗fileを通知する。通常modeのrcは変えず、-strict-analysisではrc=2にする。base で読めなかった単位が読めるようになると、本体が同じでも新しい写しに見えることがある
- base が HEAD と同じ (CI で `-base` を付け忘れた疑い) ときは、1 行の出力に注記する
- 作業ツリーを head にしたとき、ignore されていない untracked から読んだファイルの数を 1 行目に出す (`-v` と json の `untracked_files` に一覧)。
  CI の checkout には無いので、手元と CI で結果が違ったときの手がかりになる
- 寄せる作業の結果として、base の組のうち head でメンバーが揃わなくなった組を「消えた組」、一部だけ揃っている組を「縮んだ組」として
  1 行目に数え、`-v` で base の側の位置を、json の `resolved` (`remain` は揃っている数。0 は消えた) に全部出す (止めはしない。issue 920)。
  base の組は report と同じ組 (段をまたいで L1 の組と、それを含む L2 の組が両方出ていれば、それぞれを数える)。メンバーの head の側の相手
  (出どころの逆引き) が、他のメンバーの相手のどれかと見ている段 (`-levels`) のどれかで同じ形なら、そのメンバーは揃っているとする
  (`remain` はその数。組が 2 つの形に割れただけなら縮んだと数えない)。
  除外は掛けない (除外を足しただけの組は消えない)。定数だけ変えて粗い段で残る組も消えない。相手が読めない構文になった組は数えない。
  相手が下限より小さくなった (1 行の委譲に縮めた) 組は消えたと数える。写しを同じ形の薄い委譲に置き換えて下限以上に残ると、消えない (report にも組として残る)。
  起点を絞った実行で、起点の外へ移した単位は消えたと数える。プリセットも掛けないので、写しを下限以上の「定数だけが違う 1 行の委譲」に
  置き換えた組は消えない。出どころは名前・並びで当てるので、写しを改名して空いた名前に無関係な単位を足すと、取り違えて消えたと数えることがある
  (そのときは改名した写しが違反として止まる)

| flag | 既定 | 意味 |
|---|---|---|
| `-format` | `text` | `text` / `json` / `xcode` |
| `-levels` | `L1,L2` | 組を出す段。L3 (値の位置まで畳む) は偶然の一致が多く、L1m (メンバー名を畳む) は Swift で組が倍増するので、どちらも既定では見ない |
| `-min-nodes` | 20 | これより節の少ない関数・初期化子・property は比べない (節 20 未満の組は 1 行の委譲などの雑音がほとんどだった。DualNoteApp の実測、issue 898) |
| `-min-closure-nodes` | 20 | これより節の少ないクロージャは比べない |
| `-limit` | 20 | text で出す組の上限 (0 は全部)。Claude が何度も回すので短くしている |
| `-v` | false | 読めない構文の単位・除外の一覧・各組の「除外の 1 行」を出す |
| `-no-exclude` | false | `.dupscan.json` の除外を外して回す (除外が正しいかを見直すため。形の誤りは rc=2 のまま) |
| `-record` | (なし) | gate だけ。1 回ごとの結果 (違反・除外・許可) を、時刻を添えた 1 行の JSON でこのファイルに追記する (回をまたいで数えるため)。書けなければ rc=2 |
| `-fail-new-exclusions` | false | gate だけ。base から新しく足された除外か、パスの除外で通した変わった単位があれば rc=1 (CI で人の目に届けるため) |

下限の既定20節とL1・L2の選択は、issue 898のDualNoteAppでの較正とobaketでの受け入れを反映したもの。調整は上のflagで明示する。

### 使う側の repo の `make test` と CI に組み込む

手元 (Makefile)。`dupscan` が無ければ黙って飛ばさず止める (飛ばすと「通った」と区別が付かない):

```make
DUPSCAN ?= dupscan

test: dupscan-gate

dupscan-gate:
	@command -v $(DUPSCAN) >/dev/null || { echo "dupscan が無い (dotfiles の bin/dupscan を PATH に置くか DUPSCAN= で渡す)" >&2; exit 2; }
	$(DUPSCAN) gate $(DUPSCAN_GATE_FLAGS) .
.PHONY: test dupscan-gate
```

CI (GitHub Actions)。runner には dotfiles が無いので、固定した commit の `src/dupscan` を取って build する。
runner の git が 2.44 より古いと、dupscan が git に付ける `--no-lazy-fetch` を知らずに rc=2 になる (ubuntu-slim は 2.43)。その runner では、CI の中だけでこの引数を外す shim を PATH の先頭に置く (通常の clone では効く場面が無い。実例は obaket の `.github/workflows/dupscan.yml`)。
**取り出し先は workspace の外にする** (gate は ignore されていない untracked のファイルも読むので、workspace に置くと取ってきた中身まで走査する)。
base は push の前の commit か PR の対象。CI の checkout は浅いので、base まで履歴を取る (無いと rc=2 で止まる):

```yaml
- uses: actions/checkout@<固定した版>
  with:
    fetch-depth: 0            # base の commit まで要る
- uses: actions/setup-go@<固定した版>
  with:
    go-version: "1.25"
- name: build dupscan
  env:
    DOTFILES_SHA: <固定した dotfiles の commit>
  run: |
    git clone -q --no-checkout https://github.com/jiikko/dotfiles "$RUNNER_TEMP/dotfiles"
    git -C "$RUNNER_TEMP/dotfiles" checkout -q "$DOTFILES_SHA" -- src/dupscan src/subproc
    (cd "$RUNNER_TEMP/dotfiles/src/dupscan" && go build -o "$RUNNER_TEMP/dupscan" .)
- name: dupscan gate
  env:
    BASE: ${{ github.event.pull_request.base.sha || github.event.before }}
    DEFAULT_BRANCH: ${{ github.event.repository.default_branch }}
  run: |
    case "$BASE" in ""|0000000000000000000000000000000000000000) BASE="origin/$DEFAULT_BRANCH" ;; esac  # 新しいブランチの push
    make test DUPSCAN="$RUNNER_TEMP/dupscan" DUPSCAN_GATE_FLAGS="-base $BASE -fail-new-exclusions"
```

- 手元で確かめたのは Makefile と build の手順 (dotfiles を clone して `src/dupscan` と `src/subproc` だけを取り出し、build して `make test` が写しで落ち・無ければ通り・
  `dupscan` が無ければ rc=2 で止まる)。obaket の1301には、2026-10-25にGHAの通常runの成功と、写しを足した一時PRのrunがrc=1になった記録がある。上の汎用スニペットが全repoでそのまま動く保証ではないので、新しい導入先でも両方を確かめる
- `-fail-new-exclusions` は CI でだけ付ける (除外を足した PR を人の目に届ける。手元のセルフレビューでは一覧を見て報告に書く)
- cgo で文法の C のソースを build するので、runner に C compiler が要る (GitHub の macOS / Ubuntu の runner には入っている)

### 除外の設定 (`.dupscan.json`)

誤検知・意図して分けている写しは、repo の root の `.dupscan.json` で外す (設計は issue 906)。report の json の各組と text の `-v`、
gate の違反の json と `-v` に出る「除外の 1 行」に `kind` (`false-positive` / `intentional`) と `reason` を埋めて貼る。
dupscan の json は `<` `>` `&` を escape しない (`->` は `-\u003e` にならない) ので、除外の 1 行はそのまま貼れる。

```json
{"version": 1, "exclude": [
  {"paths": ["Generated/**"], "kind": "false-positive", "reason": "生成コード"},
  {"paths": ["**/*Tests.swift"], "levels": ["L3"], "kind": "false-positive", "reason": "テストの表の形が似ているだけ"},
  {"group": ["<釘>", "<釘>"], "level": "L2", "kind": "intentional", "reason": "iOS と macOS で分けておく"},
  {"unit": "<釘>", "kind": "intentional", "reason": "寄せない理由"},
  {"preset": "l2-single-call", "kind": "false-positive", "reason": "定数だけが違う 1 行の委譲"}
]}
```

- プリセット (`preset`) は形で組を外す既定の規則 (issue 919)。今あるのは `l2-single-call` だけ: L2 の組で、メンバー全員の本体が「1 つの呼び出しに原子 (リテラル・名前・メンバーの参照・
  原子だけを埋め込んだ文字列・引数の無い呼び出し) だけを渡す」1 文 (`return`・`try`・`await`・後置の `!` は可) で、L1 のハッシュが全員違う組。
  例: `try TestScratchDirectory.makeFile(prefix: "a", contents: contents)` と `prefix: "b"` の版。寄せるには定数を引数にするしかなく、それは呼んでいる関数そのもの。
  パスの除外と同じく書いた後にできた単位にも効くので、gate はプリセットで通した変わった単位を 1 行目と json (`preset_passed`)・xcode の形式 (note) に毎回出す (止めはしない)

- 釘は `<鍵 (クロージャの順番と多重定義の #n を除く)>@<本体の L1 のハッシュの先頭 12 文字>`。行がずれても効き、本体を直すと外れる。
  釘は除外に書いた回数まで当たってよい (同じ関数に並んだ同じ本体のクロージャは、順番を入れない釘がどれも同じになるので、除外の 1 行に人数分書かれる)。
  書いた回数より多くの単位に当たる除外は効かせず「名指しできない除外」に出す (後から同じ本体のクロージャが増えた・別の単位が同じ釘になった)。
  同じ釘を 2 回以上書いた除外は、釘と回数の組み合わせも要素 (`group-pins:…`) にするので、書き換えると (釘を削って狭めた場合も) 新しく足された除外に出る
- 組の除外は、組のメンバーが全部その釘に当たるときだけ組を消す (新しいメンバーが増えれば出る)。単位の除外 (許可) は寄せる側に出さないだけで、数には入れたまま
- パスの除外は、glob の下の単位を (`levels` の段で) 寄せる側に名指ししないだけで、組の相手・出どころには残す。全員が除外の下にある組は出さない。
  除外の下から外へ写した新しい写し・除外の下の単位と組になった外の写しは、外の単位として止まる。除外で通した単位は json と `-v`・xcode の形式に一覧で出る
- gate では、組・単位の除外は base に同じ釘の単位があったか、その釘が新しく足された要素のときだけ効く (消えた写しの場所に後から置いた写しを覆わない)。
  新しく足された除外 (釘・glob の 1 つずつ) と、パスの除外で通した変わった単位の数を 1 行目に出す (`-fail-new-exclusions` はどちらでも rc=1)。
  base の設定が読めなければ空とみなして注記する。壊れた glob・知らない欄・理由の無い除外は rc=2
- 何も消していない除外は「効いていない除外」として出る (寄せ終わった・改名した・本体を直した。単位の除外はその単位がどの組にもいないとき、パスの除外はどの単位にも当たらないとき)。
  段・下限を既定から変えた走査では数えない。起点を絞った走査では、起点の外の釘を指す除外とパスの除外は数えない
- 単位の許可は、関数・初期化子・computed property などの直前のコメントにも書ける: `// dupscan:allow <false-positive|intentional> <理由>`。
  印はコメントの先頭に書き、宣言との間に空行を挟まない (属性の前は可。前の行の末尾のコメントは数えない。クロージャ・didSet には付けられない)。
  設定ファイルの `unit` の除外と同じ釘に置き換えて効かせるので、gate の扱いも同じ。設定ファイルと違い、本体を直してもコメントは付いて回り、
  釘が変わるので「新しく足された除外」に出る (一覧と xcode の形式はそのコメントの場所を指す)。種別か理由の無いコメントは rc=2

## 守っていること

- **調べる repo に何も書かない**: git は `subproc.GitCommand` (`--no-optional-locks`) で打ち、`.git/index` も書き直さない。継承した `GIT_DIR` などは外す
- **0 件は判定不能**: 起点に読むファイルが 1 つも無ければ rc=2 で止まる (起点の取り違えで黙って「重複なし」にしない)
- **「重複なし」は、節が下限 (既定 20) に満たない小さい単位を比べていない**。`report` の 1 行目に `小さい単位 N 個は比べていない (節が 20 未満。-min-nodes で下げる)` と数を出す (JSON は `below_min_nodes` / `min_nodes`)。そのうち**同じ形の組が下限 12 節で見える**ものがあれば `うち同じ形の組が N 組ある (-min-nodes 12 で見える)` と添える (JSON は `below_min_groups` / `below_min_floor`。除外の設定は見ない数)。
  小さい helper の写し (1 語だけ違う 10 行前後の関数、`or(a, b)` のような定義) はここに入る。lockman では 181 単位のうち 81 単位 (45%) が比べていない側だった。
  下げると見つかる一方で、意図して分けたものも出る (dotfiles の 4 module で `-min-nodes 12` が出した 17 組のうち、寄せたのは 14 組・一部だけ寄せたのが 1 組・別の規則として残したのが 2 組)。出た組は 1 組ずつ、同じ規則かを確かめてから寄せる (skill dupscan の 4 節)
- **読めない構文**: 抽出できた単位が ERROR / MISSING を含む場合は比較から外し、件数を出す。単位を抽出できない場合など、通知の限界は下記
- 列挙は `git ls-files --cached --others --exclude-standard`。symlink・消えたファイル・submodule の中は読まない
- テスト関数そのもの (Swift Testing の `@Test` / `@Testing.Test`、XCTest の `func test…()`、Go の `_test.go` の go test が呼ぶ関数 (receiver の無い `Test…` / `Benchmark…` / `Fuzz…` で引数 1 つ・`Example…` で引数なし。頭の後ろが小文字なら除かない) と、testify の suite が呼ぶ引数の無い `Test…` メソッド) とその中のクロージャは比べない
- 単位: Swift は関数・初期化子・クロージャ・computed property・subscript・deinit・`didSet` / `willSet`。Go は関数・メソッド・関数リテラル (本体の無い宣言は除く)
- Go の生成されたファイル (`// Code generated … DO NOT EDIT.`) は単位も呼び出しも出さない (寄せる対象ではない)
- `index` / `callers` / `def` / `refs` の解析不完全の通知 (stderr) は、読めなかったファイルのどれかが問い合わせの名前 (小文字化した部分一致) を本文に含むときだけ全文 (件数・先頭 3 path・「0件は不存在の証明ではない」)。どれも名前を含まないときは `読めないファイル N 個は問い合わせの名前を含まない` の 1 行に縮める。名前を持たない問い合わせ (`index -returns` だけ等)・本文を持たないファイル・`init` の部分文字列になる名前 (初期化子の「呼び出し ~N」は型名で数えるため) では全文のまま
- `def` / `refs` は Go のファイルを読まない。読まなかった数を stderr に出す (黙って「定義なし」にしない。Go の定義・参照は gopls で引く)

## 分かっている限界 (誤検知・見逃しの形)

- def / refs は構文木の近似で、型を解決しない (同名の別の定義・参照も当たる)
- tuple の pattern の束縛 (`let (a, b): (Int, String) = …`) は def の見出しに型注釈を付けない (要素の位置で型を割り当てない)
- 1 つの宣言の 2 つ目以降の束縛 (`var p = 0,\n q = ""` の q) の def は、宣言の先頭の行を指す
- refs の囲む単位は行の範囲でも引く。型の宣言と同じ行に関数があると (`struct S: P { func g() {`)、継承節・where 句の参照もその関数の中と出る
- 束縛の列の `,` の直前にコメントがあると (`let a /* c */, b: String`)、2 つ目以降の束縛を def で引けない
- 関連値にラベルのある case (`case foo(x: Int)`) は `def foo(x:)` で引けない (`def foo` で引く)。macro の宣言は def で引けない
- 局所の名前の有効範囲を見ない (宣言がどこかにあれば、単位の中の同じ名前を全部局所として扱う)。誤検知の向き
- test のファイルの判定はパスの慣習 (`Tests` / `…Tests` / `…Tests.swift`) と `XCTestCase` の継承。`App/ABTests/` のような製品のディレクトリも test と読む。設定にtest/prod判定の上書きはなく、パス除外は誤分類自体を直すものではない
- クロージャの鍵は、囲む単位の中での出現の順番。前にクロージャを足すと後ろの鍵がずれる (除外の設定は順番を入れない釘で指すので、ずれでは外れない)
- クロージャの表示は囲む単位の名前を挟んだ `型.関数.{closure}` (入れ子は `型.関数.{closure}.{closure}`、格納 property の初期値は `型.property.{closure}`)。
  同じ関数の中の 2 つのクロージャ・多重定義の関数のクロージャは同じ表示になるので、行番号で区別する。関数の中の型の中のクロージャには外の関数の名前を付けない。
  表示だけの名前で、鍵・`name` (json) は `{closure}` のまま (json の `owner_name` に囲む単位の名前)。`callers` の呼び出し元 (`caller`) も同じ表示になる
- 読めない構文があるファイルでは、関数が単位ごと取り出せない場合がある。report は `files_with_errors`、gateはbase/head別の失敗fileと `analysis_incomplete` を出す。抽出できた変更単位は `unreadable_changed_units` にも残す。追跡済みの関数を `func broken(` に書き換えて `changed_units=0` になってもfileの解析失敗を通知し、通常modeはrc契約を維持、-strict-analysisではrc=2にする。`def` / `refs` / `index` / `callers` もtext/jsonのstderrへ解析失敗の件数と引用符付きの先頭3ファイル（残りは件数）を返す。hitと0件の両方で通知し、抽出単位0でも消さない。stdoutのJSON schema・従来の0件rcは維持し、通知だけで失敗にはしない。context/evidenceは従来どおりstdoutの要約で通知する。いずれも検索0件を不存在の証明に使わない
- Go: 型を解決しないので、呼び出しが処理か値の変換かを構文で決める。組み込みの変換・`len` などと型リテラルへの変換 (`[]byte(s)`) だけを値とし、
  ほかの呼び出しは全部処理として L3 でも畳まない。Go は公開の関数も大文字で始まるので、Swift のように大文字を型の初期化とみなさない
  (`UserID(s)` のような型への変換も処理に数える。見逃しの向き)
- Go: 複合リテラルの `Key: v` の Key が識別子なら field 名として字面で残す。map のリテラルのキーに局所の変数を書いても番号にならない (見逃しの向き)。
  逆に、局所の変数 `k` をキーにした `map[string]int{k: 1}` と、パッケージの定数 `k` をキーにした同じ形は L1 で一致する (誤検知の向き。型を解決しないと分けられない)
- Go: 型引数の制約 (`[T int]` / `[T string]`) と引数の型は、Swift と同じく本体のハッシュに入れない
- Go: 型の位置は go/printer で書き出した字面で比べる (改行の位置・field のコメントは字面に入らず、tag は field の位置に残る)。
  意味が同じでも書き方が違う型 (生の tag `` `json:"x"` `` と解釈済みの `"json:\"x\""`・余分な括弧 `[(N)]int`) は別の字面になる (見逃しの向き)
- Go: 構文の誤りは、トップレベルの宣言の始まりから次の宣言の手前までで数える (閉じないまま EOF で終わる関数を拾うため)。
  関数の後ろに余分な `}` があると、その関数も読めない単位になる (gate では止めずに「確かめられなかった」に数える)
- Go: testify の suite が呼ぶ `Test…` メソッドは、test のファイルにある receiver つきの引数の無い `Test…` で判定する。
  suite でない型のそういうメソッドも除き、`Testable()` のような後ろが小文字のメソッドは (testify は呼ぶが) 単位に残す
- Go: `testdata` の下は test のファイルとして読む。呼び出しの照合 (`callers`・index の呼び出しの数) は名前と引数の数だけで、可変長の引数の呼び出しは数が合わず数えない
- gate は組の数が増えたかしか見ないので、次の 2 つは正しい作業でも止まる (寄せる先・揃える先が前から組の外にいたため):
  既にある包み (wrapper) へ写しを寄せたら、包みと残った呼び出し側の形が一致した / 片方の写しにだけ入っていた修正を、もう片方にも入れて形を揃えた。
  どちらも出力の「同じ形」を見て意図した重複なら、除外の設定 (issue 906) で外す
- 比べる下限をまたぐ変化は鍵でしか前の単位と結び付けない。小さいクロージャの鍵の位置に大きい新しい写しが入る
  (前に足して後ろの番号がずれ、元の小さいクロージャも書き換えた) と通り、下限より小さかった写しが一緒に育ちながら
  改名・番号のずれを伴うと止まる。DualNoteApp の first-parent 500 commit ではどちらも出ていない
- 写しを移した先と新しい写しが、名前も同じで本体も同じだと、どちらを「新しい」と名指しするかは並びで決まる (rc は変わらない)
- 除外 (許可・パスの除外) は、組の中でこの変更で変わった単位 (新しい・移した・改名した・本体を変えた) が全部名指ししない単位のときだけ違反を消す。
  許可した写し・パスの除外の下の新しい写しと同じ commit で、同じ組の外の単位を移した・修正した (同じ修正・無関係な修正) と、外の単位を止める。
  出どころは名前・並びで当てるので、名指しで rc を決めると、取り違えで外の写しが通る (敵対的レビューで 4 周続けて出た)。保守側に倒している

## 作り

| package | 役割 |
|---|---|
| `internal/tree` | 言語に依らない単位と本体の木 (役割: 名前の参照・局所の宣言・メンバー・ラベル・型・リテラル・トークン) |
| `internal/norm` | 段 (L1 / L2 / L3) ごとの正規化とハッシュ |
| `internal/group` | Type-2 の組分け (細かい段と同じメンバーの組は粗い段で重ねない・囲む単位が同じ組のクロージャは出さない) |
| `internal/repo` | git でのファイルの列挙と起点の正規化。作業ツリーと commit (git のオブジェクトから読む) を同じ `Source` として読む |
| `internal/lang` | adapter が満たす形 (`Adapter` / `Parser`) と、adapter がコマンドへ返す結果の型 |
| `internal/langs` | adapter の一覧と、拡張子でどの adapter が読むかの選び方 (コマンドはここだけを見る) |
| `internal/lang/swift` | Swift の adapter (tree-sitter の node の種類の分類表・単位の取り出し・test の判定) |
| `internal/lang/golang` | Go の adapter (標準の go/ast で読む。`TestConvKnowsEveryNode` が標準ライブラリの実物で、知らない節が無いかを見る) |
| `internal/report` | text / JSON / Xcode の出力 (report と gate) |
| `internal/gate` | gate の判定 (変わった単位・出どころ・違反になった最初の段) |
| `internal/exclude` | `.dupscan.json` の読み込みと検査・釘・除外の効き方 (report と gate が同じ関数を呼ぶ)・効いていない除外 |

- 文法は `github.com/alex-pinkus/tree-sitter-swift` の `with-generated-files` branch を pseudo-version で固定する。上げたら `TestEveryKindIsClassified` (文法の全部の名前つきの種類が分類表にあるか) を通す
- `third_party/tree-sitter-swift-teststub` は、文法の module のテストが誤った import path で参照する package の空の代わり (go.mod の replace のコメント)
- cgo で文法の C のソース (`parser.c`) をコンパイルする。`TOKEN_COUNT` の再定義の警告が 1 つ出るが動作には影響しない

探索用の `index` / `def` / `refs` / `callers` / `context` / `evidence` は、scope全体の構文解析とmetadataを保ちながら、重複比較用の本体変換・L1〜L3正規化を省く。`index -file` の呼び出し数もscope全体で数える。解析cacheは持たず、編集・追加・削除を毎回読み直す。`report` / `gate` / `near` / `explain` は従来どおり完全な本体で全段を比較する（issue 924）。

### 一括で掘る入口 (`dupscan sweep`)

初めて入る module の重複を、1 回の走査で 4 つの見方から出す。節ごとに順位は別で、節の中を上から見る。
1. 既定の `report` (下限 20 節・L1/L2)。
2. 下限を 12 節に下げ、`L1m` も見て、1 に無い組だけ (メンバーが全部 `testdata` の下の組は prod/test でなく `testdata` と出す。小さい単位が多く雑音も混ざる。`dupscan:allow` で静かにできる)。
3. 部分木が似ている対 (`near -similar` を全単位の総当たりにしたもの。Dice 0.6 以上・12 節以上・組に入る対と入れ子は除く)。単位が `-similar-max-units` (既定 3000) を超えたら比べず、そう書く。同一動作・再利用は未証明。
4. 名前の束 (下限未満の関数が、同じ dir に同じ先頭の語で 4 個以上。メソッド・test・`new`/`init`・述語 `is`/`has` などは除く)。末尾の数字だけが違う名前 (`contains` / `contains2`) は `連番の名前` として出す。同じ dir に同名で受け口の型が 2 種類以上のメソッドが 3 個以上 (節の数が 2 倍以内) あれば `take (同名のメソッド)` として別に出す。写しではなく「同じ種類の仕事を各所で書いている」手がかり。

text だけ。実測は `src/glogx` で 0.27 秒、`src` 全体 (単位 8705) で 0.2 秒 (3 は上限で飛ばした)。

### 部分木の類似候補（実験用）

`dupscan near -similar -limit 5 Sources/A.swift:helper` は1対象と同言語の単位を比較する。L1の部分木（3 nodes以上）の重み付き多重集合Diceで候補を並べ、既定閾値は暫定0.4（`-similar-threshold`で変更可）。局所名は既存L1と同じ番号、callee/member/type/literalは保持するが、戻り型は比較しない。`-min-nodes`/`-levels`は既定の完全一致経路用で、この実験では上記の部分木条件を使う。返却は1〜50件、総候補数・返却数・比較/skip数・parse error件数を表示し、JSONも同じ件数上限を守る。

これは全体の構文の重なりであり、部分重複の網羅、同一動作、再利用可能性を保証しない。局所宣言の挿入で番号がずれる場合や、深い/反復した部分木の重みが偏る場合がある。祖先・子孫は除外する。位置は単位のみで、共有部分の正確なsource spanは返さない。短いtargetに3 nodes以上の部分木がなければその旨を示す。clone用除外は適用せず、候補をgate違反にはしない。既定near/report/gateの判定は変えない。

## 利用の測定

`scripts/dupscan_usage_report.py` が Claude Code の transcript (`~/.claude/projects/*/*.jsonl`) から、識別子らしい `grep` / `rg` と dupscan の探索 (`gate` は除く) の回数・言語別・hook の案内の直後 3 手以内の探索・`index` の直後の `Read` の範囲指定を数える (`--days N` `--split ISO8601` `--by-day` `--json`)。dupscan 自身を開発しているセッションは除く。
案内を出したことや利用が増えたことを効果とせず、「grep が dupscan に替わったか」を前後で比べる。数え方は `tests/scripts/test_dupscan_usage_report.sh` が固定している。
