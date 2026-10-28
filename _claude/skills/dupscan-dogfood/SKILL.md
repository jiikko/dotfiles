---
name: dupscan-dogfood
version: 1.0.0
description: dupscan を実際に使って開発タスクをこなし、使って困ったこと (見逃し・雑音・出力が大きい・遅い・grep に戻した理由) を記録して、dotfiles の issue に書き、直せるものは dupscan 自身を直す。「dupscan を使って開発して」「dupscan を使いながら実装して、フィードバックを issue に」「dupscan-dogfood」で発火。タスクが目的で、dupscan の評価・改修は副産物。dupscan を直すだけの依頼や、使わないタスクには使わない。
---

# dupscan-dogfood (使って困ったことを issue に残し、直す)

開発タスクを **dupscan を使って**こなし、その途中で見つけた dupscan の不満を記録して、issue に書き、小さく直せるものは直す。
タスクを終わらせることが先で、dupscan の改修で依頼の範囲を広げない。使い方の正本は skill dupscan (`~/.claude/skills/dupscan/SKILL.md`)。
これまでの経緯と測定は dotfiles の `issues/epic/932/` (946: 実在の module で見つけた取りこぼし / 948: 狙いが成り立っているかの判定)。

## 0. 始める前に (3 分)

1. **dotfiles の epic 932 の現状を読む**: `issues/epic/932/932-epic-dupscan-accuracy.md` の表と、直近の `946` / `948` の末尾。既に起票・対応済みの不満を、また起票しない。
2. **作業 dir を作る**: `d="$(skill-tmpdir dupscan-dogfood)"` (`<本体 checkout>/tmp/dupscan-dogfood/<session-id>/`)。摩擦のメモは `$d/friction.md` に書く。
3. **dupscan が動くか 1 回確かめる**: `dupscan capabilities` (build に時間がかかる初回は待つ)。動かないこと自体が最初の不満になる。
4. **他に dupscan を触っているセッションがいないか**: `ListAgents`。`src/dupscan` を直すのは 1 セッションだけ (衝突を避ける)。いれば、直すのはそのセッションに任せ、こちらは記録と起票だけにする。

## 1. タスクを dupscan を使ってこなす

skill dupscan の 1〜4 節に従う。**grep より先に dupscan を試す場面**を決めて守る (でないと「使ったが不満が出なかった」のか「使わなかった」のか区別できない):

| 場面 | 使う |
|---|---|
| 新しい関数・helper・テストの fixture を書く前 | `dupscan index -name / -returns / -param-type`、名前が分からなければ `search -query` |
| 関数を置き換える・消す前の呼び出し元 | `dupscan callers -limit 0 <名前>` (打ち切りで取りこぼさない) |
| 定義・参照を探す (Swift) | `def` / `refs` / `context` |
| 実装が終わったとき | `dupscan gate` (commit の前) |
| 初めて入る module・まとめて重複を寄せる前 | `dupscan sweep <dir>` |

- 文字列・コメント・設定ファイル・ログ・定義 1 件を狙い撃ちする検索は grep / rg でよい (それは不満ではない)。
- **0 件・少なすぎる結果を「無い」と読まない**。dupscan は型を解決しない。疑わしければ grep で突き合わせ、食い違ったら記録する。
- 寄せるときは skill dupscan の 4 節 (ドメインの境界) を先に確かめる。

## 2. 摩擦を、その場でメモする (後でまとめない)

`$d/friction.md` に、困った瞬間に 1 件ずつ追記する。まとめて思い出すと、出力の大きさや rc のような数字が消える。

```markdown
## F1 <一行で何が困ったか>
- 場面: <何をしようとして、どのコマンドを打ったか (コマンドは一字一句)>
- 期待: <出てほしかったもの>
- 実際: <出たもの。出力の byte 数 (`| wc -c`)・件数・所要・rc>
- その後: <grep に切り替えた / limit で絞った / 諦めた / 別の語で引き直した>
- 種類: 見逃し(あるはずが出ない) / 雑音(無関係が多い) / 出力が大きい / 遅い / 使い方が分かりにくい / 未対応(Go の def など) / 打ち切りに気づかなかった / 文書とずれ / その他
```

- **dupscan が役に立った場面も 1 行ずつ残す** (「重複を 1 件寄せられた」「呼び出し元の取りこぼしを防いだ」)。直す優先度の判断と、効果の主張に要る。
- **grep に切り替えたときは必ず理由を書く**。理由が「dupscan を思い出せなかった」でも書く (入口の問題として重要)。
- 数字は測って書く。推測は「推測」と書く。

## 3. 終わったら集計する

1. `friction.md` の各件を、既存の issue と突き合わせる (`grep -rn '<語>' issues/epic/932/`)。**同じ指摘が既にあれば、新規に起票せず、そこへ追記する** (出力の大きさや再現手順などの新しい証拠を足す)。
2. 集計を `friction.md` の先頭に書く: 件数、種類ごとの内訳、dupscan を使った回数と grep に戻した回数、役に立った場面の数。
3. dupscan 自身が直せる不満かを仕分ける:

| 仕分け | 条件 | 行き先 |
|---|---|---|
| その場で直す | 再現でき、直し方が小さく、タスクの依頼を邪魔しない (README の記述ずれ・usage の欠落・雑音を減らす小さい条件など) | 4 節 |
| issue にする | 直し方が大きい・設計の判断がある・測定が要る | 5 節 |
| 見送る | 一回きりで再現しない・好みの問題・dupscan の作りの限界 (型解決など) | 記録だけ (理由を 1 行) |

## 4. 直す (dupscan の修正)

dupscan の修正の作法 (dotfiles の `CLAUDE.md`「`src/` の Go を書くとき」):

- **dupscan の修正は dotfiles の repo (`~/dotfiles`) で行う**。開発タスクが別の repo (obaket など) でも、dotfiles の worktree を切って作業する (タスクの repo には dupscan の修正を混ぜない)。共有の checkout で編集しない。`src/dupscan` を触るセッションは 1 つだけ (0 節の 4)。
- テストを書き、**変異を当てて赤を確認する** (`~/.claude/rules/mutation-verify-new-tests.md`)。結果を変えない最適化の変異は緑のままでよい (そう書く)。
- `make -C src/dupscan test lint` と、`make test-lint` を rc で確かめてから push する。文書 (README・skill・usage) が実体とずれたら同じ commit で直す。
- 実機で再現した入力に対して、直す前後の出力を並べて確かめる (テストが緑でも、実物で効いたかは別)。
- 直した内容と確かめた方法は commit message に書き、issue にも 1 行足す。

## 5. issue に書く

- 置き場所: **dotfiles の** epic 932 の下 (`issues/epic/932/`)。開発タスクが別の repo でも、dupscan の不満は dotfiles の issue に書く (タスクの repo の issue には書かない)。種類が `research` / `feat` / `ux` / `perf` のうち近いもの。dotfiles の issue 規約 (採番を `git fetch` して数える・採番したら即 push・`next/` の claim・反証レビュー) に従う。
- 1 つの issue に、摩擦を **場面・期待・実際・数字・提案** の順で書く。実際の出力は短く引用する。
- 既存の issue への追記は、日付つきの節で足す (946 の「実装後の再測定」のような形)。
- **起票・追記したら、`friction.md` の各件に issue 番号を書く** (どれがどこへ行ったか追えるように)。
- 反証レビューを通す (issue 規約。codex が使えない環境では観点を分けた read-only のサブエージェント)。起票した issue が事実と食い違っていた指摘は直してから push する。

## 6. 報告する

```
## 開発タスク
<結果。依頼の本題を先に>

## dupscan のフィードバック (件数は friction.md を数えて書く)
| 種類 | 件数 | 対応 |
|---|---|---|
| 見逃し | 2 | 1 件を修正 (commit)、1 件は 946 に追記 |
...
dupscan を使った回数 N / grep に戻した回数 M (戻した理由の内訳)
役に立った場面: ...
起票・追記した issue: 948 (追記)、959 (新規) ...
直せなかったもの・見送ったもの: ... (理由)
```

- **効果の主張は、測ったものだけ**。「重複を防げた」と言えるのは、防げた具体的な場面を示せるときだけ。無ければ「使った・困らなかった」と書く。
- 作業 dir (`$d`) は、issue へ結論を移したら消してよい (dotfiles の `_claude/issue-rules.d/move-report-conclusions-to-issues.md`)。

## 関連

- skill `dupscan` — 使い方の正本 (書く前に引く・gate・境界)。この skill はそれを使って摩擦を集める。
- `issues/epic/932/948-research-dupscan-meets-its-aims.md` — 狙いが成り立つかの判定表。効果の指標 (入口の使用率・注入の回数) はここに追記する。
- `scripts/dupscan_usage_report.py` — transcript から dupscan と grep の回数を数える (他のセッションを含む全体の測定。dupscan を開発したセッションは既定で除く)。
