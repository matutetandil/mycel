package postgres

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

func TestFactoryAcceptsAURL(t *testing.T) {
	conn, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name: "db", Type: "database",
		Properties: map[string]interface{}{
			"driver": "postgres",
			"url":    "postgres://alice:pw@db.example.com:5433/app?sslmode=require",
		},
	})
	if err != nil {
		t.Fatalf("a url-only connector was rejected: %v", err)
	}
	if conn == nil {
		t.Fatal("no connector returned")
	}
}

func TestFactoryStillRequiresSomething(t *testing.T) {
	_, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name: "db", Type: "database",
		Properties: map[string]interface{}{"driver": "postgres"},
	})
	if err == nil {
		t.Error("a connector with neither url nor fields was accepted")
	}
}
