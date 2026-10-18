# commit は自分が触ったファイルを pathspec で明示する（並行セッションの混入防止）

## ルール

- **commit は必ず `git commit -m "..." -- <path1> <path2>` の形式で、このセッションで変更したファイルを明示して行う**。pathspec なしの `git commit` / `git commit -a` は使わない
- `git add` も対象ファイルを明示する。`git add .` / `git add -A` は禁止（既存の c skill ルールと同じ）
- **新規ファイルは pathspec commit でも事前の `git add <path>` が必要**（untracked は pathspec だけでは拾えない）。add するのは自分が作ったファイルだけ
  - **commit の後に `git status --short` の `??` を見て、自分が作ったファイルが残っていないかを確かめる**。残っていたら、その commit は手元で検証した木と別物で、
    手元の検査は緑のまま push 先だけが壊れる (retro 819: 既存のディレクトリに 1 ファイルだけ足した回で 2 度漏れ、CI のビルドが 3 commit の間落ちていた)
- commit 前の `git status` で、自分が触っていないファイルがステージ済み・変更済みでも**巻き込まない・リセットしない**。それは並行セッションの作業中データかもしれない

## なぜ

根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/commit-with-pathspec.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## pathspec は cwd 相対で解決される — repo root へ移動してから打つ

- pathspec は **cwd 相対**。`cd src/glogx` した状態で `git commit -- src/doctor/x.go` と打つと
  `src/glogx/src/doctor/x.go` を探して外れる
- 外れたら **commit されない** (`git commit` は rc=1 + `error: pathspec '...' did not match`、`git add` は rc=128。無音ではない)
- 🚨 **誤認は次の push で起きる**。commit が空振りした後の `git push` は
  **`Everything up-to-date` で rc=0** を返すので、push の出力だけを見ると成功に見える
- **push / merge に `-q` を付けない**。空振りを示す唯一の手がかり (`Everything up-to-date` /
  `Already up to date`) がその 1 行なので、黙らせると rc=0 だけが残って完全に無音になる
- **push を再試行するなら、合間の `pull --rebase` の rc を見る**。衝突で止まった rebase の上で打った push も
  `Everything up-to-date` rc=0 を返す。rc≠0 なら再試行のループを抜けて rebase を片付ける
- 予防: **commit の前に `cd "$(git rev-parse --show-toplevel)"`**。ツールの cwd がサブディレクトリに
  残っている状態で pathspec を組まない (シェルの cwd は前のコマンドから持ち越される)
- 検出: commit 直後の `git log -1 --stat` で想定ファイルが入っているか見る (下の節と同じ規律)
- **submodule・入れ子 repo の中のファイルは親から commit できない**。親の checkout の中に見えていても
  別 repo なので、親の pathspec は外れる。
  commit の前に **`git -C <ファイルの dir> rev-parse --show-toplevel`** で所属 repo を確かめ、その repo で
  commit & push してから親の参照を更新する
- **worktree からの `merge --ff-only` / `push` も cwd 依存**。作業 worktree (`wt-xxx`) の cwd で
  `git merge --ff-only <branch>` / `git push` を打つと、**worktree 側のブランチ**に対して
  「Already up to date」「Everything up-to-date」が返るだけで master は 1 mm も動かない。本体の checkout / umbrella への操作は
  **`git -C <本体の絶対パス>`** で対象を明示し、直後に `git -C <本体> log -1 --oneline` で先端が動いたことを見る
- **パイプ越しでも見失う**: `git commit ... | head` のように通すと `$?` はパイプ終端の
  status になり、上の rc=1 が消える ([`verify-execution-not-just-exit-code.md`](verify-execution-not-just-exit-code.md) の系)

## pathspec で「生成物」を漏らすと壊れたコミットになる

- **ソースを消す / 足す変更をしたら、それに連動する生成物 (Xcode の `project.pbxproj`、lock ファイル、
  スナップショット等) を同じ commit に含める**。pathspec 方式は「書いたファイルを列挙する」ため、
  自分が直接編集していない生成物を忘れやすい
- 生成物が漏れると **その commit 単体では壊れる**。手元は再生成済みで build が通るため気づけない
- 対策: **commit 後に `git show --stat HEAD` を読み、想定したファイルが全部入っているか目視する**。
  ファイル削除・追加を伴う変更では特に。`git status` に生成物が残っていたらそれが漏れのサイン
- **pathspec の一覧は `git diff HEAD --name-only` (index と作業ツリーの両方) で取り、件数を期待値 (当てたパッチのファイル数など) と突き合わせる**。
  `git diff --name-only` は未ステージだけを返すので、index に書く道具 (`git apply --3way`・merge の衝突の解決) の後では一覧が欠ける (retro 822: 269 ファイルを漏らして一覧だけを push した)
- **追記・置換だけのつもりの変更は、commit の前に `git diff --numstat` の削除行を数え、想定と合うかを見る** (追記なら 0)。
  stat はファイルの有無だけでなく**行数の向き**も読む。文書は検査が中身を見ないことが多く、中身が消えても検査は緑のまま通る
- **除外の判定は「自分が触ったか」ではなく「その差分が自分の変更と無関係か」で行う。**
  「見覚えのない変更は巻き込まない」(下の節) は**混入を防ぐ**規律だが、**自分の変更が原因で
  modified になったファイル**にそのまま当てると今度は**漏れ**を作る。典型は、自分が消した API を
  使っていた既存テストを他セッションの WIP と読んで除外する形 — **その commit 単体ではコンパイルが
  通らない**のに、手元の作業ツリーには残りの差分があるので build も test も緑になる
- 判定は `git diff -- <path>` を**読む**こと。セッション開始時から modified だったことは
  「自分と無関係」の証拠にならない (開始時点の `git status` を控えていないなら、なおさら読む)

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
🚨 **1 コマンドに heredoc を 2 つ書くとき (ファイルへの追記 + `-F -` の message)、片方を落としても構文エラーにならず、
`-F -` が無音で stdin を待ち続ける** (rc も出力も出ず「時間がかかっている」に見える)。stdin から読む形を使うなら、
その入力を同じ行で与えたかを確かめる。
`git commit -m "$(cat <<'EOF' ... EOF)"` は heredoc 自体は安全だが、`$(...)` の結果が
再度 `-m` の引用符に入るため**書き方を誤ると同じ事故になる**。`-F -` が最も安全。

## 履歴操作 (reset / amend / rebase) の前に直近コミットの所有者を確認する

🚨 **`reset` / `commit --amend` / `rebase` / `cherry-pick` / squash の soft reset / reflog・refspec を打つ前に、`~/dotfiles/_claude/rules-checklists/commit-with-pathspec.md` を Read する** (起動時には読まれない。常時読み込みの指示ファイル合計が上限 150k 字を超えたため、2026-10-12 にこの節の本文を切り出した。dotfiles issue 414 の続き)。pathspec 規律は「混入」は防ぐが、履歴を書き換える操作は防げない。直近コミットが自分のものとは限らず、`reset HEAD~1` は並行セッションの commit を切り落としうる。打つ前に `git log -N --format='%h %ad %s' --date=format:'%H:%M'` で対象が自分の commit かを見る (author と時刻では他セッションへの帰属は決まらない)。

## やること / やらないこと

- ✓ `git commit -m "..." -- path1 path2` で自分の変更ファイルだけコミットする
- ✓ 新規ファイルは自分が作ったものだけ `git add <path>` してから pathspec commit
- ✓ 見覚えのないステージ済み変更は放置する（並行セッションの作業中かもしれない）
- ✓ ただし除外の前に `git diff -- <path>` を読み、自分の変更が原因で modified になっていないか見る
- ✓ reset / amend / rebase の前に `git log` で対象コミットが自分のものか確認する
- ✗ pathspec なしの `git commit` / `git commit -a` / `git add .` / `git add -A`
- ✗ 他セッションのものかもしれない変更の unstage・checkout・restore
- ✗ 直近コミットの所有者を確認せずに reset HEAD~N / commit --amend する

## 例外

- ユーザーが明示的に「全部コミットして」と指示した場合（その場合も `git status` で内容を確認し、意図外のファイルが混ざっていないか報告してから）

## 関連

- `~/.claude/skills/c/SKILL.md` — commit 手順の一次情報（本ルールの pathspec 要件を組み込み済み）
- `~/.claude/CLAUDE.md`「Git 禁止操作」— stash 禁止・push 前確認
