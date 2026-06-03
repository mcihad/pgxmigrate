package commands

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "pgxmigrate",
		Short: "pgx tabanli migration araci",
	}

	rootCmd.AddCommand(NewMigrateCommand())
	return rootCmd
}
