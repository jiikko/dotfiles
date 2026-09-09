# bug: `enable_available` の実在判定が「cmd が関数のサーバ」に効かず、不在が無音になる

起票日: 2026-09-09
カテゴリ: bug
優先度: 低（今すぐ壊れてはいない。**壊れたときに黙る**形なので記録しておく）
出典: issue 337 の作業中に見つけたぼやき。**起票前に実コードで裏を取り、主張を 1 つ訂正した**

対象: [`_nviminit.lua`](../_nviminit.lua) の `enable_available` / [`nvim/lua/dotfiles/lsp.lua`](../nvim/lua/dotfiles/lsp.lua)

## 何が非対称か

```lua
-- _nviminit.lua:389
if type(cmd) ~= "table" or vim.fn.executable(cmd[1]) == 1 then
  table.insert(ready, name)
end
```

- `cmd` が **table** のサーバ（`solargraph` 等）は **binary が実在するときだけ** enable される
- `cmd` が **関数** のサーバ（`ruby_lsp` / `ts_ls` / `eslint` / `html` / `cssls` / `jsonls` /
  `yamlls` / `tailwindcss`）は**実在判定できないので無条件に** enable される

後者は「バイナリ不在でも enable 済み」に見える。コメント（`_nviminit.lua:384-388`）は
この挙動を認識していて「不在は通知されず `:LspLog` にだけ残る」と書いてあるので、
**実装とコメントは一致している**。問題は下記のとおり、この状態を別のファイルが読み始めたこと。

## 🚨 起票時に訂正した主張（ぼやきの元の形は誤り）

ぼやいた時点では「この非対称が『solargraph に切り替えます』の通知を**嘘にしうる**」と書いたが、
**それは issue 337 で既に閉じている**。`lsp.lua:164` が
`vim.lsp.is_enabled("solargraph")` を見て、切り替え先が無ければ
「solargraph も使えません（`:Mason` で solargraph を入れるか…）」へ文面を変える:

```lua
local fallback_ok = (deps.solargraph_enabled or vim.lsp.is_enabled)("solargraph")
```

`solargraph` は `cmd` が table なので `enable_available` の実在判定が効いており、
**この経路では `is_enabled` は正しい情報**を返す。今は嘘をつかない。

## 何が残っているか

**`is_enabled` を「起動しうるか」の proxy として使い始めた**こと自体が、上の非対称に依存している。

- 依存が **2 ファイルに跨った**（enable の判定は `_nviminit.lua`、その結果を読むのは `lsp.lua`）
- **`cmd` が関数のサーバに対しては `is_enabled` が「起動しうる」を意味しない**。将来
  フォールバック先を `cmd` が関数のサーバへ変える / `solargraph` の定義が関数化されると、
  `fallback_ok` は常に true になり、通知が**再び嘘になる**
- lint も型も止めない（`is_enabled` はどちらの場合も bool を返す）。**壊れても無音**

## 対応案（どれも未着手。実需要が出るまで凍結してよい）

1. `lsp.lua` 側が `is_enabled` ではなく「**その名前のサーバが起動しうるか**」を答える 1 つの関数を
   `_nviminit.lua` と共有する（`enable_available` の判定を関数へ切り出して両方から呼ぶ）
2. `cmd` が関数のサーバについて、`vim.lsp.config[name].cmd` を **1 回だけ評価して**
   実行ファイルを取り出せるか試す（副作用の有無を先に確かめること）
3. 何もしない。代わりに `lsp.lua:164` の直近へ
   「**`solargraph` の `cmd` が table であることに依存している**」と制約を書く
   （[`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md)）

**まず 3 をやるのが安い**。1 は「複雑性が下がるか」を確かめてから
（[`verify-design-intent-before-refactor.md`](../_claude/rules/verify-design-intent-before-refactor.md)）。

## 受け入れ条件

- [ ] `lsp.lua` の `fallback_ok` が依存している前提（`solargraph` の `cmd` が table）が、
      コードかテストのどちらかに**機械で確認できる形**で固定されている
- [ ] または、その前提に依存しない形へ寄せてある
- [ ] **変異検証**: `solargraph` の定義を「`cmd` が関数」に変えると red になる

## 関連

- issue 337（`fallback_ok` を入れた変更。この issue はその依存関係の側を見る）
- issue 332 / 333（Ruby の LSP 選択の経緯）
