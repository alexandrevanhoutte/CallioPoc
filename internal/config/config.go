package config

import (
	"fmt"
	"os"
	"strconv"
)

const defaultPort = "8080"

type Config struct {
	Port string
}

func Load() (Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("invalid PORT %q: expected a number from 1 to 65535", port)
	}

	return Config{Port: port}, nil
}
