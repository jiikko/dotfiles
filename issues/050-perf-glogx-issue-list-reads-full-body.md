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
