package main

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"
)

func databaseTransferFixture(t *testing.T,a *AppContext)(csvImportResult,[]byte){
 t.Helper();result,err:=a.importCSV(context.Background(),requireCSVPlan(t,csvFixture()),"database_source_fixture_12345");if err!=nil{t.Fatal(err)}
 db,err:=a.getDatabase(result.DatabaseID);if err!=nil{t.Fatal(err)}
 cfg,_:=json.Marshal(map[string]any{"filter":"literal "+db.Properties[1].ID,"sortRules":[]any{map[string]any{"propertyId":db.Properties[2].ID,"direction":"desc"}},"columnOrder":[]string{db.Properties[2].ID,db.Properties[0].ID},"hiddenColumns":[]string{db.Properties[1].ID},"columnWidths":map[string]int{db.Properties[2].ID:180},"filterTree":map[string]any{"kind":"group","join":"and","children":[]any{map[string]any{"kind":"rule","propertyId":db.Properties[2].ID,"operator":"gte","value":"0"}}}})
 if _,err=a.DB.Exec(`UPDATE db_views SET config=? WHERE database_id=?`,string(cfg),db.ID);err!=nil{t.Fatal(err)}
 if _,err=a.DB.Exec(`INSERT INTO db_views(id,database_id,name,type,config,position,version) VALUES('transfer_second_view',?,'Upcoming','calendar',?,1,1)`,db.ID,`{"dateProperty":"`+db.Properties[4].ID+`"}`);err!=nil{t.Fatal(err)}
 if _,err=a.DB.Exec(`UPDATE db_properties SET visible=0 WHERE id=?`,db.Properties[1].ID);err!=nil{t.Fatal(err)}
 raw,err:=a.exportDatabasePackage(context.Background(),db.ID);if err!=nil{t.Fatal(err)};return result,raw
}
func TestDatabasePackageRoundtripRetryRestartAndArchive(t *testing.T){
 a:=setupTestContext(t);defer func(){a.DB.Close()}()
 source,raw:=databaseTransferFixture(t,a)
 p,err:=decodeDatabasePackage(raw);if err!=nil{t.Fatal(err)}
 digest:=databasePackageDigest(raw);key:="database_roundtrip_receipt_12345"
 imported,err:=a.importDatabasePackage(context.Background(),p,digest,key);if err!=nil{t.Fatal(err)}
 if imported.DatabaseID==source.DatabaseID||imported.PageID==source.PageID{t.Fatal("shared source IDs")}
 check:=func(a *AppContext){
  t.Helper();exported,e:=a.exportDatabasePackage(context.Background(),imported.DatabaseID);if e!=nil{t.Fatal(e)};q,e:=decodeDatabasePackage(exported);if e!=nil{t.Fatal(e)}
  ids:=map[string]string{};
  for i,prop:=range q.Properties{if prop.ID==p.Properties[i].ID{t.Fatal("shared property ID")};ids[prop.ID]=p.Properties[i].ID;q.Properties[i].ID=p.Properties[i].ID}
  for i,r:=range q.Records{q.Records[i].Page.ID=p.Records[i].Page.ID;values:=map[string]json.RawMessage{};for id,value:=range r.Values{values[ids[id]]=value};q.Records[i].Values=values}
  q.Page.ID=p.Page.ID
  for i,view:=range q.Views{cfg,e:=rewritePackageView(view,ids);if e!=nil{t.Fatal(e)};q.Views[i].Config=cfg;orig,e:=rewritePackageView(p.Views[i],identityPackageProperties(p));if e!=nil{t.Fatal(e)};p.Views[i].Config=orig}
  if !reflect.DeepEqual(q,p){t.Fatalf("roundtrip changed package\ngot %#v\nwant %#v",q,p)}
 }
 check(a)
 again,err:=a.importDatabasePackage(context.Background(),p,digest,key);if err!=nil||again!=imported{t.Fatal("duplicate retry",err)}
 a.DB.Close();a.DB,err=initializeDatabase(a.RootPath);if err!=nil{t.Fatal(err)};check(a)
 again,err=a.importDatabasePackage(context.Background(),p,digest,key);if err!=nil||again!=imported{t.Fatal("restart lost receipt",err)}
 archive,err:=a.createWorkspaceArchive(context.Background());if err!=nil{t.Fatal(err)}
 restored,err:=restoreWorkspaceArchive(context.Background(),archive,filepath.Join(t.TempDir(),"restored"));if err!=nil{t.Fatal(err)}
 db,err:=initializeDatabase(restored.Root);if err!=nil{t.Fatal(err)};defer db.Close();recovered:=&AppContext{DB:db,RootPath:restored.Root};check(recovered)
 again,err=recovered.importDatabasePackage(context.Background(),p,digest,key);if err!=nil||again!=imported{t.Fatal("archive lost receipt",err)}
 if _,err=a.DB.Exec(`UPDATE db_views SET name='Copy only' WHERE database_id=?`,imported.DatabaseID);err!=nil{t.Fatal(err)}
 original,err:=a.getDatabase(source.DatabaseID);if err!=nil||original.Views[0].Name=="Copy only"{t.Fatal("view edits affected source",err)}
}
func identityPackageProperties(p databasePackage)map[string]string{ids:=map[string]string{};for _,prop:=range p.Properties{ids[prop.ID]=prop.ID};return ids}

func TestDatabasePackageAtomicFailureReadbackAndCancellation(t *testing.T){
 for _,failure:=range []string{"pages","databases","db_properties","db_views","db_records","db_values","app_state","ignore_value","ignore_receipt","corrupt_page","corrupt_property","corrupt_view","cancel"}{t.Run(failure,func(t *testing.T){
  a:=setupTestContext(t);defer a.DB.Close();_,raw:=databaseTransferFixture(t,a);p,err:=decodeDatabasePackage(raw);if err!=nil{t.Fatal(err)}
  tables:=[]string{"pages","databases","db_properties","db_views","db_records","db_values","app_state"};counts:=map[string]int{}
  for _,table:=range tables{var count int;if err=a.DB.QueryRow("SELECT count(*) FROM "+table).Scan(&count);err!=nil{t.Fatal(err)};counts[table]=count}
  statement:=""
  switch failure{
  case "cancel":
  case "ignore_receipt":statement=`CREATE TRIGGER transfer_fail BEFORE INSERT ON app_state BEGIN SELECT RAISE(IGNORE); END`
  case "ignore_value":statement=`CREATE TRIGGER transfer_fail BEFORE INSERT ON db_values BEGIN SELECT RAISE(IGNORE); END`
  case "corrupt_page":statement=`CREATE TRIGGER transfer_fail AFTER INSERT ON pages BEGIN UPDATE pages SET content='corrupt' WHERE id=NEW.id; END`
  case "corrupt_property":statement=`CREATE TRIGGER transfer_fail AFTER INSERT ON db_properties BEGIN UPDATE db_properties SET name='corrupt' WHERE id=NEW.id; END`
  case "corrupt_view":statement=`CREATE TRIGGER transfer_fail AFTER INSERT ON db_views BEGIN UPDATE db_views SET config='{}' WHERE id=NEW.id; END`
  default:statement="CREATE TRIGGER transfer_fail BEFORE INSERT ON "+failure+" BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"
  }
  if statement!=""{if _,err=a.DB.Exec(statement);err!=nil{t.Fatal(err)}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if failure=="cancel"{cancel()}
  if _,err=a.importDatabasePackage(ctx,p,databasePackageDigest(raw),"database_failed_receipt_12345");err==nil{t.Fatal("failure committed")}
  for _,table:=range tables{var count int;if err=a.DB.QueryRow("SELECT count(*) FROM "+table).Scan(&count);err!=nil||count!=counts[table]{t.Fatal("partial write",table,count,counts[table],err)}}
 })}
}

func TestDatabasePackageHTTPPreviewSecretsAndMalformedInput(t *testing.T){
 a:=setupTestContext(t);defer a.DB.Close();source,raw:=databaseTransferFixture(t,a)
 secret:=[]byte(`{"apiKey":"SYNTHETIC_PROVIDER_SECRET_DO_NOT_EXPORT_7321","baseURL":"https://provider.example"}`)
 provider:=filepath.Join(a.RootPath,"config","providers.json");if err:=os.WriteFile(provider,secret,0600);err!=nil{t.Fatal(err)}
 if _,err:=a.DB.Exec(`INSERT INTO app_state(key,value) VALUES('unrelated_private_setting','SYNTHETIC_PRIVATE_STATE_7321')`);err!=nil{t.Fatal(err)}
 response:=httptest.NewRecorder();a.handleDatabasePackage(response,httptest.NewRequest("GET","/api/database-package?databaseId="+source.DatabaseID,nil))
 if response.Code!=200||bytes.Contains(response.Body.Bytes(),[]byte("SYNTHETIC_PROVIDER_SECRET"))||bytes.Contains(response.Body.Bytes(),[]byte("SYNTHETIC_PRIVATE_STATE"))||bytes.Contains(response.Body.Bytes(),[]byte("apiKey")){t.Fatal("export included unrelated secrets",response.Code)}
 var before int;a.DB.QueryRow(`SELECT count(*) FROM databases`).Scan(&before)
 post:=func(body []byte,query string)*httptest.ResponseRecorder{w:=httptest.NewRecorder();a.handleDatabasePackage(w,httptest.NewRequest("POST","/api/database-package"+query,bytes.NewReader(body)));return w}
 preview:=post(raw,"?preview=1");if preview.Code!=200{t.Fatal(preview.Body.String())}
 var count int;a.DB.QueryRow(`SELECT count(*) FROM databases`).Scan(&count);if before!=count{t.Fatal("preview wrote data")}
 if post(raw,"?digest=wrong&operationKey=database_http_receipt_12345").Code!=409{t.Fatal("digest ignored")}
 query:="?digest="+databasePackageDigest(raw)+"&operationKey=database_http_receipt_12345"
 first:=post(raw,query);retry:=post(raw,query);if first.Code!=201||retry.Body.String()!=first.Body.String(){t.Fatal(first.Code,first.Body.String(),retry.Body.String())}
 after,err:=os.ReadFile(provider);if err!=nil||!bytes.Equal(after,secret){t.Fatal("provider config changed",err)}
 for _,bad:=range [][]byte{[]byte(`null`),[]byte(`{"format":"reverie-database","format":"other"}`),append(append([]byte{},raw...),[]byte(` {}`)...),[]byte(strings.Repeat("x",maxDatabasePackageBytes+1)),bytes.Replace(raw,[]byte(`"version":1`),[]byte(`"version":2`),1),bytes.Replace(raw,[]byte(`"format":`),[]byte(`"unknown":true,"format":`),1)}{
  if post(bad,"?preview=1").Code!=400{t.Fatal("malformed accepted")}
 }
}
func TestDatabasePackageRefusesUnsupportedDependencies(t *testing.T){
 for _,mutation:=range []string{
  `UPDATE db_properties SET type='relation' WHERE type='text'`,
  `UPDATE db_properties SET config='{"custom":true}' WHERE type='text'`,
  `UPDATE db_views SET config='{"future":"unknown"}'`,
  `UPDATE db_views SET config='{"sort":"missing"}'`,
  `UPDATE pages SET cover='external'`,
  `UPDATE pages SET content='legacy writing'`,
  `UPDATE pages SET content='[{"type":"paragraph","content":[{"type":"link","href":"/?page=outside","content":[]}]}]'`,
 }{t.Run(mutation,func(t *testing.T){a:=setupTestContext(t);defer a.DB.Close();source,_:=databaseTransferFixture(t,a);if _,err:=a.DB.Exec(mutation);err!=nil{t.Fatal(err)};if _,err:=a.exportDatabasePackage(context.Background(),source.DatabaseID);err==nil{t.Fatal("unsupported data silently exported")}})}
}

func TestDatabasePackageWritingLinksNullsAndEmptyDatabase(t *testing.T){
 a:=setupTestContext(t);defer a.DB.Close();_,raw:=databaseTransferFixture(t,a)
 p,err:=decodeDatabasePackage(raw);if err!=nil{t.Fatal(err)}
 p.Records[0].Values[p.Properties[2].ID]=json.RawMessage(`null`)
 p.Records[0].Page.Icon="🌸"
 p.Page.Content=`[{"type":"paragraph","content":[{"type":"link","href":"/?page=`+p.Records[0].Page.ID+`","content":[{"type":"text","text":"Included record","styles":{}}]}]}]`
 // A literal property ID remains ordinary text; only structured references move.
 p.Records[0].Page.Content=`[{"type":"paragraph","content":[{"type":"text","text":"`+p.Properties[1].ID+`","styles":{}}]}]`
 encoded,err:=json.Marshal(p);if err!=nil{t.Fatal(err)}
 result,err:=a.importDatabasePackage(context.Background(),p,databasePackageDigest(encoded),"database_link_receipt_12345");if err!=nil{t.Fatal(err)}
 copyBytes,err:=a.exportDatabasePackage(context.Background(),result.DatabaseID);if err!=nil{t.Fatal(err)}
 copy,err:=decodeDatabasePackage(copyBytes);if err!=nil{t.Fatal(err)}
 if strings.Contains(copy.Page.Content,"/?page="+p.Records[0].Page.ID)||!strings.Contains(copy.Page.Content,"/?page="+copy.Records[0].Page.ID){t.Fatal("page link not remapped",copy.Page.Content)}
 if !strings.Contains(copy.Records[0].Page.Content,p.Properties[1].ID)||copy.Records[0].Page.Icon!="🌸"{t.Fatal("literal writing or icon changed")}
 null,exists:=copy.Records[0].Values[copy.Properties[2].ID];if !exists||string(null)!="null"{t.Fatal("null was dropped")}
 p.Records=[]databasePackageRecord{};p.Page.Content=""
 empty,err:=a.importDatabasePackage(context.Background(),p,"empty_database_digest","database_empty_receipt_12345");if err!=nil{t.Fatal(err)}
 emptyRaw,err:=a.exportDatabasePackage(context.Background(),empty.DatabaseID);if err!=nil{t.Fatal(err)}
 decoded,err:=decodeDatabasePackage(emptyRaw);if err!=nil||len(decoded.Records)!=0{t.Fatal("empty database failed",err)}
}

func TestDatabasePackageMalformedReferencesAndLimits(t *testing.T){
 a:=setupTestContext(t);defer a.DB.Close();_,raw:=databaseTransferFixture(t,a)
 mutations:=[]func(*databasePackage){
  func(p *databasePackage){p.Properties=append(p.Properties,p.Properties[0])},
  func(p *databasePackage){p.Views[0].Config=`{"sortRules":[{"propertyId":"outside","direction":"asc"}]}`},
  func(p *databasePackage){p.Views[0].Config=`{"columnWidths":{"outside":150}}`},
  func(p *databasePackage){p.Views[0].Config=`{"filterTree":{"kind":"group","join":"and","children":[{"kind":"rule","propertyId":"outside","operator":"is_empty"}]}}`},
  func(p *databasePackage){p.Records[0].Values["outside"]=json.RawMessage(`true`)},
  func(p *databasePackage){p.Records[0].Values[p.Properties[2].ID]=json.RawMessage(`"12.5"`)},
  func(p *databasePackage){p.Records[0].Page.ID=p.Page.ID},
  func(p *databasePackage){p.Page.Title="multi\nline"},
  func(p *databasePackage){p.Page.Content="\xff"},
  func(p *databasePackage){p.Views[1].Position=p.Views[0].Position},
  func(p *databasePackage){for len(p.Records)<=1000{r:=p.Records[0];r.Page.ID=newID("page");p.Records=append(p.Records,r)}},
 }
 for i,mutate:=range mutations{p,err:=decodeDatabasePackage(raw);if err!=nil{t.Fatal(err)};mutate(&p);if validateDatabasePackage(p)==nil{t.Fatalf("invalid mutation %d accepted",i)}}
}
