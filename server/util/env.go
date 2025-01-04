package util

import (
	"log"
	"os"
)

const (
	ENV_PORT      = "PORT"
	ENV_LOCALHOST = "LOCALHOST"
)

func GetPort() string {
	port, exists := os.LookupEnv(ENV_PORT)
	if !exists {
		log.Fatalf("Missing %s environment variable", ENV_PORT)
	}
	return port
}

func GetStartAddress() string {
	localhostStr, exists := os.LookupEnv(ENV_LOCALHOST)
	localhostMode := false
	if exists && localhostStr == "true" {
		localhostMode = true
	}

	if localhostMode {
		return "localhost:" + GetPort()
	} else {
		return ":" + GetPort()
	}
}

func GetBaseUrl() string {
	return "http://localhost:" + GetPort()
}
