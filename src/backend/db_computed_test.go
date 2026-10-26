package main

import (
    "encoding/json"
    "fmt"
    "testing"
)

func computedProperty(t *testing.T,ctx *AppContext, dbID, name, kind string, cfg map[string]any) DBProperty {
    t.Helper()
    cfgJSON,err:=json.Marshal(cfg);if err!=nil{t.Fatal(err)}
    body,err:=json.Marshal(map[string]any{"databaseId":dbID,"name":name,"type":kind,"config":string(cfgJSON)})
    if err!=nil{t.Fatal(err)}
    res:=dbRegressionCall(t,ctx,"POST","/api/db/properties",string(body),ctx.handleDBProperties)
    if res.Code!=201{t.Fatalf("%s property: %d %s",kind,res.Code,res.Body.String())}
    var out DBProperty
    if err:=json.Unmarshal(res.Body.Bytes(),&out);err!=nil{t.Fatal(err)}
    return out
}
func computedUpdate(t *testing.T,ctx *AppContext,rec DBRecord, values map[string]any) {
    t.Helper()
    body,err:=json.Marshal(map[string]any{"id":rec.ID,"title":rec.Title,"version":rec.Version,"values":values})
    if err!=nil{t.Fatal(err)}
    r:=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(body),ctx.handleDBRecords)
    if r.Code!=200{t.Fatalf("save: %d %s",r.Code,r.Body.String())}
}
func TestDatabaseFormulaAndRollupEvaluateWithoutExecutingCode(t *testing.T) {
    ctx:=setupTestContext(t);defer ctx.DB.Close()
    source,from:=dbRegressionCreate(t,ctx)
    target,to:=dbRegressionCreate(t,ctx)
    price:=computedProperty(t,ctx,target.ID,"Price","number",map[string]any{})
    computedUpdate(t,ctx,to,map[string]any{price.ID:float64(8)})
    a:=computedProperty(t,ctx,source.ID,"A","number",map[string]any{})
    b:=computedProperty(t,ctx,source.ID,"B","number",map[string]any{})
    rel:=computedProperty(t,ctx,source.ID,"Relations","relation",map[string]any{"targetDatabaseId":target.ID})
    formula:=computedProperty(t,ctx,source.ID,"Total","formula",map[string]any{
        "operation":"sum","args":[]any{map[string]any{"propertyId":a.ID},map[string]any{"propertyId":b.ID}},
    })
    rollup:=computedProperty(t,ctx,source.ID,"Price sum","rollup",map[string]any{
        "relationPropertyId":rel.ID,"targetPropertyId":price.ID,"aggregate":"sum",
    })
    computedUpdate(t,ctx,from,map[string]any{
        a.ID:float64(2),b.ID:float64(3),rel.ID:[]string{to.ID},
    })
    got,err:=ctx.getDBRecord(from.ID);if err!=nil{t.Fatal(err)}
    if got.Values[formula.ID]!=float64(5) {t.Fatalf("formula: %+v",got.Values)}
    if got.Values[rollup.ID]!=float64(8) {t.Fatalf("rollup: %+v",got.Values)}
    invalid,err:=json.Marshal(map[string]any{"id":got.ID,"version":got.Version,"title":"Should not commit","values":map[string]any{formula.ID:float64(999)}})
    if err!=nil{t.Fatal(err)}
    res:=dbRegressionCall(t,ctx,"PUT","/api/db/records",string(invalid),ctx.handleDBRecords)
    if res.Code!=400{t.Fatalf("computed write should reject: %d %s",res.Code,res.Body.String())}
    after,err:=ctx.getDBRecord(from.ID);if err!=nil{t.Fatal(err)}
    if after.Title!=got.Title || after.Version!=got.Version{t.Fatalf("computed write violated atomicity: %+v",after)}
    invalidConfig:=fmt.Sprintf(`{"databaseId":"%s","name":"Unsafe","type":"formula","config":"{}"}`,source.ID)
    res=dbRegressionCall(t,ctx,"POST","/api/db/properties",invalidConfig,ctx.handleDBProperties)
    if res.Code!=400{t.Fatalf("invalid formula accepted: %d %s",res.Code,res.Body.String())}
}
