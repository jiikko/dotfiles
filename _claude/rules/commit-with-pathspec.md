# commit は自分が触ったファイルを pathspec で明示する（並行セッションの混入防止）

## ルール

- **commit は必ず `git commit -m "..." -- <path1> <path2>` の形式で、このセッションで変更したファイルを明示して行う**。pathspec なしの `git commit` / `git commit -a` は使わない
- `git add` も対象ファイルを明示する。`git add .` / `git add -A` は禁止（既存の c skill ルールと同じ）
- **新規ファイルは pathspec commit でも事前の `git add <path>` が必要**（untracked は pathspec だけでは拾えない）。add するのは自分が作ったファイルだけ
- commit 前の `git status` で、自分が触っていないファイルがステージ済み・変更済みでも**巻き込まない・リセットしない**。それは並行セッションの作業中データかもしれない

## なぜ

根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/commit-with-pathspec.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## pathspec は cwd 相対で解決される — repo root へ移動してから打つ

- pathspec は **cwd 相対**。`cd src/glogx` した状態で `git commit -- src/doctor/x.go` と打つと
  `src/glogx/src/doctor/x.go` を探して外れる
- 外れたら **commit されない**。実測 (2026-09-02): `git commit -- <root 相対>` は **rc=1** +
  stderr に `error: pathspec '...' did not match any file(s) known to git`、`git add` は **rc=128**。
  つまり無音ではない
- ⚠️ **誤認は次の push で起きる**。commit が空振りした後の `git push` は
  **`Everything up-to-date` で rc=0** を返すので、push の出力だけを見ると成功に見える
- 予防: **commit の前に `cd "$(git rev-parse --show-toplevel)"`**。ツールの cwd がサブディレクトリに
  残っている状態で pathspec を組まない (シェルの cwd は前のコマンドから持ち越される)
- 検出: commit 直後の `git log -1 --stat` で想定ファイルが入っているか見る (下の節と同じ規律)

## pathspec で「生成物」を漏らすと壊れたコミットになる

- **ソースを消す / 足す変更をしたら、それに連動する生成物 (Xcode の `project.pbxproj`、lock ファイル、
  スナップショット等) を同じ commit に含める**。pathspec 方式は「書いたファイルを列挙する」ため、
  自分が直接編集していない生成物を忘れやすい
- 生成物が漏れると **その commit 単体では壊れる**。手元は再生成済みで build が通るため気づけない
- 対策: **commit 後に `git show --stat HEAD` を読み、想定したファイルが全部入っているか目視する**。
  ファイル削除・追加を伴う変更では特に。`git status` に生成物が残っていたらそれが漏れのサイン

## rename (`git mv`) は旧パスも pathspec に書く

- **`git mv` は「旧パスの削除」と「新パスの追加」の 2 つの変更**なので、pathspec に**両方**
  列挙しないと片方だけがコミットされる。新パスだけ書くと、旧パスの削除がステージに残ったまま
  commit が成功する
- `git show --stat` には新パスの追加だけが載るため、**成功した commit の stat を見ても漏れに
  気づけない** (「無いもの」は stat に出ない)
- 検出は `git status`: commit 後に `D  <旧パス>` が残っていたらそれが漏れ。上の「生成物」節が
  「`git status` に残っていたらサイン」と言っているのはこの形も含む
- 予防: rename を含む commit では `git status --short` で `R`/`D` の行を数え、pathspec が
  その両側を覆っているか確認する。あるいは rename だけを独立した commit にする

## commit message は `-m "..."` で書かない (シェル展開で壊れる)

- **`git commit -m "...メッセージ..."` の二重引用符内では、バッククォートが command substitution として
  評価される**。Markdown 的に `` `foo.swift` `` と書いたつもりが `foo.swift` をコマンドとして実行し、
  **その語がメッセージから消える** (`command not found` がシェルに出るだけで commit は成功する)
- **必ず heredoc で渡す**。`$(...)` を使う形も同じ理由で危険:

```bash
git commit -F - <<'MSGEOF' -- path1 path2
feat(x): `Foo.swift` の `bar()` を直す
MSGEOF
```

`<<'MSGEOF'` のようにクォートすると変数展開もコマンド置換も起きない。
`git commit -m "$(cat <<'EOF' ... EOF)"` は heredoc 自体は安全だが、`$(...)` の結果が
再度 `-m` の引用符に入るため**書き方を誤ると同じ事故になる**。`-F -` が最も安全。

## 履歴操作 (reset / amend / rebase) の前に直近コミットの所有者を確認する

pathspec 規律は「混入」は防ぐが、**履歴を書き換える操作は防げない**。branch の先頭には並行セッションのコミットが積まれているかもしれない。

- **`git reset HEAD~N` / `git commit --amend` / `git rebase` の前に、必ず `git log -N --format='%h %ad %s' --date=format:'%H:%M'` で対象コミットが自分のものか確認する**（自分が数分前に作ったコミットと、メッセージ・時刻が一致するか）
- 「直近コミット = 自分の直近コミット」と思い込まない。自分のコミットの直後に並行セッションが commit していれば、reset HEAD~1 は**他人のコミット**を、自分のコミットの上に他人が積んでいれば**自分のつもりで他人の**を切り落とす
- ⚠️ **上の確認が効くのは「自分が書いたメッセージと一致するか」までで、他セッションへの帰属には使えない**。
  全セッションが同じ git user なので **author では区別できず**、`git pull --rebase` は他人の commit を
  自分の commit のあいだに挟むので **時刻の前後も根拠にならない** (実測 2026-09-02:
  `23:30 → 23:52 → 23:47` と逆転していた)。帰属に使えるのは **commit が触ったファイル**と、
  **本人に聞くこと**だけ。誤帰属のコストは濡れ衣だけでなく、**真の当事者を探すのをやめてしまう**こと
- 副次の注意: **`git mv` は即座に stage される**。stage された変更は共有 index 上で「他セッションの pathspec なし commit / reset に拾われ得る」状態になるため、stage から commit までの間隔を最小にする

## やること / やらないこと

- ✓ `git commit -m "..." -- path1 path2` で自分の変更ファイルだけコミットする
- ✓ 新規ファイルは自分が作ったものだけ `git add <path>` してから pathspec commit
- ✓ 見覚えのないステージ済み変更は放置する（並行セッションの作業中かもしれない）
- ✓ reset / amend / rebase の前に `git log` で対象コミットが自分のものか確認する
- ✗ pathspec なしの `git commit` / `git commit -a` / `git add .` / `git add -A`
- ✗ 他セッションのものかもしれない変更の unstage・checkout・restore
- ✗ 直近コミットの所有者を確認せずに reset HEAD~N / commit --amend する

## 例外

- ユーザーが明示的に「全部コミットして」と指示した場合（その場合も `git status` で内容を確認し、意図外のファイルが混ざっていないか報告してから）

## 関連

- `~/.claude/skills/c/SKILL.md` — commit 手順の一次情報（本ルールの pathspec 要件を組み込み済み）
- `~/.claude/CLAUDE.md`「Git 禁止操作」— stash 禁止・push 前確認
