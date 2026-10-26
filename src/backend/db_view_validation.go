package main

import (
    "encoding/json"
    "fmt"
)

func validateDBFilterTree(raw json.RawMessage,depth int) error {
    if depth>8{return fmt.Errorf("filter nesting exceeds limit")}
    var node map[string]json.RawMessage
    if err:=json.Unmarshal(raw,&node);err!=nil||node==nil{return fmt.Errorf("filter node must be a JSON object")}
    var kind string
    if err:=json.Unmarshal(node["kind"],&kind);err!=nil{return fmt.Errorf("filter kind required")}
    switch kind{
    case "group":
        var join string
        if err:=json.Unmarshal(node["join"],&join);err!=nil|| (join!="and"&&join!="or"){return fmt.Errorf("filter join must be and/or")}
        var children []json.RawMessage
        if err:=json.Unmarshal(node["children"],&children);err!=nil{return fmt.Errorf("filter children must be an array")}
        if len(children)>40{return fmt.Errorf("too many filter conditions")}
        for _,child:=range children{if err:=validateDBFilterTree(child,depth+1);err!=nil{return err}}
        return nil
    case "rule":
        var prop,operator string
        if err:=json.Unmarshal(node["propertyId"],&prop);err!=nil||prop==""{return fmt.Errorf("filter property required")}
        if err:=json.Unmarshal(node["operator"],&operator);err!=nil{return fmt.Errorf("filter operator required")}
        valid:=map[string]bool{"equals":true,"not_equals":true,"contains":true,"not_contains":true,"gt":true,"gte":true,"lt":true,"lte":true,"before":true,"after":true,"is_empty":true,"is_not_empty":true}
        if !valid[operator]{return fmt.Errorf("unsupported filter operator")}
        if operator!="is_empty"&&operator!="is_not_empty"{
            var val string
            if err:=json.Unmarshal(node["value"],&val);err!=nil{return fmt.Errorf("filter comparison value must be a string")}
        }
        return nil
    default:return fmt.Errorf("unsupported filter node")
    }
}
