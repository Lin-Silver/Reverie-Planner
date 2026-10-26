package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "strings"
)

// validateDBReferenceValue runs inside the record update transaction.
// References are stable record/member IDs, not titles supplied by clients.
func validateDBReferenceValue(tx *sql.Tx, kind, config string, val any) error {
    if val == nil { return nil }
    switch kind {
    case "person":
        id, ok := val.(string)
        if !ok { return fmt.Errorf("person must be a member ID") }
        if id == "" { return nil }
        var count int
        if err := tx.QueryRow("SELECT COUNT(*) FROM members WHERE id=?",id).Scan(&count);err!=nil {return err}
        if count!=1{return fmt.Errorf("member does not exist")}
    case "relation":
        var def struct { TargetDatabaseID string `json:"targetDatabaseId"` }
        if err:=json.Unmarshal([]byte(config),&def);err!=nil || def.TargetDatabaseID=="" {return fmt.Errorf("relation targetDatabaseId is required")}
        values,ok:=val.([]any)
        if !ok{return fmt.Errorf("relation must contain an array of record IDs")}
        if len(values)>500{return fmt.Errorf("too many relation targets")}
        used:=map[string]bool{}
        for _,v:=range values {
            id,ok:=v.(string)
            if !ok || strings.TrimSpace(id)=="" {return fmt.Errorf("invalid relation record ID")}
            if used[id]{return fmt.Errorf("duplicate relation record ID")}
            used[id]=true
            var count int
            if err:=tx.QueryRow("SELECT COUNT(*) FROM db_records WHERE id=? AND database_id=?",id,def.TargetDatabaseID).Scan(&count);err!=nil{return err}
            if count!=1{return fmt.Errorf("relation record is missing from target database")}
        }
    }
    return nil
}

func appendDBSystemValues(values map[string]any, fields []DBProperty, rec DBRecord) {
    for _, field := range fields {
        switch field.Type {
        case "created_time":
            values[field.ID]=rec.CreatedAt
        case "last_edited_time":
            values[field.ID]=rec.UpdatedAt
        }
    }
}
