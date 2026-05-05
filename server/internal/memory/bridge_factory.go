package memory

import (
	"time"
)

// NewBridgeFromConfig creates a memory bridge from the project config provider.
// Pass a function that returns (serverURL, token, projectID, orgID) for flexibility.
// Returns nil if the config provider returns an empty ServerURL.
func NewBridgeFromConfig(cfg ConfigProvider) *Bridge {
	if cfg == nil {
		return nil
	}
	serverURL, token, projectID, orgID := cfg()
	if serverURL == "" {
		return nil
	}
	b, err := New(Config{
		ServerURL:         serverURL,
		APIKey:            token,
		ProjectID:         projectID,
		OrgID:             orgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil
	}
	return b
}

// ConfigProvider is a function that returns bridge configuration.
type ConfigProvider func() (serverURL, token, projectID, orgID string)
