package main

import (
	"encoding/json"
	"os"
)

const nativeMessagingHostName = "net.hu2ty.clay_relay"

type nativeMessagingHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// generateManifest generates the manifest file for the native messaging host to the executable dir. Recreate the manifest file if it already exists.
func generateManifest(path string) error {
	// get the path to the executable
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	manifest := nativeMessagingHostManifest{
		Name:           nativeMessagingHostName,
		Description:    "Clay relay",
		Path:           ex,
		Type:           "stdio",
		AllowedOrigins: []string{"chrome-extension://ofgodpngengnlbmpnjhondghmdeembik/"},
	}
	manifestJson, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	_, err = f.Write(manifestJson)
	if err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		panic(err)
	}
	return nil
}
