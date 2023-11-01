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
	// flagProgram   = flag.String("program", "", "Only return SDNs whose program matches (case-insensitive)")
	flagSeparator = flag.String("separator", ",", "Separator for columns in output file")
	flagWriteFile = flag.Bool("write", false, "Write results to file, name will be <file>_output.csv")
)

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lmicroseconds | log.Lshortfile)
	log.Printf("Starting moov/batchsearch %s", watchman.Version)

	conf := internal.Config(*flagApiAddress, *flagLocal)
	log.Printf("[INFO] using %s for API address", conf.BasePath)

	// Setup API client
	api, ctx := moov.NewAPIClient(conf), context.TODO()

	// Ping
	if err := ping(ctx, api); err != nil {
		log.Fatalf("[FAILURE] ping Sanctions Search: %v", err)
	} else {
		log.Println("[SUCCESS] ping")
	}

	if path := *flagFile; path != "" {
		names, err := readNames(path)
		if err != nil {
			log.Fatalf("[FAILURE] %v", err)
		}
		if n := checkNames(names, *flagThreshold, api); n == Failure {
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

func checkNames(names []string, threshold float64, api *moov.APIClient) int64 {
	var wg sync.WaitGroup
	wg.Add(len(names))

	var exitCode int64 // must be protected with atomic calls
	markFailure := func() {
		atomic.CompareAndSwapInt64(&exitCode, Success, Failure) // set Failure as exit code
	}

	workers := syncutil.NewGate(*flagWorkers)
	resultsChan := make(chan string, len(names))
	output := make([]string, len(names)*5) // 5X enough space for all results?

	// TODO add to file?
	if *flagVerbose {
		log.Print(newSearchParameterString())
	}

	for i := range names {
		workers.Start()
		go func(name string) {
			defer workers.Done()
			defer wg.Done()

			if result, err := searchByName(api, name); err != nil {
				markFailure()
				log.Printf("[FATAL] problem searching for '%s': %v", name, err)
			} else {
				for i := range result {
					if !result[i].IsSet {
						if *flagVerbose {
							log.Printf("[RESULT] no hits for %s", name)
						}
						return
					}
					if *flagVerbose {
						log.Print(newSearchResultString(result[i], name))
					}

					resultsChan <- newSearchResultRecord(result[i], name)
				}
			}
		}(names[i])
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	output[0] = writeSearchResultHeader()
	count := 1
	for r := range resultsChan {
		output[count] = r
		count++
	}

	if *flagVerbose {
		fmt.Print("\n\n")
		for i := range output[0:count] {
			fmt.Printf("[%d] %s\n", i, output[i])
		}
		fmt.Print("\n\n")
	}

	if *flagWriteFile {
		if err := writeResultsToFile(output[0:count]); err != nil {
			log.Printf("[FATAL] problem writing to file: %v", err)
		}
	}

	log.Printf("[SUCCESS] %d checks complete\n", len(names))

	return exitCode
}

func readNames(path string) ([]string, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("problem reading %s: %v", path, err)
	}
	defer fd.Close()

	scanner := bufio.NewScanner(fd)

	var names []string
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(name, "//") || strings.HasPrefix(name, "#") {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func writeResultsToFile(results []string) error {
	output_filename := strings.Split(*flagFile, ".")[0] + "_output.csv"
	return os.WriteFile(output_filename, []byte(strings.Join(results, "\n")), 0644)
}

func newSearchParameterString() string {
	return fmt.Sprintf(
		"[SETTINGS] MinNameScore=%.2f; Threshold=%.2f; SdnType=%s",
		*flagMinNameScore,
		*flagThreshold,
		*flagSdnType,
	)
}

func getNoun(score float64) string {
	if score >= *flagThreshold {
		return "MATCH"
	}
	return "hit"
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

func newSearchResultRecord(result moov.SearchResult, searched_name string) string {
	return fmt.Sprintf(
		"%s%s%s%s%s%s%s%s%s%s%.2f%s%v%s%s%s%s",
		searched_name,
		*flagSeparator,
		getNoun(result.Score),
		*flagSeparator,
		*result.SdnName,
		*flagSeparator,
		*result.EntityID,
		*flagSeparator,
		result.Type,
		*flagSeparator,
		result.Score,
		*flagSeparator,
		result.Programs,
		*flagSeparator,
		result.Remarks,
		*flagSeparator,
		time.Now().Format(time.RFC3339),
	)
}

func writeSearchResultHeader() string {
	return fmt.Sprint(
		"Name",
		*flagSeparator,
		"Result",
		*flagSeparator,
		"SdnName",
		*flagSeparator,
		"EntityID",
		*flagSeparator,
		"Type",
		*flagSeparator,
		"Score",
		*flagSeparator,
		"Programs",
		*flagSeparator,
		"Remarks",
		*flagSeparator,
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
 * return entityID the EntityID of matched SDN, nil if nothing found
 * return score the match percentage, or -1.0 if nothing found
 * return programs array of strings containing matching OFAC programs, empty (not nil) if nothing found
 * return error only if an error is thrown from search calls
 */
func searchByName(api *moov.APIClient, name string) ([]moov.SearchResult, error) {
	opts := &moov.SearchOpts{
		Limit:      optional.NewInt32(5),
		Name:       optional.NewString(name),
		MinMatch:   optional.NewFloat32(float32(*flagMinNameScore)),
		SdnType:    optional.NewInterface(*flagSdnType),
		XRequestID: optional.NewString(*flagRequestID),
	}
	empty_result := []moov.SearchResult{{
		IsSet:    false,
		EntityID: nil,
		SdnName:  nil,
		Type:     "",
		Score:    0.0,
		Programs: []string{},
	}}

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

	// Prefer to return SDNs if found
	num_search_results := len(search_result.SDNs)
	if num_search_results > 0 {
		results := make([]moov.SearchResult, num_search_results)

		for i := 0; i < num_search_results; i++ {
			if *flagVerbose {
				log.Printf("[VERBOSE] search_result.SDNs[%d]=%s (%v)", i, search_result.SDNs[i].SdnName, search_result.SDNs[i].SdnType)
			}
			sdn := search_result.SDNs[i]
			results[i] = newSearchResult(sdn, sdn.EntityID, float64(sdn.Match))
		}

		return results, nil
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
			return []moov.SearchResult{
				newSearchResult(customer_result.Sdn, altEntityID, float64(search_result.AltNames[0].Match)),
			}, nil
		}

		// If no customer for altName, try companies
		company_result, company_resp, company_err := api.WatchmanApi.GetOfacCompany(ctx, altEntityID, &moov.GetOfacCompanyOpts{})
		if company_err != nil {
			return empty_result, fmt.Errorf("searchByName: %v", err)
		}
		defer company_resp.Body.Close()

		if *flagVerbose {
			log.Printf("[VERBOSE] company_result=%v", company_result)
		}
		if company_result.Sdn.EntityID == altEntityID {
			return []moov.SearchResult{
				newSearchResult(company_result.Sdn, altEntityID, float64(search_result.AltNames[0].Match)),
			}, nil
		}

	}

	// Nothing to return
	return empty_result, nil
}
