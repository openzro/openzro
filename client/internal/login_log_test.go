package internal

import (
	"errors"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/client/peerloginmfa"
)

// A registration waiting for the user's 2FA approval is expected while the
// client polls; it is not logged as an error.
func TestLogRegisterError(t *testing.T) {
	hook := test.NewGlobal()
	t.Cleanup(hook.Reset)

	logRegisterError(peerloginmfa.Required("", "token", time.Minute))
	logRegisterError(errors.New("boom"))

	entries := hook.AllEntries()
	if assert.Len(t, entries, 2) {
		assert.Equal(t, log.InfoLevel, entries[0].Level)
		assert.NotContains(t, entries[0].Message, "update openZro")
		assert.Equal(t, log.ErrorLevel, entries[1].Level)
	}
}
