# 1068 (research): fseventsd が 31 GB まで膨らみ、メモリプレッシャーが続いた (macOS 26.7) の調査記録

起票日: 2026-11-12

## 概要

メモリプレッシャーが高い状態が続いた原因は、macOS の `fseventsd` (ファイル変更の通知を配るデーモン) が 55 日の稼働で
**31 GB** を抱えていたこと。`sudo killall fseventsd` で数 MB に戻り、メモリの空きが戻った。
自作のアプリ・設定の問題ではなく、OS 側の不具合 (同じ fseventsd 1413.160.2 での報告あり) と見ている。
**再発したら、下の「再発したときの手順」から始める** (同じ調査を最初からやり直さない)。

## 環境 (2026-11-12 に測った値)

- macOS 26.7 (25G229)、fseventsd 1413.160.2 (`/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/FSEvents.framework/Versions/A/Resources/Info` の `CFBundleVersion`)
- 物理メモリ 32 GB。fseventsd の起動は 2026-09-17 12:52 (マシンの起動と同じ)

## 症状 (再起動の前、2026-11-12 09:18)

| 指標 | 値 | 出典 |
|---|---|---|
| fseventsd の MEM / 圧縮 | 31G / 21G (2 位の WindowServer は 963M) | `top -l 1 -o mem -stats pid,command,mem,cmprs` |
| PhysMem | 31G used (10G wired, 8.7G compressor), 71M unused | `top` |
| swap | 9.2G 中 8.1G 使用 | `sysctl vm.swapusage` |
| CPU | sys 31.8% (圧縮・展開とスワップの出し入れ) | `top` |
| fseventsd の footprint | 31 GB。**ほぼ全部が `MALLOC_SMALL` の Dirty** (8,885 regions)、Reclaimable 16 KB、peak = 現在値 | `sudo footprint 525` |
| fseventsd の heap | 1 億 3,420 万個・37.2 GB。ほぼ全部 `non-object`。192 B が 9,682 万個、224/320/160/256 B が各 500〜1,200 万個、256 KB が 37,058 個。生きている `xpc_connection_t` は 5 本 | `sudo heap 525` |

小さな割り当ての数 (1.34 億) は、Data ボリュームにあるファイル数 (使用 inode 340 万) の約 40 倍。

## 溜まる操作 (実験の結果)

古い fseventsd (pid 525、31 GB の状態) に対して、操作を 60 秒ずつ流し、間に何もしない 60 秒を挟んで footprint の増え方を測った (2026-11-12 09:56〜10:02)。

| 区間 | 増え方 | 操作の量 | 1 件あたり (概算) |
|---|---|---|---|
| 何もしない (対照) | 0.9〜2.6 MB/分 | — | — |
| 深いディレクトリ木 (8 段 + ファイル 20 個) を作って消す | **78 MB/分** | 6,964 回 | 作成・削除 1 件あたり約 400 B |
| 同じ 200 ファイルを繰り返し書き換える | **271 MB/分** | 5,515 回 × 200 | 書き込み 1 回あたり約 240 B |

- rename・xattr・ソースツリーのコピーの区間は、途中で fseventsd を再起動したので測れていない
- **同じファイルへの書き込みでも 1 回ごとに溜まる**。パスの表が大きくなる形ではなく、イベント 1 件ごとの記録が回収されていない形
- 外れた仮説 (同じ 31 GB の状態で測った):
  - 「消したパス 1 件ごとに記録が残る」: Python で空ファイル 20 万個を作って消した (200 ディレクトリに分散、数秒で完了) → **1 個あたり 11 B**。対照と区別できなかった。上の 400 B と食い違う理由は未確認 (数秒の間のイベントがまとめられた可能性)
  - 「終了したクライアントのバッファが残る」: FSEvents に登録して 0.3 秒で終わるクライアント (node の再帰 `fs.watch`) を 200 本 → 5.98 MB 増。同じ 111 秒の対照の換算 6.12 MB と同じで、**リークしていない**
- **未実施**: 再起動した直後の fseventsd でも同じ操作で溜まるか。溜まらないなら「長く動くうちに掃除が止まる」形、溜まるなら「起動直後から漏れる」形と分かれる

## 原因の見立て (未確認)

[Apple Developer Forums の報告](https://developer.apple.com/forums/thread/849819) (macOS 26.7.1、**同じ fseventsd 1413.160.2**) の推測: 内部の表を掃除する
32 ビットのカーソルが約 838 万回の周回で上限 0xfffffe00 に達して止まり、参照のない記録が回収されずに溜まる。表が拡張されたときだけカーソルがリセットされる。
投稿者自身も「表が上限に達していなくても暴走した例がある」と訂正しており、Apple (DTS) は「バグがある」とだけ認め、仕組みには答えていない。
Feedback: FB25090184 / FB25224901 / FB25346707。

この Mac の実測 (`MALLOC_SMALL` だけが膨らむ、イベント 1 件ごとに溜まる、再起動で戻る) はこの説明と矛盾しない。
ただし、この Mac の fseventsd の内部状態 (カーソルの値) は読んでいない。

ほかの類似報告:
- [talkstream/fseventsd-runaway](https://github.com/talkstream/fseventsd-runaway) (macOS 27.0): 2 日で 7.9 GB、最終的に 24 GB。footprint はほぼ Malloc Small でこの Mac と同じ形。
  AI のコーディングエージェント・vitest・Colima などで毎秒 5,700 件のイベントが出ていた。root 権限なしで判定するスクリプトと、Claude Code の plugin がある
- [colima #1569](https://github.com/abiosoft/colima/issues/1569): VM が `$HOME` を書き込み可でマウントし、監視して 28 GB
  (この Mac の Apple container `obaket-ftp-test-*` は `tmp/ftp-test-server` を**読み取り専用**でマウントしているだけで、該当しない)

## 原因ではなかったもの (再提起する前に読む)

- **自作のアプリ**: ファイル監視を持つのは DualNote の MenuAgent と dotfiles-gui だけで、どちらも `DispatchSource.makeFileSystemObjectSource` (kqueue) を使う。
  FSEvents を通らないので fseventsd のクライアントにならない (`grep -rlE 'FSEventStream|makeFileSystemObjectSource|NSFilePresenter|chokidar|fs\.watch\(' ~/src/my-products/apps ~/src/my-products/lib`)
- **Muse (Meta の AI エージェント、`com.meta.endo` 11.1)**: 調べたときは起動しておらず (最終使用 2026-11-11 13:25)、それでも fseventsd は毎分 1〜3 MB 増えていた。Muse と fseventsd を結ぶ報告も見つからなかった。過去に使っていた間の寄与は未確認
- **登録したクライアントの数**: 10 時間で bundle id を持たないクライアントが 24 プロセス (codex 9、git 4 など。ログに出るのは bundle id の無いものだけ)。登録・終了そのものはリークしない (上の実験)

## 再起動の後 (2026-11-12 10:03)

`sudo killall fseventsd` (launchd がすぐ起こし直す) の後:
- fseventsd (新 pid 65099) の RSS 7.7 MB
- 空きメモリの割合 60%、`vm.memory_pressure` 0、unused 13 GB、圧縮 1.6 GB、CPU sys 2.3%
- swap は 2.3 GB 使用のまま (一度 swap に出たページは、使われるまで戻らない。害は無い)
- wired 10 GB は再起動の前後で変わらない (fseventsd とは別。多めなので、気になったら別に調べる)

## 再発: 別の Mac・macOS 27.0.1 (2026-11-12 14:31)

上の記録とは別の Mac で同じ形を確認した。macOS を上げても直っていない。

- 環境: M3 MacBook Pro (Mac15,10)、macOS 27.0.1 (26A434)、fseventsd 1433 (上の `Info` の `CFBundleVersion`)、物理メモリ 96 GB
- fseventsd (pid 546) の起動は 2026-10-11 01:51 (マシンの起動と同じ)。32 日の稼働
- `top -l 1 -o mem -stats pid,command,mem,cmprs`: fseventsd が MEM 16G / 圧縮 3404M で 1 位
- `sysctl vm.swapusage`: 5120M 中 4309M 使用
- `sudo footprint 546`: footprint 16 GB。**ほぼ全部が `Malloc Small` の Dirty** (4,487 regions)、Reclaimable 1136 KB、`phys_footprint_peak` = 現在値。上の 31 GB の Mac と同じ形
- 32 日で 16 GB = 1 日あたり約 0.5 GB (55 日で 31 GB の上の Mac とほぼ同じ速さ。通しの増え方は測っていない)

## 再起動した直後の fseventsd では溜まらない (2026-11-12 14:49〜14:54、上の macOS 27.0.1 の Mac)

`sudo killall fseventsd` の直後 (新 pid 88105) に、「溜まる操作」と同じ区間 (下の「操作の種類ごとの区間を流す」のスクリプト) を流した。
root なしで測るため、footprint ではなく RSS (`ps -o rss=`) を 5 秒ごとに記録した (起動直後で圧縮がほぼ無いので、増え方は追える)。

| 区間 | 増え方 | 操作の量 | 31 GB の fseventsd での値 |
|---|---|---|---|
| 何もしない (3 区間) | -0.02〜0.05 MB/分 | — | 0.9〜2.6 MB/分 |
| 深いディレクトリ木を作って消す | **0.00 MB/分** | 5,725 回 | 78 MB/分 (6,964 回) |
| 同じ 200 ファイルを繰り返し書き換える | **0.03 MB/分** | 4,233 回 × 200 | 271 MB/分 (5,515 回 × 200) |

- 同じ量の操作でも、起動直後は溜まらない。**「起動直後から漏れる」形ではなく、「長く動くうちに掃除が止まり、そこから溜まる」形**。
  上の Apple Developer Forums の推測 (掃除のカーソルが上限に達して止まる) と矛盾しない
- 起動から最初の 5 分 (1 回目の計測。記録係の誤りで区間ごとの値は取れていない) に 7.0 MB → 11.7 MB。その後は 11.8 MB で止まった
- 限界: 31 GB の側は 26.7 の Mac の footprint、こちらは 27.0.1 の Mac の RSS で、機械・OS・指標が違う。止まり始める時点 (起動から何日・何件で溜まり始めるか) は測っていない
- 打てる手への含み: 定期的な再起動 (手でも見張り役でも) は効く。再起動した後はしばらく溜まらないので、見張り役の閾値は数 GB で足りる

## 再発したときの手順

1. 確認 (root 不要): `top -l 1 -o mem -n 5 -stats pid,command,mem,cmprs` で fseventsd が GB 単位か
2. 証拠を残す (root): `sudo footprint <pid>` と `sudo heap <pid> | head -60`。`MALLOC_SMALL` だけが膨らんでいれば同じ症状
   (heap は対象を一時停止させて走査するので、数十 GB だと数分かかり、その間はファイル変更の通知が止まる)
3. 解放: `sudo killall fseventsd`。Spotlight・Time Machine が変更履歴を失って走査し直すことがある
4. 増え方を測るなら、下のスクリプトを使う (記録係だけ sudo が要る)

<details><summary>記録係: fseventsd の footprint を 5 秒ごとに記録する (別の端末で起動したままにする)</summary>

```bash
#!/bin/bash
# 出力: "epoch秒 バイト数" を 1 行ずつ追記する。Ctrl-C で止める。fseventsd を再起動したら起動し直す (pid を起動時に拾う)
set -eu
out="${1:?出力ファイル}"
pid=$(pgrep -x fseventsd)
sudo -v
while :; do
  v=$(sudo footprint -f bytes --noCategories "$pid" | awk '/phys_footprint:/{print $2}')
  echo "$(date +%s) $v" >> "$out"
  sleep 5  # sleep-ok: 採取の間隔そのもの
done
```
</details>

<details><summary>操作の種類ごとの区間を流す (区間の時刻を phases.log に書く)</summary>

```bash
#!/bin/bash
# 引数: 作業ディレクトリ (空のもの)。各操作を 60 秒、間に何もしない 60 秒を挟む
set -eu
here="${1:?作業ディレクトリ}"; log="$here/phases.log"; : > "$log"; D=60
mark() { echo "$1 $2 $(date +%s) ${3:-}" >> "$log"; }
idle() { mark "$1" start; sleep "$D"; mark "$1" end; }  # sleep-ok: 対照区間そのもの
run() { local name=$1 fn=$2 n=0 end=$(( $(date +%s) + D )); local w; w="$(mktemp -d "$here/ph-$name.XXXXXX")"
  mark "$name" start; while [ "$(date +%s)" -lt "$end" ]; do "$fn" "$w" "$n"; n=$((n+1)); done; mark "$name" end "iter=$n"; rm -rf "$w"; }
deep()    { local p="$1/r$2"; for i in 1 2 3 4 5 6 7 8; do p="$p/d${i}_$2"; done; mkdir -p "$p"; for j in $(seq 20); do : > "$p/f$j"; done; rm -rf "$1/r$2"; }
rewrite() { [ -d "$1/s" ] || { mkdir "$1/s"; for j in $(seq 200); do : > "$1/s/f$j"; done; }; for j in $(seq 200); do echo "$2" > "$1/s/f$j"; done; }
idle idle0; run deep deep; idle idle1; run rewrite rewrite; idle idle2
```

集計: 区間ごとに、区間内の最初と最後のサンプルの差をその間の秒数で割る。

```bash
awk 'FNR==NR{t[++n]=$1;v[n]=$2;next} $2=="start"{s[$1]=$3;o[++m]=$1} $2=="end"{e[$1]=$3}
  END{for(k=1;k<=m;k++){p=o[k];a=0;b=0;for(i=1;i<=n;i++)if(t[i]>=s[p]&&t[i]<=e[p]){if(!a)a=i;b=i}
  if(a&&b>a)printf "%-8s %8.2f MB/min\n",p,(v[b]-v[a])/(t[b]-t[a])*60/1048576}}' sampler.log phases.log
```
</details>

## 打てる手 (未実施。再発の頻度を見て決める)

| 手 | 効果 | 手間 |
|---|---|---|
| 時々 `sudo killall fseventsd` | 数 MB に戻る | 小 |
| 一定量を超えたら自動で再起動する見張り役 (root の launchd ジョブ) | 気にしなくてよくなる | 中。talkstream の判定スクリプトや [Hixie の watchdog](https://github.com/Hixie/contraptions) が参考になる |
| 書き込みの多い場所 (DerivedData・worktree・`tmp/`) を別の APFS ボリュームへ移す | 溜まる量そのものを減らす (Apple の DTS の勧め) | 大 |
| Apple に Feedback を送る (上の FB 番号と同じ症状だと添える) | Apple 次第 | 小 |

## 進捗

- [x] 原因のプロセスの特定 (fseventsd、`MALLOC_SMALL`)
- [x] 溜まる操作の測定 (深い木の作成・削除、同じファイルの書き換え)
- [x] 類似報告の調査
- [x] `sudo killall fseventsd` で解放 (2026-11-12 10:02)
- [x] 別の Mac・macOS 27.0.1 (fseventsd 1433) でも再発を確認 (2026-11-12)
- [x] 再起動した直後の fseventsd でも同じ操作で溜まるかの測定 (溜まらない。2026-11-12、上の節)
- [ ] 再起動の後、何日・何件で溜まり始めるかの測定 (fseventsd の RSS を 1 日 1 回見る程度で足りる)
- [ ] 打てる手のどれを採るかの判断

反証レビューは通していない (マシンの再起動の前に記録を残すことを優先した)。数値はすべてこの日の実測で、出典のコマンドを各行に書いた。
