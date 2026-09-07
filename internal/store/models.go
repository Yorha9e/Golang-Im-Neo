package store

import (
	"time"

	"gorm.io/gorm"
)

// User — users table (SSOT DATABASE_DESIGN_SPEC.md 2.1)
type User struct {
	ID           string         `gorm:"primaryKey;type:varchar(36)"`
	Username     string         `gorm:"type:varchar(64);not null;uniqueIndex:uq_users_username,where:deleted_at IS NULL"`
	Nickname     string         `gorm:"type:varchar(64);default:''"`
	PasswordHash string         `gorm:"type:varchar(128);not null"`
	AvatarURL    string         `gorm:"type:varchar(255);default:''"`
	Signature    string         `gorm:"type:varchar(255);default:''"`
	Role         string         `gorm:"type:varchar(16);not null;default:'user';index"`
	Status       int8           `gorm:"type:tinyint;not null;default:1;index"`
	TokenVersion int            `gorm:"not null;default:1"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// UserSession — user_sessions table (2.2)
type UserSession struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	UserID           string    `gorm:"type:varchar(36);not null;index"`
	DeviceClass      string    `gorm:"type:varchar(16);not null"` // interactive | hardware
	DeviceName       string    `gorm:"type:varchar(64);default:''"`
	RefreshTokenHash string    `gorm:"type:varchar(128);not null"`
	TokenVersion     int       `gorm:"not null;default:1"`
	IPAddress        string    `gorm:"type:varchar(45);default:''"`
	IsRevoked        int8      `gorm:"type:tinyint;not null;default:0"`
	ExpiresAt        time.Time `gorm:"not null"`
	LastActiveAt     time.Time `gorm:"not null"`
	CreatedAt        time.Time `gorm:"not null"`
}

// Friendship — friendships table, physical bidirectional double-row (2.3)
type Friendship struct {
	ID          uint           `gorm:"primaryKey;autoIncrement"`
	UserID      string         `gorm:"type:varchar(36);not null;uniqueIndex:uq_active_friendship,where:deleted_at IS NULL"`
	FriendID    string         `gorm:"type:varchar(36);not null;uniqueIndex:uq_active_friendship,where:deleted_at IS NULL"`
	InitiatorID string         `gorm:"type:varchar(36);not null"`
	Status      string         `gorm:"type:varchar(16);not null;default:'pending';index:idx_friendship_user_status,where:deleted_at IS NULL"`
	Remark      string         `gorm:"type:varchar(64);default:''"`
	CreatedAt   time.Time      `gorm:"not null"`
	UpdatedAt   time.Time      `gorm:"not null"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

// Conversation — conversations table, session watermark (2.4)
type Conversation struct {
	CovID            string    `gorm:"primaryKey;type:varchar(128)"` // cov:uid1:uid2 or cov:grp:groupId
	ChatType         string    `gorm:"type:varchar(16);not null"`   // chat | groupchat
	CurrentWatermark int64     `gorm:"not null;default:0"`          // persistent watermark, step 1000
	LastMsgID        int64     `gorm:"default:0"`
	LastMsgTime      int64     `gorm:"not null;default:0"`
	LastMsgPreview   string    `gorm:"type:varchar(128);default:''"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

// Message — messages table (2.5)
type Message struct {
	ID          uint      `gorm:"primaryKey;autoIncrement"`
	CovID       string    `gorm:"type:varchar(128);not null;uniqueIndex:uq_cov_seq,priority:1;index:idx_messages_cov_seq,priority:1"`
	Seq         int64     `gorm:"not null;uniqueIndex:uq_cov_seq,priority:2;index:idx_messages_cov_seq,priority:2"`
	StanzaID    string    `gorm:"type:varchar(64);not null;index:idx_messages_stanza"`
	ChatType    string    `gorm:"type:varchar(16);not null;default:'chat'"`
	FromUID     string    `gorm:"type:varchar(36);not null"`
	ToUID       string    `gorm:"type:varchar(36);not null"`
	ContentType int8      `gorm:"type:tinyint;not null;default:1"` // 1=text 2=media 3=system
	Content     string    `gorm:"type:text;not null"`
	MediaID     string    `gorm:"type:varchar(64);default:''"`
	Extra       string    `gorm:"type:text;default:''"`
	Status      int8      `gorm:"type:tinyint;not null;default:1"`
	Timestamp   int64     `gorm:"not null;index:idx_messages_time"`
	CreatedAt   time.Time `gorm:"not null"`
}

// MediaAsset — media_assets table (2.6)
type MediaAsset struct {
	MID         string    `gorm:"primaryKey;type:varchar(64)"`
	UploaderID  string    `gorm:"type:varchar(36);not null;index:idx_media_uploader,priority:1"`
	AccessLevel string    `gorm:"type:varchar(16);not null;default:'private'"` // public | private
	MediaType   string    `gorm:"type:varchar(16);not null"`                   // image | voice | video | avatar
	FileSize    int64     `gorm:"not null"`
	FileExt     string    `gorm:"type:varchar(16);not null"`
	FileSHA256  string    `gorm:"type:varchar(64);not null;index:idx_media_sha256"`
	StoragePath string    `gorm:"type:varchar(255);not null"`
	AccessURL   string    `gorm:"type:varchar(255);not null"`
	Duration    int       `gorm:"default:0"`
	Width       int       `gorm:"default:0"`
	Height      int       `gorm:"default:0"`
	CreatedAt   time.Time `gorm:"not null;index:idx_media_uploader,priority:2"`
}

// UserPost — user_posts table (2.7, stage 4)
type UserPost struct {
	ID           uint           `gorm:"primaryKey;autoIncrement"`
	UserID       string         `gorm:"type:varchar(36);not null;index:idx_posts_user,priority:1"`
	MediaType    string         `gorm:"type:varchar(16);not null"`
	Content      string         `gorm:"type:text"`
	MediaURL     string         `gorm:"type:varchar(512);default:''"`
	BilibiliBVID string         `gorm:"type:varchar(32);default:''"`
	BilibiliMeta    string         `gorm:"type:text;default:''"`
	LinkPreviewJSON string         `gorm:"column:link_preview_json;type:text;default:''"`
	CreatedAt    time.Time      `gorm:"not null;index:idx_posts_user,priority:2"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// Group — groups table (stage 4, M1)
type Group struct {
	ID           string         `gorm:"primaryKey;type:varchar(36)"`
	Name         string         `gorm:"type:varchar(64);not null"`
	AvatarURL    string         `gorm:"type:varchar(255);default:''"`
	Announcement string         `gorm:"type:varchar(512);default:''"`
	OwnerID      string         `gorm:"type:varchar(36);not null;index"`
	MaxMembers   int            `gorm:"not null;default:500"`
	Status       int8           `gorm:"type:tinyint;not null;default:1"` // 1=active, 0=dismissed
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// GroupMember — group_members table (stage 4, M1)
type GroupMember struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	GroupID   string         `gorm:"type:varchar(36);not null;uniqueIndex:uq_active_group_member,where:deleted_at IS NULL"`
	UserID    string         `gorm:"type:varchar(36);not null;uniqueIndex:uq_active_group_member,where:deleted_at IS NULL;index:idx_group_member_user"`
	Role      string         `gorm:"type:varchar(16);not null;default:'member'"` // owner|admin|member
	Muted     bool           `gorm:"not null;default:false"`
	MutedBy   string         `gorm:"type:varchar(36);default:''"`
	MutedAt   int64          `gorm:"default:0"` // ms; 0 = never
	JoinedVia string         `gorm:"type:varchar(16);not null;default:'invite'"` // invite|join|create
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}
