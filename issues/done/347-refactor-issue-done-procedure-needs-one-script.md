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

## 追記 2026-09-10: 2 周目の敵対的レビューで P1 を 2 件塞いだ

`adversarial-review-own-safeguards.md` §7（**指摘を直した差分にもう 1 周回してから閉じる**）に従い、
1 周目の P1 修正で新設した機構（trap による rollback / `tracked()` / repo 全体走査 / 事後条件）を
攻め口として明示して 2 周目に出した。**どちらも自分で再現してから直した**。

### P1-1 `relbase` が repo root 直下で絶対パスを返し、**事後条件の主張が偽**になっていた

```sh
d="$(cd "$(dirname "$1")" && pwd -P)"   # ファイルが repo root 直下なら d == BASE_DIR
printf '%s' "${d#"$BASE_DIR/"}"         # 末尾スラッシュ付きは一致しない → 絶対パスのまま
```

`old_base` が `/Users/koji/dotfiles` になり、`normalize` が
`Users/koji/dotfiles/issues/347-x.md` を作って `moved_old` と一致しない。
**書き換えも HIT 判定も起きないのに「stale 0 件」で rc=0** になる。

再現（fixture / 実測）: `README.md` に `](issues/347-x.md)` を置いて移動すると、
`docs/g.md` と `issues/340-y.md` は張り直されるのに **`README.md` だけ残り、rc=0 ✓**。
実 repo では root 直下の md に issue を指す markdown リンクが 0 件なので無害だったが、
**「移動前のパスを指す参照が repo に 0 件」という事後条件が嘘をついていた**。

→ `${d#"$BASE_DIR"}` で剥がしてから先頭の `/` を落とす。あわせて **`ISSUES_DIR` を
`pwd -P` で物理パスへ揃えた**（macOS の `/var` → `/private/var` で、`find` 由来の論理パスと
`scan_candidates` 由来の物理パスが食い違い、`$other != $dest` の比較が静かに外れていた）。

### P1-2 母集合（`scan_candidates`）に canary が無く、走査が空振りしても緑だった

awk（report モード）には canary を置いたが、**母集合の作り方には置いていなかった**。
`scan_candidates` を空にする変異を当て、参照を `docs/` だけに置いた fixture（= `issues/` 内の
リンク検査が捕まえられない位置）で **rc=0 ✓ のまま `docs/g.md` が切れた**。
**P1-2 のために足したオラクルが、その P1-2 と同じ壊れ方をしていた。**

→ 移動する前に「その issue 自身の本文の 1 行」で `scan_candidates` を引き、
**自分が母集合に出ること**を確かめる。出なければ 0 件ではなく**判定不能**として着手しない。

### レビューが攻めて壊せなかったもの

SIGTERM を in_flight の窓へ確実に命中させる（判定を 6 秒遅らせた）→ **完全に復元** /
複数番号で 2 件目が失敗 → 1 件目は確定・2 件目で exit 1（docstring どおりで仕様） /
staged だが HEAD に無い issue → `tracked()` が正しく真 / 番号の部分一致（`47` と `347`）→ 誤爆なし /
symlink ループと repo 外への symlink → `grep -r` は symlink を辿らないのでハングなし /
baseline 赤・検査スクリプトが実行できない場合 → どちらも fail-closed。

### 未確認リスク（再現手順を作れなかったので記録に留める）

- trap 実行中の再シグナル（rollback 途中の 2 発目の INT）
- `rollback` 自身が失敗する rc=2 経路（`cp` を失敗させる状況を作れていない）
- 巨大バイナリ / 非 UTF-8 での `grep -rlF --binary-files=without-match` の挙動

### 変異検証（7 本すべて red）

claim 削除 / 本文の張り直し / 参照の張り直し / 走査範囲を `issues/` に狭める /
判定の前に異常終了 / **母集合の走査を空にする（新規）** / awk を no-op（canary）。
正常系にも **repo root 直下からの参照**を fixture に足した（P1-1 の回帰）。

### 🚨 打ち切っていない — 3 周目が要る（§7 の例外条件を満たさない）

2 周目の修正はレビュワー側が**独立した fixture で追試**し、2 件とも塞がっていることを
実行結果で確認してくれた（`README.md` が張り直される / 母集合の変異で rc=1・何も動かない）。
その上で「例外条件 (b)（各修正を直接の実測で確認できた）に該当するので打ち切ってよい」と
提案されたが、**採らない**。理由は 2 つで、どちらも `adversarial-review-own-safeguards.md` §7 の原文にある:

1. **例外は (a) かつ (b)**。今回の修正は **(a) 判定ロジックを新設せず** を満たさない
   （母集合の canary = 「走査が空振りしていないか」の新しい判定）
2. §7 の 🚨 が **「(a)(b) を満たす小修正でも、別の環境条件を新たに持ち込むもの
   (大文字小文字 / ロケール / **パスの表記**) は判定ロジックの変更と数えて省かない」**と
   名指ししている。`ISSUES_DIR` の `pwd -P` 化は**まさにパスの表記**

→ 3 周目を依頼済み（攻め口: canary の needle が自分に当たらない形 / `ISSUES_DIR` の物理パス化 /
`relbase` の新しい剥がし方）。**指摘が出たら本 issue を `issues/` へ戻す。**

## 決着 2026-09-10: 3 周目で **P1 / P2 が 0 件**。打ち切る

攻め口 3 点（母集合の canary / `ISSUES_DIR` の物理パス化 / `relbase` の新しい剥がし方）を
渡して回してもらった結果、**新設した判定ロジックはどれも壊せなかった**（すべて実行結果）:

| 攻め口 | 結果 |
|---|---|
| needle が `-x --exclude-dir=.git [a](b)`（`-` 始まり + 正規表現メタ） | ✓ 正常完了（`grep … -- "$1"` の `--` が効く） |
| 本文が空白行だけ | 「canary を張れない」で rc=1・**何も動かさない** |
| 本文 1 行目に NUL（binary 判定で自分が母集合から消える） | 「母集合の走査が壊れている」で **fail-closed**・1 mm も動かない |
| `ISSUES_DIR` が symlink 経由 | ✓ `pwd -P` 化で実体側へ正しく移動 |
| `ISSUES_DIR` が存在しない | `-d` チェックが手前で落とす |
| issue の実体が symlink（BASE_DIR の外を指す） | baseline が先に赤くなって着手拒否 |
| `grep -rlF` の走査コスト（`tmp/` 476MB を含む repo 全体） | **0.048 秒**。実害なし |

**canary の偽陽性・偽陰性はどちらも作れなかった。**

### P3 3 件 → コード側にコメントで固定した（`docs(347): 3 周目の P3 …`）

1. **`relbase` は BASE_DIR 配下のパスしか受けられない**（`BASE_DIR=/a/b` に `/a/bc/x` を渡すと
   `c/x` を返す）。呼び出し元 2 箇所がどちらも `scan_candidates` の出力なので**構造的に到達不能**
   だが、**到達不能である理由が呼び出し側にしか無かった**ので前提を関数の直近へ書いた
2. **URL エンコードされた参照**（`](../issues/%33%34%37-x.md)`）は書き換えも検出もしない。
   検査側も同じく見ないので**一貫**している → 「検出しない形」節に明記（実測 rc=0・旧パスのまま）
3. canary が保証するのは「移動**前**の走査能力」だけ。移動が `grep` の挙動を変える経路は
   双方とも思いつかなかったので、指摘ではなく記録

### 打ち切りの根拠（§7）

3 周目で**採用する指摘が 0 件**。P3 への対応は**コメントの追加だけ**で、
判定ロジックを新設せず（(a)）、各記述を実測で確認している（(b)）ので、§7 の例外条件を満たす。
→ **4 周目は回さない。**

**1 周目に「変異 4 本すべて red」で通した実装から、2 周で P1 が計 5 件出た**のが本件の要点。
変異検証は「自分が想定した不変条件」しか試さないという `mutation-verify-new-tests.md` の
注記どおりの結果になった。

## 追記 2026-09-10: 母集合の canary が **CI の Lint を赤にした**（`| grep -Fxq`）

`a9900fb4` で足した canary をパイプで書いていた:

```sh
if ! scan_candidates "$needle" | grep -Fxq "$src"; then
```

`scripts/check_pipefail_grep_q.sh`（issue 096 の検査）が落とし、**master の CI Lint が赤**になった
（run 34421389852）。別セッションが気づいて知らせてくれた。

### lint の警告以上の実害があった

`grep -q` は**一致した時点で抜ける**ので、上流（`grep -rlF … | sort`）がまだ書いている最中なら
**SIGPIPE で死んで rc≠0**。`set -euo pipefail` の下ではパイプライン全体が非 0 になり、
`if !` が真に転んで **「母集合の走査が壊れている」で着手拒否**する。
つまり**正常なのに issue を 1 件も閉じられなくなる**向きの偽陽性で、
しかも fail-closed なので「安全側だから良い」とも言えない（道具が使えなくなる）。

いまは `scan_candidates` が `… | sort || true` で終わっているため**たまたま握り潰されて**いた。
**`|| true` 1 枚に依存**していた状態で、そこは lint も見ていない。

→ `grep -Fxq "$src" <<< "$(scan_candidates "$needle")"` に寄せた（パイプを挟まないので
SIGPIPE も pipefail も関係なくなる）。理由を行の直近にコメントで残した。
`make test-lint` rc=0 / 変異 7 本すべて red のまま。

🚨 **同じ日に別セッションも同じ lint に落ちている**（`awk … | grep -qE` のヘルパー pin）。
`check_pipefail_grep_q.sh` は実際に仕事をしている検査。
