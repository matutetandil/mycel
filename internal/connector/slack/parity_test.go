package slack

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector/notifytest"
)

func TestEveryFieldOfASlackMessageCanBeWrittenByAFlow(t *testing.T) {
	notifytest.Check(t, Message{}, func(payload map[string]interface{}) (interface{}, error) {
		return slackFromData("", payload)
	}, map[string]string{})
}
