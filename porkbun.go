package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PorkbunDNSManager struct {
	apiKey    string
	secretKey string
	domain    string
	subdomain string
}

type porkbunRequest struct {
	SecretAPIKey string `json:"secretapikey"`
	APIKey       string `json:"apikey"`
	Content      string `json:"content"`
}

type porkbunResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func NewPorkbunDNSManager(apiKey, secretKey, fullDomain string) *PorkbunDNSManager {
	// Parse domain and subdomain from full domain
	// e.g., "mc.example.com" -> domain: "example.com", subdomain: "mc"
	// e.g., "example.com" -> domain: "example.com", subdomain: ""
	parts := strings.Split(fullDomain, ".")

	var domain, subdomain string
	if len(parts) > 2 {
		// Has subdomain
		subdomain = parts[0]
		domain = strings.Join(parts[1:], ".")
	} else {
		// No subdomain, just domain
		domain = fullDomain
		subdomain = ""
	}

	return &PorkbunDNSManager{
		apiKey:    apiKey,
		secretKey: secretKey,
		domain:    domain,
		subdomain: subdomain,
	}
}

func (p *PorkbunDNSManager) UpdateDNS(ipv4, ipv6 string) error {
	if err := p.updateRecord("A", ipv4); err != nil {
		return fmt.Errorf("error updating A record: %w", err)
	}

	if ipv6 != "" {
		if err := p.updateRecord("AAAA", ipv6); err != nil {
			return fmt.Errorf("error updating AAAA record: %w", err)
		}
	}

	return nil
}

func (p *PorkbunDNSManager) updateRecord(recordType, content string) error {
	// Build URL based on whether subdomain exists
	var url string
	if p.subdomain != "" {
		url = fmt.Sprintf("https://api.porkbun.com/api/json/v3/dns/editByNameType/%s/%s/%s", p.domain, recordType, p.subdomain)
	} else {
		url = fmt.Sprintf("https://api.porkbun.com/api/json/v3/dns/editByNameType/%s/%s", p.domain, recordType)
	}

	reqBody := porkbunRequest{
		SecretAPIKey: p.secretKey,
		APIKey:       p.apiKey,
		Content:      content,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result porkbunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.Status != "SUCCESS" {
		return fmt.Errorf("porkbun API error: %s", result.Message)
	}

	return nil
}
