//go:build windows

package main

import (
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
)

func getManifestPath() (string, error) {
	ex, err := os.Executable()
	exPath := filepath.Dir(ex)
	manifestPath := filepath.Join(exPath, "net.hu2ty.clay_relay.json")
	return manifestPath, err
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
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Google\Chrome\NativeMessagingHosts\`+nativeMessagingHostName, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	err = key.SetStringValue("", manifestPath) // "" means "(Default)"
	if err != nil {
		return err
	}

	return nil
}

// unregisterNativeMessagingHost unregisters the native messaging host from the registry depending on the OS.
func unregisterNativeMessagingHost() error {
	err := registry.DeleteKey(registry.CURRENT_USER, `Software\Google\Chrome\NativeMessagingHosts\`+nativeMessagingHostName)
	if err != nil {
		return err
	}

	return nil
}
