# `git checkout --` / `git restore` が未コミットの変更を捨てる直前に警告する hook

起票日: 2026-09-06
カテゴリ: feat / 対象: `_claude/hooks/`（新設）+ `_claude/settings.json`（配線）
出典: retro [295](295-retro-glogx-epic-done-default-visible-2026-09-06.md) 項目 1 / 7(a)

## 何を止めたいか

変異検証の復元 (`git checkout -- <path>`) が、**同じパスにある未コミットの修正**を一緒に捨てる。
規範は既にある（[`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
「復元の作法」が発動点まで名指しで書いている: 「レビュー指摘を直した直後は必ず未コミットで、
そこで変異を回すと復元のたびに**修正の方**が捨てられる。指摘を直したら変異の前に commit する」）。

**それでも 2026-09-06 の 1 セッションで 3 回踏んだ**（いずれも「直した直後」）。規範を読んでいる
状態で踏むので、残る手段は機械しかない。

## 設計（未確定。着手時に詰める）

- **PreToolUse(Bash) hook** で `git checkout -- <paths>` / `git restore <paths>` / `git checkout .` を検出する
- 🚨 **deny にしない**。変異の復元は正当な用途で、deny にすると変異検証が回らなくなる。
  「そのパスに未コミットの変更がある」ことを**注意として注入**するに留める
  （[`tmux-probe-requires-socket-isolation.md`](../../_claude/rules/tmux-probe-requires-socket-isolation.md)
  の deny 型とは別。あちらは常に誤りだが、こちらは正当な場合がある）
- 判定は `git status --porcelain -- <paths>` が非空か。パスが省略形・変数のときは検出できない
  （静的検査の限界。取りこぼすより出す側へ倒す既存方針に合わせる）

## 受け入れ条件

- [ ] 未コミットの変更があるパスへの `git checkout --` で注意が出る
- [ ] 変更が無ければ黙る（毎回出すとノイズになり、読まれなくなる）
- [ ] `jq` 不在で無音死する既存 hook と同じ振る舞い（`command -v jq || exit 0`）を踏襲する
- [ ] `tests/claude/` に回帰テストを置く（既存 hook のテストと同じ形）
- [ ] 🚨 **新設する検査なので**
      [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md)
      の §1（異常系の実験）§2（沈黙 = 成功になっていないか）§8（脅威モデルと「検出しない形」を先に書く）を通す

## なぜ今やらないか

retro 295 の切り出しとして起票のみ。hook は新しい安全機構なので、実装には敵対的レビューを含む
1 セットが要る（同じセッションで 293 の一括修正と検査新設を抱えているため分ける）。

## 決着（2026-09-06）

`_claude/hooks/warn-discarding-checkout.sh` を新設し、PreToolUse(Bash) に配線した。
**deny はしない**（`permissionDecision` を返さないので許可の判断を一切変えず、
`additionalContext` だけを足す）。

### 設計判断: hook か git ラッパーか

ラッパー（zsh 関数）も技術的には成立する — 実測で **Bash ツールのシェルからも `_zshrc` の
関数は見えた**。それでも hook を採った:

- 事故 3 件はいずれも変異検証の復元 = Bash ツール経由で、PreToolUse が正面から覆う
- ラッパーは zsh から出る全 git に乗り、stdout へ出せば git 出力を parse している箇所を壊す。
  しかも bash スクリプト・glogx（Go）・CI は結局覆えず、**どちらにしても部分的**
- ラッパーの唯一の優位（引数が展開後に見える）は、**パスを静的に解決しない**設計で潰した

### 敵対的レビュー（§1 / §2 / §8 を通した）

**P1×3 / P2×7 / P3×3**。重かったのは 2 つ:

- **ヘッダの射程が実物と一致していなかった**。「実装後に突き合わせた」と書きながらしておらず、
  未宣言のまま検出しない形が 8 つあった（§8 が名指しする「守られていると読ませる嘘」）。
  22 形を実物へ流して一覧を作り直した
- **cross-repo**: `git -C <path> checkout --` で **cwd 側の** dirty を見ていた。この repo の規範が
  `git -C <本体の絶対パス>` を推奨しているので、**推奨形がそのまま盲点**だった

判定は `git_scan`（グローバルオプションを読み飛ばしてサブコマンドを 1 回取り出す）に寄せ、
枝ごとのリテラル一致で開いていた 4 つの穴をまとめて閉じた。

### 受け入れ条件の結果

- [x] 未コミットの変更があるパスへの `git checkout --` で注意が出る
- [x] 変更が無ければ黙る
- [x] `jq` 不在で無音死する既存 hook と同じ振る舞い
- [x] `tests/claude/test_warn_discarding_checkout.sh`（**40 件**）
- [x] §1 異常系 / §2 沈黙 = 成功の排除 / §8 脅威モデルを実装前に記述 → 敵対的レビューを通した

### 本番での armed 確認（end-to-end）

push → `git -C ~/dotfiles pull --rebase` の後、何も書き換えない probe を 1 回打って確認した:
**dirty な別 repo を `-C` で指した `echo` に対して注意が出て、対象 repo・件数・ファイル名が
正しかった**（`~/dotfiles` は clean なので、cwd を見ていたら沈黙していたはず）。
レビューが「まだ誰もやっていない」と残した未確認リスクはこれで閉じた。

🚨 **この hook は `reset --hard` / `clean -fd` / `switch --discard-changes` / `stash` を見ない。**
事故 3 件が全部 `checkout --` だったので範囲を絞った。「この hook があるから安全」ではない
（射程はスクリプトのヘッダに実測つきで列挙してある）。
