package dataimport

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/jmoiron/sqlx"

	_ "github.com/lib/pq"
)

// Command is the cobra command.
var Command = &cobra.Command{
	Use:   "data-import",
	Short: "Import password hashes from HIBP API to the table",
	RunE:  run,
}

type commandConfig struct {
	dsn        string
	noTruncate bool
	batchSize  int
}

var config = new(commandConfig)

func initFlags() {
	Command.Flags().StringVar(&config.dsn, "dsn", "", "Database connection string")
	Command.Flags().BoolVar(&config.noTruncate, "no-truncate", false, "If set, do not truncate the table before import")
	Command.Flags().IntVar(&config.batchSize, "batch-size", 1000000, "Number of records to insert in one batch")
}

func init() {
	initFlags()
}

func fetchRange(prefix string) ([]string, error) {
	url := fmt.Sprintf("https://api.pwnedpasswords.com/range/%s", prefix)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(body), "\r\n"), nil
}

type workItem struct {
	prefix string
}

type result struct {
	prefix string
	hashes []string
	err    error
}

const (
	numWorkers = 32  // Can be adjusted based on your needs
	queueSize  = 100 // Buffer size for channels
	
	maxRetries     = 3           // Maximum number of retry attempts
	initialBackoff = time.Second // Initial backoff duration
)

func run(cmd *cobra.Command, _ []string) error {
	db, err := sqlx.Connect("postgres", config.dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error establishing database connection", err)
		os.Exit(1)
	}
	defer db.Close()

	if !config.noTruncate {
		_, sqlErr := db.Exec("truncate table hibp restart identity")
		if sqlErr != nil {
			fmt.Fprintln(os.Stderr, "error truncating SQL table", sqlErr)
			os.Exit(1)
		}
	}

	// Create channels for work distribution and results
	work := make(chan workItem, queueSize)
	results := make(chan result, queueSize)
	done := make(chan bool)

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(work, results, &wg)
	}

	// Start result processor
	go processResults(db, results, done)

	// Generate and send work items
	go func() {
		for i := 0; i < 16*16*16*16*16; i++ {
			prefix := fmt.Sprintf("%05X", i)
			work <- workItem{prefix: prefix}
		}
		close(work)
	}()

	// Wait for all workers to complete
	wg.Wait()
	close(results)

	// Wait for result processor to complete
	<-done

	return nil
}

// Add retry helper function
func fetchRangeWithRetry(prefix string) ([]string, error) {
	var lastErr error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2 // Exponential backoff
		}

		hashes, err := fetchRange(prefix)
		if err == nil {
			return hashes, nil
		}

		lastErr = err
		
		// Only retry on specific errors (e.g., 429 Too Many Requests or network errors)
		if _, ok := err.(*url.Error); ok {
			// Network errors should be retried
			continue
		} else if strings.Contains(err.Error(), "429") {
			// Rate limit errors should be retried
			continue
		} else if strings.Contains(err.Error(), "500") || 
			  strings.Contains(err.Error(), "502") || 
			  strings.Contains(err.Error(), "503") || 
			  strings.Contains(err.Error(), "504") {
			// Server errors should be retried
			continue
		}
		
		// Don't retry other types of errors
		return nil, err
	}

	return nil, fmt.Errorf("after %d attempts, last error: %v", maxRetries+1, lastErr)
}

// Modify the worker function to use retry logic
func worker(work <-chan workItem, results chan<- result, wg *sync.WaitGroup) {
	defer wg.Done()

	for item := range work {
		hashes, err := fetchRangeWithRetry(item.prefix[:5])
		results <- result{
			prefix: item.prefix,
			hashes: hashes,
			err:    err,
		}
	}
}

// Add new result processor function
func processResults(db *sqlx.DB, results <-chan result, done chan<- bool) {
	var buffer bytes.Buffer
	csvWriter := csv.NewWriter(&buffer)
	csvWriter.Comma = '\t'

	currentLine := 0
	batchCount := 0

	for res := range results {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "error fetching range for prefix %s: %v\n", res.prefix, res.err)
			continue
		}

		for _, line := range res.hashes {
			currentLine++
			
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) != 2 {
				fmt.Fprintln(os.Stderr, "line", currentLine, "skipped, split by ':' did not result in 2 items")
				continue
			}

			suffix := parts[0]
			count, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "line", currentLine, "skipped, error converting count", parts[1], "as integer", err)
				continue
			}

			fullHash := res.prefix + suffix
			partitionPrefix := fullHash[0:2]
			hashPrefix := fullHash[0:5]

			csvWriter.Write([]string{
					partitionPrefix,
					hashPrefix,
					fullHash,
					strconv.Itoa(count),
			})

			batchCount++

			if batchCount >= config.batchSize {
				if err := flushBatch(db, &buffer, csvWriter); err != nil {
					fmt.Fprintln(os.Stderr, "error flushing batch:", err)
					continue
				}
				batchCount = 0
				fmt.Printf("Imported %d lines\n", currentLine)
			}
		}
	}

	// Flush any remaining records
	if batchCount > 0 {
		if err := flushBatch(db, &buffer, csvWriter); err != nil {
			fmt.Fprintln(os.Stderr, "error flushing final batch:", err)
		}
	}

	done <- true
}

// Add new helper function for batch flushing
func flushBatch(db *sqlx.DB, buffer *bytes.Buffer, csvWriter *csv.Writer) error {
	csvWriter.Flush()
	if csvWriter.Error() != nil {
		return csvWriter.Error()
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		COPY hibp(partition_prefix, prefix, hash, count) 
		FROM STDIN WITH (FORMAT csv, DELIMITER E'\t')
	`, buffer.String())

	if err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	buffer.Reset()
	return nil
}
