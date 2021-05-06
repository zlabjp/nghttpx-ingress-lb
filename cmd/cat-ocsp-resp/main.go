package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                   "cat-ocsp-resp <CERTIFICATE>",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ExactArgs(1),
		Run:                   run,
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(255)
	}
}

func run(cmd *cobra.Command, args []string) {
	path := args[0]

	if !strings.HasSuffix(path, ".crt") {
		fmt.Fprintf(os.Stderr, ".crt suffix not found\n")
		os.Exit(255)
	}

	path = path[0:len(path)-len(".crt")] + ".ocsp-resp"

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read file %v: %v\n", path, err)
		os.Exit(255)
	}

	os.Stdout.Write(data)
}
