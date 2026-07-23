# Notification Service Architecture & Design Specification

## Overview

The **Notification Service** is a production-grade, highly resilient backend microservice written in idiomatic Go. It orchestrates notification delivery across multiple channels (Email, SMS, Push) to single or multiple recipients under high-concurrency traffic bursts.

---

## Architectural Principles & SOLID Design

1. **Clean Architecture (Separation of Concerns)**:
   - `domain`: Core domain models (`NotificationRequest`, `Channel`, `Status`) with zero external dependencies.
   - `validator`: Fast-failing concurrent validation pipeline interface and implementations.
   - `provider`: Channel provider abstraction, registry, failover chain, and simulated third-party integrations.
   - `repository`: Storage contracts and thread-safe memory implementation with query filtering & pagination.
   - `worker`: Bounded worker pool handling bursty asynchronous jobs cleanly without memory unbounded goroutine leaks.
   - `service`: Orchestration engine coupling repository, validators, providers, and worker pool.
   - `handler`: REST API HTTP endpoints, request parsing, and error code translation.
   - `resilience`: Circuit breaker pattern and exponential backoff retry helpers.

2. **Concurrent Fast-Failing Validation Pipeline**:
   - **Requirement**: Independent validations where a single failure immediately halts remaining validations to avoid wasting downstream resources.
   - **Implementation**: Executes validation checks concurrently using `context.WithCancel`. Upon the first validation error, `cancel()` is immediately called. Other concurrent validators watching `<-ctx.Done()` terminate early.

3. **Provider Failover & Resilience Chain**:
   - **Requirement**: Handle unreliable external providers (slow, failing, rate-limited).
   - **Implementation**: Each channel maintains a chain of providers (Primary $\rightarrow$ Fallback 1 $\rightarrow$ Fallback 2). Each provider call is wrapped in:
     - **Exponential Backoff with Jitter Retry**: Handles transient intermittent network blips.
     - **Circuit Breaker**: Prevents hammering failing downstream services. When a provider's failure threshold is breached, the circuit trips to `OPEN`, immediately failing fast or routing requests directly to fallback providers.

4. **Burst Handling & Resource Safety (Worker Pool Pattern)**:
   - **Requirement**: Process single/multiple recipient requests efficiently during traffic bursts while keeping resource usage predictable.
   - **Implementation**: The service uses a bounded job queue (`chan *domain.SingleRecipientItem`) fed into a configurable pool of worker goroutines. This ensures memory usage stays bounded even during huge request spikes.

5. **Graceful Shutdown**:
   - Listens to OS signals (`SIGINT`, `SIGTERM`).
   - Stops HTTP listener, drains the worker pool queue, allowing ongoing job deliveries to finalize before cleanly exiting.

---

## Component Diagram

```
+-----------------------------------------------------------------------------------+
|                                  HTTP REST API                                    |
|   POST /api/v1/notifications   |   GET /api/v1/notifications/{id}   |  GET /health |
+------------------------------------------+----------------------------------------+
                                           |
                                           v
+-----------------------------------------------------------------------------------+
|                              Service Orchestrator                                 |
+---------------------+--------------------+-------------------+--------------------+
                      |                    |                   |
                      v                    v                   v
             +----------------+   +-----------------+  +---------------+
             | Worker Pool    |   | Repository      |  | Fast-Failing  |
             | & Queue        |   | (In-Memory/DB)  |  | Validation    |
             +--------+-------+   +-----------------+  | Pipeline      |
                      |                                +---------------+
                      v
             +----------------+
             | Provider       |
             | Registry       |
             +--------+-------+
                      |
        +-------------+-------------+
        |             |             |
        v             v             v
  [ Email Chain ] [ SMS Chain ] [ Push Chain ]
   Primary (SES)   Primary (Twi) Primary (FCM)
     || (fallback)   || (fallback) || (fallback)
   Fallback (Grid) Fallback (Info) Fallback (APNS)
```

---

## System States & Lifecycle

```
[Received] ---> (Queued) ---> [Validating] --- (Fail) ---> [Invalid]
                                  |
                                (Pass)
                                  v
                             [Processing] --- (Provider Sent) ---> [Delivered]
                                  |
                           (All Providers Fail)
                                  v
                              [Failed]
```
