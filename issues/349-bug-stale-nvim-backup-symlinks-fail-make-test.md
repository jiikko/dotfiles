# bug: 2023–2024 の nvim バックアップ symlink 4 本で `make test` が**毎回**赤くなる

起票日: 2026-09-10
カテゴリ: bug
優先度: 中（**手元の `make test` が常に赤**なので、本物の失敗が埋もれる。CI では素通しする）
出典: 2026-09-10 のセッションで `make test` を回して踏んだ

## 何が起きるか

`tests/claude/test_dangling_symlinks.sh` が 4 件で落ちる:

```
FAIL: dangling symlink /Users/koji/.config/nvim/init.vim.pre-dein-vim               -> /Users/koji/dotfiles/_nvimconfig
FAIL: dangling symlink /Users/koji/.config/nvim/init.vim-12-11-2024-17-04-03.pre-dein-vim -> 同上
FAIL: dangling symlink /Users/koji/.config/nvim/init.vim-12-11-2024-17-11-27.pre-dein-vim -> 同上
FAIL: dangling symlink /Users/koji/.config/nvim/init.vim-12-11-2024-17-12-59.pre-dein-vim -> 同上
```

- 実体の `_nvimconfig` は **`502cb237 remove _nvimconfig` で repo から削除済み**
- symlink 自体は **2023-11-18 / 2023-11-22 / 2024-12-11 作成**（`ls -l` の日付）。
  dein から lazy.nvim へ移行したときに nvim が作った `.pre-dein-vim` バックアップ
- 検査のメッセージは「setup.sh を再実行して掃除」と言うが、**それでは消えない**:
  `setup.sh:107` の `legacy_links` は `bashrc bash_profile screenrc` の 3 つしか見ておらず、
  `~/.config/nvim/*.pre-dein-vim` は掃除対象に入っていない（実測）

## 🚨 これは**手元だけ**の赤で、CI では起きない（2026-09-10 の反証レビューで確定）

- `.github/` に `setup.sh` の呼び出しは **0 件**（checkout するだけ）。CI の HOME には
  dotfiles 由来の symlink が 1 本も無い
- `tests/claude/test_dangling_symlinks.sh:11` 自身がそう書いている:
  「dotfiles 未 symlink の環境 (CI 等) では対象リンクが 0 件になり素通しで pass する」

つまり **この検査は CI では構造的に vacuous** で、守っているのは開発機の HOME だけ。
「CI が赤い」ではないので緊急度はその分下がるが、下の理由で放置はしない。

## なぜ放置できないか

`make test` が**手元で常に rc≠0** になるので、「赤いのは既知のあれ」で読み飛ばす癖がつく。
[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)
が禁じる「緑を見ずに判断する」の裏返しで、**恒常的な赤は本物の失敗を隠す**。
しかも CI では素通しなので、**手元で読み飛ばした瞬間に誰も見ていない状態**になる。

## 対応案

1. **4 本を消す**（`~/.config/nvim/` 配下のユーザーファイルなので**人の承認が要る**）。
   中身は dein 時代の `init.vim` へのリンクで、指す先は既に存在しない = 情報量ゼロ
2. `setup.sh` の legacy 掃除に「`~/.config/nvim` 配下で `dotfiles/_nvimconfig` を指す
   dangling symlink」を足す（1 を機械化する。ただし**他人の HOME を消す**方向なので、
   対象を「dotfiles を指していて、かつ dangling」に厳密に絞ること）
3. 検査側で `.pre-dein-vim` を除外する ← **採らない**。指す先が消えている事実は変わらず、
   「検査の対象を減らして緑にする」は
   [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
   §2 の「沈黙 = 成功」を作る

**1 → 2 の順が素直**（まず消して緑にし、再発防止を setup.sh へ入れる）。

## 受け入れ条件

- [ ] `make test` が `tests/claude/test_dangling_symlinks.sh` で落ちなくなる
- [ ] 消す前に **4 本が本当に dangling で、dotfiles を指している**ことを 1 本ずつ確認した記録を残す
- [ ] 2 を採るなら **変異検証**: 掃除を外すと検査が red になる／
      **dotfiles を指さない symlink を消さない**ことを fixture で確かめる

## 🚨 人の承認が要る

HOME 配下のファイル削除なので、Claude は勝手に実行しない。承認が出るまでこの issue は open。
