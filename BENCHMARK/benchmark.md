# Go Backend Engineering Benchmark v1.0

## Purpose

This benchmark is intended to evaluate the engineering quality of a Go backend project, not merely whether it satisfies the functional requirements.

The goal is to assess how well the implementation reflects the practices expected from a Senior or Staff Backend Engineer.

The evaluator should prioritize engineering judgment, maintainability and long-term extensibility over feature count or framework usage.

---

# Scoring

Each category is evaluated using the following scale:

| Rating | Description |
|---------|-------------|
| Excellent | Production-quality implementation with strong engineering decisions. |
| Good | Solid implementation with only minor weaknesses. |
| Fair | Functional but with noticeable design or implementation issues. |
| Poor | Significant problems or missing implementation. |

Each category has a maximum number of points.

Scores should always be justified with evidence from the codebase.

---

# 1. Correctness (15 pts)

## Evaluate

- Functional correctness
- API behavior
- Notification lifecycle
- Requirement coverage
- Edge case handling

## Evidence

Reference endpoints, tests or implementation details.

---

# 2. Architecture & Project Organization (30 pts)

## Evaluate

- Project structure
- Separation of concerns
- Layering
- Dependency direction
- Cohesion
- Coupling
- Extensibility
- Scalability
- Folder organization

## Excellent

The project has a clear architecture with well-defined responsibilities and is easy to extend.

---

# 3. Dependency Injection & Dependency Management (25 pts)

## Evaluate

- Constructor injection
- Explicit dependencies
- Composition Root
- Interface design
- Dependency ownership
- Testability

## Penalize

- Global variables
- Service Locator
- Hidden dependencies
- Singleton abuse

---

# 4. Go Idiomatic Practices (30 pts)

## Evaluate

- Idiomatic package organization
- Naming conventions
- Small interfaces
- Composition over inheritance
- Context propagation
- Error wrapping
- Modern standard library usage
- Effective use of Go language features

Examples include (when appropriate):

- context.Context
- log/slog
- errors.Is / errors.As
- errors.Join
- slices
- maps
- sync/atomic
- http.ServeMux
- io/fs

The benchmark evaluates appropriate usage, not simply the presence of these APIs.

---

# 5. Concurrency (30 pts)

## Evaluate

- Concurrency model
- Goroutine lifecycle
- Cancellation
- Context propagation
- Worker pools
- Bounded concurrency
- Race condition avoidance
- Resource management

Particular attention should be paid to whether the chosen solution naturally fits the problem.

Solutions making effective use of errgroup (or an equivalent approach when justified) should be rewarded where independent operations benefit from cooperative cancellation.

---

# 6. Error Handling (20 pts)

## Evaluate

- Error propagation
- Error wrapping
- Domain errors
- Infrastructure errors
- Retry strategy
- Timeouts
- Failure recovery

## Penalize

- panic
- ignored errors
- log.Fatal in library code
- swallowed errors

---

# 7. Code Quality & Readability (20 pts)

## Evaluate

- Readability
- Function size
- Naming
- Duplication
- Responsibility boundaries
- Maintainability
- Code consistency

---

# 8. Testing (25 pts)

## Evaluate

- Unit tests
- Table-driven tests
- Meaningful assertions
- Edge cases
- Fake implementations
- Integration tests (when appropriate)

Testing quality is more important than coverage percentage.

---

# 9. Design Patterns & Extensibility (20 pts)

## Evaluate

Whether design patterns are introduced only when justified.

Reward appropriate use of patterns such as:

- Strategy
- Adapter
- Factory
- Repository
- Provider abstraction

Do not reward unnecessary abstraction.

---

# 10. Production Readiness (20 pts)

## Evaluate

- Configuration management
- Environment variables
- Logging
- Graceful shutdown
- Health endpoints
- Timeouts
- Docker support
- API documentation
- Build tooling

---

# 11. Documentation (10 pts)

## Evaluate

README quality.

Architecture explanation.

Assumptions.

Trade-offs.

Run instructions.

Future improvements.

---

# 12. Engineering Judgment (35 pts)

This category evaluates overall engineering maturity.

Consider:

- Appropriate level of abstraction
- Simplicity
- Trade-offs
- Technology choices
- Avoidance of overengineering
- Maintainability
- Long-term evolution
- Production mindset

This is the most subjective category and should always be thoroughly justified.

---

# Critical Issues

The following findings should reduce the overall score.

| Issue | Suggested Penalty |
|--------|------------------:|
| Race conditions | -10 |
| Hardcoded secrets | -10 |
| Circular dependencies | -10 |
| Hidden dependencies | -8 |
| Ignored errors | -5 |
| Global mutable state | -5 |
| Panic in normal execution flow | -5 |
| Business logic inside HTTP handlers | -5 |
| Service Locator | -5 |
| package util / common used as dumping ground | -3 |
| Interfaces with a single implementation without justification | -3 |
| Unnecessary concurrency | -3 |
| Dead code | -2 |

Penalties are cumulative when justified.

---

# Final Evaluation

At the end of the review, provide:

## Overall Score

Score: ___ / 280

---

## Strengths

Describe the strongest engineering aspects of the solution.

---

## Weaknesses

Describe the most important weaknesses.

---

## Suggested Improvements

List the highest-impact improvements.

---

## Hiring Recommendation

Choose one:

- Strong Hire
- Hire
- Lean Hire
- Lean No Hire
- No Hire

Provide a clear engineering justification for the recommendation.