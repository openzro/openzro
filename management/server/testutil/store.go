//go:build !ios
// +build !ios

package testutil

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	testcontainersredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	pgContainer    *postgres.PostgresContainer
	mysqlContainer *mysql.MySQLContainer
)

// CreateMysqlTestContainer creates a new MySQL container for testing.
func CreateMysqlTestContainer() (func(), string, error) {
	ctx := context.Background()

	if mysqlContainer != nil {
		connStr, err := mysqlContainer.ConnectionString(ctx)
		if err != nil {
			return nil, "", err
		}
		return noOpCleanup, connStr, nil
	}

	var err error
	// Use the official `mysql:8.0` image rather than the upstream's
	// pre-warmed `mlsmaycon/warmed-mysql:8` (a private Docker Hub
	// image gated on DOCKER_USER/DOCKER_TOKEN secrets we don't ship).
	//
	// The official image emits "ready for connections" twice during
	// startup: once on the bootstrap socket while the entrypoint runs
	// init scripts, then again on the real port:3306 after restarting.
	// Wait for the second occurrence so the connection string we hand
	// back is actually accepting traffic, and give it a generous budget
	// because cold pulls + init-script replay can take 30-60s.
	mysqlContainer, err = mysql.RunContainer(ctx,
		testcontainers.WithImage("mysql:8.0"),
		mysql.WithDatabase("testing"),
		mysql.WithUsername("root"),
		mysql.WithPassword("testing"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("/usr/sbin/mysqld: ready for connections").
				WithOccurrence(2).WithStartupTimeout(120*time.Second).WithPollInterval(250*time.Millisecond),
		),
	)
	if err != nil {
		return nil, "", err
	}

	cleanup := func() {
		if mysqlContainer != nil {
			timeoutCtx, cancelFunc := context.WithTimeout(ctx, 1*time.Second)
			defer cancelFunc()
			if err = mysqlContainer.Terminate(timeoutCtx); err != nil {
				log.WithContext(ctx).Warnf("failed to stop mysql container %s: %s", mysqlContainer.GetContainerID(), err)
			}
			mysqlContainer = nil // reset the container to allow recreation
		}
	}

	talksConn, err := mysqlContainer.ConnectionString(ctx)
	if err != nil {
		return nil, "", err
	}

	return cleanup, talksConn, nil
}

// CreatePostgresTestContainer creates a new PostgreSQL container for testing.
func CreatePostgresTestContainer() (func(), string, error) {
	ctx := context.Background()

	if pgContainer != nil {
		connStr, err := pgContainer.ConnectionString(ctx)
		if err != nil {
			return nil, "", err
		}
		return noOpCleanup, connStr, nil
	}

	var err error
	pgContainer, err = postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("openzro"),
		postgres.WithUsername("root"),
		postgres.WithPassword("openzro"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(15*time.Second),
		),
	)
	if err != nil {
		return nil, "", err
	}

	cleanup := func() {
		if pgContainer != nil {
			timeoutCtx, cancelFunc := context.WithTimeout(ctx, 1*time.Second)
			defer cancelFunc()
			if err = pgContainer.Terminate(timeoutCtx); err != nil {
				log.WithContext(ctx).Warnf("failed to stop postgres container %s: %s", pgContainer.GetContainerID(), err)
			}
			pgContainer = nil // reset the container to allow recreation
		}

	}

	talksConn, err := pgContainer.ConnectionString(ctx)
	if err != nil {
		return nil, "", err
	}

	return cleanup, talksConn, nil
}

func noOpCleanup() {
	// no-op
}

// CreateRedisTestContainer creates a new Redis container for testing.
func CreateRedisTestContainer() (func(), string, error) {
	ctx := context.Background()

	redisContainer, err := testcontainersredis.RunContainer(ctx, testcontainers.WithImage("redis:7"))
	if err != nil {
		return nil, "", err
	}

	cleanup := func() {
		timeoutCtx, cancelFunc := context.WithTimeout(ctx, 1*time.Second)
		defer cancelFunc()
		if err = redisContainer.Terminate(timeoutCtx); err != nil {
			log.WithContext(ctx).Warnf("failed to stop redis container %s: %s", redisContainer.GetContainerID(), err)
		}
	}

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		return nil, "", err
	}

	return cleanup, redisURL, nil
}
