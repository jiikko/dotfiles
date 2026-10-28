# 941 (ux): grep から dupscan への案内が、定義検索の典型形で出ない・言語を取り違える

> 🚨 **担当中: air5 (glogx)**（2026-10-28〜）

起票日: 2026-10-28

## 概要

Claude に「メソッド定義を探すときは grep でなく dupscan を使わせる」ための入口は、PreToolUse hook `_claude/hooks/suggest-dupscan-for-symbol-grep.sh` (案内だけを返し、止めない) である。この hook が、Go の定義検索で実際に打たれる形の多くで無音になり、言語の手掛かりがない検索では Go の名前に Swift 専用の `dupscan def` を勧める。案内が届かない・誤る形を測り、直す。

使わせる施策は他にも考えられる (常時ロードへの1行追記、skill の説明、サブエージェントへの伝達) が、まず**現行の入口が実際に何を拾い、何を落とすか**を測った。ここでの測定は dotfiles の 1 repo (`.go` 1258 個・`.swift` 1 個を追跡) で、合成入力を hook に渡した結果である。

## 実測 (2026-10-28、案内の間隔を 0 秒にして hook に直接渡した)

| 入力 | 結果 |
|---|---|
| Bash `grep -rn ReadCookies src/` (言語の手掛かりなし) | 案内あり。ただし **Swift 用** (`dupscan def` / `refs`) |
| Grep tool `pattern=ReadCookies path=src` (同上) | 同じく Swift 用 |
| Bash `grep -rn --include="*.go" ReadCookies src` | Go 用 (`index` / `callers`)。期待どおり |
| Grep tool `glob=*.go` / `type=go pattern="func ReadCookies"` | Go 用。期待どおり |
| Bash `rg "func.*ReadCookies" src` | **無音** |
| Bash `rg -n "func \(w \*Workspace\) ReadCookies" src` | **無音** |
| Grep tool `type=go pattern="func \(w \*Workspace\) ReadCookies"` | **無音** |
| Grep tool `type=go pattern="ReadCookies\|IsMissing"` | **無音** (識別子の alternation) |
| Bash `grep -rn "ListProfiles\|fallbackProfiles" src --include="*.go"` (同日このセッションで実際に打った形) | **無音** |

- Swift 用の案内に従って `dupscan def ReadCookies` を打つと `dupscan def: go のファイル 1258 個は def / refs の対象外` と返り、1 往復を無駄にする。dotfiles が `.swift` を 1 個追跡しているために Swift 扱いになった。Go だけの repo では言語の手掛かりがない検索は無音になるはずだが、これはコードの読みで、**未実測**。
- 無音の形のうち alternation と「dir の中身から言語を推測しない」は hook 冒頭の「検出しない形」に書かれた設計どおりの挙動で、[939](done/939-ux-dupscan-go-grep-nudge.md) の判断である。ここでは設計の誤りとは書かず、**Go の定義検索でよく打たれる形を落とす費用が大きいか**を問い直す。

## 方針 (候補。測ってから採る)

1. **Go の定義検索の形を識別する**: `func (recv) Name` / `type Name` は Go 専用の形なので、末尾の識別子を取り出して Go 用の案内 (`index -name` / `callers`) を返す。`func Name` と `func.*Name` は Swift の宣言語でもある (hook の `DECL` に含まれる) ため言語の手掛かりにならず、**方針 3 で言語を決めた後**に適用する。
2. **識別子だけの alternation** (`A\|B` / `A|B`) は、全部が識別子で 3 つ以内なら先頭の名前で案内し、他の名前も同様に引けると添える。案内文の 1024 byte 上限は守る。
3. **言語の手掛かりがないとき**の決め方: 検索対象の path を `git ls-files -- <path>` で 1 回問う (dir の走査ではなく index への照会)。939 は「dir の内容を走査して言語を推測しない」と決めたが、**理由は 939 に書かれていない**。hook は現状でも `git ls-files -- "*.$language"` を 1 回打っており、追加の照会がそれに何を足すかを書いた上で、遅延を測って採否を決める。参考: [928](928-feat-dupscan-agent-onboarding.md) は準備済みの Go 案内で hook が約 21 ms 増える (86.25→107.54 ms) 実測を持つ。採らないなら、複数言語を追跡する repo では Swift 用の `def` を勧めず、言語を問わず使える `dupscan index -name <名前>` だけを案内する。
4. **常時ロードへの追記 (ユーザー指示 2026-10-28 で実施済み)**: `_claude/CLAUDE.md` に「シンボル検索の使い分け」を足した。「grep の前に必ず dupscan」ではなく、広い名前・不確かな表記・似た関数探しは dupscan、狙い撃ちと文字列は grep という使い分けにした (942 / 943 の実測が根拠)。Swift / Go 以外の repo でも毎セッション載る費用は残る。hook が届かない形への保険で、効果は 921 の測り直しで見る。

## 守ること

- **止めない**。`additionalContext` だけを返し、grep を拒否しない。文字列・コメント・設定の検索は今までどおり無音 (小文字の語・出力を絞るパイプの後ろの grep)。
- 通知の間隔 (既定 900 秒、言語ごとに別 stamp) は変えない。
- hook から dupscan を build / 解析しない (939 と同じ)。

## 受け入れ条件

- [x] 無音だった形のうち、**receiver 付きの宣言 2 形**と**識別子だけの alternation** (Go 指定・`grep` の `\|`・`-E` の `|`・Grep ツール) が案内になる。既存の hook テストは全部維持し、新規を足して 123 passed / 0 failed。
- [ ] **`func.*Name` は Go 優位・Go だけの repo の言語不明では今も無音**。939 の決定 (言語の手掛かりが無い検索は Go だけの repo で黙る) を、測定なしには覆さないため。覆すかの判断は下の「残り」。
- [x] 偽陽性の確認 (下の「測定」)。
- [x] 追加遅延の実測 (下の「測定」)。
- [ ] サブエージェント (Explore など) の grep に hook が届くか。**未確認**。届かないなら事実を [928](928-feat-dupscan-agent-onboarding.md) に書く (本 issue では実装しない)。
- [ ] 効果は 921 の測り直し (2026-11-25 ごろ) に合わせて、Go 指定の grep と Go への dupscan 呼び出しを別集計する。**案内が増えたことを効果と書かない**。

## 進捗 (2026-10-28)

### 実装 (`_claude/hooks/suggest-dupscan-for-symbol-grep.sh`)

- Go のメソッド宣言 `func (w *Workspace) Name` (括弧と `*` のエスケープは問わない) は、言語の手掛かりが無くても Go と読む。Swift を明示した検索・`-i`・Go 以外のファイルだけの検索では読まない。
- 識別子だけの alternation を 3 つまで読む。grep の BRE は `A\|B`、rg / egrep / `-E` / `-P` は `A|B`。rg の `A\|B` と BRE の `A|B` は文字の `|` なので読まない。先頭の名前で案内し、残りは「他の名前も同様に引ける」と添える。
- コマンド文字列の分割を、引用符とバックスラッシュの外にある区切りだけで割る形にした。以前は引用符の中の `|` `;` で割れ、alternation のパターンが壊れていた。引用が閉じない入力 (コメントの `'`・`$'..'`) は以前の単純な分割に倒す。`bash -c '…'` の中身は 1 段だけ展開して読む (引用符を読む分割にしたことで失う、`-c` の中の grep の検出を取り戻すため)。
- 言語の手掛かりが無い検索は、追跡している `.swift` と `.go` の数で決める (一方が他方の 10 倍以上なら少ない方は数えない)。Swift 優位は従来どおり Swift 向け。**Go だけ・Go 優位は黙る (939)**。拮抗するときだけ、Swift 専用の `def` / `refs` を勧めず `index` / `callers` を案内する。dupscan の Go 対応を確かめられないときは、従来どおりの Swift 向けの文に倒す。
- `-tswift` のように値がくっついた短いオプションを、パターンの前に置いても次の語をパターンと読む (既存の不具合。alternation の効き目にも響くため直した)。

### 方針の変更と、その理由

- 当初の方針 3 (言語不明のとき追跡 source の言語で決める。Go だけの repo は Go 向け) は**採らなかった**。939 に「言語の手掛かりが無い検索は Go だけの repo で黙る」を固定するテストがあり、覆すには偽陽性の測定が要るため。
- 当初の実装「Swift と Go を 1 つでも追跡していれば混在」は、実データの replay で誤りと分かった。obaket (Swift 1670・Go 1) の言語不明の案内 54 件が Swift 向けの `def` / `refs` から中立文に落ちたため、優位で決める形に直した。
- `func Name` / `func.*Name` は Swift の宣言語でもあるので、言語の手掛かりにしない (言語を決めた後に適用する)。

### 測定 (直近 7 日の実際の grep / rg 呼び出しを、旧 hook と新 hook に同じ入力で流した)

- 抽出 2,038 件 (重複除去)。cwd が残っていて比較できたのは 1,776 件。同一 1,680・新たに案内 75・案内が消えた 7・文面だけ変化 14。cwd が既に無いもの 262 件は除く。この中にはこのセッション自身の作業のコマンドが数件含まれる。
- **消えた 7 件はすべて dotfiles (Go 優位) の Go の名前への Swift 向けの案内**で、誤案内が無くなった分。
- 新たに案内の 75 件から 30 件を等間隔で抜いて見た。30 件とも Swift の識別子を含む grep で、追加した alternation の対象そのもの (obaket 中心)。うち数件は `elapsed`・`lease` のような小文字の一般語を含む交互で、単独パターンのときから出る規則 (`.swift` を対象にしていれば小文字でも通す) と同じ。分類はパターンの見た目で、意図の確認ではない。
- hook 1 回の遅延 (通知が出るときだけ。既定で 900 秒に 1 回、中央値・15 回): Swift だけの repo 56→67 ms・Swift 優位 59→75 ms・拮抗 62→90 ms。増分は追加の `git ls-files` と、拮抗のときの能力照会 ([928](928-feat-dupscan-agent-onboarding.md) の約 21 ms と同程度)。

### 検証

- 変異 13 本 (ERE の判定・引用符を読む分割・receiver・混在の判定・Go だけの repo の沈黙・alternation の上限・BRE の区切り・Go を除く glob・追加の名前・未閉じの引用の倒し方・値のくっついた短いオプション・能力の確認) を当て、それぞれ該当のテストだけが赤になることを確認した。
- 反証レビュー (別視点 1 体) の指摘を実コードで再現して反映した。引用符の取り違えで後ろの grep を落とす入力、未準備の dupscan で確認の文だけに落ちる情報の減り、`-tswift`。重大な退行は見つからなかった。直した差分の再レビューはしていない。
- `test_suggest_dupscan_for_symbol_grep.sh` の既存の `| grep -q .` (master でも `make test-lint` の `test-pipefail-grep-q` を落としていた) を、意味を変えずに直した。
- `make test-lint` は成功。`tests/claude` の `test_dangling_symlinks.sh` は、この機の `~/.config/nvim/coc-settings.json` が消えたリンク先を指していて落ちる (今回の差分とは無関係)。

### 残り (この issue を閉じない理由)

- Go 優位・Go だけの repo の言語不明の検索 (`rg "func.*Name"` を含む) を案内するか。939 の決定を覆す判断で、次の測定が要る: 同じ replay で、Go の repo の言語不明な PascalCase の grep がシンボルの検索だった割合。割合が高ければ、Go 優位の repo の言語不明は Go 向けにする。
- サブエージェントの grep に hook が届くか (未確認)。
- 921 の測り直し (2026-11-25 ごろ) での効果。
