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

## 決着 (2026-09-06 実装済み)

**案 A を採用**: `epic/<name>/done/` と `epic/<name>/pending/` を状態ディレクトリとして予約し、
group issue の完了・保留は group の中で移す (global の `issues/done/` へは出さない)。
表示は「epic group の子だけ状態フィルタ (`a`) の対象外」で、展開すれば既定で全部見える。

当初の懸念「done が 2 箇所に散って集計経路が増える」は実測すると増えなかった:

- `issue-sync` skill の走査は `find issues -name done -prune` で**深さ非依存** (追加修正なし)
- hook (`human-tasks-due` / `retro-open`) は元から `epic/*/pending/*.md` を未完了として拾い、
  `epic/*/done/` を見ていない (走査集合はそのまま。コメントの記述だけ直した)
- viewer 側は状態名の列挙が `EpicChildStatus` 1 箇所に閉じている

副次的に、予約外の綴り (`closed/` `completed/` …) の配下も**迷子 `?` として一覧に出す**ように
した。done/pending を昇格したぶん「group 内でも global と同じ綴りを使う人」が出るのに、
それまでは中の md が黙って消えていた (走査・発見の両方を塞いだ)。

| 項目 | 実体 |
|---|---|
| 状態の写像 | `src/glogx/issues/parse.go` の `EpicChildStatus` (唯一の出典) |
| フィルタの例外 | 同 `StatusFilter.showsIssue` (`Filter` の唯一の入口。タブ件数も通る) |
| 親行の進捗 | `src/glogx/issues_view.go` の `groupProgress` / `countDoneIssues` |
| 3 経路の一致 | `src/glogx/issues_epic_status_dirs_test.go` (走査・発見・監視) |
| 契約 | `docs/issues-viewer-spec.md` 3 節・4 節・6 節 |

変異検証 9 本すべてで意図したテストが red (詳細は commit message)。目視確認は
`issues/278-human-verify-glogx-epic-group-view.md` に項目を足した (同じ画面・同じ期限)。

### 受け入れ条件

- [x] 所属情報の持ち方を A / B / C から決め、`docs/issues-viewer-spec.md` の該当節と
      `issues/README.md`「ディレクトリ構成」の「group 内に `done/` / `pending/` は作らない」を
      同じ変更で更新する（乖離を残さない）
- [x] epic の親行が進捗を出す。**書式は `(5 ✓2)` = 件数 + done 件数**（`2/5` の分数は、左の数が
      完了か残りかを読み手が決められないので不採用。2026-09-06 にユーザーが選択）
- [x] epic を展開すると done な子が既定（`a` を押していない状態）で並ぶ
- [x] **global な issue の既定表示は変わらない**（`○` のみ）。`TestFilterExemptsEpicChildrenFromStatusFilter`
      が両側を固定し、変異 M9（global done を常時表示）で red を確認
- [x] `a` の 3 段巡回と矛盾しない。**epic 内の `pending` も done と同じく既定で見せる**
      （2026-09-06 にユーザーが選択。「epic を展開したら中身は全部見える」の 1 契約で説明できる）
- [x] `n`（claim）・open・コピーが done な子行でどう振る舞うか → **変更不要**（実コードで確認）。
      `issues.MoveToSubdir` は「直下に居る issue」にだけ symlink の目印を置き、`done/` `pending/` に
      居るものは従来どおり rename で `next/` へ運ぶ（`isOpenPlacement`）。done な子で `n` を押すと
      ファイルが `epic/<name>/done/` から `epic/<name>/next/` へ動く = 完了を取り消して claim し直す、
      という global の done issue と同じ挙動になる。dangling な目印はできない。
      **むしろ 291 で改善された**: 従来 group 内 done は迷子（`GroupUnknown`）だったため
      `MoveToSubdir` の group 分岐に入らず、`n` が epic の外（`issues/next/`）へ運び出していた

## 残した論点（この issue では閉じない）

- **epic が閉じた（親 issue が done になった）ときの見え方**は未着手。今は親 issue も
  `epic/<name>/done/` へ移せるが、そうすると親行が消えて子だけが残る。→ issue 294
- **`issues/audit-log`** は issue をパスで参照する TSV なので、group issue を group 内 `done/` へ
  移すと参照が腐る（global `done/` へ移していた頃も同じ性質。今回の変更で経路が 1 つ増えた）。
  issue 293（done へ移すと相対リンクが切れる）と同じ「移動でパス参照が腐る」族
- 親行の件数は**タブ filter では絞られ、状態 filter では絞られない**（子は常に全部数える）。
  分母が `a` で揺れないので進捗として読める

## 関連

- [`docs/issues-viewer-spec.md`](../../docs/issues-viewer-spec.md) — 状態ディレクトリと epic の契約（一次情報）
- [`issues/README.md`](../README.md) — `issues/epic/<name>/done/` `pending/` の運用（この issue で書き換えた）
- `issues/278-human-verify-glogx-epic-group-view.md` — epic group view の動作確認（未完了）
