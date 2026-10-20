package config

import (
	"strings"
	"testing"
)

func TestParseAndValidateSecurity(t *testing.T) {
	t.Run("Valid build config", func(t *testing.T) {
		validJSON := []byte(`{
			"region": "us-east-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0123456789abcdef0",
			"ami_name": "valid-ami-name_01",
			"ssh_username": "ubuntu"
		}`)
		cfg, err := ParseAndValidate(validJSON)
		if err != nil {
			t.Fatalf("expected valid config, got error: %v", err)
		}
		if cfg.AMIName != "valid-ami-name_01" {
			t.Errorf("expected AMIName 'valid-ami-name_01', got %q", cfg.AMIName)
		}
	})

	t.Run("Path traversal in ami_name rejected", func(t *testing.T) {
		payload := []byte(`{
			"region": "us-east-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0123456789abcdef0",
			"ami_name": "../../etc/passwd"
		}`)
		_, err := ParseAndValidate(payload)
		if err == nil {
			t.Fatal("expected error on path traversal in ami_name, got nil")
		}
		if !strings.Contains(err.Error(), "ami_name") {
			t.Errorf("expected error message to mention ami_name, got: %v", err)
		}
	})

	t.Run("Shell metacharacters in ami_name rejected", func(t *testing.T) {
		payload := []byte(`{
			"region": "us-east-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0123456789abcdef0",
			"ami_name": "test; rm -rf /"
		}`)
		_, err := ParseAndValidate(payload)
		if err == nil {
			t.Fatal("expected error on shell metacharacters in ami_name, got nil")
		}
	})

	t.Run("Invalid region rejected", func(t *testing.T) {
		payload := []byte(`{
			"region": "us/east/1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0123456789abcdef0",
			"ami_name": "valid-name"
		}`)
		_, err := ParseAndValidate(payload)
		if err == nil {
			t.Fatal("expected error on invalid region, got nil")
		}
	})

	t.Run("Invalid instance_type rejected", func(t *testing.T) {
		payload := []byte(`{
			"region": "us-east-1",
			"instance_type": "t3;shutdown",
			"source_ami": "ami-0123456789abcdef0",
			"ami_name": "valid-name"
		}`)
		_, err := ParseAndValidate(payload)
		if err == nil {
			t.Fatal("expected error on invalid instance_type, got nil")
		}
	})
}
