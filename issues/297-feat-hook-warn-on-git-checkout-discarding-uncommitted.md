# `git checkout --` / `git restore` が未コミットの変更を捨てる直前に警告する hook

起票日: 2026-09-06
カテゴリ: feat / 対象: `_claude/hooks/`（新設）+ `_claude/settings.json`（配線）
出典: retro [295](295-retro-glogx-epic-done-default-visible-2026-09-06.md) 項目 1 / 7(a)

## 何を止めたいか

変異検証の復元 (`git checkout -- <path>`) が、**同じパスにある未コミットの修正**を一緒に捨てる。
規範は既にある（[`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
「復元の作法」が発動点まで名指しで書いている: 「レビュー指摘を直した直後は必ず未コミットで、
そこで変異を回すと復元のたびに**修正の方**が捨てられる。指摘を直したら変異の前に commit する」）。

**それでも 2026-09-06 の 1 セッションで 3 回踏んだ**（いずれも「直した直後」）。規範を読んでいる
状態で踏むので、残る手段は機械しかない。

## 設計（未確定。着手時に詰める）

- **PreToolUse(Bash) hook** で `git checkout -- <paths>` / `git restore <paths>` / `git checkout .` を検出する
- 🚨 **deny にしない**。変異の復元は正当な用途で、deny にすると変異検証が回らなくなる。
  「そのパスに未コミットの変更がある」ことを**注意として注入**するに留める
  （[`tmux-probe-requires-socket-isolation.md`](../_claude/rules/tmux-probe-requires-socket-isolation.md)
  の deny 型とは別。あちらは常に誤りだが、こちらは正当な場合がある）
- 判定は `git status --porcelain -- <paths>` が非空か。パスが省略形・変数のときは検出できない
  （静的検査の限界。取りこぼすより出す側へ倒す既存方針に合わせる）

## 受け入れ条件

- [ ] 未コミットの変更があるパスへの `git checkout --` で注意が出る
- [ ] 変更が無ければ黙る（毎回出すとノイズになり、読まれなくなる）
- [ ] `jq` 不在で無音死する既存 hook と同じ振る舞い（`command -v jq || exit 0`）を踏襲する
- [ ] `tests/claude/` に回帰テストを置く（既存 hook のテストと同じ形）
- [ ] 🚨 **新設する検査なので**
      [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
      の §1（異常系の実験）§2（沈黙 = 成功になっていないか）§8（脅威モデルと「検出しない形」を先に書く）を通す

## なぜ今やらないか

retro 295 の切り出しとして起票のみ。hook は新しい安全機構なので、実装には敵対的レビューを含む
1 セットが要る（同じセッションで 293 の一括修正と検査新設を抱えているため分ける）。
