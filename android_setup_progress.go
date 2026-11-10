package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var androidInstallerProgress = regexp.MustCompile(`\[[^\]\r\n]{0,80}\][ \t]*([0-9]{1,3}(?:\.[0-9]+)?)%[ \t]*([^\r\n]*)`)

// Percent is scoped to the current transfer or the SDK installer's reported
// operation. Never infer an overall percentage from unrelated package sizes.
func (setup *androidSetup) resetProgressLocked(phase string) {
	setup.status.Phase = phase
	setup.status.Percent = -1
	setup.status.ProgressLabel = ""
	setup.status.Detail = ""
	setup.status.DownloadedBytes = 0
	setup.status.TotalBytes = 0
	setup.status.BytesPerSecond = 0
	setup.status.LastActivity = time.Now().UnixMilli()
	setup.outputTail = ""
}
func (setup *androidSetup) transferProgress(downloaded, total int64, started time.Time) {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	setup.status.Phase = "downloading"
	setup.status.ProgressLabel = "Current download"
	setup.status.Detail = ""
	setup.status.DownloadedBytes = downloaded
	setup.status.TotalBytes = max(0, total)
	setup.status.Percent = -1
	if total > 0 {
		setup.status.Percent = min(100, float64(downloaded)*100/float64(total))
	}
	if elapsed := time.Since(started).Seconds(); elapsed > 0 {
		setup.status.BytesPerSecond = float64(downloaded) / elapsed
	}
	setup.status.LastActivity = time.Now().UnixMilli()
}
func (setup *androidSetup) activity(phase, detail string) {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	setup.resetProgressLocked(phase)
	setup.status.Detail = detail
}
func (setup *androidSetup) installerProgressLocked(data string) {
	setup.outputTail += data
	// sdkmanager writes progress with carriage returns, sometimes across writes.
	// Retain only its current line plus a short history, never the whole log.
	if len(setup.outputTail) > 8192 {
		setup.outputTail = setup.outputTail[len(setup.outputTail)-8192:]
	}
	matches := androidInstallerProgress.FindAllStringSubmatch(setup.outputTail, -1)
	if len(matches) == 0 {
		return
	}
	match := matches[len(matches)-1]
	percent, err := strconv.ParseFloat(match[1], 64)
	if err != nil || percent > 100 {
		return
	}
	setup.status.Percent = percent
	setup.status.ProgressLabel = "Android installer progress"
	detail := strings.TrimSpace(match[2])
	if len(detail) > 240 {
		detail = detail[:240]
	}
	setup.status.Detail = detail
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "download"):
		setup.status.Phase = "downloading"
	case strings.Contains(lower, "unzip"), strings.Contains(lower, "extract"), strings.Contains(lower, "install"):
		setup.status.Phase = "installing"
	case strings.Contains(lower, "load"), strings.Contains(lower, "fetch"):
		setup.status.Phase = "checking"
	}
}
