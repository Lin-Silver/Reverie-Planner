package main

import (
 "bytes"
 "os"
 "path/filepath"
 "testing"
)

// Simulates the pre-database v6 schema in a disposable SQLite file.
// Real historical backups still require separate acceptance testing.
func TestSyntheticV6UpgradePreservesPagesProjectsAndAIState(t *testing.T) {
 app:=setupTestContext(t)
 root:=app.RootPath
 project,err:=app.createProject("Keep project")
 if err!=nil { t.Fatal(err) }
 if _,err:=app.DB.Exec("INSERT INTO tasks(id,project_id,title) VALUES(?,?,?)","old-task",project.ID,"Keep task");err!=nil { t.Fatal(err) }
 if _,err:=app.DB.Exec("INSERT INTO pages(id,project_id,title,content) VALUES(?,?,?,?)","old-page",project.ID,"Keep page","saved BlockNote content");err!=nil { t.Fatal(err) }
 if _,err:=app.DB.Exec("INSERT INTO ai_sessions(id,provider_id,title) VALUES(?,?,?)","old-session","existing-provider","Keep AI session");err!=nil { t.Fatal(err) }
 providerPath:=filepath.Join(root,"config","providers.json")
 before,err:=os.ReadFile(providerPath)
 if err!=nil { t.Fatal(err) }
 for _,stmt:=range []string{
  "DROP TABLE db_files",
  "DROP TABLE db_values",
  "DROP TABLE db_views",
  "DROP TABLE db_properties",
  "DROP TABLE db_records",
  "DROP TABLE databases",
  "ALTER TABLE pages DROP COLUMN linked_view_id",
  "ALTER TABLE pages DROP COLUMN linked_database_id",
  "DELETE FROM schema_migrations WHERE version > 6",
 } {
  if _,err:=app.DB.Exec(stmt);err!=nil { t.Fatalf("construct synthetic v6 fixture %s: %v",stmt,err) }
 }
 if err:=app.DB.Close();err!=nil { t.Fatal(err) }
 for restart:=0;restart<2;restart++ {
  db,err:=initializeDatabase(root)
  if err!=nil { t.Fatalf("v6 upgrade restart %d: %v",restart,err) }
  for _,c:=range []struct{query,arg,want string}{
   {"SELECT name FROM projects WHERE id=?",project.ID,"Keep project"},
   {"SELECT title FROM tasks WHERE id=?","old-task","Keep task"},
   {"SELECT title FROM pages WHERE id=?","old-page","Keep page"},
   {"SELECT content FROM pages WHERE id=?","old-page","saved BlockNote content"},
   {"SELECT title FROM ai_sessions WHERE id=?","old-session","Keep AI session"},
   {"SELECT provider_id FROM ai_sessions WHERE id=?","old-session","existing-provider"},
  } {
   var got string
   if err:=db.QueryRow(c.query,c.arg).Scan(&got);err!=nil || got!=c.want { t.Fatalf("restart %d %s: got %q want %q err %v",restart,c.query,got,c.want,err) }
  }
  var migrations int
  if err:=db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations);err!=nil || migrations!=10 { t.Fatalf("migration count %d: %v",migrations,err) }
  if err:=db.Close();err!=nil { t.Fatal(err) }
 }
 after,err:=os.ReadFile(providerPath)
 if err!=nil || !bytes.Equal(before,after) { t.Fatalf("provider file changed: %v",err) }
}
