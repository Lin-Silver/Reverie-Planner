package main

import (
	"fmt"
	"time"
)

func (a *AppContext) validatePageParent(id, parentID string) error {
	if parentID == "" {
		return nil
	}
	if parentID == id {
		return fmt.Errorf("self parent")
	}
	// existence
	var exists int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM pages WHERE id=?`, parentID).Scan(&exists); err != nil || exists == 0 {
		return fmt.Errorf("parent not found")
	}
	// cycle check via recursive CTE: is parentID a descendant of id?
	var cycle int
	err := a.DB.QueryRow(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM pages WHERE parent_id = ?
			UNION ALL
			SELECT p.id FROM pages p JOIN descendants d ON p.parent_id = d.id
		)
		SELECT COUNT(*) FROM descendants WHERE id = ?
	`, id, parentID).Scan(&cycle)
	if err != nil {
		return fmt.Errorf("validation error: %v", err)
	}
	if cycle > 0 {
		return fmt.Errorf("circular")
	}
	return nil
}

func (a *AppContext) movePageAtomic(id, parentID string, version int) (int, error) {
	tx, err := a.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// existence
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id=?`, id).Scan(&exists); err != nil || exists == 0 {
		return 0, fmt.Errorf("not found")
	}
	var currentVersion int
	if err := tx.QueryRow(`SELECT version FROM pages WHERE id=?`, id).Scan(&currentVersion); err != nil {
		return 0, fmt.Errorf("not found")
	}
	if currentVersion != version {
		return 0, fmt.Errorf("version conflict")
	}

	// parent validation within transaction
	if parentID != "" {
		if parentID == id {
			return 0, fmt.Errorf("self parent")
		}
		var pexists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id=?`, parentID).Scan(&pexists); err != nil || pexists == 0 {
			return 0, fmt.Errorf("parent not found")
		}
		var cycle int
		err := tx.QueryRow(`
			WITH RECURSIVE descendants(id) AS (
				SELECT id FROM pages WHERE parent_id = ?
				UNION ALL
				SELECT p.id FROM pages p JOIN descendants d ON p.parent_id = d.id
			)
			SELECT COUNT(*) FROM descendants WHERE id = ?
		`, id, parentID).Scan(&cycle)
		if err != nil {
			return 0, fmt.Errorf("validation error: %v", err)
		}
		if cycle > 0 {
			return 0, fmt.Errorf("circular")
		}
	}

	newVersion := currentVersion + 1
	parentVal := any(nil)
	if parentID != "" {
		parentVal = parentID
	}
	res, err := tx.Exec(`UPDATE pages SET parent_id=?, updated_at=?, version=? WHERE id=? AND version=?`,
		parentVal, timeNowUTC(), newVersion, id, version)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return 0, fmt.Errorf("version conflict")
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newVersion, nil
}

func timeNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
