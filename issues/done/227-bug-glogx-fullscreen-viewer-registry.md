# 「今どの全画面ビューアが出ているか」の出典が 8 箇所に散り、doctor が既に取りこぼされている

🚨 **旧番号 222**: 別セッションが同じ 222 を同時に採番したため、参照の少ない側 (本 issue。tracked 参照は audit-log の 1 行のみ、push 済み commit message からの参照 0 本) を 227 へ寄せた (2026-09-03、issues/README.md の規律)。過去の会話やメモで「issue 222」が 全画面ビューアの話ならこの issue。

出典: audit (responsibility) 2026-09-03 / forge-Minimum。**指摘の中核部分は実コードで裏を取った** (下記「確認したこと」)。

## 症状 (先に直せる回帰)

`gitLogReloadDeferred` が **doctor を列挙していない**。

```go
// src/glogx/gitlog_watch.go: gitLogReloadDeferred
return m.actModal.active() || m.diffOv.visible() || m.prStatusOv.visible() ||
	m.detailOv.visible() || m.panelSHA != "" || m.pullAnimating || m.pushAnimating ||
	m.issuesOv.visible() || m.statusOv.visible() || m.rlDash.visible()
	//                     ^^^ doctorOv.visible() が無い
```

同じ関数の doc コメントは除外理由をこう書いている:

> 全画面の viewer (issues / status / 残量ダッシュボード) を見ている間は、そもそも git log が
> 見えていない。反映するとトーストだけが viewer の上に出て、裏でカーソルのリセットと CI の
> 再取得 (GitHub API) と見えないアニメの tick が走る (敵対レビューで実測 2026-09-01)

**doctor (D) はこの条件を等しく満たす全画面ビューア**だが、issue 148 で後から足されたため列挙から漏れた。
除外しない旨の記述はコード・docs・issues のどこにも無い (反証を探した結果、下記)。

### 発火条件

doctor を開いている間に、**外部で git が動く** (別ペインで commit / rebase / 別マシンからの pull)。
見張りが変化を観測 → `gitLogReloadDeferred` が false → `reflectGitLogChange` が走り、
doctor の裏で全面リロード (`loadLogData` = git 5〜6 fork、実測 既定 22ms / `--stat` 45ms / `-p` 139ms) +
`applyLogData` によるカーソル位置のリセット + CI の再取得 (GitHub API) が起きる。
利用者からは doctor の上にトーストだけが出る。

**silent に壊れる**。build もテストも通り、doctor を閉じたときに位置が変わっていることでしか気づけない。

## 構造 (これが本題)

「全画面ビューアが今どれか」という**単一の概念**が、型でもレジストリでもなく **8 箇所の手書き列挙**として散っている。
しかも場所ごとに membership が違う:

| 場所 (`src/glogx/`) | 列挙している集合 |
|---|---|
| `gitlog_watch.go: gitLogReloadDeferred` | issues / status / rlDash 🚨 **doctor 欠落** |
| `tui.go: issuesRestoreMsg のガード` | status / rlDash / doctor |
| `tui.go: handleKey` の routing 4 ブロック | doctor / rlDash / issues / status |
| `tui.go: viewLines` | rlDash / doctor / status / issues |
| `tui.go: hintLine` | rlDash / doctor / status / issues |
| `tui.go: spinnerActive` | 4 種の loading 述語 |
| `tui.go: updateKeyReachable` | issues / doctor / status (rlDash は意図的除外と明記あり) |
| `tui.go: restartPromptVisible` | doctor のみ |

変更理由は 3 つ — (a) ビューアを 1 枚足す (b) 横断キーの優先順位を変える (c) 見送り方針を変える —
どれも**同じ手書き列挙を人が全部揃え直す**ことになる。`updateKeyReachable` の 🚨 コメント
(「忘れても build もテストも壊れず静かに壊れる」) が、この方式の脆さを設計者自身の言葉で書いている。

### 同じ形: handleKey の routing プロトコルが 4 回コピーされている

`handleKey` 内で全画面ビューアごとの受け渡しが 4 回開き書きされている
(ownsKeys ガード → 委譲 → `deliverNotice(takeNotice())` → `takeWantQuit()` → `takeWantX()` → `maybeTick`)。
優先順位のチェーンが 1 箇所にあるのは契約なので正しいが、**プロトコルの本体まで重複している**ため差分が出ている:

- `U` (usage) は issues / status では効き、**doctor / rlDash では効かない**。前者 2 つには
  「viewer の上でも usage は出せる」という明示的な契約コメントがあるが、後者 2 つには効かせない旨の記述が無い
  → **意図と漏れが見分けられない状態**
- `p` / `b` / `u` の横取りは status だけ / `usageOv.dismiss()` の位置もブロックごとに違う

## 確認したこと (2026-09-03 実測)

- `gitLogReloadDeferred` の式に `doctorOv.visible()` が無い (上に引用したとおり)
- `doctorOv.visible()` は**実在し、本体コードで 5 箇所から呼ばれている** (呼べないから省いた、ではない)
- doc コメントの除外理由が doctor にそのまま当てはまる (全画面 = git log が見えていない)

### 反証の試み (「意図的」の証拠を探した結果)

- `gitlog_watch.go` の doc コメント: doctor を除外する旨の記述**なし** (issues/status/rlDash だけを挙げている)
- `docs/` / `issues/` / `issues/done/`: doctor を追従の対象に**残す**と決めた記録**なし**
- `updateKeyReachable` は rlDash を意図的に除外する旨を**明記している** = この repo は
  「意図的な除外は書く」規律を持っている。**書かれていない doctor の欠落は漏れと読むのが自然**

## 対応方針

### 1. 応急 (単体で検証できる回帰修正)

`gitLogReloadDeferred` に `m.doctorOv.visible()` を足す。
検証は「doctor を開いた状態で別ペインから git commit して `reflectGitLogChange` が走らないこと」。
**変異検証**: 足した項を外して red になることを見る (ミューテーションの形は「機構を戻す」= 削除した項の復活)。

### 2. 構造 (これをやらないと 5 枚目で同じことが起きる)

`browseModel` に**全画面サーフェスの単一の出典**を置く。最小形:

```go
func (m *browseModel) activeFullScreen() fullScreenID  // none/issues/status/ratelimit/doctor
```

`gitLogReloadDeferred` / `viewLines` / `hintLine` / `issuesRestoreMsg` のガードをここから導出する。
もう一段行くなら `visible() / ownsKeys() / lines(o) / hint(w) / loading()` を持つ
`fullScreenViewer` インターフェース (消費側 = `browseModel` に置く) の**順序つきスライス**にし、
routing・描画・hint・spinner をレジストリの走査に変える。routing の重複 (上記) は
`routeToFullScreen(v, key) (tea.Cmd, bool)` に寄せ、ビューア固有の横取り (status の p/b/u) だけ呼び出し側に残す。

配線漏れを機械で止めるには、**レジストリを列挙するテーブル駆動テスト**を置く
(各ビューアを開いた状態で `gitLogReloadDeferred` / `viewLines` / `hintLine` が期待どおりか)。
新しいビューアを足したらテーブルに 1 行足すだけで全サイトが検査対象になる形にする。

⚠️ テーブル駆動にするときは**ケース名ごとの pass/fail 一覧**で変異を判定すること
(スイートの rc で読むと、緑のまま残ったケースを見逃す。実例は `mutation-verify-new-tests.md`)。

### 3. `U` の扱いを決める

doctor / rlDash で usage を効かせないのが意図なら、その 1 行の理由を各ブロックに書く
(「4 箇所の差分は全部意図」の状態にする)。意図でないなら揃える。

## 先行事例 (この repo で既に 1 回起きた同じ class)

**issue [085](085-refactor-glogx-chrome-composition-dup.md) (done)** が、まったく同じ形を扱っている:

> `finishViewerWindow` と `viewLines` 末尾が、グローバル chrome の合成順を**逐語で 2 コピー**持つ。
> どちらの doc コメントも「ビューごとに書くと片方で載せ忘れる」「前面順もここで一本化する」と
> **一本化を主張しているのに、実体は 2 コピー**。

085 はさらに「**viewer が全画面だった頃、issues 中の通知が画面に一切出ない時期があった**」という
実際の事故を記録している。**本件 (222) は同じ class の 3 度目**で、今度は「見送りの列挙」で起きた。

→ 修正の形も 085 に倣える (合成順を 1 箇所へ寄せ、**片方の経路だけ落とす変異**で red を確認する)。

### 却下済み — 再提案しないこと

**issue 071 → 085 で `071-two-slide-state-machines` は却下されている**:

> `issuesView.slideAnimating` + `slideInWindow` と `statusView.slideAnimating` + `slideLeftWindow` が独立。
> **演出自体が意図的に別** (右から流し込む / 左端から板が生える) なので、共通化できるのは
> progress・closing・tickInterval の状態機械部分だけ。得られる削減は小さい。

したがって本 issue の `fullScreenViewer` レジストリ案は、**開閉演出の共通化を含まない**。
寄せるのは **membership (今どれが出ているか) と routing プロトコル**だけで、
各ビューアの `lines` / 演出は今の実装のまま残す。

同じく **`071` で却下済み**の近傍指摘 (再提案しない):
`job_detail_overlay` の `pagerScrollKey` 非委譲 / y/N の実行キー述語 3 箇所独立 (大文字 `Y` の差)。

## 関連

- issue 148 (doctor の実装。doctor が後から足された経緯)
- issue [085](085-refactor-glogx-chrome-composition-dup.md) / [071](071-research-design-audit-2026-08-20.md) — 上記の先行事例と却下一覧
- `~/.claude/rules/comment-no-restate-enforced.md` — 「実装で強制できない制約」をコメントに残す規律。
  本件は**強制できるようにする** (レジストリ + テーブル駆動テスト) 方向
- `~/.claude/rules/verify-design-intent-before-refactor.md` — 却下済みの分解を逆転提案しないための確認手順

---

## 対応 (2026-09-04, dotfiles-c2)

commit: `b2a03593` (構造) → `2dfd3972` (敵対的レビューの指摘)。
**応急 (対応方針 1) は着手前に別セッションが済ませていた** (`c973646e`。issue が `issues/` に
残ったままだったので、着手時に git 履歴を見るまで気づかなかった)。

### やったこと (対応方針 2 / 3)

- `src/glogx/fullscreen.go`: `fullScreenID` (none / ratelimit / doctor / status / issues +
  番兵 `fullScreenCount`) と `activeFullScreen()` / `fullScreenActive()`
- **「開いているか」を問う 5 サイトを全部そこから導出**: 見送り (`gitLogReloadDeferred`) /
  描画 (`viewLines`) / 最下行 (`hintLine`) / 復元の破棄 (`issuesRestoreMsg`) /
  キーの routing (`handleKey`)
- `handleKey` の 4 ブロックを `routeKeyToDoctor` / `routeKeyToRatelimitDash` /
  `routeKeyToIssues` / `routeKeyToStatus` へ切り出し、dispatch を 1 つの switch に
  (位置と理由の記述も 4 箇所の重複から dispatch 1 箇所へ)
- **`U` の非対称を解消** (方針 3): doctor でも `U` が効くようにした (issues / status と同じ契約。
  `ownsKeys()` 中は横取りごと飛ばす)。残量ダッシュボードは「同じ Snapshot を全画面で描いたもので、
  右上に小さい方を重ねると同じ値が 2 か所に出る」ため**意図的に受けない** — その旨をコードに明記

### 配線漏れの止め方 (3 段)

1. **lint**: `exhaustive` が「default なし switch は enum 全 case」を強制 → ID を足すと
   全サイトが `make lint` で赤くなる (🚨 この switch に `default:` を書くと外れる。明記済み)
2. **テスト**: ID ごとに 5 サイトの**挙動**を通す表 (`fullscreen_test.go`)
3. **AST ゲート**: `viewLines` 内の `finishWithGlobalChrome` は switch の中に ID の数ちょうど
   + 一覧の 1 本だけ。**ID を足さずに `if` で描画を配線する形**を止める (下記 P1)

### 敵対的レビュー (opus / read-only) の結果

**挙動の非等価は 1 つも作れなかった** (5 サイトで旧実装との等価を機械照合。順序が変わった
`handleKey` については「2 枚同時 shown」が到達不能であることを `shown = true` の全代入箇所から
網羅確認)。壊れたのは**強制手段の射程**:

- **P1**: 上の 1・2 は **ID を足した後にしか発火しない**。switch の前に
  `if m.newOv.visible()` を挿す旧来のやり方だと、描画・hint・routing は効くのに見送りと復元
  だけが黙って壊れる (= issue 227 と同じ形)。→ AST ゲート (3 段目) を追加し、
  **検出しない形**を脅威モデルとして `fullscreen.go` に明記した
- **P2**: 復元の破棄は「捨てる」側しかテストが無く、「常に捨てる」変異が全パッケージを素通り
  していた → 対称のケースを追加
- **P2**: 描画の検査が「一覧が出ていない」しか見ておらず、**ビューアの取り違え**
  (viewLines の doctor ↔ status を入れ替える変異) を素通りしていた → 表に識別行を追加
- **P3** 3 件 (alloc の余裕の数字が古い / `gitlog_watch.go` の doc が 4 枚を手書き列挙 /
  `hintLine` が actModal より前に return する選択の理由が無い) を修正

### 却下・見送り (再提案しないこと)

- **interface + 順序つきスライスのレジストリ** (issue の「もう一段」案) は採らない。
  4 つの `lines` / `hint` / routing はシグネチャも戻り値の語彙も別々でアダプタが要り、
  複雑性は下がらないまま**毎フレームの確保が増える** (m を捕まえた closure はスライスへ
  逃げるので必ずヒープに乗る)。1 フレームの確保には上限があり、issues-40 は実測 211 /
  上限 213 = **余裕 2 回**しかない。enum の switch は確保 0 (敵対レビューが 8 ケースで実測)
- **開閉演出の共通化**は含めない (issue 071 で却下済み。本 issue の範囲は membership と
  routing プロトコルだけ)
- **`default:` を禁じる ruleguard** は書かなかった (コメントで警告するに留めた)。
  同種の「機械化できるがまだしていない」項目として **issue 251** に記録
- **表の `show` が `toggle()` を迂回している** (開く演出中・スキャン未起動の状態を通らない)。
  今回の assert が vacuous になる形は見つかっていないので優先度低。将来 chrome 合成の事故を
  追うときは fixture の現実性を上げる候補
- **dispatch の取り違え (issues ↔ status を入れ替える変異)** は、今の 4 枚については既存の
  20 本超が red になるので追加していない。5 枚目にはそれが無いので、**識別まで見るのは描画と
  hint の 2 サイトだけ**という射程を記録しておく
