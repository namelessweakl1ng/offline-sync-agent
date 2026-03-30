package network

import (
	"crypto/tls"
	"net/http"
	"offline-sync-agent/internal/config"
	"time"
)

func GetAuthToken() string {
	return config.AppConfig.AuthToken
}

func IsOnline() (bool, time.Duration, string) {
	client := http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	start := time.Now()

	req, _ := http.NewRequest("GET", "https://localhost:8080/pull", nil)
	req.Header.Set("Authorization", "Bearer "+GetAuthToken())

	_, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return false, latency, "offline"
	}

	if latency < 500*time.Millisecond {
		return true, latency, "fast"
	} else if latency < 2*time.Second {
		return true, latency, "medium"
	}
	return true, latency, "slow"
}
