# glogx: 本文モードの issue が外部で消えると、理由が出ないまま本文が無言で畳まれる

起票日: 2026-08-14
種別: bug
優先度: **P2** (表示・通知のみ。本文が壊れる / 誤った内容を出す / クラッシュはしない)

## 何が起きるか

issues viewer の本文モードで issue を開いている間に、**別プロセス**
(別セッションの Claude Code / `git checkout` / rename) がそのファイルを削除・改名すると:

1. 再スキャン結果 (`issuesScanMsg`) が届く
2. 本文が畳まれ、一覧へ引き戻される
3. **その理由「開いていた issue が見つかりません (一覧へ戻ります): &lt;basename&gt;」は
   そのフレームの画面に一切出ない**

ユーザーには「読んでいた本文が無言で消えた」に見える。
`ccd824e` 以前は本文がそのまま出続けたので、**畳むこと自体が新規の挙動**。

## 原因: notice の配達経路が 1 本しかない

`issues_view.go` の `rebindOpen` は `v.setNotice(...)` で置くだけ。
それを取り出す `takeNotice` は **`tui.go` の `browseModel.handleKey` からしか呼ばれない**。
`rebindOpen` は `receive` (= `issuesScanMsg` の Msg 経路) から呼ばれるため、
**キーを押すまで誰も取り出さない**。

⚠️ 同じ夜の `132cc81` が `tui.go:1064` にこの罠そのものを書き残している:

> notice はどのヘッダーも描かず、次の打鍵で takeNotice されるまで画面に出ない
> (キーを押すまで失敗が黙殺される)

**その約 20 分前に入った `ccd824e` が、まさに Msg 経路から `setNotice` を使っている。**

## 理由が恒久的に失われる条件 (敵対的検証で絞り込んだ)

畳んだフレームが無言なのは常に起きるが、理由が**永久に**失われるのは限定的:

| 次の打鍵 | 結果 |
|---|---|
| `j` など通常キー | `takeNotice` → トーストとして出る (実測: 打鍵の 2 フレーム後に画面へ。トーストは作成フレームが `shown=0` のため同時には出ない) |
| **`q` / `esc`** | **恒久的に失われる** |

`q` の経路 (`tui.go:1249` → `1256`):
`notice` を消費してトーストを作った直後に `takeWantQuit` → `quit()` が `m.done=true` にし、
`viewLines()` が `""` を返して `tea.Quit`。**トーストは `shown=0` のまま 1 度も描かれずプロセスが終わる。**
`lastWarning` に積まれても `w` でコピーする機会がもう無い。

⚠️ **本文が突然消えた直後のユーザーが `q` を押すのは十分に起こる反応**で、
その経路では「なぜ消えたか」が完全に伝わらない。

## 再現

`src/glogx` に置いて `go test . -run TestZTmpGone -v`:

```go
m := newTestBrowse(t, 1, map[string]CIState{}, nil)
m.handleKey("i")
root := t.TempDir(); dir := filepath.Join(root, "issues"); os.MkdirAll(dir, 0o755)
path := filepath.Join(dir, "001-feat-x.md")
os.WriteFile(path, []byte("# 001 feat: x\n\n本文の行\n"), 0o644)
iss := &issues.Issue{Path: path, Dir: dir, Rel: "001-feat-x.md", Number: "001", Category: "feat"}
iss.LoadMeta()
m.issuesOv.cwd = root
m.Update(issuesScanMsg{root: root, dirs: []string{dir}, issues: []*issues.Issue{iss}})
m.issuesOv.finishAnim()
m.handleKey("enter"); m.issuesOv.drawer.finish()   // 本文モードへ
os.Remove(path)
m.Update(issuesScanMsg{root: root, dirs: []string{dir}})   // 消えたスキャン結果が届く
// m.viewLines() に「開いていた issue が見つかりません」が含まれない
// m.issuesOv.notice には文字列が入っている (取り出されていないだけ)
```

### 実測 (HEAD)

```
open=<nil> body=<nil> notice="開いていた issue が見つかりません (一覧へ戻ります): 001-feat-x.md"
画面に理由が出ているか: false

[next 0] [All 0]                                                               ○
このタブに open の issue はありません (a: pending も表示)
j/k: 移動  Tab: カテゴリ  /: 検索  Enter: 本文  n: next  a: +⏸  q: 閉じる
```

### A/B (`rebindOpen` の default を `997d078` の実装 = 見つからなければ現状維持 に戻す)

```
open=&{.../001-feat-x.md ...} body=&{# 001 feat: x\n\n本文の行\n ...} notice=""
[next 0]▏001-feat-x.md
        ▏ 1 █ 001 feat: x
        ▏ 3 本文の行
```

→ **変更前は本文が出続ける。畳み + 無言化は `ccd824e` で新規に入った。**

### `q` で失われる経路の実測

```
q 打鍵直後: done=true notice="" lastWarning="開いていた issue が見つかりません…"
            toast.visible=true shown=0
viewLines=""            ← tui.go:2815 `if m.done { return "" }`
q が返した Msg: tea.QuitMsg → 以降フレームは描かれない
```

## 既存テストがこの穴を素通りする (false green)

`TestIssuesViewRebindOpenDiscardsWhenGone` (`issues_view_test.go:1725` 付近) は
`v.takeNotice()` を**直接** assert しており、**唯一の配達経路
(`tui.go` の `case issuesScanMsg`) を通らない**。配達が無くても永久に green。

実証: `tui.go:1249` の配達ブロックを丸ごと削っても
`TestIssuesViewRebindOpenFollowsMove` / `...DiscardsWhenGone` は green のまま。

なお打鍵経路からの配達は別テスト 2 本が守っている。
**抜けているのは「Msg 経路からの配達」で、実装が無いのでテストも無い。**

## 対応方針 (案)

1. **Msg 経路でも notice を配達する**。`tui.go` の `case issuesScanMsg` の後で
   `takeNotice` → トースト化する (打鍵経路と同じ扱いにする)
2. あるいは **`setNotice` を Msg 経路から使えなくする**。`rebindOpen` が
   トーストを直接返す形にして、「置いたが誰も取らない」状態を構造的に作れなくする。
   `132cc81` のコメントが警告している罠を、コメントでなく型・シグネチャで塞ぐ
3. **テストを配達経路ごと固定する**。`v.takeNotice()` 直叩きではなく
   `m.Update(issuesScanMsg{...})` → `m.viewLines()` に理由が出ることを assert する

## 未確認

- `esc` が `q` と同じ経路を通るかは未検証 (`q` のみ実測)
- 本文モード以外 (一覧のみ) で同じ Msg が届いたときの notice の扱いは未検証

## 関連

- issue 057 — 同じ「通知が画面に届かない」ファミリー (あちらは描画予算、こちらは配達経路)
- `_claude/rules/mutation-verify-new-tests.md` — 「検証したいコードが本当に実行されているか
  (closure / 環境変数ゲートの内側は走らない)」。本件は**配達経路の外側で assert していた**形
- `_claude/rules/pending-issue-rationale-in-code.md` — `132cc81` が罠をコメントで残したのは
  正しいが、**実装で強制できていない**ため 20 分前の commit が既に踏んでいた

## 対応記録 (2026-08-14)

案 1 + 3 で解決 (案 2 の型レベル強制は receive が tea.Cmd を返す既存契約を崩すため見送り。
代わりに deliverNotice の doc コメントに Msg 経路の罠を明記):

- `tui.go` の `case issuesScanMsg` で `receive` の直後に `takeNotice` → トースト配達を追加。
  配達したときだけ `maybeTick` を束ねる (issuesWatchMsg の 1s チェーンの意図を崩さない)。
  これで畳んだフレームでトーストが積まれ、`q`/`esc` 即押しでも `lastWarning` に届いている
  (打鍵待ちが消えたので「恒久喪失」の条件自体が消滅)
- 打鍵経路 2 箇所 (issues/status) の同型配達ブロックを `deliverNotice` ヘルパーに抽出 (重複除去)
- 回帰テスト `TestIssuesScanMsgDeliversRebindNotice` を新設: `m.Update(issuesScanMsg{...})` の
  配達経路ごと assert する (v.takeNotice() 直叩きの false green を踏まない形)。
  変異検証: 配達ブロックを削ると 3 assert すべて red になることを実測
- 横展開 grep: status viewer の setNotice は全て打鍵経路 (runDiscard も確認モーダル経由) で、
  Msg 経路から置くのは issues の rebindOpen だけと確認
