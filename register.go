package main

import (
	"encoding/json"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
)

const nativeMessagingHostName = "net.hu2ty.clay_relay"

type nativeMessagingHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func registerNativeMessagingHost() error {
	manifestPath, err := generateManifest()
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Google\Chrome\NativeMessagingHosts\`+nativeMessagingHostName, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	err = key.SetStringValue("", manifestPath) // "" means "(Default)" here
	if err != nil {
		return err
	}

	return nil
}

func generateManifest() (string, error) {
	// get the path to the executable
	ex, err := os.Executable()
	if err != nil {
		return "", err
	}
	manifest := nativeMessagingHostManifest{
		Name:           nativeMessagingHostName,
		Description:    "Clay relay",
		Path:           ex,
		Type:           "stdio",
		AllowedOrigins: []string{"chrome-extension://mekgccaopmfdlnpcpohibfckbjfklmdi/"},
	}
	manifestJson, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	exPath := filepath.Dir(ex)
	manifestPath := filepath.Join(exPath, "net.hu2ty.clay_relay.json")
	f, err := os.OpenFile(manifestPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return "", err
	}

	_, err = f.Write(manifestJson)
	if err != nil {
		return "", err
	}

	if err := f.Close(); err != nil {
		panic(err)
	}
	return manifestPath, nil
}
