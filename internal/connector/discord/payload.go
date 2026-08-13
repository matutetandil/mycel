package discord

import "fmt"

// discordFromData turns what a flow wrote into the message that is sent.
//
// This used to live inline in the send path and read one field: `content`. A
// Discord notification is usually a card rather than a line of text — a title,
// a colour that says whether this is bad news, fields somebody reads at a
// glance — and all of it was dropped, so an alert built as a card went out as
// an empty message, which Discord refuses.
//
// The mention rules were dropped with it, and that one has teeth: an alert
// quoting text somebody else wrote — a commit message, a customer's note — can
// contain @everyone, and without the rules it pings the whole server.
func discordFromData(payload interface{}) (*Message, error) {
	switch p := payload.(type) {
	case *Message:
		return p, nil
	case Message:
		return &p, nil
	case string:
		return &Message{Content: p}, nil
	case map[string]interface{}:
		msg := &Message{
			Content:         textOf(p["content"]),
			Username:        textOf(p["username"]),
			AvatarURL:       textOf(p["avatar_url"]),
			ThreadName:      textOf(p["thread_name"]),
			Embeds:          embedsOf(p["embeds"]),
			AllowedMentions: mentionsOf(p["allowed_mentions"]),
		}
		if tts, ok := p["tts"].(bool); ok {
			msg.TTS = tts
		}
		return msg, nil
	}
	return nil, fmt.Errorf("a Discord message is a record or a line of text, and %T is neither", payload)
}

func embedsOf(value interface{}) []Embed {
	switch v := value.(type) {
	case []Embed:
		return v
	case Embed:
		return []Embed{v}
	case map[string]interface{}:
		return []Embed{embedOf(v)}
	case []interface{}:
		out := make([]Embed, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case Embed:
				out = append(out, entry)
			case map[string]interface{}:
				out = append(out, embedOf(entry))
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func embedOf(m map[string]interface{}) Embed {
	embed := Embed{
		Title:       textOf(m["title"]),
		Type:        textOf(m["type"]),
		Description: textOf(m["description"]),
		URL:         textOf(m["url"]),
		Timestamp:   textOf(m["timestamp"]),
		Fields:      fieldsOf(m["fields"]),
	}
	if colour, ok := intOf(m["color"]); ok {
		embed.Color = colour
	}
	if footer, ok := m["footer"].(map[string]interface{}); ok {
		embed.Footer = &EmbedFooter{
			Text:    textOf(footer["text"]),
			IconURL: textOf(footer["icon_url"]),
		}
	}
	if author, ok := m["author"].(map[string]interface{}); ok {
		embed.Author = &EmbedAuthor{
			Name:    textOf(author["name"]),
			URL:     textOf(author["url"]),
			IconURL: textOf(author["icon_url"]),
		}
	}
	embed.Image = mediaOf(m["image"])
	embed.Thumbnail = mediaOf(m["thumbnail"])
	return embed
}

func mediaOf(value interface{}) *EmbedMedia {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return &EmbedMedia{URL: v}
	case map[string]interface{}:
		url := textOf(v["url"])
		if url == "" {
			return nil
		}
		return &EmbedMedia{URL: url}
	}
	return nil
}

func fieldsOf(value interface{}) []EmbedField {
	list, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]EmbedField); ok {
			return typed
		}
		return nil
	}

	out := make([]EmbedField, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		field := EmbedField{
			Name:  textOf(m["name"]),
			Value: textOf(m["value"]),
		}
		if inline, ok := m["inline"].(bool); ok {
			field.Inline = inline
		}
		out = append(out, field)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mentionsOf reads the rules that decide who a message may ping.
//
// An empty list is a decision — "nobody" — and has to be told apart from the
// field being absent, which is why the block being present at all produces
// rules rather than nil.
func mentionsOf(value interface{}) *AllowedMentions {
	m, ok := value.(map[string]interface{})
	if !ok {
		if typed, ok := value.(*AllowedMentions); ok {
			return typed
		}
		return nil
	}

	mentions := &AllowedMentions{
		Parse: stringsOf(m["parse"]),
		Roles: stringsOf(m["roles"]),
		Users: stringsOf(m["users"]),
	}
	if replied, ok := m["replied_user"].(bool); ok {
		mentions.RepliedUser = replied
	}
	if mentions.Parse == nil {
		// Present and empty means nobody, which is the point of writing it.
		mentions.Parse = []string{}
	}
	return mentions
}

func stringsOf(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := textOf(item); s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
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
