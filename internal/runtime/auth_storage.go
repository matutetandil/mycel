package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/v2/internal/auth"
	"github.com/matutetandil/mycel/v2/internal/connector"
)

// buildAuthStores turns the auth storage configuration into the stores the
// manager should use.
//
// Nothing read that configuration before 2.19.0. The manager falls back to
// memory for every store it is not given, so a service with an auth block kept
// its users, sessions and tokens in the process: they were gone on the next
// restart, and a `storage` block naming a database or Redis changed nothing.
//
// The backends are not symmetric, and this reports which are persistent rather
// than leaving it to be discovered. Redis has no user store — accounts belong
// in a database — and the PostgreSQL side has no session or token store, so
// those stay in memory unless Redis or MySQL is configured for them.
func (r *Runtime) buildAuthStores(cfg *auth.Config) ([]auth.ManagerOption, error) {
	if cfg == nil || cfg.Storage == nil {
		return nil, nil
	}

	var (
		opts       []auth.ManagerOption
		persistent []string
	)

	switch cfg.Storage.Driver {
	case "", "memory":
		return nil, nil

	case "redis":
		client := auth.NewRedisClient(cfg.Storage.Address, cfg.Storage.Password, cfg.Storage.DB)
		opts = append(opts,
			auth.WithSessionStore(auth.NewRedisSessionStore(client, "mycel:auth:session")),
			auth.WithTokenStore(auth.NewRedisTokenStore(client, "mycel:auth:token")),
			auth.WithBruteForceStore(auth.NewRedisBruteForceStore(client, "mycel:auth:bf")),
		)
		persistent = append(persistent, "sessions", "tokens", "brute-force counters")

	case "database":
		db, driver, err := r.authDatabase(cfg.Storage.Connector)
		if err != nil {
			return nil, err
		}

		users := cfg.Users
		if users == nil {
			users = &auth.UsersConfig{}
		}

		switch driver {
		case "postgres", "postgresql":
			opts = append(opts, auth.WithUserStore(auth.NewPostgresUserStore(db, users)))
			persistent = append(persistent, "users")
		case "mysql", "mariadb":
			opts = append(opts,
				auth.WithUserStore(auth.NewMySQLUserStore(db, users)),
				auth.WithSessionStore(auth.NewMySQLSessionStore(db, "auth_sessions")),
				auth.WithTokenStore(auth.NewMySQLTokenStore(db, "auth_tokens")),
			)
			persistent = append(persistent, "users", "sessions", "tokens")
		default:
			return nil, fmt.Errorf("auth storage connector %q is a %s database, and auth stores exist for postgres and mysql only",
				cfg.Storage.Connector, driver)
		}

	default:
		return nil, fmt.Errorf("auth storage driver %q is not one of memory, redis or database", cfg.Storage.Driver)
	}

	slog.Info("auth storage configured", "driver", cfg.Storage.Driver, "persistent", persistent)

	// The one that matters enough to say on its own: without a user store,
	// every account registered since the last restart disappears with the
	// process, which is not what someone configuring storage is expecting.
	if !containsString(persistent, "users") {
		slog.Warn("auth users are still held in memory and will not survive a restart",
			"driver", cfg.Storage.Driver,
			"hint", "a database storage driver on postgres or mysql persists accounts")
	}

	return opts, nil
}

// authDatabase resolves the connector the auth storage names into a database
// handle and the driver behind it.
func (r *Runtime) authDatabase(name string) (db *sql.DB, driver string, err error) {
	if name == "" {
		return nil, "", fmt.Errorf("auth storage driver is \"database\" but no connector was named")
	}

	conn, err := r.connectors.Get(name)
	if err != nil {
		return nil, "", fmt.Errorf("auth storage connector %q not found: %w", name, err)
	}

	accessor, ok := conn.(connector.DBAccessor)
	if !ok {
		return nil, "", fmt.Errorf("auth storage connector %q does not provide database access", name)
	}
	handle := accessor.DB()
	if handle == nil {
		return nil, "", fmt.Errorf("auth storage connector %q returned no database handle", name)
	}

	for _, c := range r.config.Connectors {
		if c.Name == name {
			return handle, c.Driver, nil
		}
	}
	return nil, "", fmt.Errorf("auth storage connector %q has no configuration", name)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// initAuth builds the auth manager and handler.
//
// It runs in Start rather than in New because the storage configuration may
// name a connector, and connectors do not exist until Start has registered
// them.
func (r *Runtime) initAuth(ctx context.Context) error {
	if r.config.Auth == nil {
		return nil
	}

	stores, err := r.buildAuthStores(r.config.Auth)
	if err != nil {
		return err
	}

	opts := append([]auth.ManagerOption{auth.WithLogger(r.logger)}, stores...)
	manager, err := auth.NewManager(r.config.Auth, opts...)
	if err != nil {
		return fmt.Errorf("failed to create auth manager: %w", err)
	}

	r.authManager = manager
	r.authHandler = auth.NewHandler(manager)

	// Expired sessions and tokens are removed on a timer. Nothing started this
	// loop before, and nothing else removes them: every session and every token
	// a service ever issued stayed where it was written, growing without bound
	// for as long as the process ran.
	r.authCleanup = auth.NewCleanupService(manager, 0)
	r.authCleanup.SetLogger(r.logger)
	if err := r.authCleanup.Start(ctx); err != nil {
		return fmt.Errorf("failed to start auth cleanup: %w", err)
	}

	if sso := manager.SSO(); sso != nil {
		// A provider redirects a browser back to an absolute address it has on
		// record. Starting without one produces a relative redirect_uri that
		// every provider rejects, at the point of sign-in and with an error
		// that names none of this.
		if r.config.Auth.BaseURL == "" {
			return fmt.Errorf("auth declares identity providers but no base_url: " +
				"add base_url to the auth block with the address this service is reached at " +
				"(for example https://app.example.com), since that is where a provider sends users back to")
		}

		// OIDC providers describe themselves at a discovery URL, which has to
		// be fetched before the first sign-in rather than during it. A provider
		// that cannot be reached is reported and the rest still work.
		if err := sso.InitializeOIDCProviders(ctx); err != nil {
			r.logger.Warn("an OIDC provider could not be initialized", "error", err)
		}
		// Pending authorisation states expire; without this they accumulate
		// for the life of the process.
		sso.StartStateCleanup(ctx)
		r.logger.Info("single sign-on enabled", "providers", sso.GetAvailableProviders())
	}

	r.logger.Info("auth system initialized", "preset", r.config.Auth.Preset)
	return nil
}
