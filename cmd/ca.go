package cmd

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed assets/ca-install-linux.sh.tmpl
var linuxCAInstallScript string

//go:embed assets/ca-install-darwin.sh.tmpl
var darwinCAInstallScript string

var systemCAVerifier = verifySystemCA

// fetchMITMCA requests the transparent-proxy root CA from the local server.
// Returns (pem, port, true, nil) on 200 where port is the MITM listener
// port advertised by the server (0 if the server omitted the header, e.g.
// an older build — callers should fall back to DefaultMITMPort in that
// case). Returns (nil, 0, false, nil) on 404 (MITM disabled), or an error
// for any other failure. Body is always drained before returning so the
// underlying connection can be pooled.
func fetchMITMCA(addr string) (pem []byte, port int, enabled bool, err error) {
	resp, err := httpClient.Get(addr + "/v1/mitm/ca.pem")
	if err != nil {
		return nil, 0, false, fmt.Errorf("could not reach server at %s: %w", addr, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, false, fmt.Errorf("reading response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		port := 0
		if raw := resp.Header.Get("X-MITM-Port"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < 65536 {
				port = n
			}
		}
		return body, port, true, nil
	case http.StatusNotFound:
		return nil, 0, false, nil
	default:
		return nil, 0, false, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

var caCmd = &cobra.Command{
	Use:   "ca",
	Short: "Manage the transparent-proxy root CA certificate",
}

var caFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch the root CA certificate (PEM)",
	Long: `Fetch the root CA certificate used by Agent Vault's transparent MITM
proxy. Install the returned PEM into your client trust store so HTTPS
traffic routed through the proxy validates cleanly.

The transparent proxy is enabled by default. The endpoint is public —
no authentication required. If the server was started with --mitm-port 0,
this command returns an error.

Examples:
  agent-vault ca fetch > ca.pem
  agent-vault ca fetch -o agent-vault-ca.pem
  agent-vault ca install-script > agent-vault-ca-install.sh`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		addr := resolveAddress(cmd)
		output, _ := cmd.Flags().GetString("output")

		pem, _, enabled, err := fetchMITMCA(addr)
		if err != nil {
			return err
		}
		if !enabled {
			return errors.New("MITM proxy is not enabled on this server")
		}

		if output != "" {
			if err := os.WriteFile(output, pem, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", output, err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s Wrote CA cert to %s\n", successText("✓"), output)
			return nil
		}
		_, _ = cmd.OutOrStdout().Write(pem)
		return nil
	},
}

var caVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify that the Agent Vault CA is trusted by the native system store",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		addr := resolveAddress(cmd)
		caPath, _ := cmd.Flags().GetString("file")
		var certPEM []byte
		if caPath != "" {
			var err error
			certPEM, err = os.ReadFile(caPath)
			if err != nil {
				return fmt.Errorf("reading %s: %w", caPath, err)
			}
		} else {
			pemBytes, _, enabled, err := fetchMITMCA(addr)
			if err != nil {
				return err
			}
			if !enabled {
				return errors.New("MITM proxy is not enabled on this server")
			}
			certPEM = pemBytes
			file, err := os.CreateTemp("", "agent-vault-ca-*.pem")
			if err != nil {
				return fmt.Errorf("creating temporary CA file: %w", err)
			}
			caPath = file.Name()
			defer func() { _ = os.Remove(caPath) }()
			if err := file.Chmod(0o600); err != nil {
				_ = file.Close()
				return err
			}
			if _, err := file.Write(certPEM); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}

		if err := systemCAVerifier(certPEM, caPath); err != nil {
			return fmt.Errorf("the Agent Vault CA is not trusted by the native system store: %w", err)
		}
		return nil
	},
}

var caInstallScriptCmd = &cobra.Command{
	Use:   "install-script",
	Short: "Print a reviewable native CA installation script",
	Long: `Fetch the current Agent Vault root CA and print a platform-native shell
script that can install, verify, or remove that exact certificate. The command
does not execute the script, modify the trust store, or elevate privileges.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		addr := resolveAddress(cmd)
		certPEM, _, enabled, err := fetchMITMCA(addr)
		if err != nil {
			return err
		}
		if !enabled {
			return errors.New("MITM proxy is not enabled on this server")
		}
		script, err := renderCAInstallScript(runtime.GOOS, certPEM)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Review the generated script before running it; this command has not changed your trust store.")
		_, err = io.WriteString(cmd.OutOrStdout(), script)
		return err
	},
}

func parseCACertificate(certPEM []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid CA certificate PEM")
	}
	if len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("CA PEM must contain exactly one certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, errors.New("certificate is not a valid CA")
	}
	return cert, nil
}

func verifySystemCA(certPEM []byte, caPath string) error {
	cert, err := parseCACertificate(certPEM)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "linux":
		dir, err := os.MkdirTemp("", "agent-vault-system-ca-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		bundle := filepath.Join(dir, "anchors.pem")
		command := exec.Command("trust", "extract", "--filter=ca-anchors", "--format=pem-bundle", "--purpose=server-auth", "--overwrite", bundle)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("p11-kit trust extract failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		anchors, err := os.ReadFile(bundle)
		if err != nil {
			return fmt.Errorf("reading p11-kit trust bundle: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(anchors) {
			return errors.New("p11-kit returned an empty CA anchor bundle")
		}
		chains, err := cert.Verify(x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
		if err != nil {
			return err
		}
		for _, chain := range chains {
			if len(chain) > 0 && bytes.Equal(chain[len(chain)-1].Raw, cert.Raw) {
				return nil
			}
		}
		return errors.New("p11-kit trust policy did not contain the exact CA fingerprint")
	case "darwin":
		command := exec.Command("security", "verify-cert", "-q", "-c", caPath, "-p", "ssl")
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("keychain trust verification failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	default:
		return fmt.Errorf("system CA verification is unsupported on %s; use --isolation=container", runtime.GOOS)
	}
}

func renderCAInstallScript(goos string, certPEM []byte) (string, error) {
	cert, err := parseCACertificate(certPEM)
	if err != nil {
		return "", err
	}
	var template string
	switch goos {
	case "linux":
		template = linuxCAInstallScript
	case "darwin":
		template = darwinCAInstallScript
	default:
		return "", fmt.Errorf("CA installation scripts are unsupported on %s", goos)
	}
	sum := sha256.Sum256(cert.Raw)
	fingerprint := strings.ToUpper(hex.EncodeToString(sum[:]))
	script := strings.NewReplacer(
		"{{CA_PEM}}", strings.TrimSpace(string(certPEM)),
		"{{CA_SHA256}}", fingerprint,
	).Replace(template)
	if !strings.HasSuffix(script, "\n") {
		script += "\n"
	}
	return script, nil
}

func init() {
	caFetchCmd.Flags().StringP("output", "o", "", "write PEM to file instead of stdout")
	caFetchCmd.Flags().String("address", "", "server address (default: auto-detect)")
	caVerifyCmd.Flags().String("file", "", "verify this CA PEM instead of fetching it")
	caVerifyCmd.Flags().String("address", "", "server address (default: auto-detect)")
	caInstallScriptCmd.Flags().String("address", "", "server address (default: auto-detect)")
	caCmd.AddCommand(caFetchCmd, caVerifyCmd, caInstallScriptCmd)
	rootCmd.AddCommand(caCmd)
}
