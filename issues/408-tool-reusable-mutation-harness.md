# 408 (tool): 変異検証の前提検査を共通化する — 参考実装は `tests/issues/test_issue_done.sh`

起票日: 2026-09-20 (出典: obaket 872 の retro 項目 6 の切り出し検討)
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
  `bash -n` で構文を見て、期待したキーワードで red を確認し、snapshot で全復元する。
  **本 issue が欲しい guard のほぼ全部が、issue-done 専用の形で既に実装されている**。一般化の出発点はここ
- **`scripts/with_fresh_worktree.sh`** — worktree の作成と cleanup
- **`~/.claude/skills/codex-drive/SKILL.md` の `[3.8]`** — worktree / 変異適用 / 実行 / 復元 / 結果表 / spot check の手順。
  軽量経路では `cp` 1 世代の backup を明示的に許可している

→ 新規性は「**`bin/` の汎用 CLI として切り出すこと**」だけ。`[3.8]` のフロー全体を新しく機械化する話ではない。

## やること (案)

`tests/issues/test_issue_done.sh` の `check_mutant` を **repo 非依存に一般化**して `bin/` へ出す。
入口で必ず強制するもの:

1. **`git status --porcelain` が空でなければ拒否** (`--allow-dirty` は置かない。抜け道は事故の再生産)
2. 変異を当てた後、**当たったこと**を確認 (`diff -q`)
3. 構文検査 (拡張子から言語を決める)。通らなければ **第 3 の結果** (red でも green でもない)
4. `--verify` の結果を **rc と実行件数の両方**で記録。実行 0 件を green にしない
5. 復元と、残骸ゼロの確認。**trap は 1 本に束ねる**

## 受け入れ条件 (レビューが指摘した fail-open を含む)

- [ ] `bin/<名前>` と self-test が在る
- [ ] self-test が固定する (🚨 **`test_issue_done.sh` より弱くしない**):
  - [ ] dirty で拒否する
  - [ ] **`diff -q` が「何かが変わった」で通ってしまう形**を塞ぐ — 誤ファイル / 誤行 / コメントだけの変更を
        「当たった」と判定しないこと (当て先の行を指定させるか、変異後の内容を照合する)
  - [ ] **実行件数の取得契約**を決める — 任意コマンドを渡せる設計のままでは zero execution と green を
        機械的に区別できない (runner ごとのサマリ行の読み方を渡させる)
  - [ ] 残骸ゼロの確認が **untracked / 生成物 / 子プロセス**を取りこぼさないこと (`grep -c MUTANT` だけにしない)
  - [ ] baseline green / 期待した test case の失敗への帰属 / `git status` 自体の異常 /
        対象 repo と cwd の不一致 / SIGKILL / 復元失敗
- [ ] 🚨 **自作の安全機構なので敵対的レビューを最終ゲートにする** (`adversarial-review-own-safeguards.md`)。
      攻め口は「拒否したつもりで素通りする経路」
- [ ] 入口のドキュメントを同じ変更で更新 (`new-tool-requires-entrypoint-docs.md`):
      `_claude/rules/mutation-verify-new-tests.md`「復元の作法」から 1 行 + `codex-drive` の `[3.8]`

## 未確定 (着手前に決めること)

- **未コミットの実装をどう変異させるか**。dirty 拒否と「開発中コードを検証したい」は正面から衝突する。
  `[3.8]` は「worktree へ patch を持ち込み **baseline commit を作ってから**変異させる」設計なので、
  それを道具側に内蔵するのが筋。CLI 仕様に baseline commit / 対象 repo / 複数ファイル / 復元失敗時の
  結果状態が無いまま作ると、dirty 拒否を足しても普通の開発フローで使えない (P3 指摘)

## やらない判断もありうる

- 変異の当て方はタスクごとに違う (sed / python / patch / 旧実装の貼り直し)。汎用化しすぎると
  「当てる」部分は結局その場のコードになり、guard だけが残る。**それでも上の 6 episode は全部
  guard 側の抜け**なので、「当てるのは呼び出し側、guard はハーネス」の分担で足りる見込み
- ただし効果は **復元事故 6 episode に限定**される。9/14 の assert 誤り・9/17 の観測設計・9/20 の
  fixture / seam は**この道具では止まらない** (起票時はここを過大に見積もっていた)

## trigger

次に手書きの変異ハーネスを書こうとしたとき。それまで着手しない。

## 関連

- `tests/issues/test_issue_done.sh` (参考実装。一般化の出発点)
- `scripts/with_fresh_worktree.sh` / `~/.claude/skills/codex-drive/SKILL.md` の `[3.8]`
- `_claude/rules/mutation-verify-new-tests.md` (規範の正本。本 issue はその実装)
- `_claude/rules/adversarial-review-own-safeguards.md` / `_claude/rules/new-tool-requires-entrypoint-docs.md`
