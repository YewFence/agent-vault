package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Infisical/agent-vault/internal/pidfile"
	"github.com/Infisical/agent-vault/internal/store"
	"github.com/spf13/cobra"
)

var migrateDBCmd = &cobra.Command{
	Use:   "migrate-db",
	Short: "Copy data between SQLite and PostgreSQL",
	Long: `Copies all Agent Vault data between SQLite and PostgreSQL. The source and
destination database types are detected from --from and --to. PostgreSQL endpoints
must use a postgres:// or postgresql:// URL; other values are SQLite file paths.

The Agent Vault server must be stopped before running this command.

The destination must be empty. A PostgreSQL destination may contain only the
baseline schema seed. A SQLite destination must be a new file.

The CA certificate and encrypted key are migrated between SQLite's local CA
files and PostgreSQL's database-backed CA state.`,
	RunE: runMigrateDB,
}

func init() {
	migrateDBCmd.Flags().String("to", "", "destination PostgreSQL URL or SQLite database path (required)")
	migrateDBCmd.Flags().Bool("dry-run", false, "count rows per table without copying anything")
	migrateDBCmd.Flags().String("from", "", "source PostgreSQL URL or SQLite database path (default: agent-vault.db in the configured data directory)")
	migrateDBCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt (for scripted/CI usage)")
	_ = migrateDBCmd.MarkFlagRequired("to")
	rootCmd.AddCommand(migrateDBCmd)
}

type migrationEndpoint struct {
	dialect string
	value   string
}

func parseMigrationEndpoint(raw string) (migrationEndpoint, error) {
	if raw == "" {
		return migrationEndpoint{}, fmt.Errorf("database endpoint is empty")
	}
	u, err := url.Parse(raw)
	if err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		return migrationEndpoint{dialect: "postgres", value: raw}, nil
	}
	if strings.Contains(raw, "://") {
		return migrationEndpoint{}, fmt.Errorf("unsupported database endpoint %q", raw)
	}
	return migrationEndpoint{dialect: "sqlite", value: raw}, nil
}

func (e migrationEndpoint) display() string {
	if e.dialect == "postgres" {
		return store.RedactURL(e.value)
	}
	return e.value
}

func openMigrationEndpoint(e migrationEndpoint) (*store.SQLStore, error) {
	if e.dialect == "sqlite" {
		return store.Open(e.value)
	}
	db, err := store.OpenStore(store.StoreConfig{DatabaseURL: e.value})
	if err != nil {
		return nil, err
	}
	sqlStore, ok := db.(*store.SQLStore)
	if !ok {
		_ = db.Close()
		return nil, fmt.Errorf("database is not a SQL store (unexpected type %T)", db)
	}
	return sqlStore, nil
}

func prepareSQLiteDestination(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("destination SQLite database already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking destination SQLite database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}
	return nil
}

func runMigrateDB(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	toRaw, _ := cmd.Flags().GetString("to")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	fromRaw, _ := cmd.Flags().GetString("from")

	// 1. Check the server is not running.
	if pid, err := pidfile.Read(); err == nil {
		if pidfile.IsRunning(pid) {
			return fmt.Errorf("agent vault server is running (PID %d); stop it before migrating", pid)
		}
	}

	// 2. Resolve and validate endpoints.
	if fromRaw == "" {
		var err error
		fromRaw, err = store.DefaultDBPath()
		if err != nil {
			return fmt.Errorf("resolving default database path: %w", err)
		}
	}
	srcEndpoint, err := parseMigrationEndpoint(fromRaw)
	if err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}
	dstEndpoint, err := parseMigrationEndpoint(toRaw)
	if err != nil {
		return fmt.Errorf("invalid destination: %w", err)
	}
	if srcEndpoint.dialect == dstEndpoint.dialect {
		return fmt.Errorf("source and destination must use different database types")
	}
	if srcEndpoint.dialect == "sqlite" {
		if _, err := os.Stat(srcEndpoint.value); err != nil {
			return fmt.Errorf("source database not found: %w", err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Source:      %s (%s)\n", srcEndpoint.display(), srcEndpoint.dialect)
	fmt.Fprintf(cmd.OutOrStdout(), "Destination: %s (%s)\n", dstEndpoint.display(), dstEndpoint.dialect)
	fmt.Fprintln(cmd.OutOrStdout())

	// 3. Open source database.
	srcStore, err := openMigrationEndpoint(srcEndpoint)
	if err != nil {
		return fmt.Errorf("opening source database: %w", err)
	}
	defer func() { _ = srcStore.Close() }()

	// 4. Dry-run mode: count rows and exit.
	if dryRun {
		counts, err := store.CountSourceTables(srcStore)
		if err != nil {
			return fmt.Errorf("counting source rows: %w", err)
		}
		total := 0
		fmt.Fprintln(cmd.OutOrStdout(), "Table row counts (dry run):")
		for _, tc := range counts {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-30s %d\n", tc.Table, tc.Count)
			total += tc.Count
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d rows\n", total)
		return nil
	}

	// 5. Count source rows for confirmation.
	counts, err := store.CountSourceTables(srcStore)
	if err != nil {
		return fmt.Errorf("counting source rows: %w", err)
	}
	total := 0
	for _, tc := range counts {
		total += tc.Count
	}

	fmt.Fprintf(cmd.OutOrStdout(), "This will copy %d records from %s to %s.\n", total, srcEndpoint.dialect, dstEndpoint.dialect)

	autoYes, _ := cmd.Flags().GetBool("yes")
	if !autoYes {
		fmt.Fprintf(cmd.OutOrStdout(), "Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	// 6. Open and validate destination database after confirmation.
	if dstEndpoint.dialect == "sqlite" {
		if err := prepareSQLiteDestination(dstEndpoint.value); err != nil {
			return err
		}
	}
	dstStore, err := openMigrationEndpoint(dstEndpoint)
	if err != nil {
		return fmt.Errorf("opening destination database: %w", err)
	}
	defer func() { _ = dstStore.Close() }()

	found, err := store.CountDestinationData(dstStore)
	if err != nil {
		return fmt.Errorf("checking destination database: %w", err)
	}
	if found != "" {
		return fmt.Errorf("destination database already contains data (%s); aborting", found)
	}

	// 7. Copy data.
	fmt.Fprintln(cmd.OutOrStdout())
	err = store.MigrateData(ctx, srcStore, dstStore, func(table string, rows int) {
		fmt.Fprintf(cmd.OutOrStdout(), "Copying %-30s %d rows... done\n", table+":", rows)
	})
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// 8. Verify table counts before moving CA between storage modes.
	if err := verifyMigrationCounts(srcStore, dstStore); err != nil {
		return fmt.Errorf("migration verification failed: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nVerified table row counts.")

	// 9. Migrate CA between SQLite's files and Postgres' database state.
	caMigrated, err := migrateCA(ctx, srcStore, dstStore, srcEndpoint, dstEndpoint)
	if err != nil {
		return err
	}
	if caMigrated {
		fmt.Fprintln(cmd.OutOrStdout(), "CA certificate and encrypted key migrated.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "No CA state found; skipped CA migration.")
	}

	fmt.Fprintln(cmd.OutOrStdout(), "\nMigration complete.")
	return nil
}

func verifyMigrationCounts(src, dst *store.SQLStore) error {
	srcCounts, err := store.CountSourceTables(src)
	if err != nil {
		return fmt.Errorf("counting source rows: %w", err)
	}
	dstCounts, err := store.CountSourceTables(dst)
	if err != nil {
		return fmt.Errorf("counting destination rows: %w", err)
	}
	if len(srcCounts) != len(dstCounts) {
		return fmt.Errorf("table count list length differs: source=%d destination=%d", len(srcCounts), len(dstCounts))
	}
	for i := range srcCounts {
		if srcCounts[i].Table != dstCounts[i].Table || srcCounts[i].Count != dstCounts[i].Count {
			return fmt.Errorf("%s row count differs: source=%d destination=%d", srcCounts[i].Table, srcCounts[i].Count, dstCounts[i].Count)
		}
	}
	return nil
}

func migrateCA(ctx context.Context, src, dst *store.SQLStore, srcEndpoint, dstEndpoint migrationEndpoint) (bool, error) {
	if srcEndpoint.dialect == "sqlite" {
		caDir := filepath.Join(filepath.Dir(srcEndpoint.value), "ca")
		migrated, err := store.MigrateCAFromDisk(ctx, dst, caDir)
		if err != nil {
			return false, fmt.Errorf("migrating CA from disk: %w", err)
		}
		return migrated, nil
	}

	caDir := filepath.Join(filepath.Dir(dstEndpoint.value), "ca")
	migrated, err := store.MigrateCAToDisk(ctx, src, caDir)
	if err != nil {
		return false, fmt.Errorf("migrating CA to disk: %w", err)
	}
	return migrated, nil
}
