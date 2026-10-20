package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/santhosh/vib/internal/api"
	"github.com/santhosh/vib/internal/config"
	"github.com/santhosh/vib/internal/generator"
	"github.com/santhosh/vib/internal/packer"
)

func main() {
	serve := flag.Bool("serve", false, "Start the VIB HTTP & SSE API server (default port :8080)")
	port := flag.String("port", "8080", "Port to listen on when running the API server")
	addr := flag.String("addr", "", "Custom TCP address to listen on (e.g. :8080, 127.0.0.1:8080)")
	runBuild := flag.Bool("build", false, "Execute full 'packer build' in addition to init and validate (requires AWS credentials)")
	keepWorkDir := flag.Bool("keep-workdir", false, "Preserve the temporary build directory after completion")
	machineReadable := flag.Bool("machine-readable", false, "Enable Packer machine-readable output format")
	flag.Parse()

	// Initialize Packer Runner
	runner := packer.NewRunner()
	ctx := context.Background()

	version, err := runner.Version(ctx)
	if err != nil {
		log.Fatalf("Packer CLI check failed: %v\nPlease ensure Packer is installed and available in PATH.", err)
	}

	// Mode 1: Run HTTP & SSE API Server
	if *serve {
		listenAddr := *addr
		if listenAddr == "" {
			if strings.HasPrefix(*port, ":") {
				listenAddr = *port
			} else {
				listenAddr = ":" + *port
			}
		}

		apiServer := api.NewServer(runner)
		httpServer := &http.Server{
			Addr:              listenAddr,
			Handler:           apiServer.Routes(),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			displayHost := listenAddr
			if strings.HasPrefix(displayHost, ":") {
				displayHost = "http://localhost" + displayHost
			} else {
				displayHost = "http://" + displayHost
			}

			fmt.Println("================================================================")
			fmt.Printf("🚀 VIB API Server running with Packer %s\n", version)
			fmt.Printf("   URL: %s\n", displayHost)
			fmt.Println("----------------------------------------------------------------")
			fmt.Printf("   - VIB SQLite DB: %s\n", apiServer.Store().Path())
			fmt.Printf("   - AMIs & History:GET  %s/api/images\n", displayHost)
			fmt.Printf("   - Catalogue:     GET  %s/api/catalogue\n", displayHost)
			fmt.Printf("   - HCL Generate:  POST %s/api/hcl/generate\n", displayHost)
			fmt.Printf("   - HCL Validate:  POST %s/api/hcl/validate\n", displayHost)
			fmt.Printf("   - HCL Parse:     POST %s/api/hcl/parse\n", displayHost)
			fmt.Printf("   - Create Build:  POST %s/api/builds\n", displayHost)
			fmt.Printf("   - Stream Logs:   GET  %s/api/builds/{id}/stream\n", displayHost)
			fmt.Printf("   - Cancel Build:  POST %s/api/builds/{id}/cancel\n", displayHost)
			fmt.Println("================================================================")

			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("Server error: %v", err)
			}
		}()

		<-sigChan
		fmt.Println("\nReceived shutdown signal. Stopping VIB server gracefully...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		fmt.Println("VIB server stopped cleanly.")
		return
	}

	// Mode 2: CLI execution
	sampleJSON := []byte(`{
		"build_id": "vib-001",
		"region": "ap-south-1",
		"instance_type": "t3.micro",
		"source_ami": "ami-0f58b397bc5c1f2e8",
		"ssh_username": "ubuntu",
		"ami_name": "vib-nginx-demo",
		"packages": ["nginx", "docker"]
	}`)

	// 1. Validate Config
	cfg, err := config.ParseAndValidate(sampleJSON)
	if err != nil {
		log.Fatalf("Validation failed: %v", err)
	}
	fmt.Printf("=== VIB Build Config validated for %q (Region: %s) ===\n", cfg.AMIName, cfg.Region)

	// 2. Generate Packer HCL
	hclBytes, err := generator.GenerateHCL(cfg)
	if err != nil {
		log.Fatalf("HCL Generation failed: %v", err)
	}

	fmt.Println("\n=== Generated Packer HCL (aws-build.pkr.hcl) ===")
	fmt.Println(string(hclBytes))
	fmt.Printf("=== Packer Engine Detected: %s ===\n\n", version)

	onLog := func(line string) {
		fmt.Printf("[packer-stream] %s\n", line)
	}

	if *runBuild {
		fmt.Println("=== Executing Full Packer Build Pipeline (init -> validate -> build) ===")
		opts := packer.BuildOptions{
			Config:          cfg,
			HCLBytes:        hclBytes,
			KeepWorkDir:     *keepWorkDir,
			OnLog:           onLog,
			MachineReadable: *machineReadable,
		}

		result, err := runner.Build(ctx, opts)
		if err != nil {
			log.Fatalf("Packer Build failed: %v\nWorkDir: %s\nDuration: %v", err, result.WorkDir, result.Duration)
		}

		fmt.Println("\n=== Build Completed Successfully! ===")
		fmt.Printf("Build ID: %s\n", result.BuildID)
		fmt.Printf("Created AMI ID: %s\n", result.AMIID)
		fmt.Printf("WorkDir: %s\n", result.WorkDir)
		fmt.Printf("Duration: %v\n", result.Duration)
		if len(result.RegionToAMI) > 0 {
			fmt.Println("Region AMI Map:")
			for reg, ami := range result.RegionToAMI {
				fmt.Printf("  - %s: %s\n", reg, ami)
			}
		}
	} else {
		// Prepare temporary work directory and run init + validate
		workDir, cleanup, err := runner.PrepareWorkDir(cfg, hclBytes, "")
		if err != nil {
			log.Fatalf("Failed to prepare work directory: %v", err)
		}
		if !*keepWorkDir {
			defer cleanup()
		} else {
			fmt.Printf("Preserving WorkDir: %s\n", workDir)
		}

		fmt.Println("=== Step 1: Running 'packer init .' ===")
		initOut, err := runner.Init(ctx, workDir, onLog)
		if err != nil {
			log.Fatalf("packer init failed: %v\nOutput: %s", err, initOut)
		}
		fmt.Println("✓ packer init completed successfully.")

		fmt.Println("\n=== Step 2: Running 'packer validate .' ===")
		valOut, err := runner.Validate(ctx, workDir, onLog)
		if err != nil {
			log.Fatalf("packer validate failed: %v\nOutput: %s", err, valOut)
		}
		fmt.Println("✓ packer validate completed successfully: Configuration is 100% valid.")

		fmt.Println("\n=== Pipeline Verification Succeeded ===")
		fmt.Println("Packer HCL syntax and plugins are verified and ready for execution.")
		fmt.Println("To start the HTTP & SSE API server, run: go run ./cmd/vib/main.go --serve")
		fmt.Println("To execute full AMI build on AWS, run:   go run ./cmd/vib/main.go --build")
	}

	_ = os.Stdout.Sync()
}
