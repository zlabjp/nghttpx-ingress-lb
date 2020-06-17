package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

var (
	flags = pflag.NewFlagSet("", pflag.ExitOnError)
)

func main() {
	flags.Parse(os.Args)

	if len(flags.Args()) < 2 {
		fmt.Fprintf(os.Stderr, "Too few arguments\n")
		os.Exit(255)
	}

	path := flags.Args()[1]

	if !strings.HasSuffix(path, ".crt") {
		fmt.Fprintf(os.Stderr, ".crt suffix not found\n")
		os.Exit(255)
	}

	path = path[0:len(path)-len(".crt")] + ".ocsp-resp"

	data, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read file %v: %v\n", path, err)
		os.Exit(255)
	}

	os.Stdout.Write(data)
}
