package service

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var localizer *i18n.Localizer
var crmI18n func(key string, params ...string) (string, error)

func (s *TelegramService) InitI18n() error {
	bundle := i18n.NewBundle(language.Persian)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	err := filepath.WalkDir("translation", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = bundle.ParseMessageFileBytes(data, path)
		return err
	})
	if err != nil {
		return err
	}

	localizer = i18n.NewLocalizer(bundle)
	return nil
}

func Tr(key string) string {
	value, _ := localizer.Localize(&i18n.LocalizeConfig{
		MessageID: key,
	})
	return value
}
