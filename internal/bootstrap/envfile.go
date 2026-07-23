package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadNacosServerAddressFromFiles loads only NACOS_SERVER_ADDR from the first
// existing bootstrap file. Business configuration must remain in Nacos.
func LoadNacosServerAddressFromFiles(paths ...string) error {
	if strings.TrimSpace(os.Getenv("NACOS_SERVER_ADDR")) != "" {
		return nil
	}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		loaded, err := loadNacosServerAddressFromFile(path)
		if err != nil {
			return err
		}
		if loaded {
			return nil
		}
	}
	return nil
}

func loadNacosServerAddressFromFile(path string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open Nacos bootstrap file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var serverAddr string
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return false, fmt.Errorf("invalid Nacos bootstrap line in %s: only NACOS_SERVER_ADDR is allowed", path)
		}
		key = strings.TrimSpace(key)
		if key != "NACOS_SERVER_ADDR" {
			return false, fmt.Errorf("only NACOS_SERVER_ADDR is allowed in %s; found %s", path, key)
		}
		if serverAddr != "" {
			return false, fmt.Errorf("duplicate NACOS_SERVER_ADDR in %s", path)
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" {
			return false, fmt.Errorf("NACOS_SERVER_ADDR is empty in %s", path)
		}
		serverAddr = value
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read Nacos bootstrap file %s: %w", path, err)
	}
	if serverAddr == "" {
		return false, fmt.Errorf("NACOS_SERVER_ADDR is missing from %s", path)
	}
	if err := os.Setenv("NACOS_SERVER_ADDR", serverAddr); err != nil {
		return false, fmt.Errorf("set NACOS_SERVER_ADDR from %s: %w", path, err)
	}
	return true, nil
}
