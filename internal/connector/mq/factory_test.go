package mq

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/parser"
)

// TestRabbitMQConsumerDLQEndToEnd verifies the full pipeline: HCL parser →
// connector.Config → buildRabbitMQConfig → rabbitmq.Config.Consumer.DLQ.
// Guards against silent regressions where the parser accepts dlq{} but the
// factory drops it before the connector ever sees it.
func TestRabbitMQConsumerDLQEndToEnd(t *testing.T) {
	hcl := `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  url    = "amqp://guest:guest@localhost:5672/"

  consumer {
    queue    = "orders"
    prefetch = 10
    auto_ack = false
    workers  = 5

    dlq {
      enabled      = true
      max_retries  = 3
      retry_delay  = "5s"
      exchange     = "orders.dlx"
      queue        = "orders.dlq"
      routing_key  = "orders"
      retry_header = "x-retry-count"
    }
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "rabbit.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	p := parser.NewHCLParser()
	cfg, err := p.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cfg.Connectors) != 1 {
		t.Fatalf("expected 1 connector, got %d", len(cfg.Connectors))
	}

	rmqCfg := buildRabbitMQConfig(cfg.Connectors[0])

	if rmqCfg.Consumer == nil {
		t.Fatalf("Consumer config not built")
	}
	if rmqCfg.Consumer.Prefetch != 10 {
		t.Errorf("Consumer.Prefetch=%d, want 10", rmqCfg.Consumer.Prefetch)
	}
	if rmqCfg.Consumer.Concurrency != 5 {
		t.Errorf("Consumer.Concurrency=%d, want 5 (from workers alias)", rmqCfg.Consumer.Concurrency)
	}

	if rmqCfg.Consumer.DLQ == nil {
		t.Fatalf("Consumer.DLQ not built — parser may have accepted dlq{} but factory silently dropped it")
	}
	dlq := rmqCfg.Consumer.DLQ
	if !dlq.Enabled {
		t.Errorf("DLQ.Enabled=false, want true")
	}
	if dlq.MaxRetries != 3 {
		t.Errorf("DLQ.MaxRetries=%d, want 3", dlq.MaxRetries)
	}
	if dlq.RetryDelay != 5*time.Second {
		t.Errorf("DLQ.RetryDelay=%s, want 5s", dlq.RetryDelay)
	}
	if dlq.Exchange != "orders.dlx" {
		t.Errorf("DLQ.Exchange=%q, want %q", dlq.Exchange, "orders.dlx")
	}
	if dlq.Queue != "orders.dlq" {
		t.Errorf("DLQ.Queue=%q, want %q", dlq.Queue, "orders.dlq")
	}
	if dlq.RoutingKey != "orders" {
		t.Errorf("DLQ.RoutingKey=%q, want %q", dlq.RoutingKey, "orders")
	}
	if dlq.RetryHeader != "x-retry-count" {
		t.Errorf("DLQ.RetryHeader=%q, want %q", dlq.RetryHeader, "x-retry-count")
	}
}

// TestRabbitMQConsumerRetryCountShorthand verifies the existing retry_count
// shorthand still works (workaround pre-fix and equivalent post-fix).
func TestRabbitMQConsumerRetryCountShorthand(t *testing.T) {
	hcl := `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"

  consumer {
    queue       = "orders"
    retry_count = 7
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "rabbit.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	p := parser.NewHCLParser()
	cfg, err := p.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rmqCfg := buildRabbitMQConfig(cfg.Connectors[0])
	if rmqCfg.Consumer == nil || rmqCfg.Consumer.DLQ == nil {
		t.Fatalf("retry_count shorthand did not create DLQ config")
	}
	if !rmqCfg.Consumer.DLQ.Enabled {
		t.Errorf("DLQ.Enabled=false, want true")
	}
	if rmqCfg.Consumer.DLQ.MaxRetries != 7 {
		t.Errorf("DLQ.MaxRetries=%d, want 7", rmqCfg.Consumer.DLQ.MaxRetries)
	}
}

// TestRabbitMQQueueCreateIfMissing covers all four ways to set the
// create_if_missing flag introduced in v2.0.0, plus the default-false case
// that is the breaking change.
func TestRabbitMQQueueCreateIfMissing(t *testing.T) {
	cases := []struct {
		name             string
		hcl              string
		wantQueueFlag    bool
		wantExchangeFlag bool
		hasExchange      bool
	}{
		{
			name: "consumer shorthand defaults to false (v2.0.0 strict default)",
			hcl: `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  consumer { queue = "orders" }
}`,
			wantQueueFlag: false,
		},
		{
			name: "consumer shorthand with explicit create_if_missing = true",
			hcl: `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  consumer {
    queue             = "orders"
    create_if_missing = true
  }
}`,
			wantQueueFlag: true,
		},
		{
			name: "queue block with create_if_missing = true",
			hcl: `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  queue {
    name              = "orders"
    durable           = true
    create_if_missing = true
  }
  consumer { auto_ack = false }
}`,
			wantQueueFlag: true,
		},
		{
			name: "exchange block with create_if_missing = true",
			hcl: `
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  exchange {
    name              = "events"
    type              = "topic"
    create_if_missing = true
  }
  queue {
    name              = "orders"
    create_if_missing = true
  }
  consumer { auto_ack = false }
}`,
			wantQueueFlag:    true,
			wantExchangeFlag: true,
			hasExchange:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "rabbit.mycel")
			if err := os.WriteFile(tmpFile, []byte(tc.hcl), 0644); err != nil {
				t.Fatalf("write temp file: %v", err)
			}

			cfg, err := parser.NewHCLParser().ParseFile(context.Background(), tmpFile)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			rmqCfg := buildRabbitMQConfig(cfg.Connectors[0])

			if rmqCfg.Queue == nil {
				t.Fatalf("Queue config not built")
			}
			if got := rmqCfg.Queue.CreateIfMissing; got != tc.wantQueueFlag {
				t.Errorf("Queue.CreateIfMissing=%v, want %v", got, tc.wantQueueFlag)
			}

			if tc.hasExchange {
				if rmqCfg.Exchange == nil {
					t.Fatalf("Exchange config not built")
				}
				if got := rmqCfg.Exchange.CreateIfMissing; got != tc.wantExchangeFlag {
					t.Errorf("Exchange.CreateIfMissing=%v, want %v", got, tc.wantExchangeFlag)
				}
			}
		})
	}
}
