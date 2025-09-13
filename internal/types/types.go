package types

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"runtime"
	"time"
)

type OperationType string

const (
	OperationTypeSource   OperationType = "source"
	OperationTypeExec     OperationType = "exec"
	OperationTypeFile     OperationType = "file"
	OperationTypeMeta     OperationType = "meta"
	OperationTypePull     OperationType = "pull"     // Pull base image
	OperationTypeExtract  OperationType = "extract"  // Extract image layers
	OperationTypeLayer    OperationType = "layer"    // Create filesystem layer
	OperationTypeManifest OperationType = "manifest" // Generate manifest
	OperationTypePush     OperationType = "push"     // Push to registry
)

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

func (p Platform) String() string {
	if p.Variant != "" {
		return fmt.Sprintf("%s/%s/%s", p.OS, p.Architecture, p.Variant)
	}
	return fmt.Sprintf("%s/%s", p.OS, p.Architecture)
}

func ParsePlatform(platform string) Platform {
	parts := strings.Split(platform, "/")
	if len(parts) < 2 {
		return Platform{OS: "linux", Architecture: "amd64"}
	}
	
	p := Platform{
		OS:           parts[0],
		Architecture: parts[1],
	}
	
	if len(parts) > 2 {
		p.Variant = parts[2]
	}
	
	return p
}

func GetHostPlatform() Platform {
	return Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
}

func GetSupportedPlatforms() []Platform {
	return []Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
		{OS: "linux", Architecture: "arm", Variant: "v6"},
		{OS: "linux", Architecture: "386"},
		{OS: "linux", Architecture: "ppc64le"},
		{OS: "linux", Architecture: "s390x"},
		{OS: "windows", Architecture: "amd64"},
		{OS: "darwin", Architecture: "amd64"},
		{OS: "darwin", Architecture: "arm64"},
	}
}

type Operation struct {
	Type        OperationType     `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Inputs      []string          `json:"inputs,omitempty"`
	Outputs     []string          `json:"outputs,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	WorkDir     string            `json:"workdir,omitempty"`
	User        string            `json:"user,omitempty"`
	Platform    Platform          `json:"platform,omitempty"`
}

func (o *Operation) CacheKey() string {
	data := struct {
		Type        OperationType     `json:"type"`
		Command     []string          `json:"command,omitempty"`
		Inputs      []string          `json:"inputs,omitempty"`
		Environment map[string]string `json:"environment,omitempty"`
		Metadata    map[string]string `json:"metadata,omitempty"`
		WorkDir     string            `json:"workdir,omitempty"`
		User        string            `json:"user,omitempty"`
		Platform    Platform          `json:"platform,omitempty"`
	}{
		Type:        o.Type,
		Command:     o.Command,
		Inputs:      o.Inputs,
		Environment: o.Environment,
		Metadata:    o.Metadata,
		WorkDir:     o.WorkDir,
		User:        o.User,
		Platform:    o.Platform,
	}
	
	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return fmt.Sprintf("%x", hash)
}

type OperationResult struct {
	Operation   *Operation        `json:"operation"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
	Outputs     []string          `json:"outputs,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	CacheHit    bool              `json:"cache_hit"`
}

type GraphNode struct {
	ID          string      `json:"id"`
	Operation   *Operation  `json:"operation"`
	Dependencies []string    `json:"dependencies"`
	Dependents  []string    `json:"dependents"`
}

type Graph struct {
	Nodes map[string]*GraphNode `json:"nodes"`
	Root  string                `json:"root"`
}

func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[string]*GraphNode),
	}
}

func (g *Graph) AddNode(id string, op *Operation) {
	g.Nodes[id] = &GraphNode{
		ID:          id,
		Operation:   op,
		Dependencies: []string{},
		Dependents:  []string{},
	}
}

func (g *Graph) AddDependency(nodeID, dependsOnID string) error {
	node, exists := g.Nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s does not exist", nodeID)
	}
	
	dependsOn, exists := g.Nodes[dependsOnID]
	if !exists {
		return fmt.Errorf("dependency node %s does not exist", dependsOnID)
	}
	
	node.Dependencies = append(node.Dependencies, dependsOnID)
	dependsOn.Dependents = append(dependsOn.Dependents, nodeID)
	
	return nil
}

func (g *Graph) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int)
	
	for id := range g.Nodes {
		inDegree[id] = 0
	}
	
	for _, node := range g.Nodes {
		for _, dep := range node.Dependencies {
			inDegree[dep]++
		}
	}
	
	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	
	result := []string{}
	
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		
		node := g.Nodes[current]
		for _, dependent := range node.Dependents {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	
	if len(result) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}
	
	return result, nil
}

func (g *Graph) HasCycles() bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	
	for id := range g.Nodes {
		if !visited[id] {
			if g.hasCycleDFS(id, visited, recStack) {
				return true
			}
		}
	}
	
	return false
}

func (g *Graph) hasCycleDFS(nodeID string, visited, recStack map[string]bool) bool {
	visited[nodeID] = true
	recStack[nodeID] = true
	
	node := g.Nodes[nodeID]
	for _, dep := range node.Dependencies {
		if !visited[dep] && g.hasCycleDFS(dep, visited, recStack) {
			return true
		} else if recStack[dep] {
			return true
		}
	}
	
	recStack[nodeID] = false
	return false
}

func (g *Graph) Optimize() {
	redundant := make(map[string]bool)
	
	for id, node := range g.Nodes {
		if g.isRedundant(node) {
			redundant[id] = true
		}
	}
	
	for id := range redundant {
		delete(g.Nodes, id)
		for _, node := range g.Nodes {
			node.Dependencies = removeFromSlice(node.Dependencies, id)
			node.Dependents = removeFromSlice(node.Dependents, id)
		}
	}
}

func (g *Graph) isRedundant(node *GraphNode) bool {
	if node.Operation.Type == OperationTypeMeta {
		if len(node.Dependents) == 0 && node.Operation.Metadata != nil {
			if _, hasExpose := node.Operation.Metadata["expose"]; hasExpose {
				return len(node.Dependencies) == 0
			}
		}
	}
	return false
}

func removeFromSlice(slice []string, item string) []string {
	result := []string{}
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

type BuildConfig struct {
	Context     string            `json:"context"`
	Dockerfile  string            `json:"dockerfile"`
	Tags        []string          `json:"tags"`
	Output      string            `json:"output"`
	Frontend    string            `json:"frontend"`
	CacheDir    string            `json:"cache_dir"`
	NoCache     bool              `json:"no_cache"`
	Progress    bool              `json:"progress"`
	BuildArgs   map[string]string `json:"build_args"`
	Platforms   []Platform        `json:"platforms,omitempty"`
	Push        bool              `json:"push,omitempty"`
	Registry    string            `json:"registry,omitempty"`
	Rootless    bool              `json:"rootless,omitempty"`
	
	// Registry configuration
	RegistryConfig  *RegistryConfig   `json:"registry_config,omitempty"`
	Secrets         map[string]string `json:"secrets,omitempty"`
	NetworkMode     string            `json:"network_mode,omitempty"`
	SecurityContext *SecurityContext  `json:"security_context,omitempty"`
	ResourceLimits  *ResourceLimits   `json:"resource_limits,omitempty"`
}

type RegistryConfig struct {
	DefaultRegistry string                    `json:"default_registry,omitempty"`
	Registries      map[string]RegistryAuth   `json:"registries,omitempty"`
	Insecure        []string                  `json:"insecure,omitempty"`
	Mirrors         map[string][]string       `json:"mirrors,omitempty"`
}

type RegistryAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	AuthFile string `json:"auth_file,omitempty"`
}

type SecurityContext struct {
	RunAsUser    *int64   `json:"run_as_user,omitempty"`
	RunAsGroup   *int64   `json:"run_as_group,omitempty"`
	RunAsNonRoot *bool    `json:"run_as_non_root,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type ResourceLimits struct {
	Memory string `json:"memory,omitempty"` // "1Gi"
	CPU    string `json:"cpu,omitempty"`    // "1000m"
	Disk   string `json:"disk,omitempty"`   // "10Gi"
}

type CacheInfo struct {
	TotalSize   int64 `json:"total_size"`
	TotalFiles  int   `json:"total_files"`
	HitRate     float64 `json:"hit_rate"`
	Hits        int64 `json:"hits"`
	Misses      int64 `json:"misses"`
}

type CacheMetrics struct {
	TotalHits         int64                            `json:"total_hits"`
	TotalMisses       int64                            `json:"total_misses"`
	HitRate           float64                          `json:"hit_rate"`
	TotalSize         int64                            `json:"total_size"`
	TotalFiles        int                              `json:"total_files"`
	PlatformStats     map[string]*PlatformCacheStats   `json:"platform_stats"`
	InvalidationCount int64                            `json:"invalidation_count"`
	PruningCount      int64                            `json:"pruning_count"`
	SharedEntries     int                              `json:"shared_entries"`
}

type PlatformCacheStats struct {
	Hits        int64     `json:"hits"`
	Misses      int64     `json:"misses"`
	TotalSize   int64     `json:"total_size"`
	TotalFiles  int       `json:"total_files"`
	LastUpdated time.Time `json:"last_updated"`
}

type PlatformResult struct {
	Platform   Platform          `json:"platform"`
	Success    bool              `json:"success"`
	Error      string            `json:"error,omitempty"`
	ImageID    string            `json:"image_id,omitempty"`
	ManifestID string            `json:"manifest_id,omitempty"`
	Size       int64             `json:"size,omitempty"`
	CacheHits  int               `json:"cache_hits,omitempty"`
}

type BuildResult struct {
	Success         bool                       `json:"success"`
	Error           string                     `json:"error,omitempty"`
	Operations      int                        `json:"operations"`
	CacheHits       int                        `json:"cache_hits"`
	Duration        string                     `json:"duration"`
	OutputPath      string                     `json:"output_path,omitempty"`
	ImageID         string                     `json:"image_id,omitempty"`
	ManifestListID  string                     `json:"manifest_list_id,omitempty"`
	Metadata        map[string]string          `json:"metadata,omitempty"`
	PlatformResults map[string]*PlatformResult `json:"platform_results,omitempty"`
	MultiArch       bool                       `json:"multi_arch,omitempty"`
}

type DockerfileInstruction struct {
	Command string            `json:"command"`
	Value   string            `json:"value"`
	Args    map[string]string `json:"args,omitempty"`
	Line    int               `json:"line"`
	Stage   string            `json:"stage,omitempty"`   // Stage name for multi-stage builds
}

// BuildStage represents a single stage in a multi-stage Dockerfile
type BuildStage struct {
	Name         string                   `json:"name"`          // Stage name (from AS clause)
	Index        int                      `json:"index"`         // Stage index (0-based)
	BaseImage    string                   `json:"base_image"`    // FROM image
	Instructions []*DockerfileInstruction `json:"instructions"`  // Instructions in this stage
	Operations   []*Operation             `json:"operations"`    // Generated operations
	Dependencies []string                 `json:"dependencies"`  // Other stages this stage depends on
	IsFinal      bool                     `json:"is_final"`      // Whether this is the final stage
}

// MultiStageContext holds context for multi-stage builds
type MultiStageContext struct {
	Stages       []*BuildStage         `json:"stages"`        // All stages in order
	StagesByName map[string]*BuildStage `json:"stages_by_name"` // Stages indexed by name
	FinalStage   *BuildStage           `json:"final_stage"`   // The final stage to export
}

func NormalizeEnvironment(env map[string]string) map[string]string {
	if env == nil {
		return make(map[string]string)
	}
	
	normalized := make(map[string]string)
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	
	for _, k := range keys {
		normalized[k] = env[k]
	}
	
	return normalized
}

func ExpandVariables(input string, env map[string]string) string {
	result := input
	
	for key, value := range env {
		result = strings.ReplaceAll(result, "${"+key+"}", value)
		result = strings.ReplaceAll(result, "$"+key, value)
	}
	
	return result
}