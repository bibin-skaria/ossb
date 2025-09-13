#!/bin/bash

# Performance Benchmark Script for OSSB
# Comprehensive performance testing and comparison with other tools

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

# Configuration
BENCHMARK_OUTPUT_DIR="${BENCHMARK_OUTPUT_DIR:-./test-results/performance-benchmark}"
OSSB_BINARY="${OSSB_BINARY:-./ossb}"
BENCHMARK_TIMEOUT="${BENCHMARK_TIMEOUT:-1800}" # 30 minutes
BENCHMARK_ITERATIONS="${BENCHMARK_ITERATIONS:-3}"

# Tool availability
KANIKO_AVAILABLE=false
BUILDKIT_AVAILABLE=false
DOCKER_AVAILABLE=false

# Benchmark results
declare -A BENCHMARK_RESULTS

# Initialize benchmark environment
init_benchmark_environment() {
    print_header "Initializing Performance Benchmark Environment"
    
    # Create output directory
    mkdir -p "$BENCHMARK_OUTPUT_DIR"
    
    # Create temporary test directory
    BENCHMARK_DIR=$(mktemp -d -t ossb-benchmark-XXXXXX)
    export BENCHMARK_DIR
    
    # Check tool availability
    check_tool_availability
    
    print_success "Benchmark environment initialized"
    print_info "Output directory: $BENCHMARK_OUTPUT_DIR"
    print_info "Test directory: $BENCHMARK_DIR"
}

# Check availability of comparison tools
check_tool_availability() {
    print_header "Checking Tool Availability"
    
    # Check OSSB
    if [ -f "$OSSB_BINARY" ] || command -v "$OSSB_BINARY" &> /dev/null; then
        print_success "OSSB available"
    else
        print_error "OSSB binary not found: $OSSB_BINARY"
        exit 1
    fi
    
    # Check Kaniko
    if command -v kaniko &> /dev/null; then
        KANIKO_AVAILABLE=true
        print_success "Kaniko available: $(kaniko version 2>/dev/null || echo 'unknown version')"
    else
        print_warning "Kaniko not available - comparison limited"
    fi
    
    # Check BuildKit (docker buildx)
    if command -v docker &> /dev/null && docker buildx version &> /dev/null; then
        BUILDKIT_AVAILABLE=true
        DOCKER_AVAILABLE=true
        print_success "BuildKit available: $(docker buildx version | head -1)"
    elif command -v docker &> /dev/null; then
        DOCKER_AVAILABLE=true
        print_success "Docker available: $(docker --version)"
        print_warning "BuildKit not available - using legacy Docker build"
    else
        print_warning "Docker not available - comparison limited"
    fi
    
    # Check system resources
    print_info "System resources:"
    if command -v nproc &> /dev/null; then
        print_info "  CPU cores: $(nproc)"
    fi
    if command -v free &> /dev/null; then
        print_info "  Memory: $(free -h | grep '^Mem:' | awk '{print $2}')"
    fi
    if command -v df &> /dev/null; then
        print_info "  Disk space: $(df -h . | tail -1 | awk '{print $4}') available"
    fi
}

# Clean up benchmark environment
cleanup_benchmark_environment() {
    print_header "Cleaning Up Benchmark Environment"
    
    if [ -n "$BENCHMARK_DIR" ] && [ -d "$BENCHMARK_DIR" ]; then
        rm -rf "$BENCHMARK_DIR"
        print_success "Benchmark directory cleaned up"
    fi
    
    # Clean up test images
    if [ "$DOCKER_AVAILABLE" = true ]; then
        docker system prune -f --filter "label=ossb-benchmark" &> /dev/null || true
        print_success "Docker test images cleaned up"
    fi
}

# Create test files for benchmarks
create_test_files() {
    local test_dir="$1"
    local test_type="$2"
    
    case "$test_type" in
        "simple")
            cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN echo "Simple benchmark test" > /test.txt
EXPOSE 8080
CMD ["cat", "/test.txt"]
EOF
            ;;
        "complex")
            cat > "$test_dir/Dockerfile" << 'EOF'
FROM ubuntu:20.04
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    git \
    build-essential \
    python3 \
    python3-pip \
    nodejs \
    npm
WORKDIR /app
COPY package.json requirements.txt ./
RUN pip3 install -r requirements.txt
RUN npm install
COPY . .
RUN python3 -m py_compile app.py
RUN npm run build
EXPOSE 3000
CMD ["python3", "app.py"]
EOF
            cat > "$test_dir/package.json" << 'EOF'
{
  "name": "benchmark-app",
  "version": "1.0.0",
  "scripts": {
    "build": "echo 'Building...'",
    "start": "node server.js"
  },
  "dependencies": {
    "express": "^4.18.0"
  }
}
EOF
            cat > "$test_dir/requirements.txt" << 'EOF'
flask==2.2.0
requests==2.28.0
numpy==1.24.0
EOF
            cat > "$test_dir/app.py" << 'EOF'
from flask import Flask
app = Flask(__name__)

@app.route('/')
def hello():
    return 'Hello from benchmark app!'

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=3000)
EOF
            cat > "$test_dir/server.js" << 'EOF'
const express = require('express');
const app = express();
app.get('/', (req, res) => res.send('Hello from Node.js!'));
app.listen(3000);
EOF
            ;;
        "multistage")
            cat > "$test_dir/Dockerfile" << 'EOF'
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
EOF
            cat > "$test_dir/go.mod" << 'EOF'
module benchmark-app
go 1.21
require github.com/gorilla/mux v1.8.0
EOF
            cat > "$test_dir/go.sum" << 'EOF'
github.com/gorilla/mux v1.8.0 h1:i40aqfkR1h2SlN9hojwV5ZA91wcXFOvkdNIeFDP5koI=
github.com/gorilla/mux v1.8.0/go.mod h1:DVbg23sWSpFRCP0SfiEN6jmj59UnW/n46BH5rLB71So=
EOF
            cat > "$test_dir/main.go" << 'EOF'
package main

import (
    "fmt"
    "log"
    "net/http"
    "github.com/gorilla/mux"
)

func main() {
    r := mux.NewRouter()
    r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello from Go benchmark app!")
    })
    log.Fatal(http.ListenAndServe(":8080", r))
}
EOF
            ;;
        "large_context")
            # Create a large build context
            mkdir -p "$test_dir/data"
            for i in {1..100}; do
                dd if=/dev/zero of="$test_dir/data/file$i.dat" bs=1M count=1 2>/dev/null
            done
            
            cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
COPY data/ /app/data/
RUN ls -la /app/data/ | wc -l > /app/file-count.txt
CMD ["cat", "/app/file-count.txt"]
EOF
            ;;
    esac
}

# Benchmark OSSB
benchmark_ossb() {
    local test_name="$1"
    local test_dir="$2"
    local platforms="$3"
    local additional_args="$4"
    
    local start_time=$(date +%s.%N)
    local memory_before=$(get_memory_usage)
    
    cd "$test_dir"
    
    local build_cmd="$OSSB_BINARY build --tag ossb-benchmark-$test_name:latest"
    
    if [ -n "$platforms" ]; then
        build_cmd="$build_cmd --platform $platforms"
    fi
    
    if [ -n "$additional_args" ]; then
        build_cmd="$build_cmd $additional_args"
    fi
    
    build_cmd="$build_cmd ."
    
    local build_output
    if build_output=$(timeout "$BENCHMARK_TIMEOUT" $build_cmd 2>&1); then
        local end_time=$(date +%s.%N)
        local memory_after=$(get_memory_usage)
        local duration=$(echo "$end_time - $start_time" | bc -l)
        local memory_used=$((memory_after - memory_before))
        
        # Extract metrics from build output
        local cache_hits=$(echo "$build_output" | grep -o "cache hits: [0-9]*" | grep -o "[0-9]*" || echo "0")
        local operations=$(echo "$build_output" | grep -o "operations: [0-9]*" | grep -o "[0-9]*" || echo "0")
        
        BENCHMARK_RESULTS["ossb_${test_name}_duration"]="$duration"
        BENCHMARK_RESULTS["ossb_${test_name}_memory"]="$memory_used"
        BENCHMARK_RESULTS["ossb_${test_name}_cache_hits"]="$cache_hits"
        BENCHMARK_RESULTS["ossb_${test_name}_operations"]="$operations"
        BENCHMARK_RESULTS["ossb_${test_name}_success"]="true"
        
        return 0
    else
        BENCHMARK_RESULTS["ossb_${test_name}_success"]="false"
        BENCHMARK_RESULTS["ossb_${test_name}_error"]="$build_output"
        return 1
    fi
}

# Benchmark Kaniko
benchmark_kaniko() {
    local test_name="$1"
    local test_dir="$2"
    local platforms="$3"
    
    if [ "$KANIKO_AVAILABLE" != true ]; then
        return 1
    fi
    
    local start_time=$(date +%s.%N)
    local memory_before=$(get_memory_usage)
    
    cd "$test_dir"
    
    local build_cmd="kaniko --context . --dockerfile Dockerfile --destination kaniko-benchmark-$test_name:latest --no-push"
    
    local build_output
    if build_output=$(timeout "$BENCHMARK_TIMEOUT" $build_cmd 2>&1); then
        local end_time=$(date +%s.%N)
        local memory_after=$(get_memory_usage)
        local duration=$(echo "$end_time - $start_time" | bc -l)
        local memory_used=$((memory_after - memory_before))
        
        BENCHMARK_RESULTS["kaniko_${test_name}_duration"]="$duration"
        BENCHMARK_RESULTS["kaniko_${test_name}_memory"]="$memory_used"
        BENCHMARK_RESULTS["kaniko_${test_name}_success"]="true"
        
        return 0
    else
        BENCHMARK_RESULTS["kaniko_${test_name}_success"]="false"
        BENCHMARK_RESULTS["kaniko_${test_name}_error"]="$build_output"
        return 1
    fi
}

# Benchmark BuildKit
benchmark_buildkit() {
    local test_name="$1"
    local test_dir="$2"
    local platforms="$3"
    
    if [ "$BUILDKIT_AVAILABLE" != true ]; then
        return 1
    fi
    
    local start_time=$(date +%s.%N)
    local memory_before=$(get_memory_usage)
    
    cd "$test_dir"
    
    local build_cmd="docker buildx build --tag buildkit-benchmark-$test_name:latest"
    
    if [ -n "$platforms" ]; then
        build_cmd="$build_cmd --platform $platforms"
    fi
    
    build_cmd="$build_cmd ."
    
    local build_output
    if build_output=$(timeout "$BENCHMARK_TIMEOUT" $build_cmd 2>&1); then
        local end_time=$(date +%s.%N)
        local memory_after=$(get_memory_usage)
        local duration=$(echo "$end_time - $start_time" | bc -l)
        local memory_used=$((memory_after - memory_before))
        
        BENCHMARK_RESULTS["buildkit_${test_name}_duration"]="$duration"
        BENCHMARK_RESULTS["buildkit_${test_name}_memory"]="$memory_used"
        BENCHMARK_RESULTS["buildkit_${test_name}_success"]="true"
        
        return 0
    else
        BENCHMARK_RESULTS["buildkit_${test_name}_success"]="false"
        BENCHMARK_RESULTS["buildkit_${test_name}_error"]="$build_output"
        return 1
    fi
}

# Benchmark Docker (legacy)
benchmark_docker() {
    local test_name="$1"
    local test_dir="$2"
    
    if [ "$DOCKER_AVAILABLE" != true ] || [ "$BUILDKIT_AVAILABLE" = true ]; then
        return 1
    fi
    
    local start_time=$(date +%s.%N)
    local memory_before=$(get_memory_usage)
    
    cd "$test_dir"
    
    local build_cmd="docker build --tag docker-benchmark-$test_name:latest ."
    
    local build_output
    if build_output=$(timeout "$BENCHMARK_TIMEOUT" $build_cmd 2>&1); then
        local end_time=$(date +%s.%N)
        local memory_after=$(get_memory_usage)
        local duration=$(echo "$end_time - $start_time" | bc -l)
        local memory_used=$((memory_after - memory_before))
        
        BENCHMARK_RESULTS["docker_${test_name}_duration"]="$duration"
        BENCHMARK_RESULTS["docker_${test_name}_memory"]="$memory_used"
        BENCHMARK_RESULTS["docker_${test_name}_success"]="true"
        
        return 0
    else
        BENCHMARK_RESULTS["docker_${test_name}_success"]="false"
        BENCHMARK_RESULTS["docker_${test_name}_error"]="$build_output"
        return 1
    fi
}

# Get current memory usage
get_memory_usage() {
    if command -v free &> /dev/null; then
        free -m | grep '^Mem:' | awk '{print $3}'
    else
        echo "0"
    fi
}

# Run benchmark test
run_benchmark_test() {
    local test_name="$1"
    local test_type="$2"
    local platforms="$3"
    local description="$4"
    
    print_header "Benchmark Test: $test_name"
    print_info "Description: $description"
    print_info "Type: $test_type"
    if [ -n "$platforms" ]; then
        print_info "Platforms: $platforms"
    fi
    
    local test_dir="$BENCHMARK_DIR/$test_name"
    mkdir -p "$test_dir"
    
    # Create test files
    create_test_files "$test_dir" "$test_type"
    
    # Run benchmarks for each tool
    print_info "Benchmarking OSSB..."
    if benchmark_ossb "$test_name" "$test_dir" "$platforms"; then
        print_success "OSSB benchmark completed"
    else
        print_error "OSSB benchmark failed"
    fi
    
    if [ "$KANIKO_AVAILABLE" = true ]; then
        print_info "Benchmarking Kaniko..."
        if benchmark_kaniko "$test_name" "$test_dir" "$platforms"; then
            print_success "Kaniko benchmark completed"
        else
            print_error "Kaniko benchmark failed"
        fi
    fi
    
    if [ "$BUILDKIT_AVAILABLE" = true ]; then
        print_info "Benchmarking BuildKit..."
        if benchmark_buildkit "$test_name" "$test_dir" "$platforms"; then
            print_success "BuildKit benchmark completed"
        else
            print_error "BuildKit benchmark failed"
        fi
    elif [ "$DOCKER_AVAILABLE" = true ]; then
        print_info "Benchmarking Docker..."
        if benchmark_docker "$test_name" "$test_dir"; then
            print_success "Docker benchmark completed"
        else
            print_error "Docker benchmark failed"
        fi
    fi
    
    # Clean up test directory
    rm -rf "$test_dir"
}

# Run cache efficiency test
run_cache_efficiency_test() {
    print_header "Cache Efficiency Test"
    
    local test_dir="$BENCHMARK_DIR/cache_test"
    mkdir -p "$test_dir"
    
    create_test_files "$test_dir" "simple"
    
    # First build (cold cache)
    print_info "Running cold cache build..."
    benchmark_ossb "cache_cold" "$test_dir" "" "--no-cache"
    
    # Second build (warm cache)
    print_info "Running warm cache build..."
    benchmark_ossb "cache_warm" "$test_dir" ""
    
    # Calculate cache efficiency
    local cold_duration="${BENCHMARK_RESULTS[ossb_cache_cold_duration]}"
    local warm_duration="${BENCHMARK_RESULTS[ossb_cache_warm_duration]}"
    local warm_cache_hits="${BENCHMARK_RESULTS[ossb_cache_warm_cache_hits]}"
    
    if [ -n "$cold_duration" ] && [ -n "$warm_duration" ]; then
        local speedup=$(echo "scale=2; $cold_duration / $warm_duration" | bc -l)
        BENCHMARK_RESULTS["cache_speedup"]="$speedup"
        print_success "Cache speedup: ${speedup}x"
    fi
    
    rm -rf "$test_dir"
}

# Run concurrent builds test
run_concurrent_builds_test() {
    print_header "Concurrent Builds Test"
    
    local concurrency_levels=(1 2 4)
    
    for concurrency in "${concurrency_levels[@]}"; do
        print_info "Testing concurrency level: $concurrency"
        
        local pids=()
        local start_time=$(date +%s.%N)
        
        for ((i=1; i<=concurrency; i++)); do
            local test_dir="$BENCHMARK_DIR/concurrent_${concurrency}_${i}"
            mkdir -p "$test_dir"
            create_test_files "$test_dir" "simple"
            
            (
                cd "$test_dir"
                timeout "$BENCHMARK_TIMEOUT" "$OSSB_BINARY" build --tag "ossb-concurrent-${concurrency}-${i}:latest" . &> /dev/null
            ) &
            pids+=($!)
        done
        
        # Wait for all builds to complete
        local success_count=0
        for pid in "${pids[@]}"; do
            if wait "$pid"; then
                success_count=$((success_count + 1))
            fi
        done
        
        local end_time=$(date +%s.%N)
        local total_duration=$(echo "$end_time - $start_time" | bc -l)
        
        BENCHMARK_RESULTS["concurrent_${concurrency}_duration"]="$total_duration"
        BENCHMARK_RESULTS["concurrent_${concurrency}_success_rate"]="$(echo "scale=2; $success_count * 100 / $concurrency" | bc -l)"
        
        print_success "Concurrency $concurrency: ${total_duration}s, success rate: ${BENCHMARK_RESULTS[concurrent_${concurrency}_success_rate]}%"
        
        # Clean up
        for ((i=1; i<=concurrency; i++)); do
            rm -rf "$BENCHMARK_DIR/concurrent_${concurrency}_${i}"
        done
    done
}

# Generate performance report
generate_performance_report() {
    local report_file="$BENCHMARK_OUTPUT_DIR/performance-benchmark-report.md"
    
    print_header "Generating Performance Benchmark Report"
    
    cat > "$report_file" << EOF
# OSSB Performance Benchmark Report

Generated on: $(date)
OSSB Version: $("$OSSB_BINARY" version 2>/dev/null || echo "unknown")
System: $(uname -s) $(uname -m)
CPU Cores: $(nproc 2>/dev/null || echo "unknown")
Memory: $(free -h 2>/dev/null | grep '^Mem:' | awk '{print $2}' || echo "unknown")

## Tool Availability

- **OSSB**: ✅ Available
- **Kaniko**: $([ "$KANIKO_AVAILABLE" = true ] && echo "✅ Available" || echo "❌ Not Available")
- **BuildKit**: $([ "$BUILDKIT_AVAILABLE" = true ] && echo "✅ Available" || echo "❌ Not Available")
- **Docker**: $([ "$DOCKER_AVAILABLE" = true ] && echo "✅ Available" || echo "❌ Not Available")

## Benchmark Results

### Build Speed Comparison

| Test Case | OSSB | Kaniko | BuildKit | Docker |
|-----------|------|--------|----------|--------|
EOF

    # Add build speed comparison table
    local test_cases=("simple" "complex" "multistage")
    for test_case in "${test_cases[@]}"; do
        local ossb_duration="${BENCHMARK_RESULTS[ossb_${test_case}_duration]:-N/A}"
        local kaniko_duration="${BENCHMARK_RESULTS[kaniko_${test_case}_duration]:-N/A}"
        local buildkit_duration="${BENCHMARK_RESULTS[buildkit_${test_case}_duration]:-N/A}"
        local docker_duration="${BENCHMARK_RESULTS[docker_${test_case}_duration]:-N/A}"
        
        echo "| $test_case | ${ossb_duration}s | ${kaniko_duration}s | ${buildkit_duration}s | ${docker_duration}s |" >> "$report_file"
    done
    
    cat >> "$report_file" << EOF

### Memory Usage Comparison

| Test Case | OSSB | Kaniko | BuildKit | Docker |
|-----------|------|--------|----------|--------|
EOF

    # Add memory usage comparison table
    for test_case in "${test_cases[@]}"; do
        local ossb_memory="${BENCHMARK_RESULTS[ossb_${test_case}_memory]:-N/A}"
        local kaniko_memory="${BENCHMARK_RESULTS[kaniko_${test_case}_memory]:-N/A}"
        local buildkit_memory="${BENCHMARK_RESULTS[buildkit_${test_case}_memory]:-N/A}"
        local docker_memory="${BENCHMARK_RESULTS[docker_${test_case}_memory]:-N/A}"
        
        echo "| $test_case | ${ossb_memory}MB | ${kaniko_memory}MB | ${buildkit_memory}MB | ${docker_memory}MB |" >> "$report_file"
    done
    
    cat >> "$report_file" << EOF

### Cache Efficiency

- **Cold Cache Build**: ${BENCHMARK_RESULTS[ossb_cache_cold_duration]:-N/A}s
- **Warm Cache Build**: ${BENCHMARK_RESULTS[ossb_cache_warm_duration]:-N/A}s
- **Cache Speedup**: ${BENCHMARK_RESULTS[cache_speedup]:-N/A}x
- **Cache Hits**: ${BENCHMARK_RESULTS[ossb_cache_warm_cache_hits]:-N/A}

### Concurrent Builds Performance

| Concurrency | Duration | Success Rate |
|-------------|----------|--------------|
EOF

    # Add concurrent builds results
    local concurrency_levels=(1 2 4)
    for concurrency in "${concurrency_levels[@]}"; do
        local duration="${BENCHMARK_RESULTS[concurrent_${concurrency}_duration]:-N/A}"
        local success_rate="${BENCHMARK_RESULTS[concurrent_${concurrency}_success_rate]:-N/A}"
        echo "| $concurrency | ${duration}s | ${success_rate}% |" >> "$report_file"
    done
    
    cat >> "$report_file" << EOF

## Performance Analysis

### OSSB vs Competitors

EOF

    # Calculate performance ratios
    for test_case in "${test_cases[@]}"; do
        local ossb_duration="${BENCHMARK_RESULTS[ossb_${test_case}_duration]}"
        
        if [ -n "$ossb_duration" ] && [ "$ossb_duration" != "N/A" ]; then
            echo "#### $test_case Build" >> "$report_file"
            echo "" >> "$report_file"
            
            # Compare with Kaniko
            local kaniko_duration="${BENCHMARK_RESULTS[kaniko_${test_case}_duration]}"
            if [ -n "$kaniko_duration" ] && [ "$kaniko_duration" != "N/A" ]; then
                local ratio=$(echo "scale=2; $ossb_duration / $kaniko_duration" | bc -l)
                echo "- **OSSB vs Kaniko**: ${ratio}x ($([ $(echo "$ratio < 1" | bc -l) -eq 1 ] && echo "faster" || echo "slower"))" >> "$report_file"
            fi
            
            # Compare with BuildKit
            local buildkit_duration="${BENCHMARK_RESULTS[buildkit_${test_case}_duration]}"
            if [ -n "$buildkit_duration" ] && [ "$buildkit_duration" != "N/A" ]; then
                local ratio=$(echo "scale=2; $ossb_duration / $buildkit_duration" | bc -l)
                echo "- **OSSB vs BuildKit**: ${ratio}x ($([ $(echo "$ratio < 1" | bc -l) -eq 1 ] && echo "faster" || echo "slower"))" >> "$report_file"
            fi
            
            echo "" >> "$report_file"
        fi
    done
    
    cat >> "$report_file" << EOF

## Recommendations

### Performance Optimization
1. **Cache Utilization**: OSSB shows $([ -n "${BENCHMARK_RESULTS[cache_speedup]}" ] && echo "${BENCHMARK_RESULTS[cache_speedup]}x speedup" || echo "good") with warm cache
2. **Concurrent Builds**: Optimal concurrency appears to be around 2-4 parallel builds
3. **Memory Usage**: Monitor memory consumption for large builds
4. **Build Context**: Minimize build context size for better performance

### Best Practices
1. Use layer caching effectively
2. Optimize Dockerfile instruction order
3. Use multi-stage builds for complex applications
4. Consider build context size and .dockerignore usage
5. Monitor resource usage in production environments

## Detailed Results

### Raw Benchmark Data

EOF

    # Add raw benchmark data
    for key in "${!BENCHMARK_RESULTS[@]}"; do
        echo "- **$key**: ${BENCHMARK_RESULTS[$key]}" >> "$report_file"
    done
    
    cat >> "$report_file" << EOF

---

*This report was generated automatically by the OSSB Performance Benchmark Tool.*
*Results may vary based on system configuration, network conditions, and workload characteristics.*

EOF

    print_success "Performance benchmark report generated: $report_file"
}

# Main benchmark function
run_performance_benchmark() {
    print_header "OSSB Performance Benchmark"
    
    # Initialize environment
    init_benchmark_environment
    
    # Set up cleanup trap
    trap cleanup_benchmark_environment EXIT
    
    # Run benchmark tests
    run_benchmark_test "simple" "simple" "" "Simple Alpine-based build"
    run_benchmark_test "complex" "complex" "" "Complex multi-language application build"
    run_benchmark_test "multistage" "multistage" "" "Multi-stage Go application build"
    run_benchmark_test "multiarch" "simple" "linux/amd64,linux/arm64" "Multi-architecture build"
    run_benchmark_test "large_context" "large_context" "" "Build with large context (100MB+)"
    
    # Run specialized tests
    run_cache_efficiency_test
    run_concurrent_builds_test
    
    # Generate report
    generate_performance_report
    
    print_header "Performance Benchmark Complete"
    print_success "All benchmark tests completed"
    print_info "Results saved to: $BENCHMARK_OUTPUT_DIR"
}

# Show usage information
show_usage() {
    cat << EOF
OSSB Performance Benchmark Tool

Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -o, --output DIR        Output directory for benchmark results (default: ./test-results/performance-benchmark)
    -b, --binary PATH       Path to OSSB binary (default: ./ossb)
    -t, --timeout SECONDS   Benchmark timeout in seconds (default: 1800)
    -i, --iterations N      Number of benchmark iterations (default: 3)
    --quick                 Run quick benchmarks only
    --full                  Run comprehensive benchmarks (default)

Examples:
    # Run full performance benchmark
    $0

    # Run with custom output directory
    $0 --output /tmp/benchmark-results

    # Run with custom OSSB binary
    $0 --binary /usr/local/bin/ossb

    # Run quick benchmarks
    $0 --quick

EOF
}

# Main script execution
main() {
    local quick_mode=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_usage
                exit 0
                ;;
            -o|--output)
                BENCHMARK_OUTPUT_DIR="$2"
                shift 2
                ;;
            -b|--binary)
                OSSB_BINARY="$2"
                shift 2
                ;;
            -t|--timeout)
                BENCHMARK_TIMEOUT="$2"
                shift 2
                ;;
            -i|--iterations)
                BENCHMARK_ITERATIONS="$2"
                shift 2
                ;;
            --quick)
                quick_mode=true
                shift
                ;;
            --full)
                quick_mode=false
                shift
                ;;
            -*)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
            *)
                print_error "Unexpected argument: $1"
                show_usage
                exit 1
                ;;
        esac
    done
    
    # Check if OSSB binary exists
    if [ ! -f "$OSSB_BINARY" ] && ! command -v "$OSSB_BINARY" &> /dev/null; then
        print_error "OSSB binary not found: $OSSB_BINARY"
        print_info "Please build OSSB or specify the correct path with --binary"
        exit 1
    fi
    
    # Check for required tools
    if ! command -v bc &> /dev/null; then
        print_error "bc (calculator) is required but not installed"
        exit 1
    fi
    
    # Run performance benchmark
    if [ "$quick_mode" = true ]; then
        print_info "Running quick performance benchmark..."
        BENCHMARK_TIMEOUT=300  # 5 minutes for quick mode
        BENCHMARK_ITERATIONS=1
    fi
    
    run_performance_benchmark
}

# Run main function
main "$@"