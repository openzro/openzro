package server

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/testutil"
	"github.com/openzro/openzro/management/server/types"
)

// Two management replicas serving one database is the deployment this project
// ships, and it is the shape most of the concurrency defects in #143 live in:
// AcquireWriteLockByUID is a process-local mutex, so anything relying on it for
// mutual exclusion is unprotected the moment a second replica exists.
//
// Every test written for those defects so far has had to simulate that from
// inside one process — hand-rolling the transaction the manager would have run,
// which stops guarding anything as soon as the manager's own statement order
// changes. This harness removes that compromise: two independent Store
// instances, with independent lock maps, against one physical database, so a
// test can drive the real manager methods on both sides.
//
// It is infrastructure only. No production behavior changes here, and no
// invariant is fixed — the point is to be able to demonstrate the failures
// first, so each fix can land red-to-green instead of by argument.

// twoReplicas is a pair of managers that share a database and share nothing
// else — separate stores, separate connection pools, separate in-process locks.
type twoReplicas struct {
	A, B *DefaultAccountManager
}

// newTwoReplicas builds the pair against a database of its own.
//
// Skips unless the store engine is Postgres or MySQL. SQLite cannot serve this:
// the test store runs it with a single shared connection, which serializes
// every transaction and would make each of these tests pass for the wrong
// reason.
func newTwoReplicas(t *testing.T) *twoReplicas {
	t.Helper()

	engine := types.Engine(strings.ToLower(os.Getenv("OPENZRO_STORE_ENGINE")))
	switch engine {
	case types.PostgresStoreEngine, types.MysqlStoreEngine:
	default:
		t.Skipf("two-replica harness needs Postgres or MySQL, OPENZRO_STORE_ENGINE=%q", engine)
	}

	ctx := context.Background()
	baseDSN := containerDSN(t, engine)
	dsn := createHarnessDB(t, engine, baseDSN)

	// The first store migrates the schema; the second attaches to what the
	// first created. Two constructor calls rather than one store shared by
	// both managers — a shared *SqlStore would also share resourceLocks, and
	// the harness would then serialize exactly what it exists to let race.
	storeA := openStore(t, ctx, engine, dsn, false)
	storeB := openStore(t, ctx, engine, dsn, true)
	t.Cleanup(func() {
		storeA.Close(ctx)
		storeB.Close(ctx)
	})

	return &twoReplicas{
		A: buildHarnessManager(t, storeA),
		B: buildHarnessManager(t, storeB),
	}
}

func containerDSN(t *testing.T, engine types.Engine) string {
	t.Helper()

	var (
		cleanup func()
		dsn     string
		err     error
	)
	switch engine {
	case types.PostgresStoreEngine:
		cleanup, dsn, err = testutil.CreatePostgresTestContainer()
	case types.MysqlStoreEngine:
		cleanup, dsn, err = testutil.CreateMysqlTestContainer()
	}
	require.NoError(t, err, "start %s container", engine)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return dsn
}

// createHarnessDB gives the test a database of its own inside the container, so
// two harness tests running in sequence cannot see each other's rows.
func createHarnessDB(t *testing.T, engine types.Engine, baseDSN string) string {
	t.Helper()

	var (
		admin *gorm.DB
		err   error
	)
	switch engine {
	case types.PostgresStoreEngine:
		admin, err = gorm.Open(postgres.Open(baseDSN), &gorm.Config{})
	case types.MysqlStoreEngine:
		admin, err = gorm.Open(mysql.Open(baseDSN), &gorm.Config{})
	}
	require.NoError(t, err, "connect to %s", engine)

	name := "harness_" + strings.ReplaceAll(uuid.New().String(), "-", "_")
	require.NoError(t, admin.Exec(fmt.Sprintf("CREATE DATABASE %s", name)).Error, "create database")

	t.Cleanup(func() {
		drop := fmt.Sprintf("DROP DATABASE %s", name)
		if engine == types.PostgresStoreEngine {
			drop += " WITH (FORCE)"
		}
		if err := admin.Exec(drop).Error; err != nil {
			t.Logf("harness: failed to drop %s: %v", name, err)
		}
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// Same rewrite the store's own test helper performs: swap the database
	// name in place, whichever DSN shape the driver uses.
	return regexp.MustCompile(`(?P<pre>[:/@])(?P<dbname>[^/?]+)(?P<post>\?|$)`).
		ReplaceAllString(baseDSN, `${pre}`+name+`${post}`)
}

func openStore(t *testing.T, ctx context.Context, engine types.Engine, dsn string, skipMigration bool) store.Store {
	t.Helper()

	var (
		s   store.Store
		err error
	)
	switch engine {
	case types.PostgresStoreEngine:
		s, err = store.NewPostgresqlStore(ctx, dsn, nil, skipMigration)
	case types.MysqlStoreEngine:
		s, err = store.NewMysqlStore(ctx, dsn, nil, skipMigration)
	}
	require.NoError(t, err, "open %s store (skipMigration=%v)", engine, skipMigration)
	return s
}

// buildHarnessManager mirrors createManager's wiring, against a store the
// caller owns.
func buildHarnessManager(t *testing.T, s store.Store) *DefaultAccountManager {
	t.Helper()

	ctx := context.Background()
	metrics, err := telemetry.NewDefaultAppMetrics(ctx)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	settingsMock := settings.NewMockManager(ctrl)
	settingsMock.EXPECT().GetExtraSettings(gomock.Any(), gomock.Any()).
		Return(&types.ExtraSettings{}, nil).AnyTimes()
	settingsMock.EXPECT().UpdateExtraSettings(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(false, nil).AnyTimes()

	am, err := BuildManager(ctx, s, NewPeersUpdateManager(nil), nil, "", "openzro.cloud",
		&activity.InMemoryEventStore{}, nil, false, MockIntegratedValidator{}, metrics,
		port_forwarding.NewControllerMock(), settingsMock, permissions.NewManager(s), false)
	require.NoError(t, err)

	return am
}

// barrier aligns two goroutines at a chosen point. Both call wait; neither
// returns until both have arrived, so a test can put two transactions in the
// same window without sleeping and hoping.
type barrier struct {
	once sync.Once
	ch   chan struct{}
	wg   sync.WaitGroup
}

func newBarrier() *barrier {
	b := &barrier{ch: make(chan struct{})}
	b.wg.Add(2)
	return b
}

func (b *barrier) wait() {
	b.wg.Done()
	b.once.Do(func() {
		go func() {
			b.wg.Wait()
			close(b.ch)
		}()
	})
	<-b.ch
}

// TestTwoReplicaHarness_LocksAreIndependent is the sentinel.
//
// If the two managers ever end up sharing a Store — or anything else that
// shares resourceLocks — every test built on this harness would still pass,
// while quietly serializing the two sides and proving nothing. That failure is
// invisible from the tests themselves, so it gets checked here directly:
// holding A's account lock must not delay B's.
func TestTwoReplicaHarness_LocksAreIndependent(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()

	unlockA := r.A.Store.AcquireWriteLockByUID(ctx, "sentinel-account")
	defer unlockA()

	acquired := make(chan struct{})
	go func() {
		unlockB := r.B.Store.AcquireWriteLockByUID(ctx, "sentinel-account")
		unlockB()
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(3 * time.Second):
		t.Fatal("B blocked on A's account lock: the two replicas are sharing one lock map, " +
			"so every test on this harness would serialize and hide the races it exists to expose")
	}
}

// TestTwoReplicaHarness_SharesOneDatabase is the other half of the sentinel: the
// replicas have to be independent, but not so independent that they stop seeing
// each other's writes.
func TestTwoReplicaHarness_SharesOneDatabase(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()

	const accountID = "shared-db-account"
	account := newAccountWithId(ctx, accountID, "harness-user", "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	got, err := r.B.Store.GetAccount(ctx, accountID)
	require.NoError(t, err, "B cannot read the account A wrote: the replicas are on different databases")
	require.Equal(t, accountID, got.Id)
}

// TestTwoReplicaHarness_BarrierAligns exercises the barrier itself. It is the
// piece the concurrency tests will lean on to put two transactions in the same
// window without sleeping, so it should not be the one thing here that nothing
// checks.
func TestTwoReplicaHarness_BarrierAligns(t *testing.T) {
	b := newBarrier()

	var first, second time.Time
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		b.wait()
		first = time.Now()
	}()

	// Arrive late on purpose: the early side must still be waiting.
	time.Sleep(300 * time.Millisecond)
	b.wait()
	second = time.Now()

	wg.Wait()

	require.WithinDuration(t, second, first, 100*time.Millisecond,
		"both sides must leave the barrier together; the early one returned before the late one arrived")
}
