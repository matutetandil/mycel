package discord

import (
	"testing"
)

// A Discord notification is usually a card, not a line of text: a title, a
// colour that says whether this is bad news, and fields somebody reads at a
// glance. A flow builds that as a map, and only `content` was ever read — so
// the card was dropped and what went out was an empty message.

func TestACardSurvivesTheHandOff(t *testing.T) {
	msg, err := discordFromData(map[string]interface{}{
		"content": "A deploy failed",
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "orders-service",
				"description": "The migration did not apply",
				"color":       15158332,
				"url":         "https://ci.example.com/build/91",
				"fields": []interface{}{
					map[string]interface{}{"name": "Environment", "value": "production", "inline": true},
					map[string]interface{}{"name": "Commit", "value": "a1b2c3d", "inline": true},
				},
				"footer": map[string]interface{}{"text": "build 91"},
			},
		},
	})
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}

	if msg.Content != "A deploy failed" {
		t.Errorf("content = %q", msg.Content)
	}
	if len(msg.Embeds) != 1 {
		t.Fatalf("the card was dropped: %+v", msg)
	}

	card := msg.Embeds[0]
	if card.Title != "orders-service" || card.Description != "The migration did not apply" {
		t.Errorf("card = %+v", card)
	}
	// The colour is the whole reason a person can tell at a glance whether to
	// stop what they are doing.
	if card.Color != 15158332 {
		t.Errorf("colour = %d", card.Color)
	}
	if len(card.Fields) != 2 || card.Fields[0].Name != "Environment" {
		t.Errorf("fields = %+v", card.Fields)
	}
	if !card.Fields[0].Inline {
		t.Error("a field that was asked to sit inline did not")
	}
	if card.Footer == nil || card.Footer.Text != "build 91" {
		t.Errorf("footer = %+v", card.Footer)
	}
}

func TestAMessageThatIsOnlyACardIsNotEmpty(t *testing.T) {
	// The common shape: an alert is the card, with nothing above it. Dropping
	// the card left a message with nothing in it at all, which Discord refuses.
	msg, err := discordFromData(map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{"title": "Disk almost full", "color": 16776960},
		},
	})
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}
	if len(msg.Embeds) != 1 {
		t.Fatalf("a message that is only a card carried nothing: %+v", msg)
	}
}

func TestAFlowCanSayWhoTheMessageAppearsAs(t *testing.T) {
	msg, err := discordFromData(map[string]interface{}{
		"content":    "A deploy failed",
		"username":   "Mycel",
		"avatar_url": "https://example.com/icon.png",
	})
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}
	if msg.Username != "Mycel" {
		t.Errorf("username = %q, want the name the message appears under", msg.Username)
	}
	if msg.AvatarURL != "https://example.com/icon.png" {
		t.Errorf("avatar = %q", msg.AvatarURL)
	}
}

func TestWhoAMessageMayMentionIsHonoured(t *testing.T) {
	// This is the field that stops a message from pinging a whole server. An
	// alert quoting text somebody else wrote — a commit message, a customer's
	// note — can contain @everyone, and dropping this setting is the
	// difference between a notification and waking up a company.
	msg, err := discordFromData(map[string]interface{}{
		"content": "A customer wrote: @everyone please help",
		"allowed_mentions": map[string]interface{}{
			"parse": []interface{}{},
		},
	})
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}
	if msg.AllowedMentions == nil {
		t.Fatal("the mention rules were dropped, so quoted text can ping everybody")
	}
	if len(msg.AllowedMentions.Parse) != 0 {
		t.Errorf("parse = %v, want nothing allowed", msg.AllowedMentions.Parse)
	}
}

func TestAMessageCanStartAThread(t *testing.T) {
	msg, err := discordFromData(map[string]interface{}{
		"content":     "A deploy failed",
		"thread_name": "orders-service build 91",
	})
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}
	if msg.ThreadName != "orders-service build 91" {
		t.Errorf("thread = %q", msg.ThreadName)
	}
}

func TestALineOfTextIsAMessage(t *testing.T) {
	msg, err := discordFromData("A deploy failed")
	if err != nil {
		t.Fatalf("discordFromData: %v", err)
	}
	if msg.Content != "A deploy failed" {
		t.Errorf("content = %q", msg.Content)
	}
}

func TestSomethingThatIsNotAMessageIsRefused(t *testing.T) {
	if _, err := discordFromData(42); err == nil {
		t.Error("a number was accepted as a message")
	}
}
