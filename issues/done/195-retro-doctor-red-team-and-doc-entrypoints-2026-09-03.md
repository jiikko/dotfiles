# 195 retro: doctor の red team 起票 → 個別着手 → ドキュメント入口の整備 (2026-09-02 夜〜09-03)

起票日: 2026-09-03

## このセッションでやったこと

1. issue 163 の red team (6 観点) を起動 → **全体が session limit で途中終了**。残骸から 17 件起票 (167-183)、163 を索引化
2. issue 179 (svc 注記の CLI/UI 不一致) を `svc.Annotations` への一本化で解決
3. issue 170 (vacuous なテスト 11 箇所) を修正、変異 12 本で red を確認
4. retro 164 の項目 2 / 5 をルールへ切り出し
5. `tmp/` の掃除 (78 エントリ / 588MB)、`make clean-tmp` 新設、issue からの tmp 参照を是正
6. issue 190 (`src/parallel-each/README.md`)、`docs/README.md`、`rules/README.md` を新設
7. CI 赤 (`next-claim-push` のテスト 9 件) を修正
8. doctor 機能に特化した CI レーン (`doctor.yml`) を新設

## 反省・気づき (切り出し先を提案。実行はユーザーの判断待ち)

### 1. 出力をパイプで捨てて、失敗の原因を自分で消した

`make test 2>&1 | tail -25` で回し、`test-lint` の失敗行が**スクロールの上で捨てられた**。
ログに残ったのは「✗ 失敗したターゲット: test-lint」だけで、何が落ちたか分からない状態になった。
全ログを取り直して再実行したら green で、**1 回目の失敗は原因不明のまま**。

同じセッションで**もう 1 回**踏んだ: 変異検証で `bash step.sh | tail -7; echo "rc=$?"` と書き、
`$?` が `tail` のものになって「変異を当てたのに rc=0」と読んだ。分離して測り直したら rc=1 で正しく red だった。
⚠️ **この 2 つ目は、自分が同じ日に CI へ入れた gate が守ろうとしている罠そのもの**。

**切り出し済み** (2026-09-03): [`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
に 1 項追加。既存は「`cmd | tail` の `$?` はパイプ終端の status」だけで、**失敗ログ自体を捨てる形**が
無かった。足したのは「検証コマンドの出力を `tail` / `head` / `grep` で削って読まない。ファイルへ落として
から読む」。理由として **集約 target は結論行を最後に出す設計が多い**ので `| tail -n` が
「結論だけ残して理由を捨てる」形になりやすいこと、捨てた後に再実行して緑ならもう追えないことを書いた。
実例 2 件は rationale へ。

### 2. 既存ドキュメントの記述を引き写して、事実誤認を 3 件書きかけた

`src/parallel-each/README.md` を書くとき、既存の `docs/glogx-bubbletea-v2.md` の記述を引き写した。
実コードで裏を取ったら 3 件ずれていた:

| 書きかけた内容 | 実測 |
|---|---|
| `Runner` は 30 以上のフィールド | 25 |
| retry の区切りは `=== retry N/M ===` | `=== retry N/M (previous exit=…) ===` |
| 3 モジュールが**同じ** charm 依存を独立に持つ | v2 と v1 で**モジュールパスから違う** (`charm.land/bubbletea/v2` と `github.com/charmbracelet/bubbletea`) |

3 つ目は元の文書 (`docs/glogx-bubbletea-v2.md`) 側が今も不正確。**引き写した先だけ直した**ので、
出典側は誤ったまま残っている。

**切り出し済み** (2026-09-03): 出典側 (`docs/glogx-bubbletea-v2.md`) を実測に直した。
「同じ charm 依存の版がずれている」ではなく **モジュールパスから違う** ことを明示し、
3 モジュールの実測表 (bubbletea / x/ansi / lipgloss) を入れた。
`go get -u` で片方を上げてももう片方は動かないこと、同じ v2 どうしの glogx と schedkeys も
版がずれている (2.0.8 / 2.0.9) ことも書いた。**新規ルールは作らない**
(CLAUDE.md「ぼやきも事実の主張なら裏を取る」が既に一般形を持つ)。

### 3. 「ローカルにだけある状態」で CI が落ち、手元で再現しなかった

CI の Tests が 9 件落ちた。原因は `issues/next/` が**空ディレクトリなので git に載らず**、
新品チェックアウトと CI には存在しないこと。hook はその実在を opt-in ガードにしているので、
テストが全件 no-op になっていた。手元では過去の作業で残った `issues/next/` があり再現しなかった。

これは `_claude/CLAUDE.md` が `tmp/` について書いている注記
(「ignore は `~/.gitignore_global` 由来で、新品チェックアウトと CI では存在しない」) と**同型**。
issue 132 が「repo 内に `tmp/` が存在する」問題を扱っているのも同じ根。

**切り出し済み** (2026-09-03): [issues/132](132-feat-detect-ci-only-preconditions-before-push.md) の
事実の表に 3' として追記した。併せて **「gitignore されているか」ではなく「git に載っているか」で
考えないと同型を取りこぼす** ことを書いた (空ディレクトリ / ignore / untracked のどれでも起こる。
`tmp/` は ignore 由来、`issues/next/` は空ディレクトリ由来で、原因が違うのに壊れ方は同じ)。
なお `issues/next/.keep` が置かれて追跡対象になったので、この個別ケース自体は解消している。

### 4. 6 体並行起動で session limit に全滅させた (164 項目 1 の再演)

issue 163 の本文に自分で「枠の残量が少ないと途中で落ちるので、**枠が開いた直後に起動する**」と
書いていたのに、残量を見ずに 6 体起動して全体を落とした。retro 164 の項目 1 が同じ反省を
未切り出しで持っていた。

**切り出し先**: 対応済み。別マシンのセッションが
[`subagent-model-tiering.md`](../_claude/rules/subagent-model-tiering.md) へ切り出し、
done/164 に「6 体並行で全滅した実例も出た」として記録済み。**この項目は記録のみ**。

⚠️ ただし**残骸から拾えた**のは幸運だった (体 1/2/6 は報告を書き終えており、体 4 は実測ログ、
体 5 は証跡を残していた)。体 3 だけ成果ゼロで、後で単独で走らせ直した。
「並列数を絞る」の実利は**やり直しコストの回避**だと分かった形。

### 5. 並行セッションとの衝突に手数を使った (番号衝突 / push reject 3 回 / merge 失敗)

- issue 番号が衝突した (167/168 を両方のセッションが使った)。相手が 184/185 へ採番し直して解消
- `git push` が 3 回 reject され、うち 1 回は merge も競合して失敗した
  (相手の staged な rename が、remote から来る同じパスとぶつかった)
- 最終的に **worktree で cherry-pick して push** する形で回避した (共有 tree を触らずに済む)

セッション中に `issues/next/` への claim の仕組みが導入された (別マシンのセッションが実装) が、
**自分は最後まで使っていなかった**。使っていれば番号衝突の一部は防げた。

**切り出し先案**: 却下でよい。規範は
[`claim-issue-in-next-and-push.md`](../_claude/rules/claim-issue-in-next-and-push.md) に既にあり、
今日から hook も動いている。**次のセッションで実際に使うかどうかの問題**で、ルールを足す話ではない。
ただし「番号の採番も claim と同じ問題を持つ (起票時点の最大 + 1 は競合する)」のは規範に無い。
気になるなら別 issue。

### 6. 偽環境の作り方を 2 回間違えた (PATH を絞るだけでは不在にならない)

doctor の CI レーンで「依存コマンドが無い環境」を作るとき、`PATH=/usr/bin:/bin` に絞れば
`xcrun` が不在になると考えた。実際は **`/usr/bin/xcrun` が実在して成功する**ので、
`simulator-runtimes` が `ok` のままになり、検査が何も確かめない状態だった。
必ず失敗する偽物を PATH 先頭に置く形へ直した。

**切り出し先**: 却下。[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
節 1 の表が「**依存コマンドが失敗する (偽の実行ファイルを PATH 先頭に置く)**」と既に正しく書いている。
自分がそれを読まずに「PATH を絞る」方を選んだだけ。ルールの不足ではない。

### 7. 「シュッとできる?」に対して、割れる部分を先に切り分けたのは効いた

issue 180 は「軽い方」と「型変更が要る重い方」に割れていた。見積もりの時点で分けて示し、
ユーザーが「軽い方だけ」と決めた。**保留した側は理由と再開の trigger をコードと issue の両方に残した**。

**切り出し先**: 却下 (うまくいった話なので retro の対象外)。記録のみ。

## 残課題

**すべて決着済み (2026-09-03)**。この retro は `issues/done/` へ移してよい。

- [x] 項目 1 の切り出し → `verify-execution-not-just-exit-code.md` に「出力はファイルへ落としてから読む」を追加
- [x] 項目 2 の切り出し → `docs/glogx-bubbletea-v2.md` を実測 (3 モジュールの表) に直した。新規ルールは作らない
- [x] 項目 3 の切り出し → issue 132 に 3' として追記 (「git に載っているか」で考える、を併記)
- [x] 項目 4 — 対応済み (別セッションが `subagent-model-tiering.md` へ切り出し、done/164 に記録)
- [x] 項目 5 — 却下 (規範と hook は既にある。使うかどうかの問題)
- [x] 項目 6 — 却下 (`adversarial-review-own-safeguards.md` 節 1 に既にある)
- [x] 項目 7 — 却下 (うまくいった話)
