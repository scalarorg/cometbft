// Package os provides utility functions for OS-level interactions.
//
// Deprecated: This package will be removed in a future release. Users should migrate
// off this package as its functionality will no longer be part of the exported interface.
package os

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cometbft/cometbft/v2/libs/log"
)

type logger interface {
	Info(msg string, keyvals ...any)
}

// TrapSignal catches the SIGTERM/SIGINT and executes cb function. After that it exits
// with code 0.
// Deprecated: This function will be removed in a future release. Do not use.
func TrapSignal(logger logger, cb func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range c {
			logger.Info("signal trapped", "msg", log.NewLazySprintf("captured %v, exiting...", sig))
			if cb != nil {
				cb()
			}
			os.Exit(0)
		}
	}()
}

// Kill the running process by sending itself SIGTERM.
// Deprecated: This function will be removed in a future release. Do not use.
func Kill() error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return p.Signal(syscall.SIGTERM)
}

// Deprecated: This function will be removed in a future release. Do not use.
func Exit(s string) {
	fmt.Println(s)
	os.Exit(1)
}

// EnsureDir ensures the given directory exists, creating it if necessary.
// Errors if the path already exists as a non-directory.
// Deprecated: This function will be removed in a future release. Do not use.
func EnsureDir(dir string, mode os.FileMode) error {
	err := os.MkdirAll(dir, mode)
	if err != nil {
		return fmt.Errorf("could not create directory %q: %w", dir, err)
	}
	return nil
}

// Deprecated: This function will be removed in a future release. Do not use.
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// Deprecated: This function will be removed in a future release. Do not use.
func ReadFile(filePath string) ([]byte, error) {
	return os.ReadFile(filePath)
}

// Deprecated: This function will be removed in a future release. Do not use.
func MustReadFile(filePath string) []byte {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		Exit(fmt.Sprintf("MustReadFile failed: %v", err))
		return nil
	}
	return fileBytes
}

// Deprecated: This function will be removed in a future release. Do not use.
func WriteFile(filePath string, contents []byte, mode os.FileMode) error {
	return os.WriteFile(filePath, contents, mode)
}

// Deprecated: This function will be removed in a future release. Do not use.
func MustWriteFile(filePath string, contents []byte, mode os.FileMode) {
	err := WriteFile(filePath, contents, mode)
	if err != nil {
		Exit(fmt.Sprintf("MustWriteFile failed: %v", err))
	}
}

// CopyFile copies a file. It truncates the destination file if it exists.
// Deprecated: This function will be removed in a future release. Do not use.
func CopyFile(src, dst string) error {
	srcfile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcfile.Close()

	info, err := srcfile.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("cannot read from directories")
	}

	// create new file, truncate if exists and apply same permissions as the original one
	dstfile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer dstfile.Close()

	_, err = io.Copy(dstfile, srcfile)
	return err
}
