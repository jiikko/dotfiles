# refactor: issue を `done/` へ移す手順が 3 つに分かれていて、実際に 2 回落とした

起票日: 2026-09-09
カテゴリ: refactor
優先度: 中（**落とすと CI が落ちる**。実測 1 回）
出典: [retro 345](../345-retro-issue-backlog-consumption-2026-09-09.md) の気づき 5

## 何が起きているか

issue を `done/` へ移すには **3 つ**が要る:

1. `git mv issues/NNN-*.md issues/done/`
2. `issues/next/NNN-*.md` の **claim symlink を消す**（残すと dangling）
3. 本文の相対リンクを **1 段深く**する（`../_claude/…` → `../../_claude/…`）

**どれも人が覚えている**だけで、機械は「落とした後」にしか気づかせてくれない
（`tests/issues/test_next_links_valid.sh` と `test_issue_links_valid.sh`）。

## 実測 2026-09-09（1 セッションで）

| 落としたもの | 回数 | 結果 |
|---|---|---|
| ② claim symlink の削除 | **2 回**（313 / 324） | 313 の方は **CI の Tests が赤くなった** |
| ③ 相対リンクの深さ | **2 回**（310 / 336 ほか） | ローカルのリンク検査で気づけた |

同じセッションで 12 件を `done/` へ送り、そのうち **4 回**取りこぼしている。
「覚えている」に依存する形が限界に来ている。

## 対応案

**`scripts/issue_done.sh <NNN>` に寄せる。** やること:

- `issues/` 直下（または group の中）から実体を見つけ、対応する `done/` へ `git mv`
- `next/` に同名の symlink があれば `git rm`
- 本文の `](../` を移動後の深さへ合わせて書き換える（**深さは移動先から算出する**。
  `issues/done/` は 2 段、`issues/epic/<name>/done/` は 4 段）
- 最後に `test_issue_links_valid.sh` と `test_next_links_valid.sh` を回して、
  **失敗したら移動を戻す**（半端な状態で終わらせない）

🚨 **入口のドキュメントを同じ変更で更新する**
（[`new-tool-requires-entrypoint-docs.md`](../../_claude/rules/new-tool-requires-entrypoint-docs.md)）:
`issue-sync` skill の手順、`claim-issue-in-next-and-push.md` の「完了したら目印を消してから
done へ移す」、`issues/README.md`。**ヘッダコメントは入口に数えない**。

## 受け入れ条件

- [x] `scripts/issue_done.sh <NNN>` が 3 つを 1 コマンドで行う（**実際は 4 つ**。下記）
- [x] **失敗時に移動を戻す**（リンク検査が落ちたら元の位置へ）
- [x] group issue（`issues/epic/<name>/`）でも深さを正しく算出する
- [x] **変異検証**: symlink の削除を外すと `test_next_links_valid.sh` が red /
      リンクの depth 調整を外すと `test_issue_links_valid.sh` が red
- [x] 入口 3 箇所（skill / rule / README）を同じ commit で更新した

## 進捗: 実装 (commit `feat(347): issue を done へ送る手順を scripts/issue_done.sh に寄せる`)

`scripts/issue_done.sh` + `tests/issues/test_issue_done.sh`。

### 🚨 本文の「3 つ」は**数え落としていた。実際は 4 つ**

移した issue を**他の issue が参照している**ぶんの張り直しが要る。実測: `issues/345` が
`[issue 347](347-refactor-…md)` で参照しており、347 を動かすとこの 1 本が切れて
`test_issue_links_valid.sh` が赤くなる。逆向き（`issues/done/*.md` が `](../NNN-x.md)` で
open な issue を指している形）も実在する。**3 つだけ実装しても CI は緑にならない**ので、
4 つ目を入れた（`grep -rhoE '\]\((\.\./)*[0-9]{3}-…'` で全数を数えて確認）。

### 3 と 4 は同じ計算なので 1 つの awk に寄せた

「① リンクを解決前のディレクトリで絶対化 → ② 指し先が今回動かす issue なら移動後のパスへ →
③ 解決後のディレクトリからの相対に書き直す」。移動した当人（①と③のディレクトリが違う）と
他ファイル（同じ）を同じコードが扱う。素朴な `](../` → `](../../` の置換では
`](done/311-x.md)` のような**下向きリンク**を壊す（実在する形）。
コードフェンス／インラインコードは `test_issue_links_valid.sh` の `extract_links` と同形で除外。

### 安全機構と、それが実際に捕まえたもの

- **canary**（本走査と同じ awk に既知の入力を通す）／**baseline**（着手前に赤いなら着手しない）／
  **rollback**（検査が落ちたら逆操作で戻す）
- 🚨 **変異検証が rollback の実バグを 1 件炙り出した**: 逆操作を `A && { B; C; }` で繋いでいたため、
  `set -e` の下で B（既に在る symlink への `ln`）が落ちると **C（`git mv` の戻し）に到達せず**、
  issue が `done/` に置き去りになった。各手順を独立した `if` に分解し、`git rm` が空にした
  `next/` を作り直してから symlink を戻すよう直した
- 判定は「ファイルの有無」でなく **`issues/` 配下の全エントリのハッシュ／readlink のスナップショット比較**
  （有無だけ見ると「本文だけ戻っていない」を素通しする）

### 変異検証（4 本すべて red、かつ rollback 後の状態が着手前と完全一致）

| 変異 | 結果 |
|---|---|
| ② claim symlink の削除を外す | RED（`test_next_links_valid.sh`「目印の指す先が通常ファイルとして存在しない」） |
| ③ 移した本文の張り直しを外す | RED（`test_issue_links_valid.sh`「リンクが解決しない」） |
| ④ 他 md からの参照の張り直しを外す | RED（同上） |
| awk の書き換えを no-op にする | RED（**canary が触る前に**落とす。fixture は 1 バイトも変わらない） |

変異は使い捨ての fake root（`$fake/scripts/issue_done.sh` = sed で潰したコピー ＋
`$fake/tests` → 本物への symlink）で当てる。repo の `scripts/` を書き換えない。
検査を stub にすると「検査が赤くなること」を検査できず自己言及になるので、**本物の検査**を通す。

### 走った証拠

```
$ make test-dir DIR=tests/issues
[run] tests/issues/test_issue_done.sh
✓ issue_done.sh: 正常系 (global / group) / baseline 赤の拒否 / 変異 4 本 … すべて red かつ rollback 済み
[run] tests/issues/test_issue_links_valid.sh   ✓ md 348 本 / リンク 493 本
[run] tests/issues/test_issue_numbers_unique.sh ✓ 347 件
[run] tests/issues/test_next_links_valid.sh     ✓ symlink 7 件
```

`shellcheck -S warning` は両ファイルとも rc=0 / 出力なし。

## 進捗: 敵対的レビュー (opus / read-only) の P1 3 件・P2 1 件・P3 1 件を直した

commit `fix(347): 敵対レビューの P1 3 件を塞ぐ (rollback の窓 / 走査範囲 / 追跡外の claim)`。
指摘は**すべて自分で再現してから**採用した。

### P1-1 追跡外の claim symlink で `git rm` が rc=128 → **rollback に届かず半端な状態**

`set -euo pipefail` の下では `git rm` の失敗でその場で死に、rollback は「検査が赤いとき」の
`else` 節にしかないので通らない。**再現した**（移動済み・dangling claim・rc=128・警告なし）。

🚨 realism が高い: `src/glogx/issues/move.go:96` の `placeNextLink` は `os.Symlink` するだけで
**`git add` しない**ので、glogx の `n` で付けた claim は**常に追跡外**。
`_claude/hooks/next-claim-unshared.sh` はまさにその状態を毎プロンプト検出するために在る。
→ 追跡の有無で `git mv` / `git rm` と `mv` / `rm` を切り替える。

### P1-3 判定より前の中断（Ctrl+C）で、EXIT trap が**バックアップごと**消して復旧不能

P1-1 と同じ「移動してから判定までの窓に rollback が届かない」形。
→ `rollback` を関数へ括り出し、`in_flight` フラグ + `trap on_exit EXIT` / `INT` / `TERM` で
**任意の異常終了から**同じ復元を回す。窓は 1 件あたり数秒（リンク検査 1.82 秒 + 0.14 秒）。

### P1-2 `issues/` の外からの参照は張り直されず、**検査も見ないので rc=0 のまま壊れる**

**既に実 repo で発現していた**: `docs/nvim-ruby-lsp.md:4` が `../issues/332-…md` を指したまま
切れていた（332 は done へ移動済み）。次に踏むのは open な 334（参照元 2 本）。
→ 手順 4 の走査を **repo 全体**へ広げ（母集合は `grep -rlF <ファイル名>`）、
**「移動前のパスを指す参照が repo に 0 件」**を事後条件として自前で持つ
（検査側の射程は issue 293 の判断どおり `issues/` に閉じたまま）。切れていた 1 件も直した。

全数勘定（`issues/` の外から `issues/**.md` を指す md リンク 6 本）: 解決 3 / 未解決 3。
未解決のうち 2 本（`_claude/agents/tt-api-expert.md` = 別プロジェクト向け /
`issues/done/293` 本文の書式説明）は **issue 293 が既に「触らない」と決めている**ので対象外。

### P2-3 `epic/<name>/<予約外>/` の issue が 1 段深い `done/` へ黙って入る

`case` の `epic/*)` が `epic/900/blocked` にも一致していた。epic は固定 2 段（`issues/README.md`）
なので、`epic/*/*)` を先に置いて**拒否**する（リンクは整合するので両検査とも緑になり、
契約違反だけが残る形だった）。

### P2-1/P2-2 canary を素通りする変異が 2 本 → canary を広げた

`~~~` フェンス規則の削除 / 同一 base ガードの削除が canary を通っていた。
canary に `~~~` ケースと「無関係な冗長パス（`./done/…`）を触らない」ケース、
および report モードの固定を足した。

### P3-3 `issue_done.sh 347 --help` が「347 を処理せず rc=0」だった

`--help` を番号検証より先に見るようにした（成功に見える無操作を作らない）。

### 変異検証 (6 本すべて red)

claim 削除を外す / 本文の張り直しを外す / 参照の張り直しを外す /
**走査を `issues/` に狭める（事後条件だけが捕まえる）** / **判定の前に異常終了する（trap が拾う）** /
awk を no-op にする（canary が触る前に落とす）。
**変異が当たったことを diff で確認**し、構文エラーを red と読まない guard も入れた
（最初の版で「当たっていない変異の緑」を 1 度踏んだ）。

### 残タスク

- **未検証**: この道具を実際の done 移動で使うのはこの issue 自身が最初になる（自己適用で検証する）
- **スコープ外**: `_claude/agents/tt-api-expert.md` の `](issues/README.md)`（別プロジェクト向け）と
  commit message 内のパス（immutable）。issue 293 の判断を踏襲する

## 関連

- [`claim-issue-in-next-and-push.md`](../../_claude/rules/claim-issue-in-next-and-push.md) — ②の規範
- [retro 345](../345-retro-issue-backlog-consumption-2026-09-09.md) — 出典（実測 4 回の取りこぼし）
