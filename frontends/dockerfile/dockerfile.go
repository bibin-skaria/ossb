package dockerfile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bibin-skaria/ossb/frontends"
	"github.com/bibin-skaria/ossb/internal/types"
)

type DockerfileFrontend struct{}

func init() {
	frontends.RegisterFrontend("dockerfile", &DockerfileFrontend{})
}

func (d *DockerfileFrontend) Parse(dockerfileContent string, config *types.BuildConfig) ([]*types.Operation, error) {
	fmt.Printf("Debug: DockerfileFrontend.Parse called with content length: %d\n", len(dockerfileContent))
	
	parser := &Parser{
		config:      config,
		buildArgs:   config.BuildArgs,
		environment: make(map[string]string),
		workdir:     "/",
		user:        "root",
	}

	operations, err := parser.Parse(dockerfileContent)
	fmt.Printf("Debug: DockerfileFrontend.Parse returning %d operations\n", len(operations))
	return operations, err
}

type Parser struct {
	config      *types.BuildConfig
	buildArgs   map[string]string
	environment map[string]string
	workdir     string
	user        string
	operations  []*types.Operation
	
	// Multi-stage support
	multiStageContext *types.MultiStageContext
	currentStage      *types.BuildStage
	stageIndex        int
}

func (p *Parser) Parse(content string) ([]*types.Operation, error) {
	lines := strings.Split(content, "\n")
	instructions, err := p.parseInstructions(lines)
	if err != nil {
		return nil, err
	}

	// Initialize multi-stage context
	p.multiStageContext = &types.MultiStageContext{
		Stages:       []*types.BuildStage{},
		StagesByName: make(map[string]*types.BuildStage),
	}

	// First pass: identify stages and group instructions
	if err := p.identifyStages(instructions); err != nil {
		return nil, err
	}

	// Second pass: process each stage and build operations
	for _, stage := range p.multiStageContext.Stages {
		p.currentStage = stage
		p.resetStageContext()
		
		fmt.Printf("Debug: Processing stage %s with %d instructions\n", stage.Name, len(stage.Instructions))
		
		for _, instruction := range stage.Instructions {
			instruction.Stage = stage.Name
			if err := p.processInstruction(instruction); err != nil {
				return nil, fmt.Errorf("error processing instruction at line %d in stage %s: %v", instruction.Line, stage.Name, err)
			}
		}
		
		// Store operations for this stage
		stage.Operations = make([]*types.Operation, len(p.operations))
		copy(stage.Operations, p.operations)
		
		fmt.Printf("Debug: Stage %s has %d operations\n", stage.Name, len(stage.Operations))
		
		// Reset operations for next stage
		p.operations = []*types.Operation{}
	}

	// Third pass: resolve stage dependencies and build final operation list
	if err := p.resolveStageReferences(); err != nil {
		return nil, err
	}

	// Return operations from final stage or all stages if building intermediate stages
	return p.getFinalOperations(), nil
}

func (p *Parser) parseInstructions(lines []string) ([]*types.DockerfileInstruction, error) {
	var instructions []*types.DockerfileInstruction
	var currentInstruction *types.DockerfileInstruction
	
	for i, line := range lines {
		line = strings.TrimSpace(line)
		
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		if strings.HasSuffix(line, "\\") {
			if currentInstruction == nil {
				parts := strings.SplitN(line[:len(line)-1], " ", 2)
				if len(parts) < 2 {
					continue
				}
				currentInstruction = &types.DockerfileInstruction{
					Command: strings.ToUpper(parts[0]),
					Value:   strings.TrimSpace(parts[1]),
					Line:    i + 1,
				}
			} else {
				currentInstruction.Value += " " + strings.TrimSpace(line[:len(line)-1])
			}
			continue
		}
		
		if currentInstruction != nil {
			currentInstruction.Value += " " + strings.TrimSpace(line)
			instructions = append(instructions, currentInstruction)
			currentInstruction = nil
			continue
		}
		
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		
		instruction := &types.DockerfileInstruction{
			Command: strings.ToUpper(parts[0]),
			Value:   strings.TrimSpace(parts[1]),
			Line:    i + 1,
		}
		
		instructions = append(instructions, instruction)
	}
	
	return instructions, nil
}

func (p *Parser) processInstruction(instruction *types.DockerfileInstruction) error {
	switch instruction.Command {
	case "FROM":
		return p.processFrom(instruction)
	case "RUN":
		return p.processRun(instruction)
	case "COPY":
		return p.processCopy(instruction)
	case "ADD":
		return p.processAdd(instruction)
	case "WORKDIR":
		return p.processWorkdir(instruction)
	case "ENV":
		return p.processEnv(instruction)
	case "EXPOSE":
		return p.processExpose(instruction)
	case "CMD":
		return p.processCmd(instruction)
	case "ENTRYPOINT":
		return p.processEntrypoint(instruction)
	case "VOLUME":
		return p.processVolume(instruction)
	case "USER":
		return p.processUser(instruction)
	case "ARG":
		return p.processArg(instruction)
	case "LABEL":
		return p.processLabel(instruction)
	default:
		return fmt.Errorf("unsupported instruction: %s", instruction.Command)
	}
}

func (p *Parser) processFrom(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	parts := strings.Fields(value)
	
	if len(parts) == 0 {
		return fmt.Errorf("FROM instruction requires an image")
	}
	
	image := parts[0]
	var alias string
	
	if len(parts) >= 3 && strings.ToUpper(parts[1]) == "AS" {
		alias = parts[2]
	}
	
	// Check if the image is actually a reference to another stage
	var isStageReference bool
	if p.multiStageContext != nil {
		if _, exists := p.multiStageContext.StagesByName[image]; exists {
			isStageReference = true
		} else if stageIndex := p.parseStageIndex(image); stageIndex >= 0 && stageIndex < len(p.multiStageContext.Stages) {
			isStageReference = true
			// Convert numeric reference to stage name
			image = p.multiStageContext.Stages[stageIndex].Name
		}
		
		// If this is a stage reference, add it as a dependency
		if isStageReference && p.currentStage != nil {
			if !p.containsString(p.currentStage.Dependencies, image) {
				p.currentStage.Dependencies = append(p.currentStage.Dependencies, image)
			}
		}
	}
	
	op := &types.Operation{
		Type: types.OperationTypeSource,
		Metadata: map[string]string{
			"image": image,
		},
		Outputs: []string{"base"},
	}
	
	if alias != "" {
		op.Metadata["alias"] = alias
	}
	
	if isStageReference {
		op.Metadata["stage_reference"] = "true"
		op.Metadata["source_stage"] = image
	}
	
	// Add stage context if available
	if p.currentStage != nil {
		op.Metadata["current_stage"] = p.currentStage.Name
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processRun(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	command := p.parseCommand(value)
	
	op := &types.Operation{
		Type:        types.OperationTypeExec,
		Command:     command,
		Inputs:      p.getLastOutput(),
		Outputs:     []string{fmt.Sprintf("layer-%d", len(p.operations))},
		Environment: p.copyEnvironment(),
		WorkDir:     p.workdir,
		User:        p.user,
	}
	
	fmt.Printf("Debug: Created RUN operation: %s\n", value)
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processCopy(instruction *types.DockerfileInstruction) error {
	return p.processFileOperation(instruction, "copy")
}

func (p *Parser) processAdd(instruction *types.DockerfileInstruction) error {
	return p.processFileOperation(instruction, "add")
}

func (p *Parser) processFileOperation(instruction *types.DockerfileInstruction, operationType string) error {
	value := p.expandVariables(instruction.Value)
	
	// Check for --from flag in COPY instruction
	var fromStage string
	var cleanValue string
	
	if operationType == "copy" {
		fromStage, cleanValue = p.parseFromFlag(value)
	} else {
		cleanValue = value
	}
	
	parts := p.parseFileArgs(cleanValue)
	
	if len(parts) < 2 {
		return fmt.Errorf("%s instruction requires at least source and destination", strings.ToUpper(operationType))
	}
	
	sources := parts[:len(parts)-1]
	dest := parts[len(parts)-1]
	
	// Handle source paths based on whether it's copying from another stage
	if fromStage != "" {
		// Copying from another stage - sources are relative to that stage's filesystem
		// We'll handle this in the executor
	} else {
		// Copying from build context
		for i, source := range sources {
			sources[i] = filepath.Join(p.config.Context, source)
		}
	}
	
	op := &types.Operation{
		Type:    types.OperationTypeFile,
		Command: []string{operationType},
		Inputs:  append(p.getLastOutput(), sources...),
		Outputs: []string{fmt.Sprintf("layer-%d", len(p.operations))},
		Environment: p.copyEnvironment(),
		WorkDir:     p.workdir,
		User:        p.user,
		Metadata: map[string]string{
			"dest": dest,
		},
	}
	
	// Add from stage metadata if present
	if fromStage != "" {
		// Resolve numeric references
		resolvedStage := fromStage
		if p.multiStageContext != nil {
			if stageIndex := p.parseStageIndex(fromStage); stageIndex >= 0 && stageIndex < len(p.multiStageContext.Stages) {
				resolvedStage = p.multiStageContext.Stages[stageIndex].Name
			}
		}
		op.Metadata["from_stage"] = resolvedStage
		op.Type = types.OperationTypeFile // We might want a specific type for stage copies
	}
	
	p.operations = append(p.operations, op)
	return nil
}

// parseFromFlag extracts --from flag from COPY instruction and returns clean value
func (p *Parser) parseFromFlag(value string) (string, string) {
	// Look for --from=stage pattern
	re := regexp.MustCompile(`--from=([^\s]+)`)
	matches := re.FindStringSubmatch(value)
	
	if len(matches) > 1 {
		fromStage := matches[1]
		// Remove the --from flag from the value
		cleanValue := re.ReplaceAllString(value, "")
		cleanValue = strings.TrimSpace(cleanValue)
		return fromStage, cleanValue
	}
	
	return "", value
}

func (p *Parser) processWorkdir(instruction *types.DockerfileInstruction) error {
	workdir := p.expandVariables(instruction.Value)
	
	if !filepath.IsAbs(workdir) {
		workdir = filepath.Join(p.workdir, workdir)
	}
	
	p.workdir = workdir
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		Metadata: map[string]string{
			"workdir": workdir,
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processEnv(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	envVars := p.parseEnvArgs(value)
	
	for key, val := range envVars {
		p.environment[key] = val
	}
	
	op := &types.Operation{
		Type:        types.OperationTypeMeta,
		Environment: p.copyEnvironment(),
		Metadata: map[string]string{
			"type": "env",
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processExpose(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	ports := strings.Fields(value)
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		Metadata: map[string]string{
			"expose": strings.Join(ports, ","),
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processCmd(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	command := p.parseCommand(value)
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		Command: command,
		Metadata: map[string]string{
			"cmd": strings.Join(command, " "),
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processEntrypoint(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	command := p.parseCommand(value)
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		Command: command,
		Metadata: map[string]string{
			"entrypoint": strings.Join(command, " "),
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processVolume(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	volumes := p.parseVolumeArgs(value)
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		Metadata: map[string]string{
			"volume": strings.Join(volumes, ","),
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processUser(instruction *types.DockerfileInstruction) error {
	user := p.expandVariables(instruction.Value)
	p.user = user
	
	op := &types.Operation{
		Type: types.OperationTypeMeta,
		User: user,
		Metadata: map[string]string{
			"user": user,
		},
		Inputs:  p.getLastOutput(),
		Outputs: []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) processArg(instruction *types.DockerfileInstruction) error {
	value := instruction.Value
	
	var key, defaultValue string
	if strings.Contains(value, "=") {
		parts := strings.SplitN(value, "=", 2)
		key = parts[0]
		defaultValue = parts[1]
	} else {
		key = value
	}
	
	if val, exists := p.buildArgs[key]; exists {
		p.environment[key] = val
	} else if defaultValue != "" {
		p.environment[key] = defaultValue
	}
	
	return nil
}

func (p *Parser) processLabel(instruction *types.DockerfileInstruction) error {
	value := p.expandVariables(instruction.Value)
	labels := p.parseLabelArgs(value)
	
	metadata := map[string]string{"type": "label"}
	for key, val := range labels {
		metadata["label."+key] = val
	}
	
	op := &types.Operation{
		Type:     types.OperationTypeMeta,
		Metadata: metadata,
		Inputs:   p.getLastOutput(),
		Outputs:  []string{fmt.Sprintf("meta-%d", len(p.operations))},
	}
	
	p.operations = append(p.operations, op)
	return nil
}

func (p *Parser) expandVariables(input string) string {
	return types.ExpandVariables(input, p.environment)
}

func (p *Parser) copyEnvironment() map[string]string {
	env := make(map[string]string)
	for k, v := range p.environment {
		env[k] = v
	}
	return env
}

func (p *Parser) getLastOutput() []string {
	if len(p.operations) == 0 {
		return []string{}
	}
	return p.operations[len(p.operations)-1].Outputs
}

func (p *Parser) parseCommand(value string) []string {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return p.parseJSONArray(value)
	}
	return []string{"/bin/sh", "-c", value}
}

func (p *Parser) parseJSONArray(value string) []string {
	value = strings.TrimSpace(value)
	value = value[1 : len(value)-1]
	
	if value == "" {
		return []string{}
	}
	
	var result []string
	parts := strings.Split(value, ",")
	
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "\"") && strings.HasSuffix(part, "\"") {
			part = part[1 : len(part)-1]
		}
		result = append(result, part)
	}
	
	return result
}

func (p *Parser) parseFileArgs(value string) []string {
	re := regexp.MustCompile(`"([^"]+)"|(\S+)`)
	matches := re.FindAllStringSubmatch(value, -1)
	
	var result []string
	for _, match := range matches {
		if match[1] != "" {
			result = append(result, match[1])
		} else {
			result = append(result, match[2])
		}
	}
	
	return result
}

func (p *Parser) parseEnvArgs(value string) map[string]string {
	env := make(map[string]string)
	
	if strings.Contains(value, "=") {
		parts := strings.SplitN(value, " ", -1)
		for _, part := range parts {
			if strings.Contains(part, "=") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 {
					key := strings.TrimSpace(kv[0])
					val := strings.TrimSpace(kv[1])
					if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
						val = val[1 : len(val)-1]
					}
					env[key] = val
				}
			}
		}
	} else {
		parts := strings.Fields(value)
		if len(parts) >= 2 {
			env[parts[0]] = strings.Join(parts[1:], " ")
		}
	}
	
	return env
}

func (p *Parser) parseVolumeArgs(value string) []string {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return p.parseJSONArray(value)
	}
	return strings.Fields(value)
}

func (p *Parser) parseLabelArgs(value string) map[string]string {
	labels := make(map[string]string)
	
	parts := strings.Fields(value)
	for _, part := range parts {
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				val := strings.TrimSpace(kv[1])
				if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
					val = val[1 : len(val)-1]
				}
				labels[key] = val
			}
		}
	}
	
	return labels
}

// identifyStages parses instructions and groups them into stages
func (p *Parser) identifyStages(instructions []*types.DockerfileInstruction) error {
	var currentStage *types.BuildStage
	stageIndex := 0
	
	for _, instruction := range instructions {
		if instruction.Command == "FROM" {
			// Create new stage
			stageName := fmt.Sprintf("stage-%d", stageIndex)
			baseImage := ""
			
			// Parse FROM instruction to extract image and optional alias
			value := p.expandVariables(instruction.Value)
			parts := strings.Fields(value)
			
			if len(parts) == 0 {
				return fmt.Errorf("FROM instruction requires an image at line %d", instruction.Line)
			}
			
			baseImage = parts[0]
			
			// Check for AS clause
			if len(parts) >= 3 && strings.ToUpper(parts[1]) == "AS" {
				stageName = parts[2]
			}
			
			currentStage = &types.BuildStage{
				Name:         stageName,
				Index:        stageIndex,
				BaseImage:    baseImage,
				Instructions: []*types.DockerfileInstruction{},
				Operations:   []*types.Operation{},
				Dependencies: []string{},
				IsFinal:      false, // Will be set later
			}
			
			p.multiStageContext.Stages = append(p.multiStageContext.Stages, currentStage)
			p.multiStageContext.StagesByName[stageName] = currentStage
			stageIndex++
		}
		
		if currentStage == nil {
			return fmt.Errorf("instruction before FROM at line %d", instruction.Line)
		}
		
		currentStage.Instructions = append(currentStage.Instructions, instruction)
	}
	
	if len(p.multiStageContext.Stages) == 0 {
		return fmt.Errorf("no FROM instruction found")
	}
	
	// Mark the last stage as final
	lastStage := p.multiStageContext.Stages[len(p.multiStageContext.Stages)-1]
	lastStage.IsFinal = true
	p.multiStageContext.FinalStage = lastStage
	
	return nil
}

// resetStageContext resets parser state for a new stage
func (p *Parser) resetStageContext() {
	p.environment = make(map[string]string)
	p.workdir = "/"
	p.user = "root"
	p.operations = []*types.Operation{}
}

// resolveStageReferences analyzes COPY --from instructions to build stage dependencies
func (p *Parser) resolveStageReferences() error {
	for _, stage := range p.multiStageContext.Stages {
		for _, instruction := range stage.Instructions {
			if instruction.Command == "COPY" {
				// Check for --from flag
				if fromStage := p.extractFromStage(instruction.Value); fromStage != "" {
					// Resolve numeric references immediately
					resolvedStage := fromStage
					if stageIndex := p.parseStageIndex(fromStage); stageIndex >= 0 && stageIndex < len(p.multiStageContext.Stages) {
						resolvedStage = p.multiStageContext.Stages[stageIndex].Name
					}
					
					// Add dependency
					if !p.containsString(stage.Dependencies, resolvedStage) {
						stage.Dependencies = append(stage.Dependencies, resolvedStage)
					}
				}
			}
		}
	}
	
	// Validate that all referenced stages exist
	for _, stage := range p.multiStageContext.Stages {
		for _, dep := range stage.Dependencies {
			if _, exists := p.multiStageContext.StagesByName[dep]; !exists {
				return fmt.Errorf("stage '%s' referenced in stage '%s' does not exist", dep, stage.Name)
			}
		}
	}
	
	return nil
}

// extractFromStage extracts the stage name from a COPY --from instruction
func (p *Parser) extractFromStage(value string) string {
	// Look for --from=stage pattern
	re := regexp.MustCompile(`--from=([^\s]+)`)
	matches := re.FindStringSubmatch(value)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// parseStageIndex parses a numeric stage reference
func (p *Parser) parseStageIndex(ref string) int {
	if idx := strings.TrimSpace(ref); idx != "" {
		var index int
		if n, err := fmt.Sscanf(idx, "%d", &index); err == nil && n == 1 {
			return index
		}
	}
	return -1
}

// getFinalOperations returns the operations for the final build
func (p *Parser) getFinalOperations() []*types.Operation {
	if p.multiStageContext.FinalStage == nil {
		fmt.Printf("Debug: No final stage found\n")
		return []*types.Operation{}
	}
	
	fmt.Printf("Debug: Final stage: %s with %d operations\n", p.multiStageContext.FinalStage.Name, len(p.multiStageContext.FinalStage.Operations))
	
	// For multi-stage builds, we need to include operations from all dependent stages
	allOperations := []*types.Operation{}
	processedStages := make(map[string]bool)
	
	// Recursively collect operations from dependencies
	p.collectStageOperations(p.multiStageContext.FinalStage, &allOperations, processedStages)
	
	fmt.Printf("Debug: Total operations collected: %d\n", len(allOperations))
	return allOperations
}

// collectStageOperations recursively collects operations from a stage and its dependencies
func (p *Parser) collectStageOperations(stage *types.BuildStage, allOperations *[]*types.Operation, processed map[string]bool) {
	if processed[stage.Name] {
		return
	}
	
	// First, process dependencies
	for _, depName := range stage.Dependencies {
		if depStage, exists := p.multiStageContext.StagesByName[depName]; exists {
			p.collectStageOperations(depStage, allOperations, processed)
		}
	}
	
	// Then add this stage's operations
	for _, op := range stage.Operations {
		// Add stage metadata to operations
		if op.Metadata == nil {
			op.Metadata = make(map[string]string)
		}
		op.Metadata["stage"] = stage.Name
		op.Metadata["stage_index"] = fmt.Sprintf("%d", stage.Index)
		op.Metadata["is_final"] = fmt.Sprintf("%t", stage.IsFinal)
		
		*allOperations = append(*allOperations, op)
	}
	
	processed[stage.Name] = true
}

// Helper functions
func (p *Parser) containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (p *Parser) replaceString(slice []string, old, new string) []string {
	result := make([]string, len(slice))
	for i, s := range slice {
		if s == old {
			result[i] = new
		} else {
			result[i] = s
		}
	}
	return result
}