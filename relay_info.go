package main

import (
	"fmt"
	"os"
	"runtime"
)

const viewerAppName = "clay-viewer"

func getViewerUserDataPath() (string, error) {
	var appDataPath string
	if runtime.GOOS == "windows" {
		appDataPath = os.Getenv("APPDATA")
	} else if runtime.GOOS == "darwin" {
		appDataPath = os.Getenv("HOME") + "/Library/Application Support"
	} else if runtime.GOOS == "linux" {
		appDataPath = os.Getenv("HOME") + "/.config"
	} else {
		return "", fmt.Errorf("unsupported os on this build: %s", runtime.GOOS)
	}
	if appDataPath == "" {
		return "", fmt.Errorf("could not find app data path")
	}
	return appDataPath + "/" + viewerAppName, nil
}

type RelayInfo struct {
	Path string
}

func newRelayInfo(port int) (*RelayInfo, error) {
	userDataPath, err := getViewerUserDataPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(userDataPath); os.IsNotExist(err) {
		Trace.Printf("creating user data path: %s", userDataPath)
		err = os.Mkdir(userDataPath, 0755)
		if err != nil {
			return nil, err
		}
	}
	relayInfoFilePath := userDataPath + "/" + fmt.Sprintf("relayinfo-%d.json", port)
	relayInfoFile, err := os.Create(relayInfoFilePath)
	if err != nil {
		return nil, err
	}
	defer func(relayInfoFile *os.File) {
		err := relayInfoFile.Close()
		if err != nil {
			Error.Printf("could not close relay info file: %s", err)
		}
	}(relayInfoFile)

	_, err = relayInfoFile.WriteString(fmt.Sprintf(`{"port": %d, "process_id": %d}`, port, os.Getpid()))
	if err != nil {
		return nil, err
	}
	return &RelayInfo{
		Path: relayInfoFilePath,
	}, nil
}

func (r *RelayInfo) Close() error {
	return os.Remove(r.Path)
}
