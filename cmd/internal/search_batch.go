// Copyright 2022 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

/*
 * Extended by ComplyCo for batch searches
 * This is in cmd/internal package so it can be imported into the server and the CLI code
 */

package internal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/antihax/optional"
	"github.com/moov-io/base/log"
	moov "github.com/moov-io/watchman/client"
	"go4.org/syncutil"
)

var matchThreshold float64 = 0.99

func SearchBatch(logger log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(128 << 20) // 128 MB limit for file size
		if err != nil {
			http.Error(w, "Unable to parse form", http.StatusBadRequest)
			return
		}
		search_opts := newSearchOptsFromFormFields(r)

		file, handler, err := r.FormFile("csvFile")
		if err != nil {
			http.Error(w, "Unable to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		input, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Unable to read file content", http.StatusInternalServerError)
			return
		}

		rows := strings.Split(string(input), "\n")
		conf := Config(DefaultApiAddress, true)
		api := moov.NewAPIClient(conf)
		result, err := ProcessRows(rows, api, search_opts, logger)
		if err != nil {
			http.Error(w, "Unable to process input", http.StatusInternalServerError)
			return
		}
		output := strings.Join(result, "\n")

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", handler.Filename))
		w.Header().Set("Content-Type", "text/csv")
		reader := strings.NewReader(output)
		_, err = io.Copy(w, reader)

		if err != nil {
			http.Error(w, "Unable to write response", http.StatusInternalServerError)
			return
		}
	}
}

func newSearchOptsFromFormFields(r *http.Request) moov.SearchOpts {
	search_opts := &moov.SearchOpts{
		Limit:    optional.NewInt32(1),
		MinMatch: optional.NewFloat32(0.90),
		SdnType:  optional.NewInterface("individual"),
	}

	if threshold := r.FormValue("threshold"); threshold != "" {
		if f, err := strconv.ParseFloat(threshold, 64); err == nil {
			matchThreshold = f
		}
	} else {
		matchThreshold = 0.99
	}
	if min_match := r.FormValue("min-match"); min_match != "" {
		if f, err := strconv.ParseFloat(min_match, 64); err == nil {
			search_opts.MinMatch = optional.NewFloat32(float32(f))
		}
	}
	if sdn_type := r.FormValue("sdn-type"); sdn_type != "" {
		search_opts.SdnType = optional.NewInterface(sdn_type)
	}

	return *search_opts
}

type ChanResult struct {
	Index int
	Value string
}

func ProcessRows(rows []string, api *moov.APIClient, search_opts moov.SearchOpts, log log.Logger) ([]string, error) {
	// First row is headers, store them
	headings := rows[0]
	rows = rows[1:]
	input_size := len(rows)
	log.Info().Logf("Processing %d rows", input_size)

	var wg sync.WaitGroup
	workers := syncutil.NewGate(runtime.NumCPU())
	resultsChan := make(chan ChanResult, len(rows))
	output := make([]string, len(rows)+1) // +1 for header row

	for i, row := range rows {
		wg.Add(1)
		workers.Start()
		go func(i int, row string) {
			defer workers.Done()
			defer wg.Done()

			name := getNameFromRow(row)

			if result, err := searchByName(api, search_opts, name, log); err != nil {
				log.Fatal().LogErrorf("[FATAL] problem searching for '%s': %v", name, err)
				return
			} else {
				if result.IsSet {
					// log.Debug().Log(newSearchResultString(result, name))
					resultsChan <- ChanResult{Value: newSearchResultRecord(result, row), Index: i}

				} else {
					// log.Debug().Logf("[RESULT] no hits for %s", name)
					resultsChan <- ChanResult{Value: newSearchResultClearRecord(result, row), Index: i}
				}
			}
		}(i, row)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	output[0] = writeHeadings(headings)
	for r := range resultsChan {
		output[r.Index+1] = r.Value // +1 for header row
	}
	output_size := len(output) - 1
	if input_size == output_size {
		log.Info().Logf("[SUCCESS] %d checks complete\n", output_size)
	} else {
		log.Info().Logf("[FAILURES] %d of %d checks complete\n", output_size, input_size)
	}

	return output, nil
}

func getNameFromRow(row string) string {
	cols := strings.Split(row, ",")

	if len(cols) >= 3 {
		// If 3 or more columns, assume first is an ID
		return fmt.Sprintf("%s, %s", trimDelimiters(cols[2]), trimDelimiters(cols[1]))
	} else if len(cols) == 2 {
		// If 2 columns, assume both are name fields
		return fmt.Sprintf("%s, %s", trimDelimiters(cols[1]), trimDelimiters(cols[0]))
	} else {
		return trimDelimiters(cols[0])
	}
}

func trimDelimiters(s string) string {
	// Remove characters that cause problems with search
	return strings.Trim(s, ",\n\r\t")
}

func getNoun(score float64) string {
	if score < 0.0 {
		return "Clear"
	}
	if score >= matchThreshold {
		return "MATCH"
	}
	return "Hit"
}

// func newSearchResultString(result moov.SearchResult, searched_name string) string {
// 	return fmt.Sprintf(
// 		"[RESULT] found %s for %s: SdnName=%s; EntityID=%s; Type=%s; Score=%.2f; Programs=%v; Remarks=%s; Timestamp=%s",
// 		getNoun(result.Score),
// 		searched_name,
// 		*result.SdnName,
// 		*result.EntityID,
// 		result.Type,
// 		result.Score,
// 		result.Programs,
// 		result.Remarks,
// 		time.Now().Format(time.RFC3339),
// 	)
// }

func newSearchResultRecord(result moov.SearchResult, input_row string) string {
	sdn_name_no_comma := *result.SdnName
	if strings.Contains(*result.SdnName, ",") {
		sdn_name_parts := strings.Split(*result.SdnName, ",")
		sdn_name_no_comma = fmt.Sprintf("%s %s", sdn_name_parts[1], sdn_name_parts[0])
	}

	return fmt.Sprintf(
		"%s,%s,%s,%s,%.2f,%s,%s",
		trimDelimiters(input_row),
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
		trimDelimiters(searched_name),
		getNoun(result.Score),
		time.Now().Format(time.RFC3339),
	)
}

func writeHeadings(original_headings string) string {
	return fmt.Sprintf(
		"%s,%s,%s,%s,%s,%s,%s",
		trimDelimiters(original_headings),
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
func searchByName(api *moov.APIClient, search_opts moov.SearchOpts, name string, log log.Logger) (moov.SearchResult, error) {
	if name == "" {
		return moov.SearchResult{}, fmt.Errorf("searchByName: name is empty")
	}

	search_opts.Name = optional.NewString(name)
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

	search_result, resp, err := api.WatchmanApi.Search(ctx, &search_opts)
	if err != nil {
		return empty_result, fmt.Errorf("searchByName.Search: %v", err)
	}
	defer resp.Body.Close()

	// log.Debug().Logf("[VERBOSE] search_result SDNs=%d; AltNames=%d", len(search_result.SDNs), len(search_result.AltNames))

	// Return SDN if found
	if len(search_result.SDNs) > 0 {
		// Only return the best match
		sdn := search_result.SDNs[0]
		return newSearchResult(sdn, sdn.EntityID, float64(sdn.Match)), nil
	}

	//  If no SDN for name, check "customer" via EntityID
	if len(search_result.AltNames) > 0 {
		altEntityID := search_result.AltNames[0].EntityID
		// log.Debug().Logf("[VERBOSE] alternateName=%s; altEntityID=%s", search_result.AltNames[0].AlternateName, altEntityID)

		customer_result, customer_resp, customer_err := api.WatchmanApi.GetOfacCustomer(ctx, altEntityID, &moov.GetOfacCustomerOpts{})
		if customer_err != nil {
			return empty_result, fmt.Errorf("searchByName.GetOfacCustomer: %v", err)
		}
		defer customer_resp.Body.Close()

		// log.Debug().Logf("[VERBOSE] customer_result=%v", customer_result.Sdn)

		if customer_result.Sdn.EntityID == altEntityID {
			return newSearchResult(customer_result.Sdn, altEntityID, float64(search_result.AltNames[0].Match)), nil
		}
	}

	return empty_result, nil
}
