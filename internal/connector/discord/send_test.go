package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Sending a message to Discord.
//
// There are two ways in — an incoming webhook, and the bot API — and they are
// not interchangeable: one posts wherever the webhook was created, the other
// posts to a channel named per message and needs a token. Which one is used is
// decided from the configuration and the flow's target, and that decision had
// no test.

type discordAPI struct {
	status int
	answer string

	path string
	auth string
	sent map[string]interface{}
}

func (a *discordAPI) serve(t *testing.T, cfg *Config) *Connector {
	t.Helper()
	if a.status == 0 {
		a.status = http.StatusOK
	}
	if a.answer == "" {
		a.answer = `{"id":"message-1","channel_id":"channel-1"}`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.path = r.URL.Path
		a.auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &a.sent)
		w.WriteHeader(a.status)
		_, _ = w.Write([]byte(a.answer))
	}))
	t.Cleanup(server.Close)

	if cfg.WebhookURL == "-" {
		cfg.WebhookURL = server.URL + "/api/webhooks/1/token"
	}
	if cfg.BotToken != "" {
		cfg.APIURL = server.URL
	}
	return NewConnector("discord", cfg)
}

func TestAMessageSentThroughAWebhook(t *testing.T) {
	api := &discordAPI{}
	c := api.serve(t, &Config{WebhookURL: "-"})

	result, err := c.Send(context.Background(), &Message{Content: "Deploy finished"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success || result.MessageID != "message-1" {
		t.Errorf("result = %+v", result)
	}
	if api.sent["content"] != "Deploy finished" {
		t.Errorf("sent %v", api.sent)
	}
}

func TestAMessageSentAsABot(t *testing.T) {
	// The channel comes from the flow's target, so one connector serves every
	// channel a service posts to.
	api := &discordAPI{}
	c := api.serve(t, &Config{BotToken: "bot-token"})

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  "channel-42",
		Payload: map[string]interface{}{"content": "Deploy finished"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}

	if api.path != "/channels/channel-42/messages" {
		t.Errorf("posted to %s, want the flow's channel", api.path)
	}
	// Discord's own scheme, and a plain bearer token is rejected.
	if api.auth != "Bot bot-token" {
		t.Errorf("authorization = %q", api.auth)
	}
}

func TestABotMessageWithNoChannelIsRefused(t *testing.T) {
	// Nothing sensible to do with it: there is no default channel on the bot
	// API, so this must be an error rather than a message posted somewhere
	// unexpected.
	api := &discordAPI{}
	c := api.serve(t, &Config{BotToken: "bot-token"})

	result, err := c.sendViaAPI(context.Background(), &Message{Content: "hi"}, "")
	if err == nil {
		t.Fatal("a message with no channel was accepted")
	}
	if result.Success {
		t.Errorf("result = %+v", result)
	}
}

func TestACardKeepsItsShapeOnTheWay(t *testing.T) {
	// The embed is the message for most uses of this connector — a deploy
	// summary, an alert — and it travels as nested JSON, so it is the part
	// most easily lost between the flow and the wire.
	api := &discordAPI{}
	c := api.serve(t, &Config{BotToken: "bot-token"})

	_, err := c.Write(context.Background(), &connector.Data{
		Target: "channel-42",
		Payload: map[string]interface{}{
			"content": "Deploy finished",
			"embeds": []interface{}{
				map[string]interface{}{
					"title":       "orders v2.19.0",
					"description": "12 flows registered",
					"color":       3066993,
					"fields": []interface{}{
						map[string]interface{}{"name": "Environment", "value": "production", "inline": true},
					},
					"image": "https://example.test/graph.png",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	embeds, ok := api.sent["embeds"].([]interface{})
	if !ok || len(embeds) != 1 {
		t.Fatalf("embeds = %v", api.sent["embeds"])
	}
	embed := embeds[0].(map[string]interface{})
	if embed["title"] != "orders v2.19.0" || embed["description"] != "12 flows registered" {
		t.Errorf("embed = %v", embed)
	}
	if embed["color"] != float64(3066993) {
		t.Errorf("colour = %v, want the number the flow set", embed["color"])
	}
	fields, ok := embed["fields"].([]interface{})
	if !ok || len(fields) != 1 {
		t.Fatalf("fields = %v", embed["fields"])
	}
	if field := fields[0].(map[string]interface{}); field["name"] != "Environment" || field["inline"] != true {
		t.Errorf("field = %v", field)
	}
	// An image given as a bare address is still an image.
	image, ok := embed["image"].(map[string]interface{})
	if !ok || image["url"] != "https://example.test/graph.png" {
		t.Errorf("image = %v", embed["image"])
	}
}

func TestAMessageDiscordRefusedIsAFailure(t *testing.T) {
	// A revoked token, a deleted channel, or the rate limit: all of them mean
	// nobody saw the message.
	for name, api := range map[string]*discordAPI{
		"through a webhook": {status: http.StatusNotFound, answer: `{"message":"Unknown Webhook","code":10015}`},
		"through the API":   {status: http.StatusUnauthorized, answer: `{"message":"401: Unauthorized"}`},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{WebhookURL: "-"}
			target := ""
			if name == "through the API" {
				cfg = &Config{BotToken: "bot-token"}
				target = "channel-42"
			}
			c := api.serve(t, cfg)

			_, err := c.Write(context.Background(), &connector.Data{
				Target:  target,
				Payload: map[string]interface{}{"content": "hi"},
			})
			if err == nil {
				t.Error("a message Discord refused was reported as sent")
			}
		})
	}
}

func TestAskingDiscordWhetherTheTokenStillWorks(t *testing.T) {
	// A bot token that was regenerated is the usual reason a service that
	// worked yesterday posts nothing today, and it says nothing until it tries.
	api := &discordAPI{answer: `{"id":"bot-1","username":"mycel"}`}
	c := api.serve(t, &Config{BotToken: "bot-token"})

	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if api.path != "/users/@me" {
		t.Errorf("checked %s", api.path)
	}

	api.status = http.StatusUnauthorized
	if err := c.Health(context.Background()); err == nil {
		t.Error("a token Discord no longer accepts was reported healthy")
	}

	// A webhook has nothing to check against, so it is not called a failure.
	webhook := (&discordAPI{}).serve(t, &Config{WebhookURL: "-"})
	if err := webhook.Health(context.Background()); err != nil {
		t.Errorf("a webhook connector reported unhealthy: %v", err)
	}
	if err := webhook.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestAServiceThatIsNotThere(t *testing.T) {
	c := NewConnector("discord", &Config{
		BotToken: "bot-token",
		APIURL:   "http://127.0.0.1:1",
		Timeout:  time.Second,
	})

	if _, err := c.sendViaAPI(context.Background(), &Message{Content: "hi"}, "channel-1"); err == nil {
		t.Error("a message was sent to a service nobody is running")
	}
	if err := c.Health(context.Background()); err == nil {
		t.Error("a service that is not there was reported healthy")
	}
}

func TestHowDiscordSettingsAreRead(t *testing.T) {
	factory := NewFactory()
	if !factory.Supports("discord", "") || factory.Supports("slack", "") {
		t.Error("the factory answers for the wrong connector type")
	}

	built, err := factory.Create(context.Background(), &connector.Config{
		Name: "alerts",
		Properties: map[string]interface{}{
			"webhook_url": "https://discord.com/api/webhooks/1/token",
			"channel_id":  "channel-1",
			"username":    "mycel",
			"timeout":     "5s",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	c := built.(*Connector)
	if c.config.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", c.config.Timeout)
	}
	if c.config.DefaultChannelID != "channel-1" || c.config.Username != "mycel" {
		t.Errorf("config = %+v", c.config)
	}
	// Nothing said still leaves an address to talk to.
	if c.config.APIURL == "" {
		t.Error("no API address, so every bot call goes nowhere")
	}

	// An unparseable timeout falls back rather than becoming no timeout at all.
	fallback, err := factory.Create(context.Background(), &connector.Config{
		Name:       "alerts",
		Properties: map[string]interface{}{"webhook_url": "x", "timeout": "whenever"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fallback.(*Connector).config.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want the default", fallback.(*Connector).config.Timeout)
	}
}

func TestValuesArriveHoweverTheTransformProducedThem(t *testing.T) {
	// A colour computed by a transform is a double; one written in the
	// configuration is an integer; one read from JSON is a double again.
	for name, value := range map[string]interface{}{
		"an integer":   3066993,
		"a wide one":   int64(3066993),
		"JSON decoded": float64(3066993),
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := intOf(value)
			if !ok || got != 3066993 {
				t.Errorf("intOf(%v) = %d, %v", value, got, ok)
			}
		})
	}
	if _, ok := intOf("3066993"); ok {
		t.Error("text was read as a number")
	}

	// Media given as a record, as a bare address, and as neither.
	if media := mediaOf(map[string]interface{}{"url": "https://example.test/a.png"}); media == nil || media.URL != "https://example.test/a.png" {
		t.Errorf("mediaOf(record) = %+v", media)
	}
	if media := mediaOf("https://example.test/a.png"); media == nil {
		t.Error("an address on its own was not read as an image")
	}
	if media := mediaOf(""); media != nil {
		t.Errorf("an empty address became an image: %+v", media)
	}
	if media := mediaOf(map[string]interface{}{"width": 100}); media != nil {
		t.Errorf("a record with no address became an image: %+v", media)
	}
	if media := mediaOf(42); media != nil {
		t.Errorf("a number became an image: %+v", media)
	}
}

func TestTheShapesAnEmbedOrAButtonCanArriveIn(t *testing.T) {
	// A flow may build these as records, a Go caller may pass the structures
	// directly, and an aspect may hand over one rather than a list. All of
	// them have to reach Discord: dropped, the message arrives as bare text
	// with the card missing and nothing saying so.
	built := Embed{Title: "built in Go"}
	for name, tc := range map[string]struct {
		value interface{}
		want  int
	}{
		"a list of records":    {[]interface{}{map[string]interface{}{"title": "a"}, map[string]interface{}{"title": "b"}}, 2},
		"a single record":      {map[string]interface{}{"title": "a"}, 1},
		"the structure itself": {built, 1},
		"a list of structures": {[]Embed{built}, 1},
		"a mixed list":         {[]interface{}{built, map[string]interface{}{"title": "b"}}, 2},
		"a list of nothing":    {[]interface{}{42, "text"}, 0},
		"something else":       {42, 0},
	} {
		t.Run("embeds: "+name, func(t *testing.T) {
			if got := len(embedsOf(tc.value)); got != tc.want {
				t.Errorf("embedsOf(%T) gave %d, want %d", tc.value, got, tc.want)
			}
		})
	}

	button := Component{Type: 2, Label: "Open"}
	for name, tc := range map[string]struct {
		value interface{}
		want  int
	}{
		"a list of records":    {[]interface{}{map[string]interface{}{"type": 1}}, 1},
		"a single record":      {map[string]interface{}{"type": 1}, 1},
		"the structure itself": {button, 1},
		"a list of structures": {[]Component{button}, 1},
		"a list of nothing":    {[]interface{}{42}, 0},
		"something else":       {"text", 0},
	} {
		t.Run("components: "+name, func(t *testing.T) {
			if got := len(componentsOf(tc.value)); got != tc.want {
				t.Errorf("componentsOf(%T) gave %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestTextArrivesHoweverItWasComputed(t *testing.T) {
	// A title computed by a transform can be a number or a boolean, and a
	// field that renders as "%!v(MISSING)" is worse than one rendering as "3".
	for name, tc := range map[string]struct {
		value interface{}
		want  string
	}{
		"text":      {"orders", "orders"},
		"a number":  {3, "3"},
		"a boolean": {true, "true"},
		"nothing":   {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := textOf(tc.value); got != tc.want {
				t.Errorf("textOf(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
