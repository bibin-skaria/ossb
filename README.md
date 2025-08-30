# OSSB - Open Source Slim Builder

OSSB (Open Source Slim Builder) is a monolithic container builder inspired by BuildKit but designed as a single binary with no daemon dependency. It provides a simpler alternative to complex client-server container build systems while maintaining powerful features like content-addressable caching and pluggable architectures.

## Features

- **🔧 Monolithic Architecture**: Single binary with no daemon dependency
- **📦 Content-Addressable Caching**: Efficient layer caching like BuildKit
- **🧩 Pluggable System**: Extensible frontends, executors, and exporters
- **🐳 Complete Dockerfile Support**: All standard Dockerfile instructions
- **📁 Multiple Output Formats**: Image, tar, and local filesystem exports
- **🔍 Dependency Graph Solver**: Topological sorting with cycle detection
- **⚡ Fast Builds**: Optimized execution with intelligent caching
- **🖥️ Cross-Platform**: Supports Linux, macOS, and Windows

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

# Build with different output formats
ossb build . -t myapp --output tar
ossb build . -t myapp --output local

# Build with custom Dockerfile
ossb build . -f custom.Dockerfile -t myapp:v1.0

# Build with build arguments
ossb build . -t myapp --build-arg VERSION=1.0 --build-arg ENV=prod

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
- `-o, --output string` - Output type: image, tar, local (default: "image")
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

## License

OSSB is released under the MIT License.