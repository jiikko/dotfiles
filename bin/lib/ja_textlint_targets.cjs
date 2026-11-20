// ja_textlint_targets.cjs — textlint の引数を textlint と同じ関数で読み、bin/ja-textlint が ja-writing lint をどこに掛けるかを返す。
//
// usage: node ja_textlint_targets.cjs <textlint の node_modules> -- <textlint に渡す引数>...
//   標準出力に NUL 区切りの欄を書く:
//     1. どう終えるか: lint (textlint の後に ja-writing lint を掛ける) / only (textlint だけ。標準エラーに理由を出す) / quiet (textlint だけ。何も足さない)
//     2. only の理由 (それ以外は空)
//     3. textlint が標準入力を読むか: 1 / 0
//     4. 標準入力に掛けるときの名前 (掛けないなら空)
//     5. 標準入力に掛けない理由 (掛けるか、標準入力を読まないなら空)
//     6 以降. ファイルごとに 2 欄: T と名前 (掛ける。名前は引数の字面のまま) / S と理由 (掛けない)
//   rc 0 = 判定できた / 2 = 判定できない (textlint の内部が見つからない・ignore のファイルを読めない)。
//
// 引数の読みを bash で書き直さない: optionator は `-f=json`・`-o=FILE`・`--` の後ろのファイル・短いオプションの束ねを受け、
// 写すと受ける形の分だけ外れる。終え方は lib/src/cli.js の execute と executeWithOptions の分岐、
// 標準入力を読むかは bin/textlint.js (argv に `--stdin` の字面があるか) に合わせる。
// ignore と拡張子の判定も createLinter.js の lintFiles と同じ pathsToGlobPatterns → searchFiles → separateByAvailability を呼ぶ。
// ignore の書式は gitignore でなく glob の ignore で (`sub/` は sub/ の下のファイルに当たらない)、写すと外れる。
// 🚨 lib/src の下の内部のファイルを読む。textlint の package.json に exports の制限が無いから読める。
//   package-lock.json で textlint を上げたら、この 5 つのファイルと関数の名前、cli.js の分岐がまだ同じかを確かめる
//   (ファイルが無くなると rc 2 になり、ja-textlint は ja-writing lint を掛けずに rc 3 で終える)。
"use strict";
const fs = require("node:fs");
const path = require("node:path");

async function main() {
  const argv = process.argv.slice(2);
  const sep = argv.indexOf("--");
  if (sep !== 1) {
    process.stderr.write("ja_textlint_targets: usage: ja_textlint_targets.cjs <node_modules> -- <textlint の引数>...\n");
    return 2;
  }
  const src = path.join(argv[0], "textlint", "lib", "src");
  const { options } = require(path.join(src, "options.js"));
  const { loadCliDescriptor } = require(path.join(src, "loader", "CliLoader.js"));
  const { loadTextlintrc, loadBuiltinPlugins } = require(path.join(src, "loader", "TextlintrcLoader.js"));
  const { pathsToGlobPatterns, searchFiles } = require(path.join(src, "util", "find-util.js"));
  const { separateByAvailability } = require(path.join(src, "util", "separate-by-availability.js"));

  const args = argv.slice(sep + 1);
  const out = [];
  const emit = (...fields) => out.push(...fields);
  const flush = () => process.stdout.write(out.map((f) => f + "\0").join(""));
  const readsStdin = args.includes("--stdin") ? "1" : "0";

  let o;
  try {
    o = options.parse(["node", "textlint", ...args]);
  } catch (e) {
    emit("only", `textlint が引数を読めない (${e && e.message ? e.message : e}) とき`, readsStdin, "", "");
    flush();
    return 0;
  }
  if (o.version || o.init || o.printConfig || o.mcp || o.help) {
    emit("quiet", "", readsStdin, "", "");
    flush();
    return 0;
  }
  const only = o.fix ? "--fix を付けたとき"
    : o.dryRun ? "--dry-run を付けたとき"
    : o.outputFile ? `出力をファイルへ書くとき (${o.outputFile})`
    : o.format !== "stylish" ? `出力の形を指定したとき (${o.format})`
    : "";
  if (only) {
    emit("only", only, readsStdin, "", "");
    flush();
    return 0;
  }

  let extensions;
  try {
    const cliDescriptor = await loadCliDescriptor(o);
    const rcDescriptor = o.textlintrc
      ? await loadTextlintrc({ configFilePath: o.config, node_modulesDir: o.rulesBaseDirectory })
      : await loadBuiltinPlugins();
    extensions = cliDescriptor.concat(rcDescriptor).availableExtensions;
  } catch (e) {
    // 設定や規則の指定の誤りでは textlint も rc 2 で終える。ja-textlint の rc は 2 以上に決まっているので、
    // ja-writing lint の指摘は出す。拡張子は textlint の組み込みの plugin (text と markdown) から取り、ignore の判定は続ける
    process.stderr.write(`ja_textlint_targets: 設定を読めないので組み込みの plugin の拡張子で選ぶ (${e && e.message ? e.message : e})\n`);
    extensions = (await loadBuiltinPlugins()).availableExtensions;
  }

  // 標準入力の文は ignore を見ない (createLinter.js の lintText)。拡張子だけで決まる
  const sname = o.stdinFilename;
  if (readsStdin === "0") {
    emit("lint", "", "0", "", "");
  } else if (!sname) {
    emit("lint", "", "1", "", "--stdin-filename が無い");
  } else if (separateByAvailability([sname], { extensions }).availableFiles.length === 0) {
    emit("lint", "", "1", "", `--stdin-filename の ${sname} が textlint の対象の拡張子でない`);
  } else {
    emit("lint", "", "1", sname, "");
  }

  const cwd = process.cwd();
  for (const name of o._) {
    const st = fs.statSync(name, { throwIfNoEntry: false });
    if (!st || !st.isFile()) {
      // ディレクトリと glob は textlint が展開する。ja-writing lint はファイルを 1 つずつしか受けないので掛けない
      emit("S", `${name} はファイルでない`);
      continue;
    }
    const patterns = pathsToGlobPatterns([name], { extensions, cwd });
    const found = await searchFiles(patterns, { cwd, ignoreFilePath: o.ignorePath });
    // 名前に glob の文字があると、パターンが別のファイルに当たる。textlint が読むのはその別のファイルなので、この名前そのものが見つかったかで決める
    const self = path.resolve(cwd, name);
    const hit = found.ok && separateByAvailability(found.items, { extensions }).availableFiles.includes(self);
    emit(hit ? "T" : "S", hit ? name : `${name} は textlint の対象でない (ignore に載っているか拡張子が違う)`);
  }
  flush();
  return 0;
}

main().then(
  (rc) => { process.exitCode = rc; },
  (e) => {
    process.stderr.write(`ja_textlint_targets: ${e && e.message ? e.message : e}\n`);
    process.exitCode = 2;
  },
);
