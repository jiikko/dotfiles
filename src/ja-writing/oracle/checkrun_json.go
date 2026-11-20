package oracle

// run の記録で、子の代わり (fakeChild の difffile・lintfile) が出す JSON。本物の diff と lint の出力 (「# 設定を直しました。」→「# 設定の修正」) を、
// 規則の候補を 0 件にし、勢いの見直し候補 (level review) と慎重表現・文体の切り替えの件数だけを残す形に書き換えたもの (lint は候補の数だけを残して
// findings を空にしたもの)。本物の入力では作りにくい「規則の候補は無いが見直し候補がある」1 行目と、並べる候補の無い冒頭の一覧を通す。

// craftedDiff は「規則の候補は無いが、勢いの見直し候補と慎重表現・文体の切り替えの候補がある」diff の JSON。
const craftedDiff = `{
 "format": "yomiyasu-diff",
 "format_version": 2,
 "tool_version": "3.1.0",
 "status": "has_candidates",
 "changed": true,
 "candidate_count": 0,
 "meaning_risk_count": 0,
 "fail_on_meaning_risk_count": 0,
 "not_judged": [
  "meaning_equivalence",
  "reader_comprehension"
 ],
 "context": {
  "doc_type": null,
  "reader": null
 },
 "meaning_risks": [],
 "markers": [],
 "new_words": [],
 "lost_words": [],
 "structure": [],
 "spans": [
  {
   "added": "\u306e\u4fee\u6b63",
   "was": "\u3092\u76f4\u3057\u307e\u3057\u305f\u3002",
   "kinds": [],
   "new_words": [
    "\u4fee\u6b63"
   ],
   "where": "\u8a2d\u5b9a\uff3b\u306e\u4fee\u6b63\uff3d\n\n\u672c\u6587\u3067\u3059\u3002"
  }
 ],
 "logic": [],
 "bold": [],
 "endings": {
  "stance": null,
  "changes": [],
  "flags": [],
  "orig_flags": []
 },
 "type_changes": [],
 "full_diff": {
  "encoding_notes": [],
  "final_newline": {
   "old": true,
   "new": true
  },
  "limits_applied": [],
  "hunks": [
   {
    "index": 1,
    "tag": "replace",
    "old_start": 1,
    "old_end": 1,
    "new_start": 1,
    "new_end": 1,
    "old_blocks": [
     "heading"
    ],
    "new_blocks": [
     "heading"
    ],
    "whitespace_only": false,
    "notes": [],
    "moved_to": null,
    "moved_from": null,
    "old_lines": [
     "# \u8a2d\u5b9a\u3092\u76f4\u3057\u307e\u3057\u305f\u3002"
    ],
    "new_lines": [
     "# \u8a2d\u5b9a\u306e\u4fee\u6b63"
    ],
    "segments": [
     [
      "=",
      "# \u8a2d\u5b9a"
     ],
     [
      "-",
      "\u3092\u76f4\u3057\u307e\u3057\u305f\u3002"
     ],
     [
      "+",
      "\u306e\u4fee\u6b63"
     ]
    ],
    "segments_omitted": null
   }
  ]
 },
 "stiffness": {
  "level": "review",
  "candidate_count": 0,
  "candidates": [],
  "metrics": {
   "old": {
    "sentences": 1,
    "chars": 5,
    "avg_sentence_length": 5.0,
    "kanji_ratio": 50.0,
    "kanji": 2,
    "nominalizations": 0,
    "nominalization_names": {},
    "no_chains": 0
   },
   "new": {
    "sentences": 1,
    "chars": 5,
    "avg_sentence_length": 5.0,
    "kanji_ratio": 50.0,
    "kanji": 2,
    "nominalizations": 0,
    "nominalization_names": {},
    "no_chains": 0
   }
  },
  "list": "references/lists/nominalization.txt",
  "list_problem": null
 },
 "tone": {
  "level": "review",
  "candidate_count": 1,
  "candidates": [
   {
    "kind": "tone",
    "subkind": "heading",
    "old_line": 1,
    "new_line": 1,
    "old": "# \u8a2d\u5b9a\u3092\u76f4\u3057\u307e\u3057\u305f\u3002",
    "new": "# \u8a2d\u5b9a\u306e\u4fee\u6b63",
    "detail": {
     "old": 9,
     "new": 5,
     "places": [
      {
       "old_line": 1,
       "new_line": 1,
       "old": "# \u8a2d\u5b9a\u3092\u76f4\u3057\u307e\u3057\u305f\u3002",
       "new": "# \u8a2d\u5b9a\u306e\u4fee\u6b63"
      }
     ]
    },
    "level": "review"
   }
  ],
  "counts": {
   "old": {
    "exclamation": 0,
    "question": 0,
    "emoji": 0,
    "intensifier": 0,
    "heading_chars": 9
   },
   "new": {
    "exclamation": 0,
    "question": 0,
    "emoji": 0,
    "intensifier": 0,
    "heading_chars": 5
   }
  },
  "list": "references/lists/intensifiers.txt",
  "list_problem": null
 },
 "feeling": {
  "list": "references/lists/feelings.txt",
  "list_problem": null
 },
 "hedges": {
  "level": "review",
  "candidate_count": 1,
  "candidates": [],
  "list": "references/lists/hedges.txt",
  "list_problem": null
 },
 "immutable": {
  "level": "review",
  "candidate_count": 0,
  "candidates": [],
  "meaning_risk_count": 0
 },
 "politeness": {
  "level": "review",
  "candidate_count": 0,
  "candidates": [],
  "applicable": false,
  "threshold": 0.2,
  "metrics": null
 },
 "register": {
  "level": "review",
  "candidate_count": 1,
  "candidates": [],
  "threshold": {
   "min_sentences": 3,
   "min_ratio": 0.5
  },
  "skipped_by": null
 },
 "added_actors": [],
 "heading_shapes": [],
 "comma_increase": null
}
`

// craftedLint は「候補 2 件と言うが、findings が空」の lint の JSON。
const craftedLint = `{
  "format": "yomiyasu-lint",
  "format_version": 2,
  "tool_version": "3.1.0",
  "candidate_count": 2,
  "counts": {
    "error": 0,
    "warn": 0,
    "info": 0
  },
  "no_rule_findings": true,
  "rule_penalty_score": 100,
  "not_judged": [
    "meaning_preservation",
    "reader_comprehension"
  ],
  "context": {
    "doc_type": null,
    "reader": null,
    "skipped_by_doc_type": {}
  },
  "metrics": {
    "char_count": 11,
    "total_lines": 2,
    "list_lines": 0,
    "list_ratio": 0.0,
    "bold_count": 0,
    "bold_per_1000": 0.0
  },
  "findings": []
}
`
