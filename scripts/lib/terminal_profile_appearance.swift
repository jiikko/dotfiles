// .terminal プロファイルの見た目 (色 + フォント) をデコードし、1 行 1 プロパティで出力する。
// terminal_profile_restore.sh が AppleScript (settings set への設定) の入力に使う。
//
//   <色キー> R G B                                 … 16bit 0..65535 の genericRGB 成分
//   Font <アーカイブの名前> <解決後の名前|-> <サイズ> <利用可否 0|1>
//
// 🚨 出力は「空白区切り 1 行 1 プロパティ」なので、**値に空白や改行が混ざると呼び出し側の
// パースへ任意の行を注入できる**。フォント名は .terminal (= 外部入力) 由来の任意 String なので、
// 下で PostScript 名の語彙に限って弾く。実証済み (敵対レビュー 2026-09-23): 改行入りの NSName で
// ①別の色を後勝ちで上書き ②2 本目の Font 行で「利用可否 1」を偽造して在庫ゲートを迂回、の両方が
// 通り、スクリプトは rc=0 で正常終了していた。
//
// 🚨 「解決後の名前」を併せて出すのは、**Terminal が family 名を PostScript 名へ解決して保存する**
// ため (Menlo → Menlo-Regular)。呼び出し側は設定後の読み戻しをこちらと比較する (アーカイブの
// 名前と比べると、フォントが入っているのに「未導入」と誤報する)。
//
// 🚨 色の出力は genericRGB (calibrated) 成分: Terminal は AppleScript で渡された生の成分値を
// calibrated RGB として保存するため、sRGB 成分をそのまま渡すと表示色がずれる (実測)。
// ここで genericRGB へ変換してから渡すことで、blob の色空間が何であれ表示色が保存される。
// 単一ソースは .terminal ファイル (ここで色やフォント名をハードコードすると repo と drift する)。
//
// 🚨 Font の「利用可否」は呼び出し側の警告用。**フォントが入っていなくても Terminal は
// エラーを出さず SFMonoTerminal-Regular へ黙って差し替える** (実測 2026-09-23: 使い捨ての
// settings set に "NoSuchFontXYZ-Regular" を設定したら、AppleScript は成功して読み戻しが
// SFMonoTerminal-Regular になった)。つまり「設定できた」は「その字面で表示される」を
// 意味しないので、在庫の有無をここで判定して呼び出し側に渡す。
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
    FileHandle.standardError.write("usage: swift terminal_profile_appearance.swift <file.terminal>\n".data(using: .utf8)!)
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
    // 🚨 成分の有限性を確かめてから Int へ落とす。NaN を `Int(...)` に通すと **SIGTRAP で落ち**、
    // 呼び出し側には swift のスタックダンプしか届かない (issue 084 が潰した「abort させない」
    // 不変条件の再来。敵対レビュー 2026-09-23 が NSRGB を `nan nan nan` にして実証)。
    // inf / 負値 / 巨大値は NSColor 側が 0…1 にクランプするので、ここまで来るのは NaN だけ。
    guard c.redComponent.isFinite, c.greenComponent.isFinite, c.blueComponent.isFinite else {
        FileHandle.standardError.write("✗ \(key) の成分が数値でない (NaN)\n".data(using: .utf8)!)
        exit(1)
    }
    print("\(key) \(Int(c.redComponent * 65535)) \(Int(c.greenComponent * 65535)) \(Int(c.blueComponent * 65535))")
}

// Font は任意 (持たない .terminal もある)。在れば壊れていないことを要求する。
//
// 🚨 NSFont として unarchive してはいけない。**未インストールのフォントは decode の時点で
// 代替に差し替わる** (実測 2026-09-23: 在庫に無い名前へ書き換えた blob を
// `NSKeyedUnarchiver.unarchivedObject(ofClass: NSFont.self)` に通すと `fontName` が
// `.AppleSystemUIFont` になり、在庫ありと見分けが付かない)。その名前をそのまま
// AppleScript へ渡すと、フォント未導入のマシンでプロファイルを UI フォントで上書きする。
// NSFont の代わりに下の器へ差し替えて decode し、**アーカイブに書かれた名前をそのまま**読む。
final class ArchivedFont: NSObject, NSCoding {
    let name: String
    let size: Double
    required init?(coder: NSCoder) {
        // 🚨 decodeDouble は **キーが無くても 0.0 を返す**ので、欠落を範囲検査に流すと
        // 「サイズが範囲外: 0.0」という嘘の診断になる。存在は containsValue で先に分ける
        // (値が数値でない場合は依然 0.0 になるため、下の診断文でその旨を断っている)。
        guard let n = coder.decodeObject(forKey: "NSName") as? String,
              coder.containsValue(forKey: "NSSize") else { return nil }
        name = n
        size = coder.decodeDouble(forKey: "NSSize")
        super.init()
    }
    func encode(with coder: NSCoder) {}  // 読み取り専用 (書き戻しはしない)
}

if let data = dict["Font"] as? Data {
    let unarchiver = try? NSKeyedUnarchiver(forReadingFrom: data)
    unarchiver?.requiresSecureCoding = false
    unarchiver?.setClass(ArchivedFont.self, forClassName: "NSFont")
    guard let font = unarchiver?.decodeObject(forKey: NSKeyedArchiveRootObjectKey) as? ArchivedFont else {
        FileHandle.standardError.write("✗ Font をデコードできない\n".data(using: .utf8)!)
        exit(1)
    }
    // PostScript 名の語彙。空白・改行を含む名前はここで落とす (上記の注入経路)。
    // 🚨 空白を許さないので、NSName に family 名 (「Hack Nerd Font Propo」) が入った .terminal は
    // 診断して落ちる。Terminal 自身の書き出しは PostScript 名なので実害は想定していないが、
    // 落ちたらこの語彙か出力プロトコル (NUL 区切り等) のどちらかを見直すこと。
    func isPostScriptName(_ s: String) -> Bool {
        !s.isEmpty && s.unicodeScalars.allSatisfy {
            ("A"..."Z").contains(String($0)) || ("a"..."z").contains(String($0))
                || ("0"..."9").contains(String($0)) || "._-+".unicodeScalars.contains($0)
        }
    }
    // 🚨 先頭 `.` の system font (`.AppleSystemUIFont` 等) は弾く。**これは修正前のバグが
    // profile へ書き戻していた当の文字列**で、一度その状態で書き出された .terminal を食わせると
    // 「在庫ありの正常なフォント」として素通りしてしまう (敵対レビュー 2026-09-23)。
    guard !font.name.hasPrefix(".") else {
        FileHandle.standardError.write("✗ Font が system font (\(font.name)) — 壊れたプロファイルの可能性\n".data(using: .utf8)!)
        exit(1)
    }
    guard isPostScriptName(font.name) else {
        FileHandle.standardError.write("✗ Font の名前が PostScript 名として不正: \(font.name.debugDescription)\n".data(using: .utf8)!)
        exit(1)
    }
    // 🚨 サイズも検証する。inf / Int64 超過を `Int(...)` に通すと **SIGTRAP で落ち**、呼び出し側には
    // swift のスタックダンプだけが出て診断が届かない (issue 084 が潰した失敗モードの再来)。
    // 0 や巨大値は「設定できてしまう」ぶん質が悪い (端末が実用不能になり GUI でしか戻せない)。
    // 上限 288 は Terminal のフォントパネルが選べる最大に合わせた。
    guard font.size.isFinite, font.size >= 4, font.size <= 288 else {
        FileHandle.standardError.write("✗ Font のサイズが不正 (4〜288 の数値のみ。非数値は 0.0 として読まれる): \(font.size)\n".data(using: .utf8)!)
        exit(1)
    }
    // NSFont(name:size:) は未インストールの PostScript 名に対して nil を返す (差し替えない)。
    // 返ってきた fontName は Terminal が保存するのと同じ解決後の名前。
    let resolvedFont = NSFont(name: font.name, size: font.size)
    let resolved = resolvedFont.map { isPostScriptName($0.fontName) ? $0.fontName : "-" } ?? "-"
    let size = font.size.rounded() == font.size ? String(Int(font.size)) : String(font.size)
    print("Font \(font.name) \(resolved) \(size) \(resolved == "-" ? 0 : 1)")
}
