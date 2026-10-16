# 798 (feat): 常時接続にして、選んだらすぐその画面に切り替わる

起票日: 2026-10-16

> epic 793 の子。要望の 3 つ目。

## 要望 (ユーザーの言)

「アイドリングみたいに常時接続を想定する感じ」

「公式画面共有アプリでは、キーボードショートカットでの画面切り替えができなくて、ショートカットでシュッと、接続先を変えるみたいなこともやりたい」(2026-10-16)

## 調べたこと (2026-10-16)

- **相手の Mac の画面が暗くなる**: 相手の Mac にログイン中のユーザーの名前で入ると、相手の画面は暗くなり、その間は誰もその Mac を使えない
  ([Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac):「the hardware displays connected to that Mac are blanked to give
  you privacy」「no one can use that Mac while you're using High Performance screen sharing」)。物理の画面が消えて仮想ディスプレイに替わる、
  という利用者の報告もある ([Apple Community](https://discussions.apple.com/thread/256048806))
  → **ユーザー決定 (2026-10-16): 暗くなってよい**。常時接続の間、相手の Mac の前では使わない前提で作る
- curtain は「相手の画面を隠す」側の機能で、暗くしない手がかりではない (Edovia:「Curtain Mode is a macOS feature that hides the display of the
  Mac you're connected to」「requires Remote Management … It does not work with Screen Sharing」。
  [Edovia](https://help.edovia.com/screens-5/features/curtain-mode))
- 高性能モードは相手の Mac に仮想ディスプレイ (1 枚か 2 枚、最大 4K) を作る ([Mac ユーザガイド](https://support.apple.com/guide/mac-help/mchl1883115d/mac)・
  [Apple](https://support.apple.com/guide/remote-desktop/apdf8e09f5a9/mac))
- 再接続: rootshell-vnc は「Automatic reconnection with bounded exponential backoff and jitter」を持つ
- 帯域: Apple は 4K の画面 1 枚で 75 Mbps を推奨。画面が変わらない間に何 Mbps 流れるかは未実測

## 作り方の案

- 繋いだセッションは閉じずに持ち、表に出ていないセッションは描画だけ止める (復号を止めるか、受信も止めるかは測ってから決める)
- 切れたら自動で繋ぎ直す (rootshell-vnc の再接続を使う)。手元の Mac のスリープ・網の切り替えから戻ったときも
- **ショートカットで接続先を切り替える**: 繋いでいる Mac に番号を振り、キー一発でその画面へ (例: 決まった修飾キー + 数字・前後へ巡回)。
  - 相手の画面を前面にしている間は、キーを相手へ送る (799) ので、切り替えのキーだけは送らずにアプリが受ける。799 のキーの振り分けと同じ 1 か所で決める
  - アプリが後ろにいるときも効かせるなら、全体のホットキー (`RegisterEventHotKey`) が要る。sandbox でも使える見込み (未確認)
  - 手元の Mac の既存のショートカット (Mission Control・Spotlight など) と被らないキーを既定にし、変えられるようにする

## 確かめること

- [ ] 画面が変わらない間の帯域・手元の CPU・電池 (1 台と、繋いだまま裏に置いた 3 台で)
- [ ] 相手の Mac のスリープ・画面のロックとの関係 (繋いでいる間に相手は眠れるか、起こせるか)
- [ ] 切り替えのキーの既定 (ユーザーに聞く) と、`RegisterEventHotKey` が sandbox の中で効くか
- [ ] 相手の Mac の前に戻って単独で使うとき、接続を切れば物理の画面がすぐ戻るか (このアプリから「この Mac を手放す」を 1 操作でできるか)

## 受け入れ条件

- 繋いだ Mac を一覧かショートカットで切り替えると、選んでから相手の画面が出るまで 0.5 秒以内 (繋ぎ直しをしない)。網が切れて戻ったら、10 秒以内に自動で
  繋ぎ直す (数値は仮置き。1 台目が動いたら実測して決め直す)
