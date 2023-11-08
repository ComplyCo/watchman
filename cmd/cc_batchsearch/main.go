// Copyright 2022 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/antihax/optional"
	"github.com/moov-io/base/log"

	"github.com/moov-io/watchman"
	moov "github.com/moov-io/watchman/client"
	"github.com/moov-io/watchman/cmd/internal"
)

var (
	flagApiAddress   = flag.String("address", internal.DefaultApiAddress, "Moov API address")
	flagLocal        = flag.Bool("local", true, "Use local HTTP addresses")
	flagFile         = flag.String("file", "", "Filepath to file with names to check")
	flagMinNameScore = flag.Float64("min-match", 0.90, "How close must names match")
	flagSdnType      = flag.String("sdn-type", "individual", "sdnType query param")
	flagThreshold    = flag.Float64("threshold", 0.99, "Minimum match percentage required for blocking")
	flagWriteFile    = flag.Bool("write", false, "Write results to file, name will be <file>_output.csv")
)

func main() {
	flag.Parse()
	log := log.NewDefaultLogger()
	log.Info().Logf("Starting moov/batchsearch %s", watchman.Version)

	conf := internal.Config(*flagApiAddress, *flagLocal)
	log.Info().Logf("[INFO] using %s for API address", conf.BasePath)

	// Setup API client
	api, ctx := moov.NewAPIClient(conf), context.TODO()
	// TODO: pass this context through later

	// Ping
	if err := ping(ctx, api); err != nil {
		log.Fatal().LogErrorf("[FAILURE] ping Sanctions Search: %v", err)
	} else {
		log.Info().Log("[SUCCESS] ping")
	}

	if path := *flagFile; path != "" {
		rows, err := readRows(path)
		if err != nil {
			log.Fatal().LogErrorf("[FAILURE] %v", err)
		}

		search_opts := newSearchOptsFromFlags()
		result, err := internal.ProcessRows(rows, api, search_opts, log)

		if err != nil {
			log.Fatal().LogErrorf("[FAILURE] %v", err)
		}

		if *flagWriteFile {
			if err := writeResultsToFile(result); err != nil {
				log.Fatal().LogErrorf("[FATAL] problem writing to file: %v", err)
			}
		}

	}
}

func newSearchOptsFromFlags() moov.SearchOpts {
	search_opts := moov.SearchOpts{
		Limit:    optional.NewInt32(1),
		MinMatch: optional.NewFloat32(0.90),
		SdnType:  optional.NewInterface("individual"),
	}

	if *flagThreshold != 0 {
		search_opts.MinMatch = optional.NewFloat32(float32(*flagThreshold))
	}
	if *flagMinNameScore != 0 {
		search_opts.MinMatch = optional.NewFloat32(float32(*flagMinNameScore))
	}
	if *flagSdnType != "" {
		search_opts.SdnType = optional.NewInterface(*flagSdnType)
	}

	return search_opts
}

func ping(ctx context.Context, api *moov.APIClient) error {
	resp, err := api.WatchmanApi.Ping(ctx)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ping error (stats code: %d): %v", resp.StatusCode, err)
	}
	return nil
}

var (
	Success int64 = 0
	Failure int64 = 1
)

func readRows(path string) ([]string, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("problem reading %s: %v", path, err)
	}
	defer fd.Close()

	scanner := bufio.NewScanner(fd)

	var rows []string
	for scanner.Scan() {
		row := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(row, "//") || strings.HasPrefix(row, "#") {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func writeResultsToFile(results []string) error {
	output_filename := strings.Split(*flagFile, ".")[0] + "_output.csv"
	return os.WriteFile(output_filename, []byte(strings.Join(results, "\n")), 0644)
}
