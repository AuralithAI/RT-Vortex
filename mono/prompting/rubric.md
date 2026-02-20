# Code Review Rubric

Use this rubric to evaluate code changes consistently. Each category has specific criteria to check.

## 1. Security (Weight: Critical)

### Authentication & Authorization
- [ ] Proper authentication checks on protected endpoints
- [ ] Authorization verified before data access
- [ ] No hardcoded credentials or secrets
- [ ] Session management is secure

### Input Validation
- [ ] All user inputs validated and sanitized
- [ ] Parameterized queries for database operations
- [ ] No command injection vulnerabilities
- [ ] File path traversal protection

### Data Protection
- [ ] Sensitive data encrypted at rest and in transit
- [ ] PII properly handled and logged appropriately
- [ ] No sensitive data in URLs or logs
- [ ] Proper error messages (no stack traces to users)

## 2. Reliability (Weight: High)

### Error Handling
- [ ] Exceptions caught at appropriate levels
- [ ] Meaningful error messages for debugging
- [ ] Graceful degradation when dependencies fail
- [ ] No silent failures that hide problems

### Resource Management
- [ ] Files, connections, handles properly closed
- [ ] Timeouts configured for external calls
- [ ] Memory allocations bounded
- [ ] Cleanup in finally blocks or using patterns

### Concurrency
- [ ] Thread-safe access to shared state
- [ ] No race conditions in critical sections
- [ ] Deadlock-free locking order
- [ ] Atomic operations where needed

## 3. Performance (Weight: Medium)

### Efficiency
- [ ] No N+1 query patterns
- [ ] Appropriate data structures used
- [ ] Unnecessary work avoided (caching, early returns)
- [ ] Pagination for large result sets

### Scalability
- [ ] Operations scale with input size
- [ ] No unbounded loops or recursion
- [ ] Async patterns for I/O-bound work
- [ ] Connection pooling for external services

## 4. Testing (Weight: Medium)

### Coverage
- [ ] New code paths have test coverage
- [ ] Edge cases tested
- [ ] Error paths tested
- [ ] Integration points tested

### Quality
- [ ] Tests are deterministic (no flaky tests)
- [ ] Test names describe behavior
- [ ] Appropriate use of mocks/stubs
- [ ] Tests run in isolation

## 5. Documentation (Weight: Low)

### Code Comments
- [ ] Complex logic explained
- [ ] Public APIs documented
- [ ] TODOs have ticket references
- [ ] No misleading/outdated comments

### External Docs
- [ ] README updated if needed
- [ ] API docs reflect changes
- [ ] Migration/upgrade notes provided
- [ ] Architecture decisions documented

## 6. Architecture (Weight: Contextual)

### Design Patterns
- [ ] Follows established patterns in codebase
- [ ] Single responsibility principle
- [ ] Appropriate abstraction level
- [ ] Dependencies injected (not hardcoded)

### Breaking Changes
- [ ] Backward compatibility maintained
- [ ] Deprecation warnings added
- [ ] Migration path documented
- [ ] Version bump appropriate

## Scoring Guide

For each issue found, assign severity based on:

| Criteria | Critical | Error | Warning | Info |
|----------|----------|-------|---------|------|
| Security | Always | N/A | N/A | N/A |
| Data Loss | Always | Possible | N/A | N/A |
| Production Impact | Service down | Degraded | Minor | None |
| Fix Effort | Any | Any | Medium | Low |
| Frequency | Any | Common | Rare | Edge case |

## Red Flags (Immediate Attention)

- `eval()` or equivalent dynamic code execution
- Disabled security features (CSRF, auth bypass)
- Credentials in code or config files
- TODO/FIXME in critical paths
- `@SuppressWarnings` or equivalent without justification
- `catch (Exception e) {}` - swallowed exceptions
- `Thread.sleep()` in production code
- Magic numbers without constants

## Green Flags (Positive Signals)

- Comprehensive test coverage
- Clear error messages
- Proper logging (not too much, not too little)
- Input validation at boundaries
- Defensive programming
- Clear separation of concerns
- Consistent coding style
