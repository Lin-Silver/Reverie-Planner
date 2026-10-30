package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "reflect"
 "strings"
 "unicode/utf8"
 "time"
)

const maxDatabasePackageBytes = 16 << 20

type databasePackageProperty struct {
 ID string `json:"id"`
 Name string `json:"name"`
 Type string `json:"type"`
 Position int `json:"position"`
 Visible bool `json:"visible"`
}
type databasePackageView struct {
 Name string `json:"name"`
 Type string `json:"type"`
 Config string `json:"config"`
 Position int `json:"position"`
}
type databasePackageRecord struct {
 Page documentPackagePage `json:"page"`
 Values map[string]json.RawMessage `json:"values"`
}
type databasePackage struct {
 Format string `json:"format"`
 Version int `json:"version"`
 Page documentPackagePage `json:"page"`
 Properties []databasePackageProperty `json:"properties"`
 Views []databasePackageView `json:"views"`
 Records []databasePackageRecord `json:"records"`
}
type databasePackageResult struct {
 Digest string `json:"digest"`
 DatabaseID string `json:"databaseId"`
 PageID string `json:"pageId"`
 Records int `json:"records"`
}

// Explicit reference slots only. Unknown configuration is refused rather than
// copied with dangling IDs or rewritten by replacing arbitrary user text.
func rewritePackageView(view databasePackageView, ids map[string]string) (string,error) {
 if err:=validateDBViewConfig(view.Type,view.Config);err!=nil{return "",err}
 var body map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(view.Config),&body);err!=nil{return "",err}
 reference:=func(id string)(string,error){if id==""||id=="title"{return id,nil};if next,ok:=ids[id];ok{return next,nil};return "",fmt.Errorf("view references unavailable property %q",id)}
 encode:=func(v any)json.RawMessage{b,_:=json.Marshal(v);return b}
 var filter func(json.RawMessage)(json.RawMessage,error)
 filter=func(raw json.RawMessage)(json.RawMessage,error){
  var node map[string]json.RawMessage
  if err:=json.Unmarshal(raw,&node);err!=nil{return nil,err}
  var kind string;json.Unmarshal(node["kind"],&kind)
  for key:=range node{if key!="kind"&&key!="join"&&key!="children"&&key!="propertyId"&&key!="operator"&&key!="value"{return nil,errors.New("unsupported filter setting")}}
  if kind=="rule"{var id string;json.Unmarshal(node["propertyId"],&id);next,err:=reference(id);if err!=nil{return nil,err};node["propertyId"]=encode(next)}else{
   var children []json.RawMessage;json.Unmarshal(node["children"],&children)
   for i,child:=range children{next,err:=filter(child);if err!=nil{return nil,err};children[i]=next};node["children"]=encode(children)
  };return encode(node),nil
 }
 for key,raw:=range body {
  switch key {
  case "filter": // Free-text search is literal writing, never an ID.
  case "sort","groupBy","dateProperty","galleryCoverProperty":
   var id string;json.Unmarshal(raw,&id);next,err:=reference(id);if err!=nil{return "",err};body[key]=encode(next)
  case "columnOrder","hiddenColumns","galleryProperties":
   var list []string;json.Unmarshal(raw,&list);for i,id:=range list{next,err:=reference(id);if err!=nil{return "",err};list[i]=next};body[key]=encode(list)
  case "columnWidths":
   var widths map[string]float64;json.Unmarshal(raw,&widths);next:=map[string]float64{};for id,width:=range widths{mapped,err:=reference(id);if err!=nil{return "",err};next[mapped]=width};body[key]=encode(next)
  case "sortRules":
   var rules []map[string]json.RawMessage;json.Unmarshal(raw,&rules)
   for _,rule:=range rules{if len(rule)!=2{return "",errors.New("unsupported sort setting")};var id string;json.Unmarshal(rule["propertyId"],&id);next,err:=reference(id);if err!=nil{return "",err};rule["propertyId"]=encode(next)};body[key]=encode(rules)
  case "filterTree":
   next,err:=filter(raw);if err!=nil{return "",err};body[key]=next
  default:return "",fmt.Errorf("unsupported view setting %q; use a workspace archive",key)
  }
 }
 result,_:=json.Marshal(body)
 if err:=validateDBViewConfig(view.Type,string(result));err!=nil{return "",err}
 return string(result),nil
}

func validateDatabasePackage(p databasePackage) error {
 if p.Format!="reverie-database"||p.Version!=1{return errors.New("unsupported database package format/version")}
 if len(p.Properties)<1||len(p.Properties)>32||len(p.Views)<1||len(p.Views)>32||p.Records==nil||len(p.Records)>1000{return errors.New("package requires 1–32 properties, 1–32 views and at most 1000 records")}
 validText:=func(s string,max int)bool{return utf8.ValidString(s)&&len(s)<=max&&!strings.ContainsRune(s,'\x00')}
 if strings.TrimSpace(p.Page.Title)==""||!validText(p.Page.Title,250)||strings.ContainsAny(p.Page.Title,"\r\n"){return errors.New("invalid database title")}
 ids:=map[string]string{};kinds:=map[string]string{};titles:=0
 for index,prop:=range p.Properties {
  if index>0&&prop.Position<=p.Properties[index-1].Position{return errors.New("property positions must be strictly increasing")}
  if prop.ID==""||prop.ID=="title"||!validText(prop.ID,128)||ids[prop.ID]!=""||strings.TrimSpace(prop.Name)==""||!validText(prop.Name,120)||strings.ContainsAny(prop.Name,"\r\n")||prop.Position<0{return errors.New("invalid or duplicate property")}
  switch prop.Type{case "title":titles++;case "text","number","checkbox","date","url","email":default:return fmt.Errorf("unsupported property type %q; use a workspace archive",prop.Type)}
  ids[prop.ID]=prop.ID;kinds[prop.ID]=prop.Type
 }
 if titles!=1{return errors.New("package requires exactly one title property")}
 pages:=map[string]string{}
 checkPage:=func(page documentPackagePage)error{
  if page.ID==""||!validText(page.ID,128)||pages[page.ID]!=""||page.ParentID!=""||!validText(page.Title,500)||!validText(page.Icon,512)||!utf8.ValidString(page.Content){return errors.New("invalid, nested or duplicate backing page")};pages[page.ID]=page.ID;return nil
 }
 if err:=checkPage(p.Page);err!=nil{return err}
 for _,record:=range p.Records{
  if record.Values==nil{return errors.New("record values must be an object")}
  if err:=checkPage(record.Page);err!=nil{return err}
  if _,err:=csvTypedValue("title",record.Page.Title);err!=nil{return err}
  for id,raw:=range record.Values{
   kind,ok:=kinds[id];if !ok||kind=="title"{return errors.New("record contains unavailable or title property value")}
   var value any
   if rejectDuplicateJSONKeys(raw)!=nil||json.Unmarshal(raw,&value)!=nil{return errors.New("invalid record value")}
   if value==nil{continue}
   switch kind{
   case "number":n,ok:=value.(float64);if !ok{return errors.New("number must be numeric")};if _,err:=csvTypedValue(kind,fmt.Sprint(n));err!=nil{return err}
   case "checkbox":if _,ok:=value.(bool);!ok{return errors.New("checkbox must be boolean")}
   default:s,ok:=value.(string);if !ok{return errors.New("text/date/URL/email must be a string")};if _,err:=csvTypedValue(kind,s);err!=nil{return err}
   }
  }
 }
 for index,view:=range p.Views{
  if index>0&&view.Position<=p.Views[index-1].Position{return errors.New("view positions must be strictly increasing")}
  if strings.TrimSpace(view.Name)==""||!validText(view.Name,250)||view.Position<0{return errors.New("invalid view name/position")}
  if _,err:=rewritePackageView(view,ids);err!=nil{return err}
 }
 for _,page:=range append([]documentPackagePage{p.Page},packageRecordPages(p.Records)...){if _,err:=rewriteDocumentContent(page.Content,pages,nil);err!=nil{return fmt.Errorf("page %q: %w",page.Title,err)}}
 return nil
}
func packageRecordPages(records []databasePackageRecord)[]documentPackagePage{out:=make([]documentPackagePage,0,len(records));for _,r:=range records{out=append(out,r.Page)};return out}
func decodeDatabasePackage(raw []byte)(databasePackage,error){
 var p databasePackage
 if len(raw)==0||len(raw)>maxDatabasePackageBytes||!utf8.Valid(raw){return p,errors.New("choose a UTF-8 database package up to 16 MiB")}
 if err:=rejectDuplicateJSONKeys(raw);err!=nil{return p,err}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&p);err!=nil{return p,err};if decoder.Decode(new(any))!=io.EOF{return p,errors.New("unexpected package data")}
 return p,validateDatabasePackage(p)
}
func databasePackageDigest(raw []byte)string{hash:=sha256.Sum256(raw);return hex.EncodeToString(hash[:])}

func (a *AppContext) exportDatabasePackage(ctx context.Context,id string)([]byte,error){
 tx,err:=a.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if err!=nil{return nil,err};defer tx.Rollback()
 db,err:=readDatabaseInTx(tx,id);if err!=nil{return nil,err}
 p:=databasePackage{Format:"reverie-database",Version:1,Properties:[]databasePackageProperty{},Views:[]databasePackageView{},Records:[]databasePackageRecord{}}
 readPage:=func(id string)(documentPackagePage,error){
  var page documentPackagePage;var cover,linkedDB,linkedView string
  err:=tx.QueryRowContext(ctx,`SELECT id,title,COALESCE(icon,''),COALESCE(content,''),COALESCE(cover,''),COALESCE(linked_database_id,''),COALESCE(linked_view_id,'') FROM pages WHERE id=?`,id).Scan(&page.ID,&page.Title,&page.Icon,&page.Content,&cover,&linkedDB,&linkedView)
  if err!=nil{return page,err};if cover!=""||linkedDB!=""||linkedView!=""{return page,errors.New("covers and linked backing pages require a workspace archive")}
  var count int
  if err=tx.QueryRowContext(ctx,`SELECT (SELECT count(*) FROM pages WHERE parent_id=?)+(SELECT count(*) FROM page_assets WHERE root_id=?)`,id,id).Scan(&count);err!=nil{return page,err}
  if count!=0{return page,errors.New("nested pages or page assets require a workspace archive")};return page,nil
 }
 p.Page,err=readPage(db.PageID);if err!=nil{return nil,err}
 budget:=len(p.Page.Content)
 if p.Page.Title!=db.Title||p.Page.Icon!=db.Icon{return nil,errors.New("database/backing page metadata differs")}
 for _,prop:=range db.Properties{budget+=len(prop.ID)+len(prop.Name)+len(prop.Config);var cfg map[string]any;if json.Unmarshal([]byte(prop.Config),&cfg)!=nil||cfg==nil||len(cfg)!=0{return nil,errors.New("configured properties require a workspace archive")};p.Properties=append(p.Properties,databasePackageProperty{prop.ID,prop.Name,prop.Type,prop.Position,prop.Visible})}
 for _,view:=range db.Views{budget+=len(view.Config)+len(view.Name);p.Views=append(p.Views,databasePackageView{view.Name,view.Type,view.Config,view.Position})}
 var files int
 if err=tx.QueryRowContext(ctx,`SELECT count(*) FROM db_files WHERE database_id=?`,id).Scan(&files);err!=nil{return nil,err};if files>0{return nil,errors.New("database attachments require a workspace archive")}
 rows,err:=tx.QueryContext(ctx,`SELECT id,page_id FROM db_records WHERE database_id=? AND deleted_at IS NULL ORDER BY created_at,id LIMIT 1001`,id);if err!=nil{return nil,err}
 type pair struct{id,page string};records:=[]pair{}
 for rows.Next(){var r pair;if err=rows.Scan(&r.id,&r.page);err!=nil{rows.Close();return nil,err};records=append(records,r)}
 err=rows.Err();rows.Close();if err!=nil{return nil,err};if len(records)>1000{return nil,errors.New("database exceeds 1000 live records")}
 for _,r:=range records{
  page,e:=readPage(r.page);if e!=nil{return nil,e};budget+=len(page.Content)+len(page.Title)
  if budget>maxDatabasePackageBytes{return nil,errors.New("database exceeds package content limit")}
  record,e:=readDBRecordInTx(tx,r.id,true);if e!=nil{return nil,e};if record.Title!=page.Title{return nil,errors.New("record/backing page title differs")}
  values:=map[string]json.RawMessage{}
  vr,e:=tx.QueryContext(ctx,`SELECT v.property_id,v.value FROM db_values v JOIN db_properties p ON p.id=v.property_id WHERE v.record_id=? AND p.deleted_at IS NULL`,r.id);if e!=nil{return nil,e}
  for vr.Next(){var prop,value string;if e=vr.Scan(&prop,&value);e!=nil{vr.Close();return nil,e};budget+=len(value)+len(prop);if budget>maxDatabasePackageBytes{vr.Close();return nil,errors.New("database exceeds package content limit")};values[prop]=json.RawMessage(value)};e=vr.Err();vr.Close();if e!=nil{return nil,e}
  p.Records=append(p.Records,databasePackageRecord{page,values})
 }
 if err=validateDatabasePackage(p);err!=nil{return nil,err}
 raw,err:=json.Marshal(p);if err!=nil{return nil,err}
 if _,err=decodeDatabasePackage(raw);err!=nil{return nil,err}
 return raw,tx.Commit()
}

func (a *AppContext) importDatabasePackage(ctx context.Context,p databasePackage,digest,key string)(databasePackageResult,error){
 result:=databasePackageResult{Digest:digest,Records:len(p.Records)}
 if err:=validateDatabasePackage(p);err!=nil{return result,err};if !importKeyPattern.MatchString(key){return result,errors.New("invalid import operation key")}
 tx,err:=a.DB.BeginTx(ctx,nil);if err!=nil{return result,err};defer tx.Rollback()
 receipt:="database_package:"+key;var saved string
 err=tx.QueryRowContext(ctx,"SELECT value FROM app_state WHERE key=?",receipt).Scan(&saved)
 if err==nil{if err=json.Unmarshal([]byte(saved),&result);err!=nil{return result,err};if result.Digest!=digest{return result,errors.New("operation key belongs to another package")};return result,nil}
 if !errors.Is(err,sql.ErrNoRows){return result,err}
 result.DatabaseID=newID("db");result.PageID=newID("page");now:=timeNowUTC()
 pages:=map[string]string{p.Page.ID:result.PageID};properties:=map[string]string{}
 for _,r:=range p.Records{pages[r.Page.ID]=newID("page")};for _,prop:=range p.Properties{properties[prop.ID]=newID("prop")}
 writePage:=func(page documentPackagePage)error{
  content,e:=rewriteDocumentContent(page.Content,pages,nil);if e!=nil{return e}
  _,e=tx.ExecContext(ctx,`INSERT INTO pages(id,title,icon,content,workspace,created_by,version,created_at,updated_at) VALUES(?,?,?,?,1,'Import',1,?,?)`,pages[page.ID],page.Title,page.Icon,content,now,now);if e!=nil{return e}
  var title,icon,actual string
  e=tx.QueryRowContext(ctx,`SELECT title,COALESCE(icon,''),COALESCE(content,'') FROM pages WHERE id=?`,pages[page.ID]).Scan(&title,&icon,&actual)
  if e!=nil{return e};if title!=page.Title||icon!=page.Icon||actual!=content{return errors.New("page readback changed")};return nil
 }
 if err=writePage(p.Page);err!=nil{return result,err}
 if _,err=tx.ExecContext(ctx,`INSERT INTO databases(id,page_id,title,icon,version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,result.DatabaseID,result.PageID,p.Page.Title,p.Page.Icon,now,now);err!=nil{return result,err}
 for _,prop:=range p.Properties{if _,err=tx.ExecContext(ctx,`INSERT INTO db_properties(id,database_id,name,type,config,position,visible,version) VALUES(?,?,?,?,'{}',?,?,1)`,properties[prop.ID],result.DatabaseID,prop.Name,prop.Type,prop.Position,prop.Visible);err!=nil{return result,err}}
 for _,view:=range p.Views{cfg,e:=rewritePackageView(view,properties);if e!=nil{return result,e};if _,err=tx.ExecContext(ctx,`INSERT INTO db_views(id,database_id,name,type,config,position,version) VALUES(?,?,?,?,?,?,1)`,newID("view"),result.DatabaseID,view.Name,view.Type,cfg,view.Position);err!=nil{return result,err}}
 recordBaseTime:=time.Now().UTC().Add(-time.Duration(len(p.Records))*time.Nanosecond)
 for index,record:=range p.Records{
  recordTime:=recordBaseTime.Add(time.Duration(index)*time.Nanosecond).Format("2006-01-02T15:04:05.000000000Z")
  if err=writePage(record.Page);err!=nil{return result,err};recordID:=newID("rec")
  if _,err=tx.ExecContext(ctx,`INSERT INTO db_records(id,database_id,title,page_id,version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,recordID,result.DatabaseID,record.Page.Title,pages[record.Page.ID],recordTime,recordTime);err!=nil{return result,err}
  expected:=map[string]any{}
  for id,raw:=range record.Values{if _,err=tx.ExecContext(ctx,`INSERT INTO db_values(record_id,property_id,value,version,updated_at) VALUES(?,?,?,1,?)`,recordID,properties[id],string(raw),now);err!=nil{return result,err};var value any;json.Unmarshal(raw,&value);expected[properties[id]]=value}
  actual,e:=readDBRecordInTx(tx,recordID,true);if e!=nil{return result,e}
  if actual.Title!=record.Page.Title||actual.PageID!=pages[record.Page.ID]{return result,errors.New("record readback changed")}
  for id,value:=range expected{stored,exists:=actual.Values[id];if !exists||!reflect.DeepEqual(stored,value){return result,errors.New("value readback changed")}}
 }
 // Re-export inside the SAME transaction would nest a transaction. Read complete
 // schema here instead and compare every field that the package promises.
 actual,err:=readDatabaseInTx(tx,result.DatabaseID);if err!=nil{return result,err}
 if actual.Title!=p.Page.Title||actual.Icon!=p.Page.Icon||actual.PageID!=result.PageID||len(actual.Properties)!=len(p.Properties)||len(actual.Views)!=len(p.Views){return result,errors.New("database readback changed")}
 for _,prop:=range p.Properties{found:=false;for _,q:=range actual.Properties{if q.ID==properties[prop.ID]{found=q.Name==prop.Name&&q.Type==prop.Type&&q.Config=="{}"&&q.Position==prop.Position&&q.Visible==prop.Visible}};if !found{return result,errors.New("property readback changed")}}
 for i,view:=range p.Views{cfg,_:=rewritePackageView(view,properties);q:=actual.Views[i];if q.Name!=view.Name||q.Type!=view.Type||q.Config!=cfg||q.Position!=view.Position{return result,errors.New("view readback changed")}}
 encoded,err:=json.Marshal(result);if err!=nil{return result,err};if _,err=tx.ExecContext(ctx,`INSERT INTO app_state(key,value) VALUES(?,?)`,receipt,string(encoded));err!=nil{return result,err}
 var stored string
 if err=tx.QueryRowContext(ctx,`SELECT value FROM app_state WHERE key=?`,receipt).Scan(&stored);err!=nil{return result,err};if stored!=string(encoded){return result,errors.New("receipt readback changed")}
 return result,tx.Commit()
}

func (a *AppContext) handleDatabasePackage(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if r.Method==http.MethodGet{
  raw,err:=a.exportDatabasePackage(r.Context(),r.URL.Query().Get("databaseId"));if err!=nil{writeJSONError(w,400,"Cannot export database: "+err.Error());return}
  w.Header().Set("Content-Type","application/json; charset=utf-8");w.Header().Set("Content-Disposition",`attachment; filename="Reverie-database.reverie.json"`);w.Write(raw);return
 }
 if r.Method!=http.MethodPost{writeJSONError(w,405,"method not allowed");return}
 raw,err:=io.ReadAll(io.LimitReader(r.Body,maxDatabasePackageBytes+1));if err!=nil{writeJSONError(w,400,"could not read package");return}
 p,err:=decodeDatabasePackage(raw);if err!=nil{writeJSONError(w,400,err.Error());return};digest:=databasePackageDigest(raw)
 if r.URL.Query().Get("preview")=="1"{writeJSON(w,200,map[string]any{"digest":digest,"title":p.Page.Title,"properties":p.Properties,"views":p.Views,"records":len(p.Records),"sample":p.Records[:min(5,len(p.Records))]});return}
 if digest!=r.URL.Query().Get("digest"){writeJSONError(w,409,"Package changed; preview again");return}
 result,err:=a.importDatabasePackage(r.Context(),p,digest,r.URL.Query().Get("operationKey"));if err!=nil{writeJSONError(w,400,"Import was not applied: "+err.Error());return};writeJSON(w,201,result)
}
