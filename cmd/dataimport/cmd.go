package dataimport

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"bytes"
	"encoding/csv"

	"github.com/spf13/cobra"
	"github.com/jmoiron/sqlx"

	// import postgres
	_ "github.com/lib/pq"
)

// Command is the cobra command.
var Command = &cobra.Command{
	Use:   "data-import",
	Short: "Import the password file to the table",
	RunE:  run,
}

type commandConfig struct {
	dsn        string
	first      int
	noTruncate bool
	pwdFile    string
	batchSize  int
}

var config = new(commandConfig)

func initFlags() {
	Command.Flags().StringVar(&config.dsn, "dsn", "", "Database connection string")
	Command.Flags().IntVar(&config.first, "first", 0, "If greater than 0, limits the import to first N lines")
	Command.Flags().BoolVar(&config.noTruncate, "no-truncate", false, "If set, do not truncate the table before import")
	Command.Flags().StringVar(&config.pwdFile, "password-file", "", "Password file path")
	Command.Flags().IntVar(&config.batchSize, "batch-size", 1000000, "Number of records to insert in one batch")
}

func init() {
	initFlags()
}

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

	file, err := os.Open(config.pwdFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error opening password file", err)
		os.Exit(1)
	}
	defer file.Close()

	// Create temporary buffer for batch processing
	var buffer bytes.Buffer
	csvWriter := csv.NewWriter(&buffer)
	csvWriter.Comma = '\t'
	
	currentLine := 0
	batchCount := 0
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		currentLine++

		if config.first > 0 && config.first < currentLine {
			break
		}

		parts := strings.Split(strings.TrimSpace(scanner.Text()), ":")
		if len(parts) != 2 {
			fmt.Fprintln(os.Stderr, "line", currentLine, "skipped, split by ':' did not result in 2 items")
			continue
		}
		
		hash := parts[0]
		count, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "line", currentLine, "skipped, error converting assumed count", parts[1], " as integer", err)
			continue
		}

		// Write to CSV buffer (partition_prefix, prefix, hash, count)
		partitionPrefix := hash[0:2]
		prefix := hash[0:5]
		csvWriter.Write([]string{
			partitionPrefix,
			prefix,
			hash,
			strconv.Itoa(count),
		})
		
		batchCount++

		// Flush batch if we've reached batch size
		if batchCount >= config.batchSize {
			if err := flushBatch(db, &buffer, csvWriter); err != nil {
				fmt.Fprintln(os.Stderr, "error flushing batch:", err)
				return err
			}
			batchCount = 0
			fmt.Printf("Imported %d lines\n", currentLine)
		}
	}

	// Flush any remaining records
	if batchCount > 0 {
		if err := flushBatch(db, &buffer, csvWriter); err != nil {
			fmt.Fprintln(os.Stderr, "error flushing final batch:", err)
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	return nil
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
