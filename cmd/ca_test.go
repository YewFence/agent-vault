package cmd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type caRoundTripFunc func(*http.Request) (*http.Response, error)

func (f caRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Agent Vault Test Root CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestRenderCAInstallScript(t *testing.T) {
	certPEM := testCAPEM(t)
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			script, err := renderCAInstallScript(goos, certPEM)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(script, "#!/bin/sh\n") || !strings.Contains(script, strings.TrimSpace(string(certPEM))) {
				t.Fatal("rendered script must contain only a shell script with the fetched CA embedded")
			}
			if strings.Contains(script, "{{CA_") {
				t.Fatal("rendered script contains an unresolved template marker")
			}
			check := exec.Command("sh", "-n")
			check.Stdin = strings.NewReader(script)
			if output, err := check.CombinedOutput(); err != nil {
				t.Fatalf("generated script syntax: %v\n%s", err, output)
			}
			if goos == "linux" {
				for _, required := range []string{
					"trust anchor --store",
					"trust extract-compat",
					"trust extract --filter=ca-anchors --format=pem-bundle --purpose=server-auth",
				} {
					if !strings.Contains(script, required) {
						t.Errorf("Linux script missing %q", required)
					}
				}
				if strings.Contains(script, "/etc/os-release") {
					t.Error("Linux script must use the p11-kit capability contract, not distro detection")
				}
			} else {
				for _, required := range []string{"install-login", "install-system", "login.keychain-db", "System.keychain"} {
					if !strings.Contains(script, required) {
						t.Errorf("macOS script missing %q", required)
					}
				}
				if strings.Contains(script, "find-certificate -a -p") {
					t.Error("macOS script must not export Keychain as a PEM bundle")
				}
			}
		})
	}
}

func TestRenderCAInstallScriptRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := renderCAInstallScript("windows", testCAPEM(t)); err == nil {
		t.Fatal("expected unsupported platform error")
	}
}

func TestCAInstallScriptWritesOnlyScriptToStdout(t *testing.T) {
	certPEM := testCAPEM(t)
	previousClient := httpClient
	httpClient = &http.Client{Transport: caRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/mitm/ca.pem" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(certPEM)),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() {
		httpClient = previousClient
		_ = caInstallScriptCmd.Flags().Set("address", "")
	})

	var stdout, stderr bytes.Buffer
	caInstallScriptCmd.SetOut(&stdout)
	caInstallScriptCmd.SetErr(&stderr)
	if err := caInstallScriptCmd.Flags().Set("address", "http://vault.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := caInstallScriptCmd.RunE(caInstallScriptCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "#!/bin/sh\n") {
		t.Fatalf("stdout does not start with a shell script: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "Review the generated script") {
		t.Fatal("human guidance leaked into stdout")
	}
	if !strings.Contains(stderr.String(), "Review the generated script") {
		t.Fatalf("stderr missing review warning: %q", stderr.String())
	}
}

func TestCAVerifyUsesNativeVerifier(t *testing.T) {
	certPEM := testCAPEM(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	previous := systemCAVerifier
	called := false
	systemCAVerifier = func(gotPEM []byte, gotPath string) error {
		called = true
		if string(gotPEM) != string(certPEM) || gotPath != caPath {
			t.Fatalf("verifier received unexpected CA input")
		}
		return nil
	}
	t.Cleanup(func() {
		systemCAVerifier = previous
		_ = caVerifyCmd.Flags().Set("file", "")
	})

	if err := caVerifyCmd.Flags().Set("file", caPath); err != nil {
		t.Fatal(err)
	}
	if err := caVerifyCmd.RunE(caVerifyCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("native verifier was not called")
	}
}

func TestVerifySystemCALinuxExtractsServerAuthAnchors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux p11-kit verification only")
	}

	certPEM := testCAPEM(t)
	tempDir := t.TempDir()
	caPath := filepath.Join(tempDir, "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	trustPath := filepath.Join(tempDir, "trust")
	trustScript := `#!/bin/sh
set -eu
if [ "$#" -ne 6 ] ||
	[ "$1" != "extract" ] ||
	[ "$2" != "--filter=ca-anchors" ] ||
	[ "$3" != "--format=pem-bundle" ] ||
	[ "$4" != "--purpose=server-auth" ] ||
	[ "$5" != "--overwrite" ]; then
	printf 'unexpected trust arguments:' >&2
	printf ' <%s>' "$@" >&2
	printf '\n' >&2
	exit 1
fi
cp "$EXPECTED_CA" "$6"
`
	if err := os.WriteFile(trustPath, []byte(trustScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_CA", caPath)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := verifySystemCA(certPEM, caPath); err != nil {
		t.Fatal(err)
	}
}
