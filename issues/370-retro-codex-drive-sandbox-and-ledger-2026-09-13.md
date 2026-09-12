# 370 retro: issue 369 (codex-drive の sandbox 解除・台帳の計測列・astra 上振れ) 2026-09-13

- 種別: `retro`
- 対象セッション: issue 369 の評価 → 構成の組み直し → 案 0-b / 段階 1 / 案 1 / 1-3 の反映 (commits 59d221bb〜1706c91e、6 本)
- done の条件: 残課題が空になったとき

## やったこと (要点)

- 「この issue は意味があるか」の問いに対し、6 案が症状への対処で**原因側 (sandbox のキャッシュ書込拒否) を外す案が無い**ことを
  見立て、案 0 として最上流に置いた。ユーザーが `-s danger-full-access` を承認し、SKILL.md 4.3.0〜4.5.0 に反映
- `bin/codex-fanout` の台帳に started_at / elapsed_s / merger 行 (案 1)。触った箇所の潜在バグ (label `merger` の衝突) を同時に塞いだ
- `gpt-6-astra` を 1 行 probe で実在確認し、上振れ先として SKILL.md と effort 整合テストへ

## 反省・気づき

1. **`git pull --rebase --autostash` を使った** (stash 禁止に抵触)。本体 `~/dotfiles` が他セッションの `_claude/settings.json` で
   dirty だったため。2 回目以降は `git -C ~/dotfiles merge --ff-only origin/master` に切り替えた (自分の commit が触らないファイルの
   dirty なら ff は通る)。→ **切り出し先: `.claude/rules/worktree-per-session.md` の「push は反映ではない」節に追記**
   「本体への反映は `merge --ff-only origin/master`。`pull --rebase` は他セッションの未コミット変更で止まり、`--autostash` は stash 禁止に抵触する」
   (発動点が同じなので新規ルールにしない)
2. **issue の評価を「意味があるか」で聞かれたとき、前提 (継続利用するか) で答えが変わった**。最初の回答は縮小寄り、前提確認後は拡張寄り。
   評価を返す前に「この判断を変える前提は何か」を 1 行で先に聞くべきだった。→ 却下 (CLAUDE.md「解釈が分かれると成果物が変わるときだけ確認する」で
   既に覆われている。今回はそれに該当したのに聞かなかった実行側の問題で、ルールの不足ではない)
3. **`codex models` は TUI で非対話では出ない** (`stdin is not a terminal`)。可用性の確認は `codex exec -s read-only -m <model>` の 1 行 probe が
   最短。→ SKILL.md 1-3 の項に書いた (済)
4. **触った箇所の潜在バグ**: label `merger` が merger 自身の成果物と衝突する形は元から在った。台帳に merger 行を足す変更で初めて表面化した。
   → 対応済 (予約語として拒否)。切り出し不要
5. **外部反証を通していない** (codex は明示指示が無いので起動せず、read-only サブエージェントも省いた)。SKILL.md の方針変更 3 本と
   fanout の実装が対象。変異 7 本は全部 red だが、変異は自分が想定した不変条件しか試さない。
   → **残課題**: 案 0-b の実測 (obaket の次マイルストーン) が事実上の反証になる。危険側の観測 (はみ出し / 破壊的操作) が出たら
   0-a へ戻す条件は issue 369 に書いた。実測前に別セッションで敵対レビューを 1 本通すかはユーザー判断

## 残課題 (空になったら done へ)

- [ ] 反省 1 を `worktree-per-session.md` へ追記するか (ユーザー判断)
- [ ] 反省 5: SKILL.md 4.3.0〜4.5.0 と `bin/codex-fanout` の変更に敵対レビューを 1 本通すか、obaket の実測で代替するか (ユーザー判断)

## 関連

- `issues/369-research-codex-drive-cut-wall-time-keep-quality.md` — 本体。段階 2 / 段階 3 / 案 0-b の実測は obaket 側 (このマシンに checkout 無し)
