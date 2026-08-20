package queue

import (
	"database/sql"
	"fmt"
	"strings"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func CreateQueue(guildID, textChannelID, voiceChannelID string) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old queue: %w", err)
	}

	_, err = database.DB.Exec(
		`INSERT INTO queues (guild_id, text_channel_id, voice_channel_id) VALUES (?, ?, ?)`,
		guildID, textChannelID, voiceChannelID,
	)
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Queue created for guild: %s", guildID)
	return nil
}

func DeleteQueue(guildID string) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Queue deleted for guild: %s", guildID)
	return nil
}

func DeleteGuildData(guildID string) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM guild_settings WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete guild settings: %w", err)
	}

	InvalidateCache(guildID)
	guild.InvalidateCaches(guildID)
	logger.Infof("All data deleted for guild: %s", guildID)
	return nil
}

func AddSongsBatch(guildID string, songs []*Song, position int) error {
	if len(songs) == 0 {
		return nil
	}

	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	if position == -1 || position > count {
		position = count
	}

	if position < count {
		_, err = database.DB.Exec(
			`UPDATE songs SET queue_position = queue_position + ?
			 WHERE guild_id = ? AND queue_position >= ?`,
			len(songs), guildID, position,
		)
		if err != nil {
			return fmt.Errorf("failed to update positions: %w", err)
		}
	}

	const maxSongsPerBatch = 66

	for batchStart := 0; batchStart < len(songs); batchStart += maxSongsPerBatch {
		batchEnd := batchStart + maxSongsPerBatch
		if batchEnd > len(songs) {
			batchEnd = len(songs)
		}
		batch := songs[batchStart:batchEnd]

		query := `INSERT INTO songs (guild_id, url, title, duration, thumbnail, requested_by_id,
			requested_by_tag, queue_position, uploader, is_live,
			automix_style_volume, automix_style_eq, automix_style_filter,
			automix_style_effect, automix_style_loop) VALUES `

		values := []interface{}{}
		for i, song := range batch {
			if i > 0 {
				query += ", "
			}
			query += "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

			isLiveInt := 0
			if song.IsLive {
				isLiveInt = 1
			}

			values = append(values,
				guildID, song.URL, song.Title, song.Duration, song.Thumbnail,
				song.RequestedByID, song.RequestedByTag, position+batchStart+i, song.Uploader, isLiveInt,
				defaultAutoMixStyle(song.AutoMixStyleVolume), defaultAutoMixStyle(song.AutoMixStyleEQ),
				defaultAutoMixStyle(song.AutoMixStyleFilter), defaultAutoMixStyle(song.AutoMixStyleEffect),
				defaultAutoMixStyle(song.AutoMixStyleLoop),
			)
		}

		_, err = database.DB.Exec(query, values...)
		if err != nil {
			return fmt.Errorf("failed to batch insert songs (batch %d): %w", batchStart/maxSongsPerBatch+1, err)
		}
	}

	InvalidateCache(guildID)
	logger.Debugf("Added %d songs starting at position %d for guild: %s", len(songs), position, guildID)
	return nil
}

func AddSong(guildID string, song *Song, position int) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	var existingID int
	err = database.DB.QueryRow(`SELECT id FROM songs WHERE guild_id = ? AND url = ? LIMIT 1`, guildID, song.URL).Scan(&existingID)
	if err == nil {

		return fmt.Errorf("song already in queue: %s", song.Title)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check for duplicate: %w", err)
	}

	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	if position == -1 || position > count {
		position = count
	}

	if position < count {
		_, err = database.DB.Exec(
			`UPDATE songs SET queue_position = queue_position + 1
			 WHERE guild_id = ? AND queue_position >= ?`,
			guildID, position,
		)
		if err != nil {
			return fmt.Errorf("failed to update positions: %w", err)
		}
	}

	isLiveInt := 0
	if song.IsLive {
		isLiveInt = 1
	}

	_, err = database.DB.Exec(
		`INSERT INTO songs (guild_id, url, title, duration, thumbnail, requested_by_id,
		 requested_by_tag, queue_position, uploader, is_live,
		 automix_style_volume, automix_style_eq, automix_style_filter,
		 automix_style_effect, automix_style_loop)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		guildID, song.URL, song.Title, song.Duration, song.Thumbnail,
		song.RequestedByID, song.RequestedByTag, position, song.Uploader, isLiveInt,
		defaultAutoMixStyle(song.AutoMixStyleVolume), defaultAutoMixStyle(song.AutoMixStyleEQ),
		defaultAutoMixStyle(song.AutoMixStyleFilter), defaultAutoMixStyle(song.AutoMixStyleEffect),
		defaultAutoMixStyle(song.AutoMixStyleLoop),
	)
	if err != nil {
		return fmt.Errorf("failed to insert song: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Added song '%s' at position %d for guild: %s", song.Title, position, guildID)
	return nil
}

func UpdateSongSeekTime(guildID string, songID int, seekTime int) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE songs SET seek_time = ? WHERE guild_id = ? AND id = ?`,
		seekTime, guildID, songID,
	)
	if err != nil {
		return fmt.Errorf("failed to update seek time: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Updated seek time to %dms for song %d in guild: %s", seekTime, songID, guildID)
	return nil
}

func RemoveFirstSong(guildID string) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC LIMIT 1`,
		guildID,
	).Scan(&songID)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get first song: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1 WHERE guild_id = ?`,
		guildID,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Removed first song for guild: %s", guildID)
	return nil
}

func RemoveSong(guildID string, position int) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, position,
	).Scan(&songID)

	if err != nil {
		return fmt.Errorf("failed to find song at position %d: %w", position, err)
	}

	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1
		 WHERE guild_id = ? AND queue_position > ?`,
		guildID, position,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Removed song at position %d for guild: %s", position, guildID)
	return nil
}

func SkipToPosition(guildID string, targetIndex int) error {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position < ? ORDER BY queue_position ASC`,
		guildID, targetIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to query songs: %w", err)
	}
	defer rows.Close()

	var songIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		songIDs = append(songIDs, id)
	}
	rows.Close()

	if len(songIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, len(songIDs))
	for i, id := range songIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM songs WHERE id IN (%s)`, strings.Join(placeholders, ","))
	_, err = tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Skipped to position %d for guild: %s", targetIndex, guildID)
	return nil
}

func RemoveSongsByIDs(guildID string, songIDs []int) error {
	if len(songIDs) == 0 {
		return nil
	}

	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, len(songIDs)+1)
	args[0] = guildID

	for i, id := range songIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(`DELETE FROM songs WHERE guild_id = ? AND id IN (%s)`,
		strings.Join(placeholders, ","))

	result, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Removed %d songs for guild: %s", rowsAffected, guildID)
	return nil
}

func reorderSongsAfterRemovalTx(tx *sql.Tx, guildID string) error {

	rows, err := tx.Query(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC`,
		guildID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var songIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		songIDs = append(songIDs, id)
	}
	rows.Close()

	for i, id := range songIDs {
		_, err := tx.Exec(
			`UPDATE songs SET queue_position = ? WHERE id = ?`,
			i, id,
		)
		if err != nil {
			return err
		}
	}

	return nil
}
