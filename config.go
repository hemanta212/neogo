package neogo

import (
	"context"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/notifications"
)

// defaultConfig returns default configuration values from the neo4j driver.
// This configuration should be maintained and updated when the neo4j driver is updated.
func defaultConfig() *config.Config {
	return &config.Config{
		AddressResolver:                 nil,
		MaxTransactionRetryTime:         30 * time.Second,
		MaxConnectionPoolSize:           100,
		MaxConnectionLifetime:           1 * time.Hour,
		ConnectionAcquisitionTimeout:    1 * time.Minute,
		SocketConnectTimeout:            5 * time.Second,
		SocketKeepalive:                 true,
		RootCAs:                         nil,
		UserAgent:                       neo4j.UserAgent,
		FetchSize:                       neo4j.FetchDefault,
		NotificationsMinSeverity:        notifications.DefaultLevel,
		NotificationsDisabledCategories: notifications.NotificationDisabledCategories{},
	}
}

// Config extends the neo4j config with additional neogo-specific options.
type Config struct {
	config.Config

	CausalConsistencyKey func(context.Context) string
	Types                []any
	MarshalHooks         []MarshalHook
	UnmarshalHooks       []UnmarshalHook
	LocalePreferredKeys  []string
}

// Configurer is a function that configures a neogo Config.
type Configurer func(*Config)

// execConfig holds session and transaction configuration for query execution.
type execConfig struct {
	*neo4j.SessionConfig
	*neo4j.TransactionConfig
}

// causalConsistencyCache stores bookmarks for causal consistency by key.
var causalConsistencyCache map[string]neo4j.Bookmarks = map[string]neo4j.Bookmarks{}

// WithCausalConsistency configures causal consistency for the driver.
func WithCausalConsistency(when func(ctx context.Context) string) Configurer {
	return func(c *Config) {
		c.CausalConsistencyKey = when
	}
}

// WithTypes is an option for [New] that allows you to register instances of
// [IAbstract], [INode] and [IRelationship] to be used with [neogo].
func WithTypes(types ...any) Configurer {
	return func(c *Config) {
		c.Types = append(c.Types, types...)
	}
}

// WithMarshalHook registers a hook that is invoked before struct values are
// marshalled into query parameters.
func WithMarshalHook(hook MarshalHook) Configurer {
	return func(c *Config) {
		c.MarshalHooks = append(c.MarshalHooks, hook)
	}
}

// WithUnmarshalHook registers a hook that is invoked after values are
// unmarshalled into result bindings.
func WithUnmarshalHook(hook UnmarshalHook) Configurer {
	return func(c *Config) {
		c.UnmarshalHooks = append(c.UnmarshalHooks, hook)
	}
}

// WithLocales registers marshal/unmarshal hooks and locale preferred keys
// from a single LocaleSelector. This is the recommended way to configure
// locale support — everything is derived from the selector.
func WithLocales(selector LocaleSelector) Configurer {
	return func(c *Config) {
		c.MarshalHooks = append(c.MarshalHooks, LocalesHookWithSelector(selector))
		c.UnmarshalHooks = append(c.UnmarshalHooks, LocalesUnmarshalHookWithSelector(selector))
		c.LocalePreferredKeys = selector.PreferredKeys()
	}
}

// WithTxConfig configures the transaction used by Exec().
func WithTxConfig(configurers ...func(*neo4j.TransactionConfig)) func(ec *execConfig) {
	return func(ec *execConfig) {
		for _, c := range configurers {
			c(ec.TransactionConfig)
		}
	}
}

// WithSessionConfig configures the session used by Exec().
func WithSessionConfig(configurers ...func(*neo4j.SessionConfig)) func(ec *execConfig) {
	return func(ec *execConfig) {
		for _, c := range configurers {
			c(ec.SessionConfig)
		}
	}
}
