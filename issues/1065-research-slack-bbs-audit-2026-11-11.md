# 1065 (research): src/slack-bbs の全観点の監査 (2026-11-11。直接実行)

起票日: 2026-11-11

## 概要

`/all-audit-on-diy slack-bbs` (dupscan の dogfood を兼ねる。所見は [1066](epic/932/1066-research-dupscan-dogfood-slack-bbs-audit-2026-11-11.md))。
対象は src/slack-bbs (`git ls-files src/slack-bbs` で 45 ファイル・11,912 行、`732df8baa` 時点)。slack-bbs の初めての監査。
直したのは 4 点 (グループかの判定を ID に寄せた・チャンネルの一覧の一時ファイルの掃除・README の残すもの・購読しているアーカイブ済みのチャンネルをグループに入れられた)、起票は 0 本。
4 点目は、自分の読みでは見つけられず、敵対的レビューが見つけた (picker のキー処理は「読んでいない」範囲だった)。

## 走査した母集合

- 対象: 45 ファイル (Go 39: 直下 7・`view/` 11・`subs/` 4・`source/` 4・`render/` 3・`board/` 4・`cache/` 2・`llm/` 2・`job/` 1、ほか README・Makefile・go.mod・lint の設定)。
  対象を名指しする外のファイル: `.github/workflows/src_slack-bbs.yml`・`_claude/skills/slack-bbs/SKILL.md`・`docs/glogx-ui-guide.md`・issue 706 / 1051 / 1062 / 1063 (`git grep -l slack-bbs -- ':/'`)
- 全部を読んだ: `main.go`・`mentions.go`・`render/render.go`・`cache/cache.go`・`source/source.go`・`source/throttle.go`・`job/job.go`・`llm/llm.go`・`board/board.go` の Assemble 以降・`subs/store.go`・`subs/picker.go` の `NewPicker` / `Refresh`・`view/view.go` の読み直し (`reload` / `loaded`)・タブの切り替え・購読の画面 (`openPicker` / `pickKey` / `setChannels`)・`openSelected`・`view/read.go` の隠す (`toggleHidden` / `toggleShowHidden`)・README・CI の workflow
- 読んでいない (目通しもしていない): `board/units.go`・`board/mrkdwn.go`・`view/list.go`・`view/style.go`・`view/highlight.go` (節 5 の 1 組だけ)・`subs/picker.go` のキー処理と描画・`view/view.go` の描画 (`screen` 〜 `helpLines`)・テストの全部。view の大半と picker のキー処理は、1062 / 1063 で敵対的レビューを各 2〜3 周回した範囲
- 機械の検査 (監査の始め、`732df8baa`): `go vet ./...` rc=0 / `make lint` 0 issues / `deadcode ./...`・`deadcode -test ./...` ともに 0 件 (`GOENV_VERSION=1.25.4`) /
  `dupscan sweep src/slack-bbs` (単位 423・ファイル 39) と `dupscan sweep -focus src/slack-bbs src` (単位 9,929・ファイル 1,418)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| encapsulation | 1 (直した) | E1: 板がグループかを、Markdown の見出し (`render.Markdown`) と stderr・`-dry-run` の名前 (`main.pageLabel`) はメンバーの数 (`len(Members) > 0`) で、他 (`subs.Channel.IsGroup`・キャッシュ・2 段の実行) は ID (`source.IsGroupID`) で決めていた。メンバーの無いグループだけ「#times」と出た → `render.Page.IsGroup` / `Label` に寄せた。読む画面のタブは `subs.Channel.Label` (ID) で、もともと正しい |
| resource-leaks | 1 (直した) | `cache.Expire` は書きかけの一時ファイルのうち名前が板の ID で始まるものだけを消し、`channels.json.tmp.*` (一覧を書いている途中で止まったとき) は残り続けた。ほか: slack-cli の lock は `os.OpenFile` (Go は O_CLOEXEC) で子へ継承しない・Close で flock を放す。claude と slack-cli の子は `subproc.CommandContext` で、画面を閉じたら `browse` が取り消して `inflight.Wait` で待つ |
| broken-code | 1 (直した。敵対的レビューが見つけた) | `subs.Picker.visible`: 購読しているのにアーカイブ済み・一覧から消えたチャンネルの行は、外せるように出すため `memberOnly` を立てない。グループの編集中に隠す条件が `memberOnly` だけを見ていたので、`g` で作ったグループにその行を Space で入れられた (保存すると、読み直しのたびに「読めなかったチャンネル」の注記が出続ける) → 編集中は `IsArchived` の行を、編集しているグループのメンバーのときだけ出す |
| ux | 1 (直した) | README の「送るもの・残すもの」が `.hidden.json` と `channels.json` を挙げていなかった。ほか、キー表と README の突き合わせは 1062 / 1063 で済み。表示幅は `forbidigo` が x/ansi の直呼びを止めている |
| duplication | 0 | sweep の節 1・3・4・6 は「なし」。節 2 の `browse` の `Save` / `SaveHidden` の 1 行のクロージャ 2 つは寄せる価値が無い。節 5 の `numberStart` ~ `boundaryBefore` は前置きの 2 文が同じだけで返す述語が違う (1066 の F2)。module をまたぐ `source.sleepCtx` ~ `doctor/health.SleepCtx` (Dice 0.698) は 10 行が 2 か所で、共有の module を作ると両方の go.mod に replace が増えるだけなので寄せない (3 か所目が出たら再考)。`render.hasMsg` ~ glogx・`render.Count` ~ doctor の L1m は別のドメインの小さい関数 |
| error-handling | 0 | 1 チャンネル・1 スレッド・言及の検索の失敗は止めずに注記へ。キャッシュの読み書きの失敗も注記・stderr へ (warn)。記録のみ 2: (a) `source.Client.Channels` / `Users` / `Search` は slack-cli の stderr の警告 (一覧の打ち切り) を捨てる。slack-cli は 50 ページ (`internal/slack/api.go` の `maxPages`) で打ち切るので、それを超えるワークスペースでは購読の画面のチャンネルと名前の表が黙って欠ける。今のワークスペースは約 18 ページで起きない。`source/throttle.go` の `warningsShown` のコメントが whoami・users・channels・search の警告は板へ届かないと既に書いている (b) `job.Clean` はディレクトリを消せなかった理由を問わず「他のファイルがあるので残した」と出す (権限の誤りでも同じ文)。害は文が不正確なだけ |
| security | 0 | 外から来る文字列 (本文・名前・LLM の見出し・slack-cli の誤り) は `termsafe` を通してから端末へ出す。`o` で開く URL は whoami の URL・板の ID・ts から組み、言及の本文の URL は開かない。2 段の実行は依頼のハッシュと HMAC の一覧で書き換えを見破る (鍵はディレクトリに置かない)。分類の claude は `--tools ""`・`--setting-sources ""` で、本文の中の指示に従っても外へ書けない。キャッシュのファイル名は `source.IsBoardID` で確かめる |
| concurrency | 0 | K1: `Expire` は消す直前に組の時刻を取り直す (窓は残る、と理由コメントにある)。同じチャンネルの読み直しが 2 本走る形 (外して入れ直す・2 つの slack-bbs) は `Summary.Covers` のコメントが受け入れを書いている。K2: slack-cli の列の flock は呼び出しの間だけ持ち、待ちの間は放す。K3: 読み直しは世代 (`gen`) で古い結果を捨てる。`maxPause` が止めた列の期限を 5 分に詰める。K4: 一時ファイルは atomicfile の乱数の名前、2 段の実行の既定は `os.MkdirTemp` |
| performance | 0 | 実測はしていない。読み直しは 1 チャンネルずつ (Init は最初のタブだけ)、一覧の行は描くときに作って持つ (`Model.rows`)、新着のタブは開いたとき・読み直しの後・隠したときだけ並べ直す。`view/bench_test.go` がある |
| broken-code (ほか) | 0 | 依頼の版 (`job.Version`)・キャッシュの版 (`cache.Version` / `ReadVersion` / `ChannelsVersion`) は形を変えたら上げる約束で、読み違えずに捨てる。slack-cli の引数はフラグを先に置く (`History` のコメント) |
| false-green | 0 | 合否を返す経路は `-max` の止め・`board.ValidateUnits`・2 段の実行の署名だけで、どれも誤りを返す側に倒れる |
| dead-code | 0 | `deadcode` / `deadcode -test` で 0 件 |
| ci | 0 | `src_slack-bbs.yml` の paths は go.mod の replace 5 本 (atomicfile・termsafe・tuikit・subproc・userdirs) を全部覆う。tuikit の replace (testwait) は slack-bbs の go.mod に入らない (テストだけの依存) |
| dependency | 0 | `go mod tidy -diff` が rc=0・差分 0 バイト (使っていない require も足りない require も無い)。bubbletea v2 は v2.0.8 で、他の 7 つの module (glogx・tuikit・treefiler など) と同じ (`rg "charm.land/bubbletea/v2 v" src/*/go.mod`) |
| test-cleanup / test-helpers | 0 | sweep の節 1・5・6 にテストの組は無い。節 3 の `cache_test.go` の `mustSubsPath` ~ codexevents / zundamon の `must` は別 module のテストの 1 行の helper で寄せない。テストの中身は読んでいない |
| design / responsibility / polymorphism / leaky-abstraction / ui-components | 0 | `view/view.go` (1,268 行) は描画・キー・読み直し・購読の画面の入口を持つが、1062 / 1063 で `read.go` (既読・隠す・新着のタブ) と `list.go` を分けた後で、状態は `Model` 1 つに閉じる。分けて 3 問 (責務・探す手間・読む範囲) が良くなる案は出せなかった。`source.Runner` / `llm.Runner` は実物と偽物の 2 実装を持つテストの seam |
| lint-from-done | 0 | 今回の判定の食い違い (`len(Members) > 0`) は 2 か所で、lint の規則にする数ではない (寄せた後は 0 か所) |
| general | 0 | 上に含めた |

全数: 直した 4 (encapsulation 1・resource-leaks 1・ux 1・broken-code 1)、起票 0、記録のみ 5 (error-handling の (a) (b)・下の反証の未確認 2・2 周目の所見 1)。

## 対応 (どれも回帰テストか文書の突き合わせで確かめた。変異の中身は各 commit の message)

- `fix(slack-bbs): メンバーの無いグループも板の名前をグループの印 (📂) で出す`
- `fix(slack-bbs): チャンネルの一覧 (channels.json) の書きかけの一時ファイルも期限で消す`
- `docs(slack-bbs): README の残すものに、隠した話題とチャンネルの一覧のファイルを足す`
- `fix(slack-bbs): 購読しているアーカイブ済み・消えたチャンネルをグループの編集に出さない`

## 反証・敵対的レビュー (opus の読み取りのみのサブエージェント)

- 1 周目 (直した 3 commit と、読んでいない view・picker): 3 commit は壊せなかった (試した形: `channels.json` そのもの・`channels.json.tmpfoo`・`channels.jsonX.tmp.1`・symlink・
  並行の slack-bbs が書いている最中の一時ファイル・2 段の実行の show のラベル・JSON の出力)。消えすぎる形は `channels.json.<何か>.tmp.<x>` だけで、slack-bbs はこの名前を作らず、
  置き場は slack-bbs 専用なので受け入れる。README の事実は cache.go の定義と一致。
  採った 1 = 上の broken-code (picker)。未確認 2 (再現の手順を示せなかったので直さない):
  (a) 新着のタブで `x` を押したとき `toggleHidden` が抜く行の添字に丸めていない `m.list.Cursor` を使う (`selRow` は丸める。食い違うのは Cursor が一覧の長さ以上のときだけで、`fitList` が先に丸めるので入る手順が見つからない)
  (b) `newCount` の `counted` を ts だけで引くので、グループの別のチャンネルに同じ ts の発言があると言及を 1 件少なく数える (ts がチャンネルをまたいで一致する必要がある。`board.tsSet` のコメントが同じ形を受け入れている)。
  手薄と言われた点: 新しい `TestEmptyGroupIsLabeledAsGroup` は `-md` と `-dry-run` だけを見て、2 段の実行の show と stderr の警告の文は見ない (どれも同じ `render.Page.Label` を通るので足していない)
- 2 周目 (picker の直し `582ab32d7`): 壊せなかった (試した経路: Space・検索中の Tab・g の直後・別のグループの読めないメンバー・編集中の Refresh・外してから入れ直す・
  編集していないときに購読しているアーカイブ済みの行が出たままか・memberOnly の行の挙動が変わらないか)。
  記録のみ 1 (前からある挙動で、見せ方の判断が要るので直さない): グループの編集中に読めないメンバーを Space で外すと、その行がすぐ消えてカーソルは同じ添字のまま
  次の行を指すので、続けて Space を押すと隣の読めるチャンネルを入れてしまう。今回の直しで、購読しているアーカイブ済みのメンバーもこの形に入るようになった。
  直すなら、外した行を編集を終えるまで残す (淡く出す) か、カーソルを直前の行へ寄せる。取り消しは今は「保存せずに閉じる」だけ

