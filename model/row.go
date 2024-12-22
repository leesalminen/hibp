package model

// Row represents data row.
type Row struct {
	PartitionPrefix string `db:"partition_prefix"`
	Prefix          string `db:"prefix"`
	Hash            string `db:"hash"`
	Count           int    `db:"count"`
}
