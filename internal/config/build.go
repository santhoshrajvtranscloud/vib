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
	VPCID        string   `json:"vpc_id,omitempty"`
	SubnetID     string   `json:"subnet_id,omitempty"`
}

var (
	amiRegex          = regexp.MustCompile(`^ami-[a-z0-9]{8,17}$`)
	amiNameRegex      = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,128}$`)
	regionRegex       = regexp.MustCompile(`^[a-z0-9-]+$`)
	instanceTypeRegex = regexp.MustCompile(`^[a-z0-9]+\.[a-z0-9]+$`)
	vpcRegex          = regexp.MustCompile(`^vpc-[a-z0-9]{8,17}$`)
	subnetRegex       = regexp.MustCompile(`^subnet-[a-z0-9]{8,17}$`)
)

func ParseAndValidate(data []byte) (*BuildConfig, error) {
	var cfg BuildConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}
	if strings.TrimSpace(cfg.SSHUsername) == "" {
		cfg.SSHUsername = "ubuntu"
	}
	if strings.TrimSpace(cfg.Region) == "" || !regionRegex.MatchString(cfg.Region) {
		return nil, errors.New("region is required and must be a valid AWS region identifier")
	}
	if strings.TrimSpace(cfg.InstanceType) == "" || !instanceTypeRegex.MatchString(cfg.InstanceType) {
		return nil, errors.New("instance_type is required and must be a valid EC2 instance type (e.g. t3.micro)")
	}
	if !amiRegex.MatchString(cfg.SourceAMI) {
		return nil, errors.New("source_ami must be a valid AMI ID starting with 'ami-'")
	}
	if strings.TrimSpace(cfg.AMIName) == "" || !amiNameRegex.MatchString(cfg.AMIName) {
		return nil, errors.New("ami_name is required and must only contain alphanumeric characters, hyphens, and underscores (1-128 chars)")
	}
	if strings.TrimSpace(cfg.VPCID) != "" && !vpcRegex.MatchString(strings.TrimSpace(cfg.VPCID)) {
		return nil, errors.New("vpc_id must be a valid VPC ID starting with 'vpc-'")
	}
	if strings.TrimSpace(cfg.SubnetID) != "" && !subnetRegex.MatchString(strings.TrimSpace(cfg.SubnetID)) {
		return nil, errors.New("subnet_id must be a valid Subnet ID starting with 'subnet-'")
	}
	return &cfg, nil
}
