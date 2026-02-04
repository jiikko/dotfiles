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

## Usage

In your agent definition, add:

```markdown
## Language Adaptation

See @_common/language-adaptation.md for guidelines.
```
