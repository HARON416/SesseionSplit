package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultProfileRoot(configDir string) (string, error) {
	for _, name := range []string{"isolate", "sessionsplit"} {
		root := filepath.Join(configDir, name)
		info, err := os.Stat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("find profile storage: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("profile storage is not a directory: %s", root)
		}
		return root, nil
	}
	return filepath.Join(configDir, "isolate"), nil
}
