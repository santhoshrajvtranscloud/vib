package packer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh/vib/internal/config"
	"github.com/santhosh/vib/internal/generator"
)

func TestParseManifest(t *testing.T) {
	manifestJSON := []byte(`{
		"builds": [
			{
				"name": "vib-build",
				"builder_type": "amazon-ebs",
				"build_time": 1729074321,
				"files": null,
				"artifact_id": "ap-south-1:ami-0f58b397bc5c1f2e8",
				"packer_run_uuid": "e5b88f34-1234-5678-90ab-cdef12345678",
				"custom_data": null
			}
		],
		"last_run_uuid": "e5b88f34-1234-5678-90ab-cdef12345678"
	}`)

	manifest, err := ParseManifest(manifestJSON)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Builds) != 1 {
		t.Fatalf("expected 1 build, got %d", len(manifest.Builds))
	}

	amiID, regionMap, err := manifest.ExtractAMIInfo()
	if err != nil {
		t.Fatalf("ExtractAMIInfo failed: %v", err)
	}

	expectedAMI := "ami-0f58b397bc5c1f2e8"
	if amiID != expectedAMI {
		t.Errorf("expected AMI ID %q, got %q", expectedAMI, amiID)
	}

	if regionMap["ap-south-1"] != expectedAMI {
		t.Errorf("expected region ap-south-1 AMI %q, got %q", expectedAMI, regionMap["ap-south-1"])
	}
}

func TestParseManifestMultiRegion(t *testing.T) {
	manifestJSON := []byte(`{
		"builds": [
			{
				"name": "vib-build",
				"builder_type": "amazon-ebs",
				"build_time": 1729074321,
				"artifact_id": "us-east-1:ami-11111111111111111,us-west-2:ami-22222222222222222",
				"packer_run_uuid": "run-1"
			}
		],
		"last_run_uuid": "run-1"
	}`)

	manifest, err := ParseManifest(manifestJSON)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	amiID, regionMap, err := manifest.ExtractAMIInfo()
	if err != nil {
		t.Fatalf("ExtractAMIInfo failed: %v", err)
	}

	if amiID != "ami-11111111111111111" {
		t.Errorf("expected primary AMI ami-11111111111111111, got %q", amiID)
	}
	if regionMap["us-east-1"] != "ami-11111111111111111" {
		t.Errorf("expected us-east-1 ami-11111111111111111, got %q", regionMap["us-east-1"])
	}
	if regionMap["us-west-2"] != "ami-22222222222222222" {
		t.Errorf("expected us-west-2 ami-22222222222222222, got %q", regionMap["us-west-2"])
	}
}

func TestParseManifestMultipleBuildsMatchesLastRunUUID(t *testing.T) {
	manifestJSON := []byte(`{
		"builds": [
			{
				"name": "build-1",
				"builder_type": "amazon-ebs",
				"artifact_id": "ap-south-1:ami-oldoldoldoldold",
				"packer_run_uuid": "uuid-1"
			},
			{
				"name": "build-2",
				"builder_type": "amazon-ebs",
				"artifact_id": "ap-south-1:ami-newnewnewnewnew",
				"packer_run_uuid": "uuid-2"
			}
		],
		"last_run_uuid": "uuid-2"
	}`)

	manifest, err := ParseManifest(manifestJSON)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	amiID, _, err := manifest.ExtractAMIInfo()
	if err != nil {
		t.Fatalf("ExtractAMIInfo failed: %v", err)
	}
	if amiID != "ami-newnewnewnewnew" {
		t.Errorf("expected ami-newnewnewnewnew, got %q", amiID)
	}
}

func TestParseArtifactID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedAMI string
		expectedMap map[string]string
	}{
		{
			name:        "Standard region:ami",
			input:       "ap-south-1:ami-0f58b397bc5c1f2e8",
			expectedAMI: "ami-0f58b397bc5c1f2e8",
			expectedMap: map[string]string{"ap-south-1": "ami-0f58b397bc5c1f2e8"},
		},
		{
			name:        "Bare AMI without region",
			input:       "ami-0f58b397bc5c1f2e8",
			expectedAMI: "ami-0f58b397bc5c1f2e8",
			expectedMap: map[string]string{"default": "ami-0f58b397bc5c1f2e8"},
		},
		{
			name:        "Multiple regions",
			input:       "eu-west-1:ami-1234567890abcdef1,eu-central-1:ami-1234567890abcdef2",
			expectedAMI: "ami-1234567890abcdef1",
			expectedMap: map[string]string{
				"eu-west-1":    "ami-1234567890abcdef1",
				"eu-central-1": "ami-1234567890abcdef2",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ami, rmap := ParseArtifactID(tc.input)
			if ami != tc.expectedAMI {
				t.Errorf("expected AMI %q, got %q", tc.expectedAMI, ami)
			}
			for k, v := range tc.expectedMap {
				if rmap[k] != v {
					t.Errorf("expected region %q to map to %q, got %q", k, v, rmap[k])
				}
			}
		})
	}
}

func TestPrepareWorkDir(t *testing.T) {
	r := NewRunner()
	cfg := &config.BuildConfig{
		BuildID:      "test-123",
		Region:       "ap-south-1",
		InstanceType: "t3.micro",
		SourceAMI:    "ami-0f58b397bc5c1f2e8",
		SSHUsername:  "ubuntu",
		AMIName:      "test-ami",
	}

	hclBytes, err := generator.GenerateHCL(cfg)
	if err != nil {
		t.Fatalf("GenerateHCL failed: %v", err)
	}

	workDir, cleanup, err := r.PrepareWorkDir(cfg, hclBytes, "")
	if err != nil {
		t.Fatalf("PrepareWorkDir failed: %v", err)
	}

	hclFile := filepath.Join(workDir, HCLFileName)
	if _, err := os.Stat(hclFile); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", hclFile)
	}

	content, err := os.ReadFile(hclFile)
	if err != nil {
		t.Fatalf("failed to read written HCL file: %v", err)
	}
	if !strings.Contains(string(content), "vib-build") {
		t.Errorf("expected HCL file to contain 'vib-build'")
	}

	cleanup()
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("expected workDir to be cleaned up")
	}
}

func TestRealPackerInitAndValidate(t *testing.T) {
	r := NewRunner()
	cfg := &config.BuildConfig{
		BuildID:      "vib-test-validate",
		Region:       "ap-south-1",
		InstanceType: "t3.micro",
		SourceAMI:    "ami-0f58b397bc5c1f2e8",
		SSHUsername:  "ubuntu",
		AMIName:      "vib-test-ami",
		Packages:     []string{"nginx"},
	}

	hclBytes, err := generator.GenerateHCL(cfg)
	if err != nil {
		t.Fatalf("GenerateHCL failed: %v", err)
	}

	workDir, cleanup, err := r.PrepareWorkDir(cfg, hclBytes, "")
	if err != nil {
		t.Fatalf("PrepareWorkDir failed: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var logs []string
	onLog := func(line string) {
		logs = append(logs, line)
	}

	// 1. Test Init (can be silent if plugins are already cached)
	initOut, err := r.Init(ctx, workDir, onLog)
	if err != nil {
		t.Fatalf("Init failed: %v\nOutput: %s", err, initOut)
	}

	// 2. Test Validate
	valOut, err := r.Validate(ctx, workDir, onLog)
	if err != nil {
		t.Fatalf("Validate failed: %v\nOutput: %s", err, valOut)
	}
	if !strings.Contains(valOut, "The configuration is valid.") {
		t.Errorf("expected validation success message, got: %s", valOut)
	}
	if len(logs) == 0 {
		t.Errorf("expected log lines to be received via onLog during validate")
	}
}

func TestRealPackerVersion(t *testing.T) {
	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	version, err := r.Version(ctx)
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if !strings.Contains(version, "Packer") && !strings.Contains(version, "v1.") {
		t.Errorf("unexpected version string: %q", version)
	}
}

func TestValidateConfig(t *testing.T) {
	r := NewRunner()
	cfg := &config.BuildConfig{
		BuildID:      "vib-val-config",
		Region:       "ap-south-1",
		InstanceType: "t3.micro",
		SourceAMI:    "ami-0f58b397bc5c1f2e8",
		SSHUsername:  "ubuntu",
		AMIName:      "vib-test-cfg-validate",
		Packages:     []string{"git"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := r.ValidateConfig(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("ValidateConfig failed: %v", err)
	}
}

func TestBuildWithMockPacker(t *testing.T) {
	// Create a mock packer script to test the full Build lifecycle including log streaming and manifest parsing
	tmpDir := t.TempDir()
	mockScriptPath := filepath.Join(tmpDir, "mock-packer.sh")

	script := `#!/bin/sh
set -e
cmd="$1"
shift
case "$cmd" in
  init)
    echo "Installed plugin github.com/hashicorp/amazon"
    ;;
  validate)
    echo "The configuration is valid."
    ;;
  build)
    echo "==> amazon-ebs.vib: Prevalidating AMI Name..."
    echo "==> amazon-ebs.vib: Creating temporary keypair..."
    echo "==> amazon-ebs.vib: Launching source EC2 instance..."
    echo "==> amazon-ebs.vib: Stopping source instance..."
    echo "==> amazon-ebs.vib: Creating AMI vib-demo-ami..."
    cat << 'EOF' > manifest.json
{
  "builds": [
    {
      "name": "vib-build",
      "builder_type": "amazon-ebs",
      "build_time": 1729074321,
      "artifact_id": "ap-south-1:ami-0123456789mockami",
      "packer_run_uuid": "mock-run-uuid-123"
    }
  ],
  "last_run_uuid": "mock-run-uuid-123"
}
EOF
    echo "==> amazon-ebs.vib: Cleaned up temporary keypair"
    ;;
  *)
    echo "Unknown command: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(mockScriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create mock script: %v", err)
	}

	r := NewRunner(mockScriptPath)

	cfg := &config.BuildConfig{
		BuildID:      "mock-test-01",
		Region:       "ap-south-1",
		InstanceType: "t3.micro",
		SourceAMI:    "ami-0f58b397bc5c1f2e8",
		SSHUsername:  "ubuntu",
		AMIName:      "vib-mock-demo",
		Packages:     []string{"nginx"},
	}

	var streamedLogs []string
	var structuredLogs []LogEntry

	opts := BuildOptions{
		Config:      cfg,
		KeepWorkDir: false,
		OnLog: func(line string) {
			streamedLogs = append(streamedLogs, line)
		},
		OnLogEntry: func(entry LogEntry) {
			structuredLogs = append(structuredLogs, entry)
		},
	}

	ctx := context.Background()
	result, err := r.Build(ctx, opts)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected result.Success to be true")
	}
	if result.BuildID != "mock-test-01" {
		t.Errorf("expected build ID 'mock-test-01', got %q", result.BuildID)
	}
	if result.AMIID != "ami-0123456789mockami" {
		t.Errorf("expected AMI ID 'ami-0123456789mockami', got %q", result.AMIID)
	}
	if result.RegionToAMI["ap-south-1"] != "ami-0123456789mockami" {
		t.Errorf("expected region ap-south-1 to map to 'ami-0123456789mockami', got %q", result.RegionToAMI["ap-south-1"])
	}
	if len(streamedLogs) == 0 {
		t.Errorf("expected streamed logs, got none")
	}
	if len(structuredLogs) == 0 {
		t.Errorf("expected structured logs, got none")
	}

	// Verify that streamed logs captured both command output and headers
	combined := strings.Join(streamedLogs, "\n")
	if !strings.Contains(combined, "Initializing Packer plugins") {
		t.Errorf("expected log to contain init header")
	}
	if !strings.Contains(combined, "Prevalidating AMI Name") {
		t.Errorf("expected log to contain simulated build log")
	}
	if !strings.Contains(combined, "Build successful! AMI ID: ami-0123456789mockami") {
		t.Errorf("expected log to contain success message")
	}
}

func TestParseMachineReadableLine(t *testing.T) {
	line := `1612345678,amazon-ebs.vib,ui,say,Prevalidating AMI Name...`
	event, ok := ParseMachineReadableLine(line)
	if !ok {
		t.Fatalf("expected ParseMachineReadableLine to succeed")
	}
	if event.Timestamp != "1612345678" {
		t.Errorf("expected timestamp 1612345678, got %q", event.Timestamp)
	}
	if event.Target != "amazon-ebs.vib" {
		t.Errorf("expected target amazon-ebs.vib, got %q", event.Target)
	}
	if event.Type != "ui" {
		t.Errorf("expected type ui, got %q", event.Type)
	}
	if event.Message != "Prevalidating AMI Name..." {
		t.Errorf("expected message 'Prevalidating AMI Name...', got %q", event.Message)
	}

	formatted := FormatMachineReadableMessage(line)
	if formatted != "Prevalidating AMI Name..." {
		t.Errorf("expected formatted message 'Prevalidating AMI Name...', got %q", formatted)
	}
}

func TestBinaryNotFound(t *testing.T) {
	r := NewRunner("/non/existent/packer/binary/path")
	ctx := context.Background()
	_, err := r.Init(ctx, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for non-existent binary, got nil")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("expected error to mention 'not found in PATH', got: %v", err)
	}
}

func TestCommandCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	sleepScriptPath := filepath.Join(tmpDir, "sleep-packer.sh")
	script := `#!/bin/sh
sleep 10
`
	if err := os.WriteFile(sleepScriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	r := NewRunner(sleepScriptPath)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.runCommand(ctx, tmpDir, nil, nil, nil, "dummy")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected command to be cancelled, but got nil error")
	}
	if elapsed > 3*time.Second {
		t.Errorf("expected fast cancellation, took %v", elapsed)
	}
}

func TestSanitizeLogMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AWS_SECRET_ACCESS_KEY assignment",
			input:    "export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY and proceed",
			expected: "export AWS_SECRET_ACCESS_KEY: [REDACTED] and proceed",
		},
		{
			name:     "secret_access_key labeled key",
			input:    "secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expected: "secret_access_key: [REDACTED]",
		},
		{
			name:     "Clean log without credentials",
			input:    "==> amazon-ebs.vib: Prevalidating AMI Name...",
			expected: "==> amazon-ebs.vib: Prevalidating AMI Name...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := SanitizeLogMessage(tc.input)
			if output != tc.expected {
				t.Errorf("got %q, want %q", output, tc.expected)
			}
		})
	}
}
