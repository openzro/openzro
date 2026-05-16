package selfupdate

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

// fakeRunner records every command and returns a scripted result per
// command name. Lets the macOS verify/install logic be tested in full
// on a Linux box — only the thin execRunner is Mac-only.
type fakeRunner struct {
	calls  []call
	fail   map[string]error  // command name -> error to return
	output map[string]string // command name -> combined output
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name, args})
	return []byte(f.output[name]), f.fail[name]
}

func (f *fakeRunner) names() []string {
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.name
	}
	return out
}

const pkgutilOut = `Package "openzro.pkg":
   Status: signed by a developer certificate issued by Apple for distribution
   Notarization: trusted by the Apple notary service
   Certificate Chain:
    1. Developer ID Installer: openZro (AB12CD34EF)
       Expires: 2027-01-01
    2. Developer ID Certification Authority
    3. Apple Root CA
`

func TestVerifyMacPkg_S1IdentityPin(t *testing.T) {
	t.Run("empty expected Team ID -> fail-closed, nothing run", func(t *testing.T) {
		f := &fakeRunner{}
		if err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", ""); err == nil {
			t.Fatal("no configured pin must refuse")
		}
		if len(f.calls) != 0 {
			t.Fatalf("must refuse before running anything: %v", f.names())
		}
	})

	t.Run("team id matches + spctl ok -> nil", func(t *testing.T) {
		f := &fakeRunner{output: map[string]string{"pkgutil": pkgutilOut}}
		if err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", "AB12CD34EF"); err != nil {
			t.Fatal(err)
		}
		if strings.Join(f.names(), ",") != "pkgutil,spctl" {
			t.Fatalf("order wrong: %v", f.names())
		}
	})

	t.Run("team id MISMATCH -> refuse, spctl NOT run", func(t *testing.T) {
		f := &fakeRunner{output: map[string]string{"pkgutil": pkgutilOut}}
		err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", "ZZZZZZZZZZ")
		if err == nil {
			t.Fatal("a different developer's notarized pkg must be refused")
		}
		if len(f.calls) != 1 {
			t.Fatalf("must not assess a wrong-identity pkg: %v", f.names())
		}
	})

	t.Run("no identity in pkgutil output -> refuse", func(t *testing.T) {
		f := &fakeRunner{output: map[string]string{"pkgutil": "Status: no signature"}}
		if err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", "AB12CD34EF"); err == nil {
			t.Fatal("unparseable identity must refuse")
		}
	})

	t.Run("pkgutil exits non-zero -> refuse, spctl NOT run", func(t *testing.T) {
		f := &fakeRunner{fail: map[string]error{"pkgutil": fmt.Errorf("no signature")}}
		if err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", "AB12CD34EF"); err == nil {
			t.Fatal("expected refusal on unsigned package")
		}
		if len(f.calls) != 1 {
			t.Fatalf("spctl must not run after pkgutil fails: %v", f.names())
		}
	})

	t.Run("identity ok but spctl fails -> refuse (not notarized / revoked)", func(t *testing.T) {
		f := &fakeRunner{
			output: map[string]string{"pkgutil": pkgutilOut},
			fail:   map[string]error{"spctl": fmt.Errorf("rejected")},
		}
		if err := verifyMacPkg(context.Background(), f.run, "/tmp/x.pkg", "AB12CD34EF"); err == nil {
			t.Fatal("expected refusal when Gatekeeper rejects")
		}
	})
}

func TestInstallMacPkg(t *testing.T) {
	t.Run("installer ok, no label -> nil, no launchctl", func(t *testing.T) {
		f := &fakeRunner{}
		if err := installMacPkg(context.Background(), f.run, "/tmp/x.pkg", ""); err != nil {
			t.Fatal(err)
		}
		if strings.Join(f.names(), ",") != "installer" {
			t.Fatalf("only installer expected: %v", f.names())
		}
		if got := strings.Join(f.calls[0].args, " "); got != "-pkg /tmp/x.pkg -target /" {
			t.Fatalf("installer args: %q", got)
		}
	})

	t.Run("installer fails -> error AND no restart attempted", func(t *testing.T) {
		f := &fakeRunner{fail: map[string]error{"installer": fmt.Errorf("boom")}}
		if err := installMacPkg(context.Background(), f.run, "/tmp/x.pkg", "io.openzro.daemon"); err == nil {
			t.Fatal("expected install error")
		}
		if len(f.calls) != 1 {
			t.Fatalf("daemon must NOT be bounced onto a failed install: %v", f.names())
		}
	})

	t.Run("with label -> launchctl kickstart after installer", func(t *testing.T) {
		f := &fakeRunner{}
		if err := installMacPkg(context.Background(), f.run, "/tmp/x.pkg", "io.openzro.daemon"); err != nil {
			t.Fatal(err)
		}
		if strings.Join(f.names(), ",") != "installer,launchctl" {
			t.Fatalf("expected installer then launchctl: %v", f.names())
		}
		if got := strings.Join(f.calls[1].args, " "); got != "kickstart -k system/io.openzro.daemon" {
			t.Fatalf("launchctl args: %q", got)
		}
	})

	t.Run("label kickstart failing is best-effort (not fatal)", func(t *testing.T) {
		f := &fakeRunner{fail: map[string]error{"launchctl": fmt.Errorf("no such service")}}
		if err := installMacPkg(context.Background(), f.run, "/tmp/x.pkg", "io.openzro.daemon"); err != nil {
			t.Fatalf("kickstart failure must not fail the install (PKG postinstall is primary): %v", err)
		}
	})
}
