# 033 bug: 古いバイナリで動いていることを検出できない経路が残っている

`bin/lib/go_autobuild.zsh` の stale 判定は「ソースの mtime がバイナリより新しいか」(`-nt`) だけで、
これが偽になる経路では**再ビルドも警告もされないまま旧版が動き続ける**。glogx 側の警告
(`src/glogx/autobuild.go`) も根拠が `.autobuild.failed` 1 本なので、同じ穴を埋められていない。

発端 (2026-07-31): 別マシンで issues viewer が開かない (`i` が無反応) という症状。issues viewer は
同日に入った機能なので、それより前のバイナリでは `i` に対応するコードが無く**トーストもエラーも
出ない**。「古い版で動いている」ことが観測できないため、原因の切り分けに時間がかかった。

## 現状の検出手段と、それぞれが取りこぼすもの

| # | 経路 | 再ビルドされるか | 警告が出るか |
|---|---|---|---|
| 1 | `--async` の構造 (pull 直後の 1 回目は必ず旧版で exec) | 次回起動から | ○ (`GO_AUTOBUILD_PENDING` → 「ビルド中」トースト) |
| 2 | `.autobuild.failed` の backoff | TTL 600s 超で再挑戦 (6a35eba) | ○ (`autobuildStaleBinary`) |
| 3 | **lock 残留** (builder が kill され `.autobuild.lock` が残る。最大 `GO_AUTOBUILD_LOCK_TIMEOUT`=1800s) | × spawn されない | **×** |
| 4 | **ソースの mtime 巻き戻り** (`rsync -a` / 同期ツール / アーカイブ復元でソースがバイナリより古く見える) | × `-nt` が偽 | **×** |
| 5 | **shim を経ない起動** (`src/glogx/glogx` を直接叩く / 古い symlink・別 PATH の glogx) | × | **×** |
| 6 | `zsh/stat` 不在 (`_go_autobuild_age` が空を返す) | 2 の TTL 救済が効かない | ○ (2 の警告は出る) |

3〜5 は「ソースの方が新しいのに誰もビルドしていない」という 1 つの事実に還元できる。

## 方針: glogx 自身が「自分は今のソースを反映していない」と言えるようにする

`autobuildStaleBinary` の根拠を 2 本にする (どちらも同じ事実を立証する):

1. 失敗記録が自バイナリより新しい (現状の判定)
2. **ソースが自バイナリより新しい** (誰もビルドしていない)

```go
func autobuildStaleBinary(exePath string) bool {
	// ビルド中 (.autobuild.lock がある) は黙る: shim が「他がビルド中」と判断しているのと
	// 同じ状態で、そこで警告すると走っているビルドを「していない」と嘘をつく
	// → 2 回連続起動 (1 本目のビルド中に 2 本目を起動) での誤警告を防ぐ
	// 失敗記録 or ソースが自バイナリより新しい → stale
}
```

- 判定対象は shim の再帰 glob と同じ集合にする: `**/*.go` から `_test.go` を除く + 直下の
  `go.mod` / `go.sum`。**食い違うと「shim は再ビルドしないのに glogx は古いと言う」矛盾が出る**
  ので、片方を変えたら両方直す (`.autobuild.failed` / `GO_AUTOBUILD_PENDING` と同じ取り決め)
- `.autobuild.lock` の名前も shim との取り決めになるので定数化してコメントを添える
- 起動コストは実測で見る: src/glogx は 90 ファイル / 6 ディレクトリなので walk は 0.1ms 程度の
  見込みだが、**glogx の起動時間は Bench の監視対象**なので入れたら Bench を確認する
- 文面は原因ではなく行動を示す: 「glogx が古い版で動いています (`GO_AUTOBUILD_SYNC=1 glogx` で
  再ビルド。理由は src/glogx/.autobuild.log)」

## やらないと決めたこと

- **バイナリに HEAD の SHA を埋めて起動時に `git rev-parse` と比較**: 起動パスに fork が 1 本
  増える。macism fork (40-60ms) を起動から外した経緯 (4d8c0ae) と逆行するので採らない。
  mtime だけで判定できる範囲を出ない
- **shim 側の判定を「差分」ベースに変える** (ソース集合のフィンガープリントを stamp に保存し、
  順序ではなく一致で見る): 4 を構造的に潰せるが、`git pull` 運用では mtime は前進するので
  今回の症状には効かない。踏んだ実例が出てから (trigger 待ち)

## 検証

- 3: `.autobuild.lock` を作った状態で起動 → 警告が出ない (ビルド中扱い) ことを確認
- 4: `touch -t` でソースの mtime をバイナリより古くして起動 → 警告が出る
- 5: `src/glogx/glogx` を直接起動 → 警告が出る
- 通常起動 (ソースが古い / 失敗記録なし) では無言のまま = ノイズを足していない
- `make -C src/glogx test` と Bench (起動時間) を確認

## 着手条件

`src/glogx/autobuild.go` と `tui.go` は 2026-07-31 時点で別の作業者が改修中 (`autobuildWatch.handle`
のシグネチャ変更を含む)。**その改修が入ってから着手する** (同じ関数群に同時に触ると衝突する)。
