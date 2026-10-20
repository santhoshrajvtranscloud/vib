package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	vibaws "github.com/santhosh/vib/internal/aws"
	"github.com/santhosh/vib/internal/config"
	"github.com/santhosh/vib/internal/generator"
	"github.com/santhosh/vib/internal/packer"
	vibstore "github.com/santhosh/vib/internal/store"
)

// Server coordinates the HTTP & SSE API for Visual Image Builder.
type Server struct {
	runner       *packer.Runner
	buildManager *BuildManager
	credManager  *vibaws.CredentialManager
	store        *vibstore.DB
	mux          *http.ServeMux
	handler      http.Handler
}

// NewServer creates a new API server with the provided Packer runner, credential manager, and sqlite store.
func NewServer(runner *packer.Runner, deps ...interface{}) *Server {
	if runner == nil {
		runner = packer.NewRunner()
	}

	var cm *vibaws.CredentialManager
	var dbStore *vibstore.DB

	for _, dep := range deps {
		switch v := dep.(type) {
		case *vibaws.CredentialManager:
			cm = v
		case *vibstore.DB:
			dbStore = v
		}
	}

	if cm == nil {
		cm, _ = vibaws.NewCredentialManager()
	}
	if dbStore == nil {
		dbStore, _ = vibstore.NewDB()
	}

	bm := NewBuildManager(runner, cm, dbStore)
	s := &Server{
		runner:       runner,
		buildManager: bm,
		credManager:  cm,
		store:        dbStore,
		mux:          http.NewServeMux(),
	}

	s.registerRoutes()
	s.handler = corsMiddleware(s.mux)
	return s
}

// Routes returns the root http.Handler configured with CORS.
func (s *Server) Routes() http.Handler {
	return s.handler
}

// BuildManager returns the active in-memory BuildManager.
func (s *Server) BuildManager() *BuildManager {
	return s.buildManager
}

// CredentialManager returns the active AWS CredentialManager.
func (s *Server) CredentialManager() *vibaws.CredentialManager {
	return s.credManager
}

// Store returns the active SQLite store.
func (s *Server) Store() *vibstore.DB {
	return s.store
}

// registerRoutes sets up all API endpoints.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/catalogue", s.handleCatalogue)
	s.mux.HandleFunc("POST /api/hcl/generate", s.handleHCLGenerate)
	s.mux.HandleFunc("POST /api/hcl/validate", s.handleHCLValidate)
	s.mux.HandleFunc("POST /api/hcl/parse", s.handleHCLParse)
	s.mux.HandleFunc("GET /api/builds", s.handleListBuilds)
	s.mux.HandleFunc("POST /api/builds", s.handleCreateBuild)
	s.mux.HandleFunc("GET /api/builds/{id}", s.handleGetBuild)
	s.mux.HandleFunc("GET /api/builds/{id}/stream", s.handleStreamBuild)
	s.mux.HandleFunc("POST /api/builds/{id}/cancel", s.handleCancelBuild)

	// AWS Integration Endpoints
	s.mux.HandleFunc("GET /api/aws/status", s.handleAWSStatus)
	s.mux.HandleFunc("POST /api/aws/credentials", s.handleSaveAWSCredentials)
	s.mux.HandleFunc("DELETE /api/aws/credentials", s.handleClearAWSCredentials)
	s.mux.HandleFunc("GET /api/aws/base-amis", s.handleGetBaseAMIs)

	// VIB AMI & Lifecycle Endpoints (Milestone 4)
	s.mux.HandleFunc("GET /api/images", s.handleListImages)
	s.mux.HandleFunc("GET /api/images/{id}/config", s.handleGetImageConfig)
	s.mux.HandleFunc("DELETE /api/images/{id}", s.handleDeleteImage)

	// Mount static frontend (embedded single binary or local dev filesystem)
	if spaFS, isEmbedded := getSPAFileSystem(); spaFS != nil {
		s.mux.HandleFunc("GET /", s.handleStatic(spaFS, isEmbedded))
	}
}

// isAllowedOrigin strictly validates that the Origin header comes from localhost or loopback interfaces.
func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// corsMiddleware provides CORS & CSRF protection for localhost and local Vite development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			if isAllowedOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-VIB-CSRF-Protection, Accept")
				w.Header().Set("Access-Control-Max-Age", "86400")
			} else {
				// Block cross-origin state-changing requests from untrusted external websites
				if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.Method == http.MethodPut {
					http.Error(w, "Forbidden: Untrusted cross-origin request blocked", http.StatusForbidden)
					return
				}
			}
		} else {
			// Same-origin or non-browser client: allow localhost API access
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-VIB-CSRF-Protection, Accept")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ----------------------------------------------------------------------------
// Route Handlers
// ----------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	packerVer, err := s.runner.Version(ctx)
	packerStatus := "available"
	if err != nil {
		packerStatus = fmt.Sprintf("unavailable: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"time":          time.Now().UTC(),
		"packer_status": packerStatus,
		"packer_ver":    packerVer,
	})
}

// CataloguePackage describes an available pre-configured package.
type CataloguePackage struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

// handleCatalogue returns the list of supported software packages from generator.PackageCatalogue.
func (s *Server) handleCatalogue(w http.ResponseWriter, r *http.Request) {
	packages := make([]CataloguePackage, 0, len(generator.PackageCatalogue))

	for id, cmds := range generator.PackageCatalogue {
		name := strings.ToUpper(id[:1]) + id[1:]
		desc := fmt.Sprintf("Installs and configures %s", id)
		switch id {
		case "nginx":
			desc = "Installs and enables Nginx high-performance HTTP server"
		case "docker":
			desc = "Installs Docker engine and configures user permissions"
		case "git":
			desc = "Installs Git version control system"
		}

		packages = append(packages, CataloguePackage{
			ID:          id,
			Name:        name,
			Description: desc,
			Commands:    cmds,
		})
	}

	// Sort alphabetically by ID for deterministic output
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ID < packages[j].ID
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"packages":  packages,
		"catalogue": generator.PackageCatalogue,
	})
}

// handleHCLGenerate validates incoming BuildConfig JSON and returns { "hcl": "..." }.
func (s *Server) handleHCLGenerate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "Request body is empty")
		return
	}

	// Check if wrapped in { "config": { ... } } or direct BuildConfig
	var wrapper struct {
		Config *config.BuildConfig `json:"config"`
	}
	var cfg *config.BuildConfig
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Config != nil {
		// Re-encode and validate through config.ParseAndValidate
		raw, _ := json.Marshal(wrapper.Config)
		cfg, err = config.ParseAndValidate(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Configuration validation failed: %v", err))
			return
		}
	} else {
		cfg, err = config.ParseAndValidate(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Configuration validation failed: %v", err))
			return
		}
	}

	hclBytes, err := generator.GenerateHCL(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("HCL generation failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"hcl": string(hclBytes),
	})
}

// HCLValidateRequest represents the payload for POST /api/hcl/validate.
type HCLValidateRequest struct {
	HCL string `json:"hcl"`
}

// HCLValidateResponse represents the result of POST /api/hcl/validate.
type HCLValidateResponse struct {
	Valid bool               `json:"valid"`
	Stage string             `json:"stage,omitempty"`
	Error string             `json:"error,omitempty"`
	Logs  []packer.LogEntry  `json:"logs"`
}

// handleHCLValidate accepts { "hcl": "..." }, prepares a temp workspace, runs Init + Validate, and returns result.
func (s *Server) handleHCLValidate(w http.ResponseWriter, r *http.Request) {
	var req HCLValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.HCL) == "" {
		writeError(w, http.StatusBadRequest, "Attribute 'hcl' is required")
		return
	}

	workDir, cleanup, err := s.runner.PrepareWorkDir(nil, []byte(req.HCL), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to initialize workspace: %v", err))
		return
	}
	defer cleanup()

	var logs []packer.LogEntry
	var mu sync.Mutex
	onLogEntry := func(entry packer.LogEntry) {
		mu.Lock()
		logs = append(logs, entry)
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	// 1. Run Init
	onLogEntry(packer.LogEntry{
		Timestamp: time.Now(),
		Stream:    "system",
		Message:   "==> Initializing Packer plugins (packer init)...",
	})
	if initOut, initErr := s.runner.Init(ctx, workDir, nil); initErr != nil {
		onLogEntry(packer.LogEntry{
			Timestamp: time.Now(),
			Stream:    "stderr",
			Message:   initOut,
		})
		writeJSON(w, http.StatusOK, HCLValidateResponse{
			Valid: false,
			Stage: "init",
			Error: fmt.Sprintf("packer init failed: %v", initErr),
			Logs:  logs,
		})
		return
	}

	// 2. Run Validate
	onLogEntry(packer.LogEntry{
		Timestamp: time.Now(),
		Stream:    "system",
		Message:   "==> Validating Packer HCL configuration (packer validate)...",
	})
	_, valErr := s.runner.Validate(ctx, workDir, func(line string) {
		onLogEntry(packer.LogEntry{
			Timestamp: time.Now(),
			Stream:    "stdout",
			Message:   line,
		})
	})

	if valErr != nil {
		writeJSON(w, http.StatusOK, HCLValidateResponse{
			Valid: false,
			Stage: "validate",
			Error: fmt.Sprintf("packer validate failed: %v", valErr),
			Logs:  logs,
		})
		return
	}

	writeJSON(w, http.StatusOK, HCLValidateResponse{
		Valid: true,
		Logs:  logs,
	})
}

// HCLParseRequest represents the payload for POST /api/hcl/parse.
type HCLParseRequest struct {
	HCL string `json:"hcl"`
}

// HCLParseResponse represents the result of POST /api/hcl/parse.
type HCLParseResponse struct {
	Config *config.BuildConfig `json:"config"`
}

// handleHCLParse accepts HCL text, parses it via generator.ParseHCL, and returns the reconstructed BuildConfig.
func (s *Server) handleHCLParse(w http.ResponseWriter, r *http.Request) {
	var req HCLParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.HCL) == "" {
		writeError(w, http.StatusBadRequest, "HCL content is required")
		return
	}

	cfg, err := generator.ParseHCL(req.HCL)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse HCL: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, HCLParseResponse{
		Config: cfg,
	})
}

// CreateBuildRequest defines payload for POST /api/builds.
type CreateBuildRequest struct {
	BuildID string              `json:"build_id,omitempty"`
	HCL     string              `json:"hcl,omitempty"`
	Config  *config.BuildConfig `json:"config,omitempty"`
}

// handleCreateBuild accepts BuildConfig JSON or raw HCL, creates a job, starts async execution.
func (s *Server) handleCreateBuild(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "Request body is empty")
		return
	}

	var req CreateBuildRequest
	_ = json.Unmarshal(body, &req)

	var hclContent string
	var buildConfig *config.BuildConfig

	if strings.TrimSpace(req.HCL) != "" {
		hclContent = req.HCL
		buildConfig = req.Config
	} else if req.Config != nil {
		rawCfg, _ := json.Marshal(req.Config)
		validated, err := config.ParseAndValidate(rawCfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid BuildConfig: %v", err))
			return
		}
		buildConfig = validated
		hclBytes, err := generator.GenerateHCL(validated)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("HCL generation failed: %v", err))
			return
		}
		hclContent = string(hclBytes)
	} else {
		// Attempt direct unmarshal as BuildConfig
		validated, err := config.ParseAndValidate(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON or BuildConfig: %v", err))
			return
		}
		buildConfig = validated
		hclBytes, err := generator.GenerateHCL(validated)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("HCL generation failed: %v", err))
			return
		}
		hclContent = string(hclBytes)
	}

	buildID := req.BuildID
	if buildID == "" && buildConfig != nil && buildConfig.BuildID != "" {
		buildID = buildConfig.BuildID
	}
	if buildID == "" {
		buildID = "vib-" + newUUID()
	}

	build, err := s.buildManager.CreateAndStartBuild(buildID, hclContent, buildConfig)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	summary := build.StatusSummary()
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"build_id": summary.BuildID,
		"status":   summary.Status,
	})
}

// handleListBuilds returns all builds in memory.
func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	builds := s.buildManager.ListBuilds()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"builds": builds,
		"total":  len(builds),
	})
}

// handleGetBuild returns the summary of a specific build.
func (s *Server) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	build := s.buildManager.GetBuild(id)
	if build == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Build %q not found", id))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"build": build.StatusSummary(),
	})
}

// handleStreamBuild streams Server-Sent Events (past + live logs and final status).
func (s *Server) handleStreamBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	build := s.buildManager.GetBuild(id)
	if build == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Build %q not found", id))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming unsupported by connection")
		return
	}

	// Set SSE HTTP Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Subscribe to live log events and retrieve past logs
	logChan, pastLogs, status, summary := build.Subscribe()
	defer build.Unsubscribe(logChan)

	// 1. Emit past logs
	for _, entry := range pastLogs {
		data, _ := json.Marshal(entry)
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
	}
	flusher.Flush()

	// 2. If the build has already concluded, emit status event and terminate stream
	if status != "running" && status != "pending" {
		statusData, _ := json.Marshal(summary)
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
		flusher.Flush()
		return
	}

	// 3. Stream live logs until finished or disconnected
	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-logChan:
			if !ok {
				// Channel closed, build concluded
				statusData, _ := json.Marshal(build.StatusSummary())
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		case <-build.done:
			// Drain remaining logs
			for len(logChan) > 0 {
				entry := <-logChan
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			}
			statusData, _ := json.Marshal(build.StatusSummary())
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
			flusher.Flush()
			return
		}
	}
}

// handleCancelBuild cancels a currently running build by ID.
func (s *Server) handleCancelBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	build := s.buildManager.GetBuild(id)
	if build == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Build %q not found", id))
		return
	}

	cancelled, currentStatus := build.Cancel()
	if !cancelled {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Build %q is not running (status: %s)", id, currentStatus))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"build_id": id,
		"status":   "cancelling",
		"message":  "Build cancellation requested; Packer is performing graceful cleanup",
	})
}

// handleAWSStatus returns current AWS connection status, masked key, and STS identity.
func (s *Server) handleAWSStatus(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	if region == "" {
		region = "ap-south-1"
	}

	identity, err := s.credManager.VerifyIdentity(r.Context(), region)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, identity)
}

// SaveCredentialsRequest represents the payload for POST /api/aws/credentials.
type SaveCredentialsRequest struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
	Region          string `json:"region,omitempty"`
}

// handleSaveAWSCredentials validates credentials via STS GetCallerIdentity, saves encrypted via AES-256-GCM, and returns identity.
func (s *Server) handleSaveAWSCredentials(w http.ResponseWriter, r *http.Request) {
	var req SaveCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.AccessKeyID) == "" || strings.TrimSpace(req.SecretAccessKey) == "" {
		writeError(w, http.StatusBadRequest, "access_key_id and secret_access_key are required")
		return
	}

	region := strings.TrimSpace(req.Region)
	if region == "" {
		region = "ap-south-1"
	}

	creds := vibaws.CustomCredentials{
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(req.SecretAccessKey),
		SessionToken:    strings.TrimSpace(req.SessionToken),
	}

	identity, err := s.credManager.SaveCustomCredentials(r.Context(), creds, region)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to verify and save AWS credentials: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, identity)
}

// handleClearAWSCredentials removes stored encrypted credentials.
func (s *Server) handleClearAWSCredentials(w http.ResponseWriter, r *http.Request) {
	if err := s.credManager.ClearCustomCredentials(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to clear credentials: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "cleared",
		"message": "Reverted to default AWS profile/environment credentials",
	})
}

// handleGetBaseAMIs returns the list of official base AMIs for the selected region and arch.
func (s *Server) handleGetBaseAMIs(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	if region == "" {
		region = "ap-south-1"
	}
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = "x86_64"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	cfg, _ := s.credManager.GetAWSConfig(ctx, region)
	result := vibaws.ListOfficialBaseAMIs(ctx, cfg, region, arch)

	writeJSON(w, http.StatusOK, result)
}

// ImageItem represents a unified AMI record combining live AWS metadata and SQLite build configuration.
type ImageItem struct {
	BuildID         string            `json:"build_id,omitempty"`
	AMIID           string            `json:"ami_id"`
	Name            string            `json:"name"`
	Region          string            `json:"region"`
	Status          string            `json:"status"`
	CreationDate    string            `json:"creation_date"`
	DurationSeconds int               `json:"duration_seconds,omitempty"`
	LiveInAWS       bool              `json:"live_in_aws"`
	HasConfig       bool              `json:"has_config"`
	SnapshotIDs     []string          `json:"snapshot_ids,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
}

// handleListImages returns AMIs created by VIB, joining live AWS DescribeImages with local SQLite records.
func (s *Server) handleListImages(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	if region == "" {
		region = "ap-south-1"
	}

	// 1. Fetch local SQLite builds
	var localBuilds []*vibstore.BuildRecord
	if s.store != nil {
		localBuilds, _ = s.store.ListBuilds()
	}

	// 2. Fetch live AWS AMIs if AWS credentials available
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var liveImages []vibaws.VIBImage
	if s.credManager != nil {
		cfg, err := s.credManager.GetAWSConfig(ctx, region)
		if err == nil {
			liveImages, _ = vibaws.ListVIBImages(ctx, cfg, region)
		}
	}

	// 3. Merge & join
	seenAMI := make(map[string]bool)
	var items []ImageItem

	// Add live AMIs first
	for _, img := range liveImages {
		seenAMI[img.AMIID] = true
		item := ImageItem{
			AMIID:        img.AMIID,
			Name:         img.Name,
			Region:       img.Region,
			Status:       img.State,
			CreationDate: img.CreationDate,
			LiveInAWS:    true,
			SnapshotIDs:  img.SnapshotIDs,
			Tags:         img.Tags,
			BuildID:      img.BuildID,
		}

		for _, b := range localBuilds {
			if (b.AMIID != "" && b.AMIID == img.AMIID) || (img.BuildID != "" && b.BuildID == img.BuildID) {
				item.HasConfig = true
				if item.BuildID == "" {
					item.BuildID = b.BuildID
				}
				item.DurationSeconds = b.DurationSeconds
				break
			}
		}
		items = append(items, item)
	}

	// Add local SQLite records that weren't in live list
	for _, b := range localBuilds {
		if b.AMIID != "" && seenAMI[b.AMIID] {
			continue
		}
		displayID := b.AMIID
		if displayID == "" {
			displayID = b.BuildID
		}
		items = append(items, ImageItem{
			BuildID:         b.BuildID,
			AMIID:           displayID,
			Name:            b.AMIName,
			Region:          b.Region,
			Status:          b.Status,
			CreationDate:    b.CreatedAt.Format(time.RFC3339),
			DurationSeconds: b.DurationSeconds,
			LiveInAWS:       false,
			HasConfig:       true,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"images": items,
		"total":  len(items),
	})
}

// handleGetImageConfig returns config_json and hcl_code for the specified image or build ID.
func (s *Server) handleGetImageConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	if s.store == nil {
		writeError(w, http.StatusInternalServerError, "Storage database is not configured")
		return
	}

	record, err := s.store.GetBuild(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Configuration for image/build %q not found: %v", id, err))
		return
	}

	var parsedConfig map[string]interface{}
	if record.ConfigJSON != "" {
		_ = json.Unmarshal([]byte(record.ConfigJSON), &parsedConfig)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"build_id":         record.BuildID,
		"ami_id":           record.AMIID,
		"ami_name":         record.AMIName,
		"region":           record.Region,
		"status":           record.Status,
		"hcl_code":         record.HCLCode,
		"config_json":      record.ConfigJSON,
		"config":           parsedConfig,
		"duration_seconds": record.DurationSeconds,
		"created_at":       record.CreatedAt,
		"updated_at":       record.UpdatedAt,
	})
}

// handleDeleteImage calls DeleteVIBImage and updates the SQLite record.
func (s *Server) handleDeleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	region := r.URL.Query().Get("region")
	amiID := id

	var record *vibstore.BuildRecord
	if s.store != nil {
		record, _ = s.store.GetBuild(id)
		if record != nil {
			if record.AMIID != "" {
				amiID = record.AMIID
			}
			if region == "" && record.Region != "" {
				region = record.Region
			}
		}
	}

	if region == "" {
		region = "ap-south-1"
	}

	var deleteResult *vibaws.DeleteVIBImageResult
	var awsErr error

	if strings.HasPrefix(amiID, "ami-") && s.credManager != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		cfg, err := s.credManager.GetAWSConfig(ctx, region)
		if err == nil {
			deleteResult, awsErr = vibaws.DeleteVIBImage(ctx, cfg, region, amiID)
		}
	}

	if s.store != nil {
		_ = s.store.MarkBuildDeleted(id)
		if amiID != id {
			_ = s.store.MarkBuildDeleted(amiID)
		}
	}

	if b := s.buildManager.GetBuild(id); b != nil {
		b.Status = "deleted"
	}
	if b := s.buildManager.GetBuild(amiID); b != nil {
		b.Status = "deleted"
	}

	var deletedSnaps []string
	if deleteResult != nil {
		deletedSnaps = deleteResult.DeletedSnapshotIDs
	}

	msg := "AMI marked as deleted in VIB"
	if deleteResult != nil && deleteResult.Deregistered {
		msg = fmt.Sprintf("AMI %s cleanly deregistered and %d EBS snapshot(s) deleted", amiID, len(deletedSnaps))
	} else if awsErr != nil {
		msg = fmt.Sprintf("Marked deleted locally; AWS notice: %v", awsErr)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ami_id":               amiID,
		"status":               "deleted",
		"deregistered":         deleteResult != nil && deleteResult.Deregistered,
		"deleted_snapshot_ids": deletedSnaps,
		"message":              msg,
	})
}

// ----------------------------------------------------------------------------
// In-Memory Build Manager & Build Model
// ----------------------------------------------------------------------------

// Build represents an individual image build job.
type Build struct {
	ID          string
	Status      string // "pending", "running", "success", "failed", "cancelled", "deleted"
	Config      *config.BuildConfig
	HCL         string
	WorkDir     string
	AMIID       string
	RegionToAMI map[string]string
	Error       string
	Duration    time.Duration
	CreatedAt   time.Time
	FinishedAt  *time.Time
	Logs        []packer.LogEntry

	mu          sync.RWMutex
	subscribers []chan packer.LogEntry
	cancelFunc  context.CancelFunc
	isCancelled bool
	done        chan struct{}
}

// BuildStatusSummary represents the serializable status of a build.
type BuildStatusSummary struct {
	BuildID     string            `json:"build_id"`
	Status      string            `json:"status"`
	AMIID       string            `json:"ami_id,omitempty"`
	RegionToAMI map[string]string `json:"region_to_ami,omitempty"`
	DurationMs  int64             `json:"duration_ms"`
	CreatedAt   time.Time         `json:"created_at"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	WorkDir     string            `json:"work_dir,omitempty"`
}

// StatusSummary returns a serializable snapshot of the build.
func (b *Build) StatusSummary() BuildStatusSummary {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.statusSummaryLocked()
}

func (b *Build) statusSummaryLocked() BuildStatusSummary {
	return BuildStatusSummary{
		BuildID:     b.ID,
		Status:      b.Status,
		AMIID:       b.AMIID,
		RegionToAMI: b.RegionToAMI,
		DurationMs:  b.Duration.Milliseconds(),
		CreatedAt:   b.CreatedAt,
		FinishedAt:  b.FinishedAt,
		Error:       b.Error,
		WorkDir:     b.WorkDir,
	}
}

// Cancel signals cancellation of the build. Returns true if cancellation was initiated.
func (b *Build) Cancel() (bool, string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.Status != "running" && b.Status != "pending" {
		return false, b.Status
	}

	b.isCancelled = true
	b.Status = "cancelling"
	if b.cancelFunc != nil {
		b.cancelFunc()
	}
	return true, "cancelling"
}

// appendLog appends a log entry and broadcasts it to active subscribers.
func (b *Build) appendLog(entry packer.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.Logs = append(b.Logs, entry)
	for _, ch := range b.subscribers {
		select {
		case ch <- entry:
		default:
			// If buffer is full, drop to prevent blocking the build pipeline
		}
	}
}

// Subscribe attaches a listener channel and returns past logs and current status.
func (b *Build) Subscribe() (chan packer.LogEntry, []packer.LogEntry, string, BuildStatusSummary) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan packer.LogEntry, 256)
	b.subscribers = append(b.subscribers, ch)

	pastLogs := make([]packer.LogEntry, len(b.Logs))
	copy(pastLogs, b.Logs)

	return ch, pastLogs, b.Status, b.statusSummaryLocked()
}

// Unsubscribe removes a listener channel and closes it.
func (b *Build) Unsubscribe(ch chan packer.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

// finish marks the build complete, recording outcome and closing the done channel.
func (b *Build) finish(result *packer.BuildResult, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.FinishedAt = &now
	b.Duration = now.Sub(b.CreatedAt)

	if result != nil {
		b.WorkDir = result.WorkDir
		b.AMIID = result.AMIID
		b.RegionToAMI = result.RegionToAMI
	}

	if err != nil {
		if b.isCancelled || errors.Is(err, context.Canceled) {
			b.Status = "cancelled"
			b.Error = "Build was cancelled by user"
		} else {
			b.Status = "failed"
			b.Error = err.Error()
		}
	} else {
		b.Status = "success"
	}
}

// signalDone closes the done channel to signal waiters that build and persistence are finished.
func (b *Build) signalDone() {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
	default:
		close(b.done)
	}
}

// BuildManager handles concurrent builds in memory and persists to SQLite.
type BuildManager struct {
	runner      *packer.Runner
	credManager *vibaws.CredentialManager
	store       *vibstore.DB
	builds      map[string]*Build
	mu          sync.RWMutex
}

// NewBuildManager initializes a new BuildManager.
func NewBuildManager(runner *packer.Runner, deps ...interface{}) *BuildManager {
	var cm *vibaws.CredentialManager
	var dbStore *vibstore.DB

	for _, dep := range deps {
		switch v := dep.(type) {
		case *vibaws.CredentialManager:
			cm = v
		case *vibstore.DB:
			dbStore = v
		}
	}

	return &BuildManager{
		runner:      runner,
		credManager: cm,
		store:       dbStore,
		builds:      make(map[string]*Build),
	}
}

// CreateAndStartBuild creates a new build record and begins execution in a background goroutine.
func (bm *BuildManager) CreateAndStartBuild(id, hclContent string, cfg *config.BuildConfig) (*Build, error) {
	bm.mu.Lock()
	if _, exists := bm.builds[id]; exists {
		bm.mu.Unlock()
		return nil, fmt.Errorf("build with ID %q already exists", id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	build := &Build{
		ID:         id,
		Status:     "running",
		Config:     cfg,
		HCL:        hclContent,
		CreatedAt:  time.Now(),
		Logs:       make([]packer.LogEntry, 0, 100),
		cancelFunc: cancel,
		done:       make(chan struct{}),
	}
	bm.builds[id] = build
	bm.mu.Unlock()

	// Persist initial record in SQLite store
	if bm.store != nil {
		amiName := "vib-build"
		region := "ap-south-1"
		cfgJSON := "{}"
		if cfg != nil {
			if cfg.AMIName != "" {
				amiName = cfg.AMIName
			}
			if cfg.Region != "" {
				region = cfg.Region
			}
			if b, err := json.Marshal(cfg); err == nil {
				cfgJSON = string(b)
			}
		}

		_ = bm.store.CreateBuild(&vibstore.BuildRecord{
			BuildID:    id,
			AMIName:    amiName,
			Region:     region,
			Status:     "running",
			HCLCode:    hclContent,
			ConfigJSON: cfgJSON,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		})
	}

	go bm.runBuild(ctx, build)

	return build, nil
}

func (bm *BuildManager) runBuild(ctx context.Context, build *Build) {
	region := "ap-south-1"
	if build.Config != nil && build.Config.Region != "" {
		region = build.Config.Region
	}

	var env []string
	if bm.credManager != nil {
		env = bm.credManager.ExportEnv(region)
	}

	opts := packer.BuildOptions{
		Config:   build.Config,
		HCLBytes: []byte(build.HCL),
		Env:      env,
		OnLogEntry: func(entry packer.LogEntry) {
			build.appendLog(entry)
		},
	}

	result, err := bm.runner.Build(ctx, opts)
	build.finish(result, err)

	// Update SQLite store with final outcome
	if bm.store != nil {
		build.mu.RLock()
		var sb strings.Builder
		for _, l := range build.Logs {
			sb.WriteString(l.Message)
			sb.WriteString("\n")
		}
		status := build.Status
		amiID := build.AMIID
		errMsg := build.Error
		durationSec := int(build.Duration.Seconds())
		build.mu.RUnlock()

		_ = bm.store.UpdateBuildStatus(
			build.ID,
			status,
			amiID,
			sb.String(),
			errMsg,
			durationSec,
		)
	}

	build.signalDone()
}

// GetBuild retrieves a build by ID.
func (bm *BuildManager) GetBuild(id string) *Build {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.builds[id]
}

// ListBuilds returns all builds sorted by creation time descending.
func (bm *BuildManager) ListBuilds() []BuildStatusSummary {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	list := make([]BuildStatusSummary, 0, len(bm.builds))
	for _, b := range bm.builds {
		list = append(list, b.StatusSummary())
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	return list
}

// ----------------------------------------------------------------------------
// Utilities
// ----------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

