package internal

import "fmt"

// NewCRDMigrationStatusStore is only available in migrationfull builds.
func NewCRDMigrationStatusStore() (MigrationStatusStore, error) {
	return nil, fmt.Errorf("CRD migration store unavailable: rebuild with -tags migrationfull")
}
