package queue

import (
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func SwapSongs(guildID string, pos1, pos2 int) error {
	release := guild.AcquireLock(guildID)
	defer release()

	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if pos1 < 0 || pos2 < 0 || pos1 >= len(q.Songs) || pos2 >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if pos1 == pos2 {
		return nil
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var song1ID, song2ID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, pos1,
	).Scan(&song1ID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", pos1, err)
	}

	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, pos2,
	).Scan(&song2ID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", pos2, err)
	}

	tempPos := -1

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to temp: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos1, song2ID)
	if err != nil {
		return fmt.Errorf("failed to move song2 to pos1: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos2, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to pos2: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Swapped songs at positions %d and %d for guild: %s", pos1, pos2, guildID)
	return nil
}

func MoveSong(guildID string, fromPos, toPos int) error {
	release := guild.AcquireLock(guildID)
	defer release()

	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if fromPos < 0 || toPos < 0 || fromPos >= len(q.Songs) || toPos >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if fromPos == toPos {
		return nil
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, fromPos,
	).Scan(&songID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", fromPos, err)
	}

	tempPos := -1
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to temp: %w", err)
	}

	if fromPos < toPos {

		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position - 1
			 WHERE guild_id = ? AND queue_position > ? AND queue_position <= ?`,
			guildID, fromPos, toPos)
	} else {

		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position + 1
			 WHERE guild_id = ? AND queue_position >= ? AND queue_position < ?`,
			guildID, toPos, fromPos)
	}
	if err != nil {
		return fmt.Errorf("failed to shift songs: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		toPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to final position: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Moved song from position %d to %d for guild: %s", fromPos, toPos, guildID)
	return nil
}
