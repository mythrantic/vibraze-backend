package radio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"

	"github.com/valiantlynx/raga-backend/db"
)

// NowPlaying holds the currently playing track info. Thread-safe.
type NowPlaying struct {
	mu      sync.RWMutex
	track   *db.Track
	startAt time.Time
}

// Get returns the current track and when it started playing.
func (np *NowPlaying) Get() (*db.Track, time.Time) {
	np.mu.RLock()
	defer np.mu.RUnlock()
	return np.track, np.startAt
}

// Set updates the now playing track.
func (np *NowPlaying) Set(track *db.Track) {
	np.mu.Lock()
	defer np.mu.Unlock()
	np.track = track
	np.startAt = time.Now()
}

// NowPlayingResponse is the JSON response for now-playing endpoints.
type NowPlayingResponse struct {
	Track     *db.Track `json:"track"`
	StartedAt string    `json:"started_at"`
}

// SSEBroker manages Server-Sent Events connections for now-playing updates.
type SSEBroker struct {
	mu          sync.Mutex
	subscribers map[chan string]struct{}
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		subscribers: make(map[chan string]struct{}),
	}
}

// Subscribe adds a new subscriber and returns their channel.
func (b *SSEBroker) Subscribe() chan string {
	ch := make(chan string, 10)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends a message to all subscribers.
func (b *SSEBroker) Broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			// Drop message if subscriber is slow
		}
	}
}

// Radio holds all radio state and dependencies.
type Radio struct {
	DB         *db.DB
	NowPlaying *NowPlaying
	SSE        *SSEBroker
	IcecastURL string
	MusicDir   string
}

// New creates a new Radio instance.
func New(database *db.DB, icecastURL string, musicDir string) *Radio {
	return &Radio{
		DB:         database,
		NowPlaying: &NowPlaying{},
		SSE:        NewSSEBroker(),
		IcecastURL: icecastURL,
		MusicDir:   musicDir,
	}
}

// NextTrack picks the next track to play: first from the request queue, then random.
// Returns the track (or nil if library is empty).
func (r *Radio) NextTrack() (*db.Track, error) {
	// Try to pop from request queue first
	req, err := r.DB.PopNextRequest()
	if err != nil {
		return nil, fmt.Errorf("failed to pop request: %w", err)
	}
	if req != nil && req.Track != nil {
		log.Infof("Next track from request queue: %s - %s", req.Track.Artist, req.Track.Title)
		r.updateNowPlaying(req.Track)
		return req.Track, nil
	}

	// Fallback to random track
	track, err := r.DB.GetRandomTrack()
	if err != nil {
		return nil, fmt.Errorf("failed to get random track: %w", err)
	}
	if track != nil {
		log.Infof("Next track (random): %s - %s", track.Artist, track.Title)
		r.updateNowPlaying(track)
	}
	return track, nil
}

// updateNowPlaying sets the current track and broadcasts to SSE subscribers.
func (r *Radio) updateNowPlaying(track *db.Track) {
	r.NowPlaying.Set(track)

	resp := NowPlayingResponse{
		Track:     track,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		log.Errorf("Failed to marshal now playing: %v", err)
		return
	}
	r.SSE.Broadcast(string(data))
}

// HandleNextTrack is the Fiber handler for GET /api/radio/next-track.
// Returns plain text file path for Liquidsoap.
func (r *Radio) HandleNextTrack(c *fiber.Ctx) error {
	track, err := r.NextTrack()
	if err != nil {
		log.Errorf("Error getting next track: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("error: " + err.Error())
	}
	if track == nil {
		return c.Status(fiber.StatusNotFound).SendString("error: no tracks in library")
	}
	// Return the absolute file path that Liquidsoap can access
	return c.SendString(track.FilePath)
}

// HandleNowPlaying is the Fiber handler for GET /api/radio/now-playing (SSE).
func (r *Radio) HandleNowPlaying(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ch := r.SSE.Subscribe()
		defer r.SSE.Unsubscribe(ch)

		// Send current state immediately
		track, startAt := r.NowPlaying.Get()
		if track != nil {
			resp := NowPlayingResponse{
				Track:     track,
				StartedAt: startAt.UTC().Format(time.RFC3339),
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.Flush()
		}

		// Stream updates
		for msg := range ch {
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if err := w.Flush(); err != nil {
				return
			}
		}
	})

	return nil
}

// HandleRequest is the Fiber handler for POST /api/radio/request.
func (r *Radio) HandleRequest(c *fiber.Ctx) error {
	var body struct {
		TrackID int64 `json:"track_id"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}
	if body.TrackID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "track_id is required and must be positive",
		})
	}

	// Verify track exists
	track, err := r.DB.GetTrackByID(body.TrackID)
	if err != nil {
		log.Errorf("Error looking up track %d: %v", body.TrackID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "internal server error",
		})
	}
	if track == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "track not found",
		})
	}

	id, err := r.DB.AddRequest(body.TrackID)
	if err != nil {
		log.Errorf("Error adding request: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to add request",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":       id,
		"track_id": body.TrackID,
		"message":  fmt.Sprintf("Requested: %s - %s", track.Artist, track.Title),
	})
}

// HandleTracks is the Fiber handler for GET /api/radio/tracks.
func (r *Radio) HandleTracks(c *fiber.Ctx) error {
	tracks, err := r.DB.GetAllTracks()
	if err != nil {
		log.Errorf("Error getting tracks: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to get tracks",
		})
	}
	if tracks == nil {
		tracks = []db.Track{}
	}
	return c.JSON(fiber.Map{
		"tracks": tracks,
		"count":  len(tracks),
	})
}

// HandleQueue is the Fiber handler for GET /api/radio/queue.
func (r *Radio) HandleQueue(c *fiber.Ctx) error {
	requests, err := r.DB.GetPendingRequests()
	if err != nil {
		log.Errorf("Error getting queue: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to get queue",
		})
	}
	if requests == nil {
		requests = []db.Request{}
	}
	return c.JSON(fiber.Map{
		"queue": requests,
		"count": len(requests),
	})
}

// HandleScan is the Fiber handler for POST /api/radio/scan.
func (r *Radio) HandleScan(c *fiber.Ctx) error {
	added, err := r.DB.ScanMusicDir(r.MusicDir)
	if err != nil {
		log.Errorf("Error scanning music dir: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "scan completed with errors",
			"added":   added,
			"message": err.Error(),
		})
	}

	total, _ := r.DB.TrackCount()
	return c.JSON(fiber.Map{
		"added":   added,
		"total":   total,
		"message": fmt.Sprintf("Scan complete. %d new tracks added. %d total tracks in library.", added, total),
	})
}

// StartIcecastPoller starts a goroutine that polls Icecast status and updates NowPlaying.
// This runs in the background and logs errors without crashing.
func (r *Radio) StartIcecastPoller() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		client := &http.Client{Timeout: 3 * time.Second}

		for range ticker.C {
			r.pollIcecast(client)
		}
	}()
	log.Info("Icecast poller started (polling every 5s)")
}

// icecastStatus represents the relevant parts of Icecast's /status-json.xsl response.
type icecastStatus struct {
	Icestats struct {
		Source *icecastSource `json:"source"`
	} `json:"icestats"`
}

type icecastSource struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

func (r *Radio) pollIcecast(client *http.Client) {
	url := r.IcecastURL + "/status-json.xsl"
	resp, err := client.Get(url)
	if err != nil {
		// Icecast might not be running yet — this is expected during startup
		log.Debugf("Icecast poll failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debugf("Icecast returned status %d", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debugf("Failed to read Icecast response: %v", err)
		return
	}

	var status icecastStatus
	if err := json.Unmarshal(body, &status); err != nil {
		log.Debugf("Failed to parse Icecast status: %v", err)
		return
	}

	if status.Icestats.Source == nil {
		return
	}

	// Update now playing if Icecast reports different metadata.
	// This handles the case where Liquidsoap sends metadata to Icecast
	// that we haven't tracked internally (e.g., manual track insertion).
	currentTrack, _ := r.NowPlaying.Get()
	source := status.Icestats.Source

	if currentTrack == nil ||
		(source.Title != "" && source.Title != currentTrack.Title) ||
		(source.Artist != "" && source.Artist != currentTrack.Artist) {
		// Only update if Icecast has metadata we don't know about
		if source.Title != "" || source.Artist != "" {
			log.Debugf("Icecast metadata update: %s - %s", source.Artist, source.Title)
			// We don't have a full track object from Icecast, so create a partial one
			r.updateNowPlaying(&db.Track{
				Title:  source.Title,
				Artist: source.Artist,
			})
		}
	}
}

// SetupRoutes registers all radio routes on the given Fiber app.
// Applies CORS middleware to all /api/radio/* routes.
func (r *Radio) SetupRoutes(app *fiber.App) {
	api := app.Group("/api/radio", corsMiddleware)

	api.Get("/next-track", r.HandleNextTrack)
	api.Get("/now-playing", r.HandleNowPlaying)
	api.Post("/request", r.HandleRequest)
	api.Get("/tracks", r.HandleTracks)
	api.Get("/queue", r.HandleQueue)
	api.Post("/scan", r.HandleScan)

	// Radio station directory (radio-browser.info proxy + favorites)
	// Static routes MUST come before parameterized routes
	api.Get("/stations/search", r.HandleStationsSearch)
	api.Get("/stations/top", r.HandleTopStations)
	api.Get("/stations/countries", r.HandleCountries)
	api.Get("/stations/tags", r.HandleTags)
	api.Get("/stations/favorites", r.HandleFavorites)
	api.Post("/stations/favorite", r.HandleStationFavorite)
	api.Delete("/stations/favorite", r.HandleStationFavorite)
	// Parameterized routes
	api.Get("/tag-stations/:tag", r.HandleStationsByTag)
}

// corsMiddleware adds CORS headers to radio API responses.
func corsMiddleware(c *fiber.Ctx) error {
	c.Set("Access-Control-Allow-Origin", "*")
	c.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	c.Set("Access-Control-Allow-Headers", "Content-Type, Accept")

	if c.Method() == "OPTIONS" {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Next()
}


