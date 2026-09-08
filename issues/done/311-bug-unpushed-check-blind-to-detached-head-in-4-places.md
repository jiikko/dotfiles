# bug: 未 push 判定が detached HEAD を見ない同型が 3 ファイル 4 箇所にあり、正解実装は同 repo にある

起票日: 2026-09-07
カテゴリ: bug
優先度: 高（この repo は detached worktree を**規範として要求**しているので、常時踏む）
出典: /audit broken-code 2026-09-06。2 エージェントが別々の隔離 repo で再現、私が独立に再現

## 何が壊れているか

`git log --branches --not --remotes` は **detached HEAD の commit を 1 件も返さない**。
`--branches` が「ローカルブランチの先端」を指す集合なので、どのブランチにも乗っていない
detached HEAD は最初から母集合の外にある。

隔離 repo での再現（2026-09-06。bare remote + clone + `git worktree add --detach` に 1 commit）:

```
git log --branches --not --remotes --oneline  →  （空）        ← 現行の式
git log HEAD --not --remotes --oneline        →  18a17cf ...
git rev-list HEAD --not --remotes             →  18a17cfd...
```

沈黙ではなく **「未 push は無い」という積極的な結論**を出すのが重い。
retro 266 の「worktree を消して 4 commit が参照なしになった」と同じ失敗クラス。

## 該当（機械で数えた。4 箇所）

```
$ grep -rn -- '--branches --not --remotes' _claude/ scripts/ bin/ zshlib/ src/ tests/
_claude/hooks/next-claim-push.sh:92
_claude/hooks/git-state-verify.sh:35
_claude/hooks/next-claim-unshared.sh:88
_claude/hooks/next-claim-unshared.sh:112
（+ next-claim-unshared.sh:81 のコメントに同じ式の説明）
```

## 正解実装が同じ repo にある

```
src/glogx/gitlog.go:283  args = append(args, "--not", "--remotes")   # HEAD 起点
```

`UnpushedSHAs` は `rev-list HEAD --not --remotes` の形で、detached でも正しく数える。
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §0-B
（その答えを既に出している経路があるなら近似を書かない）に照らすと、
**hook 3 ファイルだけがそこから乖離している**。

## 🚨 修正式の選択（実測つきで決めること）

| 式 | 挙動 |
|---|---|
| `--branches --not --remotes` | 現行。detached を見ない |
| **`HEAD --not --remotes`** | detached を見る。**推奨**（glogx と同形） |
| `--all --not --remotes` | detached も見るが、**stash と他 worktree の commit も拾う**（実測済み） |

`--all` は「自分が今いる worktree の未 push」を知りたい用途には広すぎる。

## 影響範囲の違い（用途で分ける）

- `git-state-verify.sh`: 「今 commit したものが push されたか」→ **HEAD 起点**が正しい。
  加えて**判定不能**（remote 未設定 / repo 外）を `(なし)` に丸めず「判定不能」として出す
- `next-claim-*.sh`: 「他マシンから見えない claim があるか」→ **HEAD 起点 + ブランチ**の両方が要る
  （claim は通常ブランチで作ることも worktree で作ることもある）。用途を明記して式を決める

## テストの fixture も同じ盲点を持っている

`tests/claude/test_next_claim_unshared.sh` の fixture は**全ケース通常ブランチ**
（生成部 32 / 37 / 43 / 120 行）。production が使う detached worktree を構造的に踏まないので、
**式をどちら向きに変えても緑のまま**。

## 🚨 検証するときハッシュで照合しない（rebase で偽陽性が出る）

dotfiles-71 が本件を受けて自セッションの 22 commit を全数照合したところ、**取りこぼしはゼロ**
だったが、途中で 4 件が「origin に無い」と出た。中身を見ると **rebase 前のハッシュ**だった
（`9e18e81f` → `c45d1a02` 等。成果物はすべて `origin/master` に在った）。

この repo は worktree 運用で `git rebase origin/master` を日常的に挟むので、
**ハッシュでの照合は偽陽性を出す**。修正の検証では **subject と成果物でも照合する**こと。

## 対応 (2026-09-09)

commit `fix(311): 未 push 判定を HEAD 起点にして detached worktree を見えるようにする`。

### 直した箇所（機械で数えた）

`grep -rn -- '--branches --not --remotes' _claude/ scripts/ bin/ zshlib/ src/ tests/` の結果は
**5 件**（issue が言う「4 箇所 + コメント 1」と一致）。全部直した:

| 場所 | 用途 | 採った式 |
|---|---|---|
| `next-claim-push.sh:92` | claim を commit した直後の push 促し | `HEAD --branches --not --remotes` |
| `next-claim-unshared.sh:81` | （コメント） | 同上 + 理由 |
| `next-claim-unshared.sh:88` | 未 push の claim 抽出 | `HEAD --branches --not --remotes` |
| `next-claim-unshared.sh:112` | 同（別経路） | 同上 |
| `git-state-verify.sh:35` | 「今 commit したものが push されたか」 | `HEAD --branches --not --remotes` |

**`HEAD` と `--branches` の両方**を母集合にした。claim は通常ブランチでも worktree でも作るため
（issue の「用途で分ける」に対する答え）。`--all` は **stash と他 worktree の commit も拾う**ので採らない。

### 判定不能を成功に丸めない（`git-state-verify.sh`）

remote が無い repo で `(none — すべて push 済み)` を出すのは**逆向きの嘘**（push 先が無いので
「どこにも push していない」が正しい）。→ git repo でない / remote 未設定 を
**それぞれ「判定不能」として出す**形にした。

### テストを先に足して red を見た（受け入れ条件の順序）

`tests/claude/test_next_claim_unshared.sh` に **detached worktree の fixture**（bare remote +
clone + `git worktree add --detach` で claim を commit）を足し、**現行実装で red**
（`✗ 発火すべきなのに無出力: detached worktree で commit 済みだが未 push の claim`）を確認してから直した。

🚨 **fixture を作るとき「空の `issues/next/` は git に載らない」を踏んだ**。worktree の
チェックアウトに `issues/next/` が存在せず `git mv` が `destination directory does not exist` で
落ちた（このテストファイルの冒頭が同じ罠を注記していた）。worktree 側で `mkdir -p` する形に直した。

### 変異検証

`HEAD` を外して `--branches` だけへ戻す変異 → detached ケースが **red**
（`✗ 発火すべきなのに無出力`）。修正後は 18 件すべて green、`test_next_claim_push.sh` も 31 件 green。

## 受け入れ条件

- [x] 4 箇所すべてを同じ commit で直した（`grep` の結果 5 件 = コード 4 + コメント 1 を上に記載）
- [x] **先に** fixture を足して**現行実装で red を見た**
- [x] 判定不能を成功に丸めない（`git-state-verify.sh` に「判定不能」の 2 分岐）
- [x] **変異検証**: 式を `--branches` に戻すと detached ケースが red

## 関連

- issue 310（`git-state-verify.sh` の他の 2 つの破れ。同じファイルを触るので順序を決めて着手する）

## 実運用への影響（確認済み）

dotfiles-71 は worktree で commit するたびに `(none — すべて push 済み)` の偽の全クリアを
見ていたが、**push の判定に hook を使わず**、毎回 `git push` の出力
（`d6695a72..f51a6bb8 HEAD -> master`）と本体側の `pull` / `merge --ff-only` の結果を
読んでいたため実害は出ていない。**逆に言えば、hook の「未 push」欄は worktree では
最初から読む価値が無かった**ことになる。
