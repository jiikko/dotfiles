# 500 (bug): tmux のクライアントの接続ごとにカーネルのメモリ (data.kalloc.1024) が漏れ、zone map を使い切ってカーネルパニックする

起票日: 2026-09-26

> 🚨 **2026-09-27 12:55 時点の見立て: 漏らしているのは tmux ではなく「Claude Code (claude.exe) が動いている macOS 15 / 26」の可能性が高い**
> (上流に同じ zone・同じ 2,100 万個 / 20GB の報告が複数ある。下の「2026-09-27 12:20〜 MacBook Air」節)。タイトルは起票時の仮説のまま残す (ファイル名を変えると参照が切れる)。

## 概要

2026-09-25 23:53 に macOS がカーネルパニックで落ちた (pro-con の dogfooding 中。ユーザーの依頼「macos が今クラッシュしたので原因を調べて」)。
パニックの文は `zone map exhausted while allocating from zone [data.kalloc.1024], likely due to memory leak in zone [data.kalloc.1024] (20G, 21202480 elements allocated)`。
カーネルの `data.kalloc.1024` (513〜1024 バイトの割り当て) が約 2,120 万個・20GB 溜まり、zsh の exec (AMFI の署名の検査) の割り当てで尽きた。
そのときの wired は約 28GB (1,727,407 ページ × 16KB)。起動から 83 日目 (前回の起動は 2026-07-04)。

## 実測 (2026-09-26、macOS 15.7.7 (24G720) / Mac15,10 / tmux 3.7b)

- 前兆: 2026-09-25 20:45 から、カーネルが zone map の枯渇 (jetsam の `osr_code: 9`) を理由にアイドルのプロセスを殺し始めた (パニックまでに 200 件)
- 再起動後の `zprint` の `data.kalloc.1024` の inuse を測り、操作ごとの差を取った (差は操作の前後。ほかの常駐の揺れは 10 秒で ±100 程度):

| 操作 | 回数 | 増えた数 |
|---|---|---|
| `/usr/bin/true` の exec / Homebrew の zsh の exec | 各 2,000 | 102 / 42 (揺れの範囲) |
| pipe・pty の確保・unix socket の往復・SCM_RIGHTS で fd を 3 本渡す | 500〜2,000 | -44〜4 (揺れの範囲) |
| `proc_pidinfo` (VNODEPATHINFO / SHORTBSDINFO)・`proc_pidpath` | 各 3,000 | 3〜33 (揺れの範囲) |
| **本番の tmux サーバへの `tmux display -p`** | 2,000 | **2,100 / 2,168 / 2,049** (3 回。30 秒たっても戻らない) |
| 隔離したサーバ (`-L`・attach しているクライアント無し) への `display -p` | 2,000 | 89〜237 (ペイン 62 個・実設定を読ませても同じ) |
| **隔離したサーバに pty で attach したクライアントを 1 つ置いて** `display -p` | 2,000 | **1,061** |

→ **attach しているクライアントがあるサーバに、tmux のコマンド (クライアントの接続) を送るたびに、約 1 個 (約 1KB) が漏れて戻らない**。
attach が無ければほぼ漏れない。何が漏らしているか (tmux のサーバが attach 中のクライアントに何を書くか / カーネルのどの経路か) は未特定。
- 増え方: 再起動 (2026-09-25 23:52) から 1 時間で 1,617 → 224,793 (`tmp/zone-watch` の 1 分ごとの記録)。**17 時間後の 2026-09-26 16:46 で 1,617,783**
  (約 9.6 万個 / 時。pro-con の PG・PM・取り込みの係と画面が tmux を呼ぶ量が増えた日)。前回の 2,120 万個に届くのは、このペースなら**約 8〜9 日後**

### 2026-09-27 11:50: まだ増えている (ゆっくり)

- 在庫: **3,199,850** (08:1x の 3,120,190 から約 3.6 時間で約 8 万増。平均 約 2.2 万個 / 時)。何もしない 60 秒で +280 (10 秒ごとに ±100 で揺れながら増える)
- 残りの余裕: 1,500 万個まで約 1,180 万個。約 2.2 万個 / 時が続けば約 22 日。make test の多い日 (約 10 万個 / 時) なら約 5 日
- 在庫は減っていない (再起動するまで戻らない)。make test の段ごとの測定はまだしていない

- 何が漏らしているかを確定する手段 (2026-09-27 に整理): ①`make test` の段ごとに在庫の差を取る (何をすると漏れるかまで。今すぐできる)
  ②カーネルの zone logging (起動引数 `zlog=data.kalloc.1024`。確保した呼び出しの流れまで記録できる。SIP を切って再起動が要るので人の判断)。
  カーネルのメモリはプロセスの持ち物として数えられないので、アクティビティモニタや ps からはどのプロセスかは見えない
- 回収: ログアウト (自分のユーザーのプロセスを全部終わらせる) は未試行。クライアントの終了・kill-server・アクティビティモニタの停止では減らなかったので期待は薄い

## 次に調べること

- attach しているクライアントの何が効いているか: status-interval (本番は 1 秒) を止める / status を切る / クライアントの数を変える、で 1 接続あたりの増え方を比べる
- tmux の版 (3.7b) を変えて比べる。同じ形の報告が上流 (tmux / Apple) にあるかを探す
- どの経路が 1KB を確保しているか (kalloc の 1KB は MAXPATHLEN のパスの確保が典型。tmux のサーバが attach 中のクライアントへ何かを返すときの経路を疑う)
- 当面の手当て: 漏れの量を毎日見て、1,500 万個を越えたら再起動する (パニックの前に)。pro-con が tmux を呼ぶ回数を減らせるか (`TmuxPublish` の件数の更新 等)

## 進捗

### 2026-09-27 01:30〜01:45: 回収できるか / 接続単位かを測ろうとして、tmux の漏れが再現しなかった

- 測定前の在庫は 2,993,227 (起動から 25.6 時間。平均で約 11.7 万個 / 時)
- 隔離サーバ (`TMUX_TMPDIR` を使い捨てにし、`-L`、クライアントは pty で attach) で 2,000 回ずつ測った。
  漏れはどれも揺れの範囲 (±600) で、上の表の「attach したクライアントがあると 1,061」は再現しなかった:
  - `-f /dev/null`: 接続 2,000 回 = +73 / -64、control mode の 1 接続で 2,000 コマンド = +76 / +247
  - `status-interval 1` + `tmux-256color`: 接続 = +392 / +598、1 接続 = +445 / +25
  - クライアントの終了・`kill-server` の後も在庫は減らない (漏れていないので、回収の検査にはなっていない)
- 同じ時間帯にアクティビティモニタ (起動直後からずっと動いていて、CPU 99% のまま固まっていた) を、ユーザーの依頼で SIGTERM で止めた
- 止めた後、**本番の tmux サーバに `display -p` を 2,000 回送っても -117** (上の表では 3 回とも +2,049〜2,168)。
  アイドルの 2 分間も ±700 で、在庫は約 301 万個のまま横ばい。止めた直後の 60 秒だけは +10,713 増えたが、原因は分かっていない
- **仮説 (未検証)**: 漏らしていたのは tmux ではなく、プロセスを監視しているアクティビティモニタだった。
  つまり、tmux のクライアントのプロセスが 1 つ生まれるたびに、アクティビティモニタ側の処理が約 1KB を漏らしていたのではないか。
  上の表の測定はすべて、アクティビティモニタが動いている間に取ったもの。
  ただし、アクティビティモニタが動いていた 01:33 の隔離サーバの測定でも漏れていないので、これだけでは説明が付かない
  (その時点ですでに固まっていて、監視していなかった可能性はある)
- 在庫の回収: 漏れたカーネルメモリは、持ち主が解放しない限りユーザー空間から回収する手段は無い。
  アクティビティモニタを止めても在庫は減らなかったので、在庫を消すには再起動しかない (漏れが止まっていれば急がない)

### 2026-09-27 08:1x: アクティビティモニタが動いていても、tmux の 1 回ごとの漏れは再現しない。増えるのは `make test` の間

- 在庫: **3,107,021** (01:45 の約 301 万から約 6.5 時間で約 10 万増。平均 約 1.5 万個 / 時。前日の約 10 万個 / 時より少ない)
- アクティビティモニタは 01:36 に起動し直されていた (PID 98323、CPU 0.2% で固まっていない)。**動いている状態で**:
  本番の tmux に `display -p` 2,000 回 = **+411** (前日の表は +2,049〜2,168) / `/usr/bin/true` 2,000 回 = +55
  → 「アクティビティモニタが動いていると tmux の 1 回ごとに漏れる」はこの形では再現しない (前日は CPU 99% で固まっていた。固まっている間だけ、の可能性は残る)
- 何もしない 60 秒の増え方: +45 → +3,283 → +8,550 (3,120,190)。増えた 2 分は、テストの係が C-099 の `make test` (dotfiles 全体) を走らせていた時間と重なる。
  `make test` は隔離した tmux サーバを立てて pty で attach するテストや、対話の zsh を何度も起動するテストを含む。**どのテストかは測っていない**
- 見立て (未検証): 漏らしているのは tmux の常時の動きではなく、`make test` の中の何か (attach したクライアントを持つ隔離サーバか、exec の多い段)。
  パニックの文の割り当ては zsh の exec (AMFI の署名の検査) だった
- 残りの余裕: 1,500 万個まで約 1,190 万個。平均 1.5 万個 / 時なら約 33 日、前日の約 10 万個 / 時 (テストの多い日) なら約 5 日

### 次の一手

- A-B 比較: アクティビティモニタを起動した状態と止めた状態で、本番の `display -p` 2,000 回と `/usr/bin/true` の exec 2,000 回を測る。
  起動しているときだけ漏れるなら、この issue は tmux ではなくアクティビティモニタ (または macOS の libproc の経路) の問題になる
- 在庫の推移を数日見る。横ばいのままなら、当面の再起動は不要
- `make test` の段ごとに前後の在庫を取る (tests/tmux だけ・zshrc だけ・Go だけ)。増える段が分かれば、その段の中の 1 本まで絞る (本番の tmux は触らない。隔離サーバのテストはそのまま)

### 2026-09-27 12:20〜12:55 KOJIm2-MacBook-Air: 別のマシン (macOS 27) では何をしても漏れない。上流に claude.exe 起因の同じ報告がある

- 環境: macOS 27.0 (26A428) / Mac14,2 (24GB) / tmux 3.7 / 起動 2026-09-18 10:18 (9 日)。SIP 有効。サードパーティの ES クライアント・system extension のうち ES のものは無し (network extension と DriverKit のみ)
- 在庫: **起動 9 日で 1,173** (漏れているマシンの再起動直後 1,617 と同じ水準)。wired 約 2.7GB。この間 claude (2.1.283) が 2.5 日動き続けていた (pro-con は動かしていない。claude は 1〜3 本)
- 何もしない時間: 60 秒 ±12、120 秒 -16、60 秒 +14
- 手順 2 (`zm500.sh`、隔離サーバ + pty で attach したクライアント) 2,000 回: **+40 / +57**。本番の tmux サーバ (クライアント 2 つ) に `display -p` 2,000 回: +90。対照の `/usr/bin/true` 2,000 回: -84
- `sandbox-exec -p '(version 1)(allow default)' /usr/bin/true` 2,000 回: -126 / 0 (Claude Code の Bash が使う Seatbelt の適用ごとに漏れる、という仮説は棄却)
- 手順 3 (`make test` の段ごと、全 zone の inuse を前後で比較): data.kalloc.1024 の差は idle -16 / test-tmux +266 / test-zshrc -27 / test-bats +110 / test-nvim -116 / test-setup +76 / test-go +98 / test-discovered-rest +506。
  **全段 約 17 分で合計 約 +900** (漏れているマシンの「make test の日 約 10 万個 / 時」より 2 桁小さい)。ほかの zone も目立って増えたものは無い (キャッシュ類の数百〜数千の揺れだけ)。
  注: test-nvim (`tests/nvim/test_nvim.sh`) と test-go (`glogx [build failed]`) と test-discovered-rest は rc=2 で途中までの実行。共有 working tree の他セッションの途中状態の可能性があり、原因は追っていない
- 結論: **macOS 27 のこのマシンでは tmux・make test・exec・sandbox のどれでも漏れない**。漏れているマシン固有の条件 (macOS 15.7.7 か、pro-con で claude を多数並走させる負荷) に依存する

#### 上流の同型の報告 (2026-09-27 に確認。どれも open / 未解決)

- anthropics/claude-code #44824: macOS **15.7.4 / 15.7.5**、`kalloc.1024` が 30〜48MB/分。「**Claude Code を終了すると漏れが即止まる**」「Safe Mode でも起きる (サードパーティ kext ではない)」「端末を変えても起きる」。重複として close
- anthropics/claude-code #66020: macOS **26.5.1**、`data.kalloc.1024 (20G, 21286288 elements)` でパニック (4 回 / 8 日)。panicked task は `claude.exe`、バックトレースに **`com.apple.iokit.EndpointSecurity`** と apfs。漏れは負荷に比例 (idle 約 21 個/秒 → 並列エージェント作業で約 1,027 個/秒)。claude を全部止めると止まる
- anomalyco/opencode #32002: macOS 26.3、`data.kalloc.1024` 21,182,160 個 / 20GB。EndpointSecurity 経由と報告
- 500 の数字との照合: パニック時 21,202,480 個 / 20G (上流とほぼ同じ上限)。増え方 約 2.2 万〜10 万個 / 時 = 約 6〜28 個 / 秒 (上流の idle〜中負荷の帯)。
  pro-con の PG が多い日・テストの係が make test を走らせた日に増えた、は「claude の作業量に比例」とも読める。9/26 の「本番の tmux に display -p で +2,100」は、同じ時間に並走していた claude の分が乗っていた可能性がある (その日の揺れの幅は 10 秒 ±100 で測っており、並走中の claude の量は記録していない)

### 次の一手 (2026-09-27 12:55 改訂。どれも漏れているマシンでしかできない)

1. **決め手の A-B**: pro-con を止め、**claude のプロセスを全部終わらせて** 5〜10 分、1 分ごとに在庫を読む (手順 1 のループ)。次に claude を 1 本だけ起動して同じ時間読む。
   止めている間だけ増えなくなれば claude 起因で確定 (上流 #44824 と同じ確かめ方)。止めても増えるなら claude ではなく、表の tmux 仮説に戻る
2. パニックの報告 (`/Library/Logs/DiagnosticReports/*.panic` か `panic-full-2026-09-25-*.panic`) のバックトレースに `com.apple.iokit.EndpointSecurity` / `AppleMobileFileIntegrity` / `Sandbox` のどれが載っているかを本文に写す (上流と同じ経路かの照合)
3. 確定したら: 当面は在庫を毎日見て 1,500 万個を越える前に再起動。恒久策は **macOS を上げる** (このマシンの macOS 27.0 では再現しない。ただし 27 で直ったのか、負荷が足りないだけかは未確認) か、上流 (#66020 / Apple Feedback) に 500 の数字を足す

### 2026-09-27 14:30: パニックのバックトレースの照合 (次の一手 2) と、今の在庫

- 在庫: **3,310,716** (14:25、`kernel-alloc-watch`。claude 6 / tmux のクライアント 3)。11:50 の 3,199,850 から約 2.6 時間で約 11 万増 (約 4.3 万個 / 時)
- `/Library/Logs/DiagnosticReports/panic-full-2026-09-25-235315.0002.panic`: panicked task は `pid 64723: zsh`。
  「Kernel Extensions in backtrace」は **`com.apple.driver.AppleMobileFileIntegrity` だけ** (依存: CoreAnalyticsFamily / corecrypto / CoreTrust / AppleImage4)。
  `com.apple.iokit.EndpointSecurity` と apfs はロード済み kext の一覧に出ているだけで、バックトレースには無い
- 読み方: バックトレースは「zone が尽きたときに最後に確保しようとした側」(zsh の exec の署名検査) であって、漏らした側ではない。
  上流 #66020 のバックトレースも同じ性質なので、この照合では claude 起因かどうかを判別できない。**決め手は次の一手 1 の A-B のまま**

### 2026-09-27 14:39〜14:45: pro-con を止めた後の増え方 (次の一手 1 の前半。claude は止めていない)

- ユーザーが pro-con を停止した。claude のプロセスは残っている (`kernel-alloc-watch` の数えで 9。この session・daemon・Claude.app を含む)
- 在庫: 停止前 14:25 = 3,310,716 → 14:39 = 3,312,220 (14 分で +1,504)
- 1 分ごと: 14:39:47 3,312,326 / 14:40:48 3,312,483 / 14:41:48 3,312,611 / 14:42:48 3,312,636 / 14:43:48 3,312,681 / 14:44:49 3,312,681 (5 分で +355、最後の 1 分は 0)
- 読み方: 停止前の約 4.3 万個 / 時 (11:50〜14:25 の平均) に対し、停止後は約 4,000〜6,000 個 / 時で、さらに鈍っている。
  **漏れの大半は pro-con が動かしていた作業 (多数の claude の並走) に比例していた**と読める (上流 #66020 の「負荷に比例」と同じ形)。
  ただし claude 全停止の A-B ではないので、claude 起因か、pro-con が起こす tmux / テストの量によるものかはまだ分けられていない
- 在庫は減っていない (3,312,681 のまま。再起動するまで戻らない)

### 2026-09-27 13:10: 在庫を記録する道具 `bin/kernel-alloc-watch` を足した

- `kernel-alloc-watch` (引数なし = record) で `~/.cache/kernel-alloc-watch/log.tsv` に 1 行、7 日より古い行はそのとき落とす。`kernel-alloc-watch list` で一覧 (前の行との差つき)。
  定期実行の仕組み (launchd 等) はまだ無い。漏れているマシンで日をまたいで在庫を追うときに使う
- 13:30: 各行に claude のプロセス数と tmux のクライアント数も残すようにした (`list` の claude / tmux 列)。「claude を全部止めたら差が止まるか」の A-B と、増えた時間帯の突き合わせに使う。
  zprint は引数なしで読む (漏れているマシンで実績があるのは issue の測定と同じ `zprint | awk '$1=="data.kalloc.1024"{print $7}'` の形だけ)。
  **漏れているマシン (macOS 15.7.7) での動作は未確認**。書式が違えば記録せずにエラーで止まるので、初回は `kernel-alloc-watch; kernel-alloc-watch list` の出力を見て確かめる
- 13:40: 判定を足した。`kernel-alloc-watch` (人向け) は在庫・増え方 (今回の起動以降の記録から)・判定・再起動の目安までの時間を出し、
  `kernel-alloc-watch snapshot` は同じ判定を JSON 1 行で出す (Claude が状況を撮る用。verdict = ok / watch / suspect / leaking / critical)。
  閾値は 20,000 (要観察) / 100,000 (漏れている) / 5,000 個/時 (2 万個以上でこれより速いと漏れの疑い) / 15,000,000 (危険)。
  根拠は上の実測 (健全: 起動 9 日で約 1,200・make test 直後で約 2,200、一時的に約 3,000 個/時 / 漏れ: 17 時間で約 160 万) で、`bin/kernel-alloc-watch` 冒頭のコメントに置いた
  増え方は直近 6 時間の最初の記録から (幅が 10 分未満なら、それより前で最も新しい記録から) の平均。
  敵対的レビュー 5 周 (P1 1 件「ログを読めないと既存の記録が全部消えて rc=0」を含め、採用した指摘は全部直して変異で固定した)。
  **未確認のリスク**: make test の 10 分単位の増え方は測っていない。2 万個以上の健全なマシンで 5,000 個/時を越えると、漏れの疑いと誤って出る
  (健全なら 2 万個に届かないので、実際には起きにくい)。誤報が出たら、そのときの `kernel-alloc-watch list` を添えてこの閾値を見直す
- 14:00: `kalloc-watch` を `kernel-alloc-watch` に改名した (kalloc は XNU の kalloc() = kernel alloc。名前から中身が分かる方にした)。
  上の節の `kalloc-watch` の記述も新しい名前に置き換えた。記録先は `~/.cache/kernel-alloc-watch/` になり、旧名の記録
  (`~/.cache/kalloc-watch/`) は引き継がない (ユーザーの判断。要らなければ手で消す)

## 別のマシンで検証する手順 (2026-09-27)

再起動しやすい別のマシンで続ける。🚨 本番の tmux サーバは触らない (測るのは隔離した `-L` のサーバだけ。kill するのも自分が立てたものだけ)。

### 0. 記録の書式 (測るたびに「進捗」へ 1 節ずつ足す)

```
### YYYY-MM-DD HH:MM <マシン名>: <何を測ったか>
- 環境: macOS <sw_vers の ProductVersion / BuildVersion> / <機種 (sysctl -n hw.model)> / tmux <tmux -V> / 起動 <sysctl -n kern.boottime>
- 在庫の前後: <前> → <後> (差 <±N>)。同じ長さの何もしない時間の差: <±M> (揺れの幅として並べる)
- 動いていたもの: アクティビティモニタ (有 / 無・CPU)、pro-con (PG の数)、ほかに重いもの
- 結論: 揺れの範囲 / 漏れた (1 回あたり約 N 個) / 判定できない (理由)
```

- 在庫は `zprint | awk '$1=="data.kalloc.1024"{print $7}'` (7 列目 = inuse)。日をまたいだ推移は `kernel-alloc-watch` (`bin/kernel-alloc-watch`。実行ごとに `~/.cache/kernel-alloc-watch/log.tsv` へ 1 行、`kernel-alloc-watch list` で一覧。README の「カーネルメモリの漏れの記録」) で残す。10 秒で ±100 程度揺れるので、**差は必ず「何もしない同じ長さの時間」の差と並べる**
- 「漏れた」と書くのは、差が揺れの幅の数倍あり、同じ測定を 2 回以上して同じ向きに出たときだけ

### 1. 何もしない時間の増え方 (揺れの幅と、放っておいても漏れるか)

再起動の直後と、普段使いの状態で、1 分おきに 10 分読む:

```sh
for i in $(seq 10); do printf '%s %s\n' "$(date +%T)" "$(zprint | awk '$1=="data.kalloc.1024"{print $7}')"; sleep 60; done
```

### 2. attach したクライアントを持つ隔離サーバへ tmux コマンドを送る (9/26 の表の再現)

下のスクリプトを `./tmp/zm500.sh` に置いて `bash ./tmp/zm500.sh 2000` (引数は回数)。隔離できていなければ何もせずに止まり、最後に自分が立てたサーバだけを止めて一時 dir を消す。
2026-09-27 にこの Mac で試しに 200 回流して動くことを確かめた (+189。揺れの範囲の可能性があるので結論には使わない)。
アクティビティモニタを起動した状態 / 止めた状態の両方で 2 回ずつ回し、`/usr/bin/true` の exec 2,000 回 (`for _ in $(seq 2000); do /usr/bin/true; done`) も対照として同じ回数測る。

```bash
#!/bin/bash
# 500: 隔離した tmux サーバに pty で attach したクライアントを 1 つ置き、tmux コマンドを N 回送って data.kalloc.1024 の差を出す
set -u
unset TMUX TMUX_PANE
N=${1:-2000}
inuse() { zprint 2>/dev/null | awk '$1=="data.kalloc.1024"{print $7}'; }
d=$(mktemp -d); export TMUX_TMPDIR=$d
T() { tmux -L zm500 -f /dev/null "$@"; }
T new-session -d -s zm
# 隔離の実証: このサーバには zm しか居ない (本番のセッションが見えたら止める)
[ "$(T ls -F '#S')" = zm ] || { echo "隔離できていない: $(T ls)"; exit 1; }
python3 -c '
import os,pty,sys,time
pid,fd=pty.fork()
if pid==0: os.execvp("tmux",["tmux","-L","zm500","attach","-t","zm"])
end=time.time()+600
while time.time()<end:
    try: os.read(fd,65536)
    except OSError: break
' & att=$!
for _ in $(seq 100); do [ "$(T list-clients | wc -l | tr -d " ")" -ge 1 ] && break; sleep 0.1; done
echo "clients=$(T list-clients | wc -l | tr -d ' ')"
a=$(inuse); for _ in $(seq "$N"); do T display -p x >/dev/null; done; b=$(inuse)
echo "attach あり display -p ${N} 回: $((b-a))"
T kill-server; kill "$att" 2>/dev/null; wait "$att" 2>/dev/null; rm -rf "$d"
```

効いている条件を絞るなら、`new-session` の後に `T set -g status off` / `T set -g status-interval 0` を足した版と比べる (「次に調べること」の 1 つ目)。

### 3. make test を段ごとに測る (9/27 に増えたのは make test の間)

段ごとに、前後の在庫と、同じ長さの何もしない時間の差を取る。対象は root の Makefile の
`test-tmux` / `test-zshrc` / `test-bats` / `test-nvim` / `test-setup` / `test-go` / `test-discovered-rest`:

```sh
z() { zprint | awk '$1=="data.kalloc.1024"{print $7}'; }
for t in test-tmux test-zshrc test-bats test-nvim test-setup test-go test-discovered-rest; do
  a=$(z); s=$(date +%s); make "$t" > "tmp/zm-$t.log" 2>&1; rc=$?; b=$(z); e=$(( $(date +%s) - s ))
  printf '%s rc=%s %ss 差 %s\n' "$t" "$rc" "$e" "$((b-a))"
done
```

増える段が見つかったら、その段のテストを 1 本ずつ同じ形で測り、1 本まで絞る。

### 4. どの経路が確保しているかを見る (zone logging。重いので最後)

- 起動引数 `zlog=data.kalloc.1024` で、その zone の確保の呼び出しの流れをカーネルが記録する。SIP を切り (復旧モードで `csrutil disable`)、
  `sudo nvram boot-args="zlog=data.kalloc.1024"` で再起動する。終わったら `sudo nvram -d boot-args` と `csrutil enable` で戻す
- 🚨 **未確認**: 記録の読み出し方は確かめていない。この Mac (15.7.7) には `zlog` のコマンドが無く、`zprint` だけがある。
  読み出しには Kernel Debug Kit と lldb のマクロが要るはずで、手順は実機で確かめてから本文に書く

## 関連

- pro-con の復旧の 482 / 483 / 487 (このクラッシュで見つかった)
