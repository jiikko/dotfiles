# 1067 (bug): setup.sh が張らなくなった宛先の dotfiles のリンク切れを、~/.claude の外では掃除しない

起票日: 2026-11-11

## 概要

`tests/claude/test_dangling_symlinks.sh` が見つけるリンク切れの一部は、`./setup.sh` を回しても消えない。
消えないのは、setup.sh がもう張らなくなった宛先のもの (dotfiles からファイルを消した・名前を変えた) のうち、掃除のループの外にあるもの。
掃除のループが見るのは `~/.claude` の 6 つのディレクトリだけ。
検査の失敗の文は「setup.sh を再実行して掃除」と案内するが、その手順では直らず、手で消すことになる。

setup.sh が今も張る宛先 (`~/.config/nvim/init.lua`・`~/.claude/settings.json` など) は問題にならない。リンク先が消えても `ln -sf` が張り直す。

## 現状

| | 見る場所 | 条件 |
|---|---|---|
| 検査 (`tests/claude/test_dangling_symlinks.sh` の `scan_links`) | `$HOME` 直下・`~/.claude` (深さを切らない)・`~/.config` (深さを切らない) | リンク先が `$HOME/dotfiles/` の下で、リンク先が無い |
| 掃除 (`setup.sh` の「dangling 削除」のループ) | `~/.claude/{agents,skills,rules,hooks,workflows,commands}` の直下だけ | リンク先が `$HOME/dotfiles/_claude/` の下で、リンク先が無い |
| 掃除 (`setup.sh` の `legacy_links`) | `~/.bashrc` `~/.bash_profile` `~/.screenrc` のつもり (下の 🚨) | 名前で決め打ちし、リンク先も `case` で見る |

検査が見て掃除が見ない場所:

- `~/.config` の全部
- `$HOME` 直下
- `~/.claude` の直下と、上の 6 つ以外のディレクトリ
- 6 つのディレクトリの中の、`.` で始まる名前 (掃除の `*(N)` は隠し名を拾わない。検査の `find` は拾う)

逆に、掃除だけが見る形もある。`~/.claude/skills` などが正常なディレクトリへの symlink なら、掃除の `*(N)` はその中を見る。検査の `find` は symlink を辿らない。

🚨 `legacy_links` の掃除は一度も当たっていない。setup.sh は zsh で、`for legacy in $legacy_links` は語に分かれず 1 回だけ回る。
2026-11-11 の実測 (出るのは 1 行だけ):

```
$ zsh -c 'l="bashrc bash_profile screenrc"; for x in $l; do echo "[$x]"; done'
[bashrc bash_profile screenrc]
```

## 観測 (issue 349 の再開の条件を満たした)

issue 349 (2026-09-10) は `~/.config/nvim/*.pre-dein-vim` の 4 本を手で消し、「setup.sh へ掃除を足す案は採らない」と決めた。
理由は「母集合が一度きりの移行の産物で、増える経路が無い」。
再開の条件は「同じ形 (`dotfiles/` の実体を指す dangling symlink が HOME に増える) がもう一度観測されたとき」だった。

同じ形のリンクは `~/.config/nvim/coc-settings.json -> ~/dotfiles/_coc-settings.json`。
`c5a6ef078` (2026-07-09) は、coc.nvim から nvim のネイティブ LSP へ移行した commit。
`_coc-settings.json` と、setup.sh でそれを張る行を消した。張ってあったリンクは残った。
このリンクは 349 の前後に 2 回記録されている。

- 2026-09-05: `038daecd0` の commit message が、このリンク切れを「2 か月 dangling のまま残っていた」と記録している。349 の 5 日前で、349 はこれを母集合に数えていない。
  一方 349 の結果には「`~/.config/nvim` の symlink は 2 本だけ」とある。どちらの記録にも、測ったマシンは書かれていない (dotfiles を使う Mac は複数ある)
- 2026-11-11: このマシン (air5) に同じリンクが残っていた。リンクの作成日 (`ls -l` の日付) は 7 月 5 日で、7 月から残っていたと読める。
  `./setup.sh` を回しても消えなかった (掃除の対象外) ので、issue 1060 の作業の途中に手で消した。
  消す前に確かめたこと: リンクであること・リンク先の文字列は `~/dotfiles/_coc-settings.json` であること・リンク先が無いこと

増える経路は「setup.sh が `~/.claude` の外へ張っていたファイルを dotfiles から消す・名前を変える」で、一度きりではない。
今 setup.sh が `~/.claude` の外へ張っているリンクは次の 12 本。

- `$HOME` 直下の 9 本 (`gemrc zshrc rspec gitconfig pryrc zlogin railsrc gitignore_global` と `~/.tmux.conf`)
- `~/.config/nvim/init.lua`・`~/.config/nvim/lazy-lock.json`・`~/.config/container/config.toml`

## 対応方針

### 案 A (推奨): 判定 (読み取りだけ) を検査と掃除で 1 か所に寄せ、setup.sh がそれを使って消す

見る場所と「消してよいリンク切れか」の判定を、読み取りだけの関数 (例: `scripts/lib/dotfiles_links.sh`) に置く。
検査はそれで列挙し、setup.sh はそれで列挙してから消す。消す処理は setup.sh に残し、検査の側に消す口を持たせない。
`legacy_links` の決め打ちの掃除はこれに置き換える。

今は検査と掃除が、見る場所を別々に持っている。検査は `038daecd0` (2026-09-05) から `~/.config` も深さを切らずに見る。掃除はそのときに変わらなかった。

守ること:

- 消すのは、次を全部満たすリンクだけ。消す直前に取り直す (一覧を作った時点の判定で消さない)
  - シンボリックリンクである
  - リンク先の文字列が `$HOME/dotfiles/` で始まり、`..` の段を含まない
  - リンク先が「無い」と言い切れる
- 「無い」と「確かめられない」を分ける。今の検査の `[ ! -e ]` は、リンク先の親のディレクトリを辿れないとき (権限・途中が切れている) も真になる。
  消すのは、リンク先の親のディレクトリが dotfiles の中に在って辿れ、その中にリンク先の名前が無いときだけにする
- 見る場所は、今の検査と掃除の和にする。`~/.claude/skills` などがディレクトリへの symlink のときも、その直下を見る (今の掃除が消せているものを消せなくしない)。
  ディレクトリへの symlink の先が `$HOME` の外なら辿らない
- 消したものを 1 行ずつ出す (今の setup.sh の `removing dangling symlink` と同じ)
- テストは使い捨ての HOME で回し、次のものを消さないことを固定する
  - その HOME の外
  - `$HOME/dotfiles/` の外を指すリンク切れ (`$HOME/dotfiles/../x` の形も)
  - 生きているリンク
  - リンク先を確かめられないリンク
- 検査の失敗の文の「setup.sh を再実行して掃除」が、全部の場所で正しくなる

失うもの: setup.sh 以外の手で張った、`$HOME/dotfiles/` を指すリンク。
リンク先が一時的に無いとき (古い commit を checkout している間など) に setup.sh を回すと、そのリンクは消えて戻らない。
setup.sh が張るリンクは、次の setup.sh で張り直される。

### 案 B: dotfiles からファイルを消す変更で、`legacy_links` のような名前の決め打ちの掃除を足す

消す人が覚えていれば足りるが、`c5a6ef078` はそれを忘れた形。覚えることに頼るので採らない。

### 案 C: 検査の失敗の文だけを直す (setup.sh が張らなくなった宛先は手で消す、と案内する)

案内は正しくなるが、手で消す作業は残る。案 A を採らないときの最低限。

## 受け入れ条件

- [ ] 検査と setup.sh の掃除が、見る場所と条件を同じ 1 か所から得る
- [ ] `./setup.sh` を回すと、検査が見る場所の、`$HOME/dotfiles/` を指すリンク切れが消える (setup.sh がもう張らない宛先のもの)
- [ ] 今の掃除が消せているもの (`~/.claude/skills` などがディレクトリへの symlink のときのその中) も、引き続き消える
- [ ] `legacy_links` の決め打ちの掃除を共通の判定に置き換える。置き換えないなら、語に分けて回る形に直す
- [ ] 案 A の「消さないもの」4 つをテストで固定し、変異で red を確かめる
- [ ] 検査の失敗の文の案内が、どの場所のリンク切れでも正しい

## 関連

- issue 349 (`~/.config/nvim` の 4 本を手で消し、setup.sh へ掃除を足す案を見送った。この issue はその再開)
- issue 791 (setup.sh で `~/.claude/skills` のリンク切れを消した。今の掃除の範囲で直る例)
- issue 1060 (この観測をした作業)
- `038daecd0` (検査の範囲を `~/.config` へ広げ、同じリンク切れを記録した commit)

## 進捗

- 2026-11-11: 起票。codex (gpt-6.1-sol, high) の反証レビューの P2 4 件・P3 2 件を反映した。
  反映した点:
  - `..` で外へ出るリンク
  - 「無い」と「確かめられない」の区別
  - 9/5 の記録と 349 の結果の食い違い
  - ディレクトリへの symlink の範囲差
  - 張り直される宛先は問題にならないこと
  - `legacy_links` が語に分かれないこと (実測で確認)
  読み取りの関数だけを共有し、消す処理は setup.sh に残す形も、レビューの提案から採った
