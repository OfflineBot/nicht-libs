// Package vastai manages on-demand GPU instances on vast.ai for embedding workloads.
//
// Environment variables:
//
//	VASTAI_API_KEY       — vast.ai API key (required for auto-GPU)
//	VASTAI_MAX_PRICE_HR  — max price in USD/hr (default: 0.10)
//	VASTAI_MIN_DISK_GB   — minimum disk space in GB (default: 30)
package vastai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const apiBase = "https://console.vast.ai/api/v0"

// Offer represents a rentable GPU offer on vast.ai.
type Offer struct {
	ID           int64   `json:"id"`
	GPUName      string  `json:"gpu_name"`
	GPURAM       float64 `json:"gpu_ram"`
	DiskSpace    float64 `json:"disk_space"`
	DphTotal     float64 `json:"dph_total"`
	Reliability2 float64 `json:"reliability2"`
	NumGPUs      int     `json:"num_gpus"`
}

// Instance is a running vast.ai instance.
type Instance struct {
	ID           int64             `json:"id"`
	Status       string            `json:"actual_status"`
	PublicIP     string            `json:"public_ipaddr"`
	Ports        map[string][]Port `json:"ports"`
	DphTotal     float64           `json:"dph_total"`
	GPUName      string            `json:"gpu_name"`
	ImageUUID    string            `json:"image_uuid"`
	SSHHost      string            `json:"ssh_host"`
	SSHPort      int               `json:"ssh_port"`
}

// Port is a port mapping entry in the instance response.
type Port struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func apiKey() string { return os.Getenv("VASTAI_API_KEY") }

// HasAPIKey reports whether a vast.ai API key is configured.
func HasAPIKey() bool { return apiKey() != "" }

func maxPriceHr() float64 {
	if v := os.Getenv("VASTAI_MAX_PRICE_HR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0.06
}

func minDiskGB() float64 {
	if v := os.Getenv("VASTAI_MIN_DISK_GB"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 30
}

func doRequest(method, path string, body any) ([]byte, error) {
	key := apiKey()
	if key == "" {
		return nil, fmt.Errorf("VASTAI_API_KEY not set")
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("vast.ai API %s %s → %d: %s", method, path, resp.StatusCode, data)
	}
	return data, nil
}

// FindCheapGPU returns the cheapest reliable GPU offer under the price limit.
func FindCheapGPU() (*Offer, error) {
	offers, err := findCheapGPUList()
	if err != nil {
		return nil, err
	}
	return &offers[0], nil
}

// RentInstance creates a new instance from an offer and returns its ID.
// Ollama listens on port 8080 (forwarded via vast.ai SSH relay at ssh_host:ssh_port+1).
func RentInstance(offerID int64, ollamaModel string) (int64, error) {
	// onstart: start Ollama server first, wait for it, then pull the model.
	onstart := "OLLAMA_HOST=0.0.0.0:8080 nohup ollama serve >/var/log/ollama.log 2>&1 & " +
		"sleep 10 && OLLAMA_HOST=localhost:8080 ollama pull " + ollamaModel
	body := map[string]any{
		"client_id": "me",
		"image":     "ollama/ollama",
		"disk":      int(minDiskGB()),
		"onstart":   onstart,
		"env":       map[string]string{"OLLAMA_HOST": "0.0.0.0:8080"},
		"runtype":   "ssh",
	}

	data, err := doRequest("PUT", fmt.Sprintf("/asks/%d/", offerID), body)
	if err != nil {
		return 0, fmt.Errorf("rent instance: %w", err)
	}

	var resp struct {
		Success     bool  `json:"success"`
		NewContract int64 `json:"new_contract"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || !resp.Success {
		return 0, fmt.Errorf("rent instance failed: %s", data)
	}
	return resp.NewContract, nil
}

// GetInstance returns the current state of an instance.
func GetInstance(instanceID int64) (*Instance, error) {
	data, err := doRequest("GET", "/instances/", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	for _, inst := range resp.Instances {
		if inst.ID == instanceID {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance %d not found", instanceID)
}

// GetInstances returns all running instances tagged as embedding instances.
func GetInstances() ([]Instance, error) {
	data, err := doRequest("GET", "/instances/", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

// DestroyInstance stops and destroys a running instance.
func DestroyInstance(instanceID int64) error {
	_, err := doRequest("DELETE", fmt.Sprintf("/instances/%d/", instanceID), nil)
	return err
}

// OllamaURL returns the base URL for the Ollama API on the instance.
// vast.ai forwards ssh_port+1 on the SSH relay host to port 8080 inside the container,
// so Ollama (listening on :8080) is always reachable at http://{ssh_host}:{ssh_port+1}.
func OllamaURL(inst *Instance) (string, error) {
	if inst.SSHHost != "" && inst.SSHPort > 0 {
		return fmt.Sprintf("http://%s:%d", inst.SSHHost, inst.SSHPort+1), nil
	}
	// Fallback: try Docker-mapped port 8080/tcp on the public IP
	if inst.PublicIP != "" {
		for _, key := range []string{"8080/tcp", "8080"} {
			if mappings, ok := inst.Ports[key]; ok && len(mappings) > 0 {
				return fmt.Sprintf("http://%s:%s", inst.PublicIP, mappings[0].HostPort), nil
			}
		}
	}
	return "", fmt.Errorf("instance has no SSH relay info yet (ssh_host=%q ssh_port=%d)", inst.SSHHost, inst.SSHPort)
}

// loadingTimeout is how long we wait in "loading" state before giving up.
// Instances stuck loading usually indicate the host rejected the job or lost power.
const loadingTimeout = 3 * time.Minute

// WaitForOllama waits for the Ollama API to become available on the instance,
// polling every 10s until timeout. Returns the base URL.
func WaitForOllama(instanceID int64, model string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	var loadingStart time.Time
	notFoundStreak := 0
	for time.Now().Before(deadline) {
		inst, err := GetInstance(instanceID)
		if err != nil {
			slog.Warn("vastai: instance poll failed", "id", instanceID, "err", err)
			notFoundStreak++
			if notFoundStreak >= 3 {
				return "", fmt.Errorf("instance %d disappeared (not found 3x in a row)", instanceID)
			}
			time.Sleep(10 * time.Second)
			continue
		}
		notFoundStreak = 0

		if inst.Status == "exited" || inst.Status == "deleted" {
			return "", fmt.Errorf("instance %d exited unexpectedly", instanceID)
		}

		if inst.Status != "running" {
			// vast.ai sometimes returns null/empty actual_status even for running instances.
			// If SSH relay info is present, try to reach Ollama directly — the container may
			// already be up even though the API hasn't updated the status field yet.
			if (inst.Status == "" || inst.Status == "loading") && inst.SSHHost != "" && inst.SSHPort > 0 {
				baseURL, urlErr := OllamaURL(inst)
				if urlErr == nil {
					probeResp, probeErr := client.Get(baseURL + "/api/tags")
					if probeErr == nil && probeResp.StatusCode == 200 {
						probeResp.Body.Close()
						slog.Info("vastai: instance reachable despite null status, continuing",
							"id", instanceID, "url", baseURL, "reported_status", inst.Status)
						// Fall through to the Ollama-ready check below.
						goto ollamaCheck
					}
					if probeResp != nil {
						probeResp.Body.Close()
					}
				}
			}

			// Track how long we've been stuck in a pre-running state (loading or empty).
			// Hosts that never progress past "" or "loading" are stalled/dead.
			if inst.Status == "loading" || inst.Status == "" {
				if loadingStart.IsZero() {
					loadingStart = time.Now()
				} else if time.Since(loadingStart) > loadingTimeout {
					return "", fmt.Errorf("instance %d stuck in status %q for >%s, aborting", instanceID, inst.Status, loadingTimeout)
				}
			} else {
				loadingStart = time.Time{} // reset if status changed to something unexpected
			}
			slog.Info("vastai: waiting for instance", "id", instanceID, "status", inst.Status)
			time.Sleep(10 * time.Second)
			continue
		}
		loadingStart = time.Time{}
	ollamaCheck:

		baseURL, err := OllamaURL(inst)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		// Try to reach Ollama tags endpoint
		resp, err := client.Get(baseURL + "/api/tags")
		if err != nil || resp.StatusCode != 200 {
			slog.Info("vastai: Ollama not ready yet", "url", baseURL)
			time.Sleep(10 * time.Second)
			continue
		}
		resp.Body.Close()

		// Check if model is already pulled
		if isModelAvailable(client, baseURL, model) {
			slog.Info("vastai: Ollama ready", "url", baseURL, "model", model)
			return baseURL, nil
		}
		slog.Info("vastai: waiting for model pull", "model", model)
		time.Sleep(15 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for Ollama on instance %d", instanceID)
}

func isModelAvailable(client *http.Client, baseURL, model string) bool {
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	for _, m := range result.Models {
		if m.Name == model || m.Name == model+":latest" {
			return true
		}
	}
	return false
}

// AcquireEmbedGPU finds or reuses a running instance and waits for Ollama to be ready.
// Returns (instanceID, ollamaURL, isNew). If an instance is already running, reuses it.
// Retries up to 3 times with different offers when an instance gets preempted.
func AcquireEmbedGPU(model string) (int64, string, bool, error) {
	if apiKey() == "" {
		return 0, "", false, fmt.Errorf("VASTAI_API_KEY not set")
	}

	// Reuse running instance if available
	instances, err := GetInstances()
	if err == nil {
		for _, inst := range instances {
			if inst.Status == "running" {
				baseURL, err := OllamaURL(&inst)
				if err == nil {
					slog.Info("vastai: reusing existing instance", "id", inst.ID, "url", baseURL)
					return inst.ID, baseURL, false, nil
				}
			}
		}
	}

	// Try up to 10 times — instances can be preempted shortly after renting.
	var lastErr error
	usedOffers := map[int64]bool{}
	for attempt := 1; attempt <= 10; attempt++ {
		offers, err := findCheapGPUList()
		if err != nil {
			return 0, "", false, fmt.Errorf("find GPU: %w", err)
		}

		// Pick the first offer we haven't tried yet
		var offer *Offer
		for i := range offers {
			if !usedOffers[offers[i].ID] {
				offer = &offers[i]
				break
			}
		}
		if offer == nil {
			break
		}
		usedOffers[offer.ID] = true

		slog.Info("vastai: renting GPU", "attempt", attempt, "offer_id", offer.ID,
			"gpu", offer.GPUName, "price_hr", offer.DphTotal)

		instanceID, err := RentInstance(offer.ID, model)
		if err != nil {
			slog.Warn("vastai: rent failed, retrying", "attempt", attempt, "err", err)
			lastErr = err
			continue
		}
		slog.Info("vastai: instance rented", "instance_id", instanceID, "attempt", attempt)

		// Wait for it to be ready (up to 10 minutes)
		url, err := WaitForOllama(instanceID, model, 10*time.Minute)
		if err != nil {
			slog.Warn("vastai: instance failed after rent, trying next offer",
				"attempt", attempt, "instance_id", instanceID, "err", err)
			_ = DestroyInstance(instanceID)
			lastErr = err
			continue
		}
		return instanceID, url, true, nil
	}

	if lastErr != nil {
		return 0, "", false, fmt.Errorf("all rent attempts failed: %w", lastErr)
	}
	return 0, "", false, fmt.Errorf("no suitable GPU offers available")
}

// findCheapGPUList returns up to 20 GPU offers sorted by price ascending.
func findCheapGPUList() ([]Offer, error) {
	q := map[string]any{
		"verified":     map[string]any{"eq": true},
		"rentable":     map[string]any{"eq": true},
		"num_gpus":     map[string]any{"eq": 1},
		"dph_total":    map[string]any{"lte": maxPriceHr()},
		"disk_space":   map[string]any{"gte": minDiskGB()},
		"reliability2": map[string]any{"gte": 0.95},
		"inet_up":      map[string]any{"gte": 100.0},
		"order":        [][]string{{"dph_total", "asc"}},
		"limit":        20,
	}
	qJSON, _ := json.Marshal(q)

	data, err := doRequest("GET", "/bundles/?q="+mustURLEncode(string(qJSON)), nil)
	if err != nil {
		return nil, fmt.Errorf("search offers: %w", err)
	}

	var resp struct {
		Offers []Offer `json:"offers"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse offers: %w", err)
	}
	if len(resp.Offers) == 0 {
		return nil, fmt.Errorf("no GPU offers found under $%.2f/hr", maxPriceHr())
	}
	return resp.Offers, nil
}

func mustURLEncode(s string) string {
	var buf bytes.Buffer
	for _, b := range []byte(s) {
		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9',
			b == '-', b == '_', b == '.', b == '~':
			buf.WriteByte(b)
		default:
			fmt.Fprintf(&buf, "%%%02X", b)
		}
	}
	return buf.String()
}
