# glogx issues viewer: epic 配下の done な子 issue は既定で表示する

起票日: 2026-09-06
カテゴリ: feat / 対象: `src/glogx`（issues viewer）+ `docs/issues-viewer-spec.md` + `issues/README.md`

## やりたいこと

**global な issue は `done/` へ移した時点で既定の一覧から消える（`a` でフィルタを進めない限り
見えない）。この既定は変えない。epic group の中だけ、done な子 issue を既定で表示する。**

```
▾ glogx-epic (2/5)          ← 親行は「残り / 全体」
    ○ feat  291 …           ← open
    ▶ bug   287 …           ← next（claim 済み）
    ✓ feat  283 …           ← done も既定で見える（global では見えない）
    ✓ chore 281 …
    ✓ docs  277 …
```

## なぜ

epic は「まとまった仕事」の器で、器の価値は**進捗が読めること**にある。今の仕様では完了した子は
global `issues/done/` へ出ていくので、epic は残タスクが減っていくだけの箱になり、
`▸ <name> (3)` の 3 が「あと 3」なのか「元から 3 件の epic」なのか読めない。分母が見えて初めて
「この epic は終わりかけ / まだ入口」が分かる。

global 側で done を伏せる理由（実測で done が全体の 8 割を占める repo があり、既定で混ぜると
open が埋もれる — `docs/issues-viewer-spec.md` の「既定は open だけ」節）は epic 内には効かない。
epic 1 つの子は数件〜十数件で、done を混ぜても open が埋もれる規模にならないため。

## 前提として決めないといけないこと（本 issue の本体）

**done な子 issue の「epic 所属」をどこに持つか。** 現行契約は 2 つの文書に割れている:
`docs/issues-viewer-spec.md` の「`epic/<name>/done/` と `epic/<name>/pending/` は予約しない。
そこに置かれた md は迷子として `StatusUnknown` / `?` で表示し…」と、`issues/README.md`
「group 内に `done/` / `pending/` は作らない。完了は global の `issues/done/` へ移す」。
**global へ移した瞬間にパスから所属が消える**ので、viewer は今
「どの done がこの epic の子だったか」を知る手段を持たない。

| 案 | 内容 | 評価 |
|---|---|---|
| **A** | `epic/<name>/done/` を状態ディレクトリとして予約する | **推し。** パスが正本という既存契約のまま。代償は done の置き場が 2 箇所に割れること（横断で done を数える道具は両方を見る必要がある）と、epic クローズ時に子を global `done/` へ一括移動する手順が要ること |
| B | global `done/` のまま front matter か命名に epic 名を記録する | 一覧・claim・audit-log が全部パス前提なので、ここだけ例外の同一性キーが生える。反対 |
| C | 親 issue（`epic/467/467-*.md`）の本文に子リストを持たせて viewer が引く | 手書きの同期漏れが即バグ。反対 |

A を採る場合、既存の「`epic/<name>/done/` に置かれた md は迷子（`StatusUnknown` / `?`）」の
経路が消える。**外す前にその迷子扱いがマスクしていた failure mode を列挙すること**
（`_claude/rules/list-masked-failure-modes-before-removing-guard.md`）。少なくとも
「done 以外の綴り（`Done` / `closed`）で作られたディレクトリを黙って状態として扱わない」
ガードは残す必要がある。迷子扱いの意図は `src/glogx/issues/parse.go` の `EpicChildStatus` の
コメントにある（「置き場所を間違えた issue を消さない」ため）ので、そこも同じ変更で直す。

🚨 **状態ディレクトリ名の列挙は 1 箇所（`EpicChildStatus`）に閉じている契約**がある
（走査 `scanEpicDir` / 発見 `hasEpicMarkdown` / 監視 `issuesWatchDirs` の 3 者が同じ集合を見る
必要があるため。同関数のコメントが正本）。`done` を予約側へ移すなら、この 3 経路が揃って
追随することを変異検証で確かめる（片方だけ増やすと、監視されないディレクトリや
発見されない issue ができる）。

## 受け入れ条件

- [ ] 所属情報の持ち方を A / B / C から決め、`docs/issues-viewer-spec.md` の該当節と
      `issues/README.md`「ディレクトリ構成」の「group 内に `done/` / `pending/` は作らない」を
      同じ変更で更新する（乖離を残さない）
- [ ] epic の親行が「残り / 全体」を出す。`(N)` の N の意味が変わるので spec の該当行も直す
- [ ] epic を展開すると done な子が既定（`a` を押していない状態）で並ぶ
- [ ] **global な issue の既定表示は変わらない**（`○` のみ）。回帰テストで固定する
- [ ] `a` の 3 段巡回（`○` → `○⏸` → `○⏸✓`）と矛盾しない。epic 内の `pending` の扱いを決める
      （done と同じく既定で見せるのか、`a` を待つのか）
- [ ] `n`（claim）・open・コピーが done な子行でどう振る舞うか決める（`n` は no-op + notice が妥当か）

## 未決 / 論点

- **epic が閉じた（親 issue が done になった）ときの見え方**。「open な epic に限り既定表示」で
  合意しているが、閉じた epic 配下の done をどう畳むかは未決。子ごと global `done/` へ流すのか、
  親行ごと done タブへ移すのか
- **A を採ると done が 2 箇所に散る**。`issue-sync` skill の走査は
  `find issues -name done -prune`（`-path` ではなく `-name`）なので深さに関係なく
  `done` という名前のディレクトリを除外する = A でも追加修正なしで動く見込み（未実測）。
  一方 `issues/audit-log` は issue をパスで参照する TSV なので、移動で参照が腐る。
  着手前にこの 2 つを実測で確かめること
- 親行の `(2/5)` は現在のタブ・状態フィルタで見える件数を数えている今の定義と噛み合うか
  （フィルタで隠れた子を分母に数えるのかどうか）

## 関連

- [`docs/issues-viewer-spec.md`](../docs/issues-viewer-spec.md) — 状態ディレクトリと epic の契約（一次情報）
- [`issues/README.md`](README.md) — 「group 内に `done/` / `pending/` は作らない」の記述
- `issues/278-human-verify-glogx-epic-group-view.md` — epic group view の動作確認（未完了）
