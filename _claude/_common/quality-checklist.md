# Quality Checklist

This is a shared quality checklist for all review agents.

## Universal Quality Gates

### Code Quality
- [ ] No obvious bugs or logic errors
- [ ] Error handling is appropriate
- [ ] Edge cases are considered
- [ ] No hardcoded values that should be configurable

### Security
- [ ] No sensitive data exposed (API keys, credentials)
- [ ] Input validation where needed
- [ ] No SQL/command injection vulnerabilities
- [ ] Proper authentication/authorization checks

### Maintainability
- [ ] Code is readable and self-documenting
- [ ] Naming is clear and consistent
- [ ] No unnecessary complexity
- [ ] DRY principle followed (no excessive duplication)

### Performance
- [ ] No obvious performance issues
- [ ] Appropriate data structures used
- [ ] No unnecessary loops or iterations
- [ ] Resource cleanup handled properly

### Testing
- [ ] Tests exist for critical paths
- [ ] Tests are meaningful (not just coverage)
- [ ] No flaky tests introduced

## Review Completion Criteria

A review is complete when:
1. All high-severity issues are identified
2. Actionable recommendations are provided
3. Trade-offs are clearly explained
4. No speculation - all findings are evidence-based

## Usage

In your agent definition:

```markdown
## Quality Checklist

See @_common/quality-checklist.md for universal gates.

### Domain-Specific Checks
- [ ] [Domain-specific check 1]
- [ ] [Domain-specific check 2]
```
