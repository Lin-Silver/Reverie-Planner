package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "reflect"
    "strings"
    "testing"
)

func TestTrashRestoreRejectsMalformedRequestsWithoutChangingData(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    _, record := dbRegressionCreate(t, app)
    removed := dbRegressionCall(t, app, http.MethodDelete, "/api/db/records?id="+record.ID, "", app.handleDBRecords)
    if removed.Code != http.StatusNoContent { t.Fatalf("trash record: %d %s", removed.Code, removed.Body.String()) }

    var deletedAt string
    var version int
    if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_records WHERE id=?", record.ID).Scan(&deletedAt,&version); err != nil { t.Fatal(err) }
    providerPath := filepath.Join(app.RootPath,"config","providers.json")
    beforeProviders, err := os.ReadFile(providerPath)
    if err != nil { t.Fatal(err) }

    valid := `{"kind":"record","id":"`+record.ID+`"}`
    cases := []struct{name,body string}{
        {"malformed", `{"kind":`},
        {"trailing object", valid+valid},
        {"duplicate key", `{"kind":"record","kind":"database","id":"`+record.ID+`"}`},
        {"null", "null"},
        {"array", "["+valid+"]"},
        {"oversized", `{"kind":"record","id":"`+record.ID+`","padding":"`+strings.Repeat("x",4<<20)+`"}`},
    }
    for _, tc := range cases {
        t.Run(tc.name,func(t *testing.T) {
            result := dbRegressionCall(t,app,http.MethodPost,"/api/db/trash",tc.body,app.handleDBTrash)
            if result.Code != http.StatusBadRequest { t.Fatalf("invalid restore accepted: %d %s",result.Code,result.Body.String()) }
            var actualDeletedAt string
            var actualVersion int
            if err := app.DB.QueryRow("SELECT deleted_at,version FROM db_records WHERE id=?",record.ID).Scan(&actualDeletedAt,&actualVersion);err!=nil {t.Fatal(err)}
            if actualDeletedAt!=deletedAt || actualVersion!=version {t.Fatalf("rejected restore modified tombstone: deleted=%q version=%d; want %q %d",actualDeletedAt,actualVersion,deletedAt,version)}
            afterProviders,err:=os.ReadFile(providerPath)
            if err!=nil || !bytes.Equal(beforeProviders,afterProviders) {t.Fatalf("provider configuration changed: %v",err)}
        })
    }
    restored := dbRegressionCall(t,app,http.MethodPost,"/api/db/trash",valid,app.handleDBTrash)
    if restored.Code!=http.StatusOK {t.Fatalf("valid restore failed: %d %s",restored.Code,restored.Body.String())}
    saved,err:=app.getDBRecord(record.ID)
    if err!=nil || saved.Title!=record.Title || saved.Version!=version+1 {t.Fatalf("restored record corrupted: %+v %v",saved,err)}
}

func TestTrashRelationPropertyRestoreRequiresActiveTargetDatabase(t *testing.T) {
    app:=setupTestContext(t)
    defer app.DB.Close()
    source,from:=dbRegressionCreate(t,app)
    target,to:=dbRegressionCreate(t,app)
    providerPath:=filepath.Join(app.RootPath,"config","providers.json")
    beforeProviders,err:=os.ReadFile(providerPath)
    if err!=nil {t.Fatal(err)}

    request,err:=json.Marshal(DBProperty{DatabaseID:source.ID,Name:"Related",Type:"relation",Config:`{"targetDatabaseId":"`+target.ID+`"}`})
    if err!=nil {t.Fatal(err)}
    created:=dbRegressionCall(t,app,http.MethodPost,"/api/db/properties",string(request),app.handleDBProperties)
    if created.Code!=http.StatusCreated {t.Fatalf("create relation: %d %s",created.Code,created.Body.String())}
    var property DBProperty
    if err:=json.Unmarshal(created.Body.Bytes(),&property);err!=nil {t.Fatal(err)}
    values,err:=json.Marshal(map[string]any{"id":from.ID,"version":from.Version,"title":from.Title,"values":map[string]any{property.ID:[]string{to.ID}}})
    if err!=nil {t.Fatal(err)}
    updated:=dbRegressionCall(t,app,http.MethodPut,"/api/db/records",string(values),app.handleDBRecords)
    if updated.Code!=http.StatusOK {t.Fatalf("save relation value: %d %s",updated.Code,updated.Body.String())}

    deletedProperty:=dbRegressionCall(t,app,http.MethodDelete,"/api/db/properties?id="+property.ID,"",app.handleDBProperties)
    if deletedProperty.Code!=http.StatusNoContent {t.Fatalf("trash property: %d %s",deletedProperty.Code,deletedProperty.Body.String())}
    deletedTarget:=dbRegressionCall(t,app,http.MethodDelete,"/api/databases?id="+target.ID,"",app.handleDatabases)
    if deletedTarget.Code!=http.StatusNoContent {t.Fatalf("trash target: %d %s",deletedTarget.Code,deletedTarget.Body.String())}
    var version int
    var deletedAt string
    if err:=app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?",property.ID).Scan(&version,&deletedAt);err!=nil {t.Fatal(err)}
    var valueCount int
    if err:=app.DB.QueryRow("SELECT COUNT(*) FROM db_values WHERE record_id=? AND property_id=?",from.ID,property.ID).Scan(&valueCount);err!=nil {t.Fatal(err)}
    if valueCount!=1 {t.Fatalf("deleted property lost original value: %d",valueCount)}

    restoreProperty:=`{"kind":"property","id":"`+property.ID+`"}`
    rejected:=dbRegressionCall(t,app,http.MethodPost,"/api/db/trash",restoreProperty,app.handleDBTrash)
    if rejected.Code!=http.StatusConflict {t.Fatalf("restored relation with trashed target: %d %s",rejected.Code,rejected.Body.String())}
    var afterVersion int
    var afterDeletedAt string
    if err:=app.DB.QueryRow("SELECT version,deleted_at FROM db_properties WHERE id=?",property.ID).Scan(&afterVersion,&afterDeletedAt);err!=nil {t.Fatal(err)}
    if afterVersion!=version || afterDeletedAt!=deletedAt {t.Fatalf("rejected restore modified property: %d %q -> %d %q",version,deletedAt,afterVersion,afterDeletedAt)}

    restoredTarget:=dbRegressionCall(t,app,http.MethodPost,"/api/db/trash",`{"kind":"database","id":"`+target.ID+`"}`,app.handleDBTrash)
    if restoredTarget.Code!=http.StatusOK {t.Fatalf("restore target database: %d %s",restoredTarget.Code,restoredTarget.Body.String())}
    restoredProperty:=dbRegressionCall(t,app,http.MethodPost,"/api/db/trash",restoreProperty,app.handleDBTrash)
    if restoredProperty.Code!=http.StatusOK {t.Fatalf("restore relation property: %d %s",restoredProperty.Code,restoredProperty.Body.String())}
    saved,err:=app.getDBRecord(from.ID)
    if err!=nil {t.Fatal(err)}
    if !reflect.DeepEqual(saved.Values[property.ID],[]any{to.ID}) {t.Fatalf("restored relation value lost: %#v",saved.Values[property.ID])}
    if err:=app.DB.QueryRow("SELECT version FROM db_properties WHERE id=?",property.ID).Scan(&afterVersion);err!=nil {t.Fatal(err)}
    if afterVersion!=version+1 {t.Fatalf("restore version not incremented once: %d -> %d",version,afterVersion)}
    afterProviders,err:=os.ReadFile(providerPath)
    if err!=nil || !bytes.Equal(beforeProviders,afterProviders) {t.Fatalf("provider configuration changed: %v",err)}
}
