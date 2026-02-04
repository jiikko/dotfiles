# Tool Selection Strategy

This is a shared template for all agents. Reference this file for common tool usage patterns.

## Core Tools

| Tool | When to Use |
|------|-------------|
| **Read** | When you know the exact file path (from user mention, conventions, or previous discovery) |
| **Grep** | When searching for specific patterns, implementations, or usages across files |
| **Glob** | When searching by file naming convention or extension pattern |
| **Task(Explore)** | When you need to understand broad architecture or explore unfamiliar codebase areas |
| **LSP** | When finding definitions, references, or call hierarchies for specific symbols |

## Tool Selection Flow

```
Need to find something?
├── Know exact file path? → Read
├── Know pattern/keyword? → Grep
├── Know file naming convention? → Glob
├── Need broad exploration? → Task(Explore)
└── Need symbol navigation? → LSP
```

## Best Practices

1. **Avoid redundant searches** - Check if information was already gathered
2. **Prefer specific over broad** - Use Read over Grep when path is known
3. **Combine tools efficiently** - Glob to find files, then Read to examine
4. **Use Task(Explore) sparingly** - Only when architecture understanding is truly needed

## Agent-Specific Extensions

Each agent may add domain-specific tool usage. Document these in your agent definition:

```markdown
## Tool Selection Strategy

See @_common/tool-selection-strategy.md for base guidelines.

### Agent-Specific Tools
- **[Tool]**: [When to use for this agent's domain]
```
