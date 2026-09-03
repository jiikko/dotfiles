# 077 bug: 協業レビューで生き残った low 4 件 (自作の安全機構・テスト・コメントの瑕疵)

起票日: 2026-08-21
種別: bug
優先度: **P3** (実害はログ 1 行 / 表示の数値 / テストの空回りに限られる。ただし 3 件は自作の安全機構側)

2026-08-21 の協業レビュー (2 セッション 17 commit を 5 観点 + 孤児回収の敵対攻撃で検証) で
生存した 9 件のうち、**修正済み 5 件を除いた low 4 件**。レポートは `./tmp/collab-review.md` に
出したが `tmp/` は gitignore 対象で残らないため、必要な情報をここへ移した。

共通の性質: **4 件のうち 3 件は今回の協業で自分が新設した安全機構・テスト・コメント側の瑕疵**
(`adversarial-review-own-safeguards.md` が予測した型)。

---

## 1. `tt_archive_finalize` のコメントが実装より強い (reject 経路だけ呼んでいない)

`scripts/tmux_resurrect_save.sh` — コメントは「🚨 tt_save_main の**全 return 経路**から呼ぶこと」
と宣言しているが、reject 経路 (退行を検知して last を戻す経路) は呼んでいない。

- **振る舞いは範囲前と同一。コメントの宣言だけが新規** (`bd8e5a7` = 私の commit)
- 同じ defect を 3 観点が挙げ、medium 枠の 2 件は反証で却下され、この「コメント精度の瑕疵」だけが
  生存。「データが壊れる / 観測が失われる」という強い主張は成立しなかった
- 実験で確定 (隔離ハーネス・本番非関与): archive の状態が同一でも reject 経路では
  `archive-broken` が出ず、対照の通常経路では出る
- 縮小方向の訂正 2 点: `tt_save_main` の 8 個の return のうち finalize を呼ばないのは 6 個だが、
  5 個は `tt_archive` 代入より手前なので finalize は no-op = **観測可能な違反は 1 箇所だけ**。
  影響も薄い (reject 経路は `mv -f bak → archive` で退避を消費済みなので、finalize を呼んでも
  修復はできない。失われるのは `archive-broken` のログ 1 行と、mv 自体が失敗した稀なケースの修復機会)

**最小修正はどちらかに寄せる**: (a) reject 経路の return 直前に finalize を挟む /
(b) コメントを「退避コピーがまだ残っている経路から呼ぶ」に弱め、reject 経路が対象外の理由
(bak を mv で消費済み) を 1 行添える。**選んだ側をテストで pin する**こと。

既存テストの被覆: `test_resurrect_save_archive.sh` は escape hatch のみ構造 pin。
`test_resurrect_save_lock.sh` の fixb_regress は archive ロールバック自体は pin しているが、
検証・ログの有無は assert していない。

## 2. `human-tasks-due.sh` の「(うち期限に余裕あり N 件)」の母集団がずれている

`_claude/hooks/human-tasks-due.sh` — `unread` は「human かつ pending 以外」だけ数えるのに、
`later` はカテゴリも pending も問わず加算する。「うち」と書いているのに部分集合になっていない。

- 決定的な再現 (非 human を 1 件も置かず、README 規約だけを守った形):
  `未完了の human タスク issue: 1 件 (うち期限に余裕あり 2 件)` が出る
- この経路は pending を走査に加えた変更で生まれた (unread 側は `[ -z "$held" ]` で pending を
  除外したまま、later の母集団だけが広がった)
- 上流ガード無し (hook を参照するのはテスト・`settings.json`・CLAUDE.md の 3 つで、テスト 7 観点は
  `later` を 1 つも assert していない)。`grep -rn "余裕あり"` と `git log -S` で意図の裏付け 0 件
- **影響の訂正**: 現在の実 repo では open な human issue が 0 件なので今は parenthetical 自体が
  印字されない (「セッション開始時に毎回入る」は今日時点では成立しない)

**最小修正**: `later` の加算を `unread` と同じ母集団に揃える、または「うち」をやめて別行にする。
fixture 1 本 (human 1 件 + 遠い期限の非 human) で pin できる。

## 3. `test_human_tasks_due.sh` の観点 7「git 管理外で黙る」が空回り

`tests/claude/test_human_tasks_due.sh` — 渡している cwd が `/` なので `/issues` も無く、
**後段の「issues/ が無ければ exit 0」ガードが黙らせているだけ**。git ガードの有無を区別できない。

- 検証者が変異を 2 本当てて悪化を確認: `[ -n "$root" ] || exit 0` を `root="$cwd"` に変えても、
  `exit 3` (非 git で異常終了) に変えても **どちらも green**。ケースが `2>/dev/null || true` で
  stderr と rc を捨てているため、非 git で異常終了する実装とすら区別できない
- 冗長性も無い: cwd を無視する変異では他観点が 7 件 red になるが、その中に観点 7 は含まれない
- 導入 commit のメッセージは「7 観点」と書くが、変異検証の段落は 6 変異しか列挙しておらず、
  **この観点は変異検証を通っていない** (`mutation-verify-new-tests.md` と issues/061 が禁じる型)
- 本番 hook の挙動自体は正しい (非 git で黙る) ので影響はテストの空回りに限られる

**最小修正 (fake git 不要)**: 対象ディレクトリに `issues/` と human issue を置き、
`GIT_CEILING_DIRECTORIES` で上位への遡上を止める。実 git が非 git 扱いするので、素の hook は
無出力・変異版は出力になり差が出る (検証者が実測済み)。

## 4. 同一パスに socket が再作成されると旧孤児が恒久的に回収対象外になる

`scripts/tmux_reap_orphan_servers.sh` の alive 判定の設計 (範囲前から在った)。
socket ファイルの実在で生存を判定するため、旧孤児が開いていたパスに新しいサーバが socket を
作り直すと、旧孤児は「生存 socket を持つ」と読まれて永久に残る。

- 2026-08-21 に修正した F1 (相対パスを保護側へ倒す) と同じ「パス文字列で同一性を見る」設計の
  帰結。inode や pid 起動時刻での同定に変えないと閉じない
- 実害は「孤児が 1 つ残り続ける」= リソースの微小な浪費。誤殺の向きではない

**trigger**: 孤児回収の同一性判定 (F1 の `case "$s" in /*)` 周辺) を次に触るとき。単独で
inode 同定へ作り替えるのは変更が大きく、誤殺側の risk を新たに作るので**今はやらない**。

---

## 着手順の目安

3 (テストの空回り) → 2 (表示の母集団) → 1 (コメントか実装かを決めて pin) → 4 (trigger 待ち)。
3 は自作テストが主張を守っていない状態なので、他より先に閉じる価値がある。

## 関連

- `_claude/rules/adversarial-review-own-safeguards.md` — 「自分で作った安全機構は自己レビューで
  閉じない」。本 issue の 1〜3 はその実例 (どれも自分が新設した側の瑕疵)
- `_claude/rules/mutation-verify-new-tests.md` — 3 が該当 (変異で緑のまま = 主張を守っていない)
- issues/061 — 「存在の否定を見ていると変異が素通りする」。3 と同型
- issues/070 の「反証・対応の結果」節 — 同じ協業レビューで修正済みの 5 件

## 対応 (2026-08-25 に着手・検証)

**着手したら 1〜3 は既に対応済みだった** (`8c129c7 fix: issue 077 の 3 件`)。issue が open の
まま残っていただけなので、記述を鵜呑みにせず実コードで裏を取ってから閉じる。

| 項目 | 状態 | 裏取り |
|---|---|---|
| 1. finalize のコメントが実装より強い | **修正済み** | (a) 側を採用 = reject 経路 (`regression-stuck-override`) の `return 0` 直前に `tt_archive_finalize` が入っている (`scripts/tmux_resurrect_save.sh:428`)。`tests/zshrc/tmux-session/test_resurrect_save_archive.sh` の観点 5b が構造で pin |
| 2. 「(うち期限に余裕あり N 件)」の母集団ずれ | **修正済み** | `later` の加算に `[ "$is_human" -eq 1 ] && [ -z "$held" ]` が入り `unread` と母集団が揃った (`_claude/hooks/human-tasks-due.sh:102`)。同ファイルに理由の 🚨 コメントあり。`tests/claude/test_human_tasks_due.sh` の観点 6b が pin |
| 3. 観点 7「git 管理外で黙る」が空回り | **修正済み** | `GIT_CEILING_DIRECTORIES` で実 git に非 git 扱いさせる形になり、`issues/` と human issue を置いて後段ガードのマスクも外れている。rc も検査するようになった (`tests/claude/test_human_tasks_due.sh:120-135`) |
| 4. 同一パスの socket 再作成で旧孤児が回収対象外 | **未着手 (意図的)** | issue 本文の判断どおり **trigger 待ち**。単独で inode 同定へ作り替えるのは変更が大きく、誤殺側の risk を新たに作る。孤児回収の同一性判定を次に触るときに再評価する |

### 検証 (テストが主張を守っているか)

- `test_human_tasks_due.sh` (14 観点) / `test_resurrect_save_archive.sh` ともに PASS
- **項目 1 の pin に検知力があることを変異で確認**: reject 経路の `tt_archive_finalize` 呼び出しを
  削ると `test_resurrect_save_archive.sh` が red、戻すと PASS

項目 4 が残るが、これは「今はやらない」と本文が決めている項目なので、残課題としては閉じる
(再評価の trigger は本文に書かれている)。
