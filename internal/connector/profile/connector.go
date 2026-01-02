package profile

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/transform"
)

// ConnectorFactory is a function that creates a connector from config.
type ConnectorFactory func(cfg *connector.Config) (connector.Connector, error)

// ProfiledConnector wraps multiple backend connectors and routes operations
// to the active profile based on configuration.
type ProfiledConnector struct {
	name     string
	config   *Config
	factory  ConnectorFactory
	profiles map[string]connector.Connector
	cel      *transform.CELTransformer

	mu           sync.RWMutex
	activeCache  string // Cached active profile name
	statsLock    sync.RWMutex
	requestCount map[string]int64
	errorCount   map[string]int64
	fallbackCount map[string]int64
}

// New creates a new ProfiledConnector.
func New(name string, config *Config, factory ConnectorFactory) (*ProfiledConnector, error) {
	if config.Select == "" && config.Default == "" {
		return nil, fmt.Errorf("profiled connector %s: either 'select' or 'default' must be specified", name)
	}

	cel, err := transform.NewCELTransformer()
	if err != nil {
		return nil, fmt.Errorf("creating CEL transformer: %w", err)
	}

	return &ProfiledConnector{
		name:          name,
		config:        config,
		factory:       factory,
		profiles:      make(map[string]connector.Connector),
		cel:           cel,
		requestCount:  make(map[string]int64),
		errorCount:    make(map[string]int64),
		fallbackCount: make(map[string]int64),
	}, nil
}

// Name returns the connector name.
func (p *ProfiledConnector) Name() string {
	return p.name
}

// Type returns "profiled" as the connector type.
func (p *ProfiledConnector) Type() string {
	return "profiled"
}

// Connect establishes connections to all profile backends.
func (p *ProfiledConnector) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for profileName, profileDef := range p.config.Profiles {
		conn, err := p.factory(profileDef.ConnectorConfig)
		if err != nil {
			return fmt.Errorf("creating connector for profile %s: %w", profileName, err)
		}

		if err := conn.Connect(ctx); err != nil {
			slog.Warn("failed to connect profile",
				"connector", p.name,
				"profile", profileName,
				"error", err)
			// Don't fail completely - profile might be a fallback
			continue
		}

		p.profiles[profileName] = conn
		slog.Debug("connected profile",
			"connector", p.name,
			"profile", profileName,
			"type", profileDef.ConnectorConfig.Type)
	}

	// Verify at least the active profile (or default) is connected
	activeProfile, err := p.resolveActiveProfile()
	if err != nil {
		return err
	}

	if _, ok := p.profiles[activeProfile]; !ok {
		return fmt.Errorf("active profile %s failed to connect", activeProfile)
	}

	return nil
}

// Close terminates all profile connections.
func (p *ProfiledConnector) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for name, conn := range p.profiles {
		if err := conn.Close(ctx); err != nil {
			slog.Warn("failed to close profile",
				"connector", p.name,
				"profile", name,
				"error", err)
			lastErr = err
		}
	}
	return lastErr
}

// Health checks the health of the active profile.
func (p *ProfiledConnector) Health(ctx context.Context) error {
	conn, _, err := p.getActiveConnector()
	if err != nil {
		return err
	}
	return conn.Health(ctx)
}

// Read performs a read operation using the active profile with fallback support.
func (p *ProfiledConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	return p.executeWithFallback(ctx, func(conn connector.Connector, profileName string) (*connector.Result, error) {
		reader, ok := conn.(connector.Reader)
		if !ok {
			return nil, fmt.Errorf("profile %s does not support read operations", profileName)
		}

		result, err := reader.Read(ctx, query)
		if err != nil {
			return nil, err
		}

		// Apply profile transform
		return p.applyTransform(result, profileName)
	})
}

// Write performs a write operation using the active profile with fallback support.
func (p *ProfiledConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	return p.executeWithFallback(ctx, func(conn connector.Connector, profileName string) (*connector.Result, error) {
		writer, ok := conn.(connector.Writer)
		if !ok {
			return nil, fmt.Errorf("profile %s does not support write operations", profileName)
		}

		// TODO: Apply reverse transform for writes if needed
		return writer.Write(ctx, data)
	})
}

// Call performs a generic operation (for HTTP/GraphQL connectors).
func (p *ProfiledConnector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	var result interface{}
	_, err := p.executeWithFallback(ctx, func(conn connector.Connector, profileName string) (*connector.Result, error) {
		// Check if connector implements a Call method via type assertion
		type caller interface {
			Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error)
		}

		if c, ok := conn.(caller); ok {
			var callErr error
			result, callErr = c.Call(ctx, operation, params)
			if callErr != nil {
				return nil, callErr
			}

			// Apply transform to the result
			if mapResult, ok := result.(map[string]interface{}); ok {
				transformed, err := p.applyTransformToMap(mapResult, profileName)
				if err != nil {
					return nil, err
				}
				result = transformed
			}

			return &connector.Result{}, nil
		}

		return nil, fmt.Errorf("profile %s does not support Call operations", profileName)
	})

	return result, err
}

// executeWithFallback executes an operation with fallback support.
func (p *ProfiledConnector) executeWithFallback(ctx context.Context, fn func(conn connector.Connector, profileName string) (*connector.Result, error)) (*connector.Result, error) {
	profiles := p.getProfileOrder()

	var lastErr error
	for i, profileName := range profiles {
		p.mu.RLock()
		conn, ok := p.profiles[profileName]
		p.mu.RUnlock()

		if !ok {
			slog.Debug("profile not connected, skipping",
				"connector", p.name,
				"profile", profileName)
			continue
		}

		p.incrementRequestCount(profileName)

		result, err := fn(conn, profileName)
		if err == nil {
			return result, nil
		}

		// Record error
		p.incrementErrorCount(profileName)
		lastErr = err

		// Check if error is retriable (connection errors, timeouts, 5xx)
		if !isRetriableError(err) {
			return nil, err
		}

		// Record fallback
		if i < len(profiles)-1 {
			nextProfile := profiles[i+1]
			p.incrementFallbackCount(profileName + "->" + nextProfile)
			slog.Warn("profile failed, trying fallback",
				"connector", p.name,
				"from", profileName,
				"to", nextProfile,
				"error", err)
		}
	}

	return nil, fmt.Errorf("all profiles failed for connector %s: %w", p.name, lastErr)
}

// getProfileOrder returns the ordered list of profiles to try.
func (p *ProfiledConnector) getProfileOrder() []string {
	activeProfile, _ := p.resolveActiveProfile()

	// Start with active profile
	order := []string{activeProfile}

	// Add fallback profiles (excluding active)
	for _, fb := range p.config.Fallback {
		if fb != activeProfile {
			order = append(order, fb)
		}
	}

	return order
}

// resolveActiveProfile determines which profile should be active.
func (p *ProfiledConnector) resolveActiveProfile() (string, error) {
	// If Select is specified, evaluate it
	if p.config.Select != "" {
		result, err := p.cel.EvaluateExpression(context.Background(), nil, nil, p.config.Select)
		if err != nil {
			slog.Warn("failed to evaluate profile select expression",
				"connector", p.name,
				"expression", p.config.Select,
				"error", err)
		} else if result != nil && result != "" {
			if profileName, ok := result.(string); ok && profileName != "" {
				// Verify profile exists
				if _, exists := p.config.Profiles[profileName]; exists {
					return profileName, nil
				}
				slog.Warn("selected profile not found",
					"connector", p.name,
					"profile", profileName)
			}
		}
	}

	// Fall back to default
	if p.config.Default != "" {
		if _, exists := p.config.Profiles[p.config.Default]; exists {
			return p.config.Default, nil
		}
		return "", fmt.Errorf("default profile %s not found in connector %s", p.config.Default, p.name)
	}

	return "", fmt.Errorf("no active profile could be resolved for connector %s", p.name)
}

// getActiveConnector returns the currently active connector.
func (p *ProfiledConnector) getActiveConnector() (connector.Connector, string, error) {
	profileName, err := p.resolveActiveProfile()
	if err != nil {
		return nil, "", err
	}

	p.mu.RLock()
	conn, ok := p.profiles[profileName]
	p.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("profile %s not connected", profileName)
	}

	return conn, profileName, nil
}

// applyTransform applies the profile's transform to the result.
func (p *ProfiledConnector) applyTransform(result *connector.Result, profileName string) (*connector.Result, error) {
	profileDef, ok := p.config.Profiles[profileName]
	if !ok || len(profileDef.Transform) == 0 {
		return result, nil
	}

	// Transform each row
	transformedRows := make([]map[string]interface{}, 0, len(result.Rows))
	for _, row := range result.Rows {
		transformed, err := p.applyTransformToMap(row, profileName)
		if err != nil {
			return nil, fmt.Errorf("transform error: %w", err)
		}
		transformedRows = append(transformedRows, transformed)
	}

	return &connector.Result{
		Rows:     transformedRows,
		Affected: result.Affected,
		LastID:   result.LastID,
		Metadata: result.Metadata,
	}, nil
}

// applyTransformToMap applies the profile's transform to a single map.
func (p *ProfiledConnector) applyTransformToMap(data map[string]interface{}, profileName string) (map[string]interface{}, error) {
	profileDef, ok := p.config.Profiles[profileName]
	if !ok || len(profileDef.Transform) == 0 {
		return data, nil
	}

	// CEL transformer expects data directly (it wraps it as "input" internally)
	transformed := make(map[string]interface{})
	for outputField, expr := range profileDef.Transform {
		result, err := p.cel.EvaluateExpression(context.Background(), data, nil, expr)
		if err != nil {
			return nil, fmt.Errorf("transform field %s: %w", outputField, err)
		}
		transformed[outputField] = result
	}

	return transformed, nil
}

// isRetriableError determines if an error should trigger fallback.
func isRetriableError(err error) bool {
	// TODO: Implement proper error classification
	// For now, consider all errors as retriable
	// In production, check for:
	// - Connection errors
	// - Timeouts
	// - 5xx HTTP errors
	// NOT retriable:
	// - 4xx HTTP errors (client errors)
	// - Validation errors
	return true
}

// Stats returns profile statistics.
func (p *ProfiledConnector) Stats() map[string]interface{} {
	p.statsLock.RLock()
	defer p.statsLock.RUnlock()

	activeProfile, _ := p.resolveActiveProfile()

	stats := map[string]interface{}{
		"active_profile": activeProfile,
		"profiles":       p.config.ProfileNames(),
		"fallback":       p.config.Fallback,
		"requests":       copyMap(p.requestCount),
		"errors":         copyMap(p.errorCount),
		"fallbacks":      copyMap(p.fallbackCount),
	}

	return stats
}

// incrementRequestCount increments the request counter for a profile.
func (p *ProfiledConnector) incrementRequestCount(profileName string) {
	p.statsLock.Lock()
	p.requestCount[profileName]++
	p.statsLock.Unlock()
}

// incrementErrorCount increments the error counter for a profile.
func (p *ProfiledConnector) incrementErrorCount(profileName string) {
	p.statsLock.Lock()
	p.errorCount[profileName]++
	p.statsLock.Unlock()
}

// incrementFallbackCount increments the fallback counter.
func (p *ProfiledConnector) incrementFallbackCount(transition string) {
	p.statsLock.Lock()
	p.fallbackCount[transition]++
	p.statsLock.Unlock()
}

func copyMap(m map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
