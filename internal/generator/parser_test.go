package generator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh/vib/internal/config"
)

func TestParseHCLRoundTrip(t *testing.T) {
	testCases := []struct {
		name     string
		inputCfg *config.BuildConfig
	}{
		{
			name: "Nginx and Docker with Custom Script",
			inputCfg: &config.BuildConfig{
				AMIName:      "vib-app-server",
				Region:       "us-east-1",
				InstanceType: "t3.small",
				SourceAMI:    "ami-0c55b159cbfafe1f0",
				SSHUsername:  "ubuntu",
				BuildID:      "build-1234",
				Packages:     []string{"nginx", "docker"},
				CustomScript: []string{
					"echo 'Deploying application'",
					"mkdir -p /opt/app",
				},
			},
		},
		{
			name: "Git Only No Custom Script",
			inputCfg: &config.BuildConfig{
				AMIName:      "vib-git-builder",
				Region:       "ap-south-1",
				InstanceType: "t3.micro",
				SourceAMI:    "ami-0f58b397bc5c1f2e8",
				SSHUsername:  "ubuntu",
				BuildID:      "build-5678",
				Packages:     []string{"git"},
				CustomScript: []string{},
			},
		},
		{
			name: "Custom Script Only No Packages",
			inputCfg: &config.BuildConfig{
				AMIName:      "vib-custom-node",
				Region:       "eu-west-1",
				InstanceType: "c6i.large",
				SourceAMI:    "ami-0123456789abcdef0",
				SSHUsername:  "ec2-user",
				BuildID:      "build-9999",
				Packages:     []string{},
				CustomScript: []string{
					"curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -",
					"sudo apt-get install -y nodejs",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Step 1: Generate HCL
			hclBytes, err := GenerateHCL(tc.inputCfg)
			if err != nil {
				t.Fatalf("GenerateHCL failed: %v", err)
			}

			// Step 2: Parse back to BuildConfig
			parsedCfg, err := ParseHCL(string(hclBytes))
			if err != nil {
				t.Fatalf("ParseHCL failed: %v\nGenerated HCL:\n%s", err, string(hclBytes))
			}

			// Step 3: Compare fields
			if parsedCfg.AMIName != tc.inputCfg.AMIName {
				t.Errorf("AMIName mismatch: got %q, want %q", parsedCfg.AMIName, tc.inputCfg.AMIName)
			}
			if parsedCfg.Region != tc.inputCfg.Region {
				t.Errorf("Region mismatch: got %q, want %q", parsedCfg.Region, tc.inputCfg.Region)
			}
			if parsedCfg.InstanceType != tc.inputCfg.InstanceType {
				t.Errorf("InstanceType mismatch: got %q, want %q", parsedCfg.InstanceType, tc.inputCfg.InstanceType)
			}
			if parsedCfg.SourceAMI != tc.inputCfg.SourceAMI {
				t.Errorf("SourceAMI mismatch: got %q, want %q", parsedCfg.SourceAMI, tc.inputCfg.SourceAMI)
			}
			if parsedCfg.SSHUsername != tc.inputCfg.SSHUsername {
				t.Errorf("SSHUsername mismatch: got %q, want %q", parsedCfg.SSHUsername, tc.inputCfg.SSHUsername)
			}
			if tc.inputCfg.BuildID != "" && parsedCfg.BuildID != tc.inputCfg.BuildID {
				t.Errorf("BuildID mismatch: got %q, want %q", parsedCfg.BuildID, tc.inputCfg.BuildID)
			}

			// Packages check
			if len(tc.inputCfg.Packages) == 0 {
				if len(parsedCfg.Packages) != 0 {
					t.Errorf("Expected empty packages, got: %v", parsedCfg.Packages)
				}
			} else {
				if !reflect.DeepEqual(parsedCfg.Packages, tc.inputCfg.Packages) {
					t.Errorf("Packages mismatch: got %v, want %v", parsedCfg.Packages, tc.inputCfg.Packages)
				}
			}

			// CustomScript check (if there were non-package commands)
			if len(tc.inputCfg.CustomScript) == 0 {
				if len(parsedCfg.CustomScript) != 0 {
					t.Errorf("Expected empty custom script, got: %v", parsedCfg.CustomScript)
				}
			}
		})
	}
}

func TestParseHCLManualSnippets(t *testing.T) {
	manualHCL := `
packer {
  required_plugins {
    amazon = {
      version = ">= 1.2.8"
      source  = "github.com/hashicorp/amazon"
    }
  }
}

source "amazon-ebs" "custom" {
  ami_name      = "manual-ubuntu-server"
  instance_type = "t2.micro"
  region        = "us-west-2"
  source_ami    = "ami-0123456789abcdef0"
  ssh_username  = "admin"
}

build {
  name    = "custom-build"
  sources = ["source.amazon-ebs.custom"]

  provisioner "shell" {
    inline = [
      "sudo apt-get update -y",
      "sudo apt-get install -y nginx",
      "echo 'Custom step 1'",
      "echo 'Custom step 2'"
    ]
  }
}
`
	cfg, err := ParseHCL(manualHCL)
	if err != nil {
		t.Fatalf("ParseHCL failed: %v", err)
	}

	if cfg.AMIName != "manual-ubuntu-server" {
		t.Errorf("expected AMIName 'manual-ubuntu-server', got %q", cfg.AMIName)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("expected Region 'us-west-2', got %q", cfg.Region)
	}
	if cfg.InstanceType != "t2.micro" {
		t.Errorf("expected InstanceType 't2.micro', got %q", cfg.InstanceType)
	}
	if cfg.SourceAMI != "ami-0123456789abcdef0" {
		t.Errorf("expected SourceAMI 'ami-0123456789abcdef0', got %q", cfg.SourceAMI)
	}
	if cfg.SSHUsername != "admin" {
		t.Errorf("expected SSHUsername 'admin', got %q", cfg.SSHUsername)
	}

	// Should have identified nginx
	if len(cfg.Packages) != 1 || cfg.Packages[0] != "nginx" {
		t.Errorf("expected Packages ['nginx'], got %v", cfg.Packages)
	}

	// Should have collected custom steps
	expectedCustom := []string{"echo 'Custom step 1'", "echo 'Custom step 2'"}
	if !reflect.DeepEqual(cfg.CustomScript, expectedCustom) {
		t.Errorf("expected CustomScript %v, got %v", expectedCustom, cfg.CustomScript)
	}
}

func TestAMINameTimestampStripping(t *testing.T) {
	cases := []struct {
		raw      string
		expected string
	}{
		{"my-ami-{{timestamp}}", "my-ami"},
		{"my-ami-{{ timestamp }}", "my-ami"},
		{"bare-ami-name", "bare-ami-name"},
	}

	for _, tc := range cases {
		hclSnippet := "source \"amazon-ebs\" \"test\" {\n ami_name = \"" + tc.raw + "\"\n region = \"us-east-1\"\n instance_type = \"t3.micro\"\n source_ami = \"ami-12345678\"\n}\n"
		parsed, err := ParseHCL(hclSnippet)
		if err != nil {
			t.Fatalf("ParseHCL failed for %q: %v", tc.raw, err)
		}
		if parsed.AMIName != tc.expected {
			t.Errorf("for raw %q: got %q, want %q", tc.raw, parsed.AMIName, tc.expected)
		}
	}
}

func TestParseHCLErrors(t *testing.T) {
	t.Run("Empty input", func(t *testing.T) {
		_, err := ParseHCL("")
		if err == nil {
			t.Error("expected error on empty input, got nil")
		}
	})

	t.Run("Malformed syntax", func(t *testing.T) {
		_, err := ParseHCL("source \"amazon-ebs\" { ami_name = ")
		if err == nil {
			t.Error("expected error on malformed HCL, got nil")
		}
		if !strings.Contains(err.Error(), "HCL parse error") {
			t.Errorf("expected error to mention HCL parse error, got: %v", err)
		}
	})

	t.Run("Missing source block", func(t *testing.T) {
		_, err := ParseHCL("build { sources = [] }")
		if err == nil {
			t.Error("expected error when amazon-ebs block is missing, got nil")
		}
	})
}
