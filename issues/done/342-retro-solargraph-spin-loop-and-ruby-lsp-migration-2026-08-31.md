# 342 retro: solargraph の暴走調査と ruby-lsp 移行 (2026-08-31)

> 🚨 **2026-09-09 に 144 から 342 へ改番**。起票時、`issues/done/144-human-verify-glogx-ratelimit-dashboard.md`
> が既に 144 を使っており、`tests/issues/test_issue_numbers_unique.sh` が CI で落ちた。
> 参照数が少ない側 (この retro: tracked ファイル 0 / commit 1) を動かした
> （done/144 は push 済み commit 3 本から参照されている）。
> **採番したら即 push する**（`.claude/rules/worktree-per-session.md`）と、`done/` も含めて
> 空きを確認する、の 2 つが守られていれば起きなかった。

起票日: 2026-08-31 / 対象セッション: 2026-08-31 昼〜夜

やったこと: `d757b7dc` (Ruby LSP を project 単位で ruby_lsp / solargraph に排他切替) /
`41f9e39b` (排他をテストで固定、判定点を 1 か所に集約)。ほかに the-rss-reader での
solargraph 無限ループの原因特定、`~/.cache/solargraph` (2.9GB) の削除、nvim で色が付かない
問題の原因特定 (`~/.zshenv` の 1 行が古いシェルに届いていなかった)。

うまくいった話は書かない。踏んだところと、次に効きそうなものだけ。

---

## 1. 生きている暴走プロセスを、ダンプを取らずに kill した

solargraph が CPU 60% で回っているのを見つけた時点で、原因究明より先に `pkill` した。
ログとソース読みと子コマンドの再現で「spin ループ」までは正しく特定できたが、**その説明で
観測値が全部埋まっているかを確認しなかった**。

ユーザーに「sigdump とかして調べた?」と聞かれて再現し直し、`sample` を取ったところ
**イテレーションごとに Progress の keep-alive スレッドがリークしていた** (45 秒で 34 本) と
分かった。最初に見つけたプロセスが RSS 585MB まで育っていたのはこれで説明が付く。
spin ループだけでは 585MB は説明できないのに、**説明できていないことに気づいていなかった**。

**切り出し先の提案**: 新規ルール候補「異常なプロセスが生きているうちは kill する前に観測を採る
(`sample` / スレッド一覧 / env)。プロセスは消えたら二度と同じ状態を作れない」。
`instrument-before-second-fix.md` は「1 回目の修正が外れたら観測」だが、今回は
**修正の前に生きた観測対象があった**ケースで、既存ルールの守備範囲外だった。
併せて「観測値 (CPU / RSS / スレッド数) のうち、自分の仮説で説明できていないものを列挙する」
を同ルールに入れると、今回の見落としは報告前に自分で気づけた。

## 2. 「再現しない」を 2 回繰り返してから、実プロセスの env を読んだ

「rb ファイルで色が付かない」の調査で、headless nvim → 隔離 tmux の pty と 2 回
「私の環境では色が出る」を報告してしまった。3 回目にユーザーが「今も出ない」と言って初めて
**動いている実 nvim の `ps eww` を読み**、`SUPPORT_TRUECOLOR` が env に無いことが分かった
(親シェルが `~/.zshenv` への追記より 7 時間半前に起動していた)。

同じセッションの前半、solargraph の調査では**最初に `ps eww` で子プロセスの env を読んで**
GEM_HOME 汚染を特定していた。隣の問題に同じ手を適用しなかった。issue 137 の項目 2 と同型の
再発 (同じセッション内で得た手を隣の項目に適用しない)。

**切り出し先の提案**: 新規ルール候補「手元で再現しない不具合は、まず**動いている実プロセスの
env / cwd / 起動時刻**を読む。設定ファイルの内容とプロセスの環境は一致しない
(長命なシェル・tmux ペインでは設定の追記が届いていない)」。今回は `.zshenv` に正しい行が
**書いてあった**のに効いていなかったので、ファイルを読むだけでは永遠に分からなかった。

## 3. 自分の観測と矛盾する仮説を口に出した

同じ調査で「treesitter が attach していない可能性」と言ったが、その直前に自分で
`vim.b.ts_highlight = true` を観測していた。実際の原因は colorscheme 分岐 (gruvbox は cterm 色を
持たない) で、attach の有無とは無関係。手元の観測データと突き合わせずに仮説を出した。

**切り出し先の提案**: ルール化するほどではない。1 と 2 の「説明できていない観測値を列挙する」に
含まれる話として扱えば足りる。あるいは却下。

## 4. codex が usage limit で、代替の反証レビューに切り替えた

`codex exec review` が usage limit (再試行は 16:47 以降) で落ちたため、
`issue-creation-codex-review.md` の代替規定に従い、観点を分けた read-only サブエージェント
2 体 (①判定ロジックを壊す ②既存 solargraph 運用への回帰) で反証させた。結果は
実バグ 1 件 (allowlist の末尾スラッシュで無言に solargraph へ落ちる) + 自分が書いたコメントの
事実誤り 1 件 + テスト不足 1 件で、いずれも取り込んだ。代替経路は機能した、という記録。

なお本 retro 自体は codex レビューに通していない (コードの主張ではなくセッションの振り返りのため)。

---

## 残課題 — 2026-09-09 に実測で 5 件中 4 件を決着

### ① `~/.zshenv` が dotfiles 管理外 → [issue 346](../346-bug-truecolor-flag-is-not-version-controlled-and-fails-silently.md) へ

**「フォールバックが拾うのでは」を実測して否定した**。tmux の中では `TERM_PROGRAM` が nil
（`_nviminit.lua:40` のコメントどおり）なので判定 3 が発火せず、**判定 4 に落ちて「対応」と
誤判定**する。つまり `.zshenv` の 1 行は**実際に効いている**（実測: `SUPPORT_TRUECOLOR=false` /
`termguicolors=false` / `colorscheme=retrobox`）。

🚨 ただし retro が書いた「dotfiles へ `_zshenv` として取り込むか」は**そのままでは採れない** —
この値は**マシンごとに違う**（ファイル自身が「マシンごとに置く」と宣言している）。
対応案 4 つを 346 に整理した。

### ② `mason-tool-installer` の `ensure_installed` が動いていない → ✅ **解消済み**

実測 2026-09-09: `~/.local/share/nvim/mason` は**存在し**、`mason/bin` に **15 本**入っている
（`bash-language-server` / `gopls` / `pyright` / `solargraph` / `typescript-language-server` …）。
retro が書いた「`~/.local/share/nvim/mason` 自体が存在しない」はもう成り立たない。

### ③ mason 版が rbenv 版を shadow するか → ❌ **実測で成立しなかった**

心配された経路は**開いていない**:

```
$ which -a solargraph
/Users/koji/.rbenv/shims/solargraph      # ← mason/bin は出てこない

$ nvim ... print(vim.fn.exepath('solargraph'), (vim.env.PATH):find('mason/bin'))
exepath= /Users/koji/.rbenv/shims/solargraph
mason on PATH= false                     # ← nvim の PATH にも入っていない
```

**シェルにも nvim にも `mason/bin` が入っていない**ので、mason 版 solargraph は
lspconfig が明示的に呼ばない限り起動しない。加えて issue 337 で
`ruby_lsp` の probe / server は `mason/bin` を PATH から**明示的に除外**するようになった
（`M.ruby_env`）。`server_packages.ruby_lsp = false` で mason 管理からも外れている。

→ **「Ruby 系サーバを mason 管理から外すか」の検討は不要**。solargraph が mason に居ても
到達経路が無い。もし将来 `mason/bin` を PATH へ入れる変更が入ったら再評価する（それが trigger）。

### ④ 気づき 1・2 の切り出し（新規ルール 2 本）→ **ユーザー判断待ち**（唯一の残タスク）

### ⑤ `d757b7dc` / `41f9e39b` が未 push → ✅ **解消済み**

実測: `git branch -r --contains` で両方とも `origin/master` に含まれている。

## 残タスク

- [x] ④ 気づき 1・2 の切り出し → **2026-09-09 決着**。retro は「新規ルール 2 本」を提案していたが、
      **どちらも `instrument-before-second-fix.md` への追記**にした
      （CLAUDE.md「retro の切り出し先は既存ルールへの追記を既定にする」。2 件とも「観測を先に」の
      族で、この rule が既に複数の発動点を持っている）。追記した節:
      - **観測対象が消える操作の前に観測を採る**（kill / 再起動 / 削除。発動点は「破壊の前」で、
        既存の節がすべて「修正の後」なのと対になる）+ 「仮説で説明できていない観測値を列挙する」
      - **手元で再現しないなら、動いているプロセスの env / cwd / 起動時刻を読む**
        （設定ファイルの内容とプロセスの環境は一致しない）
      実例・実測（585MB の説明が付かなかった件 / `.zshenv` の追記より 7 時間半前に起動していた件）は
      `rules-rationale/` 側へ置いた（rule 本文は毎セッション全文読まれるため）

**残課題なし。** done へ送る。
