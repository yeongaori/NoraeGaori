package queue

import (
	"database/sql"
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func SwapSongs(guildID string, pos1, pos2 int) error {
	release := guild.AcquireLock(guildID)
	defer release()

	if err := checkPositions(guildID, pos1, pos2); err != nil {
		return err
	}

	if pos1 == pos2 {
		return nil
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	song1ID, err := songIDAt(tx, guildID, pos1)
	if err != nil {
		return err
	}

	song2ID, err := songIDAt(tx, guildID, pos2)
	if err != nil {
		return err
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

	if err := checkPositions(guildID, fromPos, toPos); err != nil {
		return err
	}

	if fromPos == toPos {
		return nil
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	songID, err := songIDAt(tx, guildID, fromPos)
	if err != nil {
		return err
	}

	tempPos := -1
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to temp: %w", err)
	}

	if err := shiftSongs(tx, guildID, fromPos, toPos); err != nil {
		return err
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

func shiftSongs(tx *sql.Tx, guildID string, fromPos, toPos int) error {
	var err error
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
	return nil
}

func checkPositions(guildID string, positions ...int) error {
	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	for _, position := range positions {
		if position < 0 || position >= len(q.Songs) {
			return fmt.Errorf("invalid positions")
		}
	}
	return nil
}

func songIDAt(tx *sql.Tx, guildID string, position int) (int, error) {
	var songID int
	err := tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, position,
	).Scan(&songID)
	if err != nil {
		return 0, fmt.Errorf("failed to get song at position %d: %w", position, err)
	}
	return songID, nil
}
