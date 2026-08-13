package store

import "gorm.io/gorm"

func init() {
	RegisterGORMMigration(func(db *gorm.DB) error {
		if !db.Migrator().HasTable("agents") {
			return nil
		}
		for _, statement := range []string{
			`ALTER TABLE agents ADD COLUMN current_token_hash TEXT`,
			`CREATE UNIQUE INDEX idx_agents_current_token_hash ON agents(current_token_hash) WHERE current_token_hash IS NOT NULL`,
		} {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
