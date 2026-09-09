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

→ この欠陥は本 issue で修正済み（「実装したもの（第 1 段階）」）。
**軸の選択も決着済み（A + B の併用。2026-09-09）**。

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
（[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
の「canary の下限は実件数の近くへ置く」）。

### ③ 検査本体と無関係な自作 `itoa` がある

`waitdelay_discipline_test.go:78-90` の自作 `itoa` は `strconv.Itoa` で足りる。
ゲート本体の主張を薄めている（軽微）。

## 🚨 方針が割れていた（→ 2026-09-09 に **A + B の併用**で決着。以下は起票時の記録）

| 案 | 内容 | 根拠 |
|---|---|---|
| **A: 字句パターンを広げる** | 検出を `exec.Command(` へ（🚨 **括弧付きの `exec.Command(` は `exec.CommandContext(` を含まない**。2026-09-09 実測。この記述のまま実装すると CommandContext の検査が丸ごと消える。`exec.Command`（括弧なし）なら両方に当たるが、ctx 有無で要求が違うので CommandContext を先に判定してフォールスルーさせる必要がある）。`doctor` にも同じ検査を置くか、置かない理由を `runner.go` に書き残す | 変更が小さく、今のゲートの延長。パイプを作らない `Run()` は既存の `subproc: no-waitdelay` 注記でそのまま除外できる |
| **B: import 境界の検査へ移す** | 「`src/glogx` の非テストコードで `os/exec` を import してよいのは `subproc` / `gitlog`（Cmd を受け取って張る側）/ `tea.ExecProcess` へ渡すだけの前景実行に限る」 | 字句ゲートは迂回が無限に出る（[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8）。import なら書き換えで迂回できず、母集合が「import しているファイル一覧」という数えられる形になる |

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

- [x] 軸（A / B）を決め、選ばなかった側を却下理由つきでこの issue に残す
      → **決定: A + B の併用（A の脅威モデルを先に書く）**。2026-09-09、ユーザー判断。
      却下理由は「軸の決定と却下理由」節
- [x] canary の下限を実件数の近くへ置く（第 1 段階で `seen>=2` / `enforced>=1` + fixture canary、
      第 2 段階で母集合が変わったので `seen>=8` / `enforced>=2` へ取り直し）
- [x] **変異検証**: 検査対象のファイルから守りを外して red になることを確認する
      （軸非依存の分 = M1〜M5、軸込みの分 = R1〜R6 + 段ごとの MA1 / MA2 / MB1 / MB1R5 / MB2）
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

### 軸 A / 軸 B の検出力（軸の選択のために使い捨てプローブで測った版 → **下の実装版に置き換え済み**）

軸を決めるための事前見積もりとして、両軸をプローブ実装して 5 退行を当てた表がここにあった。
**その表は本実装での測り直し（「R1〜R6 の測り直し」節）に置き換えたので削除した。**
残す価値のある事実は 2 つだけ:

- 🚨 **プローブの R1 行（A=green）は誤りだった**。probe A が旧実装のマッチャ
  （`strings.Contains(lines[j], "WaitDelay")` / `lookahead=8`）をコピーしていたため、
  本 issue で直したコメント誤検出の影を測っていた。**プローブに「修正前の実装」を写すと、
  修正後の設計判断に修正前の欠陥が混入する**
- 見積もり自体の結論（A と B は塞ぐ穴が重ならない / B への*置き換え*は R1 の検出力を捨てる）は、
  本実装での測り直しでも同じだった

---

## 実装したもの（第 1 段階: 軸に依存しない土台。commit `23fac3cd` / `8a655aa7`）

`src/glogx/waitdelay_discipline_test.go`:

1. **マッチャを「語の出現」から「コードとしての代入」へ**。`stripLineComment` で行末コメントを
   落としてから `WaitDelay =` / `WaitDelay:` を探す。
   🚨 コメント剥がしは**代入を探す窓の中だけ**で使う（`subproc: no-waitdelay` の注記は
   行末コメントに書く運用なので、剥がすと注記済みの行が offender に化ける）
2. **`lookahead` を 8 → 12**。`gitlog.go:80` の実ガードは呼び先 `runGitCmd` の **+9 行**にあるため。
   この窓が**関数境界を跨ぐ**ことは既知の限界としてコメントに明記した（「同じ Cmd に張っている」
   ことまでは保証しない）
3. **カウンタを 2 本に分離**。`seen` と `enforced`
4. **fixture canary を追加**（`TestWaitDelayOffendersCanary`）。走査本体と**同じ**
   `waitDelayOffenders` を通す
5. 自作 `itoa` を削除し `strconv.Itoa` へ

### 第 1 段階の変異検証（すべて build OK を確認したうえで、diff を目視して意図した行だけが変わったことを確認）

| 変異 | 期待 | 結果 |
|---|---|---|
| M1: `gitlog.go` の `cmd.WaitDelay = subproc.WaitDelay` を削除 | RED | **RED**（`--- FAIL: TestEveryCommandContextSetsWaitDelay`）※修正前は green |
| M2: 上記コメントから `WaitDelay` の語だけ削除 | **green** | **green** ※修正前は RED（**反転が消えたことの証拠**） |
| M3: `hasWaitDelayAssign` を旧実装（語の出現）へ戻す | canary が RED | **RED**。落ちたのは `コメントで_WaitDelay_に言及しているだけ` の **1 ケースのみ**（他 4 ケースは PASS）。このとき**本走査は green のまま**で、production だけではマッチャの退行を検出できないことも確認 |
| M4: 走査パターンを `exec.CommandContextXX(` へ破壊 | seen floor が発火 | **RED**（本走査 + canary 全 5 ケース） |
| M5: `gitlog.go:80` に `subproc: no-waitdelay` を足す | enforced floor が発火 | **RED**。enforced 用の `Fatalf` が落ちたことを文言で確認 |

---

## 軸の決定と却下理由（2026-09-09）

**決定: A + B の併用。A の脅威モデルを先に書く**（ユーザー判断）。

### 却下: A のみ

R5（別名 import `import xe "os/exec"` → `xe.Command(…)`）が原理的に抜ける。字句パターンが
`exec.` 接頭辞を前提にしている以上、別名を許したままでは**段 A の検出パターンが「この module の
Cmd 生成を漏れなく指す」と言えない**。段 B の別名禁止は、A の穴を塞ぐというより
**A の前提条件を成立させる**役割で入っている（この関係は実装のヘッダに書いた）。

### 却下: B のみ（本文の「import 境界の検査へ*移す*」）

2 つ理由がある。どちらも実測で確認した（下表）。

1. **R1（既存の実ガード削除 = issue 105 と同型）を検出しない**。段 B は `WaitDelay` を
   一切読まないので、本 issue の第 1 段階で回復させた検出力をそのまま捨てることになる
2. **R2 / R3（既に allowlist に載っているファイルへの追記）を検出しない**。これは
   「新しいファイルが増える」より**日常的に起きる**形で、B 単独はそこに無力

付随して、B の主要な利点とされた「母集合が数えられる形になる」も、母集合を
「`os/exec` を import しているファイル」で取ると目減りする（6 件中 2 件が `Cmd` を作らない）。
実装では母集合を「**`Cmd` を作るファイル**」に変え、allowlist の stale 検査で
`cli_health.go` / `github.go` が載らない状態を保つようにした。

### 検討したが採らなかった第 3 の案: `lookahead` を捨てて `subproc.CommandContext` 経由に寄せる

**内容**: 「`WaitDelay` の代入が近くにあるか」を見るのをやめ、
「非テストコードは `subproc.CommandContext` / `subproc.Command` だけを呼ぶ」を強制する。
`lookahead` 方式は元々噛み合っておらず（窓は関数境界を跨ぐので「同じ Cmd に張っている」ことを
保証しない）、`12` という値も `runGitCmd` の**現在の行数への依存**でしかない。
関数を数行伸ばすだけで無言でズレる。

**採らなかった理由**:

- **production の構造変更を伴う**。`gitlog.go` は ctx 有りと ctx 無しの両経路を `runGitCmd` に
  集約して 1 箇所で張る設計で、これは「素の `exec` を `subproc` で包む」形に素直に移らない
  （`subproc.CommandContext` は ctx を要求する）。ctx 無しの前景実行 4 箇所も同様
- **本 issue はテストのゲートの射程を直すもの**で、外部実行の構造をやり直す issue ではない。
  同じ変更に混ぜると、ゲートの検出力の測り直しと構造変更の妥当性が混ざって判定できなくなる
- ただし **`lookahead` 方式の弱さは実在する**。本実装ではそれを「脅威モデルの (c)
  = 検出しないと決めた形」として明記し、review の責務に置いた。構造を寄せたくなったら
  **この案が正しい方向**なので、そのときは別 issue で `subproc` 側の API（ctx 無しの入口）から設計する

---

## 実装したもの（第 2 段階: 軸 A + 軸 B。commit `a57a0ad9`）

### 先に書いた脅威モデル（`adversarial-review-own-safeguards.md` §8）

正本は `src/glogx/waitdelay_discipline_test.go` の冒頭。要点:

- **止めるもの**: `src/glogx` に新しく外部コマンド実行を書いた人の**うっかりした張り忘れ**
  （issue 105 の形）
- **検出しないと決めた形**: (a) 字句を避ける書き方すべて（構造体リテラル / `syscall.Exec` /
  別パッケージのラッパ / 複数行に割る / reflect）(b) 注記に書かれた理由が真か
  (c) 代入が**同じ Cmd** に対してか（窓は行単位で関数境界を跨ぐ）
  (d) **実行箇所と注記を同時に足す**形（免除機構がある以上、原理的に閉じられない）
  (e) glogx 以外の module
- **責務**: 上記は review。§8 の stopping rule として、迂回の指摘が (a)〜(e) に該当するなら
  **塞がずに記録する**（塞ぎ続けると規則の軸が字句に固定され、迂回指摘が無限に出て収束しない）

### 段 A（字句走査の射程拡大）

- 検出パターンを 2 本に（`exec.CommandContext(` と `exec.Command(`）。
  🚨 **前者は後者を含まない**（`exec.Command` の直後が `(` か `C` か）ので、
  本文 A 案の「前方一致で含まれる」という記述のまま実装すると CommandContext の検査が消える
- **検出行もコメントを剥がしてから見る**。剥がさないと `exec.Command(` と書いた doc コメントが
  幽霊 offender になるうえ、**`seen` を水増しして floor の低下を隠す**（canary でピン留め）
- 免除の注記を 2 種類に分けた。`subproc: no-waitdelay`（不要）/
  `subproc: waitdelay-in <helper>`（Cmd を受け取った呼び先が張る）。
  `gitlog.go:68` は後者。前者を付けると「WaitDelay は不要」という**嘘**になる
- floor を新しい母集合で取り直した（実測 `seen=9` / `enforced=2` → 下限 `8` / `2`）

### 段 B（import 境界。段 A の**追加**であって置き換えではない）

`TestOsExecImportBoundary` を新設。腕は 2 本:

1. **`os/exec` の別名 import 禁止**（`xe` / `_` / `.`）。段 A の字句が素通りする経路を塞ぐ
2. **`Cmd` を作ってよいファイルの allowlist**（4 件）。**逆向きの stale 検査**も入れ、
   allowlist に載っているのに `Cmd` を作らなくなったファイルが残らないようにした
   （これが `cli_health.go` / `github.go` を母集合から恒久的に外す仕掛け）

### production 側の変更

| 箇所 | 対応 | 理由 |
|---|---|---|
| `open_workspace.go` × 2 | `subproc: no-waitdelay` | 前景の `tea.ExecProcess`・ctx 無し・端末を継承するのでパイプを作らない |
| `external_commands.go:285` | `subproc: no-waitdelay` | `open` は即 detach。`Run()` でパイプも無い |
| `external_commands.go` の `editorCommand` × 2 | `subproc: no-waitdelay` | 返した Cmd を `runEditorCmd` が前景起動。ctx 無し・パイプ無し |
| `gitlog.go:68` | `subproc: waitdelay-in runGitCmd` | WaitDelay は呼び先が張る（「不要」ではない） |
| **`tui.go` の job ログ nvim** | **注記ではなく `cmd.WaitDelay = subproc.WaitDelay` を追加** | 下記 |
| `doctor/runner/runner.go` | doc に 1 行 | 別 module で glogx の走査が原理的に届かない（軸 B の付随判断） |

🚨 **`tui.go` を注記で逃がさなかった理由**: `Stdin` が `strings.Reader` なので os/exec は
`os.Pipe` + copy goroutine を作る（GOROOT `os/exec/exec.go` の `childStdin` で裏取り済み。上表 #5）。
つまり他の前景実行と違い「パイプが無いから安全」が成り立たず、nvim の孫が読み口を握ったまま
nvim だけ終わると `Wait` が戻らない。ctx が無く kill する主体もいないので、上限は WaitDelay しかない。
**「不要」と書くと嘘の不変条件を固定する**ので、代入する方を選んだ。
副産物として `enforced` が 1 → 2 になり、**R1 型の検出が単一箇所に乗らなくなった**。

---

## R1〜R6 の測り直し（**本実装**で。プローブ版は上で削除済み）

判定は runner のサマリ行（`--- PASS: <名前>` / `--- FAIL: <名前>`）。
各変異は**ビルド可否を別に確認**（`go test -run XXX_NO_SUCH_TEST` の rc）してから read/green を読み、
**diff を目視**して意図した行だけが変わったことを確認した。ビルド不能は
`<BUILD-FAIL>` という第 3 の結果に落とす設計にしたが、**全変異で BUILD OK** だった。
ハーネスは `tmp/303-mutate.sh`（使い捨て）。

| 退行 | 内容 | 段 A | 段 B |
|---|---|---|---|
| R1 | `gitlog.go` の実ガード `cmd.WaitDelay = …` を削除（**issue 105 と同型**） | **RED**（offenders） | green |
| R2 | allowlist 済みファイルに `exec.CommandContext(ctx,"gh",…).Output()` を追記 | **RED** | green |
| R3 | allowlist 済みファイルに `exec.Command("gh",…).Output()` を追記 | **RED** | green |
| R4 | 新規ファイル・素の import・`exec.Command("gh",…).Output()` | **RED** | **RED** |
| R5 | 新規ファイル・別名 import（`import xe "os/exec"` → `xe.Command(…)`） | green | **RED**（別名の腕） |
| R6 | 新規ファイル・素の import・`CommandContext` + WaitDelay も張る | green | **RED**（allowlist の腕） |

- **目標だった「R1〜R5 が全部 RED」は達成**（どちらかの段が必ず RED になる）。
  R5 が段 A で green なのは設計どおりで、そこが段 B を足した理由
- **R6 は本実装のために足した**。R4 は**両段が検出する**ので、R4 で段 B の allowlist 腕を
  観測しようとすると「段 B を壊しても段 A で RED になる」ため、腕が死んでいても生きて見える。
  R6（段 A が原理的に green になる形）だけが allowlist 腕を独立に観測できる

### 段ごとの変異（§1.5。A の段と B の段を別々に壊す）

| 変異 | 期待 | 結果 |
|---|---|---|
| MA1: 段 A のマッチャを旧実装（語の出現）へ戻す | 段 A の canary 1 ケースのみ RED | **RED**。`TestWaitDelayOffendersCanary/コメントで_WaitDelay_に言及しているだけ` の 1 ケースのみ。**本走査と段 B は green** |
| MA2: 段 A の検出パターンから `exec.Command(` を落とす | 段 A が RED | **RED**。canary の ctx 無し 2 ケース + `waitdelay-in` ケース + 本走査。🚨 **段 B も RED** になった（`hasExecCmdCall` を共有しているため `open_workspace.go` が観測されなくなり stale allowlist 違反が出る）。段の独立観測には使えないので、段 B の観測は MB1 / MB2 で行った |
| MB1: 段 B の別名検出を殺す | 段 B の canary 4 ケースのみ RED | **RED**。`グループ_import_の別名` / `単独_import_の別名` / `blank_import` / `dot_import`。**段 A は green**。本走査 `TestOsExecImportBoundary` は green（production に別名 import が無いので正しい） |
| MB1R5: MB1 + R5 | 本走査が green に戻る | **green**。**R5 を検出しているのが別名の腕であることの証明**（R5 単独では RED だった） |
| MB2: 段 B の allowlist 腕（順・逆の両方）を殺す + R6 | 本走査が green に戻る | **green**。**R6 を検出しているのが allowlist 腕であることの証明**（R6 単独では RED だった） |

### floor の実測（canary の下限が実件数のすぐ下にあることの確認）

下限を 999 に上げて実値を読んだ（変異を戻して green に復帰することも確認）:

- `seen` = **9**（下限 8）
- `enforced` = **2**（下限 2）

---

## 検証

- `cd src/glogx && go test ./...` = 全 package ok（42.9 秒）
- `make test-lint` rc=0（`✓ Go プロジェクト 6 件すべてに lint/test target と CI レーン` まで出力を確認）
- `gofmt -l`（`tools/` を除く）差分なし
- 全変異の適用後に `git status --short` が空であることを確認（ハーネスの後始末が効いている）

## 残タスク

- なし（軸の決定・実装・測り直し・段ごとの変異まで完了）
- **スコープ外として記録**: 「第 3 の案（`subproc` 経由に寄せて `lookahead` を捨てる）」は
  上記の理由で採らなかった。`lookahead=12` が `runGitCmd` の現在の行数に依存している弱さは
  脅威モデル (c) として明記済み。構造を寄せる判断をするなら別 issue で `subproc` の API から設計する
- **未検証として記録**: 脅威モデルの (a)(b)(d) は**意図的に検出しない**と決めた形なので、
  変異も当てていない（当てれば green になるのが仕様）

## 関連

- issue 105（この規律が生まれた経緯: 13 箇所中 1 箇所が静かに抜けた）
- research issue 308（本監査の記録。WaitDelay が保証するのは `Wait()` が返ることだけで、
  子孫の回収は保証しないという指摘を含む）
