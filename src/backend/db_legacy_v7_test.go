package main

import (
 "bytes"
 "os"
 "path/filepath"
 "testing"
)

// A synthetic v7 fixture is constructed in a temporary directory; no live
// database or user configuration is modified by this migration regression.
func TestSyntheticV7UpgradePreservesRecordsAndConfiguration(t *testing.T) {
 app := setupTestContext(t)
 root := app.RootPath
 database, record := dbRegressionCreate(t, app)
 project, err := app.createProject("Legacy project")
 if err != nil { t.Fatal(err) }
 if _, err := app.DB.Exec("INSERT INTO tasks(id,project_id,title) VALUES(?,?,?)", "legacy-task", project.ID, "Legacy task"); err != nil { t.Fatal(err) }
 if _, err := app.DB.Exec("INSERT INTO app_state(key,value) VALUES(?,?)", "legacy-marker", "persisted"); err != nil { t.Fatal(err) }
 providerPath := filepath.Join(root,"config","providers.json")
 before, err := os.ReadFile(providerPath)
 if err != nil { t.Fatal(err) }
 attachmentPath := filepath.Join(root,"data","legacy-file.bin")
 if err := os.WriteFile(attachmentPath,[]byte{0,1,2,255},0600); err != nil { t.Fatal(err) }
 removePageRecoveryTestSchema(t,app.DB)
 for _, statement := range []string{
  "DROP TABLE db_files",
  "DROP INDEX idx_db_records_live",
  "DROP INDEX idx_db_properties_live",
  "DROP INDEX idx_db_views_live",
  "ALTER TABLE databases DROP COLUMN deleted_at",
  "ALTER TABLE db_records DROP COLUMN deleted_at",
  "ALTER TABLE db_properties DROP COLUMN deleted_at",
  "ALTER TABLE db_views DROP COLUMN deleted_at",
  "DROP INDEX idx_db_records_database_created",
  "DROP INDEX idx_db_values_property",
  "DROP INDEX idx_db_properties_order",
  "DROP INDEX idx_db_views_order",
  "ALTER TABLE pages DROP COLUMN linked_view_id",
  "ALTER TABLE pages DROP COLUMN linked_database_id",
  "DELETE FROM schema_migrations WHERE version > 7",
 } {
  if _, err := app.DB.Exec(statement); err != nil { t.Fatalf("prepare v7 fixture %s: %v",statement,err) }
 }
 if err := app.DB.Close(); err != nil { t.Fatal(err) }
 for restart:=0;restart<2;restart++ {
  upgraded,err:=initializeDatabase(root)
  if err!=nil { t.Fatalf("upgrade restart %d: %v",restart,err) }
  for _,c:=range []struct{query,arg,want string}{
   {"SELECT title FROM db_records WHERE id=?",record.ID,record.Title},
   {"SELECT title FROM databases WHERE id=?",database.ID,database.Title},
   {"SELECT name FROM projects WHERE id=?",project.ID,"Legacy project"},
   {"SELECT title FROM tasks WHERE id=?","legacy-task","Legacy task"},
   {"SELECT value FROM app_state WHERE key=?","legacy-marker","persisted"},
  } {
   var got string
   if err:=upgraded.QueryRow(c.query,c.arg).Scan(&got);err!=nil || got!=c.want { t.Fatalf("restart %d: %s got %q want %q err %v",restart,c.query,got,c.want,err) }
  }
  var migrations int
  if err:=upgraded.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations);err!=nil || migrations!=latestSupportedSchemaMigration { t.Fatalf("migrations=%d err=%v",migrations,err) }
  if err:=upgraded.Close();err!=nil { t.Fatal(err) }
 }
 after,err:=os.ReadFile(providerPath)
 if err!=nil || !bytes.Equal(before,after) { t.Fatalf("provider configuration modified: %v",err) }
 file,err:=os.ReadFile(attachmentPath)
 if err!=nil || !bytes.Equal(file,[]byte{0,1,2,255}) { t.Fatalf("external file modified: %v",err) }
}
