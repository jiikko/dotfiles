# bug: lockman の `Renew` が lease 期限を検査せず、`Release` との間で不変条件が非対称

起票日: 2026-09-07
カテゴリ: bug
優先度: 中（🚨 severity の根拠は**レースの確率ではない**。「修正が既存関数の再利用で済む」+
「`Release` との doc レベルの非対称」で担保している。実際に踏むには 30 分超の凍結が
`readLock` → `OpenFile` の窓に落ちる必要があり、確率自体は低い）
出典: /audit broken-code 2026-09-06

対象: `src/lockman/lock.go:Renew` / `readLock` / `metaDirMode`

## ① `Renew` に期限検査が無い

`Release` は所有権と**期限の両方**を見てから消す:

```go
if m == nil || m.Token != token { return errNotOwner }
now, err := l.serverNow()
if expired(now, mtime, holderTTL(m)) {
    return fmt.Errorf("%w: lease が切れている (走行中に引き継がれた可能性)", errNotOwner)
}
return os.Remove(l.lockPath())
```

`Renew` は **token 照合だけ**で `os.OpenFile(..., O_WRONLY|O_TRUNC, ...)` へ進み、
`serverNow()` を呼ぶのは**書き込んだ後の「検算」**として。

```go
if m == nil || m.Token != token { return errNotOwner }
b, _ := json.Marshal(m)
f, err := os.OpenFile(l.lockPath(), os.O_WRONLY|os.O_TRUNC, lockFileMode)   // ← 期限を見ずに truncate
...
now, err := l.serverNow()   // 検算はここ
```

**発火条件**: 自分が 30 分超（`holderTTL`）凍結し、その間に別のホストが lease を引き継ぎ、
凍結明けの `Renew` が `readLock` と `OpenFile` の間に落ちる。
このとき **引き継いだ側の lock を truncate して自分のメタで上書きする**。

### 推奨対応

token 照合の直後へ、`Release` と**同じ関数**を使って期限検査を足す（新判定を増やさない）:

```go
now, err := l.serverNow()
if err != nil { return err }
if expired(now, mtime, holderTTL(m)) {
    return fmt.Errorf("%w: lease が切れている", errNotOwner)
}
```

`readLock` は既に mtime を返しているので、追加の I/O も要らない。

## ② 脅威モデルが未記載のまま 0o777 + no-sticky を採用している

```go
// 共有前提のモード。sticky を付けないこと: rename の可否は親ディレクトリの ...
metaDirMode  = fs.FileMode(0o777)
lockFileMode = fs.FileMode(0o666)
```

sticky を**意図的に付けない**設計なので、**同一ホストの任意のローカルユーザーが lock を差し替えられる**。
その上で:

- `O_NOFOLLOW` / `Lstat` / `Readlink` は repo 内 **0 件**
- `readLock` は `os.Stat` + `os.ReadFile`（どちらも symlink を辿る）
- `Renew` は `O_WRONLY|O_TRUNC` で開き直す（**symlink を辿って任意のファイルを truncate しうる**）
- 一方 `tryPlace` 側は `os.Link` + `O_CREATE|O_EXCL` で比較的堅い

🚨 **悪用は再現していない。「脆弱性がある」とは主張しない。** 主張は
**「想定する敵が書かれていない」**こと。

### 推奨対応

mode 定数の隣に**想定する敵**を明記する（協調するホストのみか、非信頼の同一ホストユーザーを含むか）。
後者が射程なら ① の期限検査だけでは足りず、`O_NOFOLLOW` + fd ベースの `Fstat` が要る。

## 🚨 ①と②は同じ commit で閉じること

① だけ直すと「直したが安全になっていない」状態（期限は見るが symlink は辿る）になる。

## 受け入れ条件

- [x] `Renew` が期限切れで `errNotOwner` を返す
- [x] 回帰テスト: 既存 `TestRenewDetectsLostLease` の**対照**として
      「TTL 超過後、誰も引き継いでいない状態の `Renew` が `errNotOwner` を返す」を足す
- [x] **変異検証**: 足した期限検査を外すと red
- [x] mode 定数の隣に脅威モデルが 1 行ある
- [x] `issues/done/091` の「renew は token 照合」の記述にも期限切れの扱いを 1 行足す
      （次の実装者へ古い契約を渡さないため）

## 進捗（2026-09-09）

対応 commit: `fix(312): Renew に lease 期限検査を足し、mode 定数の脅威モデルを書く`

### ① 期限検査

`Renew` の token 照合の直後へ `serverNow` + `expired(now, mtime, holderTTL(m))` を足した。
判定関数は `Release` と共有（新しい判定は書き起こしていない）。`readLock` の戻り値の
mtime を捨てていた（`m, _, err :=`）のを受け取る形に変えた。

書き込み後の検算 `now, err := l.serverNow()` は `now, err = ...`（再代入）になった。
**追加の I/O は「要らない」ではなく「probe が 1 往復増える」**（issue 本文 52 行目の
「追加の I/O も要らない」は mtime についてのみ正しい。`serverNow` は probe ファイルの
create/stat/remove なので、Renew あたり 1 → 2 往復になる）。既定の renew 間隔は
TTL/3 = 10 分なので許容した。

### ② 脅威モデル

`metaDirMode` / `lockFileMode` の隣に「想定する敵はいない = 協調するホスト・ユーザー
のみ。非信頼の同一ホストユーザーは射程外」と、射程に入れる場合の要件
（`O_NOFOLLOW` + fd ベースの `Fstat`）を書いた。**実装は足していない**
（issue の②は脆弱性を主張しておらず、射程を決めるのは人間のため）。
no-sticky は「他ユーザーが自分の lock を rename できること」を設計として要求して
いるので、権限で敵を締め出す方向とはそもそも両立しない、という点も書いた。

### 変異検証（受け入れ条件の 3 つ目）

- baseline: `go test ./...` rc=0
- 変異: `Renew` の期限検査ブロックを削除し、検算側の `now, err =` を `now, err :=` へ戻す
  （= 修正前の `Renew` の形。期限検査だけを消すと `mtime` / `now` が未使用でビルド不能に
  なるため、**ブロック単位で修正前へ戻す**のが唯一の妥当な変異）
- `go build ./...` rc=0（変異がビルドできたことをテストと別に確認）
- 変異後: `go test ./...` rc=1 — `TestRenewRefusesExpiredLease` のみが red
  （`TestRenewDetectsLostLease` は green のまま = 予測どおり。あちらは token が
  別人に変わることしか見ておらず、期限検査を何も守っていない）
- revert 後: rc=0

### 残タスク

- なし（受け入れ条件はすべて満たした）。②の `O_NOFOLLOW` 化は**射程外**として
  コメントに再評価の trigger だけ残した
