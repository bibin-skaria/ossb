#!/bin/bash

# Build and Save OSSB Image Script
# This script builds an image and saves it before OSSB cleans up

set -e

IMAGE_NAME="mel-test-final"
OUTPUT_DIR="./image-output"

echo "🚀 Building image: $IMAGE_NAME"
echo "📁 Output directory: $OUTPUT_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build the image and capture the output path
BUILD_OUTPUT=$(./ossb build --output tar --tag "$IMAGE_NAME:latest" test-dockerfile/ 2>&1)

echo "📋 Build output:"
echo "$BUILD_OUTPUT"

# Extract the output path from the build log
OUTPUT_PATH=$(echo "$BUILD_OUTPUT" | grep "Output:" | sed 's/Output: //')

echo "📦 Attempting to copy from: $OUTPUT_PATH"

# Try to copy the file immediately
if [ -n "$OUTPUT_PATH" ] && [ -f "$OUTPUT_PATH" ]; then
    cp "$OUTPUT_PATH" "$OUTPUT_DIR/$IMAGE_NAME.tar"
    echo "✅ Image saved to: $OUTPUT_DIR/$IMAGE_NAME.tar"
    ls -la "$OUTPUT_DIR/$IMAGE_NAME.tar"
else
    echo "❌ Could not find output file at: $OUTPUT_PATH"
    
    # Try to find any tar files in the cache
    echo "🔍 Searching for tar files in cache..."
    find ~/.ossb -name "*.tar" -type f 2>/dev/null | head -5
    
    # Try to find the build directory
    echo "🔍 Searching for build directories..."
    find ~/.ossb -name "build-*" -type d 2>/dev/null | head -5
fi

echo "🏁 Script completed"