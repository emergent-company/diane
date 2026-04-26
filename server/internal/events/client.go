// Package events provides an SSE client for consuming real-time
// notification events from the Memory Platform event stream.
// It dispatches events by type to registered handlers — agent_question
// is one type among many (run_completed, error, etc.).
package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event is a generic SSE event received from the Memory Platform stream.
// The Data field is a weakly-typed map; callers inspect it for the type.
type Event struct {
	Entity string                 `json:"entity"`
	ID     *string                `json:"id,omitempty"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// EventHandler is called when a parsed SSE event is received.
type EventHandler func(raw map[string]interface{})

// Client connects to the Memory Platform SSE event stream and dispatches
// all entity notifications to the registered handler. The handler is
// responsible for filtering by event type (e.g. agent_question).
type Client struct {
	serverURL string
	apiKey    string
	projectID string
	handler   EventHandler

	mu             sync.Mutex
	running        bool
	stop           chan struct{}
	stopped        chan struct{}
	reconnectDelay time.Duration
}

// NewClient creates a new SSE event client.
// The handler receives all entity.created notification events; it must
// inspect the "type" field in the Data map to filter what it cares about.
func NewClient(serverURL, apiKey, projectID string, handler EventHandler) *Client {
	return &Client{
		serverURL:      strings.TrimRight(serverURL, "/"),
		apiKey:         apiKey,
		projectID:      projectID,
		handler:        handler,
		stop:           make(chan struct{}),
		stopped:        make(chan struct{}),
		reconnectDelay: 5 * time.Second,
	}
}

// Start begins listening to the SSE stream in a background goroutine.
// It reconnects automatically on error with exponential backoff.
// Call Stop() to shut down cleanly.
func (c *Client) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	go c.run()
}

// Stop shuts down the SSE listener and waits for it to finish.
func (c *Client) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stop)
	c.mu.Unlock()

	<-c.stopped
}

// run is the main loop — connect, read events, reconnect on error.
func (c *Client) run() {
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		close(c.stopped)
	}()

	delay := c.reconnectDelay

	for {
		select {
		case <-c.stop:
			log.Println("[SSE] Listener stopped")
			return
		default:
		}

		log.Println("[SSE] Connecting to event stream...")
		err := c.connectAndRead()
		if err != nil {
			log.Printf("[SSE] Connection error: %v (reconnecting in %v)", err, delay)
		} else {
			log.Println("[SSE] Connection closed (reconnecting)")
		}

		select {
		case <-c.stop:
			log.Println("[SSE] Listener stopped")
			return
		case <-time.After(delay):
			// Exponential backoff, cap at 60s
			delay *= 2
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
		}
	}
}

// connectAndRead opens the SSE connection and reads events until disconnected.
func (c *Client) connectAndRead() error {
	url := fmt.Sprintf("%s/api/events/stream?projectId=%s", c.serverURL, c.projectID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// Use a client with no timeout for long-lived SSE
	httpClient := &http.Client{
		Timeout: 0, // no timeout for streaming
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	log.Printf("[SSE] Connected (project=%s)", c.projectID[:12])

	// Reset reconnect delay on successful connection
	c.mu.Lock()
	c.reconnectDelay = 5 * time.Second
	c.mu.Unlock()

	// Read SSE events line by line
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 65536), 65536)

	var currentEvent string
	var currentData strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		select {
		case <-c.stop:
			return nil
		default:
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" {
			// Empty line = end of event
			if currentEvent != "" && currentData.Len() > 0 {
				c.dispatch(currentEvent, currentData.String())
			}
			currentEvent = ""
			currentData.Reset()
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	return nil
}

// dispatch routes an SSE event to the handler if it's an entity.created
// event containing notification data with a recognized type.
func (c *Client) dispatch(eventType, data string) {
	if eventType != "entity.created" {
		return
	}

	var evt Event
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		log.Printf("[SSE] Failed to parse event: %v", err)
		return
	}

	// Only forward notification events to the handler
	if evt.Entity != "notification" {
		return
	}

	if c.handler != nil {
		c.handler(evt.Data)
	}
}
