package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDatabaseCRUD(t *testing.T) {
	ctx := setupTestContext(t)
	defer ctx.DB.Close()

	// create database
	body := `{"title":"Test DB"}`
	req := httptest.NewRequest(http.MethodPost, "/api/databases", strings.NewReader(body))
	res := httptest.NewRecorder()
	ctx.handleDatabases(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create db status=%d body=%s", res.Code, res.Body.String())
	}
	var db Database
	_ = json.Unmarshal(res.Body.Bytes(), &db)
	if db.ID == "" || db.PageID == "" {
		t.Fatalf("missing db id/pageId")
	}

	// add property
	propBody := `{"databaseId":"` + db.ID + `","name":"Status","type":"select","config":"{\"options\":[\"Todo\",\"Done\"]}"}`
	preq := httptest.NewRequest(http.MethodPost, "/api/db/properties", strings.NewReader(propBody))
	pres := httptest.NewRecorder()
	ctx.handleDBProperties(pres, preq)
	if pres.Code != http.StatusCreated {
		t.Fatalf("create prop status=%d body=%s", pres.Code, pres.Body.String())
	}
	var prop DBProperty
	_ = json.Unmarshal(pres.Body.Bytes(), &prop)

	// create record
	rreq := httptest.NewRequest(http.MethodPost, "/api/db/records", strings.NewReader(`{"databaseId":"`+db.ID+`","title":"Rec1"}`))
	rres := httptest.NewRecorder()
	ctx.handleDBRecords(rres, rreq)
	if rres.Code != http.StatusCreated {
		t.Fatalf("create record status=%d body=%s", rres.Code, rres.Body.String())
	}
	var rec DBRecord
	_ = json.Unmarshal(rres.Body.Bytes(), &rec)

	// update record value
	upd := `{"id":"` + rec.ID + `","title":"Rec1","values":{"` + prop.ID + `":"Done"},"version":` + strconvI(rec.Version) + `}`
	ureq := httptest.NewRequest(http.MethodPut, "/api/db/records", strings.NewReader(upd))
	ures := httptest.NewRecorder()
	ctx.handleDBRecords(ures, ureq)
	if ures.Code != http.StatusOK {
		t.Fatalf("update record status=%d body=%s", ures.Code, ures.Body.String())
	}

	// fetch records
	greq := httptest.NewRequest(http.MethodGet, "/api/db/records?databaseId="+db.ID, nil)
	gres := httptest.NewRecorder()
	ctx.handleDBRecords(gres, greq)
	if gres.Code != http.StatusOK {
		t.Fatalf("get records status=%d", gres.Code)
	}
	var recs []DBRecord
	_ = json.Unmarshal(gres.Body.Bytes(), &recs)
	if len(recs) != 1 || recs[0].Values[prop.ID] != "Done" {
		t.Fatalf("record value not persisted: %+v", recs)
	}

	// create view
	vreq := httptest.NewRequest(http.MethodPost, "/api/db/views", strings.NewReader(`{"databaseId":"`+db.ID+`","name":"Board","type":"board"}`))
	vres := httptest.NewRecorder()
	ctx.handleDBViews(vres, vreq)
	if vres.Code != http.StatusCreated {
		t.Fatalf("create view status=%d body=%s", vres.Code, vres.Body.String())
	}
}

func strconvI(i int) string {
	// avoid import cycle; simple
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}