# Multi-stage Dockerfile example demonstrating OSSB's multi-stage support

# Base stage with common dependencies
FROM alpine:3.18 AS base
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

# Build dependencies stage
FROM base AS build-deps
RUN apk add --no-cache gcc musl-dev make

# Application builder stage
FROM build-deps AS app-builder
COPY src/ /src/
WORKDIR /src
RUN make build
RUN cp app /tmp/app

# Asset builder stage (using different base)
FROM node:18-alpine AS asset-builder
COPY package*.json ./
RUN npm ci --only=production
COPY assets/ ./assets/
RUN npm run build

# Test stage (inherits from app-builder)
FROM app-builder AS test
RUN make test

# Final runtime stage
FROM base
# Copy application from app-builder stage
COPY --from=app-builder /tmp/app /usr/local/bin/app
# Copy assets from asset-builder stage
COPY --from=asset-builder /dist /var/www/html
# Copy test results from test stage
COPY --from=test /src/test-results.xml /app/test-results.xml

EXPOSE 8080
CMD ["/usr/local/bin/app"]