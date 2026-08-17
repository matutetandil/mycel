package email

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector/notifytest"
)

// Every field an email can carry, against what the reader reads. See
// internal/connector/notifytest: each notification connector translates a
// flow's payload by hand, and each one forgot something different.
func TestEveryFieldOfAnEmailCanBeWrittenByAFlow(t *testing.T) {
	notifytest.Check(t, Email{}, func(payload map[string]interface{}) (interface{}, error) {
		return emailFromData("", payload)
	}, map[string]string{
		"send_at": "scheduling is not implemented by any provider path yet, so accepting it would promise a delay nobody applies",
	})
}
