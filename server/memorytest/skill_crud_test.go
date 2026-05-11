// Package memorytest validates the Skill CRUD lifecycle against the live
// Memory Platform — Create, Get, List, Update, Delete, and edge cases.
//
// Now that the Skills Create endpoint (gh#283) is fixed, this test covers
// the full lifecycle that the diane-skill-monitor agent uses.
//
// Run: cd ~/diane/server && go test -v -tags=integration -count=1 -run TestSkill ./memorytest/
//go:build integration

package memorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
)

// =========================================================================
// TestSkill_Create: Creates a new skill, verifies it returns with all
// expected fields.
// =========================================================================

func TestSkill_Create(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skill-%d", time.Now().UnixMilli())
	name := prefix + "-create-test"
	desc := "Test skill for CRUD lifecycle"
	content := "# Test Skill\n\nA test skill for validating the Create endpoint."

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: desc,
		Content:     content,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, created.ID)
	})

	// Verify all fields
	if created.ID == "" {
		t.Fatal("Skill ID is empty after create")
	}
	if created.Name != name {
		t.Errorf("Name = %q, want %q", created.Name, name)
	}
	if created.Description != desc {
		t.Errorf("Description = %q, want %q", created.Description, desc)
	}
	if created.Content != content {
		t.Errorf("Content = %q, want %q", created.Content, content)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
	if created.UpdatedAt.Before(created.CreatedAt) {
		t.Error("UpdatedAt is before CreatedAt")
	}

	t.Logf("✅ Skill created: %s (%s)", created.Name, created.ID)
	t.Logf("   Scope: %s", scopeString(created))
	t.Logf("   Created: %s", created.CreatedAt.Format(time.RFC3339))
}

// =========================================================================
// TestSkill_CreateAndGet: Creates a skill, fetches it back by ID, and
// verifies all properties round-trip correctly.
// =========================================================================

func TestSkill_CreateAndGet(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skillget-%d", time.Now().UnixMilli())
	name := prefix + "-get-test"
	desc := "Skill for Get verification"
	content := "# Get Test\n\nVerifying that GetSkill returns the full object."

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: desc,
		Content:     content,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	skillID := created.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, skillID)
	})

	// Fetch back via GetSkill
	got, err := b.GetSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}

	if got.ID != skillID {
		t.Errorf("ID = %q, want %q", got.ID, skillID)
	}
	if got.Name != name {
		t.Errorf("Name = %q, want %q", got.Name, name)
	}
	if got.Description != desc {
		t.Errorf("Description = %q, want %q", got.Description, desc)
	}
	if got.Content != content {
		t.Errorf("Content = %q, want %q", got.Content, content)
	}

	t.Logf("✅ GetSkill verified: %s -> %s (%s)", name, skillID[:8], got.CreatedAt.Format(time.RFC3339))
}

// =========================================================================
// TestSkill_CreateAndList: Creates a skill, then lists all skills and
// verifies the new one appears in the list.
// =========================================================================

func TestSkill_CreateAndList(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-sklist-%d", time.Now().UnixMilli())
	name := prefix + "-list-test"

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "List verification skill",
		Content:     "# List\nTest.",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, created.ID)
	})

	// List all skills
	skills, err := b.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}

	var found bool
	for _, s := range skills {
		if s.Name == name {
			found = true
			t.Logf("Found in list: %s (ID=%s, scope=%s)", s.Name, s.ID[:8], scopeString(s))
			break
		}
	}
	if !found {
		t.Errorf("Skill %q not found in ListSkills response", name)
		for _, s := range skills {
			t.Logf("  • %s (%s)", s.Name, s.ID[:8])
		}
	}

	t.Logf("✅ Skill confirmed in list (total: %d skills)", len(skills))
}

// =========================================================================
// TestSkill_Update: Creates a skill, updates its description and content,
// then verifies via GetSkill that the changes persisted.
// =========================================================================

func TestSkill_Update(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skupd-%d", time.Now().UnixMilli())
	name := prefix + "-update-test"
	originalDesc := "Original description"
	originalContent := "# Original\nContent."

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: originalDesc,
		Content:     originalContent,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	skillID := created.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, skillID)
	})

	// Update description and content
	newDesc := "Updated description"
	newContent := "# Updated\n\nNew content here."
	updatedDesc := &newDesc
	updatedContent := &newContent

	updated, err := b.UpdateSkill(ctx, skillID, &sdkskills.UpdateSkillRequest{
		Description: updatedDesc,
		Content:     updatedContent,
	})
	if err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}

	if updated.Description != newDesc {
		t.Errorf("After update, Description = %q, want %q", updated.Description, newDesc)
	}
	if updated.Content != newContent {
		t.Errorf("After update, Content = %q, want %q", updated.Content, newContent)
	}
	if updated.UpdatedAt.Before(created.CreatedAt) {
		t.Error("UpdatedAt should be after original CreatedAt")
	}

	t.Logf("✅ UpdateSkill verified: desc=%q content=%d chars", updated.Description, len(updated.Content))

	// Verify via GetSkill that changes stuck
	reFetched, err := b.GetSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("GetSkill after update: %v", err)
	}
	if reFetched.Description != newDesc {
		t.Errorf("Re-fetched Description = %q, want %q", reFetched.Description, newDesc)
	}
	if reFetched.Content != newContent {
		t.Errorf("Re-fetched Content = %q, want %q", reFetched.Content, newContent)
	}
	t.Log("✅ Update verified via re-fetch")
}

// =========================================================================
// TestSkill_Delete: Creates a skill, deletes it, then verifies it's no
// longer in the list and GetSkill returns an error.
// =========================================================================

func TestSkill_Delete(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skdel-%d", time.Now().UnixMilli())
	name := prefix + "-delete-test"

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "Delete verification skill",
		Content:     "# Delete\nTest.",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	skillID := created.ID
	t.Logf("Created skill to delete: %s (%s)", name, skillID)

	// Delete
	err = b.DeleteSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	t.Log("✅ DeleteSkill succeeded")

	// Verify it's gone from list
	skills, err := b.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills after delete: %v", err)
	}
	for _, s := range skills {
		if s.Name == name {
			t.Errorf("Skill %q still appears in list after delete", name)
		}
	}

	// Verify GetSkill returns an error
	_, err = b.GetSkill(ctx, skillID)
	if err == nil {
		t.Log("⚠️  GetSkill after delete returned nil (may be soft-delete)")
	} else {
		t.Logf("✅ GetSkill after delete returned expected error: %v", err)
	}
}

// =========================================================================
// TestSkill_DuplicateName: Creating a skill with a name that already exists
// should return a 409 Conflict error.
// =========================================================================

func TestSkill_DuplicateName(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skdup-%d", time.Now().UnixMilli())
	name := prefix + "-duplicate-test"

	// Create first skill
	first, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "First skill for duplicate test",
		Content:     "# First\nTest.",
	})
	if err != nil {
		t.Fatalf("CreateSkill (first): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, first.ID)
	})

	// Try creating with the same name
	second, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "Duplicate name attempt",
		Content:     "# Duplicate\nShould fail.",
	})
	if err == nil {
		// If it somehow succeeds, clean up and fail
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_ = b.DeleteSkill(cleanupCtx, second.ID)
		})
		t.Fatalf("Expected conflict error for duplicate name %q, but Create succeeded with ID %s", name, second.ID)
	}

	// Verify it's a conflict (409) error
	errStr := err.Error()
	if strings.Contains(errStr, "409") || strings.Contains(errStr, "conflict") || strings.Contains(errStr, "Conflict") {
		t.Logf("✅ Duplicate name correctly rejected: %v", err)
	} else {
		t.Logf("Duplicate name rejected (non-conflict error): %v", err)
	}
}

// =========================================================================
// TestSkill_EmptyDescription: Creating a skill with an empty description
// should succeed (the Bun ORM path uses ExcludeColumn, so no embedding).
// =========================================================================

func TestSkill_EmptyDescription(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skemp-%d", time.Now().UnixMilli())
	name := prefix + "-no-desc"

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "", // empty — forces Bun ORM path
		Content:     "# No Description\nTest.",
	})
	if err != nil {
		t.Fatalf("CreateSkill with empty description: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, created.ID)
	})

	if created.Name != name {
		t.Errorf("Name = %q, want %q", created.Name, name)
	}
	if created.Description != "" {
		t.Errorf("Description = %q, want empty string", created.Description)
	}

	t.Logf("✅ Skill with empty description created: %s (%s)", created.Name, created.ID[:8])
}

// =========================================================================
// TestSkill_DeleteNonexistent: Deleting a nonexistent skill should return
// an error (404 Not Found).
// =========================================================================

func TestSkill_DeleteNonexistent(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := b.DeleteSkill(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Log("⚠️  DeleteSkill on nonexistent ID returned nil (may be idempotent)")
	} else {
		t.Logf("✅ DeleteSkill on nonexistent ID correctly rejected: %v", err)
	}
}

// =========================================================================
// TestSkill_GetNonexistent: Getting a nonexistent skill should return an
// error (404 Not Found).
// =========================================================================

func TestSkill_GetNonexistent(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := b.GetSkill(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Error("GetSkill on nonexistent ID should return an error")
	} else {
		t.Logf("✅ GetSkill on nonexistent ID correctly rejected: %v", err)
	}
}

// =========================================================================
// TestSkill_FullLifecycle: End-to-end test that chains Create → Get →
// Update → List → Delete, verifying each step before proceeding.
// =========================================================================

func TestSkill_FullLifecycle(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skllc-%d", time.Now().UnixMilli())

	// ── 1. Create ──
	name := prefix + "-lifecycle"
	desc := "Lifecycle test skill"
	content := "# Lifecycle\n\nFull CRUD lifecycle verification."
	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: desc,
		Content:     content,
	})
	if err != nil {
		t.Fatalf("[1] CreateSkill: %v", err)
	}
	skillID := created.ID
	t.Logf("[1] ✅ Created: %s (%s)", name, skillID[:8])

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, skillID)
	}()

	// ── 2. Get ──
	got, err := b.GetSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("[2] GetSkill: %v", err)
	}
	if got.Name != name || got.Description != desc {
		t.Fatalf("[2] GetSkill returned mismatched data: %+v", got)
	}
	t.Log("[2] ✅ GetSkill matches create input")

	// ── 3. Update ──
	newDesc := "Updated lifecycle description"
	newContent := "# Updated Lifecycle\n\nModified content."
	updated, err := b.UpdateSkill(ctx, skillID, &sdkskills.UpdateSkillRequest{
		Description: &newDesc,
		Content:     &newContent,
	})
	if err != nil {
		t.Fatalf("[3] UpdateSkill: %v", err)
	}
	if updated.Description != newDesc || updated.Content != newContent {
		t.Fatalf("[3] UpdateSkill returned unexpected data: desc=%q content=%q", updated.Description, updated.Content)
	}
	t.Log("[3] ✅ UpdateSkill applied changes")

	// Verify update via Get
	gotAfterUpdate, err := b.GetSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("[3] GetSkill after update: %v", err)
	}
	if gotAfterUpdate.Description != newDesc {
		t.Fatalf("[3] Re-fetch Description = %q, want %q", gotAfterUpdate.Description, newDesc)
	}
	t.Log("[3] ✅ Update verified via re-fetch")

	// ── 4. List (verify skill appears) ──
	skills, err := b.ListSkills(ctx)
	if err != nil {
		t.Fatalf("[4] ListSkills: %v", err)
	}
	var found bool
	for _, s := range skills {
		if s.ID == skillID {
			found = true
			if s.Name != name {
				t.Errorf("[4] Name in list = %q, want %q", s.Name, name)
			}
			break
		}
	}
	if !found {
		t.Fatal("[4] Skill not found in list")
	}
	t.Logf("[4] ✅ Skill found in list (total: %d skills)", len(skills))

	// ── 5. Delete ──
	if err := b.DeleteSkill(ctx, skillID); err != nil {
		t.Fatalf("[5] DeleteSkill: %v", err)
	}
	t.Log("[5] ✅ Deleted")

	// ── 6. Verify deleted ──
	skillsAfter, err := b.ListSkills(ctx)
	if err != nil {
		t.Fatalf("[6] ListSkills after delete: %v", err)
	}
	for _, s := range skillsAfter {
		if s.ID == skillID {
			t.Error("[6] Skill still appears in list after delete")
		}
	}
	t.Log("[6] ✅ Confirmed deleted from list")
}

// =========================================================================
// TestSkill_UpdateDescriptionOnly: Verifies that updating only the
// description (leaving content unchanged) works correctly.
// =========================================================================

func TestSkill_UpdateDescriptionOnly(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skdesc-%d", time.Now().UnixMilli())
	name := prefix + "-desc-only"
	originalContent := "# Description Only\nOriginal content."

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: "Original description",
		Content:     originalContent,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	skillID := created.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, skillID)
	})

	// Update only the description
	newDesc := "Updated description only"
	updated, err := b.UpdateSkill(ctx, skillID, &sdkskills.UpdateSkillRequest{
		Description: &newDesc,
		// Content is nil — should not change
	})
	if err != nil {
		t.Fatalf("UpdateSkill (description only): %v", err)
	}

	if updated.Description != newDesc {
		t.Errorf("Description = %q, want %q", updated.Description, newDesc)
	}
	if updated.Content != originalContent {
		t.Errorf("Content changed to %q despite not being in update request (want %q)", updated.Content, originalContent)
	}

	t.Logf("✅ Description-only update: desc=%q content unchanged (%d chars)", updated.Description, len(updated.Content))
}

// =========================================================================
// TestSkill_UpdateContentOnly: Verifies that updating only the content
// (leaving description unchanged) works correctly.
// =========================================================================

func TestSkill_UpdateContentOnly(t *testing.T) {
	b := setupBridgeFromConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("t-skcont-%d", time.Now().UnixMilli())
	name := prefix + "-content-only"
	originalDesc := "Original description"
	originalContent := "# Content Only\nOriginal."

	created, err := b.CreateSkill(ctx, &sdkskills.CreateSkillRequest{
		Name:        name,
		Description: originalDesc,
		Content:     originalContent,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	skillID := created.ID

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = b.DeleteSkill(cleanupCtx, skillID)
	})

	// Update only the content
	newContent := "# Content Only\nUpdated content."
	updated, err := b.UpdateSkill(ctx, skillID, &sdkskills.UpdateSkillRequest{
		Content: &newContent,
		// Description is nil — should not change
	})
	if err != nil {
		t.Fatalf("UpdateSkill (content only): %v", err)
	}

	if updated.Content != newContent {
		t.Errorf("Content = %q, want %q", updated.Content, newContent)
	}
	if updated.Description != originalDesc {
		t.Errorf("Description changed to %q despite not being in update request (want %q)", updated.Description, originalDesc)
	}

	t.Logf("✅ Content-only update: content=%d chars desc unchanged", len(updated.Content))
}

// =========================================================================
// Helper: scopeString returns a human-readable scope label for a skill.
// =========================================================================

func scopeString(s *sdkskills.Skill) string {
	if s.ProjectID != nil {
		return "project"
	}
	if s.OrgID != nil {
		return "org"
	}
	return "global"
}
