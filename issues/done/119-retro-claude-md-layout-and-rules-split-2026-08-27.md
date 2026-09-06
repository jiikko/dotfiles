# 119 retro: CLAUDE.md 配置の見直しと rules の本文 / rationale 分割 (2026-08-27)

起票日: 2026-08-27

対象コミット: `d9f8e62` (.gitignore の CLAUDE.md 無視を外す) / `e2262d2` (rules 分割) /
`2cd2dc0` (shellcheck ルールの paths) / `580c691` (tests/ と src/glogx/ の CLAUDE.md) /
`f471157` (link 完全性テスト)。

## やったこと

「この repo は Claude Code に優しいか」の質問から、起動時に読まれる 162KB の内訳を実測 →
rules 32 本を本文と rationale に分割 (rules 134.6KB → 112.9KB) → `paths:` の条件ロードを
実測しつつ 1 本に適用 → サブ CLAUDE.md 2 本 → setup.sh 再実行と link 完全性テスト。

---

## 1. 削減量を測る前に「半分以上落ちる」と言った

最初の見立てで「なぜ / 実例の節を出せば 134KB の半分以上は落ちる」と書いたが、節単位で
測ると「なぜ」節は合計 25KB (15%) しかなかった。残りの嵩は「やること / やらないこと」の
チェックリスト (≈15KB) と「関連」(≈14KB)、本文に埋まった実例だった。実例を 5 本で手当てして
最終 16%。数字を出す前に測るべきだった (`perf-claims-need-measurement.md` の同型で、性能でなく
「文書量」版)。

**切り出し先の提案**: 却下 (rule 化しない)。既存の `perf-claims-need-measurement.md` の
「数字なしで削減したと書かない」がそのまま当たる。retro に残すだけ。

## 2. `paths:` は Read でしか発火しない — 3 本に付けて 2 本を戻した

issue 作成 (`issues/**`) とレポート出力 (`tmp/**`) にも `paths:` を付けたが、
`InstructionsLoaded` hook で測ると Write では読み込まれず、同じファイルの Read でだけ
読み込まれた。「書いた瞬間に発火させたい」ルールには使えないので 2 本を無条件に戻した。
測る前に 3 本に付けていたので、測らずに commit していれば 2 本のルールが silent に死んでいた。

**切り出し先の提案**: root `CLAUDE.md` の `_claude/` 節に制約として書いた (済)。
`claude-md-maintenance.md` への追記は不要 (dotfiles 固有の運用なので repo の CLAUDE.md が正本)。

## 3. 一括変換スクリプトが途中で死んで、書き換え済みと未処理が混ざった

分割スクリプトは「1 ファイルずつ検証して書く」構造で、5 本目 (エスケープされたバッククォート
の不一致) で abort した。先の 2 本は書き換え済みで、スクリプトは非冪等 (再実行すると二重に
スタブが入る) だったため、`git checkout --` で戻してから再実行した。**全ファイルの検証を
先に通してから 1 本も書かない (two-phase)** にしておけば復元は不要だった。

**切り出し先の提案**: 却下 (rule 化しない)。使い捨てスクリプト固有で、「検証を先に全部通す」
は一般則として既に自明。同じ形で 2 回目に踏んだら rule 化を再検討。

## 4. `claude -p --allowedTools A,B "prompt"` は prompt を tool 名として食う

`--allowedTools` が可変長引数のため、後ろに置いた prompt が tool 名に吸われ
「Input must be provided…」で 2 回空振りした。prompt は stdin で渡すと確実。

**切り出し先の提案**: 新規 issue ではなく、`InstructionsLoaded` hook を使ったロード検証の手順
(hook 設定 JSON + stdin で prompt + jsonl の読み方) を **scripts/ か tests/claude/ の再利用
できる道具**にする価値がある (このセッションでは tmp/ に置いて捨てた)。「rules を触ったら
何が起動時に読まれるかを測る」は今後も要る。issue 化するか判断を待つ。

## 5. `~/.claude/hooks/` への link は何も参照していない

新テストが `~/.claude/hooks/{human-tasks-due.sh,retro-open.sh,lib}` の未リンクも検出したが、
`_claude/settings.json` の hook command は全て `~/dotfiles/_claude/hooks/...` の直接パスで、
`~/.claude/hooks/` 経由のものは 1 つも無い。setup.sh が hooks を link しているのは死んだ配線か、
別の利用者 (他 repo の settings?) がいるのか未確認。

**切り出し先の提案**: 新規 issue (調査 + 不要なら setup.sh と新テストから hooks を外す)。
判断を待つ。

## 6. link 完全性テストは CI では skip になる — 構造的には dir symlink が正解かもしれない

per-file link の「漏れ」を検出するテストを足したが、CI には ~/.claude が無いので skip し、
手元でしか効かない。漏れを構造的に消すなら `~/.claude/rules` をディレクトリ symlink に
する方が強い (足した瞬間に見える)。ただし setup.sh は過去に dir symlink → per-file へ
移行しており (二重リンク事故のコメントあり)、公式 docs も「Cowork セッションでは working
directory 外を指す symlink された rules dir / rule file を skip する」と書いている。

**切り出し先の提案**: 新規 issue (per-file を続ける理由の再確認 + dir symlink 化の可否。
Cowork 制約は per-file でも同じなので判断材料にならない可能性が高い)。判断を待つ。

## 7. rationale の「移した実例」は文の途中から始まる

本文から段落の一部を切り出したものは、rationale 側では元の文脈の断片 (文の途中から) に
なっている。内容は失っていないが、読み物としては粗い。

**切り出し先の提案**: 却下 (今は直さない)。rationale は「ルールを疑うときに読む」用途で、
本文と併読する前提。読みにくさで困ったときに該当ファイルだけ整える。

---

## 後半 (issue 106 / nvim トレンド調査) の追記

## 8. worktree 内の未コミット変更を `git checkout -- <file>` で消した

106 の変異検証で main 側 `width.go` の別名を壊し、復元に `git checkout -- width.go` を使った。
**worktree の width.go は書き換え済みで未コミットだった**ので、変異ではなく自分の変更ごと
コミット済み (旧実装) に戻り、しかも旧実装が `package main` に関数を持つため build も test も
green のまま通った (grep で `termwidth.Of` が 0 件になって気づいた)。
`mutation-verify-new-tests.md` の「復元の作法」が禁じている形そのもので、ルールを読んでいても
「worktree だから安全」という誤認で踏んだ。worktree は**並行セッションの混入**を消すだけで、
**自分の未コミット変更**は守らない。

**切り出し先の提案**: `mutation-verify-new-tests.md` の「復元の作法」に 1 行追記。「worktree で
当てるときも、変異対象ファイルに未コミットの自分の変更があるなら `cp` 1 世代で戻す (`git checkout --`
はコミット済み状態に戻すので自分の実装が消える)。変異の前に **一度 commit してから**当てるのが最も
単純」。判断を待つ。

## 9. 並行セッションとの調整が 4 往復で済んだ

着手通知 → 相手から「worktree を切れ / 112 で入れた 3 重の防御に追随を / 幅モデルは変えるな」→
触るファイルの事前連絡 → 「TestNoSecondWidthEngine の走査起点を確認して変異で実証を」。全部
実装前か commit 前に届き、手戻りは無かった。うまくいった話なので retro には残さない (規約どおり)。

---

## 切り出しの結果 (2026-08-29 実施)

- **項目 1 / 3 / 7 → 却下** (本文で理由を明記済み)
- **項目 2 → 済**。root `CLAUDE.md` の `_claude/` 節に「`paths:` は Read でしか発火せず
  Write / Edit では発火しない」を制約として記載済み
- **項目 4 → 却下 (issue 化しない)**。`InstructionsLoaded` hook でのロード検証を再利用可能な
  道具にする案は見送り。**手順そのものは本文の項目 4 に残っている**ので、次に rules の
  ロード量を測りたくなったらここを出典にして tmp/ で組み直す
- **項目 5 → issue 化**: [142](142-research-claude-hooks-link-unreferenced.md)。
  2026-08-29 に裏を取った: `_claude/settings.json` の hook command は全て
  `~/dotfiles/_claude/hooks/...` の直接パスで、`~/.claude/hooks/` を指すものは **0 件**。
  repo 内でそのパスを書いているのは `setup.sh` (張る側) と
  `tests/claude/test_claude_links_complete.sh` (検査する側) だけ = **読む側がいない**
- **項目 6 → 却下 (issue 化しない)**。per-file link を続ける。dir symlink 化は
  過去に二重リンク事故で per-file へ移行した経緯があり、**戻す動機がその事故より強くない**。
  漏れの検出は項目 5 の link 完全性テストが担う (CI では skip されるが手元では効く)
- **項目 8 → 実施**。[`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md)
  の「復元の作法」に「**worktree でも `git checkout --` は安全にならない**」を追記した
  (worktree が消すのは他人の混入だけで、その worktree にある自分の未コミット変更は同じように
  消える。しかも戻った旧実装が build も test も通せば green のまま気づけない)

## 残課題

なし (全項目が却下・実施・issue 化で閉じた)
