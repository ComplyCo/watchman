// Copyright 2022 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/antihax/optional"
	"github.com/moov-io/watchman"
	moov "github.com/moov-io/watchman/client"
	"github.com/moov-io/watchman/cmd/internal"
	"go4.org/syncutil"
)

var (
	flagApiAddress   = flag.String("address", internal.DefaultApiAddress, "Moov API address")
	flagLocal        = flag.Bool("local", true, "Use local HTTP addresses")
	flagThreshold    = flag.Float64("threshold", 0.99, "Minimum match percentage required for blocking")
	flagMinNameScore = flag.Float64("min-match", 0.90, "How close must names match")
	flagFile         = flag.String("file", "", "Filepath to file with names to check")
	flagSdnType      = flag.String("sdn-type", "individual", "sdnType query param")
	flagRequestID    = flag.String("request-id", "", "Override what is set for the X-Request-ID HTTP header")
	flagVerbose      = flag.Bool("v", false, "Enable detailed logging")
	flagWorkers      = flag.Int("workers", runtime.NumCPU(), "How many tasks to run concurrently")
	flagWriteFile    = flag.Bool("write", false, "Write results to file, name will be <file>_output.csv")
)

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lmicroseconds | log.Lshortfile)
	log.Printf("Starting moov/batchsearch %s", watchman.Version)

	conf := internal.Config(*flagApiAddress, *flagLocal)
	log.Printf("[INFO] using %s for API address", conf.BasePath)

	// Setup API client
	api, ctx := moov.NewAPIClient(conf), context.TODO()
	// TODO: pass this context through later

	// Ping
	if err := ping(ctx, api); err != nil {
		log.Fatalf("[FAILURE] ping Sanctions Search: %v", err)
	} else {
		log.Println("[SUCCESS] ping")
	}

	if path := *flagFile; path != "" {
		rows, err := readRows(path)
		if err != nil {
			log.Fatalf("[FAILURE] %v", err)
		}
		if n := processRows(rows, *flagThreshold, api); n == Failure {
			os.Exit(int(n))
		}
	}
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

func processRows(rows []string, threshold float64, api *moov.APIClient) int64 {
	// First row is headers, store them
	headings := rows[0]
	rows = rows[1:]

	var wg sync.WaitGroup
	wg.Add(len(rows))

	var exitCode int64 // must be protected with atomic calls
	markFailure := func() {
		atomic.CompareAndSwapInt64(&exitCode, Success, Failure) // set Failure as exit code
	}

	workers := syncutil.NewGate(*flagWorkers)
	resultsChan := make(chan string, len(rows))
	output := make([]string, len(rows)+1) // +1 for header row

	for i := range rows {
		workers.Start()
		go func(row string) {
			defer workers.Done()
			defer wg.Done()

			// Compose name from fixed columns - make smarter, later
			cols := strings.Split(row, ",")
			name := fmt.Sprintf("%s, %s", cols[2], cols[1])

			if result, err := searchByName(api, name); err != nil {
				markFailure()
				log.Printf("[FATAL] problem searching for '%s': %v", name, err)
			} else {
				if result.IsSet {
					if *flagVerbose {
						log.Print(newSearchResultString(result, name))
					}
					resultsChan <- newSearchResultRecord(result, row)

				} else {
					if *flagVerbose {
						log.Printf("[RESULT] no hits for %s", name)
					}
					resultsChan <- newSearchResultClearRecord(result, row)
				}
			}
		}(rows[i])
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	output[0] = writeHeadings(headings)
	count := 1
	for r := range resultsChan {
		output[count] = r
		count++
	}

	if *flagVerbose {
		fmt.Print("\n\n")
		for i := range output[0:count] {
			fmt.Printf("%s\n", output[i])
		}
		fmt.Print("\n\n")
	}

	if *flagWriteFile {
		if err := writeResultsToFile(output[0:count]); err != nil {
			log.Printf("[FATAL] problem writing to file: %v", err)
		}
	}

	log.Printf("[SUCCESS] %d checks complete\n", len(rows))

	return exitCode
}

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

func getNoun(score float64) string {
	if score < 0.0 {
		return "Clear"
	}
	if score >= *flagThreshold {
		return "MATCH"
	}
	return "Hit"
}

func newSearchResultString(result moov.SearchResult, searched_name string) string {
	return fmt.Sprintf(
		"[RESULT] found %s for %s: SdnName=%s; EntityID=%s; Type=%s; Score=%.2f; Programs=%v; Remarks=%s; Timestamp=%s",
		getNoun(result.Score),
		searched_name,
		*result.SdnName,
		*result.EntityID,
		result.Type,
		result.Score,
		result.Programs,
		result.Remarks,
		time.Now().Format(time.RFC3339),
	)
}

func newSearchResultRecord(result moov.SearchResult, input_row string) string {
	sdn_name_no_comma := *result.SdnName
	if strings.Contains(*result.SdnName, ",") {
		sdn_name_parts := strings.Split(*result.SdnName, ",")
		sdn_name_no_comma = fmt.Sprintf("%s %s", sdn_name_parts[1], sdn_name_parts[0])
	}

	return fmt.Sprintf(
		"%s,%s,%s,%s,%.2f,%s,%s",
		strings.TrimRight(input_row, ","),
		getNoun(result.Score),
		sdn_name_no_comma,
		*result.EntityID,
		result.Score,
		result.Programs,
		time.Now().Format(time.RFC3339),
	)
}

func newSearchResultClearRecord(result moov.SearchResult, searched_name string) string {
	return fmt.Sprintf(
		"%s,%s,,,,,%s",
		strings.TrimRight(searched_name, ","),
		getNoun(result.Score),
		time.Now().Format(time.RFC3339),
	)
}

func writeHeadings(original_headings string) string {
	return fmt.Sprintf(
		"%s,%s,%s,%s,%s,%s,%s",
		strings.TrimRight(original_headings, ","),
		"Result",
		"SdnName",
		"EntityID",
		"Score",
		"Programs",
		"Timestamp",
	)
}

func newSearchResult(query_result moov.OfacSdn, entity_id string, score float64) moov.SearchResult {
	return moov.SearchResult{
		IsSet:    true,
		EntityID: &entity_id,
		SdnName:  &query_result.SdnName,
		Type:     query_result.SdnType,
		Score:    score,
		Programs: query_result.Programs,
	}
}

/*
 * Search OFAC data for given name.
 * If no SDN but altNames, get data for each altName's EntityID.
 *
 * return SearchResult struct with: EntityID, SdnName, Type, Score, Programs
 */
func searchByName(api *moov.APIClient, name string) (moov.SearchResult, error) {
	opts := &moov.SearchOpts{
		Limit:      optional.NewInt32(1),
		Name:       optional.NewString(name),
		MinMatch:   optional.NewFloat32(float32(*flagMinNameScore)),
		SdnType:    optional.NewInterface(*flagSdnType),
		XRequestID: optional.NewString(*flagRequestID),
	}
	empty_result := moov.SearchResult{
		IsSet:    false,
		EntityID: nil,
		SdnName:  nil,
		Type:     "",
		Score:    -1.0, // -1.0 indicates nothing found
		Programs: []string{},
	}

	ctx, cancelFunc := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancelFunc()

	search_result, resp, err := api.WatchmanApi.Search(ctx, opts)
	if err != nil {
		return empty_result, fmt.Errorf("searchByName: %v", err)
	}
	defer resp.Body.Close()

	if *flagVerbose {
		log.Printf("[VERBOSE] search_result SDNs=%d; AltNames=%d", len(search_result.SDNs), len(search_result.AltNames))
	}

	// Return SDN if found
	if len(search_result.SDNs) > 0 {
		// Only return the best match
		sdn := search_result.SDNs[0]
		return newSearchResult(sdn, sdn.EntityID, float64(sdn.Match)), nil
	}

	//  If no SDN for name, check "customer" via EntityID
	if len(search_result.AltNames) > 0 {
		altEntityID := search_result.AltNames[0].EntityID
		if *flagVerbose {
			log.Printf("[VERBOSE] alternateName=%s; altEntityID=%s", search_result.AltNames[0].AlternateName, altEntityID)
		}

		customer_result, customer_resp, customer_err := api.WatchmanApi.GetOfacCustomer(ctx, altEntityID, &moov.GetOfacCustomerOpts{})
		if customer_err != nil {
			return empty_result, fmt.Errorf("searchByName: %v", err)
		}
		defer customer_resp.Body.Close()

		if *flagVerbose {
			log.Printf("[VERBOSE] customer_result=%v", customer_result.Sdn)
		}

		if customer_result.Sdn.EntityID == altEntityID {
			return newSearchResult(customer_result.Sdn, altEntityID, float64(search_result.AltNames[0].Match)), nil
		}
	}

	return empty_result, nil
}
