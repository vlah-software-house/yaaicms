// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// MenuLocations defines the predefined menu locations.
var MenuLocations = []string{"main", "footer", "footer_legal"}

// MenuStore manages menus and menu items in the database.
type MenuStore struct {
	db *sql.DB
}

// NewMenuStore returns a new MenuStore.
func NewMenuStore(db *sql.DB) *MenuStore {
	return &MenuStore{db: db}
}

const menuItemColumns = `id, menu_id, parent_id, label, url, content_id, target, sort_order, created_at, updated_at`

// scanMenuItem scans a row into a MenuItem struct.
func scanMenuItem(scanner interface{ Scan(...any) error }) (*models.MenuItem, error) {
	var m models.MenuItem
	err := scanner.Scan(
		&m.ID, &m.MenuID, &m.ParentID, &m.Label, &m.URL,
		&m.ContentID, &m.Target, &m.SortOrder, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// EnsureLocations creates menu rows for all predefined locations if they don't exist.
func (s *MenuStore) EnsureLocations(tenantID uuid.UUID) error {
	for _, loc := range MenuLocations {
		_, err := s.db.Exec(`
			INSERT INTO menus (tenant_id, location)
			VALUES ($1, $2)
			ON CONFLICT (tenant_id, location) DO NOTHING
		`, tenantID, loc)
		if err != nil {
			return fmt.Errorf("ensure menu location %s: %w", loc, err)
		}
	}
	return nil
}

// FindByLocation returns a menu with its nested items tree for a given location.
func (s *MenuStore) FindByLocation(tenantID uuid.UUID, location string) (*models.Menu, error) {
	var m models.Menu
	err := s.db.QueryRow(`
		SELECT id, tenant_id, location, created_at, updated_at
		FROM menus WHERE tenant_id = $1 AND location = $2
	`, tenantID, location).Scan(&m.ID, &m.TenantID, &m.Location, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find menu by location: %w", err)
	}

	items, err := s.listItems(m.ID)
	if err != nil {
		return nil, err
	}
	m.Items = buildMenuTree(items, nil)
	return &m, nil
}

// AllWithItems returns all menus for a tenant with their items as nested trees.
func (s *MenuStore) AllWithItems(tenantID uuid.UUID) ([]models.Menu, error) {
	rows, err := s.db.Query(`
		SELECT id, tenant_id, location, created_at, updated_at
		FROM menus WHERE tenant_id = $1
		ORDER BY CASE location
			WHEN 'main' THEN 1
			WHEN 'footer' THEN 2
			WHEN 'footer_legal' THEN 3
		END
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list menus: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var menus []models.Menu
	for rows.Next() {
		var m models.Menu
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Location, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan menu: %w", err)
		}
		menus = append(menus, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load items for each menu and build trees.
	for i := range menus {
		items, err := s.listItems(menus[i].ID)
		if err != nil {
			return nil, err
		}
		menus[i].Items = buildMenuTree(items, nil)
	}
	return menus, nil
}

// listItems returns all items for a menu ordered by sort_order.
func (s *MenuStore) listItems(menuID uuid.UUID) ([]models.MenuItem, error) {
	rows, err := s.db.Query(`
		SELECT `+menuItemColumns+`
		FROM menu_items WHERE menu_id = $1
		ORDER BY sort_order, label
	`, menuID)
	if err != nil {
		return nil, fmt.Errorf("list menu items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.MenuItem
	for rows.Next() {
		mi, err := scanMenuItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan menu item: %w", err)
		}
		items = append(items, *mi)
	}
	return items, rows.Err()
}

// buildMenuTree recursively builds a tree from a flat list of menu items.
func buildMenuTree(flat []models.MenuItem, parentID *uuid.UUID) []models.MenuItem {
	var result []models.MenuItem
	for _, item := range flat {
		if ptrEqual(item.ParentID, parentID) {
			item.Children = buildMenuTree(flat, &item.ID)
			result = append(result, item)
		}
	}
	return result
}

// FindItemByID retrieves a single menu item by ID, scoped to a tenant via its menu.
// Returns nil if not found.
func (s *MenuStore) FindItemByID(tenantID, id uuid.UUID) (*models.MenuItem, error) {
	row := s.db.QueryRow(`
		SELECT `+menuItemColumns+` FROM menu_items
		WHERE id = $1 AND menu_id IN (SELECT id FROM menus WHERE tenant_id = $2)
	`, id, tenantID)
	mi, err := scanMenuItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find menu item by id: %w", err)
	}
	return mi, nil
}

// CreateItem inserts a new menu item and returns it.
func (s *MenuStore) CreateItem(item *models.MenuItem) (*models.MenuItem, error) {
	row := s.db.QueryRow(`
		INSERT INTO menu_items (menu_id, parent_id, label, url, content_id, target, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+menuItemColumns,
		item.MenuID, item.ParentID, item.Label, item.URL,
		item.ContentID, item.Target, item.SortOrder,
	)
	result, err := scanMenuItem(row)
	if err != nil {
		return nil, fmt.Errorf("create menu item: %w", err)
	}
	return result, nil
}

// UpdateItem modifies an existing menu item, scoped to a tenant via its menu.
func (s *MenuStore) UpdateItem(tenantID uuid.UUID, item *models.MenuItem) error {
	_, err := s.db.Exec(`
		UPDATE menu_items SET
			label = $1, url = $2, content_id = $3, target = $4,
			parent_id = $5, sort_order = $6, updated_at = NOW()
		WHERE id = $7
		  AND menu_id IN (SELECT id FROM menus WHERE tenant_id = $8)
	`, item.Label, item.URL, item.ContentID, item.Target,
		item.ParentID, item.SortOrder, item.ID, tenantID)
	if err != nil {
		return fmt.Errorf("update menu item: %w", err)
	}
	return nil
}

// DeleteItem removes a menu item by ID, scoped to a tenant via its menu.
func (s *MenuStore) DeleteItem(tenantID, id uuid.UUID) error {
	_, err := s.db.Exec(`
		DELETE FROM menu_items
		WHERE id = $1 AND menu_id IN (SELECT id FROM menus WHERE tenant_id = $2)
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete menu item: %w", err)
	}
	return nil
}

// MenuReorderItem represents a single item in a reorder request.
type MenuReorderItem struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parent_id"`
	Order    int        `json:"order"`
}

// ReorderItems updates sort_order and parent_id for multiple menu items in a transaction.
func (s *MenuStore) ReorderItems(menuID uuid.UUID, items []MenuReorderItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		UPDATE menu_items SET parent_id = $1, sort_order = $2, updated_at = $3
		WHERE id = $4 AND menu_id = $5`)
	if err != nil {
		return fmt.Errorf("prepare reorder: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now()
	for _, item := range items {
		if _, err := stmt.Exec(item.ParentID, item.Order, now, item.ID, menuID); err != nil {
			return fmt.Errorf("reorder menu item %s: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// NextItemSortOrder returns the next sort_order value for items within a menu.
func (s *MenuStore) NextItemSortOrder(menuID uuid.UUID, parentID *uuid.UUID) (int, error) {
	var maxOrder sql.NullInt64
	var err error
	if parentID == nil {
		err = s.db.QueryRow(`SELECT MAX(sort_order) FROM menu_items WHERE menu_id = $1 AND parent_id IS NULL`, menuID).Scan(&maxOrder)
	} else {
		err = s.db.QueryRow(`SELECT MAX(sort_order) FROM menu_items WHERE menu_id = $1 AND parent_id = $2`, menuID, *parentID).Scan(&maxOrder)
	}
	if err != nil {
		return 0, err
	}
	if maxOrder.Valid {
		return int(maxOrder.Int64) + 1, nil
	}
	return 0, nil
}

// FindMenuByID retrieves a menu by ID within a tenant. Returns nil if not found.
func (s *MenuStore) FindMenuByID(tenantID, id uuid.UUID) (*models.Menu, error) {
	var m models.Menu
	err := s.db.QueryRow(`
		SELECT id, tenant_id, location, created_at, updated_at
		FROM menus WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(&m.ID, &m.TenantID, &m.Location, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find menu by id: %w", err)
	}
	return &m, nil
}
