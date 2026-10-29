package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "testing"
)

func TestDBBulkEditAtomicRollbackAndPageVersions(t *testing.T) {
 app:=setupTestContext(t)
 defer app.DB.Close()
 db,first:=dbRegressionCreate(t,app)
 created:=dbRegressionCall(t,app,"POST","/api/db/records",fmt.Sprintf(`{"databaseId":%q,"title":"Second"}`,db.ID),app.handleDBRecords)
 if created.Code!=201 {t.Fatalf("second record: %d %s",created.Code,created.Body.String())}
 var second DBRecord
 if err:=json.Unmarshal(created.Body.Bytes(),&second);err!=nil {t.Fatal(err)}
 prop:=computedProperty(t,app,db.ID,"Amount","number",map[string]any{})
 providerPath,providerBefore:=viewIntegrityProviders(t,app)
 editA:=func(version int,title string)map[string]any{return map[string]any{"id":first.ID,"version":version,"title":title}}
 editB:=func(version int,value any)map[string]any{return map[string]any{"id":second.ID,"version":version,"values":map[string]any{prop.ID:value}}}
 call:=func(edits ...map[string]any)*DBBulkTestResult{
  t.Helper()
  body,err:=json.Marshal(map[string]any{"databaseId":db.ID,"edits":edits})
  if err!=nil{t.Fatal(err)}
  response:=dbRegressionCall(t,app,http.MethodPost,"/api/db/records/bulk",string(body),app.handleDBBulkEdit)
  return &DBBulkTestResult{Code:response.Code,Body:response.Body.String()}
 }
 assertOriginal:=func(){
  t.Helper()
  a,err:=app.getDBRecord(first.ID);if err!=nil{t.Fatal(err)}
  b,err:=app.getDBRecord(second.ID);if err!=nil{t.Fatal(err)}
  if a.Title!=first.Title || a.Version!=first.Version || b.Version!=second.Version || b.Values[prop.ID]!=nil {
   t.Fatalf("failed batch mutated records: %+v %+v",a,b)
  }
  var title string;var version int
  if err:=app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?",first.PageID).Scan(&title,&version);err!=nil{t.Fatal(err)}
  if title!=first.Title || version!=1 {t.Fatalf("failed batch mutated page: %q v%d",title,version)}
  assertViewIntegrityProviders(t,providerPath,providerBefore)
 }
 if r:=call(editA(first.Version,"Rollback"),editB(second.Version,"invalid"));r.Code!=400{t.Fatalf("invalid value: %+v",r)}
 assertOriginal()
 if r:=call(editA(first.Version,"Rollback"),editB(second.Version+1,7));r.Code!=409{t.Fatalf("stale version: %+v",r)}
 assertOriginal()
 trigger:=fmt.Sprintf(`CREATE TRIGGER reject_bulk_second BEFORE UPDATE ON db_records WHEN NEW.id=%q BEGIN SELECT RAISE(ABORT,'fault'); END`,second.ID)
 if _,err:=app.DB.Exec(trigger);err!=nil{t.Fatal(err)}
 if r:=call(editA(first.Version,"Rollback"),editB(second.Version,7));r.Code!=500{t.Fatalf("SQLite failure: %+v",r)}
 if _,err:=app.DB.Exec("DROP TRIGGER reject_bulk_second");err!=nil{t.Fatal(err)}
 assertOriginal()
 if r:=call(editA(first.Version,"Bulk renamed"),editB(second.Version,7));r.Code!=200{t.Fatalf("valid bulk edit: %+v",r)}
 a,err:=app.getDBRecord(first.ID);if err!=nil{t.Fatal(err)}
 b,err:=app.getDBRecord(second.ID);if err!=nil{t.Fatal(err)}
 if a.Title!="Bulk renamed" || a.Version!=first.Version+1 || b.Version!=second.Version+1 || b.Values[prop.ID]!=float64(7) {
  t.Fatalf("valid batch not persisted: %+v %+v",a,b)
 }
 var title string;var version int
 if err:=app.DB.QueryRow("SELECT title,version FROM pages WHERE id=?",first.PageID).Scan(&title,&version);err!=nil{t.Fatal(err)}
 if title!="Bulk renamed" || version!=2 {t.Fatalf("title backing page mismatch: %q v%d",title,version)}
 if err:=app.DB.QueryRow("SELECT version FROM pages WHERE id=?",second.PageID).Scan(&version);err!=nil{t.Fatal(err)}
 if version!=1 {t.Fatalf("property-only edit bumped page version: %d",version)}
 if r:=call(editA(first.Version,"Stale"),editB(second.Version,11));r.Code!=409{t.Fatalf("stale retry: %+v",r)}
 if r:=call(editA(a.Version,"Duplicate"),editA(a.Version,"Duplicate again"));r.Code!=400{t.Fatalf("duplicate IDs: %+v",r)}
 a,err=app.getDBRecord(first.ID);if err!=nil || a.Title!="Bulk renamed" || a.Version!=first.Version+1{t.Fatalf("failed retry changed first: %+v %v",a,err)}
 b,err=app.getDBRecord(second.ID);if err!=nil || b.Values[prop.ID]!=float64(7) || b.Version!=second.Version+1{t.Fatalf("failed retry changed second: %+v %v",b,err)}
 assertViewIntegrityProviders(t,providerPath,providerBefore)
}
type DBBulkTestResult struct {Code int;Body string}
