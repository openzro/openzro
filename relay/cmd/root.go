package cmd

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openzro/openzro/encryption"
	"github.com/openzro/openzro/relay/auth"
	"github.com/openzro/openzro/relay/server"
	"github.com/openzro/openzro/signal/metrics"
	"github.com/openzro/openzro/util"
)

type Config struct {
	ListenAddress string
	// in HA every peer connect to a common domain, the instance domain has been distributed during the p2p connection
	// it is a domain:port or ip:port
	ExposedAddress     string
	MetricsPort        int
	LetsencryptEmail   string
	LetsencryptDataDir string
	LetsencryptDomains []string
	// in case of using Route 53 for DNS challenge the credentials should be provided in the environment variables or
	// in the AWS credentials file
	LetsencryptAWSRoute53 bool
	TlsCertFile           string
	TlsKeyFile            string
	AuthSecret            string
	LogLevel              string
	LogFile               string

	// Multi-pod (ADR-0014) settings — leave ClusterHeadless empty
	// for single-pod deployments. The relay then runs exactly as
	// before, with no inter-pod fabric.
	ClusterHeadless   string // K8s Headless Service FQDN
	ClusterPort       int    // inter-pod TCP port (default 7090)
	PodIP             string // POD_IP via the K8s downward API
	ClusterAuthSecret string // shared HMAC key for HELLO authentication
}

func (c Config) Validate() error {
	if c.ExposedAddress == "" {
		return fmt.Errorf("exposed address is required")
	}
	if c.AuthSecret == "" {
		return fmt.Errorf("auth secret is required")
	}
	return nil
}

func (c Config) HasCertConfig() bool {
	return c.TlsCertFile != "" && c.TlsKeyFile != ""
}

func (c Config) HasLetsEncrypt() bool {
	return c.LetsencryptDataDir != "" && c.LetsencryptDomains != nil && len(c.LetsencryptDomains) > 0
}

var (
	cobraConfig *Config
	rootCmd     = &cobra.Command{
		Use:           "relay",
		Short:         "Relay service",
		Long:          "Relay service for Openzro agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          execute,
	}
)

func init() {
	_ = util.InitLog("trace", util.LogConsole)
	cobraConfig = &Config{}
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.ListenAddress, "listen-address", "l", ":443", "listen address")
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.ExposedAddress, "exposed-address", "e", "", "instance domain address (or ip) and port, it will be distributes between peers")
	rootCmd.PersistentFlags().IntVar(&cobraConfig.MetricsPort, "metrics-port", 9090, "metrics endpoint http port. Metrics are accessible under host:metrics-port/metrics")
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.LetsencryptDataDir, "letsencrypt-data-dir", "d", "", "a directory to store Let's Encrypt data. Required if Let's Encrypt is enabled.")
	rootCmd.PersistentFlags().StringSliceVarP(&cobraConfig.LetsencryptDomains, "letsencrypt-domains", "a", nil, "list of domains to issue Let's Encrypt certificate for. Enables TLS using Let's Encrypt. Will fetch and renew certificate, and run the server with TLS")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.LetsencryptEmail, "letsencrypt-email", "", "email address to use for Let's Encrypt certificate registration")
	rootCmd.PersistentFlags().BoolVar(&cobraConfig.LetsencryptAWSRoute53, "letsencrypt-aws-route53", false, "use AWS Route 53 for Let's Encrypt DNS challenge")
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.TlsCertFile, "tls-cert-file", "c", "", "")
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.TlsKeyFile, "tls-key-file", "k", "", "")
	rootCmd.PersistentFlags().StringVarP(&cobraConfig.AuthSecret, "auth-secret", "s", "", "auth secret")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.LogLevel, "log-level", "info", "log level")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.LogFile, "log-file", "console", "log file")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.ClusterHeadless, "cluster-headless", "", "K8s Headless Service FQDN that resolves to every relay pod (enables ADR-0014 multi-pod fabric)")
	rootCmd.PersistentFlags().IntVar(&cobraConfig.ClusterPort, "cluster-port", 0, "inter-pod TCP port (defaults to 7090). Same value on every pod.")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.PodIP, "pod-ip", "", "this pod's IP, set from the K8s downward API. Required when --cluster-headless is set.")
	rootCmd.PersistentFlags().StringVar(&cobraConfig.ClusterAuthSecret, "cluster-auth-secret", "", "shared HMAC secret authenticating inter-pod HELLO frames. Same value on every relay pod. Empty = unsigned HELLO (legacy; requires NetworkPolicy isolation).")

	setFlagsFromEnvVars(rootCmd)
}

func Execute() error {
	return rootCmd.Execute()
}

func waitForExitSignal() {
	osSigs := make(chan os.Signal, 1)
	signal.Notify(osSigs, syscall.SIGINT, syscall.SIGTERM)
	<-osSigs
}

func execute(cmd *cobra.Command, args []string) error {
	err := cobraConfig.Validate()
	if err != nil {
		log.Debugf("invalid config: %s", err)
		return fmt.Errorf("invalid config: %s", err)
	}

	err = util.InitLog(cobraConfig.LogLevel, cobraConfig.LogFile)
	if err != nil {
		log.Debugf("failed to initialize log: %s", err)
		return fmt.Errorf("failed to initialize log: %s", err)
	}

	metricsServer, err := metrics.NewServer(cobraConfig.MetricsPort, "")
	if err != nil {
		log.Debugf("setup metrics: %v", err)
		return fmt.Errorf("setup metrics: %v", err)
	}

	go func() {
		log.Infof("running metrics server: %s%s", metricsServer.Addr, metricsServer.Endpoint)
		if err := metricsServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start metrics server: %v", err)
		}
	}()

	srvListenerCfg := server.ListenerConfig{
		Address: cobraConfig.ListenAddress,
	}

	tlsConfig, tlsSupport, err := handleTLSConfig(cobraConfig)
	if err != nil {
		log.Debugf("failed to setup TLS config: %s", err)
		return fmt.Errorf("failed to setup TLS config: %s", err)
	}
	srvListenerCfg.TLSConfig = tlsConfig

	hashedSecret := sha256.Sum256([]byte(cobraConfig.AuthSecret))
	authenticator := auth.NewTimedHMACValidator(hashedSecret[:], 24*time.Hour)

	cfg := server.Config{
		Meter:          metricsServer.Meter,
		ExposedAddress: cobraConfig.ExposedAddress,
		AuthValidator:  authenticator,
		TLSSupport:     tlsSupport,
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Debugf("failed to create relay server: %v", err)
		return fmt.Errorf("failed to create relay server: %v", err)
	}
	log.Infof("server will be available on: %s", srv.InstanceURL())

	// ADR-0014 multi-pod fabric. When --cluster-headless is empty
	// the relay runs as before (single-pod, drop on local-store
	// miss); set it to the headless Service name and pass POD_IP
	// via the K8s downward API to enable cross-pod forwarding.
	clusterCtx, clusterCancel := context.WithCancel(context.Background())
	defer clusterCancel()
	var clusterBoot *server.ClusterBootstrap
	if cobraConfig.ClusterHeadless != "" {
		clusterBoot, err = server.StartCluster(clusterCtx, srv.Store(), server.ClusterBootstrapConfig{
			Headless:   cobraConfig.ClusterHeadless,
			Port:       cobraConfig.ClusterPort,
			PodIP:      cobraConfig.PodIP,
			AuthSecret: cobraConfig.ClusterAuthSecret,
			Meter:      metricsServer.Meter,
		})
		if err != nil {
			return fmt.Errorf("failed to start relay cluster fabric: %w", err)
		}
		srv.SetCrossPodForwarder(clusterBoot.Forwarder)
		log.Infof("relay cluster fabric enabled — headless=%s, pod-ip=%s, port=%d",
			cobraConfig.ClusterHeadless, cobraConfig.PodIP, cobraConfig.ClusterPort)
	}

	go func() {
		if err := srv.Listen(srvListenerCfg); err != nil {
			log.Fatalf("failed to bind server: %s", err)
		}
	}()

	// it will block until exit signal
	waitForExitSignal()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop accepting new cross-pod traffic before draining peers
	// — the locator's caches go stale on shutdown anyway, but
	// closing inter-pod streams first avoids a flurry of late
	// FWD frames during peer-by-peer drain.
	if clusterBoot != nil {
		clusterBoot.Stop()
	}

	var shutDownErrors error
	if err := srv.Shutdown(ctx); err != nil {
		shutDownErrors = multierror.Append(shutDownErrors, fmt.Errorf("failed to close server: %s", err))
	}

	log.Infof("shutting down metrics server")
	if err := metricsServer.Shutdown(ctx); err != nil {
		shutDownErrors = multierror.Append(shutDownErrors, fmt.Errorf("failed to close metrics server: %v", err))
	}
	return shutDownErrors
}

func handleTLSConfig(cfg *Config) (*tls.Config, bool, error) {
	if cfg.LetsencryptAWSRoute53 {
		log.Debugf("using Let's Encrypt DNS resolver with Route 53 support")
		r53 := encryption.Route53TLS{
			DataDir: cfg.LetsencryptDataDir,
			Email:   cfg.LetsencryptEmail,
			Domains: cfg.LetsencryptDomains,
		}
		tlsCfg, err := r53.GetCertificate()
		if err != nil {
			return nil, false, fmt.Errorf("%s", err)
		}
		return tlsCfg, true, nil
	}

	if cfg.HasLetsEncrypt() {
		log.Infof("setting up TLS with Let's Encrypt.")
		tlsCfg, err := setupTLSCertManager(cfg.LetsencryptDataDir, cfg.LetsencryptDomains...)
		if err != nil {
			return nil, false, fmt.Errorf("%s", err)
		}
		return tlsCfg, true, nil
	}

	if cfg.HasCertConfig() {
		log.Debugf("using file based TLS config")
		tlsCfg, err := encryption.LoadTLSConfig(cfg.TlsCertFile, cfg.TlsKeyFile)
		if err != nil {
			return nil, false, fmt.Errorf("%s", err)
		}
		return tlsCfg, true, nil
	}
	return nil, false, nil
}

func setupTLSCertManager(letsencryptDataDir string, letsencryptDomains ...string) (*tls.Config, error) {
	certManager, err := encryption.CreateCertManager(letsencryptDataDir, letsencryptDomains...)
	if err != nil {
		return nil, fmt.Errorf("failed creating LetsEncrypt cert manager: %v", err)
	}
	return certManager.TLSConfig(), nil
}
