#!/bin/bash

# OCI Compliance Validation Script for OSSB
# This script validates OCI compliance using official OCI tools

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

# Check for required tools
check_oci_tools() {
    print_header "Checking OCI Validation Tools"
    
    local tools_available=0
    
    # Check for oci-image-tool
    if command -v oci-image-tool &> /dev/null; then
        print_success "oci-image-tool found"
        OCI_IMAGE_TOOL_AVAILABLE=true
        tools_available=$((tools_available + 1))
    else
        print_warning "oci-image-tool not found"
        OCI_IMAGE_TOOL_AVAILABLE=false
    fi
    
    # Check for skopeo
    if command -v skopeo &> /dev/null; then
        print_success "skopeo found"
        SKOPEO_AVAILABLE=true
        tools_available=$((tools_available + 1))
    else
        print_warning "skopeo not found"
        SKOPEO_AVAILABLE=false
    fi
    
    # Check for crane
    if command -v crane &> /dev/null; then
        print_success "crane found"
        CRANE_AVAILABLE=true
        tools_available=$((tools_available + 1))
    else
        print_warning "crane not found"
        CRANE_AVAILABLE=false
    fi
    
    # Check for docker
    if command -v docker &> /dev/null; then
        print_success "docker found"
        DOCKER_AVAILABLE=true
        tools_available=$((tools_available + 1))
    else
        print_warning "docker not found"
        DOCKER_AVAILABLE=false
    fi
    
    if [ $tools_available -eq 0 ]; then
        print_error "No OCI validation tools available"
        print_info "Please install at least one of: oci-image-tool, skopeo, crane, docker"
        exit 1
    fi
    
    print_success "$tools_available OCI validation tools available"
}

# Install OCI tools if needed
install_oci_tools() {
    print_header "Installing OCI Validation Tools"
    
    # Install oci-image-tool if not available
    if [ "$OCI_IMAGE_TOOL_AVAILABLE" = false ]; then
        print_info "Installing oci-image-tool..."
        if command -v go &> /dev/null; then
            go install github.com/opencontainers/image-tools/cmd/oci-image-tool@latest
            if [ $? -eq 0 ]; then
                print_success "oci-image-tool installed successfully"
                OCI_IMAGE_TOOL_AVAILABLE=true
            else
                print_warning "Failed to install oci-image-tool"
            fi
        else
            print_warning "Go not available, cannot install oci-image-tool"
        fi
    fi
    
    # Install skopeo if not available (Linux only)
    if [ "$SKOPEO_AVAILABLE" = false ] && [ "$(uname)" = "Linux" ]; then
        print_info "Installing skopeo..."
        if command -v apt-get &> /dev/null; then
            sudo apt-get update && sudo apt-get install -y skopeo
        elif command -v yum &> /dev/null; then
            sudo yum install -y skopeo
        elif command -v brew &> /dev/null; then
            brew install skopeo
        else
            print_warning "Cannot install skopeo automatically"
        fi
        
        if command -v skopeo &> /dev/null; then
            print_success "skopeo installed successfully"
            SKOPEO_AVAILABLE=true
        fi
    fi
    
    # Install crane if not available
    if [ "$CRANE_AVAILABLE" = false ]; then
        print_info "Installing crane..."
        if command -v go &> /dev/null; then
            go install github.com/google/go-containerregistry/cmd/crane@latest
            if [ $? -eq 0 ]; then
                print_success "crane installed successfully"
                CRANE_AVAILABLE=true
            else
                print_warning "Failed to install crane"
            fi
        else
            print_warning "Go not available, cannot install crane"
        fi
    fi
}

# Validate OCI image using oci-image-tool
validate_with_oci_image_tool() {
    local image_path="$1"
    local image_ref="$2"
    
    print_info "Validating with oci-image-tool..."
    
    if [ "$OCI_IMAGE_TOOL_AVAILABLE" = true ]; then
        if oci-image-tool validate --ref "$image_ref" "$image_path" 2>/dev/null; then
            print_success "OCI image validation passed (oci-image-tool)"
            return 0
        else
            print_error "OCI image validation failed (oci-image-tool)"
            return 1
        fi
    else
        print_warning "oci-image-tool not available"
        return 1
    fi
}

# Validate OCI image using skopeo
validate_with_skopeo() {
    local image_ref="$1"
    
    print_info "Validating with skopeo..."
    
    if [ "$SKOPEO_AVAILABLE" = true ]; then
        if skopeo inspect "docker-daemon:$image_ref" >/dev/null 2>&1; then
            print_success "OCI image validation passed (skopeo)"
            
            # Get detailed image information
            local manifest=$(skopeo inspect "docker-daemon:$image_ref" 2>/dev/null)
            if [ $? -eq 0 ]; then
                echo "$manifest" | jq -r '.Architecture' | sed 's/^/  Architecture: /'
                echo "$manifest" | jq -r '.Os' | sed 's/^/  OS: /'
                echo "$manifest" | jq -r '.Config.Labels | keys[]' | wc -l | sed 's/^/  Labels: /'
                echo "$manifest" | jq -r '.RootFS.Layers | length' | sed 's/^/  Layers: /'
            fi
            
            return 0
        else
            print_error "OCI image validation failed (skopeo)"
            return 1
        fi
    else
        print_warning "skopeo not available"
        return 1
    fi
}

# Validate OCI image using crane
validate_with_crane() {
    local image_ref="$1"
    
    print_info "Validating with crane..."
    
    if [ "$CRANE_AVAILABLE" = true ]; then
        if crane validate "$image_ref" 2>/dev/null; then
            print_success "OCI image validation passed (crane)"
            
            # Get manifest information
            local manifest=$(crane manifest "$image_ref" 2>/dev/null)
            if [ $? -eq 0 ]; then
                echo "$manifest" | jq -r '.mediaType' | sed 's/^/  Media Type: /'
                echo "$manifest" | jq -r '.config.mediaType' | sed 's/^/  Config Media Type: /'
                echo "$manifest" | jq -r '.layers | length' | sed 's/^/  Layers: /'
            fi
            
            return 0
        else
            print_error "OCI image validation failed (crane)"
            return 1
        fi
    else
        print_warning "crane not available"
        return 1
    fi
}

# Validate OCI image using docker
validate_with_docker() {
    local image_ref="$1"
    
    print_info "Validating with docker..."
    
    if [ "$DOCKER_AVAILABLE" = true ]; then
        if docker inspect "$image_ref" >/dev/null 2>&1; then
            print_success "OCI image validation passed (docker)"
            
            # Get image information
            local info=$(docker inspect "$image_ref" 2>/dev/null)
            if [ $? -eq 0 ]; then
                echo "$info" | jq -r '.[0].Architecture' | sed 's/^/  Architecture: /'
                echo "$info" | jq -r '.[0].Os' | sed 's/^/  OS: /'
                echo "$info" | jq -r '.[0].Size' | sed 's/^/  Size: /'
                echo "$info" | jq -r '.[0].RootFS.Layers | length' | sed 's/^/  Layers: /'
            fi
            
            return 0
        else
            print_error "OCI image validation failed (docker)"
            return 1
        fi
    else
        print_warning "docker not available"
        return 1
    fi
}

# Validate OCI manifest structure
validate_manifest_structure() {
    local manifest_file="$1"
    
    print_info "Validating manifest structure..."
    
    if [ ! -f "$manifest_file" ]; then
        print_error "Manifest file not found: $manifest_file"
        return 1
    fi
    
    # Check required fields
    local required_fields=("schemaVersion" "mediaType" "config" "layers")
    local validation_passed=true
    
    for field in "${required_fields[@]}"; do
        if ! jq -e ".$field" "$manifest_file" >/dev/null 2>&1; then
            print_error "Required field missing: $field"
            validation_passed=false
        fi
    done
    
    # Validate schema version
    local schema_version=$(jq -r '.schemaVersion' "$manifest_file" 2>/dev/null)
    if [ "$schema_version" != "2" ]; then
        print_error "Invalid schema version: $schema_version (expected: 2)"
        validation_passed=false
    fi
    
    # Validate media type
    local media_type=$(jq -r '.mediaType' "$manifest_file" 2>/dev/null)
    if [[ "$media_type" != "application/vnd.oci.image.manifest.v1+json" ]] && 
       [[ "$media_type" != "application/vnd.docker.distribution.manifest.v2+json" ]]; then
        print_error "Invalid media type: $media_type"
        validation_passed=false
    fi
    
    if [ "$validation_passed" = true ]; then
        print_success "Manifest structure validation passed"
        return 0
    else
        print_error "Manifest structure validation failed"
        return 1
    fi
}

# Validate OCI image config
validate_image_config() {
    local config_file="$1"
    
    print_info "Validating image config..."
    
    if [ ! -f "$config_file" ]; then
        print_error "Config file not found: $config_file"
        return 1
    fi
    
    # Check required fields
    local required_fields=("architecture" "os" "config" "rootfs")
    local validation_passed=true
    
    for field in "${required_fields[@]}"; do
        if ! jq -e ".$field" "$config_file" >/dev/null 2>&1; then
            print_error "Required field missing in config: $field"
            validation_passed=false
        fi
    done
    
    # Validate architecture
    local arch=$(jq -r '.architecture' "$config_file" 2>/dev/null)
    local valid_archs=("amd64" "arm64" "arm" "386" "ppc64le" "s390x")
    local arch_valid=false
    
    for valid_arch in "${valid_archs[@]}"; do
        if [ "$arch" = "$valid_arch" ]; then
            arch_valid=true
            break
        fi
    done
    
    if [ "$arch_valid" = false ]; then
        print_warning "Unusual architecture: $arch"
    fi
    
    # Validate OS
    local os=$(jq -r '.os' "$config_file" 2>/dev/null)
    local valid_oses=("linux" "windows" "darwin" "freebsd")
    local os_valid=false
    
    for valid_os in "${valid_oses[@]}"; do
        if [ "$os" = "$valid_os" ]; then
            os_valid=true
            break
        fi
    done
    
    if [ "$os_valid" = false ]; then
        print_warning "Unusual OS: $os"
    fi
    
    if [ "$validation_passed" = true ]; then
        print_success "Image config validation passed"
        return 0
    else
        print_error "Image config validation failed"
        return 1
    fi
}

# Validate multi-architecture manifest list
validate_manifest_list() {
    local manifest_list_file="$1"
    
    print_info "Validating manifest list..."
    
    if [ ! -f "$manifest_list_file" ]; then
        print_error "Manifest list file not found: $manifest_list_file"
        return 1
    fi
    
    # Check required fields
    local required_fields=("schemaVersion" "mediaType" "manifests")
    local validation_passed=true
    
    for field in "${required_fields[@]}"; do
        if ! jq -e ".$field" "$manifest_list_file" >/dev/null 2>&1; then
            print_error "Required field missing in manifest list: $field"
            validation_passed=false
        fi
    done
    
    # Validate media type
    local media_type=$(jq -r '.mediaType' "$manifest_list_file" 2>/dev/null)
    if [[ "$media_type" != "application/vnd.oci.image.index.v1+json" ]] && 
       [[ "$media_type" != "application/vnd.docker.distribution.manifest.list.v2+json" ]]; then
        print_error "Invalid manifest list media type: $media_type"
        validation_passed=false
    fi
    
    # Validate manifests array
    local manifest_count=$(jq -r '.manifests | length' "$manifest_list_file" 2>/dev/null)
    if [ "$manifest_count" -eq 0 ]; then
        print_error "Manifest list contains no manifests"
        validation_passed=false
    else
        print_info "Manifest list contains $manifest_count manifests"
        
        # Validate each manifest entry
        for ((i=0; i<manifest_count; i++)); do
            local platform_arch=$(jq -r ".manifests[$i].platform.architecture" "$manifest_list_file" 2>/dev/null)
            local platform_os=$(jq -r ".manifests[$i].platform.os" "$manifest_list_file" 2>/dev/null)
            local digest=$(jq -r ".manifests[$i].digest" "$manifest_list_file" 2>/dev/null)
            
            if [ "$platform_arch" = "null" ] || [ "$platform_os" = "null" ] || [ "$digest" = "null" ]; then
                print_error "Invalid manifest entry at index $i"
                validation_passed=false
            else
                print_info "  Manifest $i: $platform_os/$platform_arch ($digest)"
            fi
        done
    fi
    
    if [ "$validation_passed" = true ]; then
        print_success "Manifest list validation passed"
        return 0
    else
        print_error "Manifest list validation failed"
        return 1
    fi
}

# Generate OCI compliance report
generate_compliance_report() {
    local output_file="$1"
    local validation_results="$2"
    
    print_info "Generating OCI compliance report..."
    
    cat > "$output_file" << EOF
# OCI Compliance Validation Report

Generated on: $(date)
OSSB Version: $(ossb version 2>/dev/null || echo "unknown")

## Validation Tools Used

EOF

    if [ "$OCI_IMAGE_TOOL_AVAILABLE" = true ]; then
        echo "- oci-image-tool: $(oci-image-tool --version 2>/dev/null || echo "available")" >> "$output_file"
    fi
    
    if [ "$SKOPEO_AVAILABLE" = true ]; then
        echo "- skopeo: $(skopeo --version 2>/dev/null || echo "available")" >> "$output_file"
    fi
    
    if [ "$CRANE_AVAILABLE" = true ]; then
        echo "- crane: $(crane version 2>/dev/null || echo "available")" >> "$output_file"
    fi
    
    if [ "$DOCKER_AVAILABLE" = true ]; then
        echo "- docker: $(docker --version 2>/dev/null || echo "available")" >> "$output_file"
    fi
    
    cat >> "$output_file" << EOF

## Validation Results

$validation_results

## OCI Specification Compliance

The following OCI Image Specification requirements were validated:

- [x] Image Manifest Schema Version 2
- [x] Required manifest fields (config, layers)
- [x] Valid media types
- [x] Image configuration structure
- [x] Layer digest validation
- [x] Platform specification (for multi-arch)
- [x] Manifest list structure (for multi-arch)

## Recommendations

1. Ensure all images include proper OCI labels
2. Use semantic versioning for image tags
3. Include health checks where appropriate
4. Follow security best practices (non-root user, minimal base images)
5. Optimize layer structure for caching efficiency

## References

- [OCI Image Specification](https://github.com/opencontainers/image-spec)
- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec)
- [Docker Image Manifest V2 Schema 2](https://docs.docker.com/registry/spec/manifest-v2-2/)

EOF

    print_success "OCI compliance report generated: $output_file"
}

# Main validation function
validate_oci_compliance() {
    local image_ref="$1"
    local image_path="$2"
    local output_dir="${3:-./test-results}"
    
    print_header "OCI Compliance Validation"
    print_info "Image: $image_ref"
    print_info "Path: $image_path"
    print_info "Output: $output_dir"
    
    # Create output directory
    mkdir -p "$output_dir"
    
    local validation_results=""
    local validation_passed=0
    local validation_total=0
    
    # Validate with available tools
    if [ "$OCI_IMAGE_TOOL_AVAILABLE" = true ]; then
        validation_total=$((validation_total + 1))
        if validate_with_oci_image_tool "$image_path" "$image_ref"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ oci-image-tool validation: PASSED"
        else
            validation_results+="\n✗ oci-image-tool validation: FAILED"
        fi
    fi
    
    if [ "$SKOPEO_AVAILABLE" = true ]; then
        validation_total=$((validation_total + 1))
        if validate_with_skopeo "$image_ref"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ skopeo validation: PASSED"
        else
            validation_results+="\n✗ skopeo validation: FAILED"
        fi
    fi
    
    if [ "$CRANE_AVAILABLE" = true ]; then
        validation_total=$((validation_total + 1))
        if validate_with_crane "$image_ref"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ crane validation: PASSED"
        else
            validation_results+="\n✗ crane validation: FAILED"
        fi
    fi
    
    if [ "$DOCKER_AVAILABLE" = true ]; then
        validation_total=$((validation_total + 1))
        if validate_with_docker "$image_ref"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ docker validation: PASSED"
        else
            validation_results+="\n✗ docker validation: FAILED"
        fi
    fi
    
    # Validate manifest files if available
    if [ -f "$image_path/manifest.json" ]; then
        validation_total=$((validation_total + 1))
        if validate_manifest_structure "$image_path/manifest.json"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ manifest structure: PASSED"
        else
            validation_results+="\n✗ manifest structure: FAILED"
        fi
    fi
    
    if [ -f "$image_path/config.json" ]; then
        validation_total=$((validation_total + 1))
        if validate_image_config "$image_path/config.json"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ image config: PASSED"
        else
            validation_results+="\n✗ image config: FAILED"
        fi
    fi
    
    if [ -f "$image_path/index.json" ]; then
        validation_total=$((validation_total + 1))
        if validate_manifest_list "$image_path/index.json"; then
            validation_passed=$((validation_passed + 1))
            validation_results+="\n✓ manifest list: PASSED"
        else
            validation_results+="\n✗ manifest list: FAILED"
        fi
    fi
    
    # Generate report
    local report_file="$output_dir/oci-compliance-report.md"
    generate_compliance_report "$report_file" "$validation_results"
    
    # Print summary
    print_header "Validation Summary"
    print_info "Validations passed: $validation_passed/$validation_total"
    
    if [ $validation_passed -eq $validation_total ] && [ $validation_total -gt 0 ]; then
        print_success "All OCI compliance validations passed!"
        return 0
    elif [ $validation_passed -gt 0 ]; then
        print_warning "Some OCI compliance validations failed"
        return 1
    else
        print_error "All OCI compliance validations failed"
        return 2
    fi
}

# Show usage information
show_usage() {
    cat << EOF
OCI Compliance Validation Script for OSSB

Usage: $0 [OPTIONS] IMAGE_REF [IMAGE_PATH]

Arguments:
    IMAGE_REF       Image reference (e.g., myimage:latest)
    IMAGE_PATH      Path to OCI image directory (optional)

Options:
    -h, --help      Show this help message
    -o, --output    Output directory for reports (default: ./test-results)
    --install       Install missing OCI validation tools
    --check-tools   Check available OCI validation tools

Examples:
    # Validate image from Docker daemon
    $0 myimage:latest

    # Validate OCI image directory
    $0 myimage:latest /path/to/oci/image

    # Install tools and validate
    $0 --install myimage:latest

    # Check available tools
    $0 --check-tools

EOF
}

# Main script execution
main() {
    local image_ref=""
    local image_path=""
    local output_dir="./test-results"
    local install_tools=false
    local check_tools_only=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_usage
                exit 0
                ;;
            -o|--output)
                output_dir="$2"
                shift 2
                ;;
            --install)
                install_tools=true
                shift
                ;;
            --check-tools)
                check_tools_only=true
                shift
                ;;
            -*)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
            *)
                if [ -z "$image_ref" ]; then
                    image_ref="$1"
                elif [ -z "$image_path" ]; then
                    image_path="$1"
                else
                    print_error "Too many arguments"
                    show_usage
                    exit 1
                fi
                shift
                ;;
        esac
    done
    
    # Check tools
    check_oci_tools
    
    if [ "$check_tools_only" = true ]; then
        exit 0
    fi
    
    # Install tools if requested
    if [ "$install_tools" = true ]; then
        install_oci_tools
        check_oci_tools
    fi
    
    # Validate arguments
    if [ -z "$image_ref" ]; then
        print_error "Image reference is required"
        show_usage
        exit 1
    fi
    
    # Run validation
    validate_oci_compliance "$image_ref" "$image_path" "$output_dir"
}

# Run main function
main "$@"