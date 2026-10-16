# 791 (human): ./setup.sh を回して、改名された skill の壊れた link を消す

起票日: 2026-10-16
期限: 2026-10-20

## なぜ人が要るか

別のセッションが skill を改名した (`product-events` → `my-products-update-apps-event`)。そのため手元の
`~/.claude/skills/product-events` が、もう無い `~/dotfiles/_claude/skills/product-events` を指したまま残っている。

- `make test` の `tests/claude/test_dangling_symlinks.sh` が毎回落ちる (2026-10-16 に 3 回)
- 消す役目は `./setup.sh` が持つ (`setup.sh` の「dangling 削除」。SessionStart の hook は張るだけで消さない)
- `~/.claude` は利用者の環境なので、セッションからは触っていない

機械で済ませない理由は、`setup.sh` が link の掃除以外 (migrate など) もする点にある。利用者の環境全体に手が入るので、人が回す。

## 手順

```sh
cd ~/dotfiles && ./setup.sh
ls -la ~/.claude/skills/product-events     # 「No such file or directory」になる
bash tests/claude/test_dangling_symlinks.sh; echo rc=$?   # rc=0
```

- **期待**: setup.sh の出力に `removing dangling symlink /Users/koji/.claude/skills/product-events` が出る。上の検査が rc=0
- **期待と違ったら**: 出力をこの issue に貼る (setup.sh の掃除が改名を拾えていない可能性)。検査が rc≠0 なら、product-events 以外の dangling も
  出ていないかを、検査の出力ごと貼る (検査は `$HOME` 直下・`~/.claude`・`~/.config` の、`~/dotfiles` を指す dangling を全部見る)

確認が済んだら done へ移す。

## 結果 (2026-10-16)

ユーザーの許可を受けて、セッションが `~/dotfiles` で `./setup.sh` を回した (rc=0)。

- 出力: `removing dangling symlink /Users/koji/.claude/skills/product-events`
- `ls -la ~/.claude/skills/product-events` → `No such file or directory`
- `bash tests/claude/test_dangling_symlinks.sh` → rc=0 (`OK: dotfiles 由来の dangling symlink なし`)
