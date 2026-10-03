package localesync

import (
	"database/sql"
	"errors"

	"noraegaori/internal/database"
)

type DatabaseStore struct{}

func (DatabaseStore) Load(lang string) ([]byte, bool, error) {
	var content string
	err := database.DB.QueryRow("SELECT content FROM locale_baselines WHERE lang = ?", lang).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(content), true, nil
}

func (DatabaseStore) Save(lang string, content []byte) error {
	_, err := database.DB.Exec(
		"INSERT INTO locale_baselines (lang, content) VALUES (?, ?) ON CONFLICT(lang) DO UPDATE SET content = excluded.content",
		lang, string(content),
	)
	return err
}
