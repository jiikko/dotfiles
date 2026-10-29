# 965 (research): src/treefiler の重複の統合で dupscan を使った所見 (2026-10-29)

起票日: 2026-10-29

## 概要

dupscan-dogfood で「src/treefiler の重複定義を統合する」をこなしたときの所見。寄せた結果は commit
「test(treefiler): テストの helper を testing.TB で受け、手で書いていた同じ手順を寄せる」。
既存の issue との重複照合はしていない (skill dupscan-dogfood の 3 節)。allow-group については 956 の「対応 (F4)」だけ読んだ。
`src/dupscan` は別のセッションが同じ日に触っていた (955 の claim・半日以内の commit) ので、こちらでは直さず記録だけにした。

## 数

- 寄せる前後の組の数 (repo root から `dupscan -format json -limit 0 .` の `groups`): 全体 59 → 55。メンバーに `src/treefiler/` を含む組 5 → 1。
  メンバーが全部 `src/treefiler/` の組は前後とも 0 (既定の下限 20 節では、treefiler の中だけで閉じた写しは無かった)
- `dupscan sweep src/treefiler` の節 5: 3 件 → 0 件 (所要 0.35 秒・出力 4,309 B)
- dupscan を打った回数 14 (sweep 2・root の report 5・index 2・callers 1・`-v` 2・gate 1・capabilities 1。F1 の再現の sweep 3 回は除く)。
  grep に戻した回数 1 (下の F2。1 文の helper の手書きを探すため)。文字列・文書を探す grep (README の allow の節・既存の `dupscan:allow`) は数えない
- 役に立った場面 3: 節 5 がベンチの `hugeFolderModel` と `newVirtPair` の中の `newAt` / `isolateConfig` の手書きを指した (helper が `*testing.T` だけを受けていてベンチから呼べないのが原因だった) /
  root の report が module をまたぐテストの helper の組 5 つを出した (寄せないと判断して理由を残した) / `index -name rite` が treefiler の中のファイル書きの helper 7 個を 1,132 B で並べた

## 所見

### F1 (見逃し・文書にはある): sweep の節 5 は `Test…` 関数の中を見ない

- 場面: `readBody` / `mustWrite` を寄せる先にして、手で同じ手順を書いた所を探した
- 期待: `TestAppendHistoryDoesNotFollowSymlink` (history_test.go) の `os.ReadFile` + `t.Fatal` や、settings_test.go の `b, _ := os.ReadFile(p)` が節 5 に出る
- 実際: 出なかった。小さい入力で再現した: 同じ 2 文を `TestOtherName` に書くと「単位 1」で節 5 は「なし」。名前を `otherName` にすると「単位 3」で
  `readBody の中に otherName の本体の文 2/4 個` が出る。原因は README の「テスト関数そのもの…は比べない」と節 5 の「拾わないもの: Go の `Test…` 関数の中」。
  仕様どおりだが、sweep の節 5 の見出しには出ないので、出力だけを読むと「テストの中に写しは無い」と読む
- 影響: テストの helper を寄せるとき、手書きの写しが一番多いのはテスト関数の中。今回は grep で 6 か所見つけて 6 か所とも寄せた。5 か所は Test 関数の中、1 か所 (`direntries_test.go`) は helper の中で、
  そちらは寄せる先の `mustWrite` の本体が 1 文なので「文 2〜8 個」の範囲外として見ない
- 提案: 節 5 の見出しに「Test 関数の中と、文 1 個の関数は見ない」を足す。テスト関数の中を置き場所として見る (比べる単位にはしない) かは設計の判断があるので、直す側が決める
- 対応 (2026-10-29): commit「feat(dupscan): sweep の節 5 の見出しで、テスト関数の中を見ないと言う (issue 965 F1)」。見出しに「テスト関数の中は見ない」を足し、
  README の「拾わないもの」を Swift (XCTest の `test…`・Swift Testing の `@Test`) まで広げた。「文 1 個の関数は見ない」は見出しの「文 2〜8 個」が既に言っているので足さない。
  テスト関数の中を置き場所として見る変更は、需要が 1 回なので見送り (skill dupscan-dogfood の 1 節に「テストの helper に寄せるときは grep でも探す」を足して補った)

### F2 (黙って効かない): 同じ単位に 2 つ目の `dupscan:allow-group` を書いても効かず、警告も出ない

- 場面: `mustWrite` は L1 (3 人: pro-con・safefs の `write` と自分) と L2 (4 人: 同じ 3 人と subproc の `writeFile`) の 2 つの組に入っている。
  README の「人数に合う段が粗い…と、細かい段の組は残る: その組のメンバーにもう 1 つ allow-group を書く」を読み、`mustWrite` の直前に `allow-group 3` と `allow-group 4` の 2 行を書いた
- 期待: 2 つの組が両方消える。消えないなら `-v` の効いていない除外に出る
- 実際: L1 の組は消え、L2 の組は残った (全体は 59 → 55。treefiler を含む組で残ったのはこの L2 の 1 つ)。`dupscan -v -limit 0 .` の出力に、残った L2 の組の除外の 1 行は出るが、
  `allow-group 4` が効いていない旨 (人数の食い違い・無視した印) は出ない。
  原因: `src/dupscan/internal/lang/golang/golang.go` の `allowComment` は doc のコメントを順に見て、最初に当たった `dupscan:allow` の印で返す (1 単位に 1 つ。先に書いた `allow-group 3` だけが読まれる)。Swift 側の `allowComment` も 1 つだけ受ける
- その後: 効かない行は消した。L2 の組は残した (印を書けるのは L2 の組の別のメンバーで、treefiler の外にしか居ない)
  - 訂正 (2026-10-29): 起票の時点の「その組にだけ居る subproc の `writeFile` に書く必要がある」は誤り。両方の組に入っている別のメンバー
    (safefs・pro-con の `write`) に `allow-group 4` を書いても L2 の組は消える (使い捨ての repo で、L1 の 3 人・L2 の 4 人の組を作って確かめた)
- 提案: 同じ単位の 2 つ目の印を読むか、読まないなら rc=2 か `-v` に「この印は読んでいない」を出す。README の「その組のメンバーにもう 1 つ」が別の単位を指すことを明記する (文脈からそう読めなくはないが、同じ単位に書けると読んだ)
- 対応 (2026-10-29): commit「fix(dupscan): 同じ単位の 2 つ目の許可の印を黙って捨てず rc=2 にする (issue 965 F2)」。2 つ目の印は rc=2 で、誤りに読む行と読まない行を書く
  (Swift は宣言に近い側、Go は上の印を読む)。読む印の規則は作り続ける (gate の base 側で許可が消えないように)。README は「残った組の別のメンバー (両方の組に入っていてもよい) に、その組の人数で書く」に直した。
  変異 4 本・敵対的レビュー (opus) は commit message にある。対応しない P3 (記録のみ): 印で始まる説明文の行も印として読み rc=2 になる (印は行頭で読む既存の規則どおり) /
  1 つの `/* */` の中の 2 つの印は 1 つ目しか読まない (コメントの先頭しか見ない。再提起するなら、その書き方を実際に使っている repo を添える)

### F3 (使い方が分かりにくい): 単位の `dupscan:allow` は、組の全員に付けないと組を消さない

- 場面: module をまたぐ組の treefiler 側のメンバーにだけ `// dupscan:allow intentional …` を書いた
- 実際: 組の数は 59 のまま変わらなかった。README の sweep の節 2 の説明 (「メンバーが全部 `dupscan:allow` の許可済みの組は出さない」「片方だけなら出し続ける」) どおり。
  sweep の冒頭の「単位の直前に `dupscan:allow` を書くと静かになる (組ごとなら…allow-group)」を「自分の側に書けば消える」と読んだのが原因
- その後: `allow-group <人数>` に書き換えて 4 組が消えた
- 提案: 冒頭の 1 行を「組の全員に書くと静かになる。片方の側だけで決めるなら allow-group」にする (読み違いが 1 回の小さい所見)
- 2026-10-29: 別のセッションが issue 966 の commit「feat(dupscan): sweep の冒頭に印で消した組の数と、人数の合わない allow-group を出す」で冒頭の行を直した (組の全員に書くか、1 人だけなら allow-group)。F2 (同じ単位の 2 行目の印) は触っていない

### F4 (雑音): Go の `callers New` が `errors.New` / `rand.New` を拾う

- 場面: `newAt` に寄せられる `New(dir, Options{…})` の呼び出しを探すため、src/treefiler で `dupscan callers -limit 0 New`
- 実際: rc=0・5,745 B・38 行。treefiler の `New` / `filer.New` の呼び出しが 17 行、`errors.New` / `rand.New` が 21 行 (55%)。
  冒頭の注意書き (「名前のみで照合」) どおり
- その後: 出力を目で絞った。`callers -selector` (933) は試していない
- 提案: 無し (名前だけで照合する仕様)
- 2026-10-29 に `-selector` を試した: `-selector filer.New` は package の外からの呼び出しだけ (6 行・1,279 B) を返す。package の中の素の `New(…)` を
  `errors.New` から分ける手段にはならない (`-selector New` は「単純な識別子 chain を渡す」で rc=2)。素の呼び出しだけを選ぶ口が要るかは、同じ困り方が
  もう 1 度出たら考える (1 回きりなので見送り)

## 見送り

- sweep の節 2 の `settings.go` の `items` の表のクロージャ 27 個 (L1m)・`model.go` のキーの割り当てのクロージャ・tfprobe の段のクロージャ: 表の行で、寄せる対象ではない。印も書かなかった (節 2 は gate で落ちない)
- 節 3 の mutex を取ってフィールドを返す getter 3 つ・節 4 の `take` ×5: 型ごとに意味の違う Go の定型
- `intItem` / `boolItem` の set のクロージャ (L1m): 解釈と範囲の検査が違う

## 反証レビュー (2026-10-29、sonnet、読み取りのみ)

事実の主張・数 (組 55) は反証できなかった。P2: F2 の「どちらが読まれるか確かめていない」はソースで確定する → `allowComment` を読んで確かめ、原因として書き直した。
P3: README の「もう 1 つ」を「曖昧」とするのは強めの言い方 → 提案の文を、文脈からは読めることを添えた形に直した。F4 の `-selector` が素の `New(...)` だけを選ぶ用途に足りるかは未検証のまま。

## 進捗

- [x] F1: 見出しと README (commit「feat(dupscan): sweep の節 5 の見出しで、テスト関数の中を見ないと言う (issue 965 F1)」)
- [x] F2: 同じ単位の 2 つ目の印を rc=2 (commit「fix(dupscan): 同じ単位の 2 つ目の許可の印を黙って捨てず rc=2 にする (issue 965 F2)」。CI の src/dupscan は緑)
- [x] F3: 別のセッションが issue 966 で sweep の冒頭行を直した
- [x] F4: `-selector` を試し、見送り (上の F4)
- [x] skill dupscan-dogfood: 1 節に「テストの helper の写しは grep でも探す」「commit の前に repo 全体の検査を回す」を足した (commit「skill(dupscan-dogfood): …」)
- 検証で毎回落ちたもの: ルートの `make test` の `tests/claude/test_dangling_symlinks.sh` (手元の `~/.claude/agents/` に残った、削除済みの agent 5 本への link)。変更と無関係で、`./setup.sh` の再実行で消える (実行していない)
