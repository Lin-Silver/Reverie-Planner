package main

import (
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

const maxDatabaseFileBytes int64 = 8 << 20

type dbFileMeta struct{
    ID string `json:"id"`
    DatabaseID string `json:"databaseId"`
    RecordID string `json:"recordId"`
    Name string `json:"name"`
    ContentType string `json:"contentType"`
    Size int64 `json:"size"`
    SHA256 string `json:"sha256"`
    CreatedAt string `json:"createdAt"`
}
func (a *AppContext) handleDBFiles(w http.ResponseWriter,r *http.Request){
    switch r.Method{
    case http.MethodPost:
        // Multipart parsing is bounded before allocating memory or opening temp files.
        r.Body=http.MaxBytesReader(w,r.Body,maxDatabaseFileBytes+(1<<20))
        if err:=r.ParseMultipartForm(1<<20);err!=nil{writeJSON(w,413,map[string]string{"error":"attachment exceeds 8 MiB limit or invalid multipart form"});return}
        if r.MultipartForm!=nil{defer r.MultipartForm.RemoveAll()}
        dbID,recordID:=r.FormValue("databaseId"),r.FormValue("recordId")
        if dbID==""||recordID==""{writeJSON(w,400,map[string]string{"error":"databaseId and recordId required"});return}
        var count int
        if err:=a.DB.QueryRow(`SELECT COUNT(*) FROM db_records r JOIN databases d ON d.id=r.database_id
          WHERE r.id=? AND r.database_id=? AND r.deleted_at IS NULL AND d.deleted_at IS NULL`,recordID,dbID).Scan(&count);err!=nil||count!=1{
            writeJSON(w,404,map[string]string{"error":"record not found"});return
        }
        uploaded,header,err:=r.FormFile("file")
        if err!=nil{writeJSON(w,400,map[string]string{"error":"file is required"});return}
        defer uploaded.Close()
        bytes,err:=io.ReadAll(io.LimitReader(uploaded,maxDatabaseFileBytes+1))
        if err!=nil{writeJSON(w,500,map[string]string{"error":"unable to read file"});return}
        if int64(len(bytes))>maxDatabaseFileBytes{writeJSON(w,413,map[string]string{"error":"file must be at most 8 MiB"});return}
        name:=filepath.Base(strings.ReplaceAll(header.Filename,"\","/"))
        name=strings.ReplaceAll(strings.ReplaceAll(name,"",""),"
","")
        if name==""||name=="."||name==".." {name="attachment"}
        if len(name)>200{name=name[:200]}
        id:=newID("dbfile")
        folder:=filepath.Join(a.RootPath,"data","attachments")
        if err:=os.MkdirAll(folder,0700);err!=nil{writeJSON(w,500,map[string]string{"error":"unable to create attachment folder"});return}
        path:=filepath.Join(folder,id)
        file,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
        if err!=nil{writeJSON(w,500,map[string]string{"error":"unable to create attachment"});return}
        _,writeErr:=file.Write(bytes)
        if writeErr==nil{writeErr=file.Sync()}
        closeErr:=file.Close()
        if writeErr!=nil||closeErr!=nil{_ = os.Remove(path);writeJSON(w,500,map[string]string{"error":"attachment write failed"});return}
        sum:=sha256.Sum256(bytes)
        meta:=dbFileMeta{ID:id,DatabaseID:dbID,RecordID:recordID,Name:name,ContentType:http.DetectContentType(bytes),Size:int64(len(bytes)),SHA256:hex.EncodeToString(sum[:]),CreatedAt:time.Now().UTC().Format(time.RFC3339)}
        _,err=a.DB.Exec(`INSERT INTO db_files(id,database_id,record_id,original_name,content_type,size_bytes,sha256,created_at)
          VALUES(?,?,?,?,?,?,?,?)`,meta.ID,meta.DatabaseID,meta.RecordID,meta.Name,meta.ContentType,meta.Size,meta.SHA256,meta.CreatedAt)
        if err!=nil{_ = os.Remove(path);writeJSON(w,500,map[string]string{"error":"attachment metadata write failed"});return}
        writeJSON(w,201,meta)
    case http.MethodGet:
        id:=r.URL.Query().Get("id")
        if id!=""{
            var name,contentType string
            if err:=a.DB.QueryRow(`SELECT f.original_name,f.content_type FROM db_files f
                 JOIN db_records rec ON rec.id=f.record_id JOIN databases d ON d.id=f.database_id
                 WHERE f.id=? AND f.deleted_at IS NULL AND rec.deleted_at IS NULL AND d.deleted_at IS NULL`,id).Scan(&name,&contentType);err!=nil{
                writeJSON(w,404,map[string]string{"error":"file not found"});return
            }
            // A stored filename is never taken from the request or original name.
            path:=filepath.Join(a.RootPath,"data","attachments",id)
            if _,err:=os.Stat(path);err!=nil{writeJSON(w,404,map[string]string{"error":"file missing on disk"});return}
            w.Header().Set("Content-Type","application/octet-stream")
            w.Header().Set("X-Content-Type-Options","nosniff")
            w.Header().Set("Content-Disposition","attachment; filename="+strconv.Quote(name))
            http.ServeFile(w,r,path)
            return
        }
        recordID:=r.URL.Query().Get("recordId")
        if recordID==""{writeJSON(w,400,map[string]string{"error":"id or recordId required"});return}
        rows,err:=a.DB.Query(`SELECT f.id,f.database_id,f.record_id,f.original_name,f.content_type,f.size_bytes,f.sha256,f.created_at
          FROM db_files f JOIN db_records rec ON rec.id=f.record_id JOIN databases d ON d.id=f.database_id
          WHERE f.record_id=? AND f.deleted_at IS NULL AND rec.deleted_at IS NULL AND d.deleted_at IS NULL ORDER BY f.created_at`,recordID)
        if err!=nil{writeJSON(w,500,map[string]string{"error":err.Error()});return}
        files:=[]dbFileMeta{}
        for rows.Next(){
            var f dbFileMeta
            if err:=rows.Scan(&f.ID,&f.DatabaseID,&f.RecordID,&f.Name,&f.ContentType,&f.Size,&f.SHA256,&f.CreatedAt);err!=nil{rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
            files=append(files,f)
        }
        if err:=rows.Err();err!=nil{rows.Close();writeJSON(w,500,map[string]string{"error":err.Error()});return}
        rows.Close()
        writeJSON(w,200,files)
    default:
        w.WriteHeader(405)
    }
}
// validate that file refs refer to uploads bound to this record, rather than
// arbitrary paths, arbitrary other records, or deleted attachments.
func validateDBFileReferences(tx *sql.Tx, recordID string, value any) error {
    if value==nil{return nil}
    ids,ok:=value.([]any)
    if !ok{return fmt.Errorf("file references must be an array")}
    if len(ids)>50{return fmt.Errorf("too many file references")}
    seen:=map[string]bool{}
    for _,raw:=range ids{
        id,ok:=raw.(string);if !ok||id==""||seen[id]{return fmt.Errorf("invalid file ID")}
        seen[id]=true
        var count int
        if err:=tx.QueryRow("SELECT COUNT(*) FROM db_files WHERE id=? AND record_id=? AND deleted_at IS NULL",id,recordID).Scan(&count);err!=nil{return err}
        if count!=1{return fmt.Errorf("file does not belong to this record")}
    }
    return nil
}
