package mdm

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Intune is the Microsoft Intune (Endpoint Manager) provider. It
// looks up devices via Microsoft Graph and reports compliance based
// on the `complianceState` and `managementState` fields.
//
// Authentication is OAuth client_credentials against the operator's
// Microsoft Entra (Azure AD) tenant. The app registration must have
// the Application permission `DeviceManagementManagedDevices.Read.All`
// granted with admin consent — without this the Graph call fails
// 403 and the posture check fails closed.
//
// PHASE 1 (this commit) ships the constructor + interface shape +
// stub returning ErrUnsupported. The real Graph API client follows
// in the next PR — this stub keeps the framework testable end-to-
// end and lets operators see the configuration UI before the wire
// implementation lands.
type Intune struct {
	cfg    IntuneConfig
	client *http.Client
}

// NewIntune constructs the Intune driver. Returns an error if
// required fields are missing.
func NewIntune(cfg IntuneConfig) (*Intune, error) {
	if cfg.TenantID == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("intune: tenant_id, client_id, and client_secret are required")
	}
	return &Intune{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (i *Intune) Type() ProviderType { return TypeIntune }
func (i *Intune) Close() error       { return nil }

// GetDeviceStatus is currently a stub. The real implementation
// follows in PR-G9: it acquires a token via OAuth client_credentials
// against login.microsoftonline.com, then issues
// GET https://graph.microsoft.com/v1.0/deviceManagement/managedDevices?$filter=deviceName%20eq%20'<id>'
// and maps complianceState=compliant → DeviceStatus.Compliant=true.
//
// Until then, returning ErrUnsupported produces a non-compliant
// result with a clear reason — operators see the failure in the
// posture-check UI and know to wait for G9.
func (i *Intune) GetDeviceStatus(_ context.Context, _ string) (DeviceStatus, error) {
	return DeviceStatus{
		Found:     false,
		Compliant: false,
		Reason:    "Intune driver not yet implemented (PR-G9 pending)",
	}, ErrUnsupported
}
