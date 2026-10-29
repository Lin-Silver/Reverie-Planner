package main

import (
    "net/http"
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


func TestTrashedDatabaseDoesNotExposeOrAcceptChildRecords(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,record:=dbRegressionCreate(t,ctx)
    removed:=dbRegressionCall(t,ctx,"DELETE","/api/databases?id="+db.ID,"",ctx.handleDatabases)
    if removed.Code!=204{t.Fatalf("delete database: %d %s",removed.Code,removed.Body.String())}
    list:=dbRegressionCall(t,ctx,"GET","/api/db/records?databaseId="+db.ID,"",ctx.handleDBRecords)
    if list.Code!=404{t.Fatalf("trashed db still permits record listing: %d %s",list.Code,list.Body.String())}
    if _,err:=ctx.getDBRecord(record.ID);err==nil{t.Fatal("trashed database record still readable by ID")}
    put:=dbRegressionCall(t,ctx,"PUT","/api/db/records",`{"id":"`+record.ID+`","title":"Changed","version":1,"values":{}}`,ctx.handleDBRecords)
    if put.Code!=404{t.Fatalf("trashed database accepted record update: %d %s",put.Code,put.Body.String())}
    post:=dbRegressionCall(t,ctx,"POST","/api/db/records",`{"databaseId":"`+db.ID+`","title":"Hidden creation"}`,ctx.handleDBRecords)
    if post.Code!=404{t.Fatalf("trashed database accepted record creation: %d %s",post.Code,post.Body.String())}
    var count int
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_records WHERE database_id=?",db.ID).Scan(&count);err!=nil{t.Fatal(err)}
    if count!=1{t.Fatalf("rejected creation modified database: %d",count)}
    recovered:=dbRegressionCall(t,ctx,"POST","/api/db/trash",`{"kind":"database","id":"`+db.ID+`"}`,ctx.handleDBTrash)
    if recovered.Code!=200{t.Fatalf("restore database: %d %s",recovered.Code,recovered.Body.String())}
    restored,err:=ctx.getDBRecord(record.ID)
    if err!=nil || restored.ID!=record.ID || restored.Title!=record.Title{t.Fatalf("restored record missing: %+v err=%v",restored,err)}
}


func TestTrashedDatabaseRejectsPropertyViewAndLinkedMutations(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    database,_:=dbRegressionCreate(t,ctx)
    removed:=dbRegressionCall(t,ctx,"DELETE","/api/databases?id="+database.ID,"",ctx.handleDatabases)
    if removed.Code!=204{t.Fatalf("delete database: %d %s",removed.Code,removed.Body.String())}
    cases:=[]struct{
        method,path,body string
        handler http.HandlerFunc
        expected int
    }{
        {"POST","/api/db/properties",`{"databaseId":"`+database.ID+`","name":"Stale","type":"text"}`,ctx.handleDBProperties,404},
        {"POST","/api/db/views",`{"databaseId":"`+database.ID+`","name":"Stale","type":"table"}`,ctx.handleDBViews,404},
        {"POST","/api/db/linked-view",`{"databaseId":"`+database.ID+`","pageId":"`+database.PageID+`"}`,ctx.handleDBLinkedView,404},
    }
    for _,c:=range cases {
        res:=dbRegressionCall(t,ctx,c.method,c.path,c.body,c.handler)
        if res.Code!=c.expected{t.Fatalf("%s %s: expected %d, got %d %s",c.method,c.path,c.expected,res.Code,res.Body.String())}
    }
    views:=dbRegressionCall(t,ctx,"PUT","/api/db/views",`{"id":"`+database.Views[0].ID+`","databaseId":"`+database.ID+`","name":"Stale","type":"table","config":"{}","version":1}`,ctx.handleDBViews)
    if views.Code!=409{t.Fatalf("stale view update accepted: %d %s",views.Code,views.Body.String())}
    var properties,viewCount int
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_properties WHERE database_id=?",database.ID).Scan(&properties);err!=nil{t.Fatal(err)}
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_views WHERE database_id=?",database.ID).Scan(&viewCount);err!=nil{t.Fatal(err)}
    if properties!=1 || viewCount!=1{t.Fatalf("deleted database mutated: properties=%d views=%d",properties,viewCount)}
}
