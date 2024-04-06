package main

import (
	"os"
	"github.com/gofiber/fiber/v2"
	"codeberg.org/aryak/raga-proxy/utils"
)

func main() {
	app := fiber.New(fiber.Config{
		AppName:                 "raga-proxy",
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0"},
		ProxyHeader:             fiber.HeaderXForwardedFor,
	})

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
	app.Listen(GetPort())
}

// GetPort returns the port to listen on
func GetPort() string {
	port := os.Getenv("RAGA_PROXY_PORT")
	if port == "" {
		port = "3000"
	}
	return ":" + port
}
