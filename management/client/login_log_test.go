package client

import (
	"errors"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/client/peerloginmfa"
)

// A device-login 2FA refusal recurs on every poll and its caller announces
// the wait once, so it is logged at debug, not as an error.
func TestLogLoginError(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)
	level := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	t.Cleanup(func() { log.SetLevel(level) })

	logLoginError(peerloginmfa.Required("", "token", time.Minute))
	logLoginError(errors.New("boom"))

	entries := hook.AllEntries()
	if assert.Len(t, entries, 2) {
		assert.Equal(t, log.DebugLevel, entries[0].Level)
		assert.NotContains(t, entries[0].Message, "token")
		assert.NotContains(t, entries[0].Message, "update openZro", "a current client needs no update")
		assert.Equal(t, log.ErrorLevel, entries[1].Level)
	}
}
