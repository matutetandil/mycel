package discord

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector/notifytest"
)

func TestEveryFieldOfADiscordMessageCanBeWrittenByAFlow(t *testing.T) {
	notifytest.Check(t, Message{}, func(payload map[string]interface{}) (interface{}, error) {
		return discordFromData(payload)
	}, map[string]string{})
}
