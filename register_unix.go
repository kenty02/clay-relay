//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func getManifestPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	var manifestPath string
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		manifestPath = filepath.Join(homeDir, "Library/Application Support/Google/Chrome/NativeMessagingHosts/"+nativeMessagingHostName+".json")
	} else if runtime.GOOS == "linux" {
		manifestPath = filepath.Join(homeDir, ".config/google-chrome/NativeMessagingHosts/"+nativeMessagingHostName+".json")
	} else {
		return "", fmt.Errorf("unsupported os on this build: %s", runtime.GOOS)
	}
	return manifestPath, nil
}

// registerNativeMessagingHost registers the native messaging host to the registry depending on the OS.
func registerNativeMessagingHost() error {
	manifestPath, err := getManifestPath()
	if err != nil {
		return err
	}
	err = generateManifest(manifestPath)
	if err != nil {
		return err
	}

	return nil
}

// unregisterNativeMessagingHost unregisters the native messaging host from the registry depending on the OS.
func unregisterNativeMessagingHost() error {
	manifestPath, err := getManifestPath()
	// exists check
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil
	}
	err = os.Remove(manifestPath)
	if err != nil {
		return err
	}

	return nil
}
