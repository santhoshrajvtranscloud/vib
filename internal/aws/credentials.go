package aws

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	defaultKeyFileName  = "vib.key"
	defaultCredFileName = "credentials.enc"
	keySizeBytes        = 32 // AES-256
)

// CustomCredentials holds AWS access credentials.
type CustomCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
}

// IdentityInfo contains metadata verified against AWS STS.
type IdentityInfo struct {
	Connected       bool   `json:"connected"`
	AccountID       string `json:"account_id,omitempty"`
	ARN             string `json:"arn,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	Source          string `json:"source"` // "encrypted_store", "env/profile", or "none"
	MaskedAccessKey string `json:"masked_access_key,omitempty"`
	Region          string `json:"region,omitempty"`
	Error           string `json:"error,omitempty"`
}

// CredentialManager manages credential resolution, local AES-256-GCM storage, and verification.
type CredentialManager struct {
	storageDir string
	mu         sync.RWMutex
}

// NewCredentialManager initializes a CredentialManager.
// If customDir is empty, defaults to ~/.vib/
func NewCredentialManager(customDir ...string) (*CredentialManager, error) {
	dir := ""
	if len(customDir) > 0 && strings.TrimSpace(customDir[0]) != "" {
		dir = customDir[0]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		dir = filepath.Join(home, ".vib")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create credentials directory %q: %w", dir, err)
	}

	return &CredentialManager{
		storageDir: dir,
	}, nil
}

// StorageDir returns the configured directory for key and credential storage.
func (cm *CredentialManager) StorageDir() string {
	return cm.storageDir
}

// keyFilePath returns the path to the encryption key file.
func (cm *CredentialManager) keyFilePath() string {
	return filepath.Join(cm.storageDir, defaultKeyFileName)
}

// credFilePath returns the path to the encrypted credentials file.
func (cm *CredentialManager) credFilePath() string {
	return filepath.Join(cm.storageDir, defaultCredFileName)
}

// getOrCreateKey loads or generates the 32-byte AES-256 key.
func (cm *CredentialManager) getOrCreateKey() ([]byte, error) {
	keyPath := filepath.Clean(cm.keyFilePath())
	if data, err := os.ReadFile(keyPath); err == nil { // #nosec G304 -- controlled internal storage path
		if len(data) == keySizeBytes {
			return data, nil
		}
	}

	// Generate a new 256-bit random key
	key := make([]byte, keySizeBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate random encryption key: %w", err)
	}

	// Save with strict 0600 permissions
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("failed to save encryption key to %s: %w", keyPath, err)
	}

	return key, nil
}

// encrypt data using AES-256-GCM.
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt data using AES-256-GCM.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, cipherData := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, cipherData, nil)
}

// defaultHTTPClient returns an HTTP client configured with a 15-second timeout.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
	}
}

// SaveCustomCredentials encrypts and stores the credentials, then tests them via STS.
func (cm *CredentialManager) SaveCustomCredentials(ctx context.Context, creds CustomCredentials, testRegion string) (*IdentityInfo, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	keyID := strings.TrimSpace(creds.AccessKeyID)
	if keyID == "" {
		return nil, errors.New("access_key_id is required")
	}
	if !strings.HasPrefix(keyID, "AKIA") && !strings.HasPrefix(keyID, "ASIA") {
		return nil, errors.New("invalid AWS access key ID format: must begin with AKIA (standard) or ASIA (temporary STS)")
	}
	if strings.TrimSpace(creds.SecretAccessKey) == "" {
		return nil, errors.New("secret_access_key is required")
	}

	if testRegion == "" {
		testRegion = "ap-south-1"
	}

	// Test credentials before saving with hardened timeout and adaptive standard retries
	testCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(testRegion),
		awsconfig.WithHTTPClient(defaultHTTPClient()),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) {
				o.MaxAttempts = 3
			})
		}),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			keyID,
			creds.SecretAccessKey,
			creds.SessionToken,
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to configure AWS with provided credentials: %w", err)
	}

	stsClient := sts.NewFromConfig(testCfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("AWS credential validation failed: %w", err)
	}

	// Serialize and encrypt immediately before disk write
	payload, err := json.Marshal(creds) // #nosec G117 -- credentials are encrypted immediately with AES-256-GCM
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range payload {
			payload[i] = 0
		}
	}()

	key, err := cm.getOrCreateKey()
	if err != nil {
		return nil, err
	}

	encrypted, err := encrypt(key, payload)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	if err := os.WriteFile(cm.credFilePath(), encrypted, 0600); err != nil {
		return nil, fmt.Errorf("failed to write encrypted credentials: %w", err)
	}

	return &IdentityInfo{
		Connected:       true,
		AccountID:       aws.ToString(identity.Account),
		ARN:             aws.ToString(identity.Arn),
		UserID:          aws.ToString(identity.UserId),
		Source:          "encrypted_store",
		MaskedAccessKey: MaskAccessKey(keyID),
		Region:          testRegion,
	}, nil
}

// ClearCustomCredentials removes any stored encrypted credentials.
func (cm *CredentialManager) ClearCustomCredentials() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	credFile := cm.credFilePath()
	if err := os.Remove(credFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credentials file: %w", err)
	}
	return nil
}

// GetStoredCredentials returns the decrypted custom credentials if present.
func (cm *CredentialManager) GetStoredCredentials() (*CustomCredentials, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	credFile := filepath.Clean(cm.credFilePath())
	encrypted, err := os.ReadFile(credFile) // #nosec G304 -- controlled internal storage path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	key, err := cm.getOrCreateKey()
	if err != nil {
		return nil, err
	}

	plaintext, err := decrypt(key, encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	var creds CustomCredentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("corrupted credentials payload: %w", err)
	}

	return &creds, nil
}

// HasStoredCredentials checks if custom credentials exist on disk.
func (cm *CredentialManager) HasStoredCredentials() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	_, err := os.Stat(cm.credFilePath())
	return err == nil
}

// GetAWSConfig builds an aws.Config using either the stored encrypted credentials
// or the local default AWS credential chain (~/.aws, env vars, IAM role).
func (cm *CredentialManager) GetAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	if region == "" {
		region = "ap-south-1"
	}

	creds, err := cm.GetStoredCredentials()
	if err != nil {
		return aws.Config{}, err
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithHTTPClient(defaultHTTPClient()),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) {
				o.MaxAttempts = 3
			})
		}),
	}

	if creds != nil {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			creds.SessionToken,
		)))
	}

	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// VerifyIdentity checks connectivity with AWS STS GetCallerIdentity.
func (cm *CredentialManager) VerifyIdentity(ctx context.Context, region string) (*IdentityInfo, error) {
	if region == "" {
		region = "ap-south-1"
	}

	storedCreds, _ := cm.GetStoredCredentials()
	cfg, err := cm.GetAWSConfig(ctx, region)
	if err != nil {
		return &IdentityInfo{
			Connected: false,
			Source:    "none",
			Region:    region,
			Error:     err.Error(),
		}, nil
	}

	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		source := "env/profile"
		if storedCreds != nil {
			source = "encrypted_store"
		}
		return &IdentityInfo{
			Connected: false,
			Source:    source,
			Region:    region,
			Error:     err.Error(),
		}, nil
	}

	source := "env/profile"
	maskedKey := ""
	if storedCreds != nil {
		source = "encrypted_store"
		maskedKey = MaskAccessKey(storedCreds.AccessKeyID)
	} else if envKey := os.Getenv("AWS_ACCESS_KEY_ID"); envKey != "" {
		maskedKey = MaskAccessKey(envKey)
	}

	return &IdentityInfo{
		Connected:       true,
		AccountID:       aws.ToString(identity.Account),
		ARN:             aws.ToString(identity.Arn),
		UserID:          aws.ToString(identity.UserId),
		Source:          source,
		MaskedAccessKey: maskedKey,
		Region:          region,
	}, nil
}

// ExportEnv exports active AWS credentials as environment variables suitable for passing to Packer runner.
func (cm *CredentialManager) ExportEnv(region string) []string {
	var envs []string
	if region != "" {
		envs = append(envs, "AWS_DEFAULT_REGION="+region, "AWS_REGION="+region)
	}

	creds, err := cm.GetStoredCredentials()
	if err == nil && creds != nil {
		envs = append(envs,
			"AWS_ACCESS_KEY_ID="+creds.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY="+creds.SecretAccessKey,
		)
		if creds.SessionToken != "" {
			envs = append(envs, "AWS_SESSION_TOKEN="+creds.SessionToken)
		}
	}
	return envs
}

// MaskAccessKey masks an AWS access key for secure display (e.g., AKIA************1234).
func MaskAccessKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}
