package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateDataToSQLite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src, err := Open(filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	dst, err := Open(filepath.Join(root, "destination.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := src.CreateVault(ctx, "source-vault"); err != nil {
		t.Fatal(err)
	}
	if err := src.InsertRequestLogs(ctx, []RequestLog{{
		VaultID:   defaultVaultID,
		Ingress:   "explicit",
		Method:    "GET",
		Host:      "example.test",
		Path:      "/health",
		Status:    200,
		CreatedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}

	if err := MigrateData(ctx, src, dst, nil); err != nil {
		t.Fatal(err)
	}

	srcCounts, err := CountSourceTables(src)
	if err != nil {
		t.Fatal(err)
	}
	dstCounts, err := CountSourceTables(dst)
	if err != nil {
		t.Fatal(err)
	}
	for i := range srcCounts {
		if srcCounts[i] != dstCounts[i] {
			t.Fatalf("table %s count differs: source=%d destination=%d", srcCounts[i].Table, srcCounts[i].Count, dstCounts[i].Count)
		}
	}

	var sourceMaxID int64
	if err := src.db.QueryRow("SELECT MAX(id) FROM request_logs").Scan(&sourceMaxID); err != nil {
		t.Fatal(err)
	}
	if err := dst.InsertRequestLogs(ctx, []RequestLog{{
		VaultID:   defaultVaultID,
		Ingress:   "explicit",
		Method:    "POST",
		Host:      "example.test",
		Path:      "/after-migration",
		Status:    201,
		CreatedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	var destinationMaxID int64
	if err := dst.db.QueryRow("SELECT MAX(id) FROM request_logs").Scan(&destinationMaxID); err != nil {
		t.Fatal(err)
	}
	if destinationMaxID <= sourceMaxID {
		t.Fatalf("destination autoincrement did not advance: source=%d destination=%d", sourceMaxID, destinationMaxID)
	}
}

func TestMigrateCAToDisk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src, err := Open(filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	state := &CAState{
		RootCert:     []byte("certificate"),
		RootKeyCT:    []byte{1, 2, 3},
		RootKeyNonce: []byte{4, 5, 6},
		Source:       "test",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := src.SetCAState(ctx, state); err != nil {
		t.Fatal(err)
	}

	caDir := filepath.Join(root, "ca")
	migrated, err := MigrateCAToDisk(ctx, src, caDir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("expected CA state to be exported")
	}

	cert, err := os.ReadFile(filepath.Join(caDir, "ca.crt.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cert) != string(state.RootCert) {
		t.Fatalf("certificate differs: got %q", cert)
	}
	keyJSON, err := os.ReadFile(filepath.Join(caDir, "ca.key.enc"))
	if err != nil {
		t.Fatal(err)
	}
	var key encryptedKeyJSON
	if err := json.Unmarshal(keyJSON, &key); err != nil {
		t.Fatal(err)
	}
	nonce, err := base64.StdEncoding.DecodeString(key.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(key.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(nonce) != string(state.RootKeyNonce) || string(ciphertext) != string(state.RootKeyCT) {
		t.Fatalf("encrypted key differs: nonce=%v ciphertext=%v", nonce, ciphertext)
	}

	for path, want := range map[string]os.FileMode{
		filepath.Join(caDir):               0700,
		filepath.Join(caDir, "ca.crt.pem"): 0644,
		filepath.Join(caDir, "ca.key.enc"): 0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s permissions: got %o want %o", path, got, want)
		}
	}

	if _, err := MigrateCAToDisk(ctx, src, caDir); err == nil {
		t.Fatal("expected existing CA files to be protected")
	}
}
