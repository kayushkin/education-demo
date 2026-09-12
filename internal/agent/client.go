// Package agent is the client for llm-bridge-server's stateless oneshot call.
//
// Every model call in this service goes through here, which means every model
// call runs on the Claude Code subscription through the harness binary. No API
// key is read, held, or sent by this process at all.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls one llm-bridge instance.
type Client struct {
	BaseURL    string
	InstanceID string
	Model      string
	HTTP       *http.Client
}

func New(baseURL, instanceID, model string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		InstanceID: instanceID,
		Model:      model,
		// Generous: a scripted-transcript call for one team runs well past any
		// default. The bridge caps it at six minutes on its own side.
		HTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

type oneShotRequest struct {
	Prompt       string          `json:"prompt"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Model        string          `json:"model,omitempty"`
	Schema       json.RawMessage `json:"schema,omitempty"`
	MaxTokens    int             `json:"max_tokens,omitempty"`
}

type oneShotResponse struct {
	Text       string          `json:"text"`
	Parsed     json.RawMessage `json:"parsed"`
	DurationMs int64           `json:"duration_ms"`
	StopReason string          `json:"stop_reason"`
	Model      string          `json:"model"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// CallJSON makes one schema-forced call and unmarshals the model's structured
// output into out.
//
// The harness may return keys the schema never asked for — measured: it adds a
// "description" field of its own. Decoding into a struct ignores them, which
// is what we want; do not switch this to a strict decoder.
func (c *Client) CallJSON(ctx context.Context, system, prompt string, schema any, maxTokens int, out any) error {
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	body, err := json.Marshal(oneShotRequest{
		Prompt:       prompt,
		SystemPrompt: system,
		Model:        c.Model,
		Schema:       schemaBytes,
		MaxTokens:    maxTokens,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/instances/%s/oneshot", c.BaseURL, c.InstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("oneshot %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read oneshot body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// The bridge passes the harness's own explanation through on failure.
		// Surface it verbatim rather than inventing a summary of it.
		return fmt.Errorf("oneshot %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var shot oneShotResponse
	if err := json.Unmarshal(raw, &shot); err != nil {
		return fmt.Errorf("decode oneshot response: %w (body: %s)", err, truncate(string(raw), 400))
	}
	if len(shot.Parsed) == 0 {
		return fmt.Errorf("oneshot returned no structured output (stop_reason=%q, text=%q)",
			shot.StopReason, truncate(shot.Text, 300))
	}
	if err := json.Unmarshal(shot.Parsed, out); err != nil {
		return fmt.Errorf("decode structured output: %w (parsed: %s)", err, truncate(string(shot.Parsed), 400))
	}
	return nil
}

// Healthy reports whether the configured instance exists and is enabled, so
// startup can say so plainly instead of the first round failing in the dark.
func (c *Client) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/instances", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach llm-bridge at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llm-bridge %s/instances returned %d", c.BaseURL, resp.StatusCode)
	}
	var instances []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&instances); err != nil {
		return fmt.Errorf("decode instances: %w", err)
	}
	for _, in := range instances {
		if in.ID == c.InstanceID {
			if !in.Enabled {
				return fmt.Errorf("llm-bridge instance %q is disabled", c.InstanceID)
			}
			return nil
		}
	}
	return fmt.Errorf("llm-bridge has no instance %q", c.InstanceID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
