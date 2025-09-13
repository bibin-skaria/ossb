FROM busybox:latest

# Add a simple test command
RUN echo "Hello from OSSB busybox build!" > /hello.txt

# Set a working directory
WORKDIR /app

# Add a simple command
CMD ["cat", "/hello.txt"]
