package push

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector/notifytest"
)

func TestEveryFieldOfANotificationCanBeWrittenByAFlow(t *testing.T) {
	notifytest.Check(t, Message{}, func(payload map[string]interface{}) (interface{}, error) {
		return pushFromData("", payload)
	}, map[string]string{
		// These three carry the shape of FCM's HTTP v1 message — an android
		// block, an apns block, a webpush block — and the send path builds the
		// legacy flat message instead, so nothing transmits them. Reading them
		// from a payload would promise platform options that are dropped one
		// layer further down, which is the failure this whole check exists to
		// prevent. They become meaningful with the migration recorded in
		// DISCREPANCIES.md; until then, not accepting them is the honest answer.
		"android": "the send path speaks the legacy FCM message, which has no android block; nothing would transmit it",
		"apns":    "the send path speaks the legacy FCM message, which has no apns block; nothing would transmit it",
		"web":     "the send path speaks the legacy FCM message, which has no webpush block; nothing would transmit it",
	})
}
