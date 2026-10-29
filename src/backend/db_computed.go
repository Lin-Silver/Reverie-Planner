package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "math"
    "strings"
)

type formulaOperand struct {
    PropertyID string `json:"propertyId"`
    Number *float64 `json:"number"`
    Text *string `json:"text"`
}
type formulaDefinition struct {
    Operation string `json:"operation"`
    Args []formulaOperand `json:"args"`
}
type rollupDefinition struct {
    RelationPropertyID string `json:"relationPropertyId"`
    TargetPropertyID string `json:"targetPropertyId"`
    Aggregate string `json:"aggregate"`
}
func parseFormulaDefinition(config string) (formulaDefinition,error) {
    var def formulaDefinition
    // Reject ambiguous saved definitions before Go's JSON decoder can apply
    // last-key-wins semantics to formula operations or nested operands.
    if err:=rejectDuplicateJSONKeys([]byte(config));err!=nil{return def,fmt.Errorf("invalid formula config: %w",err)}
    if err:=json.Unmarshal([]byte(config),&def);err!=nil{return def,fmt.Errorf("invalid formula config: %w",err)}
    allowed:=map[string]bool{"sum":true,"subtract":true,"multiply":true,"divide":true,"concat":true}
    if !allowed[def.Operation] || len(def.Args)==0 || len(def.Args)>20 {return def,fmt.Errorf("invalid formula operation or operand count")}
    for _,operand:=range def.Args {
        n:=0; if operand.PropertyID!=""{n++}; if operand.Number!=nil{n++}; if operand.Text!=nil{n++}
        if n!=1{return def,fmt.Errorf("formula operand must have exactly one source")}
        if operand.Number!=nil && (math.IsNaN(*operand.Number)||math.IsInf(*operand.Number,0)){return def,fmt.Errorf("formula literal must be finite")}
    }
    return def,nil
}
func parseRollupDefinition(config string) (rollupDefinition,error) {
    var def rollupDefinition
    // A duplicate relation, target or aggregate key must not silently change
    // which records a computed field reads.
    if err:=rejectDuplicateJSONKeys([]byte(config));err!=nil{return def,fmt.Errorf("invalid rollup config: %w",err)}
    if err:=json.Unmarshal([]byte(config),&def);err!=nil{return def,fmt.Errorf("invalid rollup config: %w",err)}
    if def.RelationPropertyID=="" || def.TargetPropertyID=="" {return def,fmt.Errorf("rollup relation and target property required")}
    if !map[string]bool{"count":true,"sum":true,"average":true,"min":true,"max":true}[def.Aggregate] {return def,fmt.Errorf("unsupported rollup aggregate")}
    return def,nil
}
// Computed fields may only refer to persisted (noncomputed) properties; no code
// evaluation, recursive formula execution, SQL snippets or cyclic dependencies.
func validateDBComputedDefinition(db dbComputedQueryer, databaseID, kind, config string) error {
    switch kind {
    case "formula":
        def,err:=parseFormulaDefinition(config);if err!=nil{return err}
        for _,operand:=range def.Args {
            if operand.PropertyID=="" {continue}
            var propertyType string
            if err:=db.QueryRow("SELECT type FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL",operand.PropertyID,databaseID).Scan(&propertyType);err!=nil{return fmt.Errorf("formula property not found")}
            if !map[string]bool{"title":true,"text":true,"number":true,"checkbox":true}[propertyType]{return fmt.Errorf("unsupported formula operand property type")}
        }
    case "rollup":
        def,err:=parseRollupDefinition(config);if err!=nil{return err}
        var kind,relationConfig string
        if err:=db.QueryRow("SELECT type, config FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL",def.RelationPropertyID,databaseID).Scan(&kind,&relationConfig);err!=nil||kind!="relation"{return fmt.Errorf("rollup must reference relation property")}
        var relation struct{TargetDatabaseID string `json:"targetDatabaseId"`}
        // A legacy or corrupted relation config must not be interpreted with
        // encoding/json last-key-wins semantics when defining a rollup.
        if err:=rejectDuplicateJSONKeys([]byte(relationConfig));err!=nil{return fmt.Errorf("invalid relation configuration: %w",err)}
        if err:=json.Unmarshal([]byte(relationConfig),&relation);err!=nil{return fmt.Errorf("invalid relation configuration")}
        var targetType string
        if err:=db.QueryRow("SELECT type FROM db_properties WHERE id=? AND database_id=? AND deleted_at IS NULL",def.TargetPropertyID,relation.TargetDatabaseID).Scan(&targetType);err!=nil{return fmt.Errorf("rollup target property not found")}
        if def.Aggregate!="count" && targetType!="number" {return fmt.Errorf("numeric rollup requires a number property")}
    }
    return nil
}
func formulaValue(def formulaDefinition, rec DBRecord, byID map[string]DBProperty) any {
    vals:=make([]any,0,len(def.Args))
    for _,arg:=range def.Args {
        switch {
        case arg.Number!=nil: vals=append(vals,*arg.Number)
        case arg.Text!=nil: vals=append(vals,*arg.Text)
        case arg.PropertyID!="":
            p,ok:=byID[arg.PropertyID]
            if !ok{return nil}
            if p.Type=="title" {vals=append(vals,rec.Title)} else { vals=append(vals,rec.Values[arg.PropertyID]) }
        }
    }
    if def.Operation=="concat" {
        parts:=make([]string,0,len(vals))
        for _,v:=range vals {if v!=nil {parts=append(parts,fmt.Sprint(v))}}
        return strings.Join(parts,"")
    }
    nums:=make([]float64,0,len(vals))
    for _,v:=range vals {
        if v==nil{return nil}
        num,ok:=v.(float64);if !ok {return nil}
        if math.IsNaN(num)||math.IsInf(num,0){return nil}
        nums=append(nums,num)
    }
    total:=nums[0]
    switch def.Operation {
    case "sum":total=0;for _,v:=range nums{total+=v}
    case "subtract":for _,v:=range nums[1:]{total-=v}
    case "multiply":total=1;for _,v:=range nums{total*=v}
    case "divide":for _,v:=range nums[1:]{if v==0{return nil};total/=v}
    }
    if math.IsNaN(total)||math.IsInf(total,0){return nil}
    return total
}
// dbComputedQueryer allows computed rollups to read from the same SQLite
// transaction as a record mutation, before its changes are committed.
type dbComputedQueryer interface {
    QueryRow(query string, args ...any) *sql.Row
}

func (a *AppContext) computeRollup(def rollupDefinition, record DBRecord) (any,error) {
    return computeRollupWithQuery(a.DB, def, record)
}

func computeRollupWithQuery(q dbComputedQueryer, def rollupDefinition, record DBRecord) (any,error) {
    value:=record.Values[def.RelationPropertyID]
    ids,ok:=value.([]any)
    if !ok || len(ids)==0 {
        if def.Aggregate=="count" || def.Aggregate=="sum" {return float64(0),nil}
        return nil,nil
    }
    var count int;total:=float64(0);minimum:=math.Inf(1);maximum:=math.Inf(-1)
    for _,raw:=range ids {
        id,ok:=raw.(string);if !ok{return nil,fmt.Errorf("invalid relation reference")}
        // The join confirms referenced record and target property still belong
        // to the same database even after property/record deletion.
        var stored sql.NullString
        err:=q.QueryRow("SELECT dv.value FROM db_records dr JOIN db_properties dp ON dp.database_id=dr.database_id AND dp.id=? AND dp.deleted_at IS NULL LEFT JOIN db_values dv ON dv.record_id=dr.id AND dv.property_id=dp.id WHERE dr.id=? AND dr.deleted_at IS NULL",def.TargetPropertyID,id).Scan(&stored)
        if err==sql.ErrNoRows {continue}
        if err!=nil{return nil,err}
        if !stored.Valid {if def.Aggregate=="count"{count++};continue}
        var num any
        if err:=json.Unmarshal([]byte(stored.String),&num);err!=nil{return nil,err}
        value,ok:=num.(float64)
        if def.Aggregate=="count" {count++;continue}
        if !ok {continue}
        count++;total+=value
        if value<minimum{minimum=value};if value>maximum{maximum=value}
    }
    switch def.Aggregate{
    case "count":return float64(count),nil
    case "sum":return total,nil
    case "average":if count>0{return total/float64(count),nil}
    case "min":if count>0{return minimum,nil}
    case "max":if count>0{return maximum,nil}
    }
    return nil,nil
}
func (a *AppContext) appendComputedValues(fields []DBProperty, rec *DBRecord) error {
    return appendComputedValuesWithQuery(a.DB, fields, rec)
}

func appendComputedValuesWithQuery(q dbComputedQueryer, fields []DBProperty, rec *DBRecord) error {
    byID:=map[string]DBProperty{}
    for _,field:=range fields{byID[field.ID]=field}
    for _,field:=range fields{
        switch field.Type{
        case "formula":
            def,err:=parseFormulaDefinition(field.Config)
            if err!=nil{return err}
            rec.Values[field.ID]=formulaValue(def,*rec,byID)
        case "rollup":
            def,err:=parseRollupDefinition(field.Config);if err!=nil{return err}
            value,err:=computeRollupWithQuery(q,def,*rec);if err!=nil{return err}
            rec.Values[field.ID]=value
        }
    }
    return nil
}
