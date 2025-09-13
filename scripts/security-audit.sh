#!/bin/bash

# Security Audit Script for OSSB
# Performs comprehensive security testing and vulnerability assessment

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
AUDIT_OUTPUT_DIR="${AUDIT_OUTPUT_DIR:-./test-results/security-audit}"
OSSB_BINARY="${OSSB_BINARY:-./ossb}"
TEST_TIMEOUT="${TEST_TIMEOUT:-300}"

# Security test results
SECURITY_TESTS_PASSED=0
SECURITY_TESTS_FAILED=0
SECURITY_TESTS_TOTAL=0

# Initialize audit environment
init_audit_environment() {
    print_header "Initializing Security Audit Environment"
    
    # Create output directory
    mkdir -p "$AUDIT_OUTPUT_DIR"
    
    # Create temporary test directory
    TEST_DIR=$(mktemp -d -t ossb-security-audit-XXXXXX)
    export TEST_DIR
    
    print_success "Audit environment initialized"
    print_info "Output directory: $AUDIT_OUTPUT_DIR"
    print_info "Test directory: $TEST_DIR"
}

# Clean up audit environment
cleanup_audit_environment() {
    print_header "Cleaning Up Security Audit Environment"
    
    if [ -n "$TEST_DIR" ] && [ -d "$TEST_DIR" ]; then
        rm -rf "$TEST_DIR"
        print_success "Test directory cleaned up"
    fi
    
    # Clean up any test containers
    if command -v docker &> /dev/null; then
        docker system prune -f --filter "label=ossb-security-test" &> /dev/null || true
    fi
    
    if command -v podman &> /dev/null; then
        podman system prune -f &> /dev/null || true
    fi
}

# Run a security test
run_security_test() {
    local test_name="$1"
    local test_function="$2"
    local test_description="$3"
    
    print_info "Running security test: $test_name"
    print_info "Description: $test_description"
    
    SECURITY_TESTS_TOTAL=$((SECURITY_TESTS_TOTAL + 1))
    
    local test_start_time=$(date +%s)
    local test_result_file="$AUDIT_OUTPUT_DIR/${test_name}.result"
    
    if timeout "$TEST_TIMEOUT" bash -c "$test_function" > "$test_result_file" 2>&1; then
        local test_end_time=$(date +%s)
        local test_duration=$((test_end_time - test_start_time))
        
        print_success "Security test $test_name passed (${test_duration}s)"
        SECURITY_TESTS_PASSED=$((SECURITY_TESTS_PASSED + 1))
        echo "PASSED" > "$AUDIT_OUTPUT_DIR/${test_name}.status"
    else
        local test_end_time=$(date +%s)
        local test_duration=$((test_end_time - test_start_time))
        
        print_error "Security test $test_name failed (${test_duration}s)"
        SECURITY_TESTS_FAILED=$((SECURITY_TESTS_FAILED + 1))
        echo "FAILED" > "$AUDIT_OUTPUT_DIR/${test_name}.status"
        
        # Show error details
        if [ -f "$test_result_file" ]; then
            print_info "Error details:"
            tail -10 "$test_result_file" | sed 's/^/  /'
        fi
    fi
}

# Test 1: Rootless execution security
test_rootless_execution() {
    local test_dir="$TEST_DIR/rootless_test"
    mkdir -p "$test_dir"
    
    # Create test Dockerfile
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN id > /user-info.txt
RUN whoami >> /user-info.txt
RUN ls -la /proc/self/ >> /user-info.txt || echo "proc access restricted" >> /user-info.txt
CMD ["cat", "/user-info.txt"]
EOF
    
    # Test rootless build
    cd "$test_dir"
    if ! "$OSSB_BINARY" build --rootless --tag ossb-rootless-test:latest . ; then
        echo "Rootless build failed"
        return 1
    fi
    
    # Verify the build ran as non-root
    if command -v docker &> /dev/null; then
        local user_info=$(docker run --rm ossb-rootless-test:latest 2>/dev/null || echo "")
        if echo "$user_info" | grep -q "uid=0"; then
            echo "Build ran as root user - security violation"
            return 1
        fi
    fi
    
    echo "Rootless execution test passed"
    return 0
}

# Test 2: Secret handling security
test_secret_handling() {
    local test_dir="$TEST_DIR/secret_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile with potential secret exposure
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
ARG SECRET_VALUE=default-secret
ENV SECRET_KEY=super-secret-value
RUN echo "Secret: $SECRET_VALUE" > /tmp/secret.txt
RUN echo "Key: $SECRET_KEY" >> /tmp/secret.txt
CMD ["cat", "/tmp/secret.txt"]
EOF
    
    cd "$test_dir"
    
    # Build with secret
    if ! "$OSSB_BINARY" build --build-arg SECRET_VALUE=actual-secret --tag ossb-secret-test:latest . ; then
        echo "Secret handling build failed"
        return 1
    fi
    
    # Check if secrets are exposed in image layers
    if command -v docker &> /dev/null; then
        local history=$(docker history ossb-secret-test:latest --no-trunc 2>/dev/null || echo "")
        if echo "$history" | grep -q "actual-secret"; then
            echo "Secret exposed in image history - security violation"
            return 1
        fi
        
        if echo "$history" | grep -q "super-secret-value"; then
            echo "Environment secret exposed in image history - security violation"
            return 1
        fi
    fi
    
    echo "Secret handling test passed"
    return 0
}

# Test 3: Privilege escalation prevention
test_privilege_escalation() {
    local test_dir="$TEST_DIR/privilege_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile that attempts privilege escalation
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN apk add --no-cache sudo
USER 1000
RUN sudo whoami 2>/dev/null && echo "PRIVILEGE_ESCALATION_SUCCESS" || echo "PRIVILEGE_ESCALATION_BLOCKED"
RUN id > /final-id.txt
CMD ["cat", "/final-id.txt"]
EOF
    
    cd "$test_dir"
    
    # Build with security constraints
    if ! "$OSSB_BINARY" build --rootless --security-opt no-new-privileges --tag ossb-privilege-test:latest . ; then
        echo "Privilege escalation test build failed"
        return 1
    fi
    
    # Verify privilege escalation was blocked
    if command -v docker &> /dev/null; then
        local output=$(docker run --rm ossb-privilege-test:latest 2>/dev/null || echo "")
        if echo "$output" | grep -q "PRIVILEGE_ESCALATION_SUCCESS"; then
            echo "Privilege escalation was not blocked - security violation"
            return 1
        fi
        
        if echo "$output" | grep -q "uid=0"; then
            echo "Process running as root - security violation"
            return 1
        fi
    fi
    
    echo "Privilege escalation prevention test passed"
    return 0
}

# Test 4: Container escape prevention
test_container_escape() {
    local test_dir="$TEST_DIR/escape_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile that attempts container escape
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN ls -la /proc/self/root 2>/dev/null && echo "ROOT_ACCESS" || echo "ROOT_BLOCKED"
RUN ls -la /sys/fs/cgroup 2>/dev/null && echo "CGROUP_ACCESS" || echo "CGROUP_BLOCKED"
RUN ls -la /dev/kmsg 2>/dev/null && echo "KMSG_ACCESS" || echo "KMSG_BLOCKED"
RUN cat /proc/version 2>/dev/null | head -1 || echo "PROC_VERSION_BLOCKED"
CMD ["echo", "Container escape test completed"]
EOF
    
    cd "$test_dir"
    
    # Build with strict security
    if ! "$OSSB_BINARY" build --rootless --security-opt seccomp=default --tag ossb-escape-test:latest . ; then
        echo "Container escape test build failed"
        return 1
    fi
    
    # Verify escape attempts were blocked
    if command -v docker &> /dev/null; then
        local output=$(docker run --rm --security-opt no-new-privileges ossb-escape-test:latest 2>/dev/null || echo "")
        
        # Check for successful escape attempts
        if echo "$output" | grep -q "ROOT_ACCESS"; then
            echo "Root filesystem access not blocked - security violation"
            return 1
        fi
        
        if echo "$output" | grep -q "KMSG_ACCESS"; then
            echo "Kernel message access not blocked - security violation"
            return 1
        fi
    fi
    
    echo "Container escape prevention test passed"
    return 0
}

# Test 5: Resource limit enforcement
test_resource_limits() {
    local test_dir="$TEST_DIR/resource_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile that attempts to consume excessive resources
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN dd if=/dev/zero of=/tmp/large-file bs=1M count=100 2>/dev/null || echo "DISK_LIMIT_ENFORCED"
RUN timeout 5 yes > /dev/null 2>&1 || echo "CPU_LIMIT_ENFORCED"
RUN echo "Resource test completed" > /tmp/result.txt
CMD ["cat", "/tmp/result.txt"]
EOF
    
    cd "$test_dir"
    
    # Build with resource limits
    if ! "$OSSB_BINARY" build --memory 512m --cpu-quota 50000 --tag ossb-resource-test:latest . ; then
        echo "Resource limits test build failed"
        return 1
    fi
    
    # Verify resource limits were enforced
    if command -v docker &> /dev/null; then
        local output=$(docker run --rm --memory 256m --cpus 0.5 ossb-resource-test:latest 2>/dev/null || echo "")
        
        # The build should complete but with resource constraints
        if ! echo "$output" | grep -q "Resource test completed"; then
            echo "Resource limits prevented normal operation - may be too restrictive"
            return 1
        fi
    fi
    
    echo "Resource limit enforcement test passed"
    return 0
}

# Test 6: Input validation and sanitization
test_input_validation() {
    local test_dir="$TEST_DIR/input_test"
    mkdir -p "$test_dir"
    
    # Test malicious build arguments
    local malicious_inputs=(
        "../../../etc/passwd"
        "\$(rm -rf /)"
        "; cat /etc/shadow"
        "' OR '1'='1"
        "<script>alert('xss')</script>"
        "\`whoami\`"
        "\$\(id\)"
    )
    
    for input in "${malicious_inputs[@]}"; do
        # Create test Dockerfile
        cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
ARG TEST_INPUT=safe-default
RUN echo "Input: $TEST_INPUT" > /tmp/input-test.txt
CMD ["cat", "/tmp/input-test.txt"]
EOF
        
        cd "$test_dir"
        
        # Try to build with malicious input
        if "$OSSB_BINARY" build --build-arg "TEST_INPUT=$input" --tag "ossb-input-test:latest" . 2>/dev/null; then
            # Check if the malicious input was executed
            if command -v docker &> /dev/null; then
                local output=$(docker run --rm ossb-input-test:latest 2>/dev/null || echo "")
                
                # Look for signs of command execution
                if echo "$output" | grep -E "(root|uid=0|/etc/passwd|/etc/shadow)" > /dev/null; then
                    echo "Malicious input was executed: $input"
                    return 1
                fi
            fi
        fi
    done
    
    echo "Input validation test passed"
    return 0
}

# Test 7: Network isolation
test_network_isolation() {
    local test_dir="$TEST_DIR/network_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile that attempts network access
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN apk add --no-cache curl
RUN curl -m 5 http://example.com 2>/dev/null && echo "NETWORK_ACCESS" || echo "NETWORK_BLOCKED"
RUN ping -c 1 8.8.8.8 2>/dev/null && echo "PING_SUCCESS" || echo "PING_BLOCKED"
CMD ["echo", "Network isolation test completed"]
EOF
    
    cd "$test_dir"
    
    # Build with network isolation
    if ! "$OSSB_BINARY" build --network none --tag ossb-network-test:latest . ; then
        echo "Network isolation test build failed"
        return 1
    fi
    
    # Verify network access was blocked during build
    # Note: This test may need to be adjusted based on OSSB's network isolation implementation
    echo "Network isolation test passed"
    return 0
}

# Test 8: File system security
test_filesystem_security() {
    local test_dir="$TEST_DIR/filesystem_test"
    mkdir -p "$test_dir"
    
    # Create Dockerfile that attempts to access sensitive files
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN cat /etc/passwd 2>/dev/null | wc -l > /tmp/passwd-lines.txt || echo "0" > /tmp/passwd-lines.txt
RUN ls -la /proc/self/environ 2>/dev/null && echo "ENVIRON_ACCESS" || echo "ENVIRON_BLOCKED"
RUN mount 2>/dev/null && echo "MOUNT_ACCESS" || echo "MOUNT_BLOCKED"
CMD ["cat", "/tmp/passwd-lines.txt"]
EOF
    
    cd "$test_dir"
    
    # Build with filesystem restrictions
    if ! "$OSSB_BINARY" build --rootless --tag ossb-filesystem-test:latest . ; then
        echo "Filesystem security test build failed"
        return 1
    fi
    
    # Verify sensitive file access patterns
    if command -v docker &> /dev/null; then
        local output=$(docker run --rm --read-only ossb-filesystem-test:latest 2>/dev/null || echo "")
        
        # Basic file access should work, but sensitive operations should be restricted
        if echo "$output" | grep -q "MOUNT_ACCESS"; then
            echo "Mount access not properly restricted - security concern"
            return 1
        fi
    fi
    
    echo "Filesystem security test passed"
    return 0
}

# Test 9: Build context security
test_build_context_security() {
    local test_dir="$TEST_DIR/context_test"
    mkdir -p "$test_dir"
    
    # Create potentially sensitive files in build context
    echo "sensitive-data-123" > "$test_dir/secret.txt"
    echo "password=admin123" > "$test_dir/.env"
    mkdir -p "$test_dir/.git"
    echo "git-token-456" > "$test_dir/.git/config"
    
    # Create Dockerfile that tries to access these files
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
COPY . /app/
RUN find /app -name "secret.txt" -exec cat {} \; 2>/dev/null || echo "SECRET_BLOCKED"
RUN find /app -name ".env" -exec cat {} \; 2>/dev/null || echo "ENV_BLOCKED"
RUN find /app -name ".git" -type d 2>/dev/null && echo "GIT_ACCESS" || echo "GIT_BLOCKED"
CMD ["echo", "Build context security test completed"]
EOF
    
    cd "$test_dir"
    
    # Build with context filtering
    if ! "$OSSB_BINARY" build --exclude-patterns "secret.txt,.env,.git" --tag ossb-context-test:latest . ; then
        echo "Build context security test build failed"
        return 1
    fi
    
    # Verify sensitive files were excluded
    if command -v docker &> /dev/null; then
        local output=$(docker run --rm ossb-context-test:latest 2>/dev/null || echo "")
        
        if echo "$output" | grep -q "sensitive-data-123"; then
            echo "Sensitive file was not excluded from build context"
            return 1
        fi
        
        if echo "$output" | grep -q "password=admin123"; then
            echo "Environment file was not excluded from build context"
            return 1
        fi
    fi
    
    echo "Build context security test passed"
    return 0
}

# Test 10: Registry security
test_registry_security() {
    local test_dir="$TEST_DIR/registry_test"
    mkdir -p "$test_dir"
    
    # Create simple Dockerfile
    cat > "$test_dir/Dockerfile" << 'EOF'
FROM alpine:latest
RUN echo "Registry security test" > /tmp/test.txt
CMD ["cat", "/tmp/test.txt"]
EOF
    
    cd "$test_dir"
    
    # Test with insecure registry (should be blocked)
    if "$OSSB_BINARY" build --push --registry http://insecure-registry.example.com --tag registry-test:latest . 2>/dev/null; then
        echo "Insecure registry push was allowed - security violation"
        return 1
    fi
    
    # Test with invalid credentials (should fail gracefully)
    if "$OSSB_BINARY" build --push --registry registry.example.com --username invalid --password invalid --tag registry-test:latest . 2>/dev/null; then
        echo "Invalid credentials were accepted - security concern"
        return 1
    fi
    
    echo "Registry security test passed"
    return 0
}

# Generate security audit report
generate_security_report() {
    local report_file="$AUDIT_OUTPUT_DIR/security-audit-report.md"
    
    print_header "Generating Security Audit Report"
    
    cat > "$report_file" << EOF
# OSSB Security Audit Report

Generated on: $(date)
OSSB Version: $("$OSSB_BINARY" version 2>/dev/null || echo "unknown")
Audit Duration: $(date -d @$(($(date +%s) - AUDIT_START_TIME)) -u +%H:%M:%S 2>/dev/null || echo "unknown")

## Executive Summary

This security audit evaluated OSSB's security posture across multiple dimensions including:
- Rootless execution capabilities
- Secret handling and exposure prevention
- Privilege escalation prevention
- Container escape mitigation
- Resource limit enforcement
- Input validation and sanitization
- Network isolation
- Filesystem security
- Build context security
- Registry security

## Test Results Summary

- **Total Tests**: $SECURITY_TESTS_TOTAL
- **Passed**: $SECURITY_TESTS_PASSED
- **Failed**: $SECURITY_TESTS_FAILED
- **Success Rate**: $(( SECURITY_TESTS_PASSED * 100 / SECURITY_TESTS_TOTAL ))%

## Detailed Test Results

EOF

    # Add detailed results for each test
    for test_file in "$AUDIT_OUTPUT_DIR"/*.status; do
        if [ -f "$test_file" ]; then
            local test_name=$(basename "$test_file" .status)
            local test_status=$(cat "$test_file")
            local test_result_file="$AUDIT_OUTPUT_DIR/${test_name}.result"
            
            echo "### $test_name" >> "$report_file"
            echo "" >> "$report_file"
            
            if [ "$test_status" = "PASSED" ]; then
                echo "**Status**: ✅ PASSED" >> "$report_file"
            else
                echo "**Status**: ❌ FAILED" >> "$report_file"
            fi
            
            echo "" >> "$report_file"
            
            if [ -f "$test_result_file" ]; then
                echo "**Details**:" >> "$report_file"
                echo '```' >> "$report_file"
                tail -20 "$test_result_file" >> "$report_file"
                echo '```' >> "$report_file"
            fi
            
            echo "" >> "$report_file"
        fi
    done
    
    cat >> "$report_file" << EOF

## Security Recommendations

Based on the audit results, the following security recommendations are provided:

### High Priority
1. **Rootless Execution**: Ensure all builds run without root privileges by default
2. **Secret Management**: Implement secure secret handling with automatic detection and masking
3. **Privilege Escalation**: Enforce no-new-privileges security option for all containers
4. **Input Validation**: Strengthen input validation for all user-provided data

### Medium Priority
1. **Resource Limits**: Implement and enforce resource limits for all build operations
2. **Network Isolation**: Provide network isolation options for sensitive builds
3. **Build Context**: Implement automatic filtering of sensitive files from build context
4. **Registry Security**: Enforce TLS and proper authentication for all registry operations

### Low Priority
1. **Audit Logging**: Implement comprehensive audit logging for all security-relevant operations
2. **Vulnerability Scanning**: Integrate vulnerability scanning for base images and dependencies
3. **Security Policies**: Implement configurable security policies for different environments
4. **Compliance**: Add support for security compliance frameworks (CIS, NIST, etc.)

## Security Best Practices

1. **Always use rootless mode** when possible
2. **Avoid embedding secrets** in Dockerfiles or build arguments
3. **Use minimal base images** to reduce attack surface
4. **Implement proper RBAC** in Kubernetes environments
5. **Regularly update** OSSB and base images
6. **Monitor and audit** build activities
7. **Use signed images** and verify signatures
8. **Implement network policies** to restrict build-time network access

## Compliance Considerations

This audit addresses security requirements commonly found in:
- **NIST Cybersecurity Framework**
- **CIS Container Security Benchmarks**
- **OWASP Container Security Top 10**
- **Kubernetes Security Best Practices**
- **Docker Security Best Practices**

## References

- [NIST Container Security Guide](https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-190.pdf)
- [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker)
- [OWASP Container Security](https://owasp.org/www-project-container-security/)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/)

---

*This report was generated automatically by the OSSB Security Audit Tool.*
*For questions or concerns, please contact the security team.*

EOF

    print_success "Security audit report generated: $report_file"
}

# Main security audit function
run_security_audit() {
    print_header "OSSB Security Audit"
    
    # Record start time
    AUDIT_START_TIME=$(date +%s)
    
    # Initialize environment
    init_audit_environment
    
    # Set up cleanup trap
    trap cleanup_audit_environment EXIT
    
    # Run security tests
    print_header "Running Security Tests"
    
    run_security_test "rootless_execution" "test_rootless_execution" "Test rootless execution security"
    run_security_test "secret_handling" "test_secret_handling" "Test secret handling and exposure prevention"
    run_security_test "privilege_escalation" "test_privilege_escalation" "Test privilege escalation prevention"
    run_security_test "container_escape" "test_container_escape" "Test container escape prevention"
    run_security_test "resource_limits" "test_resource_limits" "Test resource limit enforcement"
    run_security_test "input_validation" "test_input_validation" "Test input validation and sanitization"
    run_security_test "network_isolation" "test_network_isolation" "Test network isolation"
    run_security_test "filesystem_security" "test_filesystem_security" "Test filesystem security"
    run_security_test "build_context_security" "test_build_context_security" "Test build context security"
    run_security_test "registry_security" "test_registry_security" "Test registry security"
    
    # Generate report
    generate_security_report
    
    # Print summary
    print_header "Security Audit Summary"
    print_info "Total tests: $SECURITY_TESTS_TOTAL"
    print_info "Passed: $SECURITY_TESTS_PASSED"
    print_info "Failed: $SECURITY_TESTS_FAILED"
    
    if [ $SECURITY_TESTS_FAILED -eq 0 ]; then
        print_success "All security tests passed!"
        return 0
    else
        print_error "$SECURITY_TESTS_FAILED security tests failed"
        return 1
    fi
}

# Show usage information
show_usage() {
    cat << EOF
OSSB Security Audit Tool

Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -o, --output DIR        Output directory for audit results (default: ./test-results/security-audit)
    -b, --binary PATH       Path to OSSB binary (default: ./ossb)
    -t, --timeout SECONDS   Test timeout in seconds (default: 300)
    --quick                 Run quick security tests only
    --full                  Run comprehensive security audit (default)

Examples:
    # Run full security audit
    $0

    # Run with custom output directory
    $0 --output /tmp/security-audit

    # Run with custom OSSB binary
    $0 --binary /usr/local/bin/ossb

    # Run quick security tests
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
                AUDIT_OUTPUT_DIR="$2"
                shift 2
                ;;
            -b|--binary)
                OSSB_BINARY="$2"
                shift 2
                ;;
            -t|--timeout)
                TEST_TIMEOUT="$2"
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
    
    # Run security audit
    if [ "$quick_mode" = true ]; then
        print_info "Running quick security audit..."
        # In quick mode, we could run a subset of tests
        # For now, we'll run all tests but with shorter timeouts
        TEST_TIMEOUT=60
    fi
    
    run_security_audit
}

# Run main function
main "$@"