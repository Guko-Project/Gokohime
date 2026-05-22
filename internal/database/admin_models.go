package database

import "gorm.io/gorm"

// AdminUser stores Web Admin login accounts.
type AdminUser struct {
	gorm.Model
	Username     string `gorm:"uniqueIndex;size:128;not null"`
	PasswordHash string `gorm:"size:255;not null"`
	DisplayName  string `gorm:"size:128"`
	Role         string `gorm:"size:32;default:admin"`
	Disabled     bool   `gorm:"default:false"`
}

// MigrateAdminModels migrates tables owned by the Web Admin service.
func MigrateAdminModels(db *gorm.DB) error {
	return db.AutoMigrate(&AdminUser{})
}
