// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// UserProfileStore handles user_profiles table operations.
type UserProfileStore struct {
	db *sql.DB
}

// NewUserProfileStore creates a new UserProfileStore.
func NewUserProfileStore(db *sql.DB) *UserProfileStore {
	return &UserProfileStore{db: db}
}

const userProfileColumns = `user_id, slug, bio, avatar_url, website, location,
	job_title, pronouns, twitter, github, linkedin, instagram,
	is_published, created_at, updated_at`

// scanUserProfile scans a row into a UserProfile struct.
func scanUserProfile(scanner interface{ Scan(...any) error }) (*models.UserProfile, error) {
	var p models.UserProfile
	err := scanner.Scan(
		&p.UserID, &p.Slug, &p.Bio, &p.AvatarURL, &p.Website, &p.Location,
		&p.JobTitle, &p.Pronouns, &p.Twitter, &p.GitHub, &p.LinkedIn, &p.Instagram,
		&p.IsPublished, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByUserID retrieves a profile by user ID. Returns nil if not found.
func (s *UserProfileStore) FindByUserID(userID uuid.UUID) (*models.UserProfile, error) {
	row := s.db.QueryRow(`SELECT `+userProfileColumns+` FROM user_profiles WHERE user_id = $1`, userID)
	p, err := scanUserProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user profile: %w", err)
	}
	return p, nil
}

// Upsert creates or updates a user profile. Uses INSERT ON CONFLICT for
// lazy creation — the row is created on first save.
func (s *UserProfileStore) Upsert(p *models.UserProfile) error {
	_, err := s.db.Exec(`
		INSERT INTO user_profiles (user_id, slug, bio, avatar_url, website, location,
			job_title, pronouns, twitter, github, linkedin, instagram, is_published)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (user_id) DO UPDATE SET
			slug = EXCLUDED.slug,
			bio = EXCLUDED.bio,
			avatar_url = EXCLUDED.avatar_url,
			website = EXCLUDED.website,
			location = EXCLUDED.location,
			job_title = EXCLUDED.job_title,
			pronouns = EXCLUDED.pronouns,
			twitter = EXCLUDED.twitter,
			github = EXCLUDED.github,
			linkedin = EXCLUDED.linkedin,
			instagram = EXCLUDED.instagram,
			is_published = EXCLUDED.is_published,
			updated_at = NOW()
	`, p.UserID, p.Slug, p.Bio, p.AvatarURL, p.Website, p.Location,
		p.JobTitle, p.Pronouns, p.Twitter, p.GitHub, p.LinkedIn, p.Instagram, p.IsPublished)
	if err != nil {
		return fmt.Errorf("upsert user profile: %w", err)
	}
	return nil
}

// FindByUserIDs retrieves profiles for multiple users at once.
// Returns a map of user_id → profile. Users without profiles are absent.
func (s *UserProfileStore) FindByUserIDs(userIDs []uuid.UUID) (map[uuid.UUID]*models.UserProfile, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	// Build placeholder list for IN clause.
	placeholders := ""
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	rows, err := s.db.Query(`
		SELECT `+userProfileColumns+`
		FROM user_profiles
		WHERE user_id IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("find user profiles by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[uuid.UUID]*models.UserProfile)
	for rows.Next() {
		p, err := scanUserProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user profile: %w", err)
		}
		result[p.UserID] = p
	}
	return result, rows.Err()
}

// FindAuthorByUserID retrieves the display name and profile for a user.
// Returns the display name and profile (profile may be nil if not yet created).
func (s *UserProfileStore) FindAuthorByUserID(userID uuid.UUID) (string, *models.UserProfile, error) {
	var displayName string
	var p models.UserProfile
	var slug, bio, avatarURL, website, location, jobTitle, pronouns sql.NullString
	var twitter, github, linkedin, instagram sql.NullString
	var isPublished sql.NullBool
	var createdAt, updatedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT u.display_name,
			   p.slug, p.bio, p.avatar_url, p.website, p.location,
			   p.job_title, p.pronouns, p.twitter, p.github, p.linkedin, p.instagram,
			   p.is_published, p.created_at, p.updated_at
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE u.id = $1
	`, userID).Scan(
		&displayName,
		&slug, &bio, &avatarURL, &website, &location,
		&jobTitle, &pronouns, &twitter, &github, &linkedin, &instagram,
		&isPublished, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("find author by user id: %w", err)
	}

	// No profile row exists (LEFT JOIN nulls).
	if !createdAt.Valid {
		return displayName, nil, nil
	}

	p.UserID = userID
	p.Slug = slug.String
	p.Bio = bio.String
	p.AvatarURL = avatarURL.String
	p.Website = website.String
	p.Location = location.String
	p.JobTitle = jobTitle.String
	p.Pronouns = pronouns.String
	p.Twitter = twitter.String
	p.GitHub = github.String
	p.LinkedIn = linkedin.String
	p.Instagram = instagram.String
	p.IsPublished = isPublished.Bool
	p.CreatedAt = createdAt.Time
	p.UpdatedAt = updatedAt.Time

	return displayName, &p, nil
}

// FindAuthorBySlug retrieves the user ID, display name, and profile for
// an author identified by their profile slug.
// FindAuthorBySlug retrieves the user ID, display name, and profile for
// an author identified by their profile slug. Only returns published profiles
// so that unpublished author pages are not accessible on the public site.
func (s *UserProfileStore) FindAuthorBySlug(slug string) (uuid.UUID, string, *models.UserProfile, error) {
	var userID uuid.UUID
	var displayName string
	var p models.UserProfile

	err := s.db.QueryRow(`
		SELECT p.user_id, u.display_name,
			   p.slug, p.bio, p.avatar_url, p.website, p.location,
			   p.job_title, p.pronouns, p.twitter, p.github, p.linkedin, p.instagram,
			   p.is_published, p.created_at, p.updated_at
		FROM user_profiles p
		JOIN users u ON u.id = p.user_id
		WHERE p.slug = $1 AND p.is_published = TRUE
	`, slug).Scan(
		&userID, &displayName,
		&p.Slug, &p.Bio, &p.AvatarURL, &p.Website, &p.Location,
		&p.JobTitle, &p.Pronouns, &p.Twitter, &p.GitHub, &p.LinkedIn, &p.Instagram,
		&p.IsPublished, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", nil, nil
	}
	if err != nil {
		return uuid.Nil, "", nil, fmt.Errorf("find author by slug: %w", err)
	}

	p.UserID = userID
	return userID, displayName, &p, nil
}
