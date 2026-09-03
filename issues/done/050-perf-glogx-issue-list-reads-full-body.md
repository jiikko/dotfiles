# glogx: issue 一覧の生成で全 issue の本文を最後まで読んでいる (checkbox 集計のため)

起票日: 2026-08-14
種別: perf
優先度: **P2** (起動・再スキャン時の I/O。フレーム描画ではないので P1 ではない)

## 観測した事実

`scanIssues` (`src/glogx/issues_view.go` の `scanIssues`) は、探索で見つかった
**全 issue に対して `Issue.LoadMeta` を呼ぶ**。

`LoadMeta` (`src/glogx/issues/parse.go`) は 3 つを取る:

1. front matter の `status:` — ファイル先頭
2. H1 のタイトル — ほぼ先頭
3. **チェックボックスの数 (`Boxes` / `Checked`)** — 本文全体に散らばる

3 のために `bufio.Scanner` が **必ず EOF まで走る**。1・2 は先頭数行で確定するので、
本文の残り全部は checkbox 計数のためだけに読んでいる。issue 件数 × ファイルサイズ分の
read と正規表現マッチが、一覧を出すたび (起動・外部編集の検知後の再スキャン) に走る。

`LoadMeta` の docstring は「一覧の表示に必要な行だけ**遅延で**呼ぶ」と書いているが、
呼び出し元は scan 時の一括ループで、可視行だけの遅延にはなっていない (**記述の乖離**)。

## 対応方針

**一覧では checkbox を数えない。詳細 (本文を読む時) にだけ数える。**

一覧の進捗表示は「あると便利」程度で、そのために全件の全文を読むのは釣り合わない。
一方、詳細表示は `ReadBody` で全文を既にメモリに載せているので、そこで数えれば
**追加の I/O はゼロ**。

### 変更点

- `issues/parse.go`
  - `Issue.Boxes` / `Issue.Checked` / `checkboxRe` / `Issue.Progress()` を削除
  - `LoadMeta`: front matter を抜けて H1 を取れた時点で `break` して打ち切る。
    H1 が無いファイルは打ち切り条件が成立せず EOF まで読む (行数上限は設けない。
    実測の最大 issue は 43KB で、素朴な実装を保つ方を採る)
  - docstring を実態に直す (「scan 時に全件分読む」/ 打ち切り条件)
- `issues/body.go`
  - `Body` に `Progress() string` を追加し、`src` から checkbox を数える
    (`Lines` と同じく遅延 + 結果キャッシュ。整形とは独立に呼べること)
- `issues_view.go`
  - `rowLine` (一覧行の右端): 進捗の表示をやめる。空いた幅はタイトルへ回る
  - `bodyHeadLines` (詳細ヘッダ): `v.open.Progress()` → 開いている `Body` の `Progress()`

### テスト

- `issues/parse_test.go`: checkbox 計数のテストを `body_test.go` 側へ移す
  (対象が `Issue` から `Body` へ移るため)。期待値は**フィクスチャを手で書いて
  リテラルで書く** (現行 `TestLoadMetaReadsTitleFrontMatterAndCheckboxes` と同じ流儀。
  production と同じ regexp から期待値を作る自己言及にしない)
- `issues/parse_test.go`: `LoadMeta` が **打ち切っている** ことを主張するテストを足す。
  観測手段は `sc.Buffer` の上限を使う (下記)
- `issues_watch_test.go` の `Progress()` を使った観測点 (再スキャンでメタデータが
  作り直されることの確認) を `Title` へ差し替える。同ファイルの `writeIssue` が書く
  `- [x]` 行は誰も読まなくなるので同時に落とす (死んだフィクスチャを残さない)
- 変異検証: `break` を外すと打ち切りテストが red、`Body.Progress` の集計条件を
  壊すと計数テストが red になることを確認する

#### 打ち切りをどう観測するか (seam を足さずに済む)

「H1 の後ろに壊れた内容を置いて、読まずに済んでいることを見る」だけでは **vacuous になる**。
`h1Re` / checkbox の regexp はゴミバイトでエラーにならないので、EOF まで読んでも
外から見える差が出ず、`break` を外しても green のままになる。時間で測るのは flaky。

代わりに **`bufio.Scanner` のトークン上限を観測点にする**。`LoadMeta` は
`sc.Buffer(64KB, 1MB)` (`parse.go` の `sc.Buffer` 行) を張って `sc.Err()` を返すので:

- H1 の**後ろ**に改行なしの 2MB の 1 行を置く → EOF まで読む実装は
  `bufio.Scanner: token too long` を返す
- H1 で打ち切る実装はその行に到達しないので `err == nil`

**実測済み (2026-08-14、現行コードで確認)**: 上記フィクスチャで現行 `LoadMeta` は
`title="099 feat: probe" err=bufio.Scanner: token too long` を返す。決定論的で
タイミングに依存せず、`break` を外すと必ず red になる。

#### 既存テストで足りているもの (追加不要)

- `rowLine` の幅算術: `issues_view_test.go` の `TestIssuesViewLinesAlwaysExactlyPageRows`
  が width を振って全行の枠越えを検査済み。進捗列を外して幅が余る変更はここで守られる

## やらないこと

- 一覧の進捗表示を「本文を読まずに近似する」ような細工はしない (front matter に
  進捗を書かせる等)。表示を消すのが一番単純で、嘘をつかない
- `LoadMeta` を真の遅延 (可視行だけ) にする改修はここでは扱わない。打ち切りで
  読む量が先頭数行になれば、遅延化の動機はほぼ消える。必要になったら別 issue
- **コードフェンス内の `- [ ]` を除外しない**。現行の `checkboxRe` はフェンス非対応で
  例示のチェックボックスも数えている。`Body` へ移しても同じ挙動のままにする
  (挙動を変えない移動に限る。フェンス対応は別 issue)
- 同様に `h1Re` のフェンス誤検出 (フェンス内の `# ...` を H1 と誤認する実例が
  他 repo に 1 件) も本 issue では直さない。打ち切りの有無で結果は変わらない

## 未検証として残すこと

- **打ち切りによる実速度の改善幅は測っていない**。`scanIssues` 内で本文を読むのは
  `LoadMeta` だけ (`Scan` / `FindDirs` は `os.ReadDir` のみ、`issuesFingerprint` は
  `os.Stat` のみ) なので構造的には支配的な I/O だが、体感差の数値主張はしない。
  必要なら実装時に before/after を測って追記する

## 打ち切りが `status:` を取り落とさない理由 (敵対レビューで確認)

`inFront` が true になるのは `firstLine` ゲートの下だけで (`parse.go` の
`case firstLine && ...`)、H1 と checkbox を見る default 分岐は `!inFront` のときだけ走る。
つまり front matter は必ずファイル先頭ブロックとして閉じてから H1 判定に入るので、
「H1 が先に来て `status:` を取り落とす」順序は構造的に作れない。

`v.body` は `openIssue` / 再読み込みのどちらでも `ReadBody()` → `NewBody` で作り直される
(issues_view.go の `ReadBody` 呼び出し 2 箇所) ので、`v.body.Progress()` を都度参照すれば
外部編集後に古い進捗を握り続ける経路も無い。

## 関連

- `Progress()` の docstring にある「ここから『着手中』を導出しない」の警告
  (実測で done/ にあるのに 0/N のファイルが 36 件) は、`Body.Progress` へ移す


---

# 対応の記録 (2026-08-14)

## 実測 (issue が「未検証として残す」としていた分)

実 repo の issues/ (50 件 / 395,541 バイト) で、走査ループを旧 (EOF まで読む) / 新 (H1 で打ち切る)
で比較 (count=6・min):

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| EOF まで読む (旧) | 1,546,027 | 3,787,164 | **4,155** |
| H1 で打ち切る (新) | 753,285 | 3,350,826 | **310** |

**約 2.05x・確保回数 −93%**。R2 が別 worktree で独立に 3.1x、R3 が 2.89x を再現 (コーパスと
機械差)。🚨 **倍率は行数に比例する**ので単一の数字で語れない。R3 のバイト数を揃えた実測:

| コーパス | 行数 | 倍率 |
|---|---|---|
| 実 repo | 5,240 | 2.89x |
| 短い行が多い | 10,300 | **3.98x** |
| 長い行が少ない | 600 | **1.86x** |
| H1 が無い (打ち切り不発) | 10,300 | **1.68x** |

最悪形でも 1.86x なので方向は堅牢。打ち切りが不発 (H1 無し) でも 1.68x 残るのは
checkbox 正規表現を全行に掛けるのをやめた分。

🚨 **削れるのは CPU で read 量ではない**。`sc.Buffer(64KB, 1MB)` なので 64KB 以下のファイルは
初回 Read で全体がバッファに載る (実測: 43KB で旧 2 read / 新 1 read・どちらも 44,047 バイト)。
走査対象の 738 ファイルに 64KB 超は **0 件** なので、「64KB 超では read も減る」は
正しいが現実の入力に存在しない分岐。

## 敵対的レビュー 3 観点

### R1 — **新設したガードが vacuous だった (P1)。反映済み**

`TestIssuesViewerReloadsAfterEditorCloses` で「一覧に進捗が出ないこと」を assert したが、
同時にフィクスチャから `- [x] やった` を**削っていた**ため、進捗を出す実装に戻しても
`1/1` は生成されず**どんな退行でも赤にならない**状態だった。私の変異検証も偽で、
`" 1/1"` を文字列で埋め込んだだけだったので red になっていた。

反映: フィクスチャに checkbox を戻し、**本物の機構** (`ReadBody()` → `Progress()`) を
戻す変異で red を確認。あわせて「詳細ヘッダには出る」assert を追加した
(一覧から消したぶん表示カバレッジが 1 → 0 に減っていた)。

R1 が指摘した**コメントの偽の断定** 3 件も訂正:

- `Body.Progress` は **front matter 内の checkbox も数える** (旧 `Issue.Progress` は
  `!inFront` でしか数えなかった。R1 が 426 件の差分入力を提示)。「挙動を変えていない」を撤回し、
  差を受け入れる理由 (この repo の 51 件すべて front matter 無し / 規則を 2 箇所に複製しない) を明記
- 「issue 件数 × ファイルサイズ分の read が走る」→ 偽。上記のとおり read 量は変わらない
- 「v.body は作り直されるので古い進捗を握らない」→ 偽。`reloadAfterEdit` は
  `err == nil` のときだけ差し替えるので、取り直し失敗時は本文も進捗も stale
- `PlainLine` の関門を外しても全テスト green だった → ANSI / NUL 始まりのケースを追加

### R2 — 悪化なし

削除シンボルの残存参照 0 件 / 一覧行の幅算術は ASCII・CJK ともに width 1..1000 で
**旧と同一の失敗集合のみ** (width 1-2 の既存バグで本変更由来でない) / `Body.Progress` は
`Body.Lines` の約 2% のコスト / 懸念していた `strings.Lines` は逆に `bufio.Scanner` より
速く確保も少ない (43KB で 31.5µs/102 allocs vs 44.3µs/527 allocs) / `BenchmarkModelInit200` 非干渉。

### R3 — **打ち切りのテストが単独編集で無言に死ぬ (P1)。反映済み**

`TestLoadMetaStopsAtH1` の識別力は「フィクスチャ 2MB > `sc.Buffer` の上限 1MB」だけに
寄生していて、テストは 1MB をどこからも参照していなかった。R3 の実測:

| 変異 | 結果 |
|---|---|
| 打ち切りの `return` を削除 | red (今日の識別力はある) |
| 上記 + `sc.Buffer` の上限を 4MB に | **全 green** |
| 上記 + 末尾 `sc.Err()` → `nil` | **全 green** |

反映: 上限を `loadMetaMaxLine` const に出し、テストが `loadMetaMaxLine+1` バイトで
フィクスチャを組むようにした (上限を変えるとフィクスチャが追従する)。

R3 の他の指摘も反映:

- 「行数上限は設けない」が無テストだった。上限 5 行の変異が全 green で、実データに
  **H1 が 239 行目**にある 559 行のファイルが存在する (別 repo) → `TestLoadMetaFindsH1DeepInFile` を追加
- 詳細ヘッダの assert がフィクスチャ `1/1` で operand swap に対称だった
  (`Itoa(boxes)+"/"+Itoa(checked)` の変異が tui 側で green) → フィクスチャを
  済み 1 / 未 1 にして期待値を `1/2` にした

## 未確認リスク / 残した穴 (正直に記録)

- **「scan 中に全文を読まない」という perf 不変条件そのものを守るテストが無い**。R3 が
  `scanIssues` に `_, _ = iss.ReadBody()` を足す変異 (表示は変えずに全文読みを復活) で
  全 green を実証した。読んだバイト数を数える seam が要るので本 issue では入れていない。
  `tests/glogx/bench_glogx.sh` / `bench_budgets.ci` に issue scan の metric が無いのも同根
- `TestLoadMetaStopsAtH1` の観測点 (`sc.Err()`) は **production が捨てている値**
  (`_ = iss.LoadMeta()`)。打ち切り除去では確実に red になるが、エラーを飲む refactor には無力
- 打ち切り後は 64KB バッファの確保が支配的。R3 の記録では
  `sc.Buffer(4KB, ...)` にするとさらに 1.66x・B/op −93% になる (本 issue の射程外)
- `Body.Progress` のキャッシュ検証は `b.src` を直接書き換える形で、production では
  `src` は不変なので観測方法が実態と少しずれている (同じ `Body` には `renders` counter seam の
  慣行があり、そちらに倣っていない)

## 見た目の変更 (ユーザーの目視確認に委ねる)

**一覧から進捗列 (`1/1` 等) が消え、その幅がタイトルに回る。** 詳細を開いたときは従来どおり出る。
TUI の見た目は自動検証できないので、ここは目視で確認してほしい。
