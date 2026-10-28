package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
)

// readDBRecordInTx constructs the full record response before committing a
// mutation. A broken row, property value, schema or rollup must roll back the
// record/version and backing-page changes instead of returning 500 after commit.
func readDBRecordInTx(tx *sql.Tx, id string, requirePageTitleMatch bool) (DBRecord, error) {
    var rec DBRecord
    var pageID sql.NullString
    var pageTitle string
    var pageVersion int
    err := tx.QueryRow(`SELECT r.id, r.database_id, r.title, r.page_id,
        r.version, r.created_at, r.updated_at, p.title, p.version
        FROM db_records r
        JOIN databases d ON d.id=r.database_id AND d.deleted_at IS NULL
        JOIN pages p ON p.id=r.page_id
        WHERE r.id=? AND r.deleted_at IS NULL`, id).
        Scan(&rec.ID, &rec.DatabaseID, &rec.Title, &pageID,
            &rec.Version, &rec.CreatedAt, &rec.UpdatedAt,
            &pageTitle, &pageVersion)
    if err != nil {
        return rec, err
    }
    if !pageID.Valid || pageID.String == "" || rec.Version <= 0 || pageVersion <= 0 {
        return rec, fmt.Errorf("record or backing page has an invalid version or link")
    }
    if requirePageTitleMatch && pageTitle != rec.Title {
        return rec, fmt.Errorf("record and backing page titles do not match")
    }
    rec.PageID = pageID.String
    rec.Values = map[string]any{}

    values, err := tx.Query(`SELECT v.property_id, v.value
        FROM db_values v
        JOIN db_properties p ON p.id=v.property_id AND p.deleted_at IS NULL
        WHERE v.record_id=?`, id)
    if err != nil {
        return rec, err
    }
    for values.Next() {
        var propertyID, raw string
        if err := values.Scan(&propertyID, &raw); err != nil {
            values.Close()
            return rec, err
        }
        var value any
        if err := json.Unmarshal([]byte(raw), &value); err != nil {
            values.Close()
            return rec, fmt.Errorf("invalid stored JSON for property %s: %w", propertyID, err)
        }
        rec.Values[propertyID] = value
    }
    if err := values.Err(); err != nil {
        values.Close()
        return rec, err
    }
    if err := values.Close(); err != nil {
        return rec, err
    }

    props, err := tx.Query(`SELECT id, database_id, name, type, config,
        position, visible, version FROM db_properties
        WHERE database_id=? AND deleted_at IS NULL ORDER BY position ASC`, rec.DatabaseID)
    if err != nil {
        return rec, err
    }
    fields := []DBProperty{}
    for props.Next() {
        var p DBProperty
        var visible int
        if err := props.Scan(&p.ID, &p.DatabaseID, &p.Name, &p.Type,
            &p.Config, &p.Position, &visible, &p.Version); err != nil {
            props.Close()
            return rec, err
        }
        p.Visible = visible == 1
        fields = append(fields, p)
    }
    if err := props.Err(); err != nil {
        props.Close()
        return rec, err
    }
    if err := props.Close(); err != nil {
        return rec, err
    }
    appendDBSystemValues(rec.Values, fields, rec)
    if err := appendComputedValuesWithQuery(tx, fields, &rec); err != nil {
        return rec, err
    }
    return rec, nil
}
