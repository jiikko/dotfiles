# 798 (feat): 常時接続にして、選んだらすぐその画面に切り替わる

起票日: 2026-10-16

> epic 793 の子。要望の 3 つ目。

## 要望 (ユーザーの言)

「アイドリングみたいに常時接続を想定する感じ」

## 調べたこと (2026-10-16)

- 🚨 **相手の Mac の画面が暗くなる**: 相手の Mac にログイン中のユーザーの名前で入ると、相手の画面は暗くなり、その間は誰もその Mac を使えない
  ([Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac):「the hardware displays connected to that Mac are blanked to give
  you privacy」「no one can use that Mac while you're using High Performance screen sharing」)。常時接続とぶつかる一番大きな制約
- ただし、クライアント側で暗くするかを選べる手がかりがある: rootshell-vnc の README の「Remote curtain control」、iShareScreen の起動例の
  `--no-curtain`。どちらも説明は無く、何が起きるか未確認
- 高性能モードは相手の Mac に仮想ディスプレイ (1 枚か 2 枚、最大 4K) を作り、主ディスプレイ・拡張・ミラーのどれとして使うかを選べる (Apple の同じページ)
- 再接続: rootshell-vnc は「Automatic reconnection with bounded exponential backoff and jitter」を持つ
- 帯域: Apple は 4K の画面 1 枚で 75 Mbps を推奨。画面が変わらない間に何 Mbps 流れるかは未実測

## 作り方の案

- 繋いだセッションは閉じずに持ち、表に出ていないセッションは描画だけ止める (復号を止めるか、受信も止めるかは測ってから決める)
- 切れたら自動で繋ぎ直す (rootshell-vnc の再接続を使う)。手元の Mac のスリープ・網の切り替えから戻ったときも
- 常時接続の間、相手の Mac の前に座って使えるようにするには、暗くしない形が要る (下の確かめること)

## 確かめること

- [ ] `--no-curtain` / curtain control で、相手の画面を暗くせずに高性能モードで繋げるか。繋げるなら、相手の画面に何が映るか (拡張の
  仮想ディスプレイを使うとき、相手の物理の画面は普通に使えるか)
- [ ] ログイン中のユーザーと別のユーザーで入ると暗くならない (Apple の文言の条件の裏) が、常時接続の使い方として成り立つか
- [ ] 画面が変わらない間の帯域・手元の CPU・電池 (1 台と、繋いだまま裏に置いた 3 台で)
- [ ] 相手の Mac のスリープ・画面のロックとの関係 (繋いでいる間に相手は眠れるか、起こせるか)
- [ ] 相手の Mac の前に座って使う時間があるか (ユーザーに聞く。暗転を許せるかが決まる)

## 受け入れ条件

- 繋いだ Mac を一覧で切り替えると、繋ぎ直しの待ちなしに画面が出る。切れたら自動で戻る
