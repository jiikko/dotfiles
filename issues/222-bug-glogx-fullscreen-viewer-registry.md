# 「今どの全画面ビューアが出ているか」の出典が 8 箇所に散り、doctor が既に取りこぼされている

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

## 関連

- issue 148 (doctor の実装。doctor が後から足された経緯)
- `~/.claude/rules/comment-no-restate-enforced.md` — 「実装で強制できない制約」をコメントに残す規律。
  本件は**強制できるようにする** (レジストリ + テーブル駆動テスト) 方向
