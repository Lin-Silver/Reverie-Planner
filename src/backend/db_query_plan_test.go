package main

import (
	"strings"
	"testing"
)

// TestDatabaseQueryPlansUseIndexes guards the migration-created indexes against
// accidental removal or schema drift. It asserts each hot query is planned as an
// index lookup (SEARCH ... USING [COVERING] INDEX) rather than a full table
// SCAN, and that the expected index is the one chosen. This is a plan-shape
// check, not a 10k-record performance benchmark.
func TestDatabaseQueryPlansUseIndexes(t *testing.T) {
	app := setupTestContext(t)
	defer app.DB.Close()

	// Each expected index must exist in the live schema.
	wantIndexes := []string{
		"idx_db_records_database_created",
		"idx_db_records_live",
		"idx_db_values_property",
		"idx_db_properties_order",
		"idx_db_properties_live",
		"idx_db_views_order",
		"idx_db_views_live",
		"idx_db_files_record",
		"idx_db_files_database",
	}
	for _, idx := range wantIndexes {
		var name string
		err := app.DB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil || name != idx {
			t.Fatalf("missing index %q: name=%q err=%v", idx, name, err)
		}
	}

	cases := []struct {
		name, query, wantIndex string
	}{
		{
			name:      "live records by database ordered by created",
			query:     `SELECT id FROM db_records WHERE database_id='d' AND deleted_at IS NULL ORDER BY created_at`,
			wantIndex: "idx_db_records_live",
		},
		{
			name:      "values by property",
			query:     `SELECT record_id, property_id FROM db_values WHERE property_id='p'`,
			wantIndex: "idx_db_values_property",
		},
		{
			name:      "live properties by database ordered by position",
			query:     `SELECT id FROM db_properties WHERE database_id='d' AND deleted_at IS NULL ORDER BY position`,
			wantIndex: "idx_db_properties_live",
		},
		{
			name:      "live views by database ordered by position",
			query:     `SELECT id FROM db_views WHERE database_id='d' AND deleted_at IS NULL ORDER BY position`,
			wantIndex: "idx_db_views_live",
		},
		{
			name:      "files by record",
			query:     `SELECT id FROM db_files WHERE record_id='r' AND deleted_at IS NULL`,
			wantIndex: "idx_db_files_record",
		},
		{
			name:      "files by database",
			query:     `SELECT id FROM db_files WHERE database_id='d'`,
			wantIndex: "idx_db_files_database",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := app.DB.Query("EXPLAIN QUERY PLAN " + tc.query)
			if err != nil {
				t.Fatalf("explain %q: %v", tc.query, err)
			}
			defer rows.Close()
			var plan strings.Builder
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plan.WriteString(detail)
				plan.WriteString("\n")
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("plan rows: %v", err)
			}
			got := plan.String()
			if !strings.Contains(got, tc.wantIndex) {
				t.Fatalf("plan for %q did not use %q:\n%s", tc.query, tc.wantIndex, got)
			}
			if strings.Contains(got, "SCAN db_") {
				t.Fatalf("plan for %q performs a full table SCAN:\n%s", tc.query, got)
			}
		})
	}
}