package main

import (
 "database/sql"
 "encoding/json"
 "errors"
 "net/http"
 "strings"
 "time"
)

type dbBulkEdit struct {
 ID string `json:"id"`
 Version int `json:"version"`
 Title *string `json:"title,omitempty"`
 Values map[string]any `json:"values,omitempty"`
}

// All records and backing pages change in one SQLite transaction. A stale
// version, invalid value, missing owner or failed readback rolls everything back.
func (a *AppContext) handleDBBulkEdit(w http.ResponseWriter, r *http.Request) {
 if r.Method!=http.MethodPost { w.WriteHeader(405); return }
 var req struct {
  DatabaseID string `json:"databaseId"`
  Edits []dbBulkEdit `json:"edits"`
 }
 if _,err:=decodeJSONRequest(r,&req);err!=nil {writeJSON(w,400,map[string]string{"error":"invalid request JSON"});return}
 if req.DatabaseID=="" || len(req.Edits)==0 || len(req.Edits)>100 {
  writeJSON(w,400,map[string]string{"error":"databaseId and 1-100 edits required"});return
 }
 seen:=map[string]bool{}
 for _,e:=range req.Edits {
  if e.ID=="" || e.Version<=0 || seen[e.ID] || (e.Title==nil && len(e.Values)==0) || len(e.Values)>100 {
   writeJSON(w,400,map[string]string{"error":"invalid, duplicate or empty record edit"});return
  }
  if e.Title!=nil && strings.TrimSpace(*e.Title)=="" {writeJSON(w,400,map[string]string{"error":"title cannot be empty"});return}
  seen[e.ID]=true
 }
 tx,err:=a.DB.Begin()
 if err!=nil {writeJSON(w,500,map[string]string{"error":"could not begin bulk edit"});return}
 defer tx.Rollback()
 var active int
 if err:=tx.QueryRow("SELECT COUNT(*) FROM databases WHERE id=? AND deleted_at IS NULL",req.DatabaseID).Scan(&active);err!=nil {
  writeJSON(w,500,map[string]string{"error":"could not inspect database"});return
 }
 if active!=1 {writeJSON(w,404,map[string]string{"error":"database not found"});return}
 type definition struct{ kind,config string }
 defs:=map[string]definition{}
 rows,err:=tx.Query("SELECT id,type,config FROM db_properties WHERE database_id=? AND deleted_at IS NULL",req.DatabaseID)
 if err!=nil {writeJSON(w,500,map[string]string{"error":"could not read properties"});return}
 for rows.Next() {
  var id,kind,config string
  if err:=rows.Scan(&id,&kind,&config);err!=nil {rows.Close();writeJSON(w,500,map[string]string{"error":"could not scan properties"});return}
  defs[id]=definition{kind,config}
 }
 if err:=rows.Err();err!=nil {rows.Close();writeJSON(w,500,map[string]string{"error":"could not scan properties"});return}
 if err:=rows.Close();err!=nil {writeJSON(w,500,map[string]string{"error":"could not close properties"});return}
 now:=time.Now().UTC().Format(time.RFC3339)
 updated:=make([]DBRecord,0,len(req.Edits))
 titleChangedByID:=make(map[string]bool,len(req.Edits))
 for _,e:=range req.Edits {
  owner,previousTitle,err:=readWritableDBRecordForUpdate(tx,e.ID)
  if err!=nil {
   if errors.Is(err,sql.ErrNoRows){writeJSON(w,404,map[string]string{"error":"record not found"})} else {writeJSON(w,500,map[string]string{"error":"could not inspect record"})}
   return
  }
  if owner!=req.DatabaseID {writeJSON(w,400,map[string]string{"error":"record belongs to another database"});return}
  title:=previousTitle
  if e.Title!=nil {title=*e.Title}
  res,err:=tx.Exec("UPDATE db_records SET title=?,version=version+1,updated_at=? WHERE id=? AND database_id=? AND version=? AND deleted_at IS NULL",title,now,e.ID,req.DatabaseID,e.Version)
  if err!=nil {writeJSON(w,500,map[string]string{"error":"could not update record"});return}
  count,err:=res.RowsAffected()
  if err!=nil {writeJSON(w,500,map[string]string{"error":"could not verify record update"});return}
  if count!=1 {writeJSON(w,409,map[string]string{"error":"record version conflict; no edits saved"});return}
  for pid,value:=range e.Values {
   def,ok:=defs[pid]
   if !ok || def.kind=="title" {writeJSON(w,400,map[string]string{"error":"unknown or title property"});return}
   if err:=validatePropertyValue(def.kind,value);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
   if err:=validateConfiguredPropertyValue(def.kind,def.config,value);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
   if err:=validateDBReferenceValue(tx,def.kind,def.config,value);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
   if def.kind=="file" {
    if err:=validateDBFileReferences(tx,e.ID,value);err!=nil {writeJSON(w,400,map[string]string{"error":err.Error()});return}
   }
   raw,err:=json.Marshal(value)
   if err!=nil {writeJSON(w,400,map[string]string{"error":"invalid value"});return}
   var exists int
   if err:=tx.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?",e.ID,pid).Scan(&exists);err!=nil {
    writeJSON(w,500,map[string]string{"error":"could not inspect property value"});return
   }
   if exists>0 {
    _,err=tx.Exec("UPDATE db_values SET value=?,version=version+1,updated_at=? WHERE record_id=? AND property_id=?",string(raw),now,e.ID,pid)
   } else {
    _,err=tx.Exec("INSERT INTO db_values(record_id,property_id,value,version,updated_at) VALUES(?,?,?,1,?)",e.ID,pid,string(raw),now)
   }
   if err!=nil {writeJSON(w,500,map[string]string{"error":"could not save property value"});return}
  }
  titleChanged:=title!=previousTitle
  if titleChanged {
   pageRes,err:=tx.Exec("UPDATE pages SET title=?,updated_at=?,version=version+1 WHERE id=(SELECT page_id FROM db_records WHERE id=?)",title,now,e.ID)
   if err!=nil {writeJSON(w,500,map[string]string{"error":"could not update backing page"});return}
   changed,err:=pageRes.RowsAffected()
   if err!=nil || changed!=1 {writeJSON(w,409,map[string]string{"error":"backing page missing"});return}
  }
  titleChangedByID[e.ID]=titleChanged
 }
 // Materialize all responses only after the entire batch has been applied:
 // a rollup on an earlier record may depend on a later record's new value.
 for _,e:=range req.Edits {
  rec,err:=readDBRecordInTx(tx,e.ID,titleChangedByID[e.ID])
  if err!=nil {writeJSON(w,500,map[string]string{"error":"could not read updated record"});return}
  updated=append(updated,rec)
 }
 if err:=tx.Commit();err!=nil {writeJSON(w,500,map[string]string{"error":"could not commit bulk edit"});return}
 writeJSON(w,200,updated)
}
