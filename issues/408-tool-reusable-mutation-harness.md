# 408 (tool): 変異検証の前提検査を共通化する — 参考実装は `tests/issues/test_issue_done.sh`

> 🚨 **担当中: dotfiles-d4**（2026-09-24〜。dotfiles-87 から引き継ぎ: 実装と 3 周目までは 1f4ef0db に入っている）

起票日: 2026-09-20 (出典: obaket 872 の retro 項目 6 の切り出し検討)

> **pending (2026-09-21〜)**: 着手条件は下の「trigger」。
> **着手するときは、まず下の「設計の出発点」節から読む** — 起票時の設計 (in-place 変異 + dirty guard) は
> 参考実装の読み違いに基づいており、受け入れ条件の半分が不要になる。
**codex 反証レビュー 1 本で中心前提が 3 つ崩れたので、起票と同日に全面改稿した** (下の「崩れた主張」節)。

## 事実 (出典にあたって数え直したもの)

**失敗の類型を混ぜずに数える。** 以下は「`git checkout --` による復元事故」だけの episode 数:

| 日付 | episode | 内容 | 出典 |
|---|---:|---|---|
| 2026-09-04 | 1 | **worktree `~/wt-227228` 上で** 未コミットの修正を復元で消した (7 ファイルに及んだ) | [250](done/250-retro-fullscreen-registry-and-termsafe-gate-2026-09-04.md) |
| 2026-09-14 | 3 | ルールを読んでいて 3 回 | [374](done/374-retro-derived-fields-guard-cache-and-mutation-2026-09-14.md) |
| 2026-09-17 | 1 | 足したばかりのテストを 1 ケース目の checkout で消した | [389](done/389-retro-glogx-cursor-glide-and-shift-space-2026-09-17.md) |
| 2026-09-20 | 1 | 未 commit の置換 4 か所を巻き戻し、「120 秒 green」の誤読を生んだ | [rationale](../_claude/rules-rationale/mutation-verify-new-tests.md) / obaket 871 |

**= 17 日で 6 episode。** 別類型 (当たっていない変異 / 構文エラーの変異) が 2026-09-10 に 2 件
([348](done/348-retro-issue-done-tool-and-truecolor-guard-2026-09-10.md))。**両者は同じ guard で止まらないので分けて数える。**

🚨 **2026-09-04 は worktree 上で起きている。** ルール本文も
「worktree が消すのは他人の混入だけで、**その worktree にある自分の未コミット変更**は同じように消える」と
明記している。**worktree を使っていても dirty guard は要る** (起票時の私の射程の見積もりは逆で、誤りだった)。

## 起票時に書いて、反証で崩れた主張 (記録)

- ❌ 「17 日で 8 件」 → **単位が混在**していた (episode / 失敗類型 / 変異ケース数 / ファイル数を足していた)。
  さらに 9/5・9/6・9/8・9/9・9/15〜16・9/19 の変異失敗・false green・fixture 事故が表から抜けていた
  (rationale の 9/15〜16 の集計だけで fixture 偽装 11 件)。**類型を分けて数え直した** (上の表)
- ❌ 「すべて『ルールは読んでいた』状態」 → 出典が明記しているのは 9/4 / 9/10 / 9/14 / 9/17。
  **9/20 は書いていない** (読んでいなかったとも言えないが、読んでいたとも一般化できない)
- ❌ 「8 件すべてが guard miss」 → 9/14 の一部は誤った assert / 閾値、9/17 は state と rendering の観測設計、
  9/20 は fixture・seam・vacuous assertion の問題を含む。**dirty check / 復元 / syntax check を足しても検出できない**。
  道具の効果見積もりが過大だった
- ❌ 「対象は worktree 非使用の手動検証」 → 9/4 が worktree 上。**非 worktree の件数は出典不足で算出不能**
- ⚠️ 「ルールは既に 4 点を要求している」 → 部分的に正しいが**本文の一部を抽象化した表現**だった
  (Swift build / runner ごとの検査が落ちており、3 値も `codex-drive [3.8]` は `red / green / hang` と書いている)
- ✅ 反証できなかった: 9/4〜9/20 が 17 日間 / `bin/` に同一の汎用 mutation harness は無い /
  9/4 と 9/20 の episode 数そのもの

## 既にあるもの (ゼロから作る issue ではない)

- **`tests/issues/test_issue_done.sh` の `check_mutant`** — 変異を当てて `diff -q` で**当たったことを確認**し、
  `bash -n` で構文を見て、期待したキーワードで red を確認し、fixture の snapshot 差分で半端な状態を検出する。
  一般化の出発点はここ。🚨 **ただし下の「設計の出発点」節のとおり、これは作業ツリーを変異させていない**
- **`scripts/with_fresh_worktree.sh`** — worktree の作成と cleanup
- **`~/.claude/skills/codex-drive/SKILL.md` の `[3.8]`** — worktree / 変異適用 / 実行 / 復元 / 結果表 / spot check の手順。
  軽量経路では `cp` 1 世代の backup を明示的に許可している

→ 新規性は「**`bin/` の汎用 CLI として切り出すこと**」だけ。`[3.8]` のフロー全体を新しく機械化する話ではない。

## 🚨 設計の出発点 — 参考実装は in-place 変異ではない (2026-09-21 追記)

起票時の設計は「作業ツリーのファイルを変異させ、guard で事故を止める」前提だったが、
**参考実装 `check_mutant` は作業ツリーを 1 バイトも変異させていない**:

- `make_fake_root` が**スクリプトを fake root へコピーしてから** sed を当て、`"$fake/scripts/issue_done.sh"` を実行する
- 作業ツリーの `$SCRIPT` は `diff -q` の比較対象として読むだけ
- `snapshot "$dm"` が見ているのも **fixture ディレクトリ**であって source tree ではない

**参考実装に dirty guard が無いのは要らないからで、copy-then-mutate という構造が復元事故を原理的に
起こさない** (`adversarial-review-own-safeguards.md` §0-A「そもそも発生させない構造はないか」)。
起票時はこの問いを飛ばして guard の設計に入っていた。

帰結:

- 下の受け入れ条件のうち **dirty 拒否 / 復元 / 残骸ゼロ / trap 1 本 / 復元失敗時の結果状態**は、
  **in-place を採る場合にだけ**要る。copy-then-mutate なら大半が消える
- 「未確定」に書いた衝突 (dirty 拒否 vs 開発中コードの検証) も in-place 固有で、
  コピーして変異させるなら未コミットのコードもそのままコピーできる
- したがって道具の本体は **guard CLI ではなく「使い捨てコピーと baseline を安く作ること」**になり、
  `codex-drive [3.8]` の機械化に近づく

**着手時に確かめること**: copy-then-mutate が一般化するか。再配置できるスクリプト
(`ISSUES_DIR` を env から取る `issue_done.sh`) では綺麗に効くが、Go module / Swift package は
ツリーを動かすとビルドキャッシュと module path にぶつかる (だから `scripts/with_fresh_worktree.sh` がある)。
一般形はおそらく「script は fake root / ビルド対象は worktree」の二本立て。

## やること (案)

`tests/issues/test_issue_done.sh` の `check_mutant` を **repo 非依存に一般化**して `bin/` へ出す。
入口で必ず強制するもの:

1. **in-place を採る場合に限り**、`git status --porcelain` が空でなければ拒否する
   (`--allow-dirty` は置かない。抜け道は事故の再生産)。copy-then-mutate ならこの条件自体が不要
2. 変異を当てた後、**当たったこと**を確認 (`diff -q`)
3. 構文検査 (拡張子から言語を決める)。通らなければ **第 3 の結果** (red でも green でもない)
4. `--verify` の結果を **rc と実行件数の両方**で記録。実行 0 件を green にしない
5. 復元と、残骸ゼロの確認。**trap は 1 本に束ねる**

## 受け入れ条件 (レビューが指摘した fail-open を含む)

- [x] `bin/<名前>` と self-test が在る → `bin/mutate-verify` / `tests/bin/test_mutate_verify.sh` (12 ケース)
- [x] self-test が固定する (🚨 **`test_issue_done.sh` より弱くしない**):
  - [~] ~~dirty で拒否する~~ → **不要になった**。worktree 方式なので dirty でも安全に動き、
        むしろ未コミットの変更を**持ち込んで**検証する (本 issue の「設計の出発点」節が
        「in-place を採る場合にだけ要る」と予告していたとおり)。代わりに
        **「作業ツリーが 1 バイトも変わらない」を self-test のケース 11 が固定する**
  - [x] **`diff -q` が「何かが変わった」で通ってしまう形**を塞ぐ → `--file` の hash 比較 (rc=4) と
        `--file` 以外が変わったら落とす (rc=8) の 2 段。**行レベルの照合はしない**が、当てた diff を
        必ず表示する (射程の外として冒頭に明記)
  - [x] **実行件数の取得契約**を決める → `--baseline-expect` を**必須**にした。baseline が rc=0 でも
        このサマリ行が出なければ rc=3 (判定不能)。self-test のケース 8 が固定
  - [~] 残骸ゼロの確認 → **worktree / 子プロセス / 作業ツリーの git state は見る** (ケース 11)。
        **repo の外に書かれた生成物は見ない** (下の「射程の書き直し」)
  - [x] baseline green (rc=3) / 期待した test case の失敗への帰属 (rc=7) → self-test のケース 5・7 が固定
- [ ] 🚨 **自作の安全機構なので敵対的レビューを最終ゲートにする** ← **実行中** (opus の red team 1 本)
- [x] 入口のドキュメントを同じ変更で更新 → `_claude/rules/mutation-verify-new-tests.md`「復元の作法」の直前 +
      `_claude/skills/codex-drive/SKILL.md` の `[3.8]`

## 未確定 (着手前に決めること)

- **in-place と copy-then-mutate のどちらを採るか** (上の「設計の出発点」節)。後者を採れば
  「未コミットの実装をどう変異させるか」という衝突自体が消える
- **残骸ゼロの確認を汎用版でどう作るか**。参考実装は fixture ディレクトリの snapshot 差分で見ているが、
  汎用版には fixture が無い。代替は `git status --porcelain` の前後比較になり、
  **repo 外の生成物・子プロセスは拾えない**。受け入れ条件「`test_issue_done.sh` より弱くしない」は
  構造的に満たせない可能性があるので、着手時に射程を書き直す

## やらない判断もありうる

- 変異の当て方はタスクごとに違う (sed / python / patch / 旧実装の貼り直し)。汎用化しすぎると
  「当てる」部分は結局その場のコードになり、guard だけが残る。**それでも上の 6 episode は全部
  guard 側の抜け**なので、「当てるのは呼び出し側、guard はハーネス」の分担で足りる見込み
- 🚨 **6 episode は「変異の前に WIP commit する」だけでコード 0 行で消える形** (表の 4 件すべてが
  未コミットの変更を `git checkout --` で消した形)。道具の上乗せ分は「その規律を機械で強制すること」
  であって、5 つの guard 全部が新規価値ではない
- ただし効果は **復元事故 6 episode に限定**される。9/14 の assert 誤り・9/17 の観測設計・9/20 の
  fixture / seam は**この道具では止まらない** (起票時はここを過大に見積もっていた)

## trigger

次に手書きの変異ハーネスを書こうとしたとき。それまで着手しない。

### 🚨 trigger 発火の記録 (2026-09-22、issue 409 の作業中)

**発火した。** 1 セッションで手書きの変異ハーネスを **2 回**書いた (2 回目は 1 回目のコピー):

1. `tmp` (scratchpad) の `mut409.sh` — `apply()` が cp バックアップ / `diff -q` で当たり確認 /
   `go build` で構文確認 / ケース名で red 判定 / 復元、を手書き
2. 直後の別 commit で、同じ形を Bash の inline 関数 `mut()` として**書き直した**

**この issue が共通化しようとしている前提検査で、同じセッション中に 2 回つまずいた**:

- **判定 grep が `^--- FAIL` でサブテストを取りこぼした**。親テスト名だけ見て「red になった」で
  閉じかけた (`_claude/rules/mutation-verify-new-tests.md` が名指ししている罠そのもの)。
  `^[[:space:]]*--- FAIL:` で採り直して、初めてケース単位の検出力が見えた
- **変異がビルド不能になった** (検証ブロックを消すと `slices` が未使用になる)。ルールが要求する
  「red でも green でもない第 3 の結果」として扱い、`if false &&` の形へ当て直した。
  ハーネスに `go build` の段を入れていたから捕まえられたが、**入れ忘れれば stale build の緑を
  「変異を検知できないテスト」と誤診していた**

どちらも「毎回手書きするから毎回同じ検査を書き忘れうる」形で、この issue の前提を裏づける。
**着手条件は満たされた** (状態の遷移は未実施 — pending のまま置いてある)。

## 関連

- `tests/issues/test_issue_done.sh` (参考実装。一般化の出発点)
- `scripts/with_fresh_worktree.sh` / `~/.claude/skills/codex-drive/SKILL.md` の `[3.8]`
- `_claude/rules/mutation-verify-new-tests.md` (規範の正本。本 issue はその実装)
- `_claude/rules/adversarial-review-own-safeguards.md` / `_claude/rules/new-tool-requires-entrypoint-docs.md`

## 進捗 (2026-09-22)

### 決めたこと: worktree 方式 (copy-then-mutate は一般形にならない)

本文が「未確定」に挙げていた **in-place vs copy-then-mutate** は、**実測で copy 案が落ちた**:

| 対象 | 一時ディレクトリへ `cp -R` して `go build` | 
|---|---|
| `src/lockman` (単独 module) | rc=0 (通る) |
| `src/doctor` (相対 `replace => ../termsafe`) | **rc=1** `termsafe@v0.0.0: replacement directory ../termsafe does not exist` |

dotfiles の Go module 6 件のうち 2 件 (`glogx` / `doctor`) が相対 `replace` を持つので、
**fake root は一般形にならない**。worktree なら全 module で成立するので worktree に一本化した。
未コミットの変更は `git diff HEAD --binary` + `git apply` と untracked の cp で持ち込む
(これが無いと「今書いたテスト」を検証できず、in-place を避けた意味が半分消える)。

### 射程の書き直し (本文が宿題にしていた分)

**止めるもの**: 復元事故 (作業ツリーを触らないので原理的に起きない) / baseline が red / zero execution /
当たっていない変異 / 誤ファイルへの変異 / 構文エラーの変異 (第 3 の結果) / 緑のまま通る変異 /
別の検査が落ちた red。

**止めないもの** (スクリプト冒頭にも明記):
- **行レベルの誤変異** — diff は必ず表示するが機械照合はしない (変異の設計そのものなので呼び出し側の責任)
- **assert の誤り / 観測設計 / fixture・seam の問題** — 起票時に過大評価していた部分
- **repo の外に書かれた生成物・子プロセス** — worktree の中は消すが、検証コマンドが `$HOME` や
  `/tmp` へ書いたものは追えない。本文が「構造的に満たせない可能性がある」と予告していたとおり

### 自律改善: worktree の掃除ロジックを共通 lib へ

`scripts/with_fresh_worktree.sh` が持っていた stale sweep / 3 回リトライの rm / 子プロセス kill を
`scripts/lib/worktree_scratch.sh` へ抽出し、両者が source する形にした (同じ規律を 2 箇所に書けば
片方が必ず腐る)。`with_fresh_worktree.sh` の実動作も確認済み。

### dogfooding で道具が自分の欠陥を 2 回検出した

1. **拡張子なしのファイルで `--syntax` を推定できず、自分自身を検証できなかった**
   (`bin/` のスクリプトは全部拡張子なし) → shebang を見る形に修正
2. その修正で self-test のケースが古くなったのを、**baseline が red** として検出

### 変異検証 (この道具自身で実施。7 本すべて red)

| 外した guard | 期待した self-test の NG |
|---|---|
| zero execution 検査 | `NG: zero execution` |
| 変異が当たったかの検査 | `NG: 当たらない変異` |
| 構文検査 | `NG: 構文エラーの変異` |
| 誤ファイル検査 | `NG: 誤ファイルへの変異` |
| `--expect` 照合 (red の帰属) | `NG: 別の検査が落ちた red` |
| baseline rc 検査 | `NG: baseline red` |
| 緑のまま通る検査 | `NG: 緑のまま通る変異` |

残骸: worktree 0 件 / TMPDIR 0 件 / 作業ツリーの git state 不変。

### 敵対的レビュー 1 周目 (opus の red team) — P1 は 3 件とも成立、全部直した

🚨 **1 周目の最大の収穫は「射程 1 の宣言が実態と違った」こと**。「復元事故は構造的に起きない」と
書いていたが、`--apply` は eval なので元 repo の絶対パスを書けてしまい、**警告 1 行を出して rc=0** を
返していた。自分で書いた宣言を自分で検証していなかった形。

| 指摘 | 実態 | 対応 |
|---|---|---|
| **P1-1** 誤ファイル guard が dirty / untracked を検出しない | `git status --porcelain` の**行**を比べていたので ` M x` → ` M x` は内容が変わっても差分が出ない。**この道具が未コミット差分を持ち込んで自分で作る集合**がまるごと死角 | パスごとの**内容 hash** を撮る `snapshot_tree()` へ |
| **P1-2** `--apply` が worktree の外を書いても rc=0 | 射程 1 の宣言が嘘になっていた | 成功パス直前に確認し、変わっていたら **rc=9** |
| **P1-3** `--expect` が baseline にも出ると帰属チェックが素通し | `FAIL: TestX` から 1 文字削って `TestX` にした瞬間に入る (verbose runner では常態) | baseline 出力に出ていたら **rc=2 で拒否** |
| **P2-4** 除外が部分一致 | `sub/guard.sh` / `guard.sh.bak` を「--file 自身」と読む | `grep -vxF` (完全一致) |
| **P2-5** untracked な `--file` の diff が空 | 新規テストファイルが主要ユースケースなのに手順 1.6 の入力が無音で消える | 変異前に `git add -N` |
| **P2-7** self-test の残骸チェックが TMPDIR 全域を見る | **並行実行中の他 run を「残骸」と誤検出** (dotfiles は並行前提) | この run で増えた分だけを見る |
| **P2-8** lib の `\| grep -q` | 「判定不能を緑にしない」ための最後の警告が pipefail で反転しうる形だった | ヒアストリングへ |
| **P2-9** macOS で `pkill` が当たらない | `TMPDIR` は `/var/folders/…/T/` (末尾 `/` + symlink) で、`git worktree list` が返す `/private/var/…` と文字列が一致しない | `pwd -P` で正規化 |

**self-test を 12 → 17 ケース**に増やし、red team の再現手順をそのまま固定した。
新ケースの検出力は**変異 5 本**で確認 (いずれも狙ったケースだけが red):

| 変異 (修正前の実装へ戻す) | red になったケース |
|---|---|
| `snapshot_tree` の内容 hash を定数へ | `dirty-tracked な別ファイル` |
| 除外を部分一致へ戻す | `同名 basename` |
| `worktree_touched` の rc=9 判定を外す | `worktree の外への書き込み` |
| baseline への `--expect` 照合を外す | `baseline にも出る --expect` |
| `git add -N` を外す | `untracked な --file の diff` |

🚨 **この変異検証でも 1 回つまずいた**: perl の `s///` に `$d` を含む置換文字列を書いて
**perl 変数として展開され空になり**、変異が意図と別の形で当たった (rc=7 = 別の検査が落ちた)。
道具の帰属チェックが教えてくれたので気づけたが、**今日の午前に self-test の fixture で踏んだ罠の再発**。
変異はファイルに書いて python で当てる形に変えた。

### 敵対的レビュー 2 周目・3 周目 — 「宣言 vs 実装」の食い違いが繰り返し出た

| 周 | P1 | 主な内容 |
|---|---:|---|
| 1 | 3 | 射程 1 の宣言 (「復元事故は構造的に起きない」) が実態と違った / 誤ファイル guard の死角 / `--expect` の帰属 |
| 2 | 5 | **1 周目の修正が作った穴** (index を見なくなった / `core.quotePath` で非 ASCII が不可視) + gitignore・`.git` |
| 3 | 2 | **2 周目に塞いだはずの指摘の再発 2 件** (`--verify` 後の exit が `finish` を通らない / self-test の残骸判定位置) |

**3 周目で直したもの**:

- `--verify` が走った後の baseline 系 3 経路が素の `exit` のままだった。rc=3 は呼び出し側に
  **再実行を最も強く促す** rc なので、2 周目 P2-2 と同じ被害ループが残っていた
- self-test の残骸判定を**本当に**末尾へ (「末尾に置く」とコメントに書きながら、その後に新ケースを
  3 つ判定より前に足していた)。「ケースはこのブロックより上に足せ」を明記
- **rename のパース**: `R  <new>\0<old>\0` の 2 レコードを 1 エントリとして読んでいた
  (2 周目 P1-5 で直した「恒久的に不可視」の別入口)
- **`--file` の表記ゆれ**: macOS は既定で case-insensitive なので `GUARD.SH` が `[ -f ]` を通り、
  除外だけ外れて**自分自身を誤ファイル扱い** (rc=8) にしていた。`:(icase)` の magic pathspec で
  git の表記へ揃える (🚨 `--` の後ろの pathspec は `core.ignorecase=true` でも case-sensitive。実測)。
  symlink の `--file` は拒否 (変異は実体に当たるので除外とずれる)
- `--help` が終了コード表を出していなかった (この道具の契約は終了コードなので、宣言に到達できない状態)
- **宣言と実装の食い違いを 5 件**: symlink / submodule / 改行入りパス / entry が失われた残骸 /
  「rc=9 は並行編集でも出る」を「何を止めないか」へ追記

🚨 **自分で書いたテストが何も守っていない形を、この周でも 1 件作った**。ケース 23 (rename) は
「rename があっても誤検出しない」しか見ておらず、読み飛ばしを外す変異が**緑のまま通った**
(誤パースは before/after の両方に同じゴミ行を作るので判定が反転しない)。診断 (rc=9 の diff) を
観測点にする形へ書き直して変異 red を確認した。

self-test は **12 → 25 ケース**。`make test` rc=0、self-test も `[ok] tests/bin/test_mutate_verify.sh`
として集約経路から実行されている。

### 残タスク

- [ ] **4 周目を回すかの判断** (§7「修正を入れた周は必ずもう 1 周」)。3 周目の修正は
      パースロジックの新設・`:(icase)` での正規化・symlink 拒否を含むので、§7 の打ち切り例外
      (「判定ロジックを新設せず」) は満たさない
- **収束の傾向**: P1 は 3 → 5 → 2 と減っているが、「宣言 vs 実装の食い違い」が毎周出ている。
  §8 の判定 (「直すたびに新しい迂回が出るなら軸を変える」) に照らすと、**射程をさらに絞るか、
  宣言を実装に追従させる検査を機械化する**のが次の手
