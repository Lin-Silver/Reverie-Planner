package main

import (
    "bytes"
    "encoding/json"
    "mime/multipart"
    "net/http"
    "net/http/httptest"
    "testing"
)

func uploadDatabaseTestFile(t *testing.T,ctx *AppContext,databaseID,recordID,filename string,payload []byte) (*httptest.ResponseRecorder,dbFileMeta) {
    t.Helper()
    var body bytes.Buffer
    writer:=multipart.NewWriter(&body)
    if err:=writer.WriteField("databaseId",databaseID);err!=nil{t.Fatal(err)}
    if err:=writer.WriteField("recordId",recordID);err!=nil{t.Fatal(err)}
    part,err:=writer.CreateFormFile("file",filename)
    if err!=nil{t.Fatal(err)}
    if _,err:=part.Write(payload);err!=nil{t.Fatal(err)}
    if err:=writer.Close();err!=nil{t.Fatal(err)}
    request:=httptest.NewRequest(http.MethodPost,"/api/db/files",&body)
    request.Header.Set("Content-Type",writer.FormDataContentType())
    response:=httptest.NewRecorder()
    ctx.handleDBFiles(response,request)
    var meta dbFileMeta
    if response.Code==201{if err:=json.Unmarshal(response.Body.Bytes(),&meta);err!=nil{t.Fatal(err)}}
    return response,meta
}
func TestDatabaseFilesUploadDownloadAndRecordOwnership(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,owner:=dbRegressionCreate(t,ctx)
    otherResponse:=dbRegressionCall(t,ctx,"POST","/api/db/records",`{"databaseId":"`+db.ID+`","title":"Other"}`,ctx.handleDBRecords)
    if otherResponse.Code!=201{t.Fatalf("other record creation: %d",otherResponse.Code)}
    var other DBRecord
    if err:=json.Unmarshal(otherResponse.Body.Bytes(),&other);err!=nil{t.Fatal(err)}
    field:=computedProperty(t,ctx,db.ID,"Attachments","file",map[string]any{})
    content:=[]byte("Local attachment with private content")
    res,meta:=uploadDatabaseTestFile(t,ctx,db.ID,owner.ID,`"....safe.txt"`,content)
    if res.Code!=201{t.Fatalf("upload: %d %s",res.Code,res.Body.String())}
    if meta.RecordID!=owner.ID || meta.DatabaseID!=db.ID || meta.Name!="safe.txt" || meta.Size!=int64(len(content)){t.Fatalf("invalid attachment metadata: %+v",meta)}
    got:=dbRegressionCall(t,ctx,"GET","/api/db/files?id="+meta.ID,"",ctx.handleDBFiles)
    if got.Code!=200 || !bytes.Equal(got.Body.Bytes(),content){t.Fatalf("download mismatch: %d %q",got.Code,got.Body.String())}
    if got.Header().Get("Content-Disposition")=="" {t.Fatal("download missing Content-Disposition")}
    values:=map[string]any{field.ID:[]string{meta.ID}}
    computedUpdate(t,ctx,owner,values)
    invalid,err:=json.Marshal(map[string]any{"id":other.ID,"version":other.Version,"title":"Should remain","values":values})
    if err!=nil{t.Fatal(err)}
    denied:=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(invalid),ctx.handleDBRecords)
    if denied.Code!=400{t.Fatalf("attachment of unrelated record accepted: %d %s",denied.Code,denied.Body.String())}
    after,err:=ctx.getDBRecord(other.ID);if err!=nil{t.Fatal(err)}
    if after.Title!=other.Title || after.Version!=other.Version{t.Fatalf("rejected upload changed record: %+v",after)}
    gone:=dbRegressionCall(t,ctx,"DELETE","/api/db/records?id="+owner.ID,"",ctx.handleDBRecords)
    if gone.Code!=204{t.Fatalf("delete owner: %d %s",gone.Code,gone.Body.String())}
    blocked:=dbRegressionCall(t,ctx,"GET","/api/db/files?id="+meta.ID,"",ctx.handleDBFiles)
    if blocked.Code!=404{t.Fatalf("deleted owner still exposes attachment: %d",blocked.Code)}
    restored:=dbRegressionCall(t,ctx,"POST","/api/db/trash",`{"kind":"record","id":"`+owner.ID+`"}`,ctx.handleDBTrash)
    if restored.Code!=200{t.Fatalf("record restore: %d %s",restored.Code,restored.Body.String())}
    again:=dbRegressionCall(t,ctx,"GET","/api/db/files?id="+meta.ID,"",ctx.handleDBFiles)
    if again.Code!=200 || !bytes.Equal(again.Body.Bytes(),content){t.Fatalf("restored attachment missing")}
}
func TestDatabaseFileUploadSizeLimit(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    db,rec:=dbRegressionCreate(t,ctx)
    oversized:=bytes.Repeat([]byte("z"),int(maxDatabaseFileBytes)+1)
    result,_:=uploadDatabaseTestFile(t,ctx,db.ID,rec.ID,"big.bin",oversized)
    if result.Code!=413{t.Fatalf("oversized file allowed: %d %s",result.Code,result.Body.String())}
}
