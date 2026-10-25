# dupscan

repo の中で、本体が同じ形のメソッド (関数・初期化子・クロージャ) を構文木で見つけて組にする CLI (`bin/dupscan`)。
Claude Code にコードを書かせている間に、似たメソッドが量産されるのを見つける。最初の言語は Swift。

設計と狙いは [`docs/dupscan-overview.md`](../../docs/dupscan-overview.md)、同じと判定する方法と誤検知は
[`docs/dupscan-detection.md`](../../docs/dupscan-detection.md)、要件の正本は epic 890 (issue 891)。

## 使い方

```sh
bin/dupscan                      # cwd の repo 全体。重複が無ければ 1 行だけ
bin/dupscan macOS Shared         # 起点を repo の中のディレクトリにする (大文字小文字は git が正規化する)
bin/dupscan -format json .       # 全部の組を JSON で
bin/dupscan -format xcode .      # Xcode / SwiftLint と同じ `ファイル:行:列: warning:` の形式
bin/dupscan -limit 0 -v .        # text で全部の組と、読めない構文の単位の一覧

bin/dupscan gate                 # upstream との merge-base から作業ツリーまでに新しく増えた重複。無ければ 1 行
bin/dupscan gate -base origin/master .        # base を明示する (CI の push なら push の前の commit、PR なら対象のブランチ)
bin/dupscan gate -base HEAD~1 -head HEAD .    # 2 つの commit を比べる (過去の commit の流し直し。checkout しない)

bin/dupscan near Sources/A.swift:location     # この単位と同じ形の単位 (一致した最も細かい段で)。起点を書かなければ repo 全体と比べる
bin/dupscan near Sources/A.swift:42 Sources   # 行で指す (その行を含む単位のうち最も後で始まるもの)。起点を絞る
bin/dupscan explain Sources/A.swift:location  # 段ごとに潰した形・ハッシュと、比べない理由 (なぜ同じ / 違うと判定したか)

bin/dupscan index -returns StorageLocation    # 書く前に引く: 戻り値の型の一部で既にある定義を (1 行 1 件・上位 20 件)
bin/dupscan index -name location -param-type String   # 名前の一部・引数の型の一部 (全部の条件に当たるものだけ)
bin/dupscan index -doc bucket                 # doc comment (/// か /** */) の最初の行の語
bin/dupscan index -file Sources/A.swift       # そのファイルの単位を行の順に (outline。件数で切らない)
bin/dupscan callers 'make(x:_:)'              # 呼び出しの箇所と、それを囲む単位 (名前だけでも引ける)
```

`callers` は呼び出しの箇所を `file:line 呼び出し元 [test] | その行` で出す (コメント・文字列の中は数えない。テスト関数の中も数え、呼び出し元は「(単位の外)」)。
型を解決しないので、同名の別のメソッド・別の型の初期化子の呼び出しも当たる (近似)。ラベルを書けば数と順で絞る。
後置クロージャのある呼び出しは括弧の中のラベルが先頭に一致すれば当てる。flag は名前の前に書く (`callers -format json make`)。

`index` は名前の付いた単位 (関数・初期化子・computed property・subscript など。クロージャとテスト関数は除く) を
`file:line 型.署名 [shared,test,private] (呼び出し ~N) // doc の最初の行` の 1 行で出す。grep と違い呼び出し側の行・コメント・文字列は混ざらない。
並べ方は名前の一致の強さ (同じ > 前方 > 一部) → 共通の置き場所 (`.dupscan.json` の `"shared": ["Tests/SharedTestSupport/**"]` のような glob。`[shared]` と出る) を前 → prod を test より前 → private / fileprivate でないもの (そのまま使える) を前 → 呼び出しの多いもの → 場所。
呼び出しの数 (`~` は近似の印) は `callers` と同じ当て方 (名前と引数ラベル。初期化子は型の名前) で関数と初期化子だけ数える。既定値のある引数を省いた呼び出しは数えない。
型は字面の一部で引く (`Tag` で `[Tag]` も当たる。型は解決しないので、typealias・同名の別の型は区別しない)。
キャッシュは持たず、引くたびに読む (DualNoteApp の 1130 単位で 0.5 秒。issue 897)。`--check` はまだ無い。

`near` は report・gate と同じ比べ方 (`group.Eligible` と段のハッシュ) で、指した単位と同じハッシュの単位を出す。指した単位そのものは出さない。
組の作り方 (被った組・入れ子のクロージャの組を外す) は通さないので、クロージャを指してもその一致が出る。
本体が局所の名前を参照しない単位どうしの L3 の一致は、report・gate では組にしないので、中身を見た一致と分けて出す。
組の除外とパスの除外は、指した単位と相手の 2 人の組に report と同じ関数で掛け、外した数を 1 行目に出す。名前で 2 つ以上に当たれば候補の行を並べて rc=2。
`gate` を手で試すなら、既にある関数の写しをわざと足してから `bin/dupscan gate` を回すと、`make test` で落ちるときと同じ違反が出る。

`gate` の終了コード: 0 = 止める段の新しい重複なし / 1 = あり / 2 = 判定不能 (base が無い・読むファイルが 0 件など)。
`-fail-levels` (既定 `L1,L2`) の段の違反だけで止め、ほかの段 (既定では L3) は報告だけ。セルフレビューと make test・CI で回す。

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
- 読めない構文がある変わった単位は確かめられないので一覧に出すが、rc は変えない。base で読めなかった単位が読めるようになると、本体が同じでも新しい写しに見えることがある
- base が HEAD と同じ (CI で `-base` を付け忘れた疑い) ときは、1 行の出力に注記する
- 作業ツリーを head にしたとき、ignore されていない untracked から読んだファイルの数を 1 行目に出す (`-v` と json の `untracked_files` に一覧)。
  CI の checkout には無いので、手元と CI で結果が違ったときの手がかりになる

| flag | 既定 | 意味 |
|---|---|---|
| `-format` | `text` | `text` / `json` / `xcode` |
| `-levels` | `L1,L2,L3` | 組を出す段 |
| `-min-nodes` | 10 | これより節の少ない関数・初期化子は比べない |
| `-min-closure-nodes` | 20 | これより節の少ないクロージャは比べない |
| `-limit` | 20 | text で出す組の上限 (0 は全部)。Claude が何度も回すので短くしている |
| `-v` | false | 読めない構文の単位・除外の一覧・各組の「除外の 1 行」を出す |
| `-no-exclude` | false | `.dupscan.json` の除外を外して回す (除外が正しいかを見直すため。形の誤りは rc=2 のまま) |
| `-record` | (なし) | gate だけ。1 回ごとの結果 (違反・除外・許可) を、時刻を添えた 1 行の JSON でこのファイルに追記する (回をまたいで数えるため)。書けなければ rc=2 |
| `-fail-new-exclusions` | false | gate だけ。base から新しく足された除外か、パスの除外で通した変わった単位があれば rc=1 (CI で人の目に届けるため) |

下限の既定値は仮 (issue 898 で obaket と DualNoteApp に当てて決める)。

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
  `dupscan` が無ければ rc=2 で止まる)。**YAML は使う側の repo の Actions で走らせていない (未検証)**。最初に入れる repo で 1 回、写しを足した PR で落ちることを見る
- `-fail-new-exclusions` は CI でだけ付ける (除外を足した PR を人の目に届ける。手元のセルフレビューでは一覧を見て報告に書く)
- cgo で文法の C のソースを build するので、runner に C compiler が要る (GitHub の macOS / Ubuntu の runner には入っている)

### 除外の設定 (`.dupscan.json`)

誤検知・意図して分けている写しは、repo の root の `.dupscan.json` で外す (設計は issue 906)。report の json の各組と text の `-v`、
gate の違反の json と `-v` に出る「除外の 1 行」に `kind` (`false-positive` / `intentional`) と `reason` を埋めて貼る。

```json
{"version": 1, "exclude": [
  {"paths": ["Generated/**"], "kind": "false-positive", "reason": "生成コード"},
  {"paths": ["**/*Tests.swift"], "levels": ["L3"], "kind": "false-positive", "reason": "テストの表の形が似ているだけ"},
  {"group": ["<釘>", "<釘>"], "level": "L2", "kind": "intentional", "reason": "iOS と macOS で分けておく"},
  {"unit": "<釘>", "kind": "intentional", "reason": "寄せない理由"}
]}
```

- 釘は `<鍵 (クロージャの順番と多重定義の #n を除く)>@<本体の L1 のハッシュの先頭 12 文字>`。行がずれても効き、本体を直すと外れる。
  1 つの釘が 2 つ以上の単位に当たる除外は効かせず「名指しできない除外」に出す
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
- **読めない構文**: tree-sitter が ERROR / MISSING を出した単位は比べず、件数を出す
- 列挙は `git ls-files --cached --others --exclude-standard`。symlink・消えたファイル・submodule の中は読まない
- テスト関数そのもの (Swift Testing の `@Test` / `@Testing.Test`、XCTest の `func test…()`) とその中のクロージャは比べない
- 単位: 関数・初期化子・クロージャ・computed property・subscript・deinit・`didSet` / `willSet`

## 分かっている限界 (誤検知・見逃しの形)

- 局所の名前の有効範囲を見ない (宣言がどこかにあれば、単位の中の同じ名前を全部局所として扱う)。誤検知の向き
- test のファイルの判定はパスの慣習 (`Tests` / `…Tests` / `…Tests.swift`) と `XCTestCase` の継承。`App/ABTests/` のような製品のディレクトリも test と読む (repo ごとの上書きは設定ファイル。issue 906)
- クロージャの鍵は、囲む単位の中での出現の順番。前にクロージャを足すと後ろの鍵がずれる (除外の設定は順番を入れない釘で指すので、ずれでは外れない)
- 読めない構文があるファイルでは、読めない部分に飲み込まれた関数は単位として出てこない。件数は 1 行の出力にも出る
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
| `internal/swift` | Swift の adapter (tree-sitter の node の種類の分類表・単位の取り出し・test の判定) |
| `internal/report` | text / JSON / Xcode の出力 (report と gate) |
| `internal/gate` | gate の判定 (変わった単位・出どころ・違反になった最初の段) |
| `internal/exclude` | `.dupscan.json` の読み込みと検査・釘・除外の効き方 (report と gate が同じ関数を呼ぶ)・効いていない除外 |

- 文法は `github.com/alex-pinkus/tree-sitter-swift` の `with-generated-files` branch を pseudo-version で固定する。上げたら `TestEveryKindIsClassified` (文法の全部の名前つきの種類が分類表にあるか) を通す
- `third_party/tree-sitter-swift-teststub` は、文法の module のテストが誤った import path で参照する package の空の代わり (go.mod の replace のコメント)
- cgo で文法の C のソース (`parser.c`) をコンパイルする。`TOKEN_COUNT` の再定義の警告が 1 つ出るが動作には影響しない
