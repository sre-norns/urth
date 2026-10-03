package worker

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadInstallationKey creates a private seed once. A restart keeps the same identity.
func LoadInstallationKey(path string) (ed25519.PrivateKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("prepare worker identity directory: %w", err)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		seed := make([]byte, ed25519.SeedSize)
		if _, err = rand.Read(seed); err != nil {
			return nil, err
		}
		file, createErr := os.CreateTemp(filepath.Dir(path), ".worker-key-*")
		if createErr != nil {
			return nil, fmt.Errorf("create private worker identity: %w", createErr)
		}
		temporary := file.Name()
		defer os.Remove(temporary)
		_, err = file.WriteString(base64.RawURLEncoding.EncodeToString(seed) + "\n")
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("write worker identity: %w", err)
		}
		// Publish complete contents without replacing another process's key.
		if err = os.Link(temporary, path); err != nil && !os.IsExist(err) {
			return nil, fmt.Errorf("persist worker identity: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read worker identity metadata: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("worker identity must be a regular private file with mode 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read worker identity: %w", err)
	}
	seed, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid worker identity seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
