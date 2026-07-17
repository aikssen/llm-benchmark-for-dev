# Backend Engineering Assessment

## Overview

You are tasked with designing and implementing a backend service responsible for orchestrating the delivery of notifications.

The goal is not only to satisfy the functional requirements, but also to produce a solution that demonstrates sound software engineering practices, good architectural decisions, maintainability, extensibility, and production readiness.

You are free to make reasonable assumptions whenever requirements are ambiguous. Any assumptions should be documented and justified.

The implementation language **must** be Go.

---

# Problem Statement

Our platform requires a backend service responsible for orchestrating notification delivery across multiple communication channels.

Initially, the supported channels are:

- Email
- SMS
- Push Notifications

The platform is expected to support additional notification channels in the future.

External notification providers are considered unreliable. They may:

- respond slowly
- temporarily fail
- become unavailable
- return intermittent errors

The service should make a best effort to deliver notifications reliably.

The system is expected to evolve over time and should be designed with future extensibility in mind.

---

# Functional Requirements

The service should expose an API that allows clients to:

- Submit a notification request.
- Retrieve the current status of a notification.
- List previously submitted notifications.

Each notification contains at least:

- Recipient
- Delivery channel
- Message contents

If one validation fails, the remaining validations should not continue consuming unnecessary resources.

The exact API design is intentionally left to your engineering judgment.

---

# Notification Processing

Before a notification can be delivered, several validation steps must be performed.

Examples may include (but are not limited to):

- validating the recipient
- checking user preferences
- verifying rate limits
- validating message contents

These validations are independent from each other.

A notification should only be delivered if all required validations succeed.

The implementation details are intentionally left open.

---

# Multiple Recipients

A single notification request may target one or multiple recipients.

The system should process notification requests efficiently while maintaining predictable resource usage.

The expected traffic pattern includes bursts of incoming requests.

---

# Providers

Notification providers are external systems.

Their implementations may be simulated.

The design should allow new providers to be added in the future with minimal modifications to the existing codebase.

---

# Non-functional Expectations

The application should be designed with long-term maintainability in mind.

The implementation should be suitable as the foundation of a production service.

You are encouraged to make reasonable engineering decisions whenever appropriate.

If you identify missing requirements that you consider important for building a robust production-ready service, you may introduce them, provided they are documented and justified.

---

# Technical Constraints

- The implementation language must be Go.
- You may use third-party libraries if you believe they improve the solution.
- External systems may be simulated.
- The project should be runnable locally.

Everything else is intentionally left to your engineering judgment.

---

# Deliverables

Your repository should contain:

- Complete source code
- Build and run instructions
- README
- Automated tests
- Any configuration required to run locally

---

# Design Decisions

Document the following:

- Architectural decisions
- Assumptions
- Trade-offs
- Future improvements

---

# Evaluation

The implementation will be evaluated primarily on engineering quality rather than feature count or the number of frameworks used.

Reviewers will consider aspects such as:

- Correctness
- Maintainability
- Extensibility
- Code readability
- Go idiomatic practices
- Software architecture
- Error handling
- Testing strategy
- Concurrency decisions
- Overall engineering judgment

Simple, well-structured and maintainable solutions are preferred over unnecessarily complex implementations.

---

# Final Note

Approach this assignment as if you were implementing the first production-ready version of a service that your team will maintain and extend over the coming years.