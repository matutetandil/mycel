package slack

import "fmt"

// slackFromData turns what a flow wrote into the message that is posted.
//
// This lived inline in the send path and read two fields — `text` and
// `channel`. A Slack notification of any substance is blocks: the header, the
// fields, the button. All of it was dropped, so a flow that built a rich
// message posted a bare line, or nothing at all when the text was empty because
// the blocks were meant to carry it.
//
// Two of the dropped fields decide behaviour rather than appearance. `blocks`
// and `thread_ts` are what the batching added in 2.5.0 checks to decide a
// message must go on its own rather than be gathered with others — and since
// neither could be set from a payload, that decision never had anything to
// look at.
func slackFromData(target string, payload interface{}) (*Message, error) {
	switch p := payload.(type) {
	case *Message:
		return p, nil
	case Message:
		return &p, nil
	case string:
		return &Message{Channel: target, Text: p}, nil
	case map[string]interface{}:
		msg := &Message{
			Channel:     textOf(p["channel"]),
			Text:        textOf(p["text"]),
			ThreadTS:    textOf(p["thread_ts"]),
			Username:    textOf(p["username"]),
			IconEmoji:   textOf(p["icon_emoji"]),
			IconURL:     textOf(p["icon_url"]),
			Blocks:      blocksOf(p["blocks"]),
			Attachments: attachmentsOf(p["attachments"]),
		}
		if unfurl, ok := p["unfurl_links"].(bool); ok {
			msg.UnfurlLinks = unfurl
		}
		if unfurl, ok := p["unfurl_media"].(bool); ok {
			msg.UnfurlMedia = unfurl
		}
		if mrkdwn, ok := p["mrkdwn"].(bool); ok {
			msg.Mrkdwn = mrkdwn
		}
		if msg.Channel == "" {
			msg.Channel = target
		}
		return msg, nil
	}
	return nil, fmt.Errorf("a Slack message is a record or a line of text, and %T is neither", payload)
}

// blocksOf reads the layout a message is built from.
//
// Blocks nest — a section holds fields, an actions row holds elements — so this
// hands the pieces it does not model to the same reader rather than flattening
// them away.
func blocksOf(value interface{}) []Block {
	switch v := value.(type) {
	case []Block:
		return v
	case Block:
		return []Block{v}
	case map[string]interface{}:
		return []Block{blockOf(v)}
	case []interface{}:
		out := make([]Block, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case Block:
				out = append(out, entry)
			case map[string]interface{}:
				out = append(out, blockOf(entry))
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func blockOf(m map[string]interface{}) Block {
	block := Block{
		Type:    textOf(m["type"]),
		BlockID: textOf(m["block_id"]),
		Text:    textObjectOf(m["text"]),
	}
	if fields, ok := m["fields"].([]interface{}); ok {
		for _, item := range fields {
			if text := textObjectOf(item); text != nil {
				block.Fields = append(block.Fields, *text)
			}
		}
	}
	return block
}

// textObjectOf reads a piece of text, which Slack carries as a record naming
// how it should be rendered. A flow writing a plain string means the ordinary
// case, so that is accepted too.
func textObjectOf(value interface{}) *TextObject {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return &TextObject{Type: "mrkdwn", Text: v}
	case TextObject:
		return &v
	case map[string]interface{}:
		text := textOf(v["text"])
		if text == "" {
			return nil
		}
		kind := textOf(v["type"])
		if kind == "" {
			kind = "mrkdwn"
		}
		return &TextObject{Type: kind, Text: text}
	}
	return nil
}

func attachmentsOf(value interface{}) []Attachment {
	switch v := value.(type) {
	case []Attachment:
		return v
	case Attachment:
		return []Attachment{v}
	case map[string]interface{}:
		return []Attachment{attachmentOf(v)}
	case []interface{}:
		out := make([]Attachment, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case Attachment:
				out = append(out, entry)
			case map[string]interface{}:
				out = append(out, attachmentOf(entry))
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func attachmentOf(m map[string]interface{}) Attachment {
	attachment := Attachment{
		Color:      textOf(m["color"]),
		Pretext:    textOf(m["pretext"]),
		AuthorName: textOf(m["author_name"]),
		AuthorLink: textOf(m["author_link"]),
		AuthorIcon: textOf(m["author_icon"]),
		Title:      textOf(m["title"]),
		TitleLink:  textOf(m["title_link"]),
		Text:       textOf(m["text"]),
		ImageURL:   textOf(m["image_url"]),
		ThumbURL:   textOf(m["thumb_url"]),
		Footer:     textOf(m["footer"]),
		FooterIcon: textOf(m["footer_icon"]),
	}

	if fields, ok := m["fields"].([]interface{}); ok {
		for _, item := range fields {
			field, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			entry := AttachmentField{
				Title: textOf(field["title"]),
				Value: textOf(field["value"]),
			}
			if short, ok := field["short"].(bool); ok {
				entry.Short = short
			}
			attachment.Fields = append(attachment.Fields, entry)
		}
	}

	if ts, ok := intOf(m["ts"]); ok {
		attachment.Timestamp = int64(ts)
	}

	return attachment
}

func intOf(value interface{}) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func textOf(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	return fmt.Sprintf("%v", value)
}
