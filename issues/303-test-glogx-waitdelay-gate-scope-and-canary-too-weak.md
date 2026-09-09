# test: WaitDelay 規律ゲートが実質 1 箇所しか検査しておらず、canary も退行を検出できない

起票日: 2026-09-06
カテゴリ: test
優先度: 中（現状の実害は 0 件。効くのは「次に誰かが書いたとき」）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。2 エージェントが**対立する修正案**を出しているので、
軸の選択はユーザー判断（下の「方針が割れている」節）

対象: `src/glogx/waitdelay_discipline_test.go:TestEveryCommandContextSetsWaitDelay`

---

## 🚨 実測で判明した最重要事実（A/B の選択より上位。2026-09-09）

**このゲートは、守るはずの退行を 1 件も検出できない状態だった**（起票時点の実装）。
軸 A でも軸 B でも直らない、**軸に依存しない欠陥**なので、A/B の議論の前に置く。

| 変異 | 期待 | 起票時点の実測 |
|---|---|---|
| `gitlog.go` の実ガード `cmd.WaitDelay = subproc.WaitDelay` を**削除** | RED | **green**（素通り） |
| その 8 行上のコメント `(理由は subproc.WaitDelay の doc)` から**語だけ削除** | green | **RED** |

原因: 検出は `lookahead=8` の窓に **`WaitDelay` という語が出現するか**しか見ておらず、
`gitlog.go:80` の唯一の強制対象は、`+8`（窓の境界ちょうど）にある**別関数のコメント**に
引っかかって緑になっていた。窓は `}` と `func` 行を跨いでおり、見つけていたのは
「同じ Cmd への代入」ですらない。

つまりゲートは **production の退行ではなくコメントの編集を検出する**、向きの反転した状態だった。
ユーザーが選ぶのは「2 つのゲートのどちらか」ではなく、**この土台の上に何を足すか**である。

→ この欠陥は本 issue で修正済み（下の「実装したもの」）。**軸の選択は未決**。

---

## 何が起きているか

ゲートの検出パターンは **`exec.CommandContext(` の 1 本だけ**（`waitdelay_discipline_test.go:44`）。

### ① `exec.Command(` を 1 件も検査していない

`src/glogx` の非テストコードで `exec.Command(`（ctx 無し）は **7 箇所**ある:

```
open_workspace.go:43, open_workspace.go:56, tui.go:3080,
gitlog.go:68, external_commands.go:285, external_commands.go:374, external_commands.go:379
```

（行番号は 2026-09-09 に `f22abf8b` で採り直した。起票時の 41 / 54 / 3078 はドリフト済み）

いずれも**現状は安全**（前景の対話実行 / 即 detach する `open` / 起動時の同期経路）なので
**実害は 0 件**。しかし誰かが `exec.Command("gh", ...).Output()` を書いた瞬間、
ゲートは**緑のまま通す**。issue 105（13 箇所中 1 箇所が静かに抜けた）と同じ形が再生産される。

### ② canary の下限が実件数と乖離している

canary は `checked == 0` で落ちる形（:70）だが、`subproc/` と `tools/` を除いた
非テストの `exec.CommandContext` は **2 件**しかなく、うち 1 件
（`external_commands.go:293`）は `subproc: no-waitdelay` で `continue` する。
**実質検査しているのは 1 件**。下限 1 の canary は「検査が消えた」をほぼ検出しない
（[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
の「canary の下限は実件数の近くへ置く」）。

### ③ 検査本体と無関係な自作 `itoa` がある

`waitdelay_discipline_test.go:78-90` の自作 `itoa` は `strconv.Itoa` で足りる。
ゲート本体の主張を薄めている（軽微）。

## 🚨 方針が割れている（ユーザー判断が要る）

| 案 | 内容 | 根拠 |
|---|---|---|
| **A: 字句パターンを広げる** | 検出を `exec.Command(` へ（🚨 **括弧付きの `exec.Command(` は `exec.CommandContext(` を含まない**。2026-09-09 実測。この記述のまま実装すると CommandContext の検査が丸ごと消える。`exec.Command`（括弧なし）なら両方に当たるが、ctx 有無で要求が違うので CommandContext を先に判定してフォールスルーさせる必要がある）。`doctor` にも同じ検査を置くか、置かない理由を `runner.go` に書き残す | 変更が小さく、今のゲートの延長。パイプを作らない `Run()` は既存の `subproc: no-waitdelay` 注記でそのまま除外できる |
| **B: import 境界の検査へ移す** | 「`src/glogx` の非テストコードで `os/exec` を import してよいのは `subproc` / `gitlog`（Cmd を受け取って張る側）/ `tea.ExecProcess` へ渡すだけの前景実行に限る」 | 字句ゲートは迂回が無限に出る（[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §8）。import なら書き換えで迂回できず、母集合が「import しているファイル一覧」という数えられる形になる |

**B 側の付随判断**: `doctor` は既に**型**で守れている（`runner.Runner` を `Options.Run` に注入。
非テストの `exec.Command` は `runner/runner.go:24` の 1 箇所だけ）ので、同種テストを置いても
検査対象は 1 箇所でほぼ何も守らない。`runner.go` に「この module の外部実行はここだけ」と
1 行残せば足りる。

## 🚨 A を採る場合の注意（行内注記が嘘になる）

`tui.go:3078` の `exec.Command("nvim", "-R", ...)` は `cmd.Stdin = strings.NewReader(...)` を
使っており、**`*os.File` でないので os/exec が `os.Pipe` と copy goroutine を作る**
（= WaitDelay が想定する構図そのもの）。ここが安全なのは「パイプが無いから」ではなく
**「前景の対話エディタで ctx も無い」**から。この理由のまま `subproc: no-waitdelay` を付けると、
**行内注記が嘘の不変条件を固定する**。

## 受け入れ条件

- [ ] 軸（A / B）を決め、選ばなかった側を却下理由つきでこの issue に残す ← **ユーザー判断待ち**
- [x] canary の下限を実件数の近くへ置く（`seen>=2` / `enforced>=1` + fixture canary を追加）
- [x] **変異検証**: 検査対象のファイルから守りを外して red になることを確認する（軸非依存の分）
- [x] `itoa` を `strconv.Itoa` へ

---

## 実測（2026-09-09 / worktree `wt-303`、起点 `f22abf8b`）

### 本文の主張の検算

| # | 本文の主張 | 実測 | 判定 |
|---|---|---|---|
| 1 | A 案「`exec.Command(` へ広げれば `exec.CommandContext(` は**前方一致で含まれる**」 | `'exec.Command(' in 'exec.CommandContext('` = **False**（`Command` の直後は `C` であって `(` ではない） | **❌ 誤り** |
| 2 | 非テストの `exec.Command(` は 7 箇所 | 7 箇所。ただし行番号が 3 件ずれ（`open_workspace.go` 41→**43** / 54→**56**、`tui.go` 3078→**3080**） | ✅（行番号のみ要修正） |
| 3 | スコープ内の `exec.CommandContext` は 2 件、うち 1 件は注記で `continue` | そのとおり（`gitlog.go:80` / `external_commands.go:293`） | ✅ |
| 4 | 「下限 1 の canary」 | `checked` は注記スキップの**前**に増えるので実値は **2**。ただし真の問題は件数ではなく**マッチャの反転**（上記） | △ 数え方が不正確 |
| 5 | `tui.go:3080` は `Stdin` が `*os.File` でないので `os.Pipe` + copy goroutine が作られる | GOROOT `os/exec/exec.go:525 childStdin` で裏取り済み（`*os.File` でなければ `os.Pipe()` + `io.Copy` goroutine） | ✅ |
| 6 | doctor は型で守られており、非テストの `exec.Command` は `runner/runner.go:24` の 1 箇所だけ | 非テストの `os/exec` importer は `doctor/runner/runner.go` の **1 ファイルのみ**、呼び出しも 24 行目の 1 箇所。ただし実体は `exec.Command` ではなく **`exec.CommandContext`** で、`WaitDelay` も設定済み。さらに doctor は**別 module**（`doctor/go.mod`）なので glogx 側の走査からは原理的に届かない | ✅（B の付随判断は成立） |

**新規の実測（本文に無い）**: `os/exec` を import している非テスト 7 ファイルのうち
**`cli_health.go` と `github.go` の 2 件は `exec.ErrNotFound` / `exec.ExitError` を使うだけで
`Cmd` を一切作らない**。軸 B の「母集合が数えられる形になる」は、
**「サブプロセスを起動するファイル」と「サブプロセスのエラーを扱うファイル」を混ぜた母集合**になる。

### 軸 A / 軸 B の検出力（使い捨てプローブを実装して変異を実測）

両軸をプローブ実装（既存箇所は注記済み / allowlist 済みとみなす）し、5 つの退行を当てた結果。
判定は runner のサマリ行（`--- PASS: <名前>` / `--- FAIL: <名前>`）で行った。

| 退行 | 内容 | 起票時点のゲート | 軸 A（字句を `exec.Command` へ拡張） | 軸 B（import 境界） |
|---|---|---|---|---|
| R1 | `gitlog.go` の実ガード `cmd.WaitDelay = …` を削除（**issue 105 と同型の退行**） | green | **green** | **green** |
| R2 | allowlist 済みファイルに `exec.CommandContext(ctx,"gh",…).Output()` を追加（WaitDelay 無し） | RED | RED | **green** |
| R3 | allowlist 済みファイルに `exec.Command("gh",…).Output()` を追加 | **green** | RED | **green** |
| R4 | 新規ファイルで `exec.Command("gh",…).Output()` | **green** | RED | RED |
| R5 | 新規ファイルで別名 import（`import xe "os/exec"` → `xe.Command(…)`） | **green** | **green** | RED |

🚨 **この表の probe は「修正前のマッチャ」を埋め込んでいる**（probe A は旧実装の
`strings.Contains(lines[j], "WaitDelay")` / `lookahead=8` をそのままコピーした）。
したがって **R1 行の「A=green」は軸 A の性質ではなく、本 issue で直したコメント誤検出の影**である。
修正後のマッチャの上では:

- **軸 A は R1 を検出する**（A は同じ走査の検出パターンを広げるだけなので、M1 の RED をそのまま引き継ぐ）
- **軸 B（本文どおり「import 境界の検査へ*移す*」）は R1 を検出しない**。B は `WaitDelay` を
  一切読まないので、**B 単独に置き換えると本 issue で直した検出力を捨てることになる**

読み取れること:

- **R1 は軸の選択で「守れる/守れない」が分かれる**。修正後の土台の上では A は R1 を維持し、
  B への*置き換え*は R1 を失う（B を採るなら、字句ゲートを残して併用する形が要る）
- **A と B は排他ではなく相補**。A だけだと R5（別名 import）が抜け、B だけだと R2 / R3
  （**既に allowlist に載っているファイルへの追記**）が抜ける。日常的に起きるのは後者なので、
  B 単独は「新しいファイルが増えるとき」しか効かない
- A の代償: 既存 7 箇所に注記が要る。うち `tui.go:3080` は
  **「パイプが無いから安全」ではない**（上表 #5 で裏取り済み）ので、
  `subproc: no-waitdelay` を素で付けると**行内注記が嘘の不変条件を固定する**（本文の警告どおり）
- B の代償: allowlist 6 件のうち 2 件が `Cmd` を作らないファイル（上記）。
  また B は `WaitDelay` を一切見ないので、R1 に対しては原理的に無力

---

## 実装したもの（軸に依存しない分だけ）

`src/glogx/waitdelay_discipline_test.go`:

1. **マッチャを「語の出現」から「コードとしての代入」へ**。`stripLineComment` で行末コメントを
   落としてから `WaitDelay =` / `WaitDelay:` を探す。
   🚨 コメント剥がしは**代入を探す窓の中だけ**で使う（`subproc: no-waitdelay` の注記は
   行末コメントに書く運用なので、剥がすと注記済みの行が offender に化ける）
2. **`lookahead` を 8 → 12**。`gitlog.go:80` の実ガードは呼び先 `runGitCmd` の **+10 行**にあるため。
   この窓が**関数境界を跨ぐ**ことは既知の限界としてコメントに明記した（「同じ Cmd に張っている」
   ことまでは保証しない）
3. **カウンタを 2 本に分離**。`seen`（見つけた `CommandContext` の数）と
   `enforced`（注記で免除されなかった数）。注記が増えると `enforced` だけが減るので、
   `seen` しか見ない canary では「守りが薄くなった」を観測できない。
   下限は実測値の位置（`seen>=2` / `enforced>=1`）に置いた
4. **fixture canary を追加**（`TestWaitDelayOffendersCanary`）。走査本体と**同じ**
   `waitDelayOffenders` を通す 5 ケース。production の実件数（2 件）に依存せず検出力を固定する。
   「コメントで言及しているだけ」ケースが今回の欠陥そのものの pin
5. 自作 `itoa` を削除し `strconv.Itoa` へ（他に利用箇所は無いことを grep で確認）

### 変異検証（すべて build OK を確認したうえで、diff を目視して意図した行だけが変わったことを確認）

| 変異 | 期待 | 結果 |
|---|---|---|
| M1: `gitlog.go` の `cmd.WaitDelay = subproc.WaitDelay` を削除 | RED | **RED**（`--- FAIL: TestEveryCommandContextSetsWaitDelay`）※修正前は green |
| M2: 上記コメントから `WaitDelay` の語だけ削除 | **green** | **green** ※修正前は RED（**反転が消えたことの証拠**） |
| M3: `hasWaitDelayAssign` を旧実装（語の出現）へ戻す | canary が RED | **RED**。落ちたのは `TestWaitDelayOffendersCanary/コメントで_WaitDelay_に言及しているだけ` の **1 ケースのみ**（他 4 ケースは PASS = 狙った主張だけが落ちている）。このとき**本走査は green のまま**で、production だけではマッチャの退行を検出できないことも同時に確認できた |
| M4: 走査パターンを `exec.CommandContextXX(` へ破壊 | seen floor が発火 | **RED**（本走査 + canary 全 5 ケース） |
| M5: `gitlog.go:80` に `subproc: no-waitdelay` を足す（seen=2 のまま enforced だけ 0 へ） | enforced floor が発火 | **RED**。落ちたのは `waitdelay_discipline_test.go:134` の**enforced 用 `Fatalf`**（「WaitDelay を強制している箇所が 0 件しかない (下限 1)」）。seen floor でも offender 一覧でもないことを文言で確認した |

`cd src/glogx && go test ./...` = 全 package ok（43.9 秒）。`gofmt -l` 差分なし。

---

## 残タスク（= ユーザーの判断待ち）

- [ ] **軸の選択**。上の実測を踏まえた選択肢は 3 つある:
  - **A のみ**: R3 / R4 を塞ぐ。R5（別名 import）は残る。既存 7 箇所への注記が要り、
    `tui.go:3080` の注記文言を「前景の対話エディタで ctx も無いから」と**正確に**書く必要がある
  - **B のみ**: R4 / R5 を塞ぐ。**R2 / R3（既存ファイルへの追記）が抜ける**ので、
    日常的な退行に対しては A より弱い。さらに本文どおり字句ゲートから*移す*と、
    **本 issue で直した R1 の検出力まで失う**（B は `WaitDelay` を読まないため）
  - **A + B**: 上表で唯一 R2〜R5 を全部塞ぐ。ただし字句ゲートを残すので
    [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) §8 の
    「脅威モデルと『検出しない形』を先に書く」が必要
- [ ] （軸を決めたら）選ばなかった側を却下理由つきでこの issue に残す
- [ ] （B を採る場合）`doctor/runner/runner.go` に「この module の外部実行はここだけ」を 1 行残す

### 推奨（決定ではない）

**A + B、ただし A の脅威モデルを先に書く**を推す。理由:

- 単独ではどちらも日常的な退行を取りこぼす。A は「既存ファイルへの追記」（最頻）に効き、
  B は「新しいファイル/迂回」に効く。**塞ぐ穴が重なっていない**（R2〜R5 で実測）
- A の「迂回が無限」という批判（§8）は正しいが、それは**脅威モデルを書けば閉じられる**種類の問題。
  「うっかり書く典型形を止める。意図的迂回は review の責務」と明記すれば、迂回指摘は
  「検出しないと決めた形」として記録に回せる
- B 単独を推さないのは、母集合の質が実測で弱かったため（6 件中 2 件が `Cmd` を作らない
  エラー型だけの importer）。「数えられる母集合」という B の主要な利点が、実データでは目減りする。
  加えて B への*置き換え*は R1 の検出力を捨てることになる（上の 🚨）

判断の順序として、**「B へ移す」だけは単独で採らない方がよい**（本 issue で直した分が戻る）。
A の追加は既存 7 箇所への注記コストがあるので、そこを払うかどうかが実質の争点になる。

## 関連

- issue 105（この規律が生まれた経緯: 13 箇所中 1 箇所が静かに抜けた）
- research issue 308（本監査の記録。WaitDelay が保証するのは `Wait()` が返ることだけで、
  子孫の回収は保証しないという指摘を含む）
