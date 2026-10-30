package main

import (
    "database/sql"
    "encoding/json"
    "errors"
    "net/http"
    "time"
)

type trashItem struct {
    ID string `json:"id"`
    Title string `json:"title"`
    DeletedAt string `json:"deletedAt"`
}
var trashSources=map[string]struct{table,title string}{
    "database":{"databases","title"},
    "record":{"db_records","title"},
    "property":{"db_properties","name"},
    "view":{"db_views","name"},
}
func (a *AppContext) handleDBTrash(w http.ResponseWriter,r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        kind:=r.URL.Query().Get("kind")
        source,ok:=trashSources[kind]
        if !ok {writeJSON(w,400,map[string]string{"error":"invalid trash kind"});return}
        databaseID:=r.URL.Query().Get("databaseId")
        query:="SELECT id,"+source.title+",deleted_at FROM "+source.table+" WHERE deleted_at IS NOT NULL"
        args:=[]any{}
        if kind!="database"{
            if databaseID==""{writeJSON(w,400,map[string]string{"error":"databaseId required"});return}
            query+=" AND database_id=?"
            args=append(args,databaseID)
        }
        query+=" ORDER BY deleted_at DESC LIMIT 250"
        rows,err:=a.DB.Query(query,args...)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        items:=[]trashItem{}
        for rows.Next(){
            var item trashItem
            if err:=rows.Scan(&item.ID,&item.Title,&item.DeletedAt);err!=nil{rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
            items=append(items,item)
        }
        if err:=rows.Err();err!=nil{rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
        rows.Close()
        writeJSON(w,200,items)
    case http.MethodPost:
        var request struct{Kind string `json:"kind"`;ID string `json:"id"`}
        if _,err:=decodeJSONRequest(r,&request);err!=nil {writeJSON(w,400,map[string]string{"error":"invalid request JSON"});return}
        source,ok:=trashSources[request.Kind]
        if !ok || request.ID==""{writeJSON(w,400,map[string]string{"error":"kind and id required"});return}
        tx,err:=a.DB.Begin()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        defer tx.Rollback()
        var databaseID string
        if request.Kind=="database"{
            var pageID string
            if err:=tx.QueryRow("SELECT page_id FROM databases WHERE id=? AND deleted_at IS NOT NULL",request.ID).Scan(&pageID);err!=nil{
                if errors.Is(err,sql.ErrNoRows) {writeJSON(w,404,map[string]string{"error":"deleted database not found"})} else {writeJSON(w,500,map[string]string{"error":"could not inspect deleted database"})}
                return
            }
            var pageCount int
            if err:=tx.QueryRow("SELECT COUNT(*) FROM pages WHERE id=?",pageID).Scan(&pageCount);err!=nil{writeJSON(w,500,map[string]string{"error":"could not inspect backing database page"});return}
            if pageCount!=1{writeJSON(w,409,map[string]string{"error":"backing database page unavailable"});return}
        } else {
            if err:=tx.QueryRow("SELECT database_id FROM "+source.table+" WHERE id=? AND deleted_at IS NOT NULL",request.ID).Scan(&databaseID);err!=nil{
                if errors.Is(err,sql.ErrNoRows) {writeJSON(w,404,map[string]string{"error":"deleted item not found"})} else {writeJSON(w,500,map[string]string{"error":"could not inspect deleted item"})}
                return
            }
            var count int
            if err:=tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",databaseID).Scan(&count);err!=nil{writeJSON(w,500,map[string]string{"error":"could not inspect parent database"});return}
            if count!=1{writeJSON(w,409,map[string]string{"error":"restore database first"});return}
            if request.Kind=="record"{
                var pageID sql.NullString
                if err:=tx.QueryRow("SELECT page_id FROM db_records WHERE id=?",request.ID).Scan(&pageID);err!=nil{writeJSON(w,500,map[string]string{"error":err.Error()});return}
                if pageID.Valid{
                    var count int
                    if err:=tx.QueryRow("SELECT COUNT(*) FROM pages WHERE id=?",pageID.String).Scan(&count);err!=nil{writeJSON(w,500,map[string]string{"error":"could not inspect backing record page"});return}
                    if count!=1{writeJSON(w,409,map[string]string{"error":"backing record page unavailable"});return}
                }
                // A deleted record retains its saved relation values. Check
                // active relation properties before exposing those references.
                valid,err:=validateRestoredRecordRelations(tx,request.ID,databaseID)
                if err!=nil{writeJSON(w,500,map[string]string{"error":"could not inspect saved record relations"});return}
                if !valid{writeJSON(w,409,map[string]string{"error":"restore referenced records or repair relation values first"});return}
            }
            // Restoring a relation property must not reactivate a reference to
            // a database that is still in trash. Preserve the property and
            // its values so the user can restore the target database first.
            if request.Kind=="property" {
                var propertyType, config string
                if err:=tx.QueryRow("SELECT type,config FROM db_properties WHERE id=? AND database_id=?",request.ID,databaseID).Scan(&propertyType,&config);err!=nil {
                    writeJSON(w,500,map[string]string{"error":"could not inspect deleted property"});return
                }
                if err:=validateDBComputedDefinition(tx,databaseID,propertyType,config);err!=nil {
                    if errors.Is(err,errInvalidComputedDefinition) {
                        writeJSON(w,409,map[string]string{"error":err.Error()})
                    } else {
                        writeJSON(w,500,map[string]string{"error":"could not inspect computed dependencies"})
                    }
                    return
                }
                if propertyType=="relation" {
                    var definition struct{TargetDatabaseID string `json:"targetDatabaseId"`}
                    // Legacy saved configurations must not resolve duplicate keys
                    // using encoding/json's last-key-wins behavior on restore.
                    if err:=rejectDuplicateJSONKeys([]byte(config));err!=nil {
                        writeJSON(w,409,map[string]string{"error":"relation target configuration is invalid"});return
                    }
                    if err:=json.Unmarshal([]byte(config),&definition);err!=nil || definition.TargetDatabaseID=="" {
                        writeJSON(w,409,map[string]string{"error":"relation target configuration is invalid"});return
                    }
                    var targetCount int
                    if err:=tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",definition.TargetDatabaseID).Scan(&targetCount);err!=nil {
                        writeJSON(w,500,map[string]string{"error":"could not validate relation target"});return
                    }
                    if targetCount!=1 {
                        writeJSON(w,409,map[string]string{"error":"restore relation target database first"});return
                    }
                    // A trashed relation property retains values belonging to both
                    // active and trashed source records. Do not expose references
                    // to records that are still deleted or have changed owners.
                    valid,err:=validateRestoredRelationValues(tx,request.ID,definition.TargetDatabaseID)
                    if err!=nil {
                        writeJSON(w,500,map[string]string{"error":"could not validate saved relation values"});return
                    }
                    if !valid {
                        writeJSON(w,409,map[string]string{"error":"restore referenced records or repair relation values first"});return
                    }
                }
            }
        }
        result,err:=tx.Exec("UPDATE "+source.table+" SET deleted_at=NULL,version=version+1 WHERE id=? AND deleted_at IS NOT NULL",request.ID)
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        changed,err:=result.RowsAffected()
        if err!=nil {writeJSON(w,500,map[string]string{"error":err.Error()});return}
        if changed!=1{writeJSON(w,409,map[string]string{"error":"trash item was already restored"});return}
        if err:=tx.Commit();err!=nil{writeJSON(w,500,map[string]string{"error":err.Error()});return}
        writeJSON(w,200,map[string]string{"id":request.ID,"status":"restored","at":time.Now().UTC().Format(time.RFC3339)})
    default:
        w.WriteHeader(405)
    }
}
