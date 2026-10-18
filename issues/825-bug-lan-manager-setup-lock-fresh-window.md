# 825 (bug): lan-manager の初期設定: 取ったばかりの lock を引き取って 2 本とも進む ほか

> 🚨 **担当中: air5 (glogx)**（2026-10-18〜）

起票日: 2026-10-18

## 概要

`src/mac-lan-manager/` の `setup/`・`live/`・`main.go`・`dump.go` のバグ探し (2026-10-18、ユーザー依頼「lan-manager でバグがないか調べて」。opus の読み取り専用レビュー) で
見つかった誤動作 4 件。P1・P2 は無い。issue 747 (done) の受容・記録済みの項目は除いてある。

## 詳細

1. **P3: 背景の lock を取った直後の短い間に 2 本目が来ると、2 本とも固定 IP への変更へ進む** (`setup/staticip.go` の `alive` と lock の取り方)
   - 1 本目は `mkdir lock` → `echo $$ > lock/pid` → `ps -o lstart= > lock/start` の順に書く。書き終わるまで `alive()` は偽 (pid が空か、start が ps の時刻と合わない)
   - この間に 2 本目が来ると「死んだ lock」と判断し、takeover を mkdir し、もう一度 `alive` を見ても偽のままなので、1 本目の lock を `rm -rf` して作り直して進む。
     747 で直した takeover は「止まった lock」どうしの競りだけを守り、取ったばかりの lock は守らない
   - 結果: 2 本とも `-setmanual` を打ち、2 本とも戻す。後に終わった方の `finish` がもう片方の lock を消す。間は ps 1 回分 (数十 ms) なので、2 人がほぼ同時に操作したときだけ
   - 747 の受容「前面の確かめと背景の lock の取得の間に別の操作者が先に取ると、後の方は背景で黙って抜ける」とは別の窓 (あちらは後の方が抜ける。こちらは両方進む)
   - 確信度: 再現 (レビュアーが `go test -overlay` でテストを差し込み、sandbox に「最初の 1 回だけ 1 秒遅い ps」を置き、2 本目を 0.3 秒遅らせた。
     結果は `同時の 2 本で変更が 2 回: [-setmanual]…12 / [-setmanual]…11 / [-setdhcp] ×2`)。main がコードで確認 (pid / start を mkdir の後に書く)
   - 直し方の案: pid と start を書き終えた一時ディレクトリを rename で lock にする。または、中身の欠けた lock は一定時間「生きている」と見る
2. **P3: 止まった takeover ディレクトリが残ると、以後の引き取りがずっと断られ、外し方が案内されない** (`setup/staticip.go`)
   - `staticip.lock.takeover` を mkdir してから rmdir するまでの間は trap がまだ無い。ここで kill・TERM・電源断が起きると takeover が残り、
     その後に lock が止まると毎回「別の切り替えが止まった lock を引き取っている」で抜ける
   - `refuse` が案内するのは `sudo rm -rf …/staticip.lock` だけで、`.takeover` に触れない
   - 確信度: コードで確認 (起きる窓は短い)
3. **P3: `dump --peer` を Ctrl-C / SIGTERM で止めても終了コードが 0** (`dump.go` の `dumpPeer`)
   - 止めると cancel → ObservePeer が `ErrUnknown` → `key: unknown` を出して `return 0`。走査の dump は同じ止め方で 130 を返すので食い違う。スクリプトから区別できない
   - 確信度: コードで確認 (`dumpPeer` は無条件に 0 を返す)
   - テストの形: `stop` を閉じたチャネルにし、observe の偽物が `ctx.Done()` を待ってから `KeyErr` を返す。rc ≠ 0 を期待する
4. **P3: `~/.ssh/id_ed25519` はあるが `.pub` が無いと、鍵を置く手順が毎回失敗する** (`setup/remote.go` の `KeyArgv`)
   - `test -f id_ed25519 || ssh-keygen …` は秘密鍵だけを見る。`.pub` が消えていると ssh-keygen を飛ばし、`ssh-copy-id -i ….pub` が失敗する
   - 確信度: コードで確認 (条件が秘密鍵だけ)。ssh-copy-id が `.pub` を開けないときに失敗することは未実測
   - 直し方の案: `.pub` も条件に入れ、無ければ `ssh-keygen -y -f` で作り直す

### 参考 (未確認・低確度)

- `setup/wiredname.go` の `launchctl bootout` の直後の `bootstrap` が「Bootstrap failed: 5」で落ちる競りが macOS では知られている。air5 で入れ直しが 2 回通っているので
  バグとしては挙げない。入れ直しが rc = 5 で失敗したら、まずここを疑う

### 見て問題が無かったもの (レビュアーの記録)

Quote の二重の入れ子 (Script → RemoteCommand → ssh) / Validate が IP・token・状態ディレクトリを sh に入れる前に通すこと / 確認の印の `$SSH_CONNECTION` の照合 /
終わり方ごとの戻し (EXIT / TERM の trap、changed を変更の前に立てる) / 印が受け取られるまで ConfirmCommand が待つこと / waitStarted の上限 /
knownHost の終了コードの読み分け / controlPath の長さ / dump の宛先が s / Y / y / o と同じ `m.target` を通ること / setup の手順が全部 ExitIsFailure を立てること。
`go vet` は 0 件

## 関連ファイル

- `src/mac-lan-manager/setup/staticip.go` / `setup/remote.go` / `dump.go`
- issue 747 (done。takeover を入れた経緯は「段 4」の fable の敵対レビュー 3 周目)

## 進捗

- [ ] 1〜4 を直す (1 は 747 の「同時の 2 本で変更が 1 回だけ」のテストに、取ったばかりの lock の窓を作る形を足す)

- 2026-10-18 反証レビュー (sonnet・読み取り専用): P1 なし・「既に直っている / 起きない」なし。記述の不正確さ (P3) を訂正した
