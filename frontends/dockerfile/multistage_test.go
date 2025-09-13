package dockerfile

import (
	"testing"

	"github.com/bibin-skaria/ossb/internal/types"
)

func TestMultiStageDockerfile(t *testing.T) {
	tests := []struct {
		name           string
		dockerfile     string
		expectedStages int
		expectedDeps   map[string][]string
		expectError    bool
	}{
		{
			name: "simple multi-stage",
			dockerfile: `FROM alpine:3.18 AS builder
RUN apk add --no-cache gcc
COPY . /src
RUN gcc -o app /src/main.c

FROM alpine:3.18
COPY --from=builder /app /usr/local/bin/app
CMD ["/usr/local/bin/app"]`,
			expectedStages: 2,
			expectedDeps: map[string][]string{
				"builder":  {},
				"stage-1": {"builder"},
			},
			expectError: false,
		},
		{
			name: "three stage build",
			dockerfile: `FROM node:18 AS deps
COPY package*.json ./
RUN npm ci --only=production

FROM node:18 AS builder
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM alpine:3.18
RUN apk add --no-cache nodejs npm
COPY --from=deps /node_modules ./node_modules
COPY --from=builder /dist ./dist
CMD ["node", "dist/index.js"]`,
			expectedStages: 3,
			expectedDeps: map[string][]string{
				"deps":     {},
				"builder":  {},
				"stage-2": {"deps", "builder"},
			},
			expectError: false,
		},
		{
			name: "numeric stage reference",
			dockerfile: `FROM alpine:3.18 AS builder
RUN echo "building"

FROM alpine:3.18
COPY --from=0 /app /app
CMD ["/app"]`,
			expectedStages: 2,
			expectedDeps: map[string][]string{
				"builder":  {},
				"stage-1": {"builder"},
			},
			expectError: false,
		},
		{
			name: "stage from stage",
			dockerfile: `FROM alpine:3.18 AS base
RUN apk add --no-cache ca-certificates

FROM base AS builder
RUN apk add --no-cache gcc
COPY . /src
RUN gcc -o app /src/main.c

FROM base
COPY --from=builder /app /usr/local/bin/app
CMD ["/usr/local/bin/app"]`,
			expectedStages: 3,
			expectedDeps: map[string][]string{
				"base":     {},
				"builder":  {"base"},
				"stage-2": {"base", "builder"},
			},
			expectError: false,
		},
		{
			name: "invalid stage reference",
			dockerfile: `FROM alpine:3.18
COPY --from=nonexistent /app /app`,
			expectedStages: 1,
			expectedDeps:   map[string][]string{},
			expectError:    true,
		},
		{
			name: "no FROM instruction",
			dockerfile: `RUN echo "no from"`,
			expectedStages: 0,
			expectedDeps:   map[string][]string{},
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &types.BuildConfig{
				Context:    "/tmp/test",
				Dockerfile: "Dockerfile",
				BuildArgs:  make(map[string]string),
			}

			parser := &Parser{
				config:    config,
				buildArgs: config.BuildArgs,
			}

			operations, err := parser.Parse(tt.dockerfile)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if parser.multiStageContext == nil {
				t.Errorf("multi-stage context not initialized")
				return
			}

			if len(parser.multiStageContext.Stages) != tt.expectedStages {
				t.Errorf("expected %d stages, got %d", tt.expectedStages, len(parser.multiStageContext.Stages))
			}

			// Check stage dependencies
			for stageName, expectedDeps := range tt.expectedDeps {
				stage, exists := parser.multiStageContext.StagesByName[stageName]
				if !exists {
					t.Errorf("stage %s not found", stageName)
					continue
				}

				if len(stage.Dependencies) != len(expectedDeps) {
					t.Errorf("stage %s: expected %d dependencies, got %d", stageName, len(expectedDeps), len(stage.Dependencies))
					continue
				}

				for _, expectedDep := range expectedDeps {
					found := false
					for _, actualDep := range stage.Dependencies {
						if actualDep == expectedDep {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("stage %s: missing dependency %s", stageName, expectedDep)
					}
				}
			}

			// Verify operations were generated
			if len(operations) == 0 {
				t.Errorf("no operations generated")
			}

			// Check that final stage is marked correctly
			if parser.multiStageContext.FinalStage == nil {
				t.Errorf("final stage not set")
			} else if !parser.multiStageContext.FinalStage.IsFinal {
				t.Errorf("final stage not marked as final")
			}
		})
	}
}

func TestCopyFromStageInstruction(t *testing.T) {
	tests := []struct {
		name           string
		instruction    string
		expectedFrom   string
		expectedClean  string
	}{
		{
			name:          "copy with from stage",
			instruction:   "--from=builder /app /usr/local/bin/app",
			expectedFrom:  "builder",
			expectedClean: "/app /usr/local/bin/app",
		},
		{
			name:          "copy with numeric from",
			instruction:   "--from=0 /app /app",
			expectedFrom:  "0",
			expectedClean: "/app /app",
		},
		{
			name:          "copy without from",
			instruction:   "/src /dst",
			expectedFrom:  "",
			expectedClean: "/src /dst",
		},
		{
			name:          "copy with from and multiple sources",
			instruction:   "--from=builder /app /lib /usr/local/",
			expectedFrom:  "builder",
			expectedClean: "/app /lib /usr/local/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &Parser{}
			fromStage, cleanValue := parser.parseFromFlag(tt.instruction)

			if fromStage != tt.expectedFrom {
				t.Errorf("expected from stage '%s', got '%s'", tt.expectedFrom, fromStage)
			}

			if cleanValue != tt.expectedClean {
				t.Errorf("expected clean value '%s', got '%s'", tt.expectedClean, cleanValue)
			}
		})
	}
}

func TestStageOperationMetadata(t *testing.T) {
	dockerfile := `FROM alpine:3.18 AS builder
RUN echo "building"

FROM alpine:3.18
COPY --from=builder /app /app`

	config := &types.BuildConfig{
		Context:    "/tmp/test",
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that operations have stage metadata
	foundStageMetadata := false
	for _, op := range operations {
		if op.Metadata != nil {
			if stage, exists := op.Metadata["stage"]; exists && stage != "" {
				foundStageMetadata = true
				
				// Check for other expected metadata
				if _, exists := op.Metadata["stage_index"]; !exists {
					t.Errorf("operation missing stage_index metadata")
				}
				if _, exists := op.Metadata["is_final"]; !exists {
					t.Errorf("operation missing is_final metadata")
				}
			}
		}
	}

	if !foundStageMetadata {
		t.Errorf("no operations found with stage metadata")
	}
}

func TestComplexMultiStageWithDependencies(t *testing.T) {
	dockerfile := `# Base stage with common dependencies
FROM alpine:3.18 AS base
RUN apk add --no-cache ca-certificates tzdata

# Build dependencies
FROM base AS build-deps
RUN apk add --no-cache gcc musl-dev

# Application builder
FROM build-deps AS app-builder
COPY src/ /src/
WORKDIR /src
RUN gcc -static -o app main.c

# Asset builder
FROM node:18-alpine AS asset-builder
COPY package*.json ./
RUN npm ci
COPY assets/ ./assets/
RUN npm run build

# Final runtime image
FROM base
COPY --from=app-builder /src/app /usr/local/bin/app
COPY --from=asset-builder /dist /var/www/html
EXPOSE 8080
CMD ["/usr/local/bin/app"]`

	config := &types.BuildConfig{
		Context:    "/tmp/test",
		Dockerfile: "Dockerfile",
		BuildArgs:  make(map[string]string),
	}

	parser := &Parser{
		config:    config,
		buildArgs: config.BuildArgs,
	}

	operations, err := parser.Parse(dockerfile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 5 stages
	if len(parser.multiStageContext.Stages) != 5 {
		t.Errorf("expected 5 stages, got %d", len(parser.multiStageContext.Stages))
	}

	// Check final stage dependencies
	finalStage := parser.multiStageContext.FinalStage
	if finalStage == nil {
		t.Fatalf("final stage not found")
	}

	expectedDeps := []string{"base", "app-builder", "asset-builder"}
	if len(finalStage.Dependencies) != len(expectedDeps) {
		t.Errorf("final stage: expected %d dependencies, got %d", len(expectedDeps), len(finalStage.Dependencies))
	}

	for _, expectedDep := range expectedDeps {
		found := false
		for _, actualDep := range finalStage.Dependencies {
			if actualDep == expectedDep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("final stage missing dependency: %s", expectedDep)
		}
	}

	// Verify operations were generated
	if len(operations) == 0 {
		t.Errorf("no operations generated")
	}

	// Check that we have operations from multiple stages
	stagesInOps := make(map[string]bool)
	for _, op := range operations {
		if op.Metadata != nil {
			if stage, exists := op.Metadata["stage"]; exists {
				stagesInOps[stage] = true
			}
		}
	}

	if len(stagesInOps) < 2 {
		t.Errorf("expected operations from multiple stages, got stages: %v", stagesInOps)
	}
}