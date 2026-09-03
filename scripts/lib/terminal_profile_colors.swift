// .terminal プロファイルの color blob をデコードし「キー R G B」(16bit 0..65535) で 1 行ずつ
// 出力する。terminal_profile_restore.sh が AppleScript (settings set の色設定) の入力に使う。
// 🚨 出力は genericRGB (calibrated) 成分: Terminal は AppleScript で渡された生の成分値を
// calibrated RGB として保存するため、sRGB 成分をそのまま渡すと表示色がずれる (実測)。
// ここで genericRGB へ変換してから渡すことで、blob の色空間が何であれ表示色が保存される。
// 単一ソースは .terminal ファイル (ここで色をハードコードすると repo と drift する)。
//
// blob は NSKeyedArchiver (bplist00) だけを読む = 現行 Terminal.app 自身の序列化形式。
// import 検証もこれを要求し、旧形式のファイルは「ファイルが壊れています」で拒否される (2026-07 実測)。
//
// 🚨 旧 NSArchiver (streamtyped) 形式のフォールバックは意図的に持たない。NSUnarchiver は
// ObjC 例外 (NSArchiverArchiveInconsistency) を投げるため Swift では捕捉できず (`try?` が効かない)、
// 壊れた旧形式 blob に対して下の診断メッセージではなく SIGABRT が出て、呼び出し側
// (terminal_profile_restore.sh) の「非 0 終了を見る」判定に何も伝わらなかった
// (実測 2026-08-21: 切り詰めた streamtyped blob で exit 134 / uncaught exception)。
// 旧形式を読む必要が出たら ObjC 側で例外を捕まえる薄いラッパを噛ませること (Swift だけでは閉じない)。
import AppKit
import Foundation

guard CommandLine.arguments.count > 1,
      let dict = NSDictionary(contentsOfFile: CommandLine.arguments[1]) else {
    FileHandle.standardError.write("usage: swift terminal_profile_colors.swift <file.terminal>\n".data(using: .utf8)!)
    exit(1)
}

func decodeColor(_ data: Data) -> NSColor? {
    try? NSKeyedUnarchiver.unarchivedObject(ofClass: NSColor.self, from: data)
}

for key in ["BackgroundColor", "TextColor", "TextBoldColor", "CursorColor"] {
    guard let data = dict[key] as? Data,
          let color = decodeColor(data),
          let c = color.usingColorSpace(.genericRGB) else {
        FileHandle.standardError.write("✗ \(key) をデコードできない\n".data(using: .utf8)!)
        exit(1)
    }
    print("\(key) \(Int(c.redComponent * 65535)) \(Int(c.greenComponent * 65535)) \(Int(c.blueComponent * 65535))")
}
