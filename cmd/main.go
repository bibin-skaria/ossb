package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bibin-skaria/ossb/engine"
	_ "github.com/bibin-skaria/ossb/executors"
	_ "github.com/bibin-skaria/ossb/exporters"
	_ "github.com/bibin-skaria/ossb/frontends/dockerfile"
	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/k8s"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ossb",
		Short: "Open Source Slim Builder - A monolithic container builder",
		Long: `OSSB is a monolithic container builder inspired by BuildKit but designed 
as a single binary with no daemon dependency. It features content-addressable 
caching, pluggable frontends, executors, and exporters.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildDate),
	}

	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newCacheCommand())

	return cmd
}

func newBuildCommand() *cobra.Command {
	var (
		dockerfile string
		tags       []string
		output     string
		frontend   string
		cacheDir   string
		noCache    bool
		progress   bool
		buildArgs  []string
		platforms  []string
		push       bool
		registry   string
		executor   string
		rootless   bool
	)

	cmd := &cobra.Command{
		Use:   "build [context]",
		Short: "Build an image from a Dockerfile",
		Long: `Build a container image from a Dockerfile. The context should be the path 
to the directory containing the Dockerfile and any files referenced by it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Generate build ID
			buildID := fmt.Sprintf("ossb-build-%d", time.Now().Unix())
			
			// Initialize Kubernetes integration if running in Kubernetes
			var jobManager *k8s.JobLifecycleManager
			k8sIntegration := k8s.NewKubernetesIntegration()
			
			if k8sIntegration.IsRunningInKubernetes() {
				jobManager = k8s.NewJobLifecycleManager(buildID)
				fmt.Printf("Running in Kubernetes environment\n")
			}

			ctx := context.Background()
			
			// Start job lifecycle if in Kubernetes
			if jobManager != nil {
				var err error
				ctx, err = jobManager.Start(ctx)
				if err != nil {
					return fmt.Errorf("failed to start job lifecycle: %v", err)
				}
				
				// Add cleanup for builder
				defer func() {
					if jobManager != nil {
						// This will be called by Complete/Fail methods
					}
				}()
			}

			context := "."
			if len(args) > 0 {
				context = args[0]
			}

			// Handle build context mounting in Kubernetes
			if jobManager != nil {
				if err := jobManager.GetIntegration().MountBuildContext(context); err != nil {
					// If mounting fails, try to use the provided context path
					fmt.Printf("Warning: Failed to mount build context from Kubernetes: %v\n", err)
				}
			}

			absContext, err := filepath.Abs(context)
			if err != nil {
				if jobManager != nil {
					jobManager.Fail(ctx, err, "context_resolution")
					return nil
				}
				return fmt.Errorf("failed to resolve context path: %v", err)
			}

			if _, err := os.Stat(absContext); os.IsNotExist(err) {
				if jobManager != nil {
					jobManager.Fail(ctx, err, "context_validation")
					return nil
				}
				return fmt.Errorf("context directory does not exist: %s", absContext)
			}

			dockerfilePath := filepath.Join(absContext, dockerfile)
			if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
				if jobManager != nil {
					jobManager.Fail(ctx, err, "dockerfile_validation")
					return nil
				}
				return fmt.Errorf("Dockerfile does not exist: %s", dockerfilePath)
			}

			buildArgsMap := make(map[string]string)
			for _, arg := range buildArgs {
				parts := strings.SplitN(arg, "=", 2)
				if len(parts) == 2 {
					buildArgsMap[parts[0]] = parts[1]
				} else {
					buildArgsMap[parts[0]] = ""
				}
			}

			var targetPlatforms []types.Platform
			if len(platforms) > 0 {
				for _, platform := range platforms {
					targetPlatforms = append(targetPlatforms, types.ParsePlatform(platform))
				}
			} else {
				targetPlatforms = []types.Platform{types.GetHostPlatform()}
			}

			if len(targetPlatforms) > 1 && output == "image" {
				output = "multiarch"
			}

			// Auto-select executor based on rootless flag
			if rootless && executor == "container" {
				executor = "rootless"
			}

			config := &types.BuildConfig{
				Context:    absContext,
				Dockerfile: dockerfile,
				Tags:       tags,
				Output:     output,
				Frontend:   frontend,
				CacheDir:   cacheDir,
				NoCache:    noCache,
				Progress:   progress,
				BuildArgs:  buildArgsMap,
				Platforms:  targetPlatforms,
				Push:       push,
				Registry:   registry,
				Rootless:   rootless,
			}

			// Load Kubernetes secrets and configuration if available
			if jobManager != nil {
				if registryConfig, err := jobManager.GetIntegration().LoadRegistryCredentials(); err == nil {
					config.RegistryConfig = registryConfig
				}
				
				if secrets, err := jobManager.GetIntegration().LoadBuildSecrets(); err == nil {
					config.Secrets = secrets
				}
				
				// Report build start
				platformStrs := make([]string, len(targetPlatforms))
				for i, p := range targetPlatforms {
					platformStrs[i] = p.String()
				}
				jobManager.GetLogger().LogBuildStart(ctx, platformStrs, tags)
				jobManager.ReportProgress(ctx, "init", 5.0, "Starting build", "", "INIT", false)
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				if jobManager != nil {
					jobManager.Fail(ctx, err, "builder_creation")
					return nil
				}
				return fmt.Errorf("failed to create builder: %v", err)
			}
			
			// Add builder cleanup to job manager
			if jobManager != nil {
				jobManager.AddCleanupFunc(func() error {
					builder.Cleanup()
					return nil
				})
			} else {
				defer builder.Cleanup()
			}

			// Report progress during build
			if jobManager != nil {
				jobManager.ReportProgress(ctx, "build", 10.0, "Building container image", "", "BUILD", false)
			}

			result, err := builder.Build()
			if err != nil {
				if jobManager != nil {
					jobManager.Fail(ctx, err, "build")
					return nil
				}
				return fmt.Errorf("build failed: %v", err)
			}

			if !result.Success {
				buildErr := fmt.Errorf("build failed: %s", result.Error)
				if jobManager != nil {
					jobManager.Fail(ctx, buildErr, "build")
					return nil
				}
				return buildErr
			}

			// Complete job lifecycle if in Kubernetes
			if jobManager != nil {
				exitCode := jobManager.Complete(ctx, result)
				if exitCode != k8s.ExitCodeSuccess {
					os.Exit(int(exitCode))
				}
			}

			fmt.Printf("Build completed successfully!\n")
			
			if result.MultiArch && len(result.PlatformResults) > 1 {
				fmt.Printf("Multi-architecture build completed for %d platforms:\n", len(result.PlatformResults))
				for platformStr, platformResult := range result.PlatformResults {
					status := "✓"
					if !platformResult.Success {
						status = "✗"
					}
					fmt.Printf("  %s %s", status, platformStr)
					if platformResult.Error != "" {
						fmt.Printf(" (error: %s)", platformResult.Error)
					}
					fmt.Printf("\n")
				}
				
				if result.ManifestListID != "" {
					fmt.Printf("Manifest List ID: %s\n", result.ManifestListID)
				}
			}
			
			if result.OutputPath != "" {
				fmt.Printf("Output: %s\n", result.OutputPath)
			}
			if result.ImageID != "" {
				fmt.Printf("Image ID: %s\n", result.ImageID)
			}
			
			fmt.Printf("Operations: %d\n", result.Operations)
			fmt.Printf("Cache hits: %d\n", result.CacheHits)
			fmt.Printf("Duration: %s\n", result.Duration)
			
			if config.Push && result.Success {
				fmt.Printf("Successfully pushed to registry\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dockerfile, "file", "f", "Dockerfile", "Path to the Dockerfile")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", []string{}, "Name and optionally a tag in the 'name:tag' format")
	cmd.Flags().StringVarP(&output, "output", "o", "image", "Output type (image, tar, local, multiarch)")
	cmd.Flags().StringVar(&frontend, "frontend", "dockerfile", "Frontend type")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: ~/.ossb/cache)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable caching")
	cmd.Flags().BoolVar(&progress, "progress", true, "Show progress")
	cmd.Flags().StringArrayVar(&buildArgs, "build-arg", []string{}, "Build arguments in KEY=VALUE format")
	cmd.Flags().StringArrayVar(&platforms, "platform", []string{}, "Target platforms (e.g., linux/amd64,linux/arm64)")
	cmd.Flags().BoolVar(&push, "push", false, "Push image to registry after build")
	cmd.Flags().StringVar(&registry, "registry", "", "Registry to push to (required with --push)")
	cmd.Flags().StringVar(&executor, "executor", "container", "Executor type (local, container, rootless)")
	cmd.Flags().BoolVar(&rootless, "rootless", false, "Enable rootless mode (requires no root privileges)")

	return cmd
}

func newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage build cache",
		Long:  "Commands for managing the OSSB build cache.",
	}

	cmd.AddCommand(newCacheInfoCommand())
	cmd.AddCommand(newCachePruneCommand())

	return cmd
}

func newCacheInfoCommand() *cobra.Command {
	var cacheDir string

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show cache statistics",
		Long:  "Display information about the current cache including size and hit rate.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cacheDir == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %v", err)
				}
				cacheDir = filepath.Join(homeDir, ".ossb", "cache")
			}

			cache := engine.NewCache(cacheDir)
			info, err := cache.Info()
			if err != nil {
				return fmt.Errorf("failed to get cache info: %v", err)
			}

			fmt.Printf("Cache Directory: %s\n", cacheDir)
			fmt.Printf("Total Size: %s\n", formatBytes(info.TotalSize))
			fmt.Printf("Total Files: %d\n", info.TotalFiles)
			fmt.Printf("Hit Rate: %.2f%%\n", info.HitRate*100)
			fmt.Printf("Hits: %d\n", info.Hits)
			fmt.Printf("Misses: %d\n", info.Misses)

			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: ~/.ossb/cache)")

	return cmd
}

func newCachePruneCommand() *cobra.Command {
	var cacheDir string

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove unused cache entries",
		Long:  "Remove cache entries older than 24 hours.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cacheDir == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %v", err)
				}
				cacheDir = filepath.Join(homeDir, ".ossb", "cache")
			}

			cache := engine.NewCache(cacheDir)
			
			infoBefore, err := cache.Info()
			if err != nil {
				return fmt.Errorf("failed to get cache info: %v", err)
			}

			if err := cache.Prune(); err != nil {
				return fmt.Errorf("failed to prune cache: %v", err)
			}

			infoAfter, err := cache.Info()
			if err != nil {
				return fmt.Errorf("failed to get cache info after prune: %v", err)
			}

			freedFiles := infoBefore.TotalFiles - infoAfter.TotalFiles
			freedSize := infoBefore.TotalSize - infoAfter.TotalSize

			fmt.Printf("Cache pruned successfully!\n")
			fmt.Printf("Removed %d files\n", freedFiles)
			fmt.Printf("Freed %s\n", formatBytes(freedSize))
			fmt.Printf("Remaining: %d files, %s\n", infoAfter.TotalFiles, formatBytes(infoAfter.TotalSize))

			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: ~/.ossb/cache)")

	return cmd
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func init() {
	cobra.OnInitialize(func() {
		if os.Getenv("OSSB_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "OSSB Debug Mode Enabled\n")
		}
	})
}