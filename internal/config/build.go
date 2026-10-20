package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type BuildConfig struct {
	BuildID      string   `json:"build_id,omitempty"`
	Region       string   `json:"region"`
	InstanceType string   `json:"instance_type"`
	SourceAMI    string   `json:"source_ami"`
	SSHUsername  string   `json:"ssh_username"`
	AMIName      string   `json:"ami_name"`
	Packages     []string `json:"packages"`
	CustomScript []string `json:"custom_script,omitempty"`
}

var amiRegex = regexp.MustCompile(`^ami-[a-z0-9]{8,17}$`)

func ParseAndValidate(data []byte) (*BuildConfig, error) {
	var cfg BuildConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}
	if strings.TrimSpace(cfg.SSHUsername) == "" {
		cfg.SSHUsername = "ubuntu"
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("region is required")
	}
	if strings.TrimSpace(cfg.InstanceType) == "" {
		return nil, errors.New("instance_type is required")
	}
	if !amiRegex.MatchString(cfg.SourceAMI) {
		return nil, errors.New("source_ami must be a valid AMI ID starting with 'ami-'")
	}
	if strings.TrimSpace(cfg.AMIName) == "" {
		return nil, errors.New("ami_name is required")
	}
	return &cfg, nil
}
