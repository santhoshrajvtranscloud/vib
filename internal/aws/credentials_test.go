package aws

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAES256GCMEncryption(t *testing.T) {
	key := make([]byte, keySizeBytes)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("secret-aws-access-key-and-token")
	encrypted, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if string(encrypted) == string(plaintext) {
		t.Fatalf("ciphertext equals plaintext")
	}

	decrypted, err := decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("expected %q, got %q", string(plaintext), string(decrypted))
	}
}

func TestCredentialManagerStorage(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCredentialManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCredentialManager failed: %v", err)
	}

	// 1. Initial state: no stored credentials
	if cm.HasStoredCredentials() {
		t.Errorf("expected no stored credentials initially")
	}

	creds, err := cm.GetStoredCredentials()
	if err != nil {
		t.Fatalf("GetStoredCredentials failed: %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil credentials initially, got %+v", creds)
	}

	// 2. Test Key creation and file permissions
	key, err := cm.getOrCreateKey()
	if err != nil {
		t.Fatalf("getOrCreateKey failed: %v", err)
	}
	if len(key) != keySizeBytes {
		t.Fatalf("expected key size %d, got %d", keySizeBytes, len(key))
	}

	keyPath := filepath.Join(tmpDir, defaultKeyFileName)
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file does not exist: %v", err)
	}
	// Verify 0600 permissions on unix
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", fi.Mode().Perm())
	}

	// 3. Directly simulate storing encrypted credentials
	testCreds := CustomCredentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "mock-session-token",
	}

	// Encrypt manually using cm's key
	payload := []byte(`{"access_key_id":"AKIAIOSFODNN7EXAMPLE","secret_access_key":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY","session_token":"mock-session-token"}`)
	encrypted, err := encrypt(key, payload)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if err := os.WriteFile(cm.credFilePath(), encrypted, 0600); err != nil {
		t.Fatalf("write credentials file failed: %v", err)
	}

	if !cm.HasStoredCredentials() {
		t.Errorf("expected HasStoredCredentials to be true")
	}

	retrieved, err := cm.GetStoredCredentials()
	if err != nil {
		t.Fatalf("GetStoredCredentials failed: %v", err)
	}
	if retrieved == nil {
		t.Fatalf("expected retrieved credentials, got nil")
	}
	if retrieved.AccessKeyID != testCreds.AccessKeyID {
		t.Errorf("expected access key %q, got %q", testCreds.AccessKeyID, retrieved.AccessKeyID)
	}
	if retrieved.SecretAccessKey != testCreds.SecretAccessKey {
		t.Errorf("expected secret key %q, got %q", testCreds.SecretAccessKey, retrieved.SecretAccessKey)
	}
	if retrieved.SessionToken != testCreds.SessionToken {
		t.Errorf("expected session token %q, got %q", testCreds.SessionToken, retrieved.SessionToken)
	}

	// 4. Test ExportEnv
	envs := cm.ExportEnv("ap-south-1")
	envMap := make(map[string]string)
	for _, e := range envs {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	if envMap["AWS_ACCESS_KEY_ID"] != testCreds.AccessKeyID {
		t.Errorf("expected exported AWS_ACCESS_KEY_ID, got %v", envMap["AWS_ACCESS_KEY_ID"])
	}
	if envMap["AWS_SECRET_ACCESS_KEY"] != testCreds.SecretAccessKey {
		t.Errorf("expected exported AWS_SECRET_ACCESS_KEY, got %v", envMap["AWS_SECRET_ACCESS_KEY"])
	}
	if envMap["AWS_REGION"] != "ap-south-1" {
		t.Errorf("expected exported AWS_REGION ap-south-1, got %v", envMap["AWS_REGION"])
	}

	// 5. Test ClearCustomCredentials
	if err := cm.ClearCustomCredentials(); err != nil {
		t.Fatalf("ClearCustomCredentials failed: %v", err)
	}
	if cm.HasStoredCredentials() {
		t.Errorf("expected HasStoredCredentials to be false after clear")
	}
}

func TestMaskAccessKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"AKIAIOSFODNN7EXAMPLE", "AKIA************MPLE"},
		{"SHORTKEY", "****"},
		{"", "****"},
		{"AKIA12345678WXYZ", "AKIA********WXYZ"},
	}

	for _, tt := range tests {
		got := MaskAccessKey(tt.input)
		if got != tt.expected {
			t.Errorf("MaskAccessKey(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestVerifyIdentityNoCreds(t *testing.T) {
	tmpDir := t.TempDir()
	cm, _ := NewCredentialManager(tmpDir)

	ctx := context.Background()
	// Clear any environment keys so it doesn't accidentally succeed or pick up host keys
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_PROFILE", "non-existent-vib-profile-999")

	identity, err := cm.VerifyIdentity(ctx, "ap-south-1")
	if err != nil {
		t.Fatalf("expected VerifyIdentity to return error inside identity object, got error: %v", err)
	}
	if identity.Connected {
		t.Errorf("expected Connected=false for non-existent profile")
	}
}
