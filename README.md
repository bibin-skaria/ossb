# OSSB - Open Source Slim Builder

OSSB (Open Source Slim Builder) is a monolithic container builder inspired by BuildKit but designed as a single binary with no daemon dependency. It provides a simpler alternative to complex client-server container build systems while maintaining powerful features like content-addressable caching and pluggable architectures.

## Features

- **🔧 Monolithic Architecture**: Single binary with no daemon dependency
- **📦 Content-Addressable Caching**: Efficient layer caching like BuildKit
- **🧩 Pluggable System**: Extensible frontends, executors, and exporters
- **🐳 Complete Dockerfile Support**: All standard Dockerfile instructions
- **📁 Multiple Output Formats**: Image, tar, local filesystem, and multi-arch exports
- **🔍 Dependency Graph Solver**: Topological sorting with cycle detection
- **⚡ Fast Builds**: Optimized execution with intelligent caching
- **🖥️ Cross-Platform**: Supports Linux, macOS, and Windows
- **🏗️ Multi-Architecture Support**: Build for multiple platforms simultaneously
- **🐋 Container Integration**: Native Docker/Podman support with QEMU emulation
- **📤 Registry Push**: Direct push to container registries with manifest lists
- **👤 Rootless Mode**: Run without root privileges using rootless containers
- **🔒 User Namespace Support**: Secure builds with user namespace isolation

## Quick Start

### Installation

#### Build from Source
```bash
git clone https://github.com/bibin-skaria/ossb.git
cd ossb
make build
sudo cp bin/ossb /usr/local/bin/
```

#### Using Go Install
```bash
go install github.com/bibin-skaria/ossb/cmd@latest
```

### Basic Usage

```bash
# Build a container image from current directory
ossb build . -t myapp:latest

# Build for multiple architectures (multi-arch)
ossb build . -t myapp:latest --platform linux/amd64,linux/arm64

# Build with different output formats
ossb build . -t myapp --output tar
ossb build . -t myapp --output local
ossb build . -t myapp --output multiarch --platform linux/amd64,linux/arm64

# Build with custom Dockerfile
ossb build . -f custom.Dockerfile -t myapp:v1.0

# Build with build arguments
ossb build . -t myapp --build-arg VERSION=1.0 --build-arg ENV=prod

# Build and push to registry (multi-arch)
ossb build . -t myregistry.com/myapp:latest --platform linux/amd64,linux/arm64 --push --registry myregistry.com

# Use container executor for proper cross-compilation
ossb build . -t myapp:latest --platform linux/amd64,linux/arm64 --executor container

# ROOTLESS MODE - No root privileges required
ossb build . -t myapp:latest --rootless

# Rootless multi-arch build
ossb build . -t myapp:latest --platform linux/amd64,linux/arm64 --rootless

# Rootless build with push (uses rootless Podman/Docker)
ossb build . -t registry.io/myapp:latest --platform linux/amd64,linux/arm64 --rootless --push --registry registry.io

# Disable caching for clean build
ossb build . -t myapp --no-cache

# Check cache statistics
ossb cache info

# Clean up old cache entries
ossb cache prune
```

## Architecture

OSSB follows a modular architecture with pluggable components:

### Core Components

1. **Frontend System** (`frontends/`)
   - Parses build instructions (Dockerfiles, etc.)
   - Converts instructions into operation graphs
   - Currently supports: Dockerfile

2. **Execution System** (`executors/`)
   - Executes operations in the dependency graph
   - Handles different operation types: source, exec, file, meta
   - Currently supports: Local execution

3. **Export System** (`exporters/`)
   - Exports build results to different formats
   - Supports: OCI images, tar archives, local filesystem

4. **Build Engine** (`engine/`)
   - **Cache**: Content-addressable storage with SHA256 keys
   - **Graph Solver**: Dependency resolution with topological sorting
   - **Builder**: Orchestrates the entire build process

### Dockerfile Support

OSSB supports all standard Dockerfile instructions:

- `FROM` - Base image specification
- `RUN` - Execute commands during build
- `COPY` / `ADD` - Copy files into the image
- `WORKDIR` - Set working directory
- `ENV` - Set environment variables
- `EXPOSE` - Document exposed ports
- `CMD` / `ENTRYPOINT` - Set default commands
- `VOLUME` - Define mount points
- `USER` - Set user context
- `ARG` - Build-time arguments
- `LABEL` - Add metadata

## CLI Reference

### Build Command
```bash
ossb build [context] [flags]
```

**Flags:**
- `-f, --file string` - Dockerfile path (default: "Dockerfile")
- `-t, --tag strings` - Image tags (format: name:tag)
- `-o, --output string` - Output type: image, tar, local, multiarch (default: "image")
- `--platform strings` - Target platforms (e.g., linux/amd64,linux/arm64)
- `--push` - Push image to registry after build
- `--registry string` - Registry to push to (required with --push)
- `--executor string` - Executor type: local, container, rootless (default: "container")
- `--rootless` - Enable rootless mode (requires no root privileges)
- `--frontend string` - Frontend type (default: "dockerfile")
- `--cache-dir string` - Cache directory (default: ~/.ossb/cache)
- `--no-cache` - Disable caching
- `--progress` - Show build progress (default: true)
- `--build-arg strings` - Build arguments (format: KEY=VALUE)

### Cache Commands
```bash
# Show cache statistics
ossb cache info [--cache-dir path]

# Remove old cache entries
ossb cache prune [--cache-dir path]
```

## Output Formats

### Image (OCI Format)
```bash
ossb build . -t myapp:latest --output image
```
Creates OCI-compliant image manifest and configuration files.

### Tar Archive
```bash
ossb build . -t myapp:latest --output tar
```
Exports the built layers as a tar archive for distribution.

### Local Filesystem
```bash
ossb build . -t myapp:latest --output local
```
Exports the final filesystem to a local directory structure.

## Development

### Building from Source
```bash
# Clone repository
git clone https://github.com/bibin-skaria/ossb.git
cd ossb

# Install dependencies
make deps

# Build binary
make build

# Run tests
make test

# Build for all platforms
make build-all
```

### Project Structure
```
ossb/
├── cmd/                    # CLI entry point
├── engine/                 # Build engine (cache, graph, builder)
├── frontends/              # Frontend parsers (dockerfile)
├── executors/              # Execution engines (local)
├── exporters/              # Output exporters (image, tar, local)
├── internal/types/         # Common types and interfaces
├── Makefile               # Build automation
├── Dockerfile             # Multi-stage container build
└── README.md              # This file
```

## Examples

### Basic Web Application
```dockerfile
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
```

```bash
ossb build . -t webapp:latest
```

### Multi-stage Build
```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN go build -o app .

# Production stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/app /usr/local/bin/app
CMD ["app"]
```

```bash
ossb build . -t goapp:latest
```

### Multi-Architecture Build
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o app .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/app /usr/local/bin/app
CMD ["app"]
```

```bash
# Build for multiple architectures
ossb build . -t myapp:latest --platform linux/amd64,linux/arm64,linux/arm/v7

# Build and push to registry with manifest list
ossb build . -t registry.io/myapp:latest \
  --platform linux/amd64,linux/arm64 \
  --push --registry registry.io
```

## Rootless Mode

OSSB supports **rootless operation** for secure builds without requiring root privileges:

### Prerequisites for Rootless Mode
```bash
# Install rootless Podman (recommended)
sudo apt install podman

# Or configure Docker for rootless mode
dockerd-rootless-setuptool.sh install

# Verify user namespaces are available
cat /proc/sys/user/max_user_namespaces  # Should be > 0
```

### Rootless Examples
```bash
# Simple rootless build
ossb build . -t myapp:latest --rootless

# Rootless multi-architecture build
ossb build . -t myapp:latest \
  --platform linux/amd64,linux/arm64 \
  --rootless

# Rootless build with registry push
ossb build . -t ghcr.io/myapp:latest \
  --platform linux/amd64,linux/arm64 \
  --rootless --push --registry ghcr.io

# Rootless with custom cache directory (in user home)
ossb build . -t myapp:latest \
  --rootless --cache-dir ~/.ossb/rootless-cache
```

### Rootless Features
- ✅ **No sudo required**: Runs entirely as regular user
- ✅ **User namespace isolation**: Secure container execution
- ✅ **Multi-arch support**: Cross-platform builds without privileged containers
- ✅ **Registry push**: Direct push using rootless container runtime
- ✅ **Separate caching**: Isolated cache in user directory
- ✅ **QEMU emulation**: Unprivileged cross-architecture support

### Rootless vs Regular Mode

| Feature | Regular Mode | Rootless Mode |
|---------|--------------|---------------|
| Root privileges | Required for container operations | Not required |
| Container runtime | Docker/Podman (privileged) | Rootless Podman/Docker |
| QEMU emulation | Privileged containers | User-mode emulation |
| Cache location | System-wide | User directory |
| Security | Host root access | User namespace isolated |
| Registry push | Full access | User credentials only |

## License

OSSB is released under the MIT License.