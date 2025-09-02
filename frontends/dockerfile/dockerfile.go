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
	parser := &Parser{
		config:      config,
		buildArgs:   config.BuildArgs,
		environment: make(map[string]string),
		workdir:     "/",
		user:        "root",
	}

	return parser.Parse(dockerfileContent)
}

type Parser struct {
	config      *types.BuildConfig
	buildArgs   map[string]string
	environment map[string]string
	workdir     string
	user        string
	operations  []*types.Operation
}

func (p *Parser) Parse(content string) ([]*types.Operation, error) {
	lines := strings.Split(content, "\n")
	instructions, err := p.parseInstructions(lines)
	if err != nil {
		return nil, err
	}

	for _, instruction := range instructions {
		if err := p.processInstruction(instruction); err != nil {
			return nil, fmt.Errorf("error processing instruction at line %d: %v", instruction.Line, err)
		}
	}

	return p.operations, nil
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
	parts := p.parseFileArgs(value)
	
	if len(parts) < 2 {
		return fmt.Errorf("%s instruction requires at least source and destination", strings.ToUpper(operationType))
	}
	
	sources := parts[:len(parts)-1]
	dest := parts[len(parts)-1]
	
	for i, source := range sources {
		sources[i] = filepath.Join(p.config.Context, source)
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
	
	p.operations = append(p.operations, op)
	return nil
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