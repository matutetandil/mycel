package sms

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector/notifytest"
)

func TestEveryFieldOfATextMessageCanBeWrittenByAFlow(t *testing.T) {
	notifytest.Check(t, Message{}, func(payload map[string]interface{}) (interface{}, error) {
		return smsFromData("", payload)
	}, map[string]string{})
}
