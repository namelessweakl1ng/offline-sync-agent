package network

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"
)

func GetAuthToken() string {
	return os.Getenv("AUTH_TOKEN")
}

func IsOnline() (bool, time.Duration, string) {

	url := os.Getenv("SERVER_URL")

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	start := time.Now()

	req, _ := http.NewRequest("GET", url+"/pull", nil)
	req.Header.Set("Authorization", "Bearer "+GetAuthToken())

	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		println("NETWORK ERROR:", err.Error())
		return false, latency, "offline"
	}

	defer resp.Body.Close()

	// 🔥 IMPORTANT FIX
	if resp.StatusCode != 200 {
		return false, latency, "offline"
	}

	if latency < 500*time.Millisecond {
		return true, latency, "fast"
	} else if latency < 2*time.Second {
		return true, latency, "medium"
	}
	return true, latency, "slow"
}
