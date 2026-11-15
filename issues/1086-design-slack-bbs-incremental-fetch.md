# 1086 (design): slack-bbs の取得を、チャンネルごとに前回の取得位置から差分で取る

起票日: 2026-11-15
出典: ユーザーの質問 (2026-11-15「最後に取得した日時をチャンネルごとに記録して、それ以降のメッセージのみを取得するようになっている？」→ なっていない、と答えて起票)

## 概要

slack-bbs は取得のたびに、チャンネルごとに期間全体の history と、期間内に動きのあったスレッドを全部読み直している。
前回の取得で分かっていることを記録して、変わった分だけを Slack から取る形にできないかを設計する。
`oldest` を前回の取得時刻に変えるだけでは、古いスレッドへの新しい返信と、編集された発言を取り逃がす (下の「落とし穴」)。

## 現状 (2026-11-15 にコードで確認)

- `src/slack-bbs/main.go` の `collectChannel` が `cli.History(ctx, ch.ID, board.TsOf(o.lookback.Unix()))` を呼ぶ。`o.lookback` は
  `parseFlags` で `o.since.Add(-back)` (何も渡さないと since は 2 日前、back は 30 日) なので、毎回およそ 32 日前からの history を取る
  (`source.HistoryLimit` = 20000 件で打ち切り)
- 続けて `board.NeedThreads` が選んだスレッドを 1 本ずつ `cli.Thread` で読む。選ぶ条件は、親の `reply_count > 0` で、親の ts か
  `latest_reply` が since 以降のもの (と、since 以降の「チャンネルにも投稿する」返信の親)
- 単位が 1 つでもあれば、`findMentions` が `cli.Search` (search.messages。Tier 2 で 3 秒間隔、ページ送りあり) で言及を探す。これも毎回やり直している
- 読む画面の読み直し (`main.go` の `load`) は、期間の長さを保ったまま窓を押した時刻へずらす (-since を日付で渡したときはずらさない。`sinceFixed`)。前回の取得位置は使わない
- slack-cli の呼び出しは `source/throttle.go` で間を空ける (history・replies は Tier 3 として 1.2 秒間隔。間はサブコマンドごとに測るので、
  history の直後の最初の thread は `gapFloor` の 0.5 秒)。スレッドが N 本あれば、1 チャンネルの取得の下限はおよそ 1.2 × N + 0.5 秒に、言及の検索の 3 秒 × ページ数を足したもの
- キャッシュ (`cache.Raw` の `<ID>.raw.json`) は組み直した後の単位 (`[]board.Unit`。返信は名前を解決した `board.Msg` の形) と `Page`・`Me`・`Base` を持ち、
  起動時に前回の板をすぐ出すのに使う (`cachedPage`)。親ごとの `reply_count` / `latest_reply` と、`source.Message` の形の返信は持たない
- 分類 (`claude -p`) の呼び出しは `cache.Summary.Covers` (発言の ts と本文のハッシュ・モデル・読み手が前回と同じか) で減らしているが、取得の量を減らす仕組みはどこにも無い (slack-cli がキャッシュするのはチャンネルの一覧だけ)
- `lastposts.json` (チャンネルごとの最終投稿の ts) は購読の画面に出すためのもので、取得の範囲には使っていない

## 落とし穴 (差分にすると取り逃がすもの)

1. **古いスレッドへの新しい返信**: history は親の ts で絞るので、`-oldest` を前回の時刻にすると、親が前回より古いスレッドは返らない。
   今は期間全体の history を読み、親の `latest_reply` を見て気づいている (706 の「スレッドの取り方」節の案 B)
2. **編集された発言**: 編集で ts は変わらないので、前回より古い発言の編集は ts の範囲では拾えない。slack-cli の `Message` (`internal/slack/types.go`) は `edited` を持たず、
   今は全体を読み直したうえで本文のハッシュの違いで気づいている
3. **削除された発言・返信**: 全体を読み直さない限り、前回取った発言が消えたことは分からない
4. **期間の窓が動く**: since より前になった単位は板から外れる。差分の結果と前回のキャッシュを合わせるときも、今と同じ範囲で切り直す必要がある

## 対応方針の案 (未決)

- **案 1 (スレッドだけ差分にする)**: history は今のまま期間全体を読み、スレッドは親の `(reply_count, latest_reply)` が前回と同じなら
  `cli.Thread` を呼ばず前回の返信を使う。history は 32 日分でも数ページで済み (未実測)、時間の大半はスレッドの本数に比例するはずなので、
  減らせる量はこちらが大きいと見ている (見積もり)。
  取り逃がすもの: スレッドの中の返信の編集 (返信の編集で親の `latest_reply` が変わるかは**未確認**)。返信の削除は `reply_count` が減るので気づける見込み (**未確認**)。
  使い回した返信の表示名は前回のまま残る (`board.Build` が `us.Names` で解決した名前を単位に焼き込むため。改名が反映されない)。
  使い回せないもの: 「チャンネルにも投稿する」返信から選んだスレッドで、親が lookback より古いもの (history に親が無く、比べる `reply_count` / `latest_reply` が無い)。これは毎回読み直す
- **案 2 (history も差分にする)**: history は前回の最新 ts より後だけを取り、古い親の新しい返信は別の手段で拾う。
  🚨 その手段の候補だった search (706 の案 A) は、706 の進捗 (2026-11-09) で「索引の都合でエラーを出さずに漏れるので採らない」と決めている。
  再提起するなら、漏れないことの実測を添える。古い発言の編集と削除も拾えない。今のところ案 1 が本命
- 言及の検索 (`findMentions`) の使い回しも同じ設計の対象に入れるかを決める (毎回の固定の費用で、Tier 2 なので 1 回が重い)
- どちらの案でも、取り逃がしを戻す口が要る: `R` (全部を読み直す) か `-no-cache` で全体を取り直す、あるいは N 時間ごとに全体を取り直す
- キャッシュに親ごとの `(reply_count, latest_reply)` と、`source.Message` の形の返信を持たせる必要がある。
  `cache.Version` は Raw と Summary で共有しているので、上げると全チャンネルの分類も捨てて一度 `claude -p` でやり直すことになる。避けるなら Raw の版を分けて持つ

## 決めること (ユーザーの判断)

- 返信の編集を取り逃がしてよいか。取り逃がすなら、全体を取り直す口 (キー・間隔) をどうするか

## 着手の前に測ること

性能が主題なので、実装の前に今の所要を測る (`~/.claude/rules/perf-claims-need-measurement.md`)。
購読しているチャンネルごとに、history のページ数・読んだスレッドの本数・取得の所要を記録し、そのうち `latest_reply` が前回と同じだったスレッドが何本あったかを数える。
この数が小さいなら案 1 の効果は小さく、着手しない判断もありうる。
**未実測**。スレッドの本数は今の `-dry-run` で測れる (`main.go` の `read` が「スレッドの取得 %d 回」を出す。-dry-run も Slack は普通に呼ぶ)。
所要・history のページ数・「`latest_reply` が前回と同じだった本数」を出す口はまだ無い

## 反証レビュー (2026-11-15)

読み取りのみのサブエージェント (opus) 1 体で反証した (codex は使わない環境)。指摘 P1 1・P2 4・P3 6 件を、コードと 706 で確かめて全部本文へ反映した:
search を採らない 706 の決定 (P1)、`-dry-run` がスレッドの本数を出すこと、言及の検索の固定費、親が lookback より古い投稿返信のスレッド、
表示名の焼き込み、所要の式の 0.5 秒、`cache.Raw` の中身、`cache.Version` の共有、`sinceFixed`、`edited` を持たないこと。
反証できなかった主張: 既定値・`NeedThreads` の条件・`Covers` の判定・`lastposts.json` の用途・取得の量を減らす仕組みが無いこと・落とし穴 1/3/4

## 関連ファイル

- `src/slack-bbs/main.go` (`collectChannel` / `parseFlags` / `load`)
- `src/slack-bbs/board/units.go` (`NeedThreads` / `Build`)
- `src/slack-bbs/source/source.go` (`History` / `Thread` / `Message`)
- `src/slack-bbs/source/throttle.go` (呼び出しの間隔)
- `src/slack-bbs/cache/cache.go` (`Raw` / `Summary.Covers` / `MsgKeys`)
- 706 (slack-bbs の設計メモ。スレッドの取り方の案 A / B)

## 進捗

- [ ] 今の取得の所要とスレッドの本数を測る
- [ ] 返信の編集の扱いをユーザーと決める
- [ ] 案を決めて実装する
