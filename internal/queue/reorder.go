package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func SwapSongsByIDs(guildID string, firstID, secondID int) error {
	release := guild.AcquireLock(guildID)
	defer release()

	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	pos1, err := songPosition(tx, guildID, firstID)
	if err != nil {
		return err
	}

	pos2, err := songPosition(tx, guildID, secondID)
	if err != nil {
		return err
	}

	if pos1 == pos2 {
		return nil
	}

	if err := requireNotCurrent(tx, guildID, pos1, pos2); err != nil {
		return err
	}

	tempPos := -1

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, firstID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to temp: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos1, secondID)
	if err != nil {
		return fmt.Errorf("failed to move song2 to pos1: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos2, firstID)
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

	if err = shiftSongs(tx, guildID, fromPos, toPos); err != nil {
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
	if q == nil {
		return fmt.Errorf("invalid positions")
	}

	for _, position := range positions {
		if position < 0 || position >= len(q.Songs) {
			return fmt.Errorf("invalid positions")
		}
	}
	return nil
}

func songPosition(tx *sql.Tx, guildID string, songID int) (int, error) {
	var position int
	err := tx.QueryRow(
		`SELECT queue_position FROM songs WHERE guild_id = ? AND id = ?`,
		guildID, songID,
	).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSongNotInQueue
	}
	return position, err
}

func requireNotCurrent(tx *sql.Tx, guildID string, positions ...int) error {
	if !slices.Contains(positions, 0) {
		return nil
	}
	isPlaying, err := isQueuePlaying(tx, guildID)
	if err != nil {
		return err
	}
	if isPlaying {
		return ErrSongIsCurrent
	}
	return nil
}

func isQueuePlaying(tx *sql.Tx, guildID string) (bool, error) {
	var isPlaying bool
	err := tx.QueryRow(
		`SELECT COALESCE(playing, 0) OR COALESCE(loading, 0) FROM queues WHERE guild_id = ?`,
		guildID,
	).Scan(&isPlaying)
	return isPlaying, err
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
