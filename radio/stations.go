package radio

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"

	"github.com/valiantlynx/raga-backend/db"
)

const radioBrowserBaseURL = "https://de1.api.radio-browser.info/json"

// stationsClient is a shared HTTP client for radio-browser.info API calls.
var stationsClient = &http.Client{
	Timeout: 10 * time.Second,
}

// HandleStationStream proxies a station stream so the frontend can consume it
// with a stable CORS policy and use the Web Audio API for visualization.
// GET /api/radio/stations/stream?url=https://...
func (r *Radio) HandleStationStream(c *fiber.Ctx) error {
	streamURL := c.Query("url")
	if streamURL == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "url query parameter is required",
		})
	}

	parsed, err := url.Parse(streamURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "url must be a valid http or https URL",
		})
	}

	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		log.Errorf("Failed to create stream proxy request: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to build stream request",
		})
	}
	req.Header.Set("User-Agent", "vibraze-radio/1.0")

	resp, err := stationsClient.Do(req)
	if err != nil {
		log.Errorf("Failed to reach station stream: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "failed to reach station stream",
		})
	}
	defer resp.Body.Close()

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Set("Content-Type", contentType)
	}
	if icyName := resp.Header.Get("Icy-Name"); icyName != "" {
		c.Set("Icy-Name", icyName)
	}
	if icyGenre := resp.Header.Get("Icy-Genre"); icyGenre != "" {
		c.Set("Icy-Genre", icyGenre)
	}
	c.Set("Cache-Control", "no-store")
	c.Set("X-Accel-Buffering", "no")
	c.Status(resp.StatusCode)

	_, err = io.Copy(c.Response().BodyWriter(), resp.Body)
	if err != nil {
		log.Debugf("Stream proxy closed: %v", err)
	}

	return nil
}

// radioBrowserGet performs a GET request to the radio-browser.info API and
// streams the response body directly into the Fiber response.
func radioBrowserGet(c *fiber.Ctx, path string) error {
	url := radioBrowserBaseURL + path

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Errorf("Failed to create request to radio-browser.info: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to build upstream request",
		})
	}
	req.Header.Set("User-Agent", "vibraze-radio/1.0")

	resp, err := stationsClient.Do(req)
	if err != nil {
		log.Errorf("Failed to reach radio-browser.info: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "failed to reach radio-browser.info",
		})
	}
	defer resp.Body.Close()

	c.Set("Content-Type", "application/json")
	c.Status(resp.StatusCode)

	_, err = io.Copy(c.Response().BodyWriter(), resp.Body)
	if err != nil {
		log.Errorf("Failed to proxy radio-browser.info response: %v", err)
	}
	return nil
}

// HandleStationsSearch searches stations on radio-browser.info.
// GET /api/radio/stations/search?name=...&country=...&countrycode=...&tag=...&limit=20&offset=0&order=votes&reverse=true
func (r *Radio) HandleStationsSearch(c *fiber.Ctx) error {
	limit := c.Query("limit", "20")
	offset := c.Query("offset", "0")
	order := c.Query("order", "votes")
	reverse := c.Query("reverse", "true")

	path := fmt.Sprintf("/stations/search?limit=%s&offset=%s&order=%s&reverse=%s",
		limit, offset, order, reverse)

	if name := c.Query("name"); name != "" {
		path += "&name=" + name
	}
	if country := c.Query("country"); country != "" {
		path += "&country=" + country
	}
	if countrycode := c.Query("countrycode"); countrycode != "" {
		path += "&countrycode=" + countrycode
	}
	if tag := c.Query("tag"); tag != "" {
		path += "&tag=" + tag
	}

	return radioBrowserGet(c, path)
}

// HandleStationsByCountry lists stations by country code.
// GET /api/radio/stations/country/:countrycode?limit=50&offset=0&order=votes&reverse=true
func (r *Radio) HandleStationsByCountry(c *fiber.Ctx) error {
	countrycode := c.Params("countrycode")
	if countrycode == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "countrycode is required",
		})
	}

	limit := c.Query("limit", "50")
	offset := c.Query("offset", "0")
	order := c.Query("order", "votes")
	reverse := c.Query("reverse", "true")

	path := fmt.Sprintf("/stations/bycountrycode/%s?limit=%s&offset=%s&order=%s&reverse=%s",
		countrycode, limit, offset, order, reverse)

	return radioBrowserGet(c, path)
}

// HandleTopStations returns top voted stations worldwide.
// GET /api/radio/stations/top?limit=50&offset=0
func (r *Radio) HandleTopStations(c *fiber.Ctx) error {
	limit := c.Query("limit", "50")
	offset := c.Query("offset", "0")

	path := fmt.Sprintf("/stations?order=votes&reverse=true&limit=%s&offset=%s", limit, offset)

	return radioBrowserGet(c, path)
}

// HandleStationsByTag returns stations filtered by tag/genre.
// GET /api/radio/stations/tag/:tag?limit=50&offset=0
func (r *Radio) HandleStationsByTag(c *fiber.Ctx) error {
	tag := c.Params("tag")
	if tag == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "tag is required",
		})
	}

	limit := c.Query("limit", "50")
	offset := c.Query("offset", "0")

	path := fmt.Sprintf("/stations/bytag/%s?order=votes&reverse=true&limit=%s&offset=%s",
		tag, limit, offset)

	return radioBrowserGet(c, path)
}

// HandleCountries lists all countries with station counts.
// GET /api/radio/stations/countries
func (r *Radio) HandleCountries(c *fiber.Ctx) error {
	return radioBrowserGet(c, "/countries?order=stationcount&reverse=true")
}

// HandleTags lists popular tags/genres.
// GET /api/radio/stations/tags?limit=50
func (r *Radio) HandleTags(c *fiber.Ctx) error {
	limit := c.Query("limit", "50")

	path := fmt.Sprintf("/tags?order=stationcount&reverse=true&limit=%s&hidefaulty=true", limit)

	return radioBrowserGet(c, path)
}

// HandleStationFavorite saves or removes a favorite station.
// POST /api/radio/stations/favorite — saves a station
// DELETE /api/radio/stations/favorite?station_uuid=... — removes a station
func (r *Radio) HandleStationFavorite(c *fiber.Ctx) error {
	switch c.Method() {
	case "POST":
		var body struct {
			StationUUID string `json:"station_uuid"`
			Name        string `json:"name"`
			URL         string `json:"url"`
			Favicon     string `json:"favicon"`
			Country     string `json:"country"`
			Tags        string `json:"tags"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}
		if body.StationUUID == "" || body.Name == "" || body.URL == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "station_uuid, name, and url are required",
			})
		}

		fav := db.Favorite{
			StationUUID: body.StationUUID,
			Name:        body.Name,
			URL:         body.URL,
			Favicon:     body.Favicon,
			Country:     body.Country,
			Tags:        body.Tags,
		}
		if err := r.DB.AddFavorite(fav); err != nil {
			log.Errorf("Failed to add favorite: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to save favorite",
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message":      "station added to favorites",
			"station_uuid": body.StationUUID,
		})

	case "DELETE":
		stationUUID := c.Query("station_uuid")
		if stationUUID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "station_uuid query parameter is required",
			})
		}

		if err := r.DB.RemoveFavorite(stationUUID); err != nil {
			log.Errorf("Failed to remove favorite: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to remove favorite",
			})
		}

		return c.JSON(fiber.Map{
			"message":      "station removed from favorites",
			"station_uuid": stationUUID,
		})

	default:
		return c.Status(fiber.StatusMethodNotAllowed).JSON(fiber.Map{
			"error": "method not allowed",
		})
	}
}

// HandleFavorites lists all favorite stations from SQLite.
// GET /api/radio/stations/favorites
func (r *Radio) HandleFavorites(c *fiber.Ctx) error {
	favorites, err := r.DB.GetFavorites()
	if err != nil {
		log.Errorf("Failed to get favorites: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to get favorites",
		})
	}
	if favorites == nil {
		favorites = []db.Favorite{}
	}
	return c.JSON(fiber.Map{
		"favorites": favorites,
		"count":     len(favorites),
	})
}
