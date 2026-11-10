// Prepare an installer payload using original Temurin/scrcpy artifacts only.
// Source archives and upstream license files accompany every binary payload.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"isolate/internal/runtimebundle"
)

func main() {
	out := flag.String("out", "", "Payload output directory (defaults to .tools/runtime/OS-ARCH)")
	target := flag.String("target", "windows/amd64", "Runtime target: windows/amd64 or linux/amd64")
	debConfig := flag.String("deb-config", "", "Generate a bundled DEB configuration from this base nfpm config")
	verify := flag.Bool("verify", false, "Verify existing payload without downloading")
	flag.Parse()
	if *out == "" {
		*out = filepath.Join(".tools", "runtime", strings.ReplaceAll(*target, "/", "-"))
	}
	var err error
	if _, err = runtimebundle.JavaArchive(*target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *verify {
		var manifest runtimebundle.Manifest
		manifest, err = runtimebundle.Verify(*out)
		if err == nil && manifest.Target != *target {
			err = errors.New("Runtime payload target does not match requested target")
		}
	} else {
		err = prepare(*out, *target)
	}
	if err == nil && *debConfig != "" {
		err = writeDEBConfig(*debConfig, *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Verified bundled tools:", *out, "(Android/Google Play downloads are still required)")
}
func getJSON(ctx context.Context, url string, target any) error {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "Isolate-runtime-builder")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", url, response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target)
}
func fetch(ctx context.Context, url, destination, expected string) (string, error) {
	if expected != "" {
		if hash, err := runtimebundle.Hash(destination); err == nil && hash == expected {
			return hash, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("%s: HTTP %d", url, response.StatusCode)
	}
	file, err := os.CreateTemp(filepath.Dir(destination), "bundle-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	fmt.Printf("Downloading %s (%.1f MiB)\n", filepath.Base(destination), float64(response.ContentLength)/(1<<20))
	if response.ContentLength > 8<<20 && response.ContentLength <= 1<<30 && response.Header.Get("Accept-Ranges") == "bytes" {
		size := response.ContentLength
		_ = response.Body.Close()
		err = fetchRanges(ctx, url, file, size)
	} else {
		_, err = io.Copy(file, io.LimitReader(response.Body, (1<<30)+1))
	}
	if err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	info, err := os.Stat(file.Name())
	if err != nil || info.Size() > 1<<30 {
		return "", errors.New("Runtime artifact exceeds size limit")
	}
	hash, err := runtimebundle.Hash(file.Name())
	if err != nil {
		return "", err
	}
	if expected != "" && hash != expected {
		return "", fmt.Errorf("Checksum mismatch for %s", destination)
	}
	return hash, os.Rename(file.Name(), destination)
}
func prepare(out, target string) error {
	if manifest, err := runtimebundle.Verify(out); err == nil && manifest.Target == target {
		return nil
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var java []struct {
		Release string `json:"release_name"`
		Binary  struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binary"`
	}
	if err := getJSON(ctx, "https://api.adoptium.net/v3/assets/latest/21/hotspot?architecture=x64&image_type=jre&os="+strings.Split(target, "/")[0]+"&vendor=eclipse", &java); err != nil {
		return err
	}
	if len(java) == 0 {
		return errors.New("No Java runtime release found")
	}
	pkg := java[0].Binary.Package
	if !strings.HasPrefix(pkg.Link, "https://github.com/adoptium/temurin21-binaries/releases/download/") {
		return errors.New("Unexpected Java artifact URL")
	}
	// Match sources to this exact binary release, not another latest-version query.
	var release struct {
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	releaseAPI := "https://api.github.com/repos/adoptium/temurin21-binaries/releases/tags/" + strings.ReplaceAll(java[0].Release, "+", "%2B")
	if err := getJSON(ctx, releaseAPI, &release); err != nil {
		return err
	}
	sourceURL, sourceHash := "", ""
	for _, asset := range release.Assets {
		if strings.HasPrefix(asset.Name, "OpenJDK21U-jdk-sources_") && strings.HasSuffix(asset.Name, ".tar.gz") {
			sourceURL = asset.URL
			sourceHash = strings.TrimPrefix(asset.Digest, "sha256:")
		}
	}
	if sourceURL == "" || len(sourceHash) != 64 {
		return errors.New("Matching Java sources with a verified digest are unavailable; refusing to redistribute Java without them")
	}
	manifest := runtimebundle.Manifest{Version: 1, Target: target, JavaRelease: java[0].Release}
	javaArchive, err := runtimebundle.JavaArchive(target)
	if err != nil {
		return err
	}
	for _, item := range []struct{ name, url, hash string }{
		{javaArchive, pkg.Link, pkg.Checksum},
		{"java-sources.tar.gz", sourceURL, sourceHash},
		{"scrcpy-server-v" + runtimebundle.ScrcpyVersion, "https://github.com/Genymobile/scrcpy/releases/download/v" + runtimebundle.ScrcpyVersion + "/scrcpy-server-v" + runtimebundle.ScrcpyVersion, runtimebundle.ScrcpyHash},
		{"scrcpy-sources.tar.gz", "https://codeload.github.com/Genymobile/scrcpy/tar.gz/refs/tags/v" + runtimebundle.ScrcpyVersion, ""},
	} {
		// Sources and scrcpy are identical across OS targets. Reuse only a
		// checksum-verified artifact from a sibling manifest with the same source URL.
		hash, reused, err := reuseArtifact(out, item.name, item.url, item.hash)
		if err != nil {
			return err
		}
		if !reused {
			hash, err = fetch(ctx, item.url, filepath.Join(out, item.name), item.hash)
		}
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, runtimebundle.File{Name: item.name, SHA256: hash, Source: item.url})
	}
	notices := `Isolate bundled tools for ` + target + `

Eclipse Temurin Java 21: ` + java[0].Release + `
Original binaries are unmodified. Included ` + javaArchive + ` retains upstream legal
notices and GPLv2 with Classpath Exception terms. Matching complete upstream
source archive is supplied alongside the binary as java-sources.tar.gz.
https://adoptium.net/docs/faq/

scrcpy ` + runtimebundle.ScrcpyVersion + `
Original server binary is unmodified. Included scrcpy-sources.tar.gz contains
the corresponding upstream source, LICENSE (Apache-2.0), and notices.
https://github.com/Genymobile/scrcpy/tree/v` + runtimebundle.ScrcpyVersion + `

Google SDK tools and Google Play system images are not included. They download
directly from Google during setup after the client's applicable license prompts.
This installer is not a fully offline Android installation.
`
	path := filepath.Join(out, "THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(path, []byte(notices), 0644); err != nil {
		return err
	}
	hash, err := runtimebundle.Hash(path)
	if err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, runtimebundle.File{Name: filepath.Base(path), SHA256: hash})
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(out, "manifest.json.tmp")
	if err = os.WriteFile(temporary, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err = os.Rename(temporary, filepath.Join(out, "manifest.json")); err != nil {
		return err
	}
	_, err = runtimebundle.Verify(out)
	return err
}

// Range transfers shorten build preparation on slow connections while retaining
// full-file SHA-256 verification. Each response must match its requested range.
func fetchRanges(parent context.Context, url string, file *os.File, size int64) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := file.Truncate(size); err != nil {
		return err
	}
	const workers = 6
	var group sync.WaitGroup
	failures := make(chan error, workers)
	for index := int64(0); index < workers; index++ {
		start, end := size*index/workers, size*(index+1)/workers-1
		group.Add(1)
		go func() {
			defer group.Done()
			request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				failures <- err
				cancel()
				return
			}
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				defer response.Body.Close()
				if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, size) {
					err = errors.New("Download server returned an unexpected byte range")
				} else {
					_, err = io.CopyN(io.NewOffsetWriter(file, start), response.Body, end-start+1)
				}
			}
			if err != nil {
				failures <- err
				cancel()
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func reuseArtifact(out, name, source, expected string) (string, bool, error) {
	for _, target := range []string{"windows-amd64", "linux-amd64"} {
		sibling := filepath.Join(filepath.Dir(out), target)
		if filepath.Clean(sibling) == filepath.Clean(out) {
			continue
		}
		manifest, err := runtimebundle.Load(sibling)
		if err != nil {
			continue
		}
		entry, ok := manifest.Entry(name)
		if !ok || entry.Source != source || (expected != "" && entry.SHA256 != expected) {
			continue
		}
		inputPath := filepath.Join(sibling, name)
		hash, err := runtimebundle.Hash(inputPath)
		if err != nil || hash != entry.SHA256 {
			continue
		}
		input, err := os.Open(inputPath)
		if err != nil {
			return "", false, err
		}
		output, err := os.CreateTemp(out, "reuse-*")
		if err != nil {
			input.Close()
			return "", false, err
		}
		_, err = io.Copy(output, input)
		input.Close()
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(output.Name(), filepath.Join(out, name))
		}
		_ = os.Remove(output.Name())
		if err != nil {
			return "", false, err
		}
		fmt.Println("Reusing verified artifact:", name)
		return hash, true, nil
	}
	return "", false, nil
}
func writeDEBConfig(base, directory string) error {
	manifest, err := runtimebundle.Verify(directory)
	if err != nil {
		return err
	}
	if manifest.Target != "linux/amd64" {
		return errors.New("A Linux amd64 payload is required for the bundled DEB")
	}
	data, err := os.ReadFile(base)
	if err != nil {
		return err
	}
	var config map[string]any
	if err = yaml.Unmarshal(data, &config); err != nil {
		return err
	}
	contents, ok := config["contents"].([]any)
	if !ok {
		return errors.New("Base DEB config has no contents list")
	}
	names := []string{"manifest.json"}
	for _, entry := range manifest.Files {
		names = append(names, entry.Name)
	}
	for _, name := range names {
		source, err := filepath.Abs(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		contents = append(contents, map[string]any{"src": source, "dst": "/usr/lib/isolate/runtime/" + name, "file_info": map[string]any{"mode": 0644}})
	}
	config["contents"] = contents
	data, err = yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "nfpm-bundled.yaml"), data, 0644)
}
