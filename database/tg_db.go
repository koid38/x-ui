package database

import (
	"io/fs"
	"os"
	"path"
	"x-ui/config"
	"x-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var tgDb *gorm.DB

func initTgClient() error {
	return tgDb.AutoMigrate(&model.TgClient{}, &model.TgClientMsg{})
}

func InitTgDB(tgDbPath string) error {
	dir := path.Dir(tgDbPath)
	err := os.MkdirAll(dir, fs.ModeDir)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	tgDb, err = gorm.Open(sqlite.Open(tgDbPath), c)
	if err != nil {
		return err
	}

	err = initTgClient()
	if err != nil {
		return err
	}

	return nil
}

func GetTgDB() *gorm.DB {
	return tgDb
}
