# Shamil Platform - AI Development Guidelines

This document serves as the primary instruction set for AI coding assistants (like Claude, Antigravity, or Relay) when generating, refactoring, or reviewing code for the Shamil Platform.

Always prioritize these rules when making code changes to any of the microservices or common libraries to ensure CI/CD pipelines, linting bindings, and quality gates pass successfully.

---

## 1. Technology Stack & Architecture

**Language:** Java 17  
**Core Framework:** Spring Boot 3.5.8 & Spring Cloud 2025.0.0  
**Workflow Engine:** Camunda BPM (7.24.0-alpha1)  
**Build Tool:** Maven (Multi-module architecture)

### Rule

Never hardcode dependency versions in child `pom.xml` files.

All dependency versions and plugin management are centralized in:

`/ebp-parent/pom.xml`

If a new dependency is required:

1. Map its version in the parent POM properties.
2. Import it into the child module application.

---

## 2. Code Formatting & CheckStyle Rules

The project strictly enforces Google's Java Style rules extended with Digitinary custom rules via `maven-checkstyle-plugin`.

If you fail to follow these, the build will fail immediately.

### Line Length

Maximum 150 characters per line.

### Imports

NO wildcard imports.

Example (forbidden):

```java
import java.util.*;
```

Expand all imports explicitly.

### Import Order

1. Static imports first  
2. Followed by third-party packages

### final Modifier

All method parameters, constructor parameters, and catch block parameters must be explicitly marked as `final`.

Correct:

```java
public void processReq(final String reqId)
```

Incorrect:

```java
public void processReq(String reqId)
```

### Variable Declarations

One statement per line.  
No multiple variable declarations on the same line.

### Braces

Always use braces `{}` for:

- if
- else
- for
- do
- while

Even if empty or single-line.

### Naming Conventions

Variables / Methods pattern:

```
^[a-z]([a-z0-9][a-zA-Z0-9_]*)?$
```

(Strict CamelCase)

Constant Names:

Must be ALL_CAPS separated by underscores.

Example:

```
MAX_RETRY_COUNT
```

---

## 3. Documentation (Javadoc)

Strict Javadoc enforcement is enabled for all public methods, classes, and interfaces.

### Required Tags

If applicable, the following tags MUST be present and in this exact sequence:

1. `@param`
2. `@return`
3. `@throws`

### Formatting

Avoid using forbidden summary fragments like:

- `@return the...`
- `This method returns`

### Method Scope

Any public method with more than 2 lines requires Javadoc.

### Descriptions

Ensure non-empty `@param` and `@return` descriptions.

---

## 4. Testing & Code Coverage

The platform strictly enforces test coverage via JaCoCo, Mockito, and JUnit 5.

### Minimum Coverage

80% (`COVEREDRATIO -> 0.80`) complexity and instruction coverage.

The build will fail if coverage drops below this threshold.

### Library Tooling

Use `mockito-inline` for static mocking (`MockedStatic`) when dealing with utility classes.

### Exclusions

The following packages are excluded from JaCoCo coverage:

- `/exception`
- `/config`
- `/model` (DTOs/Models)
- `/client` (external clients)

Do not waste AI context deeply testing POJOs. Focus on Service and Controller/Web layers.

---

## 5. Security & Tracing

Maintain Spring Security `SecurityContextHolder` patterns.

If spinning up asynchronous threads (e.g., `CompletableFuture.runAsync` or `ThreadPoolTaskExecutor`), you must explicitly propagate `RequestAttributes` and `SecurityContext` down to the child threads.

Logging should leverage Slf4j (`@Slf4j`). Avoid `System.out.println`.

---

## 6. General Coding Directives

Prefer `Optional<T>` over explicit null checking where API limits allow.

Leverage Lombok annotations such as:

- `@Data`
- `@Builder`
- `@AllArgsConstructor`
- `@NoArgsConstructor`
- `@Slf4j`

Use them to reduce boilerplate, but do not use them to bypass explicit mapping logic if specific DTO constructs require custom manipulation.

Code should be stateless wherever possible in the service layers to support horizontal scaling constraints.
