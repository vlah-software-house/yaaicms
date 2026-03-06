// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"testing"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// testTemplateTenantID is a fixed tenant ID used across template store tests.
var testTemplateTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func TestTemplateStoreCreateAndFind(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	name := "Test Template " + uuid.NewString()[:8]
	t.Cleanup(func() { cleanTemplates(t, db, name) })

	tmpl := &models.Template{
		Name:        name,
		Type:        models.TemplateTypePage,
		HTMLContent: "<h1>{{.Title}}</h1>",
	}

	created, err := s.Create(testTemplateTenantID, tmpl)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if created.Name != name {
		t.Errorf("name: got %q, want %q", created.Name, name)
	}
	if created.Version != 1 {
		t.Errorf("version: got %d, want 1", created.Version)
	}
	if created.IsActive {
		t.Error("new templates should not be active")
	}

	// FindByID.
	found, err := s.FindByID(testTemplateTenantID, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("expected template, got nil")
	}
	if found.HTMLContent != "<h1>{{.Title}}</h1>" {
		t.Errorf("html_content mismatch")
	}

	// Not found.
	found, _ = s.FindByID(testTemplateTenantID, uuid.New())
	if found != nil {
		t.Error("expected nil for random UUID")
	}
}

func TestTemplateStoreUpdate(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	name := "Update Template " + uuid.NewString()[:8]
	t.Cleanup(func() { cleanTemplates(t, db, name, "Renamed Template") })

	created, _ := s.Create(testTemplateTenantID, &models.Template{
		Name: name, Type: models.TemplateTypeHeader,
		HTMLContent: "<header>old</header>",
	})

	created.Name = "Renamed Template"
	created.HTMLContent = "<header>new</header>"

	if err := s.Update(created); err != nil {
		t.Fatalf("Update: %v", err)
	}

	found, _ := s.FindByID(testTemplateTenantID, created.ID)
	if found.HTMLContent != "<header>new</header>" {
		t.Errorf("html_content: got %q, want new", found.HTMLContent)
	}
	if found.Version != 2 {
		t.Errorf("version: got %d, want 2 (incremented)", found.Version)
	}
}

func TestTemplateStoreActivate(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	name1 := "Activate A " + uuid.NewString()[:8]
	name2 := "Activate B " + uuid.NewString()[:8]
	t.Cleanup(func() { cleanTemplates(t, db, name1, name2) })

	a, _ := s.Create(testTemplateTenantID, &models.Template{
		Name: name1, Type: models.TemplateTypeFooter,
		HTMLContent: "<footer>A</footer>",
	})
	b, _ := s.Create(testTemplateTenantID, &models.Template{
		Name: name2, Type: models.TemplateTypeFooter,
		HTMLContent: "<footer>B</footer>",
	})

	// Activate A.
	if err := s.Activate(testTemplateTenantID, a.ID); err != nil {
		t.Fatalf("Activate A: %v", err)
	}

	active, _ := s.FindActiveByType(testTemplateTenantID, models.TemplateTypeFooter)
	if active == nil {
		t.Fatal("expected active footer template")
	}
	if active.ID != a.ID {
		t.Errorf("expected A to be active, got %s", active.ID)
	}

	// Activate B — should deactivate A.
	if err := s.Activate(testTemplateTenantID, b.ID); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	active, _ = s.FindActiveByType(testTemplateTenantID, models.TemplateTypeFooter)
	if active.ID != b.ID {
		t.Errorf("expected B to be active after switch, got %s", active.ID)
	}

	// Verify A is no longer active.
	aRefresh, _ := s.FindByID(testTemplateTenantID, a.ID)
	if aRefresh.IsActive {
		t.Error("A should no longer be active")
	}
}

func TestTemplateStoreDeleteInactive(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	name := "Delete Me " + uuid.NewString()[:8]

	created, _ := s.Create(testTemplateTenantID, &models.Template{
		Name: name, Type: models.TemplateTypePage,
		HTMLContent: "<p>delete</p>",
	})

	// Delete inactive — should succeed.
	if err := s.Delete(testTemplateTenantID, created.ID); err != nil {
		t.Fatalf("Delete inactive: %v", err)
	}

	found, _ := s.FindByID(testTemplateTenantID, created.ID)
	if found != nil {
		t.Error("expected nil after delete")
	}
}

func TestTemplateStoreDeleteActiveBlocked(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	name := "Active No Delete " + uuid.NewString()[:8]
	t.Cleanup(func() {
		// Deactivate so cleanup can delete it.
		_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE name = $1", name)
		cleanTemplates(t, db, name)
	})

	created, _ := s.Create(testTemplateTenantID, &models.Template{
		Name: name, Type: models.TemplateTypeHeader,
		HTMLContent: "<header>nodelet</header>",
	})

	_ = s.Activate(testTemplateTenantID, created.ID)

	// Delete active — should fail.
	err := s.Delete(testTemplateTenantID, created.ID)
	if err == nil {
		t.Error("expected error when deleting active template")
	}
}

func TestTemplateStoreList(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	// Create a template to ensure List returns at least one.
	name := "List Test Template " + uuid.NewString()[:8]
	t.Cleanup(func() { cleanTemplates(t, db, name) })

	_, _ = s.Create(testTemplateTenantID, &models.Template{
		Name:        name,
		Type:        models.TemplateTypeHeader,
		HTMLContent: "<header>list test</header>",
	})

	templates, err := s.List(testTemplateTenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(templates) < 1 {
		t.Error("expected at least 1 template")
	}
}

func TestTemplateStoreCount(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	count, err := s.Count(testTemplateTenantID)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 0 {
		t.Error("expected non-negative count")
	}
}

func TestTemplateStoreFindActiveByTypeNone(t *testing.T) {
	db := testDB(t)
	s := NewTemplateStore(db)

	// Use a type that might not have any active templates.
	// This test verifies nil is returned, not an error.
	// (If seed data has active templates, this just verifies the happy path.)
	result, err := s.FindActiveByType(testTemplateTenantID, "nonexistent_type")
	if err != nil {
		t.Fatalf("FindActiveByType: %v", err)
	}
	if result != nil {
		t.Error("expected nil for nonexistent type")
	}
}
