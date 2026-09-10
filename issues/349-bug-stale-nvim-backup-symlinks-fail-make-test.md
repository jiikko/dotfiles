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

- [x] `make test` が `tests/claude/test_dangling_symlinks.sh` で落ちなくなる
- [x] 消す前に **4 本が本当に dangling で、dotfiles を指している**ことを 1 本ずつ確認した記録を残す
- [ ] ~~2 を採るなら変異検証~~ ← **対応案 1 のみを採ったので不要**（下記）

## 🚨 人の承認が要る

HOME 配下のファイル削除なので、Claude は勝手に実行しない。承認が出るまでこの issue は open。

## 決着 2026-09-10: 対応案 1（消す）だけを採った。**案 2（setup.sh へ掃除を足す）は採らない**

ユーザー承認（「消してもいいよ」）を得て実施。

### 消す前に 1 本ずつ確認したこと（エントリ単位で plan → exec → verify）

**消す直前に条件を取り直して**から `rm` した（呼び出し元の申告値を根拠にしない）:

| 確認 | 結果 |
|---|---|
| symlink であること | 4 本とも ✅ |
| 指し先が `/Users/koji/dotfiles/_nvimconfig` であること | 4 本とも ✅（違えば SKIP する分岐を用意した） |
| dangling であること（`[ -e ]` が偽） | 4 本とも ✅ |
| 作成日 | 2023-11-18 / 2023-11-22 / 2024-12-11 ×2 = **9〜22 か月前** |

### 「dein はもう使っていないか」を確かめた

- **repo 全体で `dein` の参照は 0 件**（`pre-dein` という語を除く）
- 現在のプラグインマネージャは **lazy.nvim**（`_nviminit.lua:1-3` が bootstrap している）
- 指し先の `_nvimconfig` は `502cb237 remove _nvimconfig` で削除済み

→ 4 本は **dein 時代の `init.vim` バックアップ**で、指し先も dein 自体も既に無い。情報量ゼロ。

### 結果

```
pre-dein の残数: 0
~/.config/nvim の symlink 総数: 2
  init.lua       -> ~/dotfiles/_nviminit.lua   (OK)
  lazy-lock.json -> ~/dotfiles/_lazy-lock.json (OK)
tests/claude/test_dangling_symlinks.sh → OK: dotfiles 由来の dangling symlink なし
```

**正当な symlink 2 本は無傷**（消す条件を「dotfiles の `_nvimconfig` を指す」に絞ったため、
そもそも候補に入らない）。

### 🚨 案 2（`setup.sh` の legacy 掃除に足す）は採らない

- **母集合が 4 本しか無く、しかも 2023–24 年の一度きりの移行の産物**。増える経路が無い
  （dein はもう無い）。「発生源を断つ」対象そのものが存在しない
- 他人の HOME を消す方向のコードを、**再発しないもののために**足すのは割に合わない
  （`adversarial-review-own-safeguards.md` §0-A「掃除機構はそれ自体が新しい失敗の入口」）
- **再開の trigger**: 同じ形（`dotfiles/` の実体を指す dangling symlink が HOME に増える）が
  **もう一度**観測されたとき。そのときは母集合が 2 回以上あるので、機械化の価値が出る

### 🚨 この検査は CI では vacuous なまま（変えていない）

`.github/` に `setup.sh` の呼び出しは 0 件で、`test_dangling_symlinks.sh:11` 自身が
「dotfiles 未 symlink の環境（CI 等）では対象リンクが 0 件になり素通しで pass する」と書いている。
**守っているのは開発機の HOME だけ**という性質は今回の対応では変わらない。
「手元でしか赤くならない検査は読み飛ばされて死ぬ」という懸念は残るので、記録しておく。
