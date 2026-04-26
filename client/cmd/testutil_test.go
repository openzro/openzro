package cmd

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/types"

	"github.com/openzro/openzro/util"

	"google.golang.org/grpc"

	"github.com/openzro/management-integrations/integrations"

	clientProto "github.com/openzro/openzro/client/proto"
	client "github.com/openzro/openzro/client/server"
	mgmtProto "github.com/openzro/openzro/management/proto"
	mgmt "github.com/openzro/openzro/management/server"
	sigProto "github.com/openzro/openzro/signal/proto"
	sig "github.com/openzro/openzro/signal/server"
)

func startTestingServices(t *testing.T) string {
	t.Helper()
	config := &types.Config{}
	_, err := util.ReadJson("../testdata/management.json", config)
	if err != nil {
		t.Fatal(err)
	}

	_, signalLis := startSignal(t)
	signalAddr := signalLis.Addr().String()
	config.Signal.URI = signalAddr

	_, mgmLis := startManagement(t, config, "../testdata/store.sql")
	mgmAddr := mgmLis.Addr().String()
	return mgmAddr
}

func startSignal(t *testing.T) (*grpc.Server, net.Listener) {
	t.Helper()
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	srv, err := sig.NewServer(context.Background(), otel.Meter(""))
	require.NoError(t, err)

	sigProto.RegisterSignalExchangeServer(s, srv)
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()

	return s, lis
}

func startManagement(t *testing.T, config *types.Config, testFile string) (*grpc.Server, net.Listener) {
	t.Helper()

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	store, cleanUp, err := store.NewTestStoreFromSQL(context.Background(), testFile, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanUp)

	peersUpdateManager := mgmt.NewPeersUpdateManager(nil)
	eventStore := &activity.InMemoryEventStore{}
	if err != nil {
		return nil, nil
	}
	iv, _ := integrations.NewIntegratedValidator(context.Background(), eventStore)

	metrics, err := telemetry.NewDefaultAppMetrics(context.Background())
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	settingsMockManager := settings.NewMockManager(ctrl)
	permissionsManagerMock := permissions.NewMockManager(ctrl)

	settingsMockManager.EXPECT().
		GetSettings(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&types.Settings{}, nil).
		AnyTimes()

	accountManager, err := mgmt.BuildManager(context.Background(), store, peersUpdateManager, nil, "", "openzro.selfhosted", eventStore, nil, false, iv, metrics, port_forwarding.NewControllerMock(), settingsMockManager, permissionsManagerMock, false)
	if err != nil {
		t.Fatal(err)
	}

	secretsManager := mgmt.NewTimeBasedAuthSecretsManager(peersUpdateManager, config.TURNConfig, config.Relay, settingsMockManager)
	mgmtServer, err := mgmt.NewServer(context.Background(), config, accountManager, settingsMockManager, peersUpdateManager, secretsManager, nil, nil, nil, &mgmt.MockIntegratedValidator{})
	if err != nil {
		t.Fatal(err)
	}
	mgmtProto.RegisterManagementServiceServer(s, mgmtServer)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Error(err)
		}
	}()

	return s, lis
}

func startClientDaemon(
	t *testing.T, ctx context.Context, _, _ string,
) (*grpc.Server, net.Listener) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()

	server := client.New(ctx,
		"", false)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	clientProto.RegisterDaemonServiceServer(s, server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Error(err)
		}
	}()

	time.Sleep(time.Second)

	return s, lis
}
