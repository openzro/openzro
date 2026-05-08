package cmd

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/version"
)

const (
	// ExitSetupFailed defines exit code
	ExitSetupFailed                  = 1
	idpSignKeyRefreshEnabledFlagName = "idp-sign-key-refresh-enabled"
)

var (
	dnsDomain                string
	mgmtDataDir              string
	logLevel                 string
	logFile                  string
	disableMetrics           bool
	disableSingleAccMode     bool
	disableGeoliteUpdate     bool
	maxmindLicenseKey        string
	idpSignKeyRefreshEnabled bool
	userDeleteFromIDPEnabled bool

	rootCmd = &cobra.Command{
		Use:          "openzro-mgmt",
		Short:        "",
		Long:         "",
		Version:      version.OpenzroVersion(),
		SilenceUsage: true,
	}

	migrationCmd = &cobra.Command{
		Use:          "sqlite-migration",
		Short:        "Contains sub-commands to perform JSON file store to SQLite store migration and rollback",
		Long:         "",
		SilenceUsage: true,
	}
	// Execution control channel for stopCh signal
	stopCh chan int
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	stopCh = make(chan int)
	mgmtCmd.Flags().IntVar(&mgmtPort, "port", 80, "server port to listen on (defaults to 443 if TLS is enabled, 80 otherwise")
	mgmtCmd.Flags().IntVar(&mgmtMetricsPort, "metrics-port", 9090, "metrics endpoint http port. Metrics are accessible under host:metrics-port/metrics")
	mgmtCmd.Flags().StringVar(&mgmtDataDir, "datadir", defaultMgmtDataDir, "server data directory location")
	mgmtCmd.Flags().StringVar(&types.MgmtConfigPath, "config", defaultMgmtConfig, "Openzro config file location. Config params specified via command line (e.g. datadir) have a precedence over configuration from this file")
	mgmtCmd.Flags().StringVar(&mgmtLetsencryptDomain, "letsencrypt-domain", "", "a domain to issue Let's Encrypt certificate for. Enables TLS using Let's Encrypt. Will fetch and renew certificate, and run the server with TLS")
	mgmtCmd.Flags().StringVar(&mgmtSingleAccModeDomain, "single-account-mode-domain", defaultSingleAccModeDomain, "Enables single account mode. This means that all the users will be under the same account grouped by the specified domain. If the installation has more than one account, the property is ineffective. Enabled by default with the default domain "+defaultSingleAccModeDomain)
	mgmtCmd.Flags().BoolVar(&disableSingleAccMode, "disable-single-account-mode", false, "If set to true, disables single account mode. The --single-account-mode-domain property will be ignored and every new user will have a separate Openzro account.")
	mgmtCmd.Flags().StringVar(&certFile, "cert-file", "", "Location of your SSL certificate. Can be used when you have an existing certificate and don't want a new certificate be generated automatically. If letsencrypt-domain is specified this property has no effect")
	mgmtCmd.Flags().StringVar(&certKey, "cert-key", "", "Location of your SSL certificate private key. Can be used when you have an existing certificate and don't want a new certificate be generated automatically. If letsencrypt-domain is specified this property has no effect")
	mgmtCmd.Flags().BoolVar(&disableMetrics, "disable-anonymous-metrics", false, "disables push of anonymous usage metrics to Openzro")
	mgmtCmd.Flags().StringVar(&dnsDomain, "dns-domain", defaultSingleAccModeDomain, fmt.Sprintf("Domain used for peer resolution. This is appended to the peer's name, e.g. pi-server. %s. Max length is 192 characters to allow appending to a peer name with up to 63 characters.", defaultSingleAccModeDomain))
	mgmtCmd.Flags().BoolVar(&idpSignKeyRefreshEnabled, idpSignKeyRefreshEnabledFlagName, false, "Enable cache headers evaluation to determine signing key rotation period. This will refresh the signing key upon expiry.")
	mgmtCmd.Flags().BoolVar(&userDeleteFromIDPEnabled, "user-delete-from-idp", false, "Allows to delete user from IDP when user is deleted from account")
	mgmtCmd.Flags().BoolVar(&disableGeoliteUpdate, "disable-geolite-update", false, "disables automatic updates to the GeoLite2 geolocation databases. When false (default) management fetches from the openZro GeoLite2 mirror (github.com/openzro/geolocation-dbs) on cold boot — set --maxmind-license-key to fetch directly from MaxMind instead, or set this to true on air-gapped installs that stage their own mmdb")
	mgmtCmd.Flags().StringVar(&maxmindLicenseKey, "maxmind-license-key", "", "MaxMind GeoLite2 license key (free at https://www.maxmind.com/en/geolite2/signup). When set, management bypasses the openZro mirror and fetches GeoLite2 directly from download.maxmind.com")
	rootCmd.MarkFlagRequired("config") //nolint

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", defaultLogFile, "sets Openzro log path. If console is specified the log will be output to stdout")
	rootCmd.AddCommand(mgmtCmd)

	migrationCmd.PersistentFlags().StringVar(&mgmtDataDir, "datadir", defaultMgmtDataDir, "server data directory location")
	migrationCmd.MarkFlagRequired("datadir") //nolint

	migrationCmd.AddCommand(upCmd)

	rootCmd.AddCommand(migrationCmd)
}

// SetupCloseHandler handles SIGTERM signal and exits with success
func SetupCloseHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			fmt.Println("\r- Ctrl+C pressed in Terminal")
			stopCh <- 0
		}
	}()
}
