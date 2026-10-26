package main

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestDatabaseTrashRestoresRecordAndPreservesValues(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,rec:=dbRegressionCreate(t,ctx)
    p:=computedProperty(t,ctx,db.ID,"Note","text",map[string]any{})
    computedUpdate(t,ctx,rec,map[string]any{p.ID:"keep this"})
    updated,err:=ctx.getDBRecord(rec.ID);if err!=nil{t.Fatal(err)}
    removed:=dbRegressionCall(t,ctx,"DELETE","/api/db/records?id="+rec.ID,"",ctx.handleDBRecords)
    if removed.Code!=204{t.Fatalf("delete record: %d %s",removed.Code,removed.Body.String())}
    if _,err:=ctx.getDBRecord(rec.ID);err==nil{t.Fatal("deleted record is visible to active query")}
    trash:=dbRegressionCall(t,ctx,"GET","/api/db/trash?kind=record&databaseId="+db.ID,"",ctx.handleDBTrash)
    if trash.Code!=200 || !strings.Contains(trash.Body.String(),rec.ID){t.Fatalf("record absent from trash: %d %s",trash.Code,trash.Body.String())}
    var stored int
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?",rec.ID,p.ID).Scan(&stored);err!=nil{t.Fatal(err)}
    if stored!=1{t.Fatal("delete destroyed record property values")}
    restored:=dbRegressionCall(t,ctx,"POST","/api/db/trash",`{"kind":"record","id":"`+rec.ID+`"}`,ctx.handleDBTrash)
    if restored.Code!=200{t.Fatalf("restore record: %d %s",restored.Code,restored.Body.String())}
    got,err:=ctx.getDBRecord(rec.ID);if err!=nil{t.Fatal(err)}
    if got.Values[p.ID]!="keep this"||got.Version<=updated.Version {t.Fatalf("restoration lost data/version: %+v",got)}
}
func TestDatabaseTrashRestoresPropertyAndDatabase(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,rec:=dbRegressionCreate(t,ctx)
    p:=computedProperty(t,ctx,db.ID,"Note","text",map[string]any{})
    computedUpdate(t,ctx,rec,map[string]any{p.ID:"retain"})
    removed:=dbRegressionCall(t,ctx,"DELETE","/api/db/properties?id="+p.ID,"",ctx.handleDBProperties)
    if removed.Code!=204{t.Fatalf("delete property: %d %s",removed.Code,removed.Body.String())}
    current,err:=ctx.getDatabase(db.ID);if err!=nil{t.Fatal(err)}
    for _,field:=range current.Properties {if field.ID==p.ID{t.Fatal("deleted property still visible")}}
    back:=dbRegressionCall(t,ctx,"POST","/api/db/trash",`{"kind":"property","id":"`+p.ID+`"}`,ctx.handleDBTrash)
    if back.Code!=200{t.Fatalf("property restore: %d %s",back.Code,back.Body.String())}
    current,err=ctx.getDatabase(db.ID);if err!=nil{t.Fatal(err)}
    present:=false;for _,field:=range current.Properties{if field.ID==p.ID{present=true}}
    if !present{t.Fatal("property absent after restore")}
    record,err:=ctx.getDBRecord(rec.ID);if err!=nil{t.Fatal(err)}
    if record.Values[p.ID]!="retain"{t.Fatalf("restored property lost data: %+v",record.Values)}
    deletedDB:=dbRegressionCall(t,ctx,"DELETE","/api/databases?id="+db.ID,"",ctx.handleDatabases)
    if deletedDB.Code!=204{t.Fatalf("database delete: %d %s",deletedDB.Code,deletedDB.Body.String())}
    if _,err:=ctx.getDatabase(db.ID);err==nil{t.Fatal("deleted database visible")}
    restoreDB:=dbRegressionCall(t,ctx,"POST","/api/db/trash",`{"kind":"database","id":"`+db.ID+`"}`,ctx.handleDBTrash)
    if restoreDB.Code!=200{t.Fatalf("database restore: %d %s",restoreDB.Code,restoreDB.Body.String())}
    if _,err:=ctx.getDatabase(db.ID);err!=nil{t.Fatal(err)}
}
func TestDatabaseTrashMigrationNine(t *testing.T) {
    ctx:=setupTestContext(t)
    defer ctx.DB.Close()
    var count int
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=9").Scan(&count);err!=nil{t.Fatal(err)}
    if count!=1{t.Fatal("soft-delete migration must be applied exactly once")}
    for _,table:=range []string{"databases","db_records","db_properties","db_views"} {
        var dummy int
        if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE deleted_at IS NOT NULL").Scan(&dummy);err!=nil{t.Fatalf("missing deleted_at in %s: %v",table,err)}
    }
}
func TestDatabaseRestoreRequestRejectsUnknownKind(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    body,_:=json.Marshal(map[string]any{"kind":"pages","id":"arbitrary"})
    res:=dbRegressionCall(t,ctx,"POST","/api/db/trash",string(body),ctx.handleDBTrash)
    if res.Code!=400{t.Fatalf("invalid restore kind accepted: %d",res.Code)}
}
