// 表示されない太字を知らせる。CommonMark では閉じの ** の前が約物 (」・) など) で後ろが文字だと閉じにならず、
// 「**「x」**に」は ** が文字のまま画面に出る。解析器 (remark-parse 9 = micromark) が強調にしなかった ** は
// Str の節に文字として残るので、それを数える。** の位置を正規表現で推して flanking の規則を書き直さない
// (規則の写しが解析器とずれると、表示される太字を誤って指摘する)。
// 直し方は、外側に空白を入れる (「**x**」→ 「 **x** 」) か、約物を太字の内側から出す (「**x**」→「x」を太字にする)。
// 着想は yomiyasu (https://github.com/nanaism/yomiyasu, MIT。LICENSE-yomiyasu) の bold_not_rendered。
"use strict";

module.exports = function (context) {
  const { Syntax, RuleError, report, getSource, locator } = context;
  return {
    [Syntax.Str](node) {
      const text = getSource(node);
      for (const m of text.matchAll(/\*\*/gu)) {
        report(
          node,
          new RuleError(
            "この ** は太字にならず、文字のまま表示される。閉じの ** の前が約物で後ろが文字だと閉じない。** の外側に空白を入れるか、約物を太字の外へ出す",
            { padding: locator.range([m.index, m.index + 2]) },
          ),
        );
      }
    },
  };
};
