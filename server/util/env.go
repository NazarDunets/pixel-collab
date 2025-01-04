package util

import (
	"log"
	"os"
)

const (
	ENV_PORT = "PORT"
)

func GetPort() string {
	port, exists := os.LookupEnv(ENV_PORT)
	if !exists {
		log.Fatalf("Missing %s environment variable", ENV_PORT)
	}
	return port
}

func GetBaseUrl() string {
	return "http://localhost:" + GetPort()
}
