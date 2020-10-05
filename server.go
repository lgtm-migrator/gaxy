package main

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/valyala/fasthttp"
)

var config = LoadConfig()
var proxyClient = &fasthttp.Client{}

func main() {
	app := Setup()

	// Start server
	fmt.Printf("Listen on port %s", config.Port)
	log.Fatal(app.Listen(fmt.Sprintf(":%s", config.Port)))
}

// Setup Setup a fiber app with all of its routes
func Setup() *fiber.App {
	app := fiber.New()

	// CORS
	app.Use(cors.New())
	// Logger
	app.Use(logger.New())
	// Ping
	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.Send([]byte("pong"))
	})

	// Handler
	app.All("/*", handleRequestAndRedirect)

	return app
}

// Given a request send it to the appropriate url
func handleRequestAndRedirect(c *fiber.Ctx) error {
	req := c.Request()
	resp := c.Response()

	// Overwrite
	url, _ := url.Parse(config.GoogleOrigin)
	req.SetHost(url.Host)
	req.URI().SetScheme(url.Scheme)

	// Prepare request
	prepareRequest(req, c)
	fmt.Printf("GET %s -> making request to %s", c.Params("*"), req.String())

	// Start request to dest URL
	if err := proxyClient.Do(req, resp); err != nil {
		return err
	}
	// Post process the response
	if err := postprocessResponse(resp, c); err != nil {
		return err
	}

	return nil
}

// Prepare request
func prepareRequest(req *fasthttp.Request, c *fiber.Ctx) {
	for _, name := range strings.Split(config.InjectParamsFromReqHeaders, ",") {
		// Convert header fields to request params
		// e.g. INJECT_PARAMS_FROM_REQ_HEADERS=uip,user-agent
		//   will be add this to the URI: ?uip=[VALUE]&user-agent=[VALUE]
		// To rename the key, use [HEADER_NAME]__[NEW_NAME]
		// e.g. INJECT_PARAMS_FROM_REQ_HEADERS=x-email__uip,user-agent__ua

		if name != "" {
			if strings.Contains(name, "__") {
				ss := strings.Split(name, "__")
				val := c.Get(ss[0])
				req.URI().QueryArgs().Add(ss[1], val)
			} else {
				val := c.Get(name)
				req.URI().QueryArgs().Add(name, val)
			}
		}
	}

	// Overwrite IP, UA
	req.URI().QueryArgs().Add("uip", c.IP())
	req.URI().QueryArgs().Add("ua", c.Get("User-Agent"))
}

// Post process response
func postprocessResponse(resp *fasthttp.Response, c *fiber.Ctx) error {
	// Inject
	resp.Header.Add("x-proxy-by", "gaxy")

	if strings.Contains(c.Params("*"), "ga.js") {
		contentEncoding := resp.Header.Peek("Content-Encoding")
		var body []byte
		if bytes.EqualFold(contentEncoding, []byte("gzip")) {
			body, _ = resp.BodyGunzip()
		} else {
			body = resp.Body()
		}
		bodyString := string(body[:])

		url, _ := url.Parse(config.GoogleOrigin)
		currentHost := url.Host
		find := []string{
			"ssl.google-analytics.com",
			"www.google-analytics.com",
			"google-analytics.com",
		}
		for _, toReplace := range find {
			r := strings.NewReplacer(toReplace, currentHost)
			bodyString = r.Replace(bodyString)
		}

		// Error: incorrect header check
		// resp.SetBodyString(bodyString)

		newResp := fasthttp.Response{}
		resp.CopyTo(&newResp)
		newResp.SetBodyString(bodyString)
		resp = &newResp
	}

	return nil
}