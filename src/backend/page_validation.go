// helper added at end of file via append
package main

import "fmt"

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
	// cycle check
	var isDesc func(ancestor, target string) bool
	isDesc = func(ancestor, target string) bool {
		rows, err := a.DB.Query(`SELECT id FROM pages WHERE parent_id=?`, ancestor)
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var cid string
			_ = rows.Scan(&cid)
			if cid == target {
				return true
			}
			if isDesc(cid, target) {
				return true
			}
		}
		return false
	}
	if isDesc(id, parentID) {
		return fmt.Errorf("circular")
	}
	return nil
}
