# 863 (refactor): lan-manager 監査 (2026-10-22) の設計系

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の設計系 7 タイプ (design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components)。
1 つの発見は一番狭く当てはまる型に 1 回だけ帰属させた。どれも「今は正しい (か、1 か所だけ食い違っている) が、動作や画面の層を足すたびに
複数箇所の更新が要り、漏れが silent に通る」形。今すでに出ている症状は品質側の issue に切り出してある (862 の 8・861・860 の 1・859 の 2)。

読んだ木は 85f306a6 (速度測定の進捗モーダル) の後。直すときは HEAD で読み直す。reach (P1 の p の案内・P7) は 847 の作業範囲。

## 詳細

### P1 (polymorphism・P2) 行の Mac への動作の可否・案内・カード・実行がキーごとに散る

- 規則の置き場: `model.go` の `keyList` の各 case・`Binds`・`needsNet`・`rowKeys`・`detailKeys`・self の断りの列、`view.go` の `rowHints`・`detailLines`・`overlayBox` の ovHelp の表。
  断りの述語が 1 か所にあるのは s (`sshRefusal`)・t (`speedTarget`)・p (`reachTarget`) だけで、Y / y / O / o / a はその場で判定する
- 今の食い違い: `rowHints` は先頭の `if r.self || r.connectIP() == "" { return nil }` で p の案内も消すが、`startReach` は connectIP を見ないので p は効く
  (見えていない管理対象の行でだけ p の案内が出ない。847 の範囲)。同じ「繋ぐ IP が無い」でも s / o / a は `awayReason` を出し、Y / y / O は素の文
- 実例: a (848, d2fb37a6) を足したとき model.go と view.go の 9 hunk を触った
- 方向: `rowAction{key, label, prio, needsNet, refusal, run}` の表 1 つから keyList・rowHints・detailLines・needsNet・rowKeys・detailKeys・Binds を引く

### P2 (polymorphism・P2) 画面の層の優先順を 10 か所の読み手が組む

- 層は独立したフラグ (`m.sc`・`m.ov`・`m.detailOpen`・`m.filtering`・`m.bandEdit`・`m.small`・`progressShown()`・`speedShown()`・`ask.auto` / `selfOnly`)。
  読み手は `HandleInput`・`OwnsKeys`・`Binds`・`footer`・`overlayBox`・`View`・`CaretPos`・`Paste`・`Close`・`logKey` / `screenName`
- 今の食い違い (報告。私は未照合): 初期設定の進捗モーダルの間も `footer` は「x 実行 / r リロード / e メッセージ」を出す。
  85f306a6 で板の間の最下行を板のキーにしたのは ovSpeed だけで、ovConfirm / ovCopyIP / ovAlias / ovLog / ovHelp の間は一覧のキーを案内する
- 履歴: 85f306a6 の敵対レビューで `OwnsKeys` が speedShown を数え忘れていた。retro 788 の (1) も同じ構造
- 方向: `func (m *Model) layer() layerID` で一番上の層を 1 つ決め、OwnsKeys・footer・HandleInput・overlayBox をその switch にする。層ごとに `hints()` を持たせる (U3 も消える)

### P3 (polymorphism・P2) 裏の仕事の一覧が Busy / header / Advance / Close に別々に書かれる

- `Busy` (9 条件の OR)・`header` の待ちの文 (7 case)・`Advance` の take* の並び・`Close` / `Reload` の stop*。speed (827) と reach (847) はどちらも header を漏らした (症状は 862 の 8)
- 方向: `bgJob{busy, label, take, stop}` の列にして 4 か所はそれを回すだけにする

### P4 (encapsulation E2・P2) 固定 IP の試みのライフサイクルを 5 つの map で呼び出し側が守る

- `Model.pending`・`claimed`・`unconf`・`tried`・`confirmed`。書き手 `markUnconf`・`setupExecDone`・`expireConfirms`・`applyObserve`、
  読み手 `ledgerBlocked`・`usedIPsExcept`・`destFor`・`conf`・`awaitingUnconf`・`unconfOverdue`・`Busy`・`header`。
  `setupExecDone` は `destFor` の「mainIP が無ければ確認済みの IP」の規則を自分で書き直している (規則が 2 か所)
- 遷移が増えると (取り消し・台帳から外したときの片付け。今は confirmed / claimed / tried が残る)、全部の書き手で揃え直す必要がある
- 方向: host ごとの `fixAttempt` 型に `Mark`・`Started`・`Confirmed`・`Expire`・`Observed`・`Blocks`・`EntryIP` を持たせ、Model は map を 1 つにする

### P6 (encapsulation E3・P2) ssh の宛先とホスト名が生の string で越境し、検証が経路ごとに違う

- 同じ ssh の宛先を組む場所が 6 か所 (`keyList` の s・Y のコピー・`detailLines`・`speedTarget`・`keySpeed`・`destFor`・`dump.go`)。名前の述語も 4 つ
  (`termsafe.IsPlain`・`setup.reAlias`・`setup.reLabel`・`ledger.Key`)。`setup.Dest` は IPv4 しか表せないので、有線の名前を宛先にする s / o / a / Y は Dest を通れない (症状は 860 の 1)
- ssh の宛先 (Dest・SSHArgv・controlPath) が初期設定の package に置かれ、`speedtest` はそのためだけに `setup` を import している (循環は無い)
- 方向: `peer.Dest{User, Target, Alias}` と Validate / SSHArgv を独立した package に置く。ただし Y のコピーが HostKeyAlias を持たない形は
  テストで固定された意図的な形 (`model_test.go`) なので、変えるかはユーザーの判断

- 2026-10-22: 症状の側 (s の HostKeyAlias が reAlias を通らない) は 860 で直した (`setup.ValidAlias` を sshRefusal で使う)。宛先と名前が生の string のまま越境する構造は残る

### P7 (encapsulation E1・P3) 有線の名前の判定を setup の外に写している

- `lanmgr/reach.go:reachLines` と `ping.go:toPingName` の `strings.HasSuffix(n.Name, ".wired.local")`、`reachCell` の `t.Names[1]` (位置で判定)、
  `reach.Name.Elsewhere` と `wiredname.go:wiredCheck` の `ContainsFunc(ip != lanIP)`。正本は `setup.WiredName`。847 の範囲
- 方向: `ReachTarget.Names` を `[]struct{Name string; Wired bool}` にして判定を 1 つの関数へ

### P8 (leaky-abstraction L3・P3) 表示の文言で振る舞いを分ける

- `view.go:selfStaticKey` の `enterLabel == "初期設定"`。パネルの語を直すと、この Mac のパネルの案内が一覧の語に変わる
- 方向: `detailLines` に `inPanel bool` を渡す

### P9 (duplication・P3) `setup.Step.Via` は計算・テストされるが本番で誰も読まない

- `plan.go` が Via を計算し、`setup_test.go` が値を検査するが、本番の読み手は 0 件 (`grep '\.Via\b'` を mac-lan-manager と glogx の非テストに当てて 0)。
  実際に入る IP は `lanmgr/setup.go:destFor` が別の規則で決める
- 方向: Via を消すか、destFor の唯一の出典にする

### P10 (duplication・P3) `runSpeed` と `runPing` の骨組みの重複

- flag の読み取り → `newModel` → `settle` → scanErr → 相手を選ぶループ → stop を待つ goroutine → JSON がほぼ同じ (`pickTarget` と `settle` は共有済み)。
  止める合図の見方が既に分かれている (症状は 859 の 2)
- 方向: 「相手を選ぶ」と「stop を受けて 1 台ずつ走らせる」を helper にする
- 2026-10-22: 「stop を受けて走らせる」は 859 で `untilStop` (`dump.go`) に寄せた。「相手を選ぶ」ループと JSON の出力は残る

### P11 (duplication・P3) 外のコマンドの失敗に stderr を付ける実装が 2 つ

- `live/live.go` の `RunQuiet` と `live/ssh.go` の `SSHRun` / `RunError`。片方だけ形を変えると `unadvertise` の ExitCode の判定と知らせの文が分かれる

### P12 (leaky-abstraction L5・P3) 画面を出さないサブコマンドが、Options を 1 つずつ nil にして自動の仕事を止める

- `main.go:quietModel` が `o.Log = nil` と `o.Reach = nil` を手で外す。Advance に自動の仕事を足すと、quietModel を直さない限り dump や speedtest の中でも走る
- 方向: `Options.Passive bool` を 1 つ持つ

### U1〜U3 (ui-components・P3)

- U1: `JoinVertical(header(w), gradRule(w, …), "")` が `view.go` に 3 回 (初期設定・設定・一覧) → `pageHead(w)`
- U2: 「項目名は最初の行だけ」の `next := func()…` が `speedLine` と `reachLines` に 2 回 → `labeledGroup(label)`
- U3: 同じ板のキーの案内が `speedBox` の hint と `footer` の ovSpeed の keyHint に literal で 2 回 (測定モーダルも同じ) → P2 の `hints()` に寄せる

### 0 件

- responsibility: 分割で 3 問が良くなる候補は P2・P3・P4 に出したものだけ。`rows()` を inventory 型に切り出す案は注記の前提が今も成り立つので出していない
- design: 循環依存なし (`go list`)。glogx は公開の口だけを使う

## 対応方針

P1〜P4 は「層 / 動作 / 裏の仕事を 1 つ足すたびに N か所」の形で、次に動作・画面を足すときが trigger。先回りで全部はやらず、
次の機能追加の前に該当する 1 つを寄せる (`verify-design-intent-before-refactor.md` の trigger 待ち)。P6 は 860 を直すときに一緒に見る。

## 関連ファイル

項目ごとに上に書いた。

## 進捗

- [ ] P1 行の動作の表
- [ ] P2 画面の層
- [ ] P3 裏の仕事
- [ ] P4 固定の試み
- [x] P6 ssh の宛先 (dotfiles-c3。commit「refactor(mac-lan-manager): ssh の宛先 Dest を sshdest へ移し、s / Y / 詳細のカードの宛先を login 1 つから組む」)
  - `setup.Dest`・`Validate`・`SSHArgv`・`controlPath`・`ValidAlias` を中身を変えずに新 package `sshdest` へ移した。speedtest が import するのは ledger と sshdest だけになった
  - s・Y・詳細のカードの `user@<target>` を `lanmgr` の `Model.login` から組む。target は呼び出し側が 1 回だけ取って渡す (カードの ssh と smb の行で宛先を揃える)
  - 採らなかった案 (設計の反証レビュー、opus): Dest を `{User, Target, Alias}` に広げて有線の名前も通す案。同じ名前の `Validate` の意味が緩むと、IPv4 を前提にする
    呼び出し元 (ConfirmArgv の IP の比較・ssh -G・controlPath の %C・速度の経路の突き合わせ) の置き換え漏れを compiler が拾わない。s に Target の検査を足す案も、
    `shellRefusal` が IP の空を先に断るので届かない (足すと最下行・カードの案内と s の述語がまたずれる)。名前の述語 (`reLabel`) の一本化は P7 (847) に残した
  - 変異 (`mutate-verify`): login が有線の名前を落とす → `TestWiredNameUsedAsTarget` / ユーザー名を落とす → `TestSSHUsesLanIPAndHostKeyAlias` ほか 6 本が red
  - 実装の敵対的レビュー (opus、読み取りのみ): 挙動の変わる経路は作れなかった。P3 の 2 件 (カードで target を 2 回取る・865 の旧い置き場所の参照) を直した
- [ ] P7 有線の名前の判定の写し (847 の範囲。847 の作業に任せる)
- [x] P8 (dotfiles-0e。commit「refactor(mac-lan-manager): 詳細の enter の案内を文言で分けない / 見出し・項目名・モーダルのキーの重複をまとめる」)。detailLines / selfStaticKey は「パネルの中か」の真偽を受ける。既存のテストは最下行の案内にも同じ語が出るせいで、パネルの案内が消えても緑だった (変異で確認) ので、カードの台帳の行だけを見る形にした
- [x] P9 (commit「refactor(mac-lan-manager): 本番で誰も読まない setup.Step.Via を消す」)。destFor の出典にする案は採らない (Plan は確認済みの IP を知らない)
- [x] P10 は 859 (b9) で止める合図 (untilStop)・相手の選び方 (pickTarget)・待ち方 (settle) が共有になった。残りは出力 (行・JSON) の形が違う部分で、寄せても減る複雑性が小さいので今は寄せない
- [x] P11 (commit「refactor(mac-lan-manager): RunQuiet を SSHRun に寄せ、失敗に stderr を付ける実装を 1 つにする」)
- [x] P12 (commit「refactor(mac-lan-manager): 画面を出さないサブコマンドの自動の仕事を Options.Passive 1 つで止める」)
- [x] U1〜U3 (P8 と同じ commit)。U3 で経路を選ぶ板の中の案内が最下行と同じ書式 (「j/k 経路   y 実行   n/esc キャンセル」) になった
- 残り: P1〜P4 は trigger 待ち (次に動作・画面の層・裏の仕事・固定の遷移を足すとき)、P6 は済み (上)、P7 は 847。
  dotfiles-0e は 2026-10-22 に claim を外した (上の済んだ項目のほかに、今すぐ進めるものが無いため)
- [x] 862 で解消 (2026-10-22。P3 の 9 件 (却下 1) を全部決着。done へ移した)
