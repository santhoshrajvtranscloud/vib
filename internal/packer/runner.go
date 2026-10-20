package packer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/santhosh/vib/internal/config"
	"github.com/santhosh/vib/internal/generator"
)

const (
	HCLFileName      = "aws-build.pkr.hcl"
	ManifestFileName = "manifest.json"
)

var (
	amiRegex          = regexp.MustCompile(`ami-[a-z0-9]{8,17}`)
	sanitizeRegex     = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)
	secretKeyRegex    = regexp.MustCompile(`(?i)(secret[_-]?access[_-]?key|aws[_-]?secret)[\s:=]+([A-Za-z0-9/+=]{40})`)
	awsSecretValRegex = regexp.MustCompile(`(?i)(AWS_SECRET_ACCESS_KEY|AWS_SESSION_TOKEN)=([^\s]+)`)
)

// SanitizeLogMessage scrubs sensitive AWS credentials and session tokens from log output.
func SanitizeLogMessage(msg string) string {
	msg = secretKeyRegex.ReplaceAllString(msg, "$1: [REDACTED]")
	msg = awsSecretValRegex.ReplaceAllString(msg, "$1=[REDACTED]")
	return msg
}

// LogHandler is a callback function invoked when a new log line is received.
type LogHandler func(line string)

// LogEntry represents a single log line with timestamp and origin stream.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"` // "stdout", "stderr", "system"
	Message   string    `json:"message"`
}

// StructuredLogHandler is a callback invoked with structured log events (ideal for SSE / DB persistence).
type StructuredLogHandler func(entry LogEntry)

// BuildOptions configures the execution of a Packer build.
type BuildOptions struct {
	// Config is the build configuration. Used to generate HCL if HCLBytes is nil/empty.
	Config *config.BuildConfig
	// HCLBytes is the raw Packer HCL content. Overrides Config generation if provided.
	HCLBytes []byte
	// WorkDir specifies a custom directory to run the build in. If empty, a temp dir is created.
	WorkDir string
	// KeepWorkDir indicates whether to preserve the build directory after execution.
	KeepWorkDir bool
	// Env specifies additional environment variables (e.g. AWS credentials).
	Env []string
	// OnLog is an optional callback invoked for each line of stdout and stderr.
	OnLog LogHandler
	// OnLogEntry is an optional callback for structured log entries with stream and timestamps.
	OnLogEntry StructuredLogHandler
	// SkipValidate skips running 'packer validate' prior to 'packer build'.
	SkipValidate bool
	// MachineReadable enables '-machine-readable' flag on 'packer build'.
	MachineReadable bool
	// Color enables ANSI colored output (defaults to false for clean log streams).
	Color bool
}

// BuildResult contains the outcome of a Packer build execution.
type BuildResult struct {
	BuildID     string            `json:"build_id"`
	WorkDir     string            `json:"work_dir"`
	Manifest    *Manifest         `json:"manifest,omitempty"`
	AMIID       string            `json:"ami_id,omitempty"`
	RegionToAMI map[string]string `json:"region_to_ami,omitempty"`
	Output      string            `json:"output"`
	Duration    time.Duration     `json:"duration"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
}

// Manifest represents the output structure of Packer's manifest post-processor.
type Manifest struct {
	Builds      []ManifestBuild `json:"builds"`
	LastRunUUID string          `json:"last_run_uuid"`
}

// ManifestBuild represents an individual build entry in manifest.json.
type ManifestBuild struct {
	Name          string                 `json:"name"`
	BuilderType   string                 `json:"builder_type"`
	BuildTime     int64                  `json:"build_time"`
	Files         []ManifestFile         `json:"files,omitempty"`
	ArtifactID    string                 `json:"artifact_id"`
	PackerRunUUID string                 `json:"packer_run_uuid"`
	CustomData    map[string]interface{} `json:"custom_data,omitempty"`
}

// ManifestFile represents a file entry within a ManifestBuild.
type ManifestFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// MachineReadableEvent represents a parsed event from Packer's -machine-readable output.
type MachineReadableEvent struct {
	Timestamp string   `json:"timestamp"`
	Target    string   `json:"target,omitempty"`
	Type      string   `json:"type"`
	Data      []string `json:"data"`
	Message   string   `json:"message,omitempty"`
}

// Runner manages execution of HashiCorp Packer commands.
type Runner struct {
	PackerBinary string
}

// NewRunner creates a new Runner instance. If packerBinary is omitted, defaults to "packer".
func NewRunner(packerBinary ...string) *Runner {
	bin := "packer"
	if len(packerBinary) > 0 && strings.TrimSpace(packerBinary[0]) != "" {
		bin = strings.TrimSpace(packerBinary[0])
	}
	return &Runner{PackerBinary: bin}
}

func (r *Runner) packerBinary() string {
	if strings.TrimSpace(r.PackerBinary) == "" {
		return "packer"
	}
	return r.PackerBinary
}

// Version executes 'packer version' and returns the version string.
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, err := r.runCommand(ctx, "", nil, nil, nil, "version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// PrepareWorkDir creates a working directory, writes aws-build.pkr.hcl, and returns the path and a cleanup function.
func (r *Runner) PrepareWorkDir(cfg *config.BuildConfig, hclBytes []byte, customWorkDir string) (string, func(), error) {
	var workDir string
	var cleanup func()

	if customWorkDir != "" {
		workDir = customWorkDir
		if err := os.MkdirAll(workDir, 0750); err != nil {
			return "", nil, fmt.Errorf("failed to create custom working directory %q: %w", workDir, err)
		}
		cleanup = func() {}
	} else {
		prefix := "vib-build-"
		if cfg != nil && cfg.BuildID != "" {
			prefix = fmt.Sprintf("vib-build-%s-", sanitizeID(cfg.BuildID))
		}
		tempDir, err := os.MkdirTemp("", prefix)
		if err != nil {
			return "", nil, fmt.Errorf("failed to create temporary working directory: %w", err)
		}
		workDir = tempDir
		cleanup = func() {
			_ = os.RemoveAll(tempDir)
		}
	}

	hclFile := filepath.Join(workDir, HCLFileName)
	if err := os.WriteFile(hclFile, hclBytes, 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write %s: %w", HCLFileName, err)
	}

	return workDir, cleanup, nil
}

// Init runs 'packer init .' in the specified working directory.
func (r *Runner) Init(ctx context.Context, workDir string, onLog LogHandler, env ...string) (string, error) {
	return r.runCommand(ctx, workDir, onLog, nil, env, "init", ".")
}

// Validate runs 'packer validate .' in the specified working directory.
func (r *Runner) Validate(ctx context.Context, workDir string, onLog LogHandler, env ...string) (string, error) {
	return r.runCommand(ctx, workDir, onLog, nil, env, "validate", ".")
}

// ValidateConfig generates HCL from cfg, writes it to a temporary directory, and runs 'packer init' and 'packer validate'.
func (r *Runner) ValidateConfig(ctx context.Context, cfg *config.BuildConfig, onLog LogHandler, env ...string) error {
	hclBytes, err := generator.GenerateHCL(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate HCL: %w", err)
	}

	workDir, cleanup, err := r.PrepareWorkDir(cfg, hclBytes, "")
	if err != nil {
		return err
	}
	defer cleanup()

	if _, err := r.Init(ctx, workDir, onLog, env...); err != nil {
		return fmt.Errorf("packer init failed: %w", err)
	}

	if _, err := r.Validate(ctx, workDir, onLog, env...); err != nil {
		return fmt.Errorf("packer validate failed: %w", err)
	}

	return nil
}

// Build coordinates the complete build workflow: creates temp directory, writes aws-build.pkr.hcl,
// runs 'packer init', optionally 'packer validate', runs 'packer build', streams logs in real-time,
// and parses manifest.json for the created AMI ID.
func (r *Runner) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	startTime := time.Now()

	// 1. Resolve HCL content
	hclBytes := opts.HCLBytes
	if len(hclBytes) == 0 {
		if opts.Config == nil {
			return nil, errors.New("either Config or HCLBytes must be provided")
		}
		var err error
		hclBytes, err = generator.GenerateHCL(opts.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to generate HCL: %w", err)
		}
	}

	// 2. Resolve Build ID
	buildID := "local"
	if opts.Config != nil && opts.Config.BuildID != "" {
		buildID = opts.Config.BuildID
	}

	// 3. Prepare WorkDir and write aws-build.pkr.hcl
	workDir, cleanup, err := r.PrepareWorkDir(opts.Config, hclBytes, opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare build directory: %w", err)
	}
	if !opts.KeepWorkDir {
		defer cleanup()
	}

	result := &BuildResult{
		BuildID: buildID,
		WorkDir: workDir,
	}

	var allOutput strings.Builder

	emitLog := func(stream, message string) {
		allOutput.WriteString(message)
		allOutput.WriteString("\n")
		if opts.OnLog != nil {
			opts.OnLog(message)
		}
		if opts.OnLogEntry != nil {
			opts.OnLogEntry(LogEntry{
				Timestamp: time.Now(),
				Stream:    stream,
				Message:   message,
			})
		}
	}

	// 4. Run packer init
	emitLog("system", fmt.Sprintf("==> [%s] Initializing Packer plugins (packer init)...", buildID))
	initOut, err := r.runCommand(ctx, workDir, opts.OnLog, opts.OnLogEntry, opts.Env, "init", ".")
	if initOut != "" {
		allOutput.WriteString(initOut)
	}
	if err != nil {
		result.Output = allOutput.String()
		result.Duration = time.Since(startTime)
		result.Success = false
		result.Error = fmt.Sprintf("packer init failed: %v", err)
		return result, fmt.Errorf("packer init failed: %w", err)
	}

	// 5. Run packer validate (unless skipped)
	if !opts.SkipValidate {
		emitLog("system", fmt.Sprintf("==> [%s] Validating Packer configuration (packer validate)...", buildID))
		valOut, err := r.runCommand(ctx, workDir, opts.OnLog, opts.OnLogEntry, opts.Env, "validate", ".")
		if valOut != "" {
			allOutput.WriteString(valOut)
		}
		if err != nil {
			result.Output = allOutput.String()
			result.Duration = time.Since(startTime)
			result.Success = false
			result.Error = fmt.Sprintf("packer validate failed: %v", err)
			return result, fmt.Errorf("packer validate failed: %w", err)
		}
	}

	// 6. Run packer build
	var buildArgs []string
	if opts.MachineReadable {
		buildArgs = append(buildArgs, "-machine-readable")
	}
	if !opts.Color {
		buildArgs = append(buildArgs, "-color=false")
	}
	buildArgs = append(buildArgs, ".")

	emitLog("system", fmt.Sprintf("==> [%s] Executing Packer build (packer build)...", buildID))
	buildOut, err := r.runCommand(ctx, workDir, opts.OnLog, opts.OnLogEntry, opts.Env, "build", buildArgs...)
	if buildOut != "" {
		allOutput.WriteString(buildOut)
	}
	result.Output = allOutput.String()
	result.Duration = time.Since(startTime)

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("packer build failed: %v", err)
		return result, fmt.Errorf("packer build failed: %w", err)
	}

	// 7. Parse manifest.json
	manifestPath := filepath.Join(workDir, ManifestFileName)
	manifest, manifestErr := ParseManifestFile(manifestPath)
	if manifestErr != nil {
		result.Success = false
		result.Error = fmt.Sprintf("packer build completed but failed to read manifest (%s): %v", ManifestFileName, manifestErr)
		return result, fmt.Errorf("packer build completed but failed to read manifest (%s): %w", ManifestFileName, manifestErr)
	}

	result.Manifest = manifest
	amiID, regionMap, err := manifest.ExtractAMIInfo()
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to extract AMI ID from manifest: %v", err)
		return result, fmt.Errorf("failed to extract AMI ID from manifest: %w", err)
	}
	result.AMIID = amiID
	result.RegionToAMI = regionMap
	result.Success = true

	emitLog("system", fmt.Sprintf("==> [%s] Build successful! AMI ID: %s", buildID, amiID))
	result.Output = allOutput.String()

	return result, nil
}

// runCommand runs an external command in workDir and streams stdout/stderr lines to onLog and onLogEntry.
func (r *Runner) runCommand(
	ctx context.Context,
	workDir string,
	onLog LogHandler,
	onLogEntry StructuredLogHandler,
	env []string,
	subCmd string,
	args ...string,
) (string, error) {
	bin := r.packerBinary()
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("packer binary %q not found in PATH: %w", bin, err)
	}

	cmdArgs := append([]string{subCmd}, args...)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Group subprocesses so cancelled contexts cleanly signal all child processes/plugins
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 30 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			// Send SIGINT to the entire process group so Packer can gracefully
			// clean up temporary AWS EC2 instances, keypairs, and security groups.
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		}
		return nil
	}

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command %s %s: %w", bin, subCmd, err)
	}

	var outputBuf bytes.Buffer
	var mu sync.Mutex
	var wg sync.WaitGroup

	readPipe := func(reader io.Reader, streamName string) {
		defer wg.Done()
		bufReader := bufio.NewReader(reader)
		for {
			line, err := bufReader.ReadString('\n')
			if len(line) > 0 {
				cleanLine := SanitizeLogMessage(strings.TrimRight(line, "\r\n"))
				mu.Lock()
				outputBuf.WriteString(cleanLine)
				outputBuf.WriteByte('\n')
				if onLog != nil {
					onLog(cleanLine)
				}
				if onLogEntry != nil {
					onLogEntry(LogEntry{
						Timestamp: time.Now(),
						Stream:    streamName,
						Message:   cleanLine,
					})
				}
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}

	wg.Add(2)
	go readPipe(stdoutPipe, "stdout")
	go readPipe(stderrPipe, "stderr")

	wg.Wait()
	cmdErr := cmd.Wait()
	output := outputBuf.String()

	if cmdErr != nil {
		if ctx.Err() != nil {
			return output, fmt.Errorf("%s %s cancelled: %w", bin, subCmd, ctx.Err())
		}
		return output, fmt.Errorf("%s %s failed: %w", bin, subCmd, cmdErr)
	}

	return output, nil
}

// ParseManifestFile reads and parses a Packer manifest JSON file.
func ParseManifestFile(filePath string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file %s: %w", filePath, err)
	}
	return ParseManifest(data)
}

// ParseManifest parses raw JSON bytes into a Manifest struct.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest JSON: %w", err)
	}
	return &m, nil
}

// ExtractAMIInfo retrieves the primary AMI ID and region mapping from the manifest.
func (m *Manifest) ExtractAMIInfo() (string, map[string]string, error) {
	if len(m.Builds) == 0 {
		return "", nil, errors.New("no builds found in manifest")
	}

	build := m.GetLatestBuild()
	if build == nil {
		return "", nil, errors.New("no matching build found in manifest")
	}

	if strings.TrimSpace(build.ArtifactID) == "" {
		return "", nil, errors.New("manifest build artifact_id is empty")
	}

	primaryAMI, regionMap := ParseArtifactID(build.ArtifactID)
	if primaryAMI == "" {
		return "", nil, fmt.Errorf("could not extract valid AMI ID from artifact_id: %q", build.ArtifactID)
	}

	return primaryAMI, regionMap, nil
}

// GetLatestBuild returns the build matching LastRunUUID, or the last build in the list.
func (m *Manifest) GetLatestBuild() *ManifestBuild {
	if len(m.Builds) == 0 {
		return nil
	}
	if m.LastRunUUID != "" {
		for i := range m.Builds {
			if m.Builds[i].PackerRunUUID == m.LastRunUUID {
				return &m.Builds[i]
			}
		}
	}
	return &m.Builds[len(m.Builds)-1]
}

// ParseArtifactID parses Packer's artifact_id string (e.g. "ap-south-1:ami-0123456789abcdef0"
// or "us-east-1:ami-11111111111111111,us-west-2:ami-22222222222222222") into a primary AMI ID and region mapping.
func ParseArtifactID(artifactID string) (string, map[string]string) {
	regionMap := make(map[string]string)
	var primaryAMI string

	parts := strings.Split(artifactID, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, ":") {
			kv := strings.SplitN(part, ":", 2)
			region := strings.TrimSpace(kv[0])
			ami := strings.TrimSpace(kv[1])
			if amiRegex.MatchString(ami) {
				regionMap[region] = ami
				if primaryAMI == "" {
					primaryAMI = ami
				}
			} else {
				found := amiRegex.FindString(ami)
				if found != "" {
					regionMap[region] = found
					if primaryAMI == "" {
						primaryAMI = found
					}
				}
			}
		} else {
			found := amiRegex.FindString(part)
			if found != "" {
				if primaryAMI == "" {
					primaryAMI = found
				}
				regionMap["default"] = found
			}
		}
	}

	if primaryAMI == "" {
		primaryAMI = amiRegex.FindString(artifactID)
	}

	return primaryAMI, regionMap
}

// ParseMachineReadableLine parses a line produced by Packer's -machine-readable flag.
// Format: timestamp,target,type,data...
func ParseMachineReadableLine(line string) (*MachineReadableEvent, bool) {
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return nil, false
	}

	event := &MachineReadableEvent{
		Timestamp: parts[0],
		Target:    parts[1],
		Type:      parts[2],
	}

	if len(parts) > 3 {
		event.Data = parts[3:]
		// Format human-readable message if this is a ui log
		if event.Type == "ui" && len(event.Data) >= 2 {
			msg := strings.Join(event.Data[1:], ",")
			msg = strings.ReplaceAll(msg, "%!(PACKER_COMMA)", ",")
			msg = strings.ReplaceAll(msg, `\,`, ",")
			msg = strings.ReplaceAll(msg, `\n`, "\n")
			event.Message = msg
		}
	}

	return event, true
}

// FormatMachineReadableMessage extracts the user-facing message from a machine-readable line if applicable,
// otherwise returns the raw line.
func FormatMachineReadableMessage(line string) string {
	if event, ok := ParseMachineReadableLine(line); ok && event.Message != "" {
		return event.Message
	}
	return line
}

func sanitizeID(id string) string {
	cleaned := sanitizeRegex.ReplaceAllString(id, "_")
	if len(cleaned) > 32 {
		cleaned = cleaned[:32]
	}
	return cleaned
}
