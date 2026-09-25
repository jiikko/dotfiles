# 437 (feat): 新しい依頼が来たら pro-con が PM を起こして知らせる

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

今の本物のモードは、PM (依頼を受けてカードを分解する Claude の session) を pro-con が起動しない。誰かが PM の session を開いて
`pro-con card guide` を渡しておかないと、`n` で出した依頼は「依頼」の列から先へ進まない。PM が開いていても、新しいカードが来たことを
知らせる経路が無い。**実際に pro-con で開発を回すときの最大の穴** (2026-09-25 のユーザーとの話で確認)。

## 設計 (2026-09-25 に決定)

### 形

- dispatcher の Tick (PG の割り当ての直前) が PM を扱う (`dispatcher/pm.go`)。PM の様子は `pm.json` (状態の置き場の下。書き手は dispatcher だけ) に持つ:
  短い id / 起動・再開の印 (`Launching` と `LaunchedAt`) / 印と一緒に渡している最中のカード (`Telling`) / 今の PM に知らせ済みのカード (`Told`) /
  一覧から消えたのを最初に見た時刻 (`DeadSince`) / 終了で止めた印 (`Stopped`)
- PM は 1 つ (415 の論点 6)。**起こす条件**は次のどちらか:
  1. 依頼の列に、今の PM にまだ知らせていないカードがある
  2. PM が生きていない (一覧に無い・止まった) のに、依頼の列にカードが残っている (知らせた後に PM が落ちた / 終了で止めた分を取りこぼさない)
- **起こし方は PG の回答と同じ経路** (`Launcher.Resume` = 止めてから `claude --bg --resume`)。PM の記録 (sessions.json の行) があれば再開、無ければ起動
  (`claude --bg -w pc-pm-<時刻> -n pc-pm-<時刻>`)。SendMessage は Claude の道具で dispatcher (Go) からは使えない (425 の結果 3)
- **PM が busy の間は再開しない** (止めて再開すると作業中の turn を殺す)。turn が終わって idle になってから知らせる。
  落ちて自動の再開を待っている (pid 無しの working)・一覧から消えた直後は restartWait 待つ (PG と同じ。待たずに再開すると 2 本立つ)。終了で止めた PM は待たない
- 知らせの文は新しいカード (ID・題・repo) と、まだ依頼の列に残っているカードの ID。起動のときは先頭に `pm-guide.md` の全文を付ける
  (**指示の正本は pm-guide.md だけ**。dispatcher は main から埋め込みの文を受け取り、自分では指示を書かない)

### 失敗モードと吸収

| 失敗モード | 吸収 |
|---|---|
| Tick が重なる | 起きない: Tick は dispatcher の 1 goroutine で順に回り、dispatcher は Lock で 1 つだけ。画面・CLI は箱に置くだけ |
| claude が失敗と返したが立っている / dispatcher が起動の途中で落ちた | PG と同じく、起動・再開の**前に**印を pm.json に書き、次の Tick で一覧と照らす (起動は名前 + cwd = PM の worktree、再開は session id か同じ cwd で、印の後に始まったもの)。launchGrace (1 分) の間は起動し直さない。過ぎても出なければ起動し直す |
| 同じ依頼で PM を 2 本起こす | 上の印の待ち + 再開は必ず前の session を止めてから + 落ちて自動の再開の途中 (pid 0) / 消えた直後は restartWait 待つ |
| PM が知らせの途中で落ちる | プロセスの死は Claude Code が同じ session で自動で再開する (transcript は続く)。止まった・消えたなら、条件 2 で依頼の列に残るカードを全部知らせ直す (少なくとも 1 回。重複して届くことはある) |
| dispatcher が落ちる | 状態は全部 pm.json と sessions.json (再起動を越える)。`Telling` は印と一緒に書き、取り込めたら `Told` へ移す |
| 利用枠が尽きかけ | 枠 95% 以上 (PG の新しい起動・再開を止める閾値) では PM も起こさない |

### 終了の保証と 447

- PM の session は `sessions.json` に **カード ID `PM`** の行で記録する (再開で入れ替わった前の PM は `sessions-retired.json` へ退く = 既存の `live.ReplaceCard`)。
  カード ID は `C-%03d` なので `PM` とは重ならない
- 終了 (`Shutdown`): stopCards の周で PM も止める (起動の途中で記録にまだ無い PM も一覧で取り込んで止める)。最後の `ensureStopped` (カードで絞らない) は記録の全行を見るので、PM も確かめて止め直す
- 447 (閉じたカードの PG を止める): `ensureStopped` をそのカードの ID で絞って呼ぶので、`PM` の行には当たらない。どちらもテストで固定する

### 利用枠のゲージ (--limit) に数えない

- `--limit` は「同時に動かす **PG** の数」(レビューと作業場所の混み具合の上限)。PM は常に 1 つで、依頼が来るまで idle で居続けるので、数えると
  limit 2 のとき PG が 1 本に減り続ける (idle の PM が枠を占める)。枠の 80% で「1 本まで」も PG だけに効かせる
- ただし枠 95% 以上 (新しく起動・再開しない) では PM も起こさない (起こしても、分けたカードの PG は起動しないので、枠を使う意味が薄い)

### PM の作業場所

- 設定 `pm_repo` (既定 `~/dotfiles`) の repo で `claude --bg -w pc-pm-<時刻>` として起動する = Claude Code が `<repo>/.claude/worktrees/pc-pm-<時刻>` に worktree を作る。
  🚨 **PM は repo の checkout 本体 (例 `~/dotfiles`) に書かない**。issue の commit は PM の worktree で行う。別の repo の issue は、その repo に PM 用の worktree を
  `git worktree add` で作ってそこで書く (pm-guide.md の「作業場所」)
- 名前に時刻を入れる: 前の PM の worktree が残っていても同じ名前で `-w` しない (既存の名前での `-w` の挙動は未実測)
- e2e モードは PM を起こさない (偽の PM = FakePM が役を持つ)

## 進捗

### 2026-09-25 (カード C-007 の PG)

やったこと:
- `dispatcher/pm.go` (tellPM / registerPM / pmAdopt / stopPM)・`store/pm_state.go` (pm.json)。Tick は PG の割り当ての直前に PM を扱う。
  PM の壊れ (pm.json が読めない等) は出来事に書いて PG の割り当ては続ける
- 終了: `Shutdown` の stopCards の周で PM も止める (記録に載る前の起動・再開の直後の PM も)。最後の ensureStopped は記録の PM の行 (と退いた前の PM) も確かめる
- 設定 `pm_repo` (既定 `~/dotfiles`)。main が pm-guide.md の埋め込みを `Dispatcher.PMGuide` に渡す (指示の正本は pm-guide.md だけ)。
  pm-guide.md に「作業場所」を足した (PM の worktree で commit・push し、本体の checkout に書かない / その worktree は消さない)
- C-006 (444) の出来事の型に rebase した (PM の出来事はカード ID `PM` で、kind は launch / hold / suspect / stop / error)

設計に後から足したもの (Opus の敵対的レビューの指摘で、再現できた分):
- 再開が返った直後 (記録の行はまだ前の PM) に終了すると、新しい PM が止まらずに残っていた → stopPM は記録の行に加え、今の PM の短い id (最後の起動・再開の後に始まったもの) も止める
- 落ち続ける PM を上限なしで約 90 秒ごとに再開していた → 生きていない PM を起こし直すのは 30 分に 3 回まで (`Revivals`。窓が過ぎたらまた起こす)
- PM の worktree が消えると再開が失敗し続け、依頼が永久に届かなかった → 記録の cwd が無ければ新しい worktree で起動する
- 知らない status (版で増えた値) の PM を idle とみなして止めていた → idle のときだけ止めて再開する (知らない値は出来事に 1 度書く)
- pid 無しの working が続く PM を期限なく待っていた → PG と同じく restartWait で見切る

確かめたこと:
- テスト: `dispatcher/pm_test.go` 19 本 (偽の launcher と一覧。本物の claude と state dir に触らない) + `config` 1 本。
  447 が PM を止めない / 終了では PM を止める、は `TestCloseDoesNotStopPM` で固定した
- 変異: `bin/mutate-verify` で 21 本。すべて想定したテストが red (busy の待ち・launchGrace・起動の取り込み・restartWait・自動の再開の待ち・
  条件 2・枠の閾値・知らせ済みへの移し・前の PM を退かせる・終了の取り込み 2 本・PMRepo 空・stopPM の呼び出し・close の絞り込み・
  再開の直後の終了・起こし直しの上限 2 本・worktree の消失・知らない status・pm.json の壊れ)
- make test: 1 回目は gofmt の 1 件で rc=2 (テストは全部 ok)。直して頼み直したら rc=0 (4m10s。テストの係の実行)

残り:
- 🚨 **本物の claude ではまだ走らせていない** (起動・再開の引数は PG と同じ ExecLauncher)。PM を実際に起こして、知らせが turn の区切りで届くか・
  `-w pc-pm-<時刻>` の worktree ができるかを dogfooding (440) で確かめる
- 未確認のリスク: 知らせの turn の途中で PM が落ち、Claude Code の自動の再開がその turn を続けないと、そのカードは知らせ済みのまま届かない
  (PM が生きているので条件 2 に当たらない)。自動の再開が turn を続けるかは未実測
- 画面に PM の様子 (居る / 知らせ待ち) を出していない。PM の数を設定で変える話は 456
- 別の repo の issue を PM がどこで書くか (その repo に PM 用の worktree を作る) は指示書に書いただけで、機械では強制しない

### 2026-09-25 レビューの差し戻し: PM を起こさない口

- **dispatcher の `--pm=on|off` と設定の `pm = "on"|"off"`。`--pm` を書けば設定より勝つ** (起動ごとに明示した方を優先する。設定で off でも
  `--pm=on` で 1 回だけ起こせる)。README の設定の節に書いた。画面が起こす dispatcher は `--pm` を付けないので、画面から使うときは設定で決める
- off のときは PM を起動も再開もせず、依頼の列のカードはそのまま置く (`pm.json` にも書かない)。`PMRepo` は残すので、前の dispatcher が起こした PM は終了で止める
- off で起動した dispatcher は「PM を起こさない (--pm=off / 設定 pm = "off")。依頼の列のカードはそのまま置く」を出来事 (events.jsonl) と dispatcher のログに 1 行出す。
  文は dispatcher に渡した `PMOff` から出す (配線の渡し忘れも告知のテストで捕まる)
- 設定の `pm` と `--pm` の書き間違いは誤り (on と読むと、止めたつもりの PM が起動して枠を使う)
- 確かめたこと: テスト 4 本 (`TestPMOffLeavesRequestedCards` / `TestResolvePM` / `TestRunDispatcherAnnouncesPMOff` / `TestLoadPMMode`)、
  変異 7 本すべて想定したテストが red (tellPM の off の判定・--pm=off・設定 off・--pm=on が設定に勝つ・告知・PMOff の配線・設定の値の検査)。
  敵対的レビューは省いた (値で分岐を 1 つ止める口で、状態遷移・外部 I/O の新しい経路は無い)
- origin/master (C-008 = 451 の削除) に rebase した。451 の削除も止める相手をカード ID で絞るので PM には当たらない

## 関連

- 427 の残っていること / 415 の論点 6 (PM の数と役割)
- dogfooding (issue 440) では、PM の役を人間 (か Claude の対話 session) が CLI で代わりに行う

## PM (人間の代わりの Claude) の決定 (2026-09-25)

- カード C-007 で PG が実装。1 回目のレビューで差し戻した: **PM を起こさない口** (dispatcher の flag と設定) を足す。今は手で PM をしているので、
  取り込んで dispatcher を入れ替えた途端に本物の PM が起きて、同じカードを 2 人で分け合う形になるため
- **分担はユーザーが決めた** (「両方: 依頼の分解だけ自動」): 437 が入ったら、依頼の分解 (issue に分けてキューに積む) と PG の質問への回答は自動の PM、
  レビュー・差し戻し・閉じる・master への取り込みは人間の代わりの Claude。取り込むときに `pm-guide.md` の役目 5 をこの分担に合わせて書き直す
- 取り込むときに確かめること: 457 (カード C-010。記録に無い session も止める対象に入れる) と組み合わせて、PM の session を PG と取り違えずに終了で止めるか (468 の「同じ判断を変える組」)
