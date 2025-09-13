FROM alpine:latest

# Install some basic tools
RUN apk add --no-cache curl wget

# Create a simple script
RUN echo '#!/bin/sh' > /hello.sh && \
    echo 'echo "Hello from OSSB!"' >> /hello.sh && \
    echo 'echo "Build time: $(date)"' >> /hello.sh && \
    chmod +x /hello.sh

# Set working directory
WORKDIR /app

# Copy a test file
COPY test.txt /app/test.txt

# Expose a port
EXPOSE 8080

# Set the default command
CMD ["/hello.sh"]