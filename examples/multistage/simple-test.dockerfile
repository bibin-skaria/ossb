# Simple multi-stage test that can actually be built
FROM scratch AS builder
# In a real scenario, this would copy and build something
# For testing, we'll just create a simple file structure

FROM scratch
# Copy from the builder stage (this tests the multi-stage functionality)
# In practice, this would copy built artifacts
# For now, this demonstrates the parsing and dependency tracking