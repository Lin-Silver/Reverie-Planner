package main

import (
    "encoding/json"
    "testing"
)

func TestDatabaseSortRulesValidationAndPersistence(t *testing.T) {
    ctx:=setupTestContext(t)
    defer ctx.DB.Close()
    db,_:=dbRegressionCreate(t,ctx)
    invalid:=[]string{
        `{"sortRules":null}`,
        `{"sortRules":"no"}`,
        `{"sortRules":[{"propertyId":"score","direction":"sideways"}]}`,
        `{"sortRules":[{"propertyId":"score","direction":"asc"},{"propertyId":"score","direction":"desc"}]}`,
        `{"sortRules":[{"propertyId":"","direction":"asc"}]}`,
        `{"sortRules":[{"propertyId":"a","direction":"asc"},{"propertyId":"b","direction":"asc"},{"propertyId":"c","direction":"asc"},{"propertyId":"d","direction":"asc"},{"propertyId":"e","direction":"asc"},{"propertyId":"f","direction":"asc"}]}`,
    }
    for _,cfg:=range invalid {
        if err:=validateDBViewConfig("table",cfg);err==nil{t.Fatalf("invalid sort config accepted: %s",cfg)}
        request,err:=json.Marshal(map[string]any{"databaseId":db.ID,"name":"Bad","type":"table","config":cfg})
        if err!=nil{t.Fatal(err)}
        res:=dbRegressionCall(t,ctx,"POST","/api/db/views",string(request),ctx.handleDBViews)
        if res.Code!=400{t.Fatalf("invalid view created, %d: %s",res.Code,res.Body.String())}
    }
    valid:=`{"sort":"title","sortRules":[{"propertyId":"title","direction":"desc"}]}`
    if err:=validateDBViewConfig("table",valid);err!=nil{t.Fatal(err)}
    body,err:=json.Marshal(map[string]any{"databaseId":db.ID,"name":"Ordered","type":"table","config":valid})
    if err!=nil{t.Fatal(err)}
    res:=dbRegressionCall(t,ctx,"POST","/api/db/views",string(body),ctx.handleDBViews)
    if res.Code!=201{t.Fatalf("valid view rejected: %d %s",res.Code,res.Body.String())}
    var view DBView
    if err:=json.Unmarshal(res.Body.Bytes(),&view);err!=nil{t.Fatal(err)}
    if view.Config!=valid{t.Fatalf("view config not persisted: %q",view.Config)}
    badUpdate,err:=json.Marshal(map[string]any{"id":view.ID,"databaseId":db.ID,"name":"Damaged","type":"table","config":invalid[2],"version":view.Version})
    if err!=nil{t.Fatal(err)}
    rejected:=dbRegressionCall(t,ctx,"PUT","/api/db/views",string(badUpdate),ctx.handleDBViews)
    if rejected.Code!=400{t.Fatalf("invalid view update accepted: %d %s",rejected.Code,rejected.Body.String())}
    saved,err:=ctx.listViews(db.ID)
    if err!=nil{t.Fatal(err)}
    for _,candidate:=range saved {
        if candidate.ID==view.ID{
            if candidate.Config!=valid || candidate.Version!=view.Version{t.Fatalf("invalid update mutated stored view: %+v",candidate)}
            return
        }
    }
    t.Fatal("valid view disappeared")
}
