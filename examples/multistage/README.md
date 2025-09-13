# Multi-Stage Dockerfile Example

This example demonstrates OSSB's multi-stage Dockerfile support with complex stage dependencies.

## Dockerfile Structure

The example Dockerfile contains 6 stages:

1. **base** - Common base with certificates and timezone data
2. **build-deps** - Inherits from `base`, adds build tools (gcc, make)
3. **app-builder** - Inherits from `build-deps`, builds the application
4. **asset-builder** - Independent stage using Node.js to build assets
5. **test** - Inherits from `app-builder`, runs tests
6. **final** - Inherits from `base`, copies artifacts from multiple stages

## Stage Dependencies

```
base
├── build-deps
│   ├── app-builder
│   │   └── test
│   └── final (FROM base)
├── final (FROM base)
└── asset-builder (independent)
    └── final (COPY --from=asset-builder)
```

## Features Demonstrated

- **Stage Inheritance**: `FROM base AS build-deps`
- **Cross-stage Copying**: `COPY --from=app-builder /tmp/app /usr/local/bin/app`
- **Complex Dependencies**: Final stage depends on `base`, `app-builder`, `asset-builder`, and `test`
- **Stage Isolation**: Each stage has its own filesystem and can be cached independently
- **Numeric References**: Could use `COPY --from=0` to reference the first stage

## Building with OSSB

```bash
# Build the multi-stage Dockerfile
ossb build -f examples/multistage/Dockerfile -t myapp:latest .

# Build only up to a specific stage
ossb build -f examples/multistage/Dockerfile --target test -t myapp:test .
```

## Benefits

1. **Optimized Final Image**: Only includes runtime dependencies
2. **Build Caching**: Each stage can be cached independently
3. **Parallel Builds**: Independent stages can be built in parallel
4. **Flexible Targeting**: Can build intermediate stages for testing
5. **Clean Separation**: Build tools don't end up in production image