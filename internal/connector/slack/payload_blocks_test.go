package slack

import "testing"

// Blocks are what a Slack notification of any substance is made of: a header,
// fields somebody reads at a glance, a link to the thing that went wrong. Only
// `text` and `channel` were read, so a flow that built a rich message posted a
// bare line — or nothing at all, when the text was empty because the blocks
// were meant to carry it.

func TestABlockLayoutSurvivesTheHandOff(t *testing.T) {
	msg, err := slackFromData("#alerts", map[string]interface{}{
		"text": "A deploy failed",
		"blocks": []interface{}{
			map[string]interface{}{
				"type": "header",
				"text": map[string]interface{}{"type": "plain_text", "text": "orders-service"},
			},
			map[string]interface{}{
				"type": "section",
				"text": map[string]interface{}{"type": "mrkdwn", "text": "*The migration did not apply*"},
				"fields": []interface{}{
					map[string]interface{}{"type": "mrkdwn", "text": "*Environment*\nproduction"},
					map[string]interface{}{"type": "mrkdwn", "text": "*Commit*\na1b2c3d"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}

	if len(msg.Blocks) != 2 {
		t.Fatalf("the layout was dropped: %+v", msg)
	}
	if msg.Blocks[0].Type != "header" || msg.Blocks[0].Text == nil {
		t.Errorf("header = %+v", msg.Blocks[0])
	}
	if msg.Blocks[0].Text.Text != "orders-service" {
		t.Errorf("header text = %q", msg.Blocks[0].Text.Text)
	}
	if len(msg.Blocks[1].Fields) != 2 {
		t.Errorf("fields = %+v", msg.Blocks[1].Fields)
	}
}

func TestAMessageThatIsOnlyBlocksCarriesThem(t *testing.T) {
	// The common shape for an alert: the blocks are the message, with the text
	// only there as the notification preview. Dropping them left nothing.
	msg, err := slackFromData("#alerts", map[string]interface{}{
		"blocks": []interface{}{
			map[string]interface{}{"type": "section", "text": "Disk almost full"},
		},
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if len(msg.Blocks) != 1 {
		t.Fatal("a message that is only blocks carried nothing")
	}
	// A plain string where Slack wants a text object is the obvious thing to
	// write, and it means the ordinary case.
	if msg.Blocks[0].Text == nil || msg.Blocks[0].Text.Text != "Disk almost full" {
		t.Errorf("text = %+v", msg.Blocks[0].Text)
	}
}

func TestAReplyGoesToItsThread(t *testing.T) {
	// Without this a reply starts a new message in the channel, which is how a
	// tidy alert thread turns into forty separate posts.
	msg, err := slackFromData("#alerts", map[string]interface{}{
		"text":      "resolved",
		"thread_ts": "1699999999.000100",
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if msg.ThreadTS != "1699999999.000100" {
		t.Errorf("thread = %q", msg.ThreadTS)
	}
}

func TestTheBatchingHasSomethingToLookAt(t *testing.T) {
	// Messages are gathered into one post by default, and a message carrying
	// blocks or replying to a thread must go on its own. That decision reads
	// the fields this hand-off is about — while they were dropped it could
	// only ever see an empty message, so it never fired for anything a flow
	// sent.
	blocks, err := slackFromData("#alerts", map[string]interface{}{
		"blocks": []interface{}{map[string]interface{}{"type": "section", "text": "hello"}},
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if len(blocks.Blocks) == 0 {
		t.Error("a message with blocks reaches the batching with none")
	}

	threaded, err := slackFromData("#alerts", map[string]interface{}{
		"text": "resolved", "thread_ts": "1699999999.000100",
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if threaded.ThreadTS == "" {
		t.Error("a reply reaches the batching looking like a fresh message")
	}
}

func TestTheChannelInTheBodyWinsOverTheTarget(t *testing.T) {
	msg, err := slackFromData("#alerts", map[string]interface{}{
		"channel": "#incidents", "text": "hello",
	})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if msg.Channel != "#incidents" {
		t.Errorf("channel = %q, want the one in the payload", msg.Channel)
	}

	msg, err = slackFromData("#alerts", map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatalf("slackFromData: %v", err)
	}
	if msg.Channel != "#alerts" {
		t.Errorf("channel = %q, want the flow's target", msg.Channel)
	}
}

func TestSomethingThatIsNotAMessageIsRefused(t *testing.T) {
	if _, err := slackFromData("#alerts", 42); err == nil {
		t.Error("a number was accepted as a message")
	}
}
