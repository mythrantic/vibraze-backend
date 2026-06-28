package db

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	log "github.com/sirupsen/logrus"
)

// Track represents a music track in the library.
type Track struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"`
	FilePath string `json:"file_path"`
	Genre    string `json:"genre"`
	AddedAt  string `json:"added_at"`
}

// Request represents a song request from a listener.
type Request struct {
	ID          int64  `json:"id"`
	TrackID     int64  `json:"track_id"`
	RequestedAt string `json:"requested_at"`
	Played      bool   `json:"played"`
	Track       *Track `json:"track,omitempty"`
}

// DB wraps the SQL database connection.
type DB struct {
	conn *sql.DB
}

// InitDB opens (or creates) the SQLite database at dbPath and ensures the schema exists.
func InitDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if err := createSchema(conn); err != nil {
		conn.Close()
		return nil, err
	}

	log.Info("Database initialized at ", dbPath)
	return &DB{conn: conn}, nil
}

func createSchema(conn *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL DEFAULT '',
		artist TEXT NOT NULL DEFAULT '',
		album TEXT NOT NULL DEFAULT '',
		duration INTEGER NOT NULL DEFAULT 0,
		file_path TEXT NOT NULL UNIQUE,
		genre TEXT NOT NULL DEFAULT '',
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		track_id INTEGER NOT NULL,
		requested_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		played BOOLEAN DEFAULT 0,
		FOREIGN KEY (track_id) REFERENCES tracks(id)
	);

	CREATE INDEX IF NOT EXISTS idx_requests_pending ON requests(played, requested_at);
	CREATE INDEX IF NOT EXISTS idx_tracks_file_path ON tracks(file_path);

	CREATE TABLE IF NOT EXISTS favorites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		station_uuid TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		favicon TEXT DEFAULT '',
		country TEXT DEFAULT '',
		tags TEXT DEFAULT '',
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_favorites_station_uuid ON favorites(station_uuid);
	`
	_, err := conn.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// AddTrack inserts a track into the database. Skips duplicates by file_path.
func (d *DB) AddTrack(track Track) (int64, error) {
	result, err := d.conn.Exec(
		`INSERT OR IGNORE INTO tracks (title, artist, album, duration, file_path, genre) VALUES (?, ?, ?, ?, ?, ?)`,
		track.Title, track.Artist, track.Album, track.Duration, track.FilePath, track.Genre,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert track: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// GetAllTracks returns all tracks in the library.
func (d *DB) GetAllTracks() ([]Track, error) {
	rows, err := d.conn.Query(
		`SELECT id, title, artist, album, duration, file_path, genre, added_at FROM tracks ORDER BY artist, title`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query tracks: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath, &t.Genre, &t.AddedAt); err != nil {
			return nil, fmt.Errorf("failed to scan track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

// GetTrackByID returns a single track by its ID.
func (d *DB) GetTrackByID(id int64) (*Track, error) {
	var t Track
	err := d.conn.QueryRow(
		`SELECT id, title, artist, album, duration, file_path, genre, added_at FROM tracks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath, &t.Genre, &t.AddedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get track: %w", err)
	}
	return &t, nil
}

// GetRandomTrack returns a random track from the library.
func (d *DB) GetRandomTrack() (*Track, error) {
	var count int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count tracks: %w", err)
	}
	if count == 0 {
		return nil, nil
	}

	offset := rand.Intn(count)
	var t Track
	err := d.conn.QueryRow(
		`SELECT id, title, artist, album, duration, file_path, genre, added_at FROM tracks LIMIT 1 OFFSET ?`, offset,
	).Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath, &t.Genre, &t.AddedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get random track: %w", err)
	}
	return &t, nil
}

// TrackCount returns the number of tracks in the library.
func (d *DB) TrackCount() (int, error) {
	var count int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&count)
	return count, err
}

// AddRequest inserts a new song request into the queue.
func (d *DB) AddRequest(trackID int64) (int64, error) {
	result, err := d.conn.Exec(
		`INSERT INTO requests (track_id) VALUES (?)`, trackID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert request: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// GetPendingRequests returns all unplayed requests, oldest first, with track info.
func (d *DB) GetPendingRequests() ([]Request, error) {
	rows, err := d.conn.Query(`
		SELECT r.id, r.track_id, r.requested_at, r.played,
		       t.id, t.title, t.artist, t.album, t.duration, t.file_path, t.genre, t.added_at
		FROM requests r
		JOIN tracks t ON r.track_id = t.id
		WHERE r.played = 0
		ORDER BY r.requested_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending requests: %w", err)
	}
	defer rows.Close()

	var requests []Request
	for rows.Next() {
		var r Request
		var t Track
		if err := rows.Scan(&r.ID, &r.TrackID, &r.RequestedAt, &r.Played,
			&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath, &t.Genre, &t.AddedAt); err != nil {
			return nil, fmt.Errorf("failed to scan request: %w", err)
		}
		r.Track = &t
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

// PopNextRequest returns the oldest unplayed request and marks it as played.
// Returns nil if no pending requests exist.
func (d *DB) PopNextRequest() (*Request, error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var r Request
	var t Track
	err = tx.QueryRow(`
		SELECT r.id, r.track_id, r.requested_at, r.played,
		       t.id, t.title, t.artist, t.album, t.duration, t.file_path, t.genre, t.added_at
		FROM requests r
		JOIN tracks t ON r.track_id = t.id
		WHERE r.played = 0
		ORDER BY r.requested_at ASC
		LIMIT 1
	`).Scan(&r.ID, &r.TrackID, &r.RequestedAt, &r.Played,
		&t.ID, &t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath, &t.Genre, &t.AddedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get next request: %w", err)
	}
	r.Track = &t

	if _, err := tx.Exec(`UPDATE requests SET played = 1 WHERE id = ?`, r.ID); err != nil {
		return nil, fmt.Errorf("failed to mark request as played: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return &r, nil
}

// ScanMusicDir walks a directory and adds all supported audio files to the database.
// Supported extensions: .mp3, .flac, .ogg, .wav
// Metadata is parsed from filename using "Artist - Title.ext" pattern.
// Duplicates (by file_path) are skipped.
func (d *DB) ScanMusicDir(dir string) (int, error) {
	supportedExts := map[string]bool{
		".mp3":  true,
		".flac": true,
		".ogg":  true,
		".wav":  true,
	}

	var added int
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Warnf("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}

		track := parseFilename(path, dir)
		id, err := d.AddTrack(track)
		if err != nil {
			log.Warnf("Failed to add track %s: %v", path, err)
			return nil
		}
		if id > 0 {
			added++
		}
		return nil
	})

	if err != nil {
		return added, fmt.Errorf("failed to walk directory %s: %w", dir, err)
	}

	log.Infof("Music scan complete: %d new tracks added from %s", added, dir)
	return added, nil
}

// parseFilename extracts metadata from a filename.
// Expected pattern: "Artist - Title.ext" or just "Title.ext".
// The file_path stored is the absolute path.
func parseFilename(path string, baseDir string) Track {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	name = strings.TrimSuffix(name, ext)

	var artist, title string
	if parts := strings.SplitN(name, " - ", 2); len(parts) == 2 {
		artist = strings.TrimSpace(parts[0])
		title = strings.TrimSpace(parts[1])
	} else {
		title = strings.TrimSpace(name)
	}

	return Track{
		Title:    title,
		Artist:   artist,
		FilePath: path,
	}
}

// Favorite represents a saved radio station from radio-browser.info.
type Favorite struct {
	ID          int64  `json:"id"`
	StationUUID string `json:"station_uuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Favicon     string `json:"favicon"`
	Country     string `json:"country"`
	Tags        string `json:"tags"`
	AddedAt     string `json:"added_at"`
}

// AddFavorite inserts a station into the favorites table.
func (d *DB) AddFavorite(fav Favorite) error {
	_, err := d.conn.Exec(
		`INSERT OR REPLACE INTO favorites (station_uuid, name, url, favicon, country, tags) VALUES (?, ?, ?, ?, ?, ?)`,
		fav.StationUUID, fav.Name, fav.URL, fav.Favicon, fav.Country, fav.Tags,
	)
	if err != nil {
		return fmt.Errorf("failed to insert favorite: %w", err)
	}
	return nil
}

// RemoveFavorite deletes a station from the favorites table by station_uuid.
func (d *DB) RemoveFavorite(stationUUID string) error {
	_, err := d.conn.Exec(`DELETE FROM favorites WHERE station_uuid = ?`, stationUUID)
	if err != nil {
		return fmt.Errorf("failed to remove favorite: %w", err)
	}
	return nil
}

// GetFavorites returns all favorite stations.
func (d *DB) GetFavorites() ([]Favorite, error) {
	rows, err := d.conn.Query(
		`SELECT id, station_uuid, name, url, favicon, country, tags, added_at FROM favorites ORDER BY added_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query favorites: %w", err)
	}
	defer rows.Close()

	var favorites []Favorite
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.ID, &f.StationUUID, &f.Name, &f.URL, &f.Favicon, &f.Country, &f.Tags, &f.AddedAt); err != nil {
			return nil, fmt.Errorf("failed to scan favorite: %w", err)
		}
		favorites = append(favorites, f)
	}
	return favorites, rows.Err()
}

// IsFavorite checks if a station is in favorites.
func (d *DB) IsFavorite(stationUUID string) (bool, error) {
	var count int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM favorites WHERE station_uuid = ?`, stationUUID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check favorite: %w", err)
	}
	return count > 0, nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
