package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VulnerabilityScanner provides vulnerability scanning capabilities
type VulnerabilityScanner struct {
	scanners       []Scanner
	timeout        time.Duration
	failOnHigh     bool
	failOnCritical bool
	outputDir      string
}

// Scanner interface for different vulnerability scanners
type Scanner interface {
	Name() string
	IsAvailable() bool
	ScanImage(imageRef string) (*ScanResult, error)
	ScanFilesystem(path string) (*ScanResult, error)
}

// ScanResult represents the result of a vulnerability scan
type ScanResult struct {
	Scanner      string                 `json:"scanner"`
	ImageRef     string                 `json:"image_ref,omitempty"`
	Path         string                 `json:"path,omitempty"`
	ScanTime     time.Time              `json:"scan_time"`
	Duration     time.Duration          `json:"duration"`
	Summary      VulnerabilitySummary   `json:"summary"`
	Vulnerabilities []Vulnerability     `json:"vulnerabilities"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// VulnerabilitySummary provides a summary of vulnerabilities found
type VulnerabilitySummary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// Vulnerability represents a single vulnerability
type Vulnerability struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    string            `json:"severity"`
	Score       float64           `json:"score,omitempty"`
	Package     string            `json:"package"`
	Version     string            `json:"version"`
	FixedIn     string            `json:"fixed_in,omitempty"`
	References  []string          `json:"references,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// NewVulnerabilityScanner creates a new vulnerability scanner
func NewVulnerabilityScanner(outputDir string) *VulnerabilityScanner {
	vs := &VulnerabilityScanner{
		timeout:        10 * time.Minute,
		failOnHigh:     false,
		failOnCritical: true,
		outputDir:      outputDir,
	}

	// Initialize available scanners
	vs.scanners = []Scanner{
		NewTrivyScanner(),
		NewGrypeScanner(),
	}

	return vs
}

// SetFailurePolicy sets when scans should fail the build
func (vs *VulnerabilityScanner) SetFailurePolicy(failOnCritical, failOnHigh bool) {
	vs.failOnCritical = failOnCritical
	vs.failOnHigh = failOnHigh
}

// SetTimeout sets the scan timeout
func (vs *VulnerabilityScanner) SetTimeout(timeout time.Duration) {
	vs.timeout = timeout
}

// ScanImage scans a container image for vulnerabilities
func (vs *VulnerabilityScanner) ScanImage(imageRef string) (*ScanResult, error) {
	// Find available scanner
	scanner := vs.getAvailableScanner()
	if scanner == nil {
		return nil, fmt.Errorf("no vulnerability scanners available")
	}

	// Perform scan
	result, err := scanner.ScanImage(imageRef)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %v", err)
	}

	// Save results
	if err := vs.saveResults(result); err != nil {
		fmt.Printf("Warning: failed to save scan results: %v\n", err)
	}

	// Check if scan should fail the build
	if vs.shouldFailBuild(result) {
		return result, fmt.Errorf("vulnerability scan failed: %d critical, %d high vulnerabilities found",
			result.Summary.Critical, result.Summary.High)
	}

	return result, nil
}

// ScanFilesystem scans a filesystem path for vulnerabilities
func (vs *VulnerabilityScanner) ScanFilesystem(path string) (*ScanResult, error) {
	// Find available scanner
	scanner := vs.getAvailableScanner()
	if scanner == nil {
		return nil, fmt.Errorf("no vulnerability scanners available")
	}

	// Perform scan
	result, err := scanner.ScanFilesystem(path)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %v", err)
	}

	// Save results
	if err := vs.saveResults(result); err != nil {
		fmt.Printf("Warning: failed to save scan results: %v\n", err)
	}

	// Check if scan should fail the build
	if vs.shouldFailBuild(result) {
		return result, fmt.Errorf("vulnerability scan failed: %d critical, %d high vulnerabilities found",
			result.Summary.Critical, result.Summary.High)
	}

	return result, nil
}

// getAvailableScanner returns the first available scanner
func (vs *VulnerabilityScanner) getAvailableScanner() Scanner {
	for _, scanner := range vs.scanners {
		if scanner.IsAvailable() {
			return scanner
		}
	}
	return nil
}

// shouldFailBuild determines if the scan results should fail the build
func (vs *VulnerabilityScanner) shouldFailBuild(result *ScanResult) bool {
	if vs.failOnCritical && result.Summary.Critical > 0 {
		return true
	}
	if vs.failOnHigh && result.Summary.High > 0 {
		return true
	}
	return false
}

// saveResults saves scan results to disk
func (vs *VulnerabilityScanner) saveResults(result *ScanResult) error {
	if vs.outputDir == "" {
		return nil
	}

	if err := os.MkdirAll(vs.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	filename := fmt.Sprintf("scan-%s-%d.json", result.Scanner, time.Now().Unix())
	filepath := filepath.Join(vs.outputDir, filename)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write results: %v", err)
	}

	return nil
}

// TrivyScanner implements the Scanner interface for Trivy
type TrivyScanner struct{}

// NewTrivyScanner creates a new Trivy scanner
func NewTrivyScanner() *TrivyScanner {
	return &TrivyScanner{}
}

// Name returns the scanner name
func (ts *TrivyScanner) Name() string {
	return "trivy"
}

// IsAvailable checks if Trivy is available
func (ts *TrivyScanner) IsAvailable() bool {
	_, err := exec.LookPath("trivy")
	return err == nil
}

// ScanImage scans a container image using Trivy
func (ts *TrivyScanner) ScanImage(imageRef string) (*ScanResult, error) {
	startTime := time.Now()

	// Run trivy scan
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "trivy", "image", "--format", "json", "--quiet", imageRef)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("trivy scan failed: %v", err)
	}

	// Parse Trivy output
	result, err := ts.parseTrivyOutput(output, imageRef, "")
	if err != nil {
		return nil, fmt.Errorf("failed to parse trivy output: %v", err)
	}

	result.ScanTime = startTime
	result.Duration = time.Since(startTime)

	return result, nil
}

// ScanFilesystem scans a filesystem using Trivy
func (ts *TrivyScanner) ScanFilesystem(path string) (*ScanResult, error) {
	startTime := time.Now()

	// Run trivy scan
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "trivy", "fs", "--format", "json", "--quiet", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("trivy scan failed: %v", err)
	}

	// Parse Trivy output
	result, err := ts.parseTrivyOutput(output, "", path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse trivy output: %v", err)
	}

	result.ScanTime = startTime
	result.Duration = time.Since(startTime)

	return result, nil
}

// parseTrivyOutput parses Trivy JSON output
func (ts *TrivyScanner) parseTrivyOutput(output []byte, imageRef, path string) (*ScanResult, error) {
	var trivyResult struct {
		Results []struct {
			Target          string `json:"Target"`
			Class           string `json:"Class"`
			Type            string `json:"Type"`
			Vulnerabilities []struct {
				VulnerabilityID  string   `json:"VulnerabilityID"`
				PkgName          string   `json:"PkgName"`
				InstalledVersion string   `json:"InstalledVersion"`
				FixedVersion     string   `json:"FixedVersion"`
				Severity         string   `json:"Severity"`
				Title            string   `json:"Title"`
				Description      string   `json:"Description"`
				References       []string `json:"References"`
				CVSS             struct {
					Score float64 `json:"Score"`
				} `json:"CVSS"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}

	if err := json.Unmarshal(output, &trivyResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trivy output: %v", err)
	}

	result := &ScanResult{
		Scanner:         "trivy",
		ImageRef:        imageRef,
		Path:            path,
		Vulnerabilities: []Vulnerability{},
		Summary:         VulnerabilitySummary{},
	}

	// Process vulnerabilities
	for _, trivyRes := range trivyResult.Results {
		for _, vuln := range trivyRes.Vulnerabilities {
			v := Vulnerability{
				ID:          vuln.VulnerabilityID,
				Title:       vuln.Title,
				Description: vuln.Description,
				Severity:    strings.ToLower(vuln.Severity),
				Score:       vuln.CVSS.Score,
				Package:     vuln.PkgName,
				Version:     vuln.InstalledVersion,
				FixedIn:     vuln.FixedVersion,
				References:  vuln.References,
			}

			result.Vulnerabilities = append(result.Vulnerabilities, v)

			// Update summary
			result.Summary.Total++
			switch strings.ToLower(vuln.Severity) {
			case "critical":
				result.Summary.Critical++
			case "high":
				result.Summary.High++
			case "medium":
				result.Summary.Medium++
			case "low":
				result.Summary.Low++
			default:
				result.Summary.Unknown++
			}
		}
	}

	return result, nil
}

// GrypeScanner implements the Scanner interface for Grype
type GrypeScanner struct{}

// NewGrypeScanner creates a new Grype scanner
func NewGrypeScanner() *GrypeScanner {
	return &GrypeScanner{}
}

// Name returns the scanner name
func (gs *GrypeScanner) Name() string {
	return "grype"
}

// IsAvailable checks if Grype is available
func (gs *GrypeScanner) IsAvailable() bool {
	_, err := exec.LookPath("grype")
	return err == nil
}

// ScanImage scans a container image using Grype
func (gs *GrypeScanner) ScanImage(imageRef string) (*ScanResult, error) {
	startTime := time.Now()

	// Run grype scan
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grype", imageRef, "-o", "json", "-q")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("grype scan failed: %v", err)
	}

	// Parse Grype output
	result, err := gs.parseGrypeOutput(output, imageRef, "")
	if err != nil {
		return nil, fmt.Errorf("failed to parse grype output: %v", err)
	}

	result.ScanTime = startTime
	result.Duration = time.Since(startTime)

	return result, nil
}

// ScanFilesystem scans a filesystem using Grype
func (gs *GrypeScanner) ScanFilesystem(path string) (*ScanResult, error) {
	startTime := time.Now()

	// Run grype scan
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grype", "dir:"+path, "-o", "json", "-q")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("grype scan failed: %v", err)
	}

	// Parse Grype output
	result, err := gs.parseGrypeOutput(output, "", path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse grype output: %v", err)
	}

	result.ScanTime = startTime
	result.Duration = time.Since(startTime)

	return result, nil
}

// parseGrypeOutput parses Grype JSON output
func (gs *GrypeScanner) parseGrypeOutput(output []byte, imageRef, path string) (*ScanResult, error) {
	var grypeResult struct {
		Matches []struct {
			Vulnerability struct {
				ID          string   `json:"id"`
				DataSource  string   `json:"dataSource"`
				Severity    string   `json:"severity"`
				URLs        []string `json:"urls"`
				Description string   `json:"description"`
				Cvss        []struct {
					Metrics struct {
						BaseScore float64 `json:"baseScore"`
					} `json:"metrics"`
				} `json:"cvss"`
			} `json:"vulnerability"`
			Artifact struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Type    string `json:"type"`
			} `json:"artifact"`
			MatchDetails []struct {
				Type       string `json:"type"`
				Matcher    string `json:"matcher"`
				SearchedBy struct {
					Language  string `json:"language"`
					Namespace string `json:"namespace"`
				} `json:"searchedBy"`
				Found struct {
					VersionConstraint string `json:"versionConstraint"`
				} `json:"found"`
			} `json:"matchDetails"`
		} `json:"matches"`
	}

	if err := json.Unmarshal(output, &grypeResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal grype output: %v", err)
	}

	result := &ScanResult{
		Scanner:         "grype",
		ImageRef:        imageRef,
		Path:            path,
		Vulnerabilities: []Vulnerability{},
		Summary:         VulnerabilitySummary{},
	}

	// Process vulnerabilities
	for _, match := range grypeResult.Matches {
		var score float64
		if len(match.Vulnerability.Cvss) > 0 {
			score = match.Vulnerability.Cvss[0].Metrics.BaseScore
		}

		// Extract fixed version from match details
		var fixedIn string
		for _, detail := range match.MatchDetails {
			if detail.Found.VersionConstraint != "" {
				fixedIn = detail.Found.VersionConstraint
				break
			}
		}

		v := Vulnerability{
			ID:          match.Vulnerability.ID,
			Title:       match.Vulnerability.ID, // Grype doesn't provide separate title
			Description: match.Vulnerability.Description,
			Severity:    strings.ToLower(match.Vulnerability.Severity),
			Score:       score,
			Package:     match.Artifact.Name,
			Version:     match.Artifact.Version,
			FixedIn:     fixedIn,
			References:  match.Vulnerability.URLs,
		}

		result.Vulnerabilities = append(result.Vulnerabilities, v)

		// Update summary
		result.Summary.Total++
		switch strings.ToLower(match.Vulnerability.Severity) {
		case "critical":
			result.Summary.Critical++
		case "high":
			result.Summary.High++
		case "medium":
			result.Summary.Medium++
		case "low":
			result.Summary.Low++
		default:
			result.Summary.Unknown++
		}
	}

	return result, nil
}

// ScanResultAnalyzer provides analysis of scan results
type ScanResultAnalyzer struct{}

// NewScanResultAnalyzer creates a new scan result analyzer
func NewScanResultAnalyzer() *ScanResultAnalyzer {
	return &ScanResultAnalyzer{}
}

// AnalyzeResults analyzes scan results and provides recommendations
func (sra *ScanResultAnalyzer) AnalyzeResults(result *ScanResult) *ScanAnalysis {
	analysis := &ScanAnalysis{
		Result:          result,
		RiskLevel:       sra.calculateRiskLevel(result),
		Recommendations: sra.generateRecommendations(result),
		FixableCount:    sra.countFixableVulnerabilities(result),
		TopPackages:     sra.getTopVulnerablePackages(result),
	}

	return analysis
}

// ScanAnalysis represents analysis of scan results
type ScanAnalysis struct {
	Result          *ScanResult            `json:"result"`
	RiskLevel       string                 `json:"risk_level"`
	Recommendations []string               `json:"recommendations"`
	FixableCount    int                    `json:"fixable_count"`
	TopPackages     []PackageVulnerability `json:"top_packages"`
}

// PackageVulnerability represents vulnerability information for a package
type PackageVulnerability struct {
	Package     string `json:"package"`
	Version     string `json:"version"`
	VulnCount   int    `json:"vuln_count"`
	MaxSeverity string `json:"max_severity"`
}

// calculateRiskLevel calculates the overall risk level
func (sra *ScanResultAnalyzer) calculateRiskLevel(result *ScanResult) string {
	if result.Summary.Critical > 0 {
		return "critical"
	}
	if result.Summary.High > 5 {
		return "high"
	}
	if result.Summary.High > 0 || result.Summary.Medium > 10 {
		return "medium"
	}
	if result.Summary.Medium > 0 || result.Summary.Low > 20 {
		return "low"
	}
	return "minimal"
}

// generateRecommendations generates recommendations based on scan results
func (sra *ScanResultAnalyzer) generateRecommendations(result *ScanResult) []string {
	var recommendations []string

	if result.Summary.Critical > 0 {
		recommendations = append(recommendations, "Immediately update packages with critical vulnerabilities")
	}

	if result.Summary.High > 0 {
		recommendations = append(recommendations, "Update packages with high severity vulnerabilities")
	}

	fixableCount := sra.countFixableVulnerabilities(result)
	if fixableCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Update packages to fix %d vulnerabilities", fixableCount))
	}

	if result.Summary.Total > 50 {
		recommendations = append(recommendations, "Consider using a more secure base image")
	}

	// Package-specific recommendations
	packageCounts := make(map[string]int)
	for _, vuln := range result.Vulnerabilities {
		packageCounts[vuln.Package]++
	}

	for pkg, count := range packageCounts {
		if count > 5 {
			recommendations = append(recommendations, fmt.Sprintf("Consider replacing or updating package '%s' (%d vulnerabilities)", pkg, count))
		}
	}

	return recommendations
}

// countFixableVulnerabilities counts vulnerabilities that have fixes available
func (sra *ScanResultAnalyzer) countFixableVulnerabilities(result *ScanResult) int {
	count := 0
	for _, vuln := range result.Vulnerabilities {
		if vuln.FixedIn != "" {
			count++
		}
	}
	return count
}

// getTopVulnerablePackages returns the packages with the most vulnerabilities
func (sra *ScanResultAnalyzer) getTopVulnerablePackages(result *ScanResult) []PackageVulnerability {
	packageMap := make(map[string]*PackageVulnerability)

	for _, vuln := range result.Vulnerabilities {
		key := vuln.Package + ":" + vuln.Version
		if pv, exists := packageMap[key]; exists {
			pv.VulnCount++
			if sra.severityLevel(vuln.Severity) > sra.severityLevel(pv.MaxSeverity) {
				pv.MaxSeverity = vuln.Severity
			}
		} else {
			packageMap[key] = &PackageVulnerability{
				Package:     vuln.Package,
				Version:     vuln.Version,
				VulnCount:   1,
				MaxSeverity: vuln.Severity,
			}
		}
	}

	// Convert to slice and sort by vulnerability count
	var packages []PackageVulnerability
	for _, pv := range packageMap {
		packages = append(packages, *pv)
	}

	// Simple sort by vulnerability count (descending)
	for i := 0; i < len(packages)-1; i++ {
		for j := i + 1; j < len(packages); j++ {
			if packages[j].VulnCount > packages[i].VulnCount {
				packages[i], packages[j] = packages[j], packages[i]
			}
		}
	}

	// Return top 10
	if len(packages) > 10 {
		packages = packages[:10]
	}

	return packages
}

// severityLevel returns numeric level for severity comparison
func (sra *ScanResultAnalyzer) severityLevel(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}