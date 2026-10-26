package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func dbRegressionCall(t *testing.T, ctx *AppContext, method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
    t.Helper()
    w := httptest.NewRecorder()
    handler(w, httptest.NewRequest(method, path, strings.NewReader(body)))
    return w
}

func dbRegressionCreate(t *testing.T, ctx *AppContext) (Database, DBRecord) {
    t.Helper()
    result := dbRegressionCall(t, ctx, "POST", "/api/databases", `{"title":"Regression"}`, ctx.handleDatabases)
    if result.Code != 201 { t.Fatalf("create database: %d %s", result.Code, result.Body.String()) }
    var db Database
    if err := json.Unmarshal(result.Body.Bytes(), &db); err != nil { t.Fatal(err) }
    result = dbRegressionCall(t, ctx, "POST", "/api/db/records", `{"databaseId":"` + db.ID + `","title":"Original"}`, ctx.handleDBRecords)
    if result.Code != 201 { t.Fatalf("create record: %d %s", result.Code, result.Body.String()) }
    var rec DBRecord
    if err := json.Unmarshal(result.Body.Bytes(), &rec); err != nil { t.Fatal(err) }
    return db, rec
}

func TestDatabaseRejectsInvalidConfiguredSelectAtomically(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    db, rec := dbRegressionCreate(t, ctx)
    cfg, err := json.Marshal(map[string]any{"options": []string{"Todo", "Done"}})
    if err != nil { t.Fatal(err) }
    payload, err := json.Marshal(map[string]any{"databaseId": db.ID, "name": "Status", "type": "select", "config": string(cfg)})
    if err != nil { t.Fatal(err) }
    prop := dbRegressionCall(t, ctx, "POST", "/api/db/properties", string(payload), ctx.handleDBProperties)
    if prop.Code != 201 { t.Fatalf("property: %d %s", prop.Code, prop.Body.String()) }
    var p DBProperty
    if err := json.Unmarshal(prop.Body.Bytes(), &p); err != nil { t.Fatal(err) }
    update := `{"id":"` + rec.ID + `","version":1,"title":"Corrupted","values":{"` + p.ID + `":"Invalid"}}`
    outcome := dbRegressionCall(t, ctx, "PUT", "/api/db/records", update, ctx.handleDBRecords)
    if outcome.Code != 400 { t.Fatalf("expected 400, got %d: %s", outcome.Code, outcome.Body.String()) }
    actual, err := ctx.getDBRecord(rec.ID)
    if err != nil { t.Fatal(err) }
    if actual.Title != "Original" || actual.Version != 1 || len(actual.Values) != 0 { t.Fatalf("non-atomic failed update: %+v", actual) }
}

func TestDatabaseRecordUpdateRejectsMissingLinkedPageAtomically(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    _, rec := dbRegressionCreate(t, ctx)
    if _, err := ctx.DB.Exec(`UPDATE db_records SET page_id=NULL WHERE id=?`, rec.ID); err != nil { t.Fatal(err) }
    response := dbRegressionCall(t, ctx, "PUT", "/api/db/records", `{"id":"` + rec.ID + `","title":"Broken","version":1,"values":{}}`, ctx.handleDBRecords)
    if response.Code < 400 { t.Fatalf("unexpected success: %s", response.Body.String()) }
    got, err := ctx.getDBRecord(rec.ID)
    if err != nil { t.Fatal(err) }
    if got.Title != "Original" || got.Version != 1 { t.Fatalf("updated despite missing page: %+v", got) }
}

func TestDatabaseListWithRecordsTerminates(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    dbRegressionCreate(t, ctx)
    result := make(chan *httptest.ResponseRecorder, 1)
    go func() { result <- dbRegressionCall(t, ctx, "GET", "/api/databases", "", ctx.handleDatabases) }()
    select {
    case res := <-result:
        if res.Code != 200 { t.Fatalf("list status %d: %s", res.Code, res.Body.String()) }
        var databases []Database
        if err := json.Unmarshal(res.Body.Bytes(), &databases); err != nil { t.Fatal(err) }
        if len(databases) == 0 || len(databases[0].Properties) == 0 || len(databases[0].Views) == 0 { t.Fatalf("incomplete db list: %+v", databases) }
    case <-time.After(3 * time.Second):
        t.Fatal("database list blocked (possible nested SQLite connection deadlock)")
    }
}


func TestDatabaseRejectsUnsupportedProperties(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    db, _ := dbRegressionCreate(t, ctx)
    cases := []string{
        `{"databaseId":"` + db.ID + `","name":"Danger","type":"unsupported"}`,
        `{"databaseId":"` + db.ID + `","name":"Bad","type":"select","config":"{not-json}"}`,
    }
    for _, body := range cases {
        res := dbRegressionCall(t, ctx, "POST", "/api/db/properties", body, ctx.handleDBProperties)
        if res.Code != 400 { t.Fatalf("invalid property accepted: %d %s", res.Code, res.Body.String()) }
    }
    current, err := ctx.getDatabase(db.ID)
    if err != nil { t.Fatal(err) }
    if len(current.Properties) != 1 || current.Properties[0].Type != "title" { t.Fatalf("invalid properties affected schema: %+v", current.Properties) }
}

func TestDatabaseTitlePropertyCannotBeDeletedOrConverted(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    db, _ := dbRegressionCreate(t, ctx)
    titleProp := db.Properties[0]
    del := dbRegressionCall(t, ctx, "DELETE", "/api/db/properties?id="+titleProp.ID, "", ctx.handleDBProperties)
    if del.Code != 400 { t.Fatalf("title deletion should be rejected: %d %s", del.Code, del.Body.String()) }
    changed, err := json.Marshal(DBProperty{ID: titleProp.ID, DatabaseID:db.ID, Name:"Name", Type:"number", Version:titleProp.Version, Visible:true})
    if err != nil { t.Fatal(err) }
    put := dbRegressionCall(t, ctx, "PUT", "/api/db/properties", string(changed), ctx.handleDBProperties)
    if put.Code != 400 { t.Fatalf("title type change should be rejected: %d %s", put.Code, put.Body.String()) }
    current, err := ctx.getDatabase(db.ID)
    if err != nil { t.Fatal(err) }
    if len(current.Properties)!=1 || current.Properties[0].Type!="title" || current.Properties[0].Version!=titleProp.Version { t.Fatalf("title property corrupted: %+v",current.Properties) }
}


func TestDatabaseRecordCannotHijackUnrelatedPage(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    db, _ := dbRegressionCreate(t, ctx)
    unrelated := db.PageID
    response := dbRegressionCall(t, ctx, "POST", "/api/db/records", `{"databaseId":"`+db.ID+`","title":"Hijack","pageId":"`+unrelated+`"}`, ctx.handleDBRecords)
    if response.Code != 400 { t.Fatalf("arbitrary existing page accepted as record backing page: %d %s", response.Code, response.Body.String()) }
    var total int
    if err := ctx.DB.QueryRow(`SELECT COUNT(*) FROM db_records WHERE database_id=?`,db.ID).Scan(&total); err != nil { t.Fatal(err) }
    if total != 1 { t.Fatalf("malformed record created: %d",total) }
}

func TestDatabaseRecordTitleChangeVersionsBackingPage(t *testing.T) {
    ctx := setupTestContext(t)
    defer ctx.DB.Close()
    _, rec := dbRegressionCreate(t, ctx)
    var previousVersion int
    if err := ctx.DB.QueryRow(`SELECT version FROM pages WHERE id=?`,rec.PageID).Scan(&previousVersion); err != nil { t.Fatal(err) }
    response := dbRegressionCall(t, ctx, "PUT", "/api/db/records", `{"id":"`+rec.ID+`","version":1,"title":"Revised","values":{}}`, ctx.handleDBRecords)
    if response.Code != 200 { t.Fatalf("record title change failed: %d %s",response.Code,response.Body.String()) }
    var nextVersion int
    var title string
    if err := ctx.DB.QueryRow(`SELECT title, version FROM pages WHERE id=?`,rec.PageID).Scan(&title,&nextVersion); err != nil { t.Fatal(err) }
    if title != "Revised" || nextVersion != previousVersion+1 { t.Fatalf("page version/title mismatch: %q, %d -> %d",title,previousVersion,nextVersion) }
}


func TestDatabaseRelationReferencesAndSystemTimestamps(t *testing.T) {
    ctx:=setupTestContext(t)
    defer ctx.DB.Close()
    source, from:=dbRegressionCreate(t,ctx)
    target, to:=dbRegressionCreate(t,ctx)
    config,err:=json.Marshal(map[string]any{"targetDatabaseId":target.ID})
    if err!=nil {t.Fatal(err)}
    payload,err:=json.Marshal(map[string]any{"databaseId":source.ID,"name":"Related","type":"relation","config":string(config)})
    if err!=nil {t.Fatal(err)}
    response:=dbRegressionCall(t,ctx,"POST","/api/db/properties",string(payload),ctx.handleDBProperties)
    if response.Code!=201 {t.Fatalf("create relation property: %d %s",response.Code,response.Body.String())}
    var prop DBProperty
    if err:=json.Unmarshal(response.Body.Bytes(),&prop);err!=nil {t.Fatal(err)}
    good:=map[string]any{"id":from.ID,"title":from.Title,"version":from.Version,"values":map[string]any{prop.ID:[]string{to.ID}}}
    goodBody,_:=json.Marshal(good)
    response=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(goodBody),ctx.handleDBRecords)
    if response.Code!=200 {t.Fatalf("valid relation write: %d %s",response.Code,response.Body.String())}
    saved,err:=ctx.getDBRecord(from.ID)
    if err!=nil {t.Fatal(err)}
    if saved.Version!=from.Version+1 {t.Fatalf("version not incremented: %+v",saved)}
    invalid:=map[string]any{"id":from.ID,"title":"Bad relation write","version":saved.Version,"values":map[string]any{prop.ID:[]string{from.ID}}}
    invalidBody,_:=json.Marshal(invalid)
    response=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(invalidBody),ctx.handleDBRecords)
    if response.Code!=400 {t.Fatalf("cross database target accepted: %d %s",response.Code,response.Body.String())}
    after,err:=ctx.getDBRecord(from.ID)
    if err!=nil {t.Fatal(err)}
    if after.Version!=saved.Version || after.Title!=saved.Title {t.Fatalf("invalid relation modified record: %+v",after)}
    stamps:=dbRegressionCall(t,ctx,"POST","/api/db/properties",`{"databaseId":"`+source.ID+`","name":"Created","type":"created_time"}`,ctx.handleDBProperties)
    if stamps.Code!=201 {t.Fatalf("create system property: %d %s",stamps.Code,stamps.Body.String())}
    var created DBProperty
    if err:=json.Unmarshal(stamps.Body.Bytes(),&created);err!=nil {t.Fatal(err)}
    after,err=ctx.getDBRecord(from.ID)
    if err!=nil {t.Fatal(err)}
    if after.Values[created.ID]!=from.CreatedAt {t.Fatalf("system created time missing: %+v",after.Values)}
    bogus:=map[string]any{"id":from.ID,"version":after.Version,"title":after.Title,"values":map[string]any{created.ID:"2020-01-01"}}
    bogusBody,_:=json.Marshal(bogus)
    response=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(bogusBody),ctx.handleDBRecords)
    if response.Code!=400 {t.Fatalf("read-only timestamp update accepted: %d %s",response.Code,response.Body.String())}
}


func TestDatabasePropertyOnlyEditDoesNotConflictWithPageDraft(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    database,record:=dbRegressionCreate(t,ctx)
    propBody,err:=json.Marshal(map[string]any{"databaseId":database.ID,"name":"Count","type":"number"})
    if err!=nil {t.Fatal(err)}
    result:=dbRegressionCall(t,ctx,"POST","/api/db/properties",string(propBody),ctx.handleDBProperties)
    if result.Code!=201{t.Fatalf("property creation: %d %s",result.Code,result.Body.String())}
    var p DBProperty
    if err:=json.Unmarshal(result.Body.Bytes(),&p);err!=nil{t.Fatal(err)}
    var before int
    if err:=ctx.DB.QueryRow("SELECT version FROM pages WHERE id=?",record.PageID).Scan(&before);err!=nil{t.Fatal(err)}
    change,err:=json.Marshal(map[string]any{"id":record.ID,"title":record.Title,"version":record.Version,"values":map[string]any{p.ID:42}})
    if err!=nil{t.Fatal(err)}
    res:=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(change),ctx.handleDBRecords)
    if res.Code!=200{t.Fatalf("property update: %d %s",res.Code,res.Body.String())}
    var after int
    if err:=ctx.DB.QueryRow("SELECT version FROM pages WHERE id=?",record.PageID).Scan(&after);err!=nil{t.Fatal(err)}
    if before!=after {t.Fatalf("property edit wrongly changed BlockNote page revision: %d -> %d",before,after)}
}
func TestDatabaseCorruptStoredPropertyReturnsErrorInsteadOfPanicking(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,record:=dbRegressionCreate(t,ctx)
    body,err:=json.Marshal(map[string]any{"databaseId":db.ID,"name":"Text","type":"text"})
    if err!=nil{t.Fatal(err)}
    res:=dbRegressionCall(t,ctx,"POST","/api/db/properties",string(body),ctx.handleDBProperties)
    if res.Code!=201{t.Fatalf("create property: %d",res.Code)}
    var field DBProperty
    if err:=json.Unmarshal(res.Body.Bytes(),&field);err!=nil{t.Fatal(err)}
    if _,err:=ctx.DB.Exec("INSERT INTO db_values(record_id,property_id,value,version) VALUES (?,?,?,1)",record.ID,field.ID,"not-json");err!=nil{t.Fatal(err)}
    if _,err:=ctx.getDBRecord(record.ID);err==nil{t.Fatal("corrupt stored value silently accepted")}
    output:=dbRegressionCall(t,ctx,"GET","/api/db/records?databaseId="+db.ID,"",ctx.handleDBRecords)
    if output.Code!=500{t.Fatalf("expected server error for corrupt value, got %d: %s",output.Code,output.Body.String())}
}


func TestDatabaseMalformedRequestsDoNotMutateState(t *testing.T) {
    ctx:=setupTestContext(t)
    defer ctx.DB.Close()
    database,record:=dbRegressionCreate(t,ctx)
    cases:=[]struct {
        method,path,body string
        handler http.HandlerFunc
    }{
        {"POST","/api/databases",`{"title":`,ctx.handleDatabases},
        {"POST","/api/db/properties",`{"databaseId":`,ctx.handleDBProperties},
        {"POST","/api/db/records",`{"databaseId":`,ctx.handleDBRecords},
        {"POST","/api/db/views",`{"databaseId":`,ctx.handleDBViews},
        {"PUT","/api/db/records",`{"id":`,ctx.handleDBRecords},
        {"POST","/api/db/linked-view",`{"databaseId":`,ctx.handleDBLinkedView},
    }
    for _,c:=range cases{
        res:=dbRegressionCall(t,ctx,c.method,c.path,c.body,c.handler)
        if res.Code!=400{t.Fatalf("malformed %s %s accepted: %d %s",c.method,c.path,res.Code,res.Body.String())}
    }
    var databases,records,views,properties int
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM databases").Scan(&databases);err!=nil{t.Fatal(err)}
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_records").Scan(&records);err!=nil{t.Fatal(err)}
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_views").Scan(&views);err!=nil{t.Fatal(err)}
    if err:=ctx.DB.QueryRow("SELECT COUNT(*) FROM db_properties").Scan(&properties);err!=nil{t.Fatal(err)}
    if databases!=1 || records!=1 || views!=1 || properties!=1 {
        t.Fatalf("invalid request modified database: db=%d rec=%d views=%d props=%d",databases,records,views,properties)
    }
    saved,err:=ctx.getDBRecord(record.ID)
    if err!=nil || saved.Title!=record.Title {t.Fatalf("record changed after malformed request: %+v err=%v",saved,err)}
    if saved.DatabaseID!=database.ID {t.Fatal("record moved")}
}


func TestDatabaseRenameRollsBackIfBackingPageUpdateFails(t *testing.T) {
    ctx:=setupTestContext(t)
    defer ctx.DB.Close()
    database,_:=dbRegressionCreate(t,ctx)
    var originalVersion int
    if err:=ctx.DB.QueryRow("SELECT version FROM pages WHERE id=?",database.PageID).Scan(&originalVersion);err!=nil {t.Fatal(err)}
    if _,err:=ctx.DB.Exec(`CREATE TRIGGER reject_database_page_title BEFORE UPDATE OF title ON pages
      WHEN OLD.id='`+database.PageID+`'
      BEGIN SELECT RAISE(ABORT, 'simulated page failure'); END`);err!=nil {t.Fatal(err)}
    update:=map[string]any{"id":database.ID,"version":database.Version,"title":"Should roll back","icon":"📒"}
    encoded,err:=json.Marshal(update)
    if err!=nil {t.Fatal(err)}
    failed:=dbRegressionCall(t,ctx,"PUT","/api/databases",string(encoded),ctx.handleDatabases)
    if failed.Code!=500 {t.Fatalf("page failure should reject rename: %d %s",failed.Code,failed.Body.String())}
    if _,err:=ctx.DB.Exec("DROP TRIGGER reject_database_page_title");err!=nil {t.Fatal(err)}
    current,err:=ctx.getDatabase(database.ID)
    if err!=nil {t.Fatal(err)}
    if current.Title!=database.Title || current.Version!=database.Version {t.Fatalf("rename persisted despite transaction failure: %+v",current)}
    var pageTitle string
    var pageVersion int
    if err:=ctx.DB.QueryRow("SELECT title,version FROM pages WHERE id=?",database.PageID).Scan(&pageTitle,&pageVersion);err!=nil {t.Fatal(err)}
    if pageTitle!=database.Title || pageVersion!=originalVersion {t.Fatalf("page changed despite rollback: %s version %d",pageTitle,pageVersion)}
    done:=dbRegressionCall(t,ctx,"PUT","/api/databases",string(encoded),ctx.handleDatabases)
    if done.Code!=200 {t.Fatalf("rename retry failed: %d %s",done.Code,done.Body.String())}
    if stale:=dbRegressionCall(t,ctx,"PUT","/api/databases",string(encoded),ctx.handleDatabases);stale.Code!=409 {t.Fatalf("stale rename accepted: %d %s",stale.Code,stale.Body.String())}
}
