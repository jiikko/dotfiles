# 941 (ux): grep から dupscan への案内が、定義検索の典型形で出ない・言語を取り違える

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

- [ ] 上の表の無音 4 形 (`rg "func.*Name"`・receiver 付き 2 形・alternation) が Go 用の案内になる。`tests/claude/test_suggest_dupscan_for_symbol_grep.sh` の既存 89 ケースは全部維持する。
- [ ] 偽陽性の確認: 直近 7 日の Go セッションの grep / rg 呼び出しを [921](../890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md) と同じ方法で集計し、新たに案内が出る呼び出しから 30 件を抜き出して「シンボルの定義・参照の検索だった」割合を書く。
- [ ] 採った方針に `git ls-files` の照会が入るなら、hook 1 回あたりの追加遅延を、928 の 21 ms の実測と同じ手順で測って記録する。
- [ ] サブエージェント (Explore など) の grep に hook が届くかを確認する。**現時点では未確認**。届かないなら事実を [928](928-feat-dupscan-agent-onboarding.md) に書く (本 issue では実装しない)。
- [ ] 効果は 921 の測り直し (2026-11-25 ごろ) に合わせて、Go 指定の grep と Go への dupscan 呼び出しを別集計する。**案内が増えたことを効果と書かない**。
