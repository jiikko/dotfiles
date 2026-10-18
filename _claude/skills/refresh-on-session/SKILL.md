---
name: refresh-on-session
version: 1.0.0
description: このブランチ、またはこのセッションで書いた差分の全体を読み直し、用語の揺れ・古いまま残ったコメント・追従し損ねた実装 (呼び出し元・対になる箇所・テスト・文書) を探して、裏の取れたものを直す。「差分全体を見直して」「用語の揺れや更新漏れが無いか見て」「refresh-on-session」「/refresh-on-session」で発火。バグ探しは code-review、ディレクトリの文書と実体の一括照合は claude-md-refresh、issue 本文の書き戻しは issue-writeback の担当。
---

# Refresh on Session

差分を 1 つずつ書いている間は、その場の hunk しか見ていない。この skill は**書き終えた差分を全体として**読み直し、
差分の中どうし・差分と周りのコードのあいだで食い違っているものを直す。

見るのは 3 つだけ:

1. **用語の揺れ** — 同じものを違う名前で呼んでいる
2. **古いまま残ったコメント・文書** — 差分で変えた挙動・名前を、前のまま説明している
3. **追従し損ねた実装** — 差分で変えた契約に、呼び出し元・対になる箇所・テストが付いてきていない

バグそのもの・設計の良し悪し・差分と無関係の既存の負債は扱わない (差分と無関係のものは最後に報告だけする)。

## 引数

```
/refresh-on-session              # 既定: ブランチに未 merge の commit があれば branch、無ければ session
/refresh-on-session branch       # 基点ブランチとの merge-base から作業ツリーまで
/refresh-on-session session      # このセッションで触ったファイルの差分だけ
/refresh-on-session branch base=origin/release
```

## 作業ディレクトリ

用語集は**ブランチ単位で持ち、PR を merge するまで別のセッションからも使い回す**。そのため branch モードの dir はセッションではなく
ブランチで決める。session モードと detached HEAD はその回限りなので、`skill-tmpdir` の dir (`<本体 checkout>/tmp/refresh-on-session/<session-id>/`) を使う。

```sh
common=$(git rev-parse --path-format=absolute --git-common-dir)
case "$common" in */.git) root=${common%/.git} ;; *) root=$(git rev-parse --show-toplevel) ;; esac   # worktree からでも本体 checkout
branch=$(git symbolic-ref --short -q HEAD)
if [ "$mode" = branch ] && [ -n "$branch" ]; then
  d="$root/tmp/refresh-on-session/branch-$(printf '%s' "$branch" | tr '/ ' '__')"
  mkdir -p "$d"
else
  d="$(skill-tmpdir refresh-on-session)"    # 同じ run の中では最初に得たパスを使い回す
fi
# $d/README.md            この dir の説明と消してよい条件 (毎回書き直す。下節)
# $d/glossary.md          用語集 (3-1)。branch モードでは run をまたいで使い回す
# $d/diff.patch           対象の差分        ┐
# $d/ledger.md            台帳 (Step 2)     │ 最後の run の出力。
# $d/spelling-groups.txt  綴りの揺れ (3-1)  │ 次の run が上書きする
# $d/grep/                照合の grep の出力 ┘
```

- `skill-tmpdir` が無い環境では `$root/tmp/refresh-on-session/<日時>/` を作って使う
- repo の `tmp/` が ignore されているかを `git check-ignore -q tmp/` で確かめ、されていなければ中のファイルを commit に含めない

### README.md を毎回書き直す (残骸を他のセッションが消せるように)

この dir は merge の後に残りがちで、残った dir を見つけた無関係なセッションが「消してよいか」をすぐ決められる必要がある。
**起動するたびに、照合を始める前に** `$d/README.md` を次の形で書き直す (途中で止まった run の dir にも説明が残るように、最初に書く)。
`<…>` は実際の値で埋め、コマンドは絶対パス・実際のブランチ名のままコピーして打てる形にする。

```markdown
# refresh-on-session の作業ファイル

refresh-on-session skill (`~/.claude/skills/refresh-on-session/SKILL.md`) が書いた。ブランチ `<branch>` の差分を読み直すときの
用語集 (`glossary.md`) を、PR を merge するまで使い回すために残している。それ以外のファイルは最後の run の出力で、次の run が上書きする。
中身はどれも作り直せる (用語集は次の run が差分から作り直す)。

- repo: `<root>` / ブランチ: `<branch>` / 基点: `<base>`
- 最後に使った: <YYYY-MM-DD HH:MM> / そのときの HEAD: `<sha>`

## 消してよい条件 (どれか 1 つが成り立てば、この dir ごと消してよい)

1. PR が merge か close された: `gh -R <owner/repo> pr list --head <branch> --state all --json state -q '.[].state'` に MERGED か CLOSED が出る
2. ブランチが基点に取り込まれた: `git -C <root> fetch -q origin && git -C <root> merge-base --is-ancestor <sha> origin/<base>` が rc 0
3. ブランチがローカルにも origin にも無い: `git -C <root> rev-parse -q --verify refs/heads/<branch>` と `git -C <root> ls-remote --exit-code origin refs/heads/<branch>` がどちらも失敗する
4. 「最後に使った」から 30 日を超えた

消し方: `rm -rf <$d の絶対パス>`
```

- squash merge・rebase merge では 2 が成り立たないので、1 と 3 を並べている。どれも確かめられない (gh が無い・origin に届かない) ときは 4 で判断する
- session モードの dir では、ブランチの行と条件 1〜3 を「この run 限りの dir。run が終わったら消してよい」に置き換える

## Step 1: 対象の差分を決めて、先に示す

### branch

基点は、引数 `base=` → repo の CLAUDE.md / README に書かれた基点ブランチ → `git symbolic-ref --short refs/remotes/origin/HEAD` の順で決める。
決められなければユーザーに聞く (`main` を決め打ちしない)。

```sh
mb=$(git merge-base "$base" HEAD)
git diff "$mb" --name-only --no-renames      # commit 済み + index + 作業ツリー (rename の旧パスも出す)
git diff "$mb" --stat
git diff "$mb" > "$d/diff.patch"
git log --oneline "$mb"..HEAD
```

### session

一次情報は**会話履歴**。このセッションで Edit / Write した・Bash で書き換えたファイルを列挙し、それぞれの差分を取る
(commit したものは、このセッションの commit の範囲で `git diff <最初の commit>^ -- <path>`)。
差分は `git diff <起点> -- <path>...` で `$d/diff.patch` に書く。

### 共通

- 未追跡のファイルは `git diff` に出ないので、入れるものは `git diff --no-index /dev/null <path> >> "$d/diff.patch"` で足す (rc は差分ありで 1)
- 🚨 **未追跡のファイルは、このセッションで作ったと会話履歴で言えるものだけ入れる**。作業ツリーには過去の残骸や
  並行セッションの作業が置かれていることがあり、`git status` の `??` を全部拾うと他人のファイルを書き換える
- **作業ツリーに、このセッションで触っていないファイルの未 commit の差分があれば、対象から外して一覧だけ出す**
  (並行セッションの作業中かもしれない。`~/.claude/rules/commit-with-pathspec.md`)。branch で commit 済みの差分は対象に含めてよい
- 自動生成のファイル (`Code generated ... DO NOT EDIT` の見出しを持つもの・lock ファイル・snapshot) は**読むが書き換えない**。
  食い違いが生成物にあるなら、生成元を直して生成のコマンドを示す
- 照合に入る前に、モード・範囲 (`<base>..HEAD` + 作業ツリー)・ファイル数・行数・外したファイルをユーザーへ出す。
  差分が空なら、範囲が合っているかを伝えて終える

## Step 2: 差分が「変えたもの」を一覧にする

照合の前に、差分から次を抜き出して表にする。3 つの観点はどれもこの表を起点に探す。

| 種類 | 例 |
|---|---|
| 改名 | 関数・型・変数・ファイル・設定キー・CLI の flag・画面の文言 (旧名 → 新名) |
| 契約の変更 | 引数・戻り値・エラーの種類・既定値・値の意味・単位・enum の値の追加と削除 |
| 挙動の変更 | 条件・順序・副作用・対象の範囲が変わった関数 |
| 新しく入れた語 | 差分で初めて出てきた名詞 (識別子・コメント・文書・ログ・UI の文言) |

`grep '^[-+]' "$d/diff.patch"` の削除行と追加行を対にして読むと、改名と契約の変更を拾いやすい。

この表は `$d/ledger.md` に書き、検査の台帳として使う。改名・契約の変更・挙動の変更の行ごとに、3-2 (コメント・文書) と 3-3 (実装) で
**何を grep して何件当たり、どう判定したか**を埋める。空欄を残したまま報告しない (見ていない行が分からなくなる)。

## Step 3: 3 つの観点で探す

### 3-1. 用語の揺れ — 差分から用語集を作り、それと照合する

読んだ印象で「揺れていない」と判定しない。**用語集を作り、揺れの候補を grep で数えて 0 件になることを確かめる**。

**(a) 用語集を作る。** Step 2 の「改名」「新しく入れた語」と、差分の追加行に出てくる概念の名詞・識別子から、概念ごとに 1 行:

| 概念 | 採る語 | 揺れの候補 (検索の語) | 根拠 |
|---|---|---|---|
| 一覧を読み込む処理 | `load` | `fetch`, `retrieve`, 読み込み/取得 | repo 内 `load` 41 件 / `fetch` 3 件 |
| 旧名 → 新名 | `ItemList` | `item_list`, `itemList`, `item-list`, 「アイテム一覧」 | 差分で改名 |

- 揺れの候補には、同義語・略す / 略さない・単数 / 複数・日本語の表記揺れ・**識別子の綴りの形** (`snake_case` / `camelCase` /
  `kebab-case` / 文字列リテラル / 文書での呼び方) を入れる
- 用語集は自分が思いついた揺れしか拾えないので、**綴りの形の揺れは機械で補う**。差分の追加行の語を、区切りと大文字小文字を
  そろえた形でまとめ、表記が 2 つ以上ある組を出す。出た組は用語集の行にする (同じ言語の慣習で形が分かれるだけ — Go の型 `ItemList`
  と変数 `itemList` など — は揺れではないので、そう判断した理由を書いて外す)

  ```sh
  grep '^+' "$d/diff.patch" | grep -v '^+++' \
    | grep -oE '[A-Za-z][A-Za-z0-9_-]{2,}' | sort -u \
    | awk '{ k=$0; gsub(/[A-Z]/, " &", k); gsub(/[_-]/, " ", k); k=tolower(k); gsub(/ +/, " ", k); sub(/^ /, "", k)
             f[k]=f[k] " " $0; n[k]++ }
           END { for (k in n) if (n[k] > 1) print k ":" f[k] }' > "$d/spelling-groups.txt"
  # 例: "item list: item_list item-list itemList ItemList"
  ```

  🚨 この検出は**綴りの形しか見ない**。同義語 (`load` と `fetch`) と日本語の表記揺れは拾えないので、用語集の方で見る
- 採る語の決め方は、repo の CLAUDE.md・用語集・UI の文言の規約 → repo 全体の件数と使われている場所 (`git grep -c -i '<語>'`) の順。
  改名なら新名
- 🚨 **どちらが定着しているか決められない概念は、採る語を空にしてユーザーに聞く**。語の選択は成果物を実質的に変える
- 用語集は `$d/glossary.md` に書き、照合に入る前にユーザーへ出す
- **`$d/glossary.md` が既にあれば (前の run・別のセッションが作ったもの)、読んでから足す**。採る語の決定、特にユーザーに聞いて決めた語は
  引き継ぐ (聞き直さない。決めた日と「ユーザー決定」の印を行に残しておく)。今の差分で入った概念の行を足し、差分から消えた概念の行は消す。
  🚨 **件数と根拠は引き継がず、毎回数え直す** (前の run の後に commit が増えている)

**(b) 照合する。** 揺れの候補ごとに、2 つの範囲で grep する:

```sh
git grep -n -i -w -e '<揺れの候補>' -- <対象のファイル>   # 差分の中の揺れ → 期待は 0 件
git grep -n -i -w -e '<旧名・揺れの候補>'                  # repo 全体の取り残し (改名・差分で入れた語)
```

- 当たった行ごとに「揺れ (直す) / 別の概念 (同じ綴りで意味が違う) / 差分と無関係の既存」に分ける。別の概念と判断したら理由を残す
- grep の出力は `$d/grep/` に残し、**用語集の行ごとに件数を記録する** (直す前 → 直した後)。報告に載せる
- `-w` が効かない日本語は `-w` を外し、当たり過ぎるなら語を長くする。0 件のときは、採る語の方が当たることを先に確かめる
  (検索の書き方の誤りで 0 件になっていないか。採る語も 0 件なら検索が壊れている)

### 3-2. 古いまま残ったコメント・文書

- 挙動・契約を変えた関数の **doc コメントと、本体の中のコメント** が今の挙動を説明しているか
- 改名・削除した名前を参照している**コメント**が無いか (3-1 の grep の結果からコメント行を拾う)
- 変えた挙動を説明している **README・CLAUDE.md・docs・skill・runbook・API 定義の説明文** が無いか。
  探すのは文言ではなく**旧い値そのもの** (旧いエラーコード・旧い flag・旧い既定値) で grep する
- **テスト名・テストの説明**が、今の assert と違うことを言っていないか
- 差分で足したコメントが、コードを読めば分かることの言い直しや作業の経緯になっていないか
  (`~/.claude/rules/comment-no-restate-enforced.md`。repo のコメントの規約があればそちらに従う)

### 3-3. 追従し損ねた実装

- **呼び出し元**: 契約を変えた関数・型を呼んでいる箇所を全部挙げ (grep に加え、使えるなら compiler / LSP の参照)、
  全部が新しい契約で呼んでいるか。エラーの種類を変えたなら、それを判別している呼び出し側の分岐も見る
- **対になる箇所**: 同じ判断・同じ一覧を別の場所でも持っていないか — 表と裏 (フロントとバックエンド・
  API 定義と実装・送る側と受ける側)、複製されたロジック、allowlist・CI の paths・設定の一覧・enum を数える switch。
  片方だけ変わっていれば取り残し
- **テストと fake**: 変えた挙動を、テストの fixture・fake・stub がまだ旧い挙動で模していないか
- **差分の中で途中まで**: 差分で始めた置き換え (新しい helper への移行など) が、差分の中の一部の箇所に旧い形のまま残っていないか
- grep は直した行の字面ではなく**仕組み**で書く (同じ旧い契約を、別の引数・別の形で使っている箇所も拾う)

### 差分が大きいとき

ファイルが 30 を超える、または変更行が 2,000 を超えるなら、観点ごとに sonnet の read-only サブエージェント (Explore) を
起こしてよい (3 体まで。read-only なので並行でよい)。

- 指示には、Step 2 の表・対象の範囲・その観点の節をそのまま渡し、「読み取りのみ。何も編集せず、シェルのリダイレクトでも
  どこにも書かない」と書く。出力は「場所 (file:symbol) / 何と何が食い違うか / 根拠 (grep の結果・commit subject)」の一覧
- 返ってきた一覧は **main が根拠を確かめてから**採る (`~/.claude/rules/subagent-model-tiering.md` の検閲の観点)。
  特に「〜はもう使われていない」「呼び出しは無い」は、main が数え直す

## Step 4: 直す

- **直すのは、根拠を確かめた食い違いだけ**。確信が持てないものは書き換えず、報告の「確かめられなかった」に回す
- 見つけたら**同じターンで直す**。「次に直します」でターンを終えない
- **差分が原因で古くなった箇所は、差分の外のファイルでも直す** (それが更新漏れの定義)。
  差分と関係なく前から食い違っていたもの (既存の負債) は直さず、報告の最後に挙げる
- 用語を寄せたら、**3-1 (b) の grep を用語集の全行で回し直し**、差分の中の揺れが 0 件になったこと
  (残すなら「別の概念」の理由つき) を確かめる。寄せた語が別の概念の語とぶつかっていないかもこのとき見る
- 自動生成のファイルは書き換えない (Step 1)。repo が「人の確認が要る」と定めた作業 (GUI の操作・ビルドの禁止など) があれば従う
- 直した後に、**直したファイルを実際に検査する lint・テスト・型検査**を回し、出力を読む
  (`~/.claude/rules/verify-execution-not-just-exit-code.md`)。repo が実行を禁じている検査は回さず、回すコマンドを報告に書く

## Step 5: 報告する

```
## 差分の読み直し: branch (origin/main..HEAD + 作業ツリー) / 32 ファイル

| 観点 | 場所 | 食い違い | 結果 |
|---|---|---|---|
| 用語 | service/foo.go:LoadItems, docs/foo.md | 同じ処理を load / fetch と呼んでいた | load に寄せた (repo 内 41 件 対 3 件) |
| コメント | handler/foo.go:Create | doc コメントが旧い戻り値 (bool) を説明 | 直した |
| 実装 | web/api/foo.ts | バックエンドで消した status=draft をまだ送っている | 直した |
| 用語 | ui/Status.tsx | 「有効」と「利用中」のどちらが定着しているか決められない | 確かめられなかった (要判断) |

用語集 (揺れの候補の件数。差分の中 直す前 → 直した後 / repo 全体の取り残し):
| 採る語 | 揺れの候補 | 差分の中 | repo 全体 |
|---|---|---|---|
| load | fetch, retrieve | 4 → 0 | 3 (既存。直していない) |
| ItemList | item_list, itemList | 2 → 0 | 0 |

外したファイル (このセッションで触っていない未 commit の差分): a/b.go, c/d.ts
検査: make lint → 0 件 / go test ./service/... → ok
差分と無関係の既存の食い違い (直していない): ...
```

- 観点ごとに**見つからなかったものも「なし」と書く** (無報告と未点検は区別できない)
- 件数は数えてから書く (直したファイル数は `git diff --stat` で数える)
- **commit・push はしない**。ユーザーの指示があったときだけ、直したファイルを pathspec で明示して commit する

## 関連

- 組み込みの `/code-review` — **境界**: あちらは差分のバグ (正しさ) を探す。こちらは差分の一貫性
  (名前・説明・追従) を見る
- `~/.claude/skills/claude-md-refresh/SKILL.md` — **境界**: あちらはディレクトリの文書を、差分と関係なく実体と一括で照合する。
  こちらは差分が古くした文書だけを見る
- `~/.claude/skills/issue-writeback/SKILL.md` — issue 本文への書き戻しはあちら。こちらは issues/ を扱わない
