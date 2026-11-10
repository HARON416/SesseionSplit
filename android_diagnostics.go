package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const androidLogLimit = 2 * 1024 * 1024
const androidLogTail = 8 * 1024

// Keep a bounded disk log and an in-memory tail, even if disk writes fail.
// Logging failures must never terminate the emulator's output pipe.
type androidDiagnosticLog struct {
	mu   sync.Mutex
	file *os.File
	path string
	size int
	tail []byte
	err  error
}

func newAndroidDiagnosticLog(root, name, kind string) *androidDiagnosticLog {
	log := &androidDiagnosticLog{}
	if root == "" || !avdNamePattern.MatchString(name) {
		log.err = fmt.Errorf("no valid Android log directory or device name")
		return log
	}
	log.path = filepath.Join(root, "logs", name+"-"+kind+".log")
	if err := os.MkdirAll(filepath.Dir(log.path), 0700); err != nil {
		log.err = err
		return log
	}
	// Preserve the previous run without allowing an unbounded collection of logs.
	previous := log.path + ".previous"
	if err := os.Remove(previous); err != nil && !os.IsNotExist(err) {
		log.err = err
		return log
	}
	if err := os.Rename(log.path, previous); err != nil && !os.IsNotExist(err) {
		log.err = err
		return log
	}
	log.file, log.err = os.OpenFile(log.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	return log
}

func (log *androidDiagnosticLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	length := len(data)
	if len(data) >= androidLogTail {
		log.tail = append(log.tail[:0], data[len(data)-androidLogTail:]...)
	} else {
		log.tail = append(log.tail, data...)
		if len(log.tail) > androidLogTail {
			log.tail = append(log.tail[:0], log.tail[len(log.tail)-androidLogTail:]...)
		}
	}
	if log.file != nil && log.err == nil {
		if log.size+len(data) > androidLogLimit {
			log.err = log.file.Truncate(0)
			if log.err == nil {
				_, log.err = log.file.Seek(0, 0)
			}
			log.size = 0
			data = log.tail
		}
		if log.err == nil {
			var written int
			written, log.err = log.file.Write(data)
			log.size += written
		}
	}
	return length, nil
}

func (log *androidDiagnosticLog) close() {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file != nil {
		if err := log.file.Close(); log.err == nil {
			log.err = err
		}
		log.file = nil
	}
}

func (log *androidDiagnosticLog) details() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	location := "Log: " + log.path
	if log.err != nil {
		location += "\nCould not save the full log: " + log.err.Error()
	}
	return location + "\n\n" + strings.TrimSpace(string(log.tail))
}

func monitorAndroidCrashes(adb, serial string, log *androidDiagnosticLog) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer log.close()
		fmt.Fprintf(log, "Android crash buffer for %s, started %s\n", serial, time.Now().UTC().Format(time.RFC3339))
		for _, args := range [][]string{{"-s", serial, "wait-for-device"}, {"-s", serial, "logcat", "-b", "crash", "-v", "threadtime"}} {
			command := exec.CommandContext(ctx, adb, args...)
			hideAndroidCommand(command)
			command.Stdout, command.Stderr = log, log
			command.WaitDelay = 2 * time.Second
			if err := command.Run(); err != nil {
				if ctx.Err() == nil {
					fmt.Fprintf(log, "Crash capture stopped: %v\n", err)
				}
				return
			}
		}
	}()
	return func() { cancel(); <-done }
}
