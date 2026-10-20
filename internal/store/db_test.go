package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestInMemoryDB(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory db: %v", err)
	}
	defer db.Close()

	// 1. Create Build Record
	record := &BuildRecord{
		BuildID:         "vib-test-1",
		AMIID:           "",
		AMIName:         "vib-ubuntu-app",
		Region:          "ap-south-1",
		Status:          "running",
		HCLCode:         `packer { required_plugins { amazon = { source = "github.com/hashicorp/amazon" version = ">= 1.2.8" } } }`,
		ConfigJSON:      `{"ami_name":"vib-ubuntu-app","region":"ap-south-1","packages":["nginx"]}`,
		BuildLogs:       "==> Starting build",
		DurationSeconds: 0,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	if err := db.CreateBuild(record); err != nil {
		t.Fatalf("CreateBuild failed: %v", err)
	}

	// 2. Get Build by build_id
	fetched, err := db.GetBuild("vib-test-1")
	if err != nil {
		t.Fatalf("GetBuild failed: %v", err)
	}
	if fetched.BuildID != "vib-test-1" || fetched.Status != "running" {
		t.Errorf("expected status 'running', got %s", fetched.Status)
	}
	if fetched.AMIName != "vib-ubuntu-app" {
		t.Errorf("expected ami_name 'vib-ubuntu-app', got %s", fetched.AMIName)
	}

	// 3. Update Build Status
	err = db.UpdateBuildStatus("vib-test-1", "success", "ami-0123456789abcdef0", "==> Build complete!\nami-0123456789abcdef0", "", 120)
	if err != nil {
		t.Fatalf("UpdateBuildStatus failed: %v", err)
	}

	// 4. Get by ami_id
	fetchedAMI, err := db.GetBuild("ami-0123456789abcdef0")
	if err != nil {
		t.Fatalf("GetBuild by ami_id failed: %v", err)
	}
	if fetchedAMI.Status != "success" || fetchedAMI.AMIID != "ami-0123456789abcdef0" {
		t.Errorf("expected ami_id 'ami-0123456789abcdef0' and status 'success', got %s, %s", fetchedAMI.AMIID, fetchedAMI.Status)
	}
	if fetchedAMI.DurationSeconds != 120 {
		t.Errorf("expected duration 120, got %d", fetchedAMI.DurationSeconds)
	}

	// 5. Create a second build to test listing
	record2 := &BuildRecord{
		BuildID:    "vib-test-2",
		AMIID:      "ami-0987654321fedcba0",
		AMIName:    "vib-debian-app",
		Region:     "us-east-1",
		Status:     "success",
		HCLCode:    "source \"amazon-ebs\" \"vib\" {}",
		ConfigJSON: `{"ami_name":"vib-debian-app","region":"us-east-1"}`,
		CreatedAt:  time.Now().UTC().Add(time.Minute),
		UpdatedAt:  time.Now().UTC().Add(time.Minute),
	}
	if err := db.CreateBuild(record2); err != nil {
		t.Fatalf("failed to create second record: %v", err)
	}

	list, err := db.ListBuilds()
	if err != nil {
		t.Fatalf("ListBuilds failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 builds in list, got %d", len(list))
	}
	// Newer record should be first
	if list[0].BuildID != "vib-test-2" {
		t.Errorf("expected newest build 'vib-test-2' first, got %s", list[0].BuildID)
	}

	// 6. Mark Build Deleted
	if err := db.MarkBuildDeleted("vib-test-1"); err != nil {
		t.Fatalf("MarkBuildDeleted failed: %v", err)
	}
	deletedRec, err := db.GetBuild("vib-test-1")
	if err != nil {
		t.Fatalf("failed to get marked build: %v", err)
	}
	if deletedRec.Status != "deleted" {
		t.Errorf("expected status 'deleted', got %s", deletedRec.Status)
	}

	// 7. Delete Build (Hard delete)
	if err := db.DeleteBuild("vib-test-2"); err != nil {
		t.Fatalf("DeleteBuild failed: %v", err)
	}
	_, err = db.GetBuild("vib-test-2")
	if err == nil {
		t.Errorf("expected error getting deleted build, got nil")
	}
}

func TestFileBasedDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create file-based db: %v", err)
	}
	defer db.Close()

	rec := &BuildRecord{
		BuildID:    "file-test-1",
		AMIName:    "test-ami",
		Region:     "eu-west-1",
		Status:     "running",
		HCLCode:    "source {}",
		ConfigJSON: "{}",
	}

	if err := db.CreateBuild(rec); err != nil {
		t.Fatalf("failed to create record in file db: %v", err)
	}

	fetched, err := db.GetBuild("file-test-1")
	if err != nil {
		t.Fatalf("failed to fetch from file db: %v", err)
	}
	if fetched.Region != "eu-west-1" {
		t.Errorf("expected region eu-west-1, got %s", fetched.Region)
	}
}

func TestWALConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "concurrency.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	// Initial record
	if err := db.CreateBuild(&BuildRecord{
		BuildID: "concurrency-base",
		AMIName: "base-ami",
		Region:  "us-east-1",
		Status:  "running",
	}); err != nil {
		t.Fatalf("failed to insert base: %v", err)
	}

	const goroutines = 20
	errChan := make(chan error, goroutines*2)

	// Launch concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			rec := &BuildRecord{
				BuildID: "concurrent-build-" + string(rune('A'+idx)),
				AMIName: "ami-" + string(rune('A'+idx)),
				Region:  "us-east-1",
				Status:  "running",
			}
			if err := db.CreateBuild(rec); err != nil {
				errChan <- err
				return
			}
			if err := db.UpdateBuildStatus(rec.BuildID, "success", "ami-idx-"+string(rune('A'+idx)), "logs", "", 10); err != nil {
				errChan <- err
				return
			}
			errChan <- nil
		}(i)
	}

	// Launch concurrent readers
	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := db.ListBuilds()
			errChan <- err
		}()
	}

	for i := 0; i < goroutines*2; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("concurrent SQLite operation failed with error: %v", err)
		}
	}
}
