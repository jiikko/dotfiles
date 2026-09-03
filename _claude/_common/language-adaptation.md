# Language Adaptation Guidelines

This is a shared template for all agents. Reference this file instead of duplicating language adaptation instructions.

## Detection Rules

1. **Detect user's language from conversation context**
2. **Use Japanese (日本語) if:**
   - User writes in Japanese
   - Code comments are primarily in Japanese
   - CLAUDE.md contains Japanese instructions
   - Project documentation is in Japanese
3. **Use English otherwise**
4. **Keep technical terms in English** (e.g., "Protocol", "async/await", "SwiftUI", "N+1 query")

## Output Language Guidelines

| Content Type | Language Rule |
|--------------|---------------|
| Section headers | Match user's language |
| Technical terms | Always English |
| Code examples | English (with Japanese comments if user prefers) |
| Explanations | Match user's language |
| Error messages | Match user's language |

> 🚨 **agent 定義から `@` 参照しても展開されない** (実測 2026-09-02: `architecture-reviewer` に
> 自分の instructions を引用させたところ、`See @../_common/language-adaptation.md for guidelines.`
> の 1 行がリテラルのまま届いており、この文書の中身は一切効いていなかった)。
> **この文書は人が読む正本であって、agent への配布経路ではない**。agent 定義には中身を
> インラインで書く (repo 内の他 agent はすべてその形)。

## Usage

agent 定義には、この文書の要点を**インラインで**書く (3 行程度に圧縮してよい):

```markdown
## Language Adaptation

- Detect user's language from conversation context
- Use Japanese (日本語) if user writes in Japanese
- Keep technical terms in English (e.g., "Protocol", "async/await")
```
