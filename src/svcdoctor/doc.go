// Package main は svcdoctor: 壊れた launchd 登録の検出 (issue 148 の 4 章)。
//
// 「使っていないこと」ではなく「壊れていること」で拾う。判定は構造的に確定する事実だけ:
//
//   - A: 起動対象が存在しない (Program / ProgramArguments[0] の絶対パスが不在)。主判定
//   - B: 失敗し続けている (launchctl list の正の exit code + 再起動条件を持つ)。補助
//   - C: Homebrew の台帳に無い (homebrew.mxcl.<formula> の formula が brew list に無い)。補助
//
// 停止・削除の経路はこのプログラムに存在しない (コマンドを表示するだけ)。「実行しない」を
// 運用ルールではなくコードの不在で担保する。
package main
