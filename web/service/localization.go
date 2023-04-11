package service

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"x-ui/logger"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var localizers map[string]*i18n.Localizer = make(map[string]*i18n.Localizer)
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

	for _, lang := range bundle.LanguageTags() {
		localizers[lang.String()] = i18n.NewLocalizer(bundle, lang.String())
	}
	return nil
}

func Tr(key string, lang string) string {
	localizer, ok := localizers[lang]
	if !ok {
		logger.Error("Unsupported language")
		return ""
	}
	value, _ := localizer.Localize(&i18n.LocalizeConfig{
		MessageID: key,
	})
	return value
}

func GetAvailableLangs() []string {
	langs := make([]string, 0)
	for lang := range localizers {
		langs = append(langs, lang)
	}
	return langs
}
