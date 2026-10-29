package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "net/http"
    "testing"
)

func createIncomingRelationForDeleteTest(t *testing.T, app *AppContext, source, target Database, from, to DBRecord) (DBProperty, DBRecord) {
    t.Helper()
    config := fmt.Sprintf(`{"targetDatabaseId":%q}`, target.ID)
    body := fmt.Sprintf(`{"databaseId":%q,"name":"Related","type":"relation","config":%q}`, source.ID, config)
    response := dbRegressionCall(t, app, http.MethodPost, "/api/db/properties", body, app.handleDBProperties)
    if response.Code != http.StatusCreated { t.Fatalf("create relation: %d %s", response.Code, response.Body.String()) }
    var property DBProperty
    if err := json.Unmarshal(response.Body.Bytes(), &property); err != nil { t.Fatal(err) }
    update := fmt.Sprintf(`{"id":%q,"version":%d,"values":{%q:[%q]}}`, from.ID, from.Version, property.ID, to.ID)
    response = dbRegressionCall(t, app, http.MethodPut, "/api/db/records", update, app.handleDBRecords)
    if response.Code != http.StatusOK { t.Fatalf("save relation: %d %s", response.Code, response.Body.String()) }
    var saved DBRecord
    if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil { t.Fatal(err) }
    return property, saved
}

func TestRecordDeleteRestrictsIncomingRelationsAndRollsBackReadFaults(t *testing.T) {
    app := setupTestContext(t)
    defer app.DB.Close()
    source, from := dbRegressionCreate(t, app)
    target, to := dbRegressionCreate(t, app)
    property, saved := createIncomingRelationForDeleteTest(t, app, source, target, from, to)
    providerPath, providerBefore := viewIntegrityProviders(t, app)

    assertTargetUnchanged := func() {
        t.Helper()
        var version, pageVersion int
        var deleted sql.NullString
        if err := app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?", to.ID).Scan(&version,&deleted); err != nil { t.Fatal(err) }
        if version != to.Version || deleted.Valid { t.Fatalf("failed delete changed target: version=%d deleted=%v",version,deleted) }
        if err := app.DB.QueryRow("SELECT version FROM pages WHERE id=?",to.PageID).Scan(&pageVersion); err != nil { t.Fatal(err) }
        if pageVersion != 1 { t.Fatalf("failed delete changed backing page version: %d",pageVersion) }
        var value string
        if err := app.DB.QueryRow("SELECT value FROM db_values WHERE record_id=? AND property_id=?",from.ID,property.ID).Scan(&value); err != nil { t.Fatal(err) }
        if value != fmt.Sprintf(`[%q]`,to.ID) { t.Fatalf("failed delete changed source relation: %s",value) }
        assertViewIntegrityProviders(t,providerPath,providerBefore)
    }
    path := "/api/db/records?id="+to.ID
    blocked := dbRegressionCall(t,app,http.MethodDelete,path,"",app.handleDBRecords)
    if blocked.Code != http.StatusConflict { t.Fatalf("incoming relation must block deletion: %d %s",blocked.Code,blocked.Body.String()) }
    assertTargetUnchanged()

    if _,err:=app.DB.Exec("ALTER TABLE db_values RENAME COLUMN value TO unavailable_value");err!=nil {t.Fatal(err)}
    failed := dbRegressionCall(t,app,http.MethodDelete,path,"",app.handleDBRecords)
    if failed.Code != http.StatusInternalServerError {t.Fatalf("SQLite read failure must be 500: %d %s",failed.Code,failed.Body.String())}
    if _,err:=app.DB.Exec("ALTER TABLE db_values RENAME COLUMN unavailable_value TO value");err!=nil {t.Fatal(err)}
    assertTargetUnchanged()

    clear := fmt.Sprintf(`{"id":%q,"version":%d,"values":{%q:[]}}`,from.ID,saved.Version,property.ID)
    cleared := dbRegressionCall(t,app,http.MethodPut,"/api/db/records",clear,app.handleDBRecords)
    if cleared.Code != http.StatusOK {t.Fatalf("clear incoming relation: %d %s",cleared.Code,cleared.Body.String())}
    deleted := dbRegressionCall(t,app,http.MethodDelete,path,"",app.handleDBRecords)
    if deleted.Code != http.StatusNoContent {t.Fatalf("delete after clearing relation: %d %s",deleted.Code,deleted.Body.String())}
    var version int
    var tombstone sql.NullString
    if err:=app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?",to.ID).Scan(&version,&tombstone);err!=nil {t.Fatal(err)}
    if version!=to.Version+1 || !tombstone.Valid {t.Fatalf("successful delete did not tombstone target: %d %v",version,tombstone)}
    assertViewIntegrityProviders(t,providerPath,providerBefore)
}

func TestRecordDeleteAllowsSelfRelationButProtectsTrashedDatabaseSources(t *testing.T) {
    t.Run("self relation",func(t *testing.T) {
        app:=setupTestContext(t);defer app.DB.Close()
        db,record:=dbRegressionCreate(t,app)
        providerPath,providerBefore:=viewIntegrityProviders(t,app)
        createIncomingRelationForDeleteTest(t,app,db,db,record,record)
        deleted:=dbRegressionCall(t,app,http.MethodDelete,"/api/db/records?id="+record.ID,"",app.handleDBRecords)
        if deleted.Code!=http.StatusNoContent {t.Fatalf("self relation blocked own deletion: %d %s",deleted.Code,deleted.Body.String())}
        assertViewIntegrityProviders(t,providerPath,providerBefore)
    })
    t.Run("trashed source database",func(t *testing.T) {
        app:=setupTestContext(t);defer app.DB.Close()
        source,from:=dbRegressionCreate(t,app)
        target,to:=dbRegressionCreate(t,app)
        createIncomingRelationForDeleteTest(t,app,source,target,from,to)
        providerPath,providerBefore:=viewIntegrityProviders(t,app)
        removed:=dbRegressionCall(t,app,http.MethodDelete,"/api/databases?id="+source.ID,"",app.handleDatabases)
        if removed.Code!=http.StatusNoContent {t.Fatalf("trash source database: %d %s",removed.Code,removed.Body.String())}
        blocked:=dbRegressionCall(t,app,http.MethodDelete,"/api/db/records?id="+to.ID,"",app.handleDBRecords)
        if blocked.Code!=http.StatusConflict {t.Fatalf("trashed database's live relation must block target deletion: %d %s",blocked.Code,blocked.Body.String())}
        var version int
        var deleted sql.NullString
        if err:=app.DB.QueryRow("SELECT version,deleted_at FROM db_records WHERE id=?",to.ID).Scan(&version,&deleted);err!=nil {t.Fatal(err)}
        if version!=to.Version || deleted.Valid {t.Fatalf("rejected deletion changed target: %d %v",version,deleted)}
        assertViewIntegrityProviders(t,providerPath,providerBefore)
    })
}
