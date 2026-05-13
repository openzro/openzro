package posture

import (
	"context"
	"errors"
	"net/netip"
	"regexp"

	"github.com/hashicorp/go-version"
	"github.com/openzro/openzro/management/server/http/api"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
)

const (
	NBVersionCheckName        = "NBVersionCheck"
	OSVersionCheckName        = "OSVersionCheck"
	GeoLocationCheckName      = "GeoLocationCheck"
	PeerNetworkRangeCheckName = "PeerNetworkRangeCheck"
	ProcessCheckName          = "ProcessCheck"
	EndpointSecurityCheckName = "EndpointSecurityCheck"
	ScheduleCheckName         = "ScheduleCheck"

	CheckActionAllow string = "allow"
	CheckActionDeny  string = "deny"
)

var (
	countryCodeRegex = regexp.MustCompile("^[a-zA-Z]{2}$")
)

// Check represents an interface for performing a check on a peer.
type Check interface {
	Name() string
	Check(ctx context.Context, peer nbpeer.Peer) (bool, error)
	Validate() error
}

type Checks struct {
	// ID of the posture checks
	ID string `gorm:"primaryKey"`

	// Name of the posture checks
	Name string

	// Description of the posture checks visible in the UI
	Description string

	// AccountID is a reference to the Account that this object belongs
	AccountID string `json:"-" gorm:"index"`

	// Checks is a set of objects that perform the actual checks
	Checks ChecksDefinition `gorm:"serializer:json"`
}

// ChecksDefinition contains definition of actual check
type ChecksDefinition struct {
	NBVersionCheck        *NBVersionCheck        `json:",omitempty"`
	OSVersionCheck        *OSVersionCheck        `json:",omitempty"`
	GeoLocationCheck      *GeoLocationCheck      `json:",omitempty"`
	PeerNetworkRangeCheck *PeerNetworkRangeCheck `json:",omitempty"`
	ProcessCheck          *ProcessCheck          `json:",omitempty"`
	// EndpointSecurityCheck delegates compliance to a configured
	// MDM/EDR vendor (Intune / SentinelOne / Huntress). See
	// endpoint_security.go.
	EndpointSecurityCheck *EndpointSecurityCheck `json:",omitempty"`
	// ScheduleCheck gates peer access by wall-clock time (single
	// window of day-of-week + HH:MM range, interpreted in an IANA
	// timezone, evaluated server-side). See schedule.go.
	ScheduleCheck *ScheduleCheck `json:",omitempty"`
}

// Copy returns a copy of a checks definition.
func (cd ChecksDefinition) Copy() ChecksDefinition {
	var cdCopy ChecksDefinition
	if cd.NBVersionCheck != nil {
		cdCopy.NBVersionCheck = &NBVersionCheck{
			MinVersion: cd.NBVersionCheck.MinVersion,
		}
	}
	if cd.OSVersionCheck != nil {
		cdCopy.OSVersionCheck = &OSVersionCheck{}
		osCheck := cd.OSVersionCheck
		if osCheck.Android != nil {
			cdCopy.OSVersionCheck.Android = &MinVersionCheck{MinVersion: osCheck.Android.MinVersion}
		}
		if osCheck.Darwin != nil {
			cdCopy.OSVersionCheck.Darwin = &MinVersionCheck{MinVersion: osCheck.Darwin.MinVersion}
		}
		if osCheck.Ios != nil {
			cdCopy.OSVersionCheck.Ios = &MinVersionCheck{MinVersion: osCheck.Ios.MinVersion}
		}
		if osCheck.Linux != nil {
			cdCopy.OSVersionCheck.Linux = &MinKernelVersionCheck{MinKernelVersion: osCheck.Linux.MinKernelVersion}
		}
		if osCheck.Windows != nil {
			cdCopy.OSVersionCheck.Windows = &MinKernelVersionCheck{MinKernelVersion: osCheck.Windows.MinKernelVersion}
		}
	}
	if cd.GeoLocationCheck != nil {
		geoCheck := cd.GeoLocationCheck
		cdCopy.GeoLocationCheck = &GeoLocationCheck{
			Action:    geoCheck.Action,
			Locations: make([]Location, len(geoCheck.Locations)),
		}
		copy(cdCopy.GeoLocationCheck.Locations, geoCheck.Locations)
	}
	if cd.PeerNetworkRangeCheck != nil {
		peerNetRangeCheck := cd.PeerNetworkRangeCheck
		cdCopy.PeerNetworkRangeCheck = &PeerNetworkRangeCheck{
			Action: peerNetRangeCheck.Action,
			Ranges: make([]netip.Prefix, len(peerNetRangeCheck.Ranges)),
		}
		copy(cdCopy.PeerNetworkRangeCheck.Ranges, peerNetRangeCheck.Ranges)
	}
	if cd.ProcessCheck != nil {
		processCheck := cd.ProcessCheck
		cdCopy.ProcessCheck = &ProcessCheck{
			Processes: make([]Process, len(processCheck.Processes)),
		}
		copy(cdCopy.ProcessCheck.Processes, processCheck.Processes)
	}
	if cd.EndpointSecurityCheck != nil {
		cdCopy.EndpointSecurityCheck = &EndpointSecurityCheck{
			ProviderID: cd.EndpointSecurityCheck.ProviderID,
			FailOpen:   cd.EndpointSecurityCheck.FailOpen,
		}
	}
	if cd.ScheduleCheck != nil {
		src := cd.ScheduleCheck
		schedCopy := &ScheduleCheck{
			Timezone: src.Timezone,
			Action:   src.Action,
			Window: TimeWindow{
				StartTime:  src.Window.StartTime,
				EndTime:    src.Window.EndTime,
				DaysOfWeek: append([]int(nil), src.Window.DaysOfWeek...),
			},
		}
		cdCopy.ScheduleCheck = schedCopy
	}
	return cdCopy
}

// TableName returns the name of the table for the Checks model in the database.
func (*Checks) TableName() string {
	return "posture_checks"
}

// Copy returns a copy of a posture checks.
func (pc *Checks) Copy() *Checks {
	checks := &Checks{
		ID:          pc.ID,
		Name:        pc.Name,
		Description: pc.Description,
		AccountID:   pc.AccountID,
		Checks:      pc.Checks.Copy(),
	}
	return checks
}

// EventMeta returns activity event meta-related to this posture checks.
func (pc *Checks) EventMeta() map[string]any {
	return map[string]any{"name": pc.Name}
}

// GetChecks returns list of all initialized checks definitions
func (pc *Checks) GetChecks() []Check {
	var checks []Check
	if pc.Checks.NBVersionCheck != nil {
		checks = append(checks, pc.Checks.NBVersionCheck)
	}
	if pc.Checks.OSVersionCheck != nil {
		checks = append(checks, pc.Checks.OSVersionCheck)
	}
	if pc.Checks.GeoLocationCheck != nil {
		checks = append(checks, pc.Checks.GeoLocationCheck)
	}
	if pc.Checks.PeerNetworkRangeCheck != nil {
		checks = append(checks, pc.Checks.PeerNetworkRangeCheck)
	}
	if pc.Checks.ProcessCheck != nil {
		checks = append(checks, pc.Checks.ProcessCheck)
	}
	if pc.Checks.EndpointSecurityCheck != nil {
		checks = append(checks, pc.Checks.EndpointSecurityCheck)
	}
	if pc.Checks.ScheduleCheck != nil {
		checks = append(checks, pc.Checks.ScheduleCheck)
	}
	return checks
}

func NewChecksFromAPIPostureCheck(source api.PostureCheck) (*Checks, error) {
	description := ""
	if source.Description != nil {
		description = *source.Description
	}

	return buildPostureCheck(source.Id, source.Name, description, source.Checks)
}

func NewChecksFromAPIPostureCheckUpdate(source api.PostureCheckUpdate, postureChecksID string) (*Checks, error) {
	return buildPostureCheck(postureChecksID, source.Name, source.Description, *source.Checks)
}

func buildPostureCheck(postureChecksID string, name string, description string, checks api.Checks) (*Checks, error) {
	postureChecks := Checks{
		ID:          postureChecksID,
		Name:        name,
		Description: description,
	}

	if nbVersionCheck := checks.NbVersionCheck; nbVersionCheck != nil {
		postureChecks.Checks.NBVersionCheck = &NBVersionCheck{
			MinVersion: nbVersionCheck.MinVersion,
		}
	}

	if osVersionCheck := checks.OsVersionCheck; osVersionCheck != nil {
		postureChecks.Checks.OSVersionCheck = &OSVersionCheck{
			Android: (*MinVersionCheck)(osVersionCheck.Android),
			Darwin:  (*MinVersionCheck)(osVersionCheck.Darwin),
			Ios:     (*MinVersionCheck)(osVersionCheck.Ios),
			Linux:   (*MinKernelVersionCheck)(osVersionCheck.Linux),
			Windows: (*MinKernelVersionCheck)(osVersionCheck.Windows),
		}
	}

	if geoLocationCheck := checks.GeoLocationCheck; geoLocationCheck != nil {
		postureChecks.Checks.GeoLocationCheck = toPostureGeoLocationCheck(geoLocationCheck)
	}

	var err error
	if peerNetworkRangeCheck := checks.PeerNetworkRangeCheck; peerNetworkRangeCheck != nil {
		postureChecks.Checks.PeerNetworkRangeCheck, err = toPeerNetworkRangeCheck(peerNetworkRangeCheck)
		if err != nil {
			return nil, status.Errorf(status.InvalidArgument, "invalid network prefix")
		}
	}

	if processCheck := checks.ProcessCheck; processCheck != nil {
		postureChecks.Checks.ProcessCheck = toProcessCheck(processCheck)
	}

	if endpointSecurityCheck := checks.EndpointSecurityCheck; endpointSecurityCheck != nil {
		postureChecks.Checks.EndpointSecurityCheck = toEndpointSecurityCheck(endpointSecurityCheck)
	}

	if scheduleCheck := checks.ScheduleCheck; scheduleCheck != nil {
		postureChecks.Checks.ScheduleCheck = toScheduleCheck(scheduleCheck)
	}

	return &postureChecks, nil
}

func (pc *Checks) ToAPIResponse() *api.PostureCheck {
	var checks api.Checks

	if pc.Checks.NBVersionCheck != nil {
		checks.NbVersionCheck = &api.NBVersionCheck{
			MinVersion: pc.Checks.NBVersionCheck.MinVersion,
		}
	}

	if pc.Checks.OSVersionCheck != nil {
		checks.OsVersionCheck = &api.OSVersionCheck{
			Android: (*api.MinVersionCheck)(pc.Checks.OSVersionCheck.Android),
			Darwin:  (*api.MinVersionCheck)(pc.Checks.OSVersionCheck.Darwin),
			Ios:     (*api.MinVersionCheck)(pc.Checks.OSVersionCheck.Ios),
			Linux:   (*api.MinKernelVersionCheck)(pc.Checks.OSVersionCheck.Linux),
			Windows: (*api.MinKernelVersionCheck)(pc.Checks.OSVersionCheck.Windows),
		}
	}

	if pc.Checks.GeoLocationCheck != nil {
		checks.GeoLocationCheck = toGeoLocationCheckResponse(pc.Checks.GeoLocationCheck)
	}

	if pc.Checks.PeerNetworkRangeCheck != nil {
		checks.PeerNetworkRangeCheck = toPeerNetworkRangeCheckResponse(pc.Checks.PeerNetworkRangeCheck)
	}

	if pc.Checks.ProcessCheck != nil {
		checks.ProcessCheck = toProcessCheckResponse(pc.Checks.ProcessCheck)
	}

	if pc.Checks.EndpointSecurityCheck != nil {
		checks.EndpointSecurityCheck = toEndpointSecurityCheckResponse(pc.Checks.EndpointSecurityCheck)
	}

	if pc.Checks.ScheduleCheck != nil {
		checks.ScheduleCheck = toScheduleCheckResponse(pc.Checks.ScheduleCheck)
	}

	return &api.PostureCheck{
		Id:          pc.ID,
		Name:        pc.Name,
		Description: &pc.Description,
		Checks:      checks,
	}
}

func toEndpointSecurityCheck(check *api.EndpointSecurityCheck) *EndpointSecurityCheck {
	failOpen := false
	if check.FailOpen != nil {
		failOpen = *check.FailOpen
	}
	return &EndpointSecurityCheck{
		ProviderID: check.ProviderId,
		FailOpen:   failOpen,
	}
}

func toEndpointSecurityCheckResponse(check *EndpointSecurityCheck) *api.EndpointSecurityCheck {
	failOpen := check.FailOpen
	return &api.EndpointSecurityCheck{
		ProviderId: check.ProviderID,
		FailOpen:   &failOpen,
	}
}

func toScheduleCheck(check *api.ScheduleCheck) *ScheduleCheck {
	tz := ""
	if check.Timezone != nil {
		tz = *check.Timezone
	}
	var days []int
	if check.Window.DaysOfWeek != nil {
		days = append([]int(nil), *check.Window.DaysOfWeek...)
	}
	return &ScheduleCheck{
		Window: TimeWindow{
			DaysOfWeek: days,
			StartTime:  check.Window.StartTime,
			EndTime:    check.Window.EndTime,
		},
		Timezone: tz,
		Action:   string(check.Action),
	}
}

func toScheduleCheckResponse(check *ScheduleCheck) *api.ScheduleCheck {
	var daysPtr *[]int
	if len(check.Window.DaysOfWeek) > 0 {
		days := append([]int(nil), check.Window.DaysOfWeek...)
		daysPtr = &days
	}
	tz := check.Timezone
	var tzPtr *string
	if tz != "" {
		tzPtr = &tz
	}
	return &api.ScheduleCheck{
		Window: api.TimeWindow{
			DaysOfWeek: daysPtr,
			StartTime:  check.Window.StartTime,
			EndTime:    check.Window.EndTime,
		},
		Timezone: tzPtr,
		Action:   api.ScheduleCheckAction(check.Action),
	}
}

// Validate checks the validity of a posture checks.
func (pc *Checks) Validate() error {
	if pc.Name == "" {
		return errors.New("posture checks name shouldn't be empty")
	}

	checks := pc.GetChecks()
	if len(checks) == 0 {
		return errors.New("posture checks shouldn't be empty")
	}

	for _, check := range checks {
		if err := check.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func isVersionValid(ver string) bool {
	newVersion, err := version.NewVersion(ver)
	if err != nil {
		return false
	}

	if newVersion != nil {
		return true
	}

	return false
}

func toGeoLocationCheckResponse(geoLocationCheck *GeoLocationCheck) *api.GeoLocationCheck {
	locations := make([]api.Location, 0, len(geoLocationCheck.Locations))
	for _, loc := range geoLocationCheck.Locations {
		l := loc // make G601 happy
		var cityName *string
		if loc.CityName != "" {
			cityName = &l.CityName
		}
		locations = append(locations, api.Location{
			CityName:    cityName,
			CountryCode: loc.CountryCode,
		})
	}

	return &api.GeoLocationCheck{
		Action:    api.GeoLocationCheckAction(geoLocationCheck.Action),
		Locations: locations,
	}
}

func toPostureGeoLocationCheck(apiGeoLocationCheck *api.GeoLocationCheck) *GeoLocationCheck {
	locations := make([]Location, 0, len(apiGeoLocationCheck.Locations))
	for _, loc := range apiGeoLocationCheck.Locations {
		cityName := ""
		if loc.CityName != nil {
			cityName = *loc.CityName
		}
		locations = append(locations, Location{
			CountryCode: loc.CountryCode,
			CityName:    cityName,
		})
	}

	return &GeoLocationCheck{
		Action:    string(apiGeoLocationCheck.Action),
		Locations: locations,
	}
}

func toPeerNetworkRangeCheckResponse(check *PeerNetworkRangeCheck) *api.PeerNetworkRangeCheck {
	netPrefixes := make([]string, 0, len(check.Ranges))
	for _, netPrefix := range check.Ranges {
		netPrefixes = append(netPrefixes, netPrefix.String())
	}

	return &api.PeerNetworkRangeCheck{
		Ranges: netPrefixes,
		Action: api.PeerNetworkRangeCheckAction(check.Action),
	}
}

func toPeerNetworkRangeCheck(check *api.PeerNetworkRangeCheck) (*PeerNetworkRangeCheck, error) {
	prefixes := make([]netip.Prefix, 0)
	for _, prefix := range check.Ranges {
		parsedPrefix, err := netip.ParsePrefix(prefix)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, parsedPrefix)
	}

	return &PeerNetworkRangeCheck{
		Ranges: prefixes,
		Action: string(check.Action),
	}, nil
}

func toProcessCheckResponse(check *ProcessCheck) *api.ProcessCheck {
	processes := make([]api.Process, 0, len(check.Processes))
	for i := range check.Processes {
		processes = append(processes, api.Process{
			LinuxPath:   &check.Processes[i].LinuxPath,
			MacPath:     &check.Processes[i].MacPath,
			WindowsPath: &check.Processes[i].WindowsPath,
		})
	}

	return &api.ProcessCheck{
		Processes: processes,
	}
}

func toProcessCheck(check *api.ProcessCheck) *ProcessCheck {
	processes := make([]Process, 0, len(check.Processes))
	for _, process := range check.Processes {
		var p Process
		if process.LinuxPath != nil {
			p.LinuxPath = *process.LinuxPath
		}
		if process.MacPath != nil {
			p.MacPath = *process.MacPath
		}
		if process.WindowsPath != nil {
			p.WindowsPath = *process.WindowsPath
		}

		processes = append(processes, p)
	}

	return &ProcessCheck{
		Processes: processes,
	}
}
