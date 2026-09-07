// Router-facing helpers for the group domain (M1).
//
// These are PINNED CONTRACTS imported verbatim by M2 — do not change their
// signatures. They answer membership questions with plain *gorm.DB reads so
// hot paths (gateway fan-out, mute checks) never need a GroupService.
package group

import (
	"errors"

	"gorm.io/gorm"
)

// BuildCovID returns the canonical group conversation id: cov:grp:<groupID>.
func BuildCovID(groupID string) string { return "cov:grp:" + groupID }

// IsMember reports whether userID is an ACTIVE member of groupID
// (member row present and not soft-deleted, group present and active).
// A missing group or non-member yields (false, nil); only DB failures error.
func IsMember(db *gorm.DB, groupID, userID string) (bool, error) {
	if groupID == "" || userID == "" {
		return false, nil
	}
	return isMemberOf(db, groupID, userID)
}

// IsMuted reports whether userID is an ACTIVE, muted member of groupID.
// Non-members (and members of missing/dismissed groups) yield (false, nil).
func IsMuted(db *gorm.DB, groupID, userID string) (bool, error) {
	if groupID == "" || userID == "" {
		return false, nil
	}
	if ok, err := isMemberOf(db, groupID, userID); err != nil || !ok {
		return false, err
	}
	var m memberState
	if err := db.Table("group_members").
		Select("muted").
		Where("group_id = ? AND user_id = ? AND deleted_at IS NULL", groupID, userID).
		First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return m.Muted, nil
}

// ListActiveMemberIDs returns user ids of all active members (used by fan-out).
// A missing/dismissed group yields an empty (non-nil) slice and nil error.
func ListActiveMemberIDs(db *gorm.DB, groupID string) ([]string, error) {
	out := []string{}
	if groupID == "" {
		return out, nil
	}
	if err := db.Table("group_members").
		Select("user_id").
		Where("group_id = ? AND deleted_at IS NULL", groupID).
		Order("id ASC").
		Scan(&out).Error; err != nil {
		return []string{}, err
	}
	// Dismissed groups fan out to nobody even if rows linger.
	active, err := groupIsActive(db, groupID)
	if err != nil {
		return []string{}, err
	}
	if !active {
		return []string{}, nil
	}
	return out, nil
}

// memberState is a minimal scan target for the muted flag.
type memberState struct {
	Muted bool
}

// isMemberOf is the shared active-membership probe: group must exist with
// status=1 (and not soft-deleted) and an live member row must exist.
func isMemberOf(db *gorm.DB, groupID, userID string) (bool, error) {
	active, err := groupIsActive(db, groupID)
	if err != nil {
		return false, err
	}
	if !active {
		return false, nil
	}
	var cnt int64
	if err := db.Table("group_members").
		Where("group_id = ? AND user_id = ? AND deleted_at IS NULL", groupID, userID).
		Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// groupIsActive reports whether the group row exists (not soft-deleted) with
// status=1 (active; 0=dismissed).
func groupIsActive(db *gorm.DB, groupID string) (bool, error) {
	var cnt int64
	if err := db.Table("groups").
		Where("id = ? AND status = 1 AND deleted_at IS NULL", groupID).
		Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}
