package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/mcihad/pgxmigrate/internal/commands"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, ".env okunamadi: %v\n", err)
		os.Exit(1)
	}

	cmd := commands.NewMigrateCommand()
	cmd.Use = "pgxmigrate"
	cmd.Aliases = nil

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
