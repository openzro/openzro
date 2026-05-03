package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/server/activity"
)

func TestNewSqlStore(t *testing.T) {
	dataDir := t.TempDir()
	key, _ := GenerateKey()
	store, err := NewSqlStore(context.Background(), dataDir, key)
	if err != nil {
		t.Fatal(err)
		return
	}
	defer store.Close(context.Background()) //nolint

	accountID := "account_1"

	for i := 0; i < 10; i++ {
		_, err = store.Save(context.Background(), &activity.Event{
			Timestamp:   time.Now().UTC(),
			Activity:    activity.PeerAddedByUser,
			InitiatorID: "user_" + fmt.Sprint(i),
			TargetID:    "peer_" + fmt.Sprint(i),
			AccountID:   accountID,
		})
		if err != nil {
			t.Fatal(err)
			return
		}
	}

	result, err := store.Get(context.Background(), accountID, 0, 10, false)
	if err != nil {
		t.Fatal(err)
		return
	}

	assert.Len(t, result, 10)
	assert.True(t, result[0].Timestamp.Before(result[len(result)-1].Timestamp))

	result, err = store.Get(context.Background(), accountID, 0, 5, true)
	if err != nil {
		t.Fatal(err)
		return
	}

	assert.Len(t, result, 5)
	assert.True(t, result[0].Timestamp.After(result[len(result)-1].Timestamp))
}

// TestEngineDispatch_MissingDSN locks in the dispatch behavior so a
// future refactor can't silently drop the postgres or mysql branches.
// We don't stand up real DB containers here (the rest of the SQL store
// suite stays sqlite-only), but we do prove that asking for an engine
// without the matching DSN returns a clear error naming the env var.
// That has caught two regressions on the flow store side already.
func TestEngineDispatch_MissingDSN(t *testing.T) {
	cases := []struct {
		engine     string
		wantEnvVar string
	}{
		{engine: "postgres", wantEnvVar: postgresDsnEnv},
		{engine: "mysql", wantEnvVar: mysqlDsnEnv},
	}
	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			t.Setenv(storeEngineEnv, tc.engine)
			key, _ := GenerateKey()
			_, err := NewSqlStore(context.Background(), t.TempDir(), key)
			if err == nil {
				t.Fatalf("expected error when %s is unset, got nil", tc.wantEnvVar)
			}
			assert.Contains(t, err.Error(), tc.wantEnvVar)
		})
	}
}

// TestEngineDispatch_UnsupportedEngine documents that misspelled or
// future-but-not-implemented engines fail-loud at NewSqlStore time
// rather than silently falling back to sqlite — operators get a clear
// configuration error in the management pod's first log line.
func TestEngineDispatch_UnsupportedEngine(t *testing.T) {
	t.Setenv(storeEngineEnv, "clickhouse")
	key, _ := GenerateKey()
	_, err := NewSqlStore(context.Background(), t.TempDir(), key)
	if err == nil {
		t.Fatal("expected error for unsupported engine, got nil")
	}
	assert.Contains(t, err.Error(), "unsupported store engine")
}
