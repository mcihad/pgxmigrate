package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/mcihad/pgxmigrate/migrator"
)

type migrateOptions struct {
	dir         string
	databaseURL string
	schema      string
}

func NewMigrateCommand() *cobra.Command {
	opts := &migrateOptions{}
	cmd := &cobra.Command{
		Use:     "migrate",
		Aliases: []string{"migration", "migrations"},
		Short:   "Migration olusturma, silme, ileri/geri alma ve durum islemleri",
	}

	cmd.PersistentFlags().StringVar(&opts.dir, "dir", migrator.DefaultDirectory, "migration dosyalari dizini")
	cmd.PersistentFlags().StringVar(&opts.databaseURL, "database-url", "", "postgres baglanti adresi")
	cmd.PersistentFlags().StringVar(&opts.schema, "schema", "", "postgres sema adi (DB_SCHEMA env degiskenini gecer)")

	cmd.AddCommand(createCommand(opts))
	cmd.AddCommand(deleteCommand(opts))
	cmd.AddCommand(upCommand(opts))
	cmd.AddCommand(downCommand(opts))
	cmd.AddCommand(redoCommand(opts))
	cmd.AddCommand(resetCommand(opts))
	cmd.AddCommand(rollbackCommand(opts))
	cmd.AddCommand(toCommand(opts))
	cmd.AddCommand(forceCommand(opts))
	cmd.AddCommand(statusCommand(opts))
	cmd.AddCommand(pendingCommand(opts))
	cmd.AddCommand(appliedCommand(opts))
	cmd.AddCommand(validateCommand(opts))
	cmd.AddCommand(currentCommand(opts))

	return cmd
}

func createCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Yeni migration dosyasi olusturur",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := migrator.New(nil, opts.dir)
			path, err := m.Create(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Olusturuldu: %s\n", path)
			return nil
		},
	}
}

func deleteCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <version-or-name>",
		Short: "Uygulanmamis migration dosyasini siler",
		Args:  cobra.ExactArgs(1),
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			path, err := m.Delete(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Silindi: %s\n", path)
			return nil
		}),
	}
}

func upCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "up [steps]",
		Short: "Bekleyen migrationlari ileri uygular",
		Args:  cobra.MaximumNArgs(1),
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			steps, err := optionalSteps(args, 0)
			if err != nil {
				return err
			}
			applied, err := m.Up(ctx, steps)
			if err != nil {
				return err
			}
			printRuns(cmd, "Uygulandi", applied)
			return nil
		}),
	}
}

func downCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "down [steps]",
		Short: "Son uygulanan migrationlari geri alir",
		Args:  cobra.MaximumNArgs(1),
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			steps, err := optionalSteps(args, 1)
			if err != nil {
				return err
			}
			rolledBack, err := m.Down(ctx, steps)
			if err != nil {
				return err
			}
			printRuns(cmd, "Geri alindi", rolledBack)
			return nil
		}),
	}
}

func redoCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "redo",
		Short: "Son migrationi geri alip tekrar uygular",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			rolledBack, applied, err := m.Redo(ctx)
			if err != nil {
				return err
			}
			printRuns(cmd, "Geri alindi", rolledBack)
			printRuns(cmd, "Tekrar uygulandi", applied)
			return nil
		}),
	}
}

func resetCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Uygulanmis tum migrationlari geri alir",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			rolledBack, err := m.Reset(ctx)
			if err != nil {
				return err
			}
			printRuns(cmd, "Geri alindi", rolledBack)
			return nil
		}),
	}
}

func rollbackCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Son batch migrationlari geri alir",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			rolledBack, err := m.Rollback(ctx)
			if err != nil {
				return err
			}
			printRuns(cmd, "Geri alindi", rolledBack)
			return nil
		}),
	}
}

func toCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "to <version>",
		Short: "Belirtilen versiyona ileri/geri gider",
		Args:  cobra.ExactArgs(1),
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			runs, err := m.To(ctx, args[0])
			if err != nil {
				return err
			}
			printRuns(cmd, "Calisti", runs)
			return nil
		}),
	}
}

func forceCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "force <version|0|none>",
		Short: "SQL calistirmadan migration durumunu belirtilen versiyona ayarlar",
		Args:  cobra.ExactArgs(1),
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			runs, err := m.Force(ctx, args[0])
			if err != nil {
				return err
			}
			printRuns(cmd, "Isaretlendi", runs)
			return nil
		}),
	}
}

func statusCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"list"},
		Short:   "Migration durumlarini listeler",
		Args:    cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			items, err := m.Status(ctx)
			if err != nil {
				return err
			}
			printStatusItems(cmd, items)
			return nil
		}),
	}
}

func pendingCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "pending",
		Short: "Bekleyen migrationlari listeler",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			items, err := m.Pending(ctx)
			if err != nil {
				return err
			}
			printStatusItems(cmd, items)
			return nil
		}),
	}
}

func appliedCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "applied",
		Short: "Uygulanmis migrationlari listeler",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			items, err := m.Applied(ctx)
			if err != nil {
				return err
			}
			printStatusItems(cmd, items)
			return nil
		}),
	}
}

func validateCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Migration dosyalarini ve uygulanmis kayitlari dogrular",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			issues, err := m.Validate(ctx)
			if err != nil {
				return err
			}
			if len(issues) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "OK")
				return nil
			}
			for _, issue := range issues {
				target := issue.Path
				if target == "" {
					target = issue.Version
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", target, issue.Message)
			}
			return fmt.Errorf("%d validation issue bulundu", len(issues))
		}),
	}
}

func currentCommand(opts *migrateOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Son uygulanmis migration versiyonunu yazar",
		Args:  cobra.NoArgs,
		RunE: withMigrator(opts, func(ctx context.Context, m *migrator.Migrator, cmd *cobra.Command, args []string) error {
			current, err := m.Current(ctx)
			if err != nil {
				return err
			}
			if current == "" {
				current = "none"
			}
			fmt.Fprintln(cmd.OutOrStdout(), current)
			return nil
		}),
	}
}

func withMigrator(opts *migrateOptions, fn func(context.Context, *migrator.Migrator, *cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		schema := opts.schema
		if schema == "" {
			schema = os.Getenv("DB_SCHEMA")
		}
		pool, err := newPool(cmd.Context(), opts.databaseURL, schema)
		if err != nil {
			return err
		}
		defer pool.Close()

		m := migrator.New(pool, opts.dir, migrator.WithSchema(schema))
		if err := m.Ensure(cmd.Context()); err != nil {
			return err
		}
		return fn(cmd.Context(), m, cmd, args)
	}
}

func newPool(ctx context.Context, databaseURL, schema string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		databaseURL = os.Getenv("DB_URL")
	}
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL()
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres baglantisi hazirlanamadi: %w", err)
	}
	if schema != "" {
		config.ConnConfig.RuntimeParams["search_path"] = schema
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("postgres baglantisi hazirlanamadi: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres baglantisi kurulamadi: %w", err)
	}
	return pool, nil
}

func defaultDatabaseURL() string {
	host := env("DB_HOST", "localhost")
	port := env("DB_PORT", "5432")
	user := env("DB_USER", "postgres")
	password := env("DB_PASSWORD", "postgres")
	name := env("DB_NAME", "postgres")
	sslMode := env("DB_SSLMODE", "disable")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, name, sslMode)
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func optionalSteps(args []string, fallback int) (int, error) {
	if len(args) == 0 {
		return fallback, nil
	}
	steps, err := strconv.Atoi(args[0])
	if err != nil || steps < 0 {
		return 0, fmt.Errorf("steps pozitif sayi olmali")
	}
	return steps, nil
}

func printRuns(cmd *cobra.Command, prefix string, runs []migrator.Run) {
	if len(runs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Islem yok")
		return
	}
	for _, run := range runs {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s_%s\n", prefix, run.Version, run.Name)
	}
}

func printStatusItems(cmd *cobra.Command, items []migrator.StatusItem) {
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Islem yok")
		return
	}
	for _, item := range items {
		state := "pending"
		if item.Missing {
			state = "missing"
		} else if item.Dirty {
			state = "dirty"
		} else if item.Applied {
			state = "applied"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.Version, item.Name, state)
	}
}
