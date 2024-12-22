package migrate

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jmoiron/sqlx"

	// import postgres
	_ "github.com/lib/pq"
)

// Command is the cobra command.
var Command = &cobra.Command{
	Use:   "migrate",
	Short: "Create the SQL table required to run this program",
	RunE:  run,
}

type commandConfig struct {
	dsn string
}

var config = new(commandConfig)

func initFlags() {
	Command.Flags().StringVar(&config.dsn, "dsn", "", "Database connection string")
}

func init() {
	initFlags()
}

func generatePartitionSchema() string {
	baseSchema := `
CREATE TABLE public.hibp (
	row_id serial NOT NULL,
	partition_prefix varchar(2) NOT NULL,
	prefix varchar(5) NOT NULL,
	hash varchar(40) NOT NULL,
	count integer NOT NULL,
	CONSTRAINT hibp_pkey PRIMARY KEY (row_id, partition_prefix, prefix)
) PARTITION BY LIST (partition_prefix);
`
	
	var partitions, indexes strings.Builder
	
	// Generate partitions and indexes for all hex prefixes (00-FF)
	for i := 0; i <= 255; i++ {
		prefix := fmt.Sprintf("%02X", i)
		
		// Create partition
		partitions.WriteString(fmt.Sprintf(
			"CREATE TABLE hibp_prefix_%s PARTITION OF hibp FOR VALUES IN ('%s');\n",
			prefix, prefix,
		))
		
		// Create index for the partition
		indexes.WriteString(fmt.Sprintf(
			"CREATE INDEX hibp_prefix_idx_%s ON hibp_prefix_%s (prefix);\n",
			prefix, prefix,
		))
	}
	
	return baseSchema + partitions.String() + indexes.String()
}

func run(cmd *cobra.Command, _ []string) error {
	db, err := sqlx.Connect("postgres", config.dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error establishing database connection", err)
		os.Exit(1)
	}
	defer db.Close()

	// Generate and execute the schema
	schema := generatePartitionSchema()
	_, schemaErr := db.Exec(schema)
	if schemaErr != nil {
		fmt.Fprintln(os.Stderr, "error creating schema", schemaErr)
		os.Exit(1)
	}

	return nil
}
