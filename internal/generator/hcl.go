package generator

import (
	"fmt"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/santhosh/vib/internal/config"
	"github.com/zclconf/go-cty/cty"
)

var PackageCatalogue = map[string][]string{
	"nginx": {
		"sudo apt-get update -y",
		"sudo apt-get install -y nginx",
		"sudo systemctl enable nginx",
	},
	"docker": {
		"sudo apt-get update -y",
		"sudo apt-get install -y docker.io",
		"sudo systemctl enable --now docker",
		"sudo usermod -aG docker ubuntu || true",
	},
	"git": {
		"sudo apt-get update -y",
		"sudo apt-get install -y git",
	},
}

func GenerateHCL(cfg *config.BuildConfig) ([]byte, error) {
	f := hclwrite.NewEmptyFile()
	rootBody := f.Body()

	packerBlock := rootBody.AppendNewBlock("packer", nil)
	reqPluginsBlock := packerBlock.Body().AppendNewBlock("required_plugins", nil)
	reqPluginsBlock.Body().SetAttributeValue("amazon", cty.ObjectVal(map[string]cty.Value{
		"version": cty.StringVal(">= 1.2.8"),
		"source":  cty.StringVal("github.com/hashicorp/amazon"),
	}))
	rootBody.AppendNewline()

	sourceBlock := rootBody.AppendNewBlock("source", []string{"amazon-ebs", "vib"})
	sourceBody := sourceBlock.Body()

	amiNameWithTimestamp := fmt.Sprintf("%s-{{timestamp}}", cfg.AMIName)
	sourceBody.SetAttributeValue("ami_name", cty.StringVal(amiNameWithTimestamp))
	sourceBody.SetAttributeValue("instance_type", cty.StringVal(cfg.InstanceType))
	sourceBody.SetAttributeValue("region", cty.StringVal(cfg.Region))
	sourceBody.SetAttributeValue("source_ami", cty.StringVal(cfg.SourceAMI))
	sourceBody.SetAttributeValue("ssh_username", cty.StringVal(cfg.SSHUsername))

	buildID := cfg.BuildID
	if buildID == "" {
		buildID = "local-dev"
	}
	sourceBody.SetAttributeValue("tags", cty.ObjectVal(map[string]cty.Value{
		"Name":         cty.StringVal(cfg.AMIName),
		"CreatedBy":    cty.StringVal("VIB"),
		"VIB_Build_ID": cty.StringVal(buildID),
	}))
	rootBody.AppendNewline()

	buildBlock := rootBody.AppendNewBlock("build", nil)
	buildBody := buildBlock.Body()
	buildBody.SetAttributeValue("name", cty.StringVal("vib-build"))
	buildBody.SetAttributeValue("sources", cty.ListVal([]cty.Value{
		cty.StringVal("source.amazon-ebs.vib"),
	}))

	var inlineCmds []cty.Value
	for _, pkg := range cfg.Packages {
		if cmds, exists := PackageCatalogue[pkg]; exists {
			for _, cmd := range cmds {
				inlineCmds = append(inlineCmds, cty.StringVal(cmd))
			}
		} else {
			inlineCmds = append(inlineCmds, cty.StringVal(fmt.Sprintf("sudo apt-get install -y %s", pkg)))
		}
	}
	for _, customCmd := range cfg.CustomScript {
		inlineCmds = append(inlineCmds, cty.StringVal(customCmd))
	}

	if len(inlineCmds) > 0 {
		buildBody.AppendNewline()
		provBlock := buildBody.AppendNewBlock("provisioner", []string{"shell"})
		provBlock.Body().SetAttributeValue("inline", cty.ListVal(inlineCmds))
	}

	buildBody.AppendNewline()
	postProcBlock := buildBody.AppendNewBlock("post-processor", []string{"manifest"})
	postProcBlock.Body().SetAttributeValue("output", cty.StringVal("manifest.json"))
	postProcBlock.Body().SetAttributeRaw("strip_path", hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("true")},
	})

	return hclwrite.Format(f.Bytes()), nil
}
