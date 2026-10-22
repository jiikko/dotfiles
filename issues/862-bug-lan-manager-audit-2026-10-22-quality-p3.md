# 862 (bug): lan-manager 監査 (2026-10-22) の品質系の P3

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-22〜）

起票日: 2026-10-22

## 概要

lan-manager の全体監査 (865) の品質系のうち P3 の小物をまとめる。項目ごとに独立して直せる。
「未確認」と書いたものは、コードの読みで経路を確かめただけで、実行による再現はしていない。

## 詳細

### 1. ping の `lan_mismatch` の rc がテストで固定されていない (847 の範囲)

- `ping.go:runPing`。有線の名前が内部 IP と違う IP に引けた (`lan_mismatch`) とき、CLI は ✓ に `🚨 内部 IP (…) と違う` を付けて rc=0 を返す。
  画面の `reachCell` は同じ状態を `W✗` にする。README と usage は rc を「0 = 確かめた名前が全部届く」と定義し、lan_mismatch は「知らせる」とだけ書くので、
  今の挙動は文書の契約どおり (false-green とは言えない)
- 問題は検査の側: `TestPingMarksWiredNameElsewhere` は rc を捨てているので、rc をどちらに倒しても緑 (読みでの判断。変異は未確認)
- 画面 (✗) と CLI (rc=0) を揃えるかは 847 の設計の判断。どちらにするにしても、テストで rc を固定する

### 2. 相手の読み取りの部分失敗を「無い」に畳む (error-handling・未確認)

- `netinfo/netinfo.go:Read` と `setup/remote.go:ObservePeer`。期限切れ以外で ifconfig や -getinfo が 1 本だけ失敗すると
  (多重化した ssh の 255 など)、そのサービスは Active=false か Read=false になって `Offline()` から黙って外れ、`NetKnown=true, Service=""` になる
- 初期設定は「相手にルーターの無い有線のサービスが無い」で止まり、`dump --peer` は「(無し)」と言い切る。ルーターの無い有線が 2 つある相手では、もう一方を選びうる
- 期限切れ以外で読めなかったサービスがあれば、相手側は `NetKnown=false` (判定できない) にする。ただし I/F の無いサービス (VPN。Device="") と
  抜いたアダプタ (`IfGone`) は普段から読めないので除く (除かないと、VPN を持つ相手やアダプタを抜いた相手が毎回「判定できない」になる)

### 3. 有線の名前の「広告されていない」と「引くのが遅い」を区別できない。テストの偽物は本物と違う形で「広告されていない」を模す

- `lanmgr/wiredname.go:wiredCheck` は `w.err != nil` (期限切れなど) を wiredMissing にし、`setupPending` が `!` を立てる
- 実測 (反証レビュー): 存在しない名前の `dscacheutil -q host -a name audit862-nonexistent-q7.wired.local` は real 10.02 秒・rc=0・出力 0 byte。
  `wiredLookupTimeout` は 3 秒なので、**広告されていない名前は実機ではほぼ必ず期限切れの err で返る** (0 件では返らない)。
  今の「err なら missing」は実機の「広告されていない」を正しく拾っている。「err は unknown、0 件だけ missing」に直すと `!` がほぼ立たなくなるので採らない
- 残る問題は 2 つ: (a) mDNS が遅いだけの期限切れも missing (偽の赤) になり、区別する手段が無い。(b) `TestWiredNameNotUsedUnlessItMatches` (`lanmgr/model_test.go`) は
  「まだ広告されていない」を `(nil, nil)` で模していて、本物 (期限切れの err) と違う。上の誤った直し方をしても緑のまま
- 方向: まず偽物を本物の形 (期限切れの err) に揃える。(a) は、区別できる観測 (別の手段で mDNS の応答を見る) が見つかるまで受容

### 4. (却下) 有線の名前の広告の入れ替えの途中で bootstrap が落ちる

- `setup/wiredname.go:InstallScript` の bootout 直後の bootstrap の競り。done の 825 の「参考」で「air5 で入れ直しが 2 回通っているのでバグとしては挙げない。
  rc=5 で失敗したらここを疑う」と見送り済みで、今回も新しい実測は無い。825 の trigger (rc=5 の失敗) が来るまで再提起しない

### 5. Bonjour の名前の `\\` の戻し方の順 (broken-code・実行で確認)

- `discover/discover.go:unescape`。`\ddd` を戻してから `\\` を戻す順なので、`a\\032b` が `a\ b` になる (正しくは `a\032b`)。表示だけで、`-L` には生の名前を渡すので繋ぐ先は変わらない
- 左から 1 回の置換 (`\\(\d{3}|.)`) で読む

### 6. 台帳の登録が lock を 2 回に分けて取る (concurrency K1・未確認)

- `lanmgr/model.go` の space の確認の `yes` と `lanmgr/setup.go` の台帳の手順は `Ledger.Update` → `ErrNotFound` なら `Upsert`。
  2 つはそれぞれ `modify` で lock を取り直すので、間に別のプロセス (glogx の L / 単体の CLI) が同じ Mac を別名・LAN 付きで載せると、
  Upsert が開いたときの値で丸ごと置き換え、別名・LAN が消える。窓は µs〜ms (done の 824 の 7 と同じ失敗が、窓を狭めて残っている)
- ledger に「1 回の lock の中で、在れば f で更新し、無ければ挿入する」口 (例: `Merge`) を足し、2 か所を寄せる。lint で止めるのは 864 の L1

### 7. 単体の TUI を閉じると、広告を止める ssh の結果が失われる (resource-leaks・一部未確認)

- `lanmgr/model.go:unadvertise` の goroutine (`m.bg`) は `context.Background()` + 20 秒で走り、`Model.Close` は取り消さない。
  台帳から外した直後に単体の TUI を閉じると、ssh の子は孤児になり、結果の知らせ (失敗時の `RemoveHint`) が出ない。glogx は Model を使い回すので `takeBG` が次に開いたときに出す。単体のバイナリだけの問題
- 孤児になった ssh が最後まで走るかは未確認 (`RemoveScript` は成功時に何も出さず、ssh は SIGPIPE を無視するので、走りきると読んでいる)
- 終了時に `len(m.bg) > 0` なら短く待つか、終わっていないと知らせる

### 8. 速度測定と疎通の確認の間、見出しが「有線の名前を確かめている」と出る (ux・コードで確認)

- `Busy` は `m.speedRun != nil` と `len(m.reachRuns) > 0` を含むが、`view.go:header` の待ちの文の switch に 2 つの case が無く、最後の
  `" 有線の名前を確かめている"` に落ちる (確認待ちがあれば「固定 IP の確認待ち」)。構造の側 (裏の仕事の一覧が 4〜5 か所に分かれる) は 863 の P3
- header に 2 つの case を足し、その組み合わせのテストを足す

### 9. 相手の観測で使わない I/F の IP を ssh 1 本ずつで集める (performance・見積もり)

- `setup/remote.go:ObservePeer` は `netinfo.Read` の `IPs` を使わない (コメントに「IPs は使わない」) が、`Read` は `ifconfig -l` と I/F ごとの `ifconfig <dev>` を相手へ 1 本ずつ ssh で打つ
- この Mac (lo0 を除く I/F 31・サービス 5) で数えると、1 回の観測で ssh が約 45 本、そのうち 32 本 (約 70%) が捨てられる
- 見積もり (実測していない): 多重化が効いて 1 本 40ms なら 1 回 1.3 秒の無駄。多重化が効かない (`controlPath()` が "" を返す) と 1 本 0.8 秒前後で、
  IP の段が `ObserveTimeout` の 15 秒を食い、続く `StatusScript` が期限切れの ctx で走って `AdvertKnown=false` になりうる
- `Read` に IP の段を飛ばす口を作る (ObservePeer は要らない)。または I/F ごとの呼び出しを `ifconfig` 1 本にまとめる

## 関連ファイル

項目ごとに上に書いた。

## 進捗

- [ ] 1 ping の rc をテストで固定 (画面と揃えるかは 847 の判断)
- [ ] 2 部分失敗を判定不能に (VPN・IfGone を除く)
- [ ] 3 wiredCheck のテストの偽物を本物の形に揃える
- [x] 4 却下 (825 で見送り済み)
- [x] 5 unescape (dotfiles-0e。commit「fix(mac-lan-manager): Bonjour の名前のエスケープを左から 1 回で読む / .golangci.yml の写し残りを消す」)
  - `\ddd`・`\.`・`\\` を 1 本の正規表現 (`reEsc`) で左から読む。戻す対象は今までと同じ 3 種類
  - 変異: 旧の 2 段に戻す → TestUnescape が `a\\032b` = `a\ b` で red。敵対的レビューで旧と新の違い (長さ 6 まで 478 件) が全部旧の二重の解読と確認
  - 残り (この変更の前から): `\128` 以上を UTF-8 のバイトでなく rune として戻す (`\227\131\159` が化ける)。dns-sd が非 ASCII をこの形で出すかは未確認
- [x] 6 台帳の Merge (dotfiles-0e。commit「fix(mac-lan-manager): 台帳の登録を 1 回のロックで行い、行を丸ごと置き換える口を Store から外す」)
  - `ledger.Store` に `Merge` (在れば f、無ければ足す。1 回のロック) を足し、space の登録と初期設定の台帳の手順を寄せた。初期設定の手順は r.managed なら今までどおり Update だけ
  - `Upsert` は Store から外し、`FileStore.Upsert` はテストのファイル (`ledger/upsert_test.go`) へ移した (864 の A7)
  - 変異 5 本で red を確認 (commit message に一覧)。敵対的レビュー 2 周 (opus)。`TestMergeTakesLockOnce` が見るのは「2 回の書き直しに戻す」形だけで、ロックの外で読んでから 1 回で書く形は見ない (テストのコメントに明記)
- [ ] 7 unadvertise と Close
- [ ] 8 header の待ちの文
- [ ] 9 ObservePeer の IP の段
