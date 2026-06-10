package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"

	"github.com/mcihad/pgxmigrate/internal/commands"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aktif dizin okunamadi: %v\n", err)
		os.Exit(1)
	}

	if err := godotenv.Load(filepath.Join(wd, ".env")); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, ".env okunamadi: %v\n", err)
		os.Exit(1)
	}

	if err := commands.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
