package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// ── Phone Node Registration ───────────────────────────────────────────────────

// handlePhoneNodeRegister handles POST /api/nodes for phone node registration.
func (h *apiHandlers) handlePhoneNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		InstanceID   string         `json:"instance_id"`
		NodeType     string         `json:"node_type"`
		Mode         string         `json:"mode"`
		Capabilities []string       `json:"capabilities"`
		Metadata     map[string]any `json:"metadata"`
		PushToken    string         `json:"push_token,omitempty"`
		Sandbox      bool           `json:"sandbox,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.InstanceID == "" {
		jsonError(w, http.StatusBadRequest, "instance_id is required")
		return
	}

	ctx := r.Context()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	props := map[string]any{
		"instance_id":    req.InstanceID,
		"node_type":      "phone",
		"mode":           "passive",
		"capabilities":   req.Capabilities,
		"status":         "online",
		"last_heartbeat": time.Now().UTC().Format(time.RFC3339),
	}
	if req.PushToken != "" {
		props["push_token"] = req.PushToken
		props["sandbox"] = req.Sandbox
	}
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			props["meta_"+k] = v
		}
	}

	key := "phone-" + req.InstanceID
	result, err := bridge.Client().Graph.UpsertObject(ctx, &graph.CreateObjectRequest{
		Type:       "PhoneNode",
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		log.Printf("[PUSH] phone node register failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to register node: "+err.Error())
		return
	}

	log.Printf("[PUSH] phone node registered: %s (graph entity: %s)", req.InstanceID, result.EntityID)
	writeJSON(w, map[string]any{
		"success": true,
		"node_id": req.InstanceID,
		"status":  "registered",
	})
}

// handleNodeHeartbeat handles PUT /api/nodes/{instanceID}/heartbeat.
func (h *apiHandlers) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodPut {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	props := map[string]any{
		"last_heartbeat": now,
		"status":         "online",
	}

	var body struct {
		Status string `json:"status,omitempty"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&body); decErr == nil && body.Status != "" {
		props["status"] = body.Status
	}

	key := "phone-" + instanceID
	if _, upsErr := bridge.Client().Graph.UpsertObject(ctx, &graph.CreateObjectRequest{
		Type:       "PhoneNode",
		Key:        &key,
		Properties: props,
	}); upsErr != nil {
		log.Printf("[PUSH] heartbeat failed for %s: %v", instanceID, upsErr)
		jsonError(w, http.StatusInternalServerError, "heartbeat failed: "+upsErr.Error())
		return
	}

	log.Printf("[PUSH] heartbeat: %s → %s", instanceID, props["status"])
	writeJSON(w, map[string]any{
		"success": true,
		"time":    now,
	})
}

// handleNodeDeregister handles DELETE /api/nodes/{instanceID}.
func (h *apiHandlers) handleNodeDeregister(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodDelete {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	// Find phone nodes with this instance_id
	list, searchErr := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: "PhoneNode",
		PropertyFilters: []graph.PropertyFilter{
			{Path: "instance_id", Op: "eq", Value: instanceID},
		},
	})
	if searchErr != nil {
		log.Printf("[PUSH] deregister lookup failed for %s: %v", instanceID, searchErr)
		jsonError(w, http.StatusInternalServerError, "lookup failed: "+searchErr.Error())
		return
	}

	if len(list.Items) == 0 {
		jsonError(w, http.StatusNotFound, "node not found")
		return
	}

	for _, obj := range list.Items {
		if delErr := bridge.Client().Graph.DeleteObject(ctx, obj.EntityID, nil); delErr != nil {
			log.Printf("[PUSH] deregister delete failed for %s (%s): %v", instanceID, obj.EntityID, delErr)
		}
	}

	log.Printf("[PUSH] phone node deregistered: %s", instanceID)
	writeJSON(w, map[string]any{
		"success": true,
		"message": "node deregistered",
	})
}

// handleNodeUpdate handles PUT /api/nodes/{instanceID} for push token updates.
func (h *apiHandlers) handleNodeUpdate(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodPut {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		PushToken string         `json:"push_token,omitempty"`
		Sandbox   bool           `json:"sandbox,omitempty"`
		Metadata  map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	ctx := r.Context()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	props := map[string]any{
		"last_heartbeat": time.Now().UTC().Format(time.RFC3339),
	}
	if req.PushToken != "" {
		props["push_token"] = req.PushToken
		props["sandbox"] = req.Sandbox
	}
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			props["meta_"+k] = v
		}
	}

	key := "phone-" + instanceID
	if _, upsErr := bridge.Client().Graph.UpsertObject(ctx, &graph.CreateObjectRequest{
		Type:       "PhoneNode",
		Key:        &key,
		Properties: props,
	}); upsErr != nil {
		log.Printf("[PUSH] node update failed for %s: %v", instanceID, upsErr)
		jsonError(w, http.StatusInternalServerError, "update failed: "+upsErr.Error())
		return
	}

	log.Printf("[PUSH] node updated: %s (push_token: %s)", instanceID, maskToken(req.PushToken))
	writeJSON(w, map[string]any{"success": true})
}

// ── Push Send ─────────────────────────────────────────────────────────────────

// handlePushSend handles POST /api/push/send.
func (h *apiHandlers) handlePushSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Title     string `json:"title,omitempty"`
		Body      string `json:"body"`
		SessionID string `json:"session_id,omitempty"`
		AgentName string `json:"agent_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Body == "" {
		jsonError(w, http.StatusBadRequest, "body is required")
		return
	}

	ctx := r.Context()
	bridge, err := h.bridge(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "bridge: "+err.Error())
		return
	}
	defer bridge.Close()

	// Find all registered phone nodes with push tokens
	list, listErr := bridge.Client().Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		Type: "PhoneNode",
	})
	if listErr != nil {
		log.Printf("[PUSH] list phone nodes failed: %v", listErr)
		jsonError(w, http.StatusInternalServerError, "list nodes failed: "+listErr.Error())
		return
	}

	if len(list.Items) == 0 {
		writeJSON(w, map[string]any{
			"success":   true,
			"delivered": 0,
			"message":   "no phone nodes registered",
		})
		return
	}

	type pushTarget struct {
		token   string
		sandbox bool
	}
	var targets []pushTarget
	for _, obj := range list.Items {
		token, _ := obj.Properties["push_token"].(string)
		if token == "" {
			continue
		}
		sandbox, _ := obj.Properties["sandbox"].(bool)
		targets = append(targets, pushTarget{token: token, sandbox: sandbox})
	}

	if len(targets) == 0 {
		writeJSON(w, map[string]any{
			"success":   true,
			"delivered": 0,
			"message":   "no push tokens found",
		})
		return
	}

	// Build APNs payload
	payload := buildAPNsPayload(req.Title, req.Body, req.SessionID, req.AgentName)
	payloadJSON, _ := json.Marshal(payload)

	apnsKey := os.Getenv("APNS_P8_KEY")
	apnsKeyID := os.Getenv("APNS_KEY_ID")
	apnsTeamID := os.Getenv("APNS_TEAM_ID")

	delivered := 0
	failed := 0

	if apnsKey != "" && apnsKeyID != "" && apnsTeamID != "" {
		for _, t := range targets {
			if sendErr := sendAPNs(t.token, t.sandbox, payloadJSON, apnsKey, apnsKeyID, apnsTeamID); sendErr != nil {
				log.Printf("[PUSH] APNs send failed for token %s: %v", maskToken(t.token), sendErr)
				failed++
			} else {
				delivered++
				log.Printf("[PUSH] delivered to %s", maskToken(t.token))
			}
		}
	} else {
		log.Printf("[PUSH] dry-run — APNs not configured (set APNS_P8_KEY, APNS_KEY_ID, APNS_TEAM_ID)")
		log.Printf("[PUSH] would deliver to %d devices: payload=%s", len(targets), string(payloadJSON))
		delivered = len(targets)
	}

	writeJSON(w, map[string]any{
		"success":    true,
		"delivered":  delivered,
		"failed":     failed,
		"total":      len(targets),
		"apns_ready": apnsKey != "" && apnsKeyID != "" && apnsTeamID != "",
	})
}

// ── APNs HTTP/2 Client ────────────────────────────────────────────────────────

func buildAPNsPayload(title, body, sessionID, agentName string) map[string]any {
	alert := map[string]string{
		"body": truncateText(body, 150),
	}
	if title != "" {
		alert["title"] = truncateText(title, 50)
	}
	aps := map[string]any{
		"alert": alert,
		"sound": "default",
		"badge": 1,
	}
	payload := map[string]any{"aps": aps}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	if agentName != "" {
		payload["agent"] = agentName
	}
	return payload
}

func sendAPNs(token string, sandbox bool, payload []byte, p8Key, keyID, teamID string) error {
	host := "api.push.apple.com"
	if sandbox {
		host = "api.sandbox.push.apple.com"
	}
	url := fmt.Sprintf("https://%s/3/device/%s", host, token)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	req, reqErr := http.NewRequest("POST", url, bytes.NewReader(payload))
	if reqErr != nil {
		return fmt.Errorf("create request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", "com.emergent-company.diane-companion.ios")
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", "0")

	jwt, jwtErr := signAPNsJWT(p8Key, keyID, teamID)
	if jwtErr != nil {
		return fmt.Errorf("sign JWT: %w", jwtErr)
	}
	req.Header.Set("authorization", fmt.Sprintf("bearer %s", jwt))

	resp, respErr := client.Do(req)
	if respErr != nil {
		return fmt.Errorf("APNs request: %w", respErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var apnsErr struct {
			Reason string `json:"reason"`
		}
		if decErr := json.NewDecoder(resp.Body).Decode(&apnsErr); decErr == nil && apnsErr.Reason != "" {
			switch apnsErr.Reason {
			case "BadDeviceToken", "Unregistered", "DeviceTokenNotForTopic":
				return fmt.Errorf("APNs %d: %s (remove token)", resp.StatusCode, apnsErr.Reason)
			default:
				return fmt.Errorf("APNs %d: %s", resp.StatusCode, apnsErr.Reason)
			}
		}
		return fmt.Errorf("APNs %d", resp.StatusCode)
	}

	return nil
}

func signAPNsJWT(p8Key, keyID, teamID string) (string, error) {
	header := fmt.Sprintf(`{"alg":"ES256","kid":"%s"}`, keyID)
	claims := fmt.Sprintf(`{"iss":"%s","iat":%d}`, teamID, time.Now().Unix())

	encHeader := base64URLEncode([]byte(header))
	encClaims := base64URLEncode([]byte(claims))
	signingInput := encHeader + "." + encClaims

	block, _ := pem.Decode([]byte(p8Key))
	if block == nil {
		return "", fmt.Errorf("failed to parse p8 key PEM")
	}

	ecKey, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
	if parseErr != nil {
		return "", fmt.Errorf("parse private key: %w", parseErr)
	}

	privateKey, ok := ecKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("p8 key is not an EC private key")
	}

	hash := sha256.Sum256([]byte(signingInput))
	r, s, signErr := ecdsa.Sign(rand.Reader, privateKey, hash[:])
	if signErr != nil {
		return "", fmt.Errorf("ecdsa sign: %w", signErr)
	}

	sig, marshalErr := asn1.Marshal(ecdsaSignature{R: r, S: s})
	if marshalErr != nil {
		return "", fmt.Errorf("asn1 marshal: %w", marshalErr)
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

type ecdsaSignature struct {
	R, S *big.Int
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "…"
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
