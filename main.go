package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"

	"github.com/mythrantic/vibraze-backend/db"
	"github.com/mythrantic/vibraze-backend/radio"
	"github.com/mythrantic/vibraze-backend/utils"
)

func main() {
	app := fiber.New(fiber.Config{
		AppName:                 "raga-backend",
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0"},
		ProxyHeader:             fiber.HeaderXForwardedFor,
	})

	// --- Existing CDN proxy routes (unchanged) ---
	app.Get("/media/+", func(c *fiber.Ctx) error {
		utils.ProxyRequest(c, "https://c.saavncdn.com/"+c.Params("+"))
		return nil
	})
	app.Get("/aac/:id/:path", func(c *fiber.Ctx) error {
		utils.ProxyRequest(c, "https://aac.saavncdn.com/"+c.Params("id")+"/"+c.Params("path"))
		return nil
	})
	app.Get("/svg/+", func(c *fiber.Ctx) error {
		utils.ProxyRequest(c, "https://www.jiosaavn.com/"+c.Params("+"))
		return nil
	})

	// --- Radio setup ---
	musicDir := getEnv("RAGA_MUSIC_DIR", "/music")
	dbPath := getEnv("RAGA_DB_PATH", "./radio.db")
	icecastURL := getEnv("RAGA_ICECAST_URL", "http://icecast:8000")

	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Auto-scan music directory on startup if library is empty
	count, err := database.TrackCount()
	if err != nil {
		log.Warnf("Failed to check track count: %v", err)
	} else if count == 0 {
		log.Info("Library is empty, scanning music directory: ", musicDir)
		if _, err := os.Stat(musicDir); err == nil {
			added, scanErr := database.ScanMusicDir(musicDir)
			if scanErr != nil {
				log.Warnf("Music scan had errors: %v", scanErr)
			}
			log.Infof("Initial scan added %d tracks", added)
		} else {
			log.Warnf("Music directory %s not accessible: %v", musicDir, err)
		}
	} else {
		log.Infof("Library has %d tracks, skipping startup scan", count)
	}

	// Initialize radio and register routes
	r := radio.New(database, icecastURL, musicDir)
	r.SetupRoutes(app)
	r.StartIcecastPoller()

	log.Infof("Starting raga-backend on port %s", GetPort())
	log.Fatal(app.Listen(GetPort()))
}

// GetPort returns the port to listen on
func GetPort() string {
	port := os.Getenv("RAGA_PROXY_PORT")
	if port == "" {
		port = "3000"
	}
	return ":" + port
}

// getEnv returns the value of an environment variable or a default.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
