// Package runtimebundle defines the narrowly scoped redistributable runtime
// payload. Google SDK files, images, AVDs and account data are never bundled.
package runtimebundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const ScrcpyVersion = "5.0.1"
const ScrcpyHash = "764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536"

type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
}
type Manifest struct {
	Version     int    `json:"version"`
	Target      string `json:"target"`
	JavaRelease string `json:"javaRelease"`
	Files       []File `json:"files"`
}

func JavaArchive(target string) (string, error) {
	switch target {
	case "windows/amd64":
		return "java.zip", nil
	case "linux/amd64":
		return "java.tar.gz", nil
	default:
		return "", fmt.Errorf("Unsupported runtime bundle target %q", target)
	}
}

func Load(directory string) (Manifest, error) {
	var manifest Manifest
	file, err := os.Open(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	javaArchive, targetErr := JavaArchive(manifest.Target)
	if manifest.Version != 1 || targetErr != nil || manifest.JavaRelease == "" {
		return manifest, fmt.Errorf("Unsupported runtime bundle")
	}
	required := map[string]bool{javaArchive: true, "java-sources.tar.gz": true, "scrcpy-server-v" + ScrcpyVersion: true, "scrcpy-sources.tar.gz": true, "THIRD_PARTY_NOTICES.txt": true}
	found := map[string]bool{}
	for _, entry := range manifest.Files {
		hash, err := hex.DecodeString(entry.SHA256)
		if !required[entry.Name] || found[entry.Name] || err != nil || len(hash) != 32 {
			return manifest, fmt.Errorf("Invalid or unapproved bundle component %q", entry.Name)
		}
		found[entry.Name] = true
		if entry.Name == "scrcpy-server-v"+ScrcpyVersion && entry.SHA256 != ScrcpyHash {
			return manifest, fmt.Errorf("Incorrect scrcpy server version/checksum")
		}
	}
	if len(found) != len(required) {
		return manifest, fmt.Errorf("Bundle is missing runtime files, matching sources, or notices")
	}
	return manifest, nil
}
func Hash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func Verify(directory string) (Manifest, error) {
	manifest, err := Load(directory)
	if err != nil {
		return manifest, err
	}
	for _, entry := range manifest.Files {
		hash, err := Hash(filepath.Join(directory, entry.Name))
		if err != nil {
			return manifest, err
		}
		if hash != entry.SHA256 {
			return manifest, fmt.Errorf("Runtime bundle checksum mismatch: %s", entry.Name)
		}
	}
	return manifest, nil
}
func (manifest Manifest) Entry(name string) (File, bool) {
	for _, entry := range manifest.Files {
		if entry.Name == name {
			return entry, true
		}
	}
	return File{}, false
}
