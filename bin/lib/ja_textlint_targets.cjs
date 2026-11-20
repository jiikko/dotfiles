// ja_textlint_targets.cjs — textlint の引数のうち、textlint が実際に読むファイルの名前を返す (bin/ja-textlint が ja-writing lint を掛ける先を決める)。
//
// usage: node ja_textlint_targets.cjs <textlint の node_modules> -- <textlint に渡す引数>...
//   引数のうち通常のファイルを 1 つずつ判定し、textlint の対象なら引数の字面のまま NUL 区切りで標準出力へ書く。
//   ディレクトリと glob は判定しない (bin/ja-textlint は ja-writing lint をファイル 1 つずつにしか掛けない)。
//   rc 0 = 判定できた (対象が 0 件でも 0) / 2 = 判定できない (textlint の内部が見つからない・引数や設定を読めない)。
//
// ignore (.textlintignore・--ignore-path) と拡張子の判定を自分で書かず、textlint の CLI と同じ関数を呼ぶ:
//   引数の読み (options)・設定の読み込み (cli.js の loadDescriptor と同じ組み合わせ)・
//   ファイルの探索と ignore (createLinter.js の lintFiles と同じ pathsToGlobPatterns → searchFiles → separateByAvailability)。
//   ignore の書式は gitignore でなく glob の ignore で (`sub/` は sub/ の下のファイルに当たらない)、写すと外れる。
// 🚨 lib/src の下の内部のファイルを読む。textlint の package.json に exports の制限が無いから読める。
//   package-lock.json で textlint を上げたら、この 4 つのファイルと関数の名前がまだ在るかを確かめる
//   (無くなると rc 2 になり、ja-textlint は ja-writing lint を掛けずに rc 3 で終える)。
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

  const cliOptions = options.parse(["node", "textlint", ...argv.slice(sep + 1)]);
  let extensions;
  try {
    const cliDescriptor = await loadCliDescriptor(cliOptions);
    const rcDescriptor = cliOptions.textlintrc
      ? await loadTextlintrc({ configFilePath: cliOptions.config, node_modulesDir: cliOptions.rulesBaseDirectory })
      : await loadBuiltinPlugins();
    extensions = cliDescriptor.concat(rcDescriptor).availableExtensions;
  } catch (e) {
    // 設定や規則の指定の誤りでは textlint も rc 2 で終える。ja-textlint の rc は 2 以上に決まっているので、
    // ja-writing lint の指摘は出す。拡張子は textlint の組み込みの plugin (text と markdown) から取り、ignore の判定は続ける
    process.stderr.write(`ja_textlint_targets: 設定を読めないので組み込みの plugin の拡張子で選ぶ (${e && e.message ? e.message : e})\n`);
    extensions = (await loadBuiltinPlugins()).availableExtensions;
  }

  const cwd = process.cwd();
  const out = [];
  for (const name of cliOptions._) {
    const st = fs.statSync(name, { throwIfNoEntry: false });
    if (!st || !st.isFile()) continue;
    const patterns = pathsToGlobPatterns([name], { extensions, cwd });
    const found = await searchFiles(patterns, { cwd, ignoreFilePath: cliOptions.ignorePath });
    if (!found.ok) continue;
    if (separateByAvailability(found.items, { extensions }).availableFiles.length > 0) out.push(name);
  }
  process.stdout.write(out.map((n) => n + "\0").join(""));
  return 0;
}

main().then(
  (rc) => { process.exitCode = rc; },
  (e) => {
    process.stderr.write(`ja_textlint_targets: ${e && e.message ? e.message : e}\n`);
    process.exitCode = 2;
  },
);
