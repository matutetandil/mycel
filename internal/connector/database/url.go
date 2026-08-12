package database

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// URLFields are the parts of a connection URL, in the shape the factories read
// them from a connector's properties.
type URLFields struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	// Options carries the query string, so driver-specific settings such as
	// sslmode or charset survive the decomposition.
	Options map[string]string
}

// ParseURL decomposes a database connection URL into discrete fields.
//
// Discrete fields are the primary way to configure a database connector: each
// one is validated on its own, and the password can come from a secret while
// the host comes from a configmap. A URL exists for the case where the
// environment hands one over whole — every managed platform exposes a single
// DATABASE_URL — because HCL cannot take a string apart, so without this the
// only way to use one is a wrapper script.
//
//	postgres://user:pass@host:5432/dbname?sslmode=require
//	mysql://user:pass@host:3306/dbname
func ParseURL(raw string) (*URLFields, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("empty database url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid database url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("database url must include a scheme and a host, e.g. postgres://user:pass@host:5432/db")
	}

	fields := &URLFields{
		Host:    u.Hostname(),
		Options: map[string]string{},
	}

	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q in database url", p)
		}
		fields.Port = n
	}

	// The path is "/dbname"; an empty one is legal in a URL but useless here,
	// and the factories already report a missing database themselves.
	fields.Database = strings.TrimPrefix(u.Path, "/")

	if u.User != nil {
		fields.User = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			fields.Password = pass
		}
	}

	for k, vs := range u.Query() {
		if len(vs) > 0 {
			fields.Options[k] = vs[0]
		}
	}

	return fields, nil
}

// ApplyURL fills properties from a connection URL found under "url", leaving
// anything the author set explicitly untouched.
//
// Explicit wins on purpose: writing both a url and a `database` is how you
// point one connection string at a different database on the same server, and
// on the reading of an accident, the hand-written value is the one that was
// meant.
func ApplyURL(props map[string]interface{}) error {
	raw, ok := props["url"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}

	fields, err := ParseURL(raw)
	if err != nil {
		return err
	}

	setIfAbsent := func(key string, value interface{}) {
		if value == nil || value == "" || value == 0 {
			return
		}
		if existing, present := props[key]; present {
			if s, isStr := existing.(string); !isStr || s != "" {
				return
			}
		}
		props[key] = value
	}

	setIfAbsent("host", fields.Host)
	setIfAbsent("port", fields.Port)
	setIfAbsent("database", fields.Database)
	setIfAbsent("user", fields.User)
	setIfAbsent("password", fields.Password)
	for k, v := range fields.Options {
		setIfAbsent(k, v)
	}

	return nil
}
