package generator

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/santhosh/vib/internal/config"
	"github.com/zclconf/go-cty/cty"
)

var aptInstallRegex = regexp.MustCompile(`(?:sudo\s+)?apt-get\s+install\s+(?:-[a-zA-Z]+\s+)*([a-zA-Z0-9_\-\.]+)`)

// ParseHCL parses Packer HCL configuration text into a config.BuildConfig struct.
func ParseHCL(hclContent string) (*config.BuildConfig, error) {
	if strings.TrimSpace(hclContent) == "" {
		return nil, errors.New("empty HCL content")
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL([]byte(hclContent), "input.pkr.hcl")
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL parse error: %s", diags.Error())
	}

	syntaxBody, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.New("failed to inspect HCL syntax tree")
	}

	cfg := &config.BuildConfig{
		Packages:     []string{},
		CustomScript: []string{},
	}

	var foundSource bool
	var inlineCmds []string

	for _, block := range syntaxBody.Blocks {
		switch block.Type {
		case "source":
			if len(block.Labels) > 0 && block.Labels[0] == "amazon-ebs" {
				foundSource = true
				extractSourceAttributes(block.Body, file.Bytes, cfg)
			}
		case "build":
			extractBuildCommands(block.Body, file.Bytes, &inlineCmds)
		}
	}

	if !foundSource {
		return nil, errors.New("no 'source \"amazon-ebs\"' block found in HCL")
	}

	// Apply default ssh_username if not set
	if strings.TrimSpace(cfg.SSHUsername) == "" {
		cfg.SSHUsername = "ubuntu"
	}

	// Classify inline commands into Packages and CustomScript
	pkgs, custom := classifyCommands(inlineCmds)
	cfg.Packages = pkgs
	cfg.CustomScript = custom

	return cfg, nil
}

// extractSourceAttributes pulls builder attributes from the amazon-ebs source block.
func extractSourceAttributes(body *hclsyntax.Body, fileBytes []byte, cfg *config.BuildConfig) {
	for name, attr := range body.Attributes {
		strVal := evalStringOrLiteral(attr.Expr, fileBytes)
		switch name {
		case "region":
			cfg.Region = strVal
		case "instance_type":
			cfg.InstanceType = strVal
		case "source_ami":
			cfg.SourceAMI = strVal
		case "ssh_username":
			cfg.SSHUsername = strVal
		case "vpc_id":
			cfg.VPCID = strVal
		case "subnet_id":
			cfg.SubnetID = strVal
		case "ami_name":
			// Strip trailing -{{timestamp}} or -{{ timestamp }} if present
			cleaned := strings.TrimSuffix(strVal, "-{{timestamp}}")
			cleaned = strings.TrimSuffix(cleaned, "-{{ timestamp }}")
			cfg.AMIName = cleaned
		case "tags":
			val, diags := attr.Expr.Value(nil)
			if !diags.HasErrors() && val.CanIterateElements() {
				for it := val.ElementIterator(); it.Next(); {
					k, v := it.Element()
					if k.AsString() == "VIB_Build_ID" && v.Type() == cty.String {
						id := v.AsString()
						if id != "local-dev" {
							cfg.BuildID = id
						}
					}
					if cfg.AMIName == "" && k.AsString() == "Name" && v.Type() == cty.String {
						cfg.AMIName = v.AsString()
					}
				}
			}
		}
	}
}

// extractBuildCommands extracts inline shell commands from provisioners inside a build block.
func extractBuildCommands(body *hclsyntax.Body, fileBytes []byte, inlineCmds *[]string) {
	for _, block := range body.Blocks {
		if block.Type == "provisioner" && len(block.Labels) > 0 && block.Labels[0] == "shell" {
			if inlineAttr, ok := block.Body.Attributes["inline"]; ok {
				val, diags := inlineAttr.Expr.Value(nil)
				if !diags.HasErrors() && val.CanIterateElements() {
					for it := val.ElementIterator(); it.Next(); {
						_, el := it.Element()
						if el.Type() == cty.String {
							*inlineCmds = append(*inlineCmds, el.AsString())
						}
					}
				} else if tupleExpr, ok := inlineAttr.Expr.(*hclsyntax.TupleConsExpr); ok {
					// Fallback for expressions that didn't evaluate cleanly
					for _, elem := range tupleExpr.Exprs {
						str := evalStringOrLiteral(elem, fileBytes)
						if str != "" {
							*inlineCmds = append(*inlineCmds, str)
						}
					}
				}
			}
		}
	}
}

type matchedPkg struct {
	pkg   string
	index int
}

// classifyCommands separates a slice of inline shell commands into catalogue packages and custom script commands.
func classifyCommands(cmds []string) ([]string, []string) {
	if len(cmds) == 0 {
		return []string{}, []string{}
	}

	consumed := make([]bool, len(cmds))
	var matches []matchedPkg

	// Catalogue package sequence lengths (match longest sequences first)
	type catEntry struct {
		name string
		cmds []string
	}
	var entries []catEntry
	for name, c := range PackageCatalogue {
		entries = append(entries, catEntry{name: name, cmds: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].cmds) > len(entries[j].cmds)
	})

	// Pass 1: Match full catalogue sequences
	for _, entry := range entries {
		seqLen := len(entry.cmds)
		for i := 0; i <= len(cmds)-seqLen; i++ {
			// Check if already consumed
			alreadyConsumed := false
			for k := 0; k < seqLen; k++ {
				if consumed[i+k] {
					alreadyConsumed = true
					break
				}
			}
			if alreadyConsumed {
				continue
			}

			// Check equality
			allMatch := true
			for k := 0; k < seqLen; k++ {
				if strings.TrimSpace(cmds[i+k]) != strings.TrimSpace(entry.cmds[k]) {
					allMatch = false
					break
				}
			}

			if allMatch {
				for k := 0; k < seqLen; k++ {
					consumed[i+k] = true
				}
				matches = append(matches, matchedPkg{pkg: entry.name, index: i})
			}
		}
	}

	// Pass 2: Match individual apt-get install commands for catalogue or other packages
	for i, cmd := range cmds {
		if consumed[i] {
			continue
		}
		trimmed := strings.TrimSpace(cmd)
		submatches := aptInstallRegex.FindStringSubmatch(trimmed)
		if len(submatches) > 1 {
			rawPkg := submatches[1]
			pkgName := rawPkg
			if rawPkg == "docker.io" {
				pkgName = "docker"
			}

			// Only treat as a catalogue package if it exists in PackageCatalogue
			if _, exists := PackageCatalogue[pkgName]; !exists {
				continue
			}

			consumed[i] = true
			// If preceding command was apt-get update and not consumed, consume it
			if i > 0 && !consumed[i-1] {
				if strings.Contains(cmds[i-1], "apt-get update") {
					consumed[i-1] = true
				}
			}
			// If subsequent command is systemctl enable for this package, consume it
			if i+1 < len(cmds) && !consumed[i+1] {
				if strings.Contains(cmds[i+1], "systemctl") && strings.Contains(cmds[i+1], pkgName) {
					consumed[i+1] = true
				}
			}

			matches = append(matches, matchedPkg{pkg: pkgName, index: i})
		}
	}

	// Pass 3: Collect unconsumed commands into custom script
	var customScript []string
	for i, cmd := range cmds {
		if !consumed[i] {
			customScript = append(customScript, cmd)
		}
	}

	// Pass 4: Sort packages by their appearance index
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].index < matches[j].index
	})

	var packages []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if !seen[m.pkg] {
			seen[m.pkg] = true
			packages = append(packages, m.pkg)
		}
	}

	if packages == nil {
		packages = []string{}
	}
	if customScript == nil {
		customScript = []string{}
	}

	return packages, customScript
}

// evalStringOrLiteral evaluates an HCL expression as a string, with fallback to raw source slice.
func evalStringOrLiteral(expr hclsyntax.Expression, fileBytes []byte) string {
	val, diags := expr.Value(nil)
	if !diags.HasErrors() && val.Type() == cty.String {
		return val.AsString()
	}

	rng := expr.Range()
	if int(rng.End.Byte) <= len(fileBytes) && int(rng.Start.Byte) < int(rng.End.Byte) {
		raw := string(fileBytes[rng.Start.Byte:rng.End.Byte])
		return strings.Trim(strings.TrimSpace(raw), `"'`)
	}

	return ""
}

// ValidateHCLSecurity performs an AST security audit on Packer HCL.
// It verifies that:
// 1. No local provisioners (e.g. shell-local, local-exec) are used to run arbitrary host commands.
// 2. All required plugins originate exclusively from trusted namespaces (github.com/hashicorp/*).
func ValidateHCLSecurity(hclBytes []byte) error {
	if len(strings.TrimSpace(string(hclBytes))) == 0 {
		return errors.New("empty HCL content")
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(hclBytes, "security-check.pkr.hcl")
	if diags.HasErrors() {
		return fmt.Errorf("HCL syntax error: %s", diags.Error())
	}

	syntaxBody, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return errors.New("failed to inspect HCL syntax tree")
	}

	return inspectBlocksSecurity(syntaxBody.Blocks, file.Bytes)
}

func inspectBlocksSecurity(blocks hclsyntax.Blocks, fileBytes []byte) error {
	for _, block := range blocks {
		// 1. Check provisioners
		if block.Type == "provisioner" {
			for _, label := range block.Labels {
				lower := strings.ToLower(label)
				if lower == "shell-local" || lower == "local-exec" || strings.Contains(lower, "local") {
					return fmt.Errorf("security violation: local provisioner %q is prohibited; execution is restricted to remote instances", label)
				}
			}
		}

		// 2. Check required_plugins inside packer block
		if block.Type == "packer" {
			for _, child := range block.Body.Blocks {
				if child.Type == "required_plugins" {
					if err := inspectRequiredPluginsSecurity(child.Body, fileBytes); err != nil {
						return err
					}
				}
			}
		}

		// Recursively inspect any nested child blocks
		if block.Body != nil && len(block.Body.Blocks) > 0 {
			if err := inspectBlocksSecurity(block.Body.Blocks, fileBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

func inspectRequiredPluginsSecurity(body *hclsyntax.Body, fileBytes []byte) error {
	if body == nil {
		return nil
	}

	// Case A: Plugin configured as attributes: amazon = { source = "...", version = "..." }
	for _, attr := range body.Attributes {
		if objExpr, ok := attr.Expr.(*hclsyntax.ObjectConsExpr); ok {
			for _, item := range objExpr.Items {
				key := evalStringOrLiteral(item.KeyExpr, fileBytes)
				if key == "source" {
					source := evalStringOrLiteral(item.ValueExpr, fileBytes)
					if source != "" && !strings.HasPrefix(source, "github.com/hashicorp/") {
						return fmt.Errorf("security violation: untrusted plugin source %q; only github.com/hashicorp/* plugins are permitted", source)
					}
				}
			}
		}
	}

	// Case B: Plugin configured as nested blocks: plugin "amazon" { source = "..." }
	for _, block := range body.Blocks {
		if srcAttr, exists := block.Body.Attributes["source"]; exists {
			source := evalStringOrLiteral(srcAttr.Expr, fileBytes)
			if source != "" && !strings.HasPrefix(source, "github.com/hashicorp/") {
				return fmt.Errorf("security violation: untrusted plugin source %q; only github.com/hashicorp/* plugins are permitted", source)
			}
		}
	}

	return nil
}
